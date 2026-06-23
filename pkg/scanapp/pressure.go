package scanapp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PressureFetcher defines the interface for fetching router pressure data.
// Implementations determine how pressure is retrieved (plain HTTP, OAuth, etc.).
// The returned value is a percentage (e.g., 45.0 for 45%).
type PressureFetcher interface {
	// Fetch retrieves the current pressure value.
	//
	// # Parameters
	//
	//	ctx: Context for the HTTP request with timeout/cancellation.
	//
	// # Returns
	//
	//	Pressure as a percentage (e.g., 45.0) on success; error on failure.
	Fetch(ctx context.Context) (float64, error)
}

type pressureSourceStatusFetcher interface {
	FetchWithSourceStatuses(ctx context.Context) (float64, []PressureSourceResult, error)
}

// PressureSourceResult describes one pressure source result from a multi-source poll.
type PressureSourceResult struct {
	Name     string
	Pressure float64
	Err      error
}

// SimplePressureFetcher is a PressureFetcher that makes unauthenticated HTTP GET
// requests to a pressure API endpoint. It expects a JSON response with a
// "pressure" field (number or numeric string).
type SimplePressureFetcher struct {
	url    string
	client *http.Client
}

// NewSimplePressureFetcher creates a SimplePressureFetcher for the given URL.
// If client is nil, a default HTTP client with a 2-second timeout is used.
//
// # Example
//
//	fetcher := NewSimplePressureFetcher("http://localhost:8080/api/pressure", nil)
func NewSimplePressureFetcher(url string, client *http.Client) PressureFetcher {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &SimplePressureFetcher{url: url, client: client}
}

// Fetch performs an HTTP GET to the configured URL and returns the pressure value.
// It expects a JSON body: {"pressure": <number>}.
func (f *SimplePressureFetcher) Fetch(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return 0.0, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return 0.0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return 0.0, fmt.Errorf("pressure api status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0.0, err
	}
	raw, ok := body["pressure"]
	if !ok {
		return 0.0, fmt.Errorf("pressure field missing")
	}
	pressure, err := parsePressureValue(raw)
	if err != nil {
		return 0.0, err
	}
	return pressure, nil
}

// AuthenticatedPressureFetcher is a PressureFetcher that first obtains an OAuth
// bearer token from authURL, then uses it to fetch pressure data from dataURL.
// It caches and automatically refreshes tokens before expiry.
type AuthenticatedPressureFetcher struct {
	authURL      string
	dataURL      string
	clientID     string
	clientSecret string
	client       *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewAuthenticatedPressureFetcher creates an AuthenticatedPressureFetcher.
// If client is nil, a default HTTP client with a 2-second timeout is used.
//
// # Example
//
//	fetcher := NewAuthenticatedPressureFetcher(
//	    "https://auth.example.com/token",
//	    "https://api.example.com/pressure",
//	    "client-id", "secret", nil,
//	)
func NewAuthenticatedPressureFetcher(authURL, dataURL, clientID, clientSecret string, client *http.Client) PressureFetcher {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &AuthenticatedPressureFetcher{
		authURL:      authURL,
		dataURL:      dataURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		client:       client,
	}
}

// Fetch retrieves pressure data using a cached bearer token. It automatically
// refreshes the token when it is within 30 seconds of expiry.
func (f *AuthenticatedPressureFetcher) Fetch(ctx context.Context) (float64, error) {
	// Get valid token (refresh if needed)
	token, err := f.getToken(ctx)
	if err != nil {
		return 0.0, fmt.Errorf("auth failed: %w", err)
	}

	// Make data request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.dataURL, nil)
	if err != nil {
		return 0.0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.client.Do(req)
	if err != nil {
		return 0.0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return 0.0, fmt.Errorf("data api status=%d", resp.StatusCode)
	}

	// Parse array response
	var data []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0.0, fmt.Errorf("failed to decode response: %w", err)
	}
	if len(data) == 0 {
		return 0.0, fmt.Errorf("no data entries in response")
	}

	// Loop through all entries and find the maximum Percent value.
	// Use foundValid to distinguish "no valid value" from a valid 0 percent.
	var (
		maxPressure float64
		foundValid  bool
	)
	for _, entry := range data {
		dataObj, ok := entry["data"].(map[string]any)
		if !ok {
			continue // skip entries without valid data
		}
		raw, ok := dataObj["Percent"]
		if !ok {
			continue // skip entries without Percent
		}
		pressure, err := parsePressureValue(raw)
		if err != nil {
			continue // skip invalid values
		}
		if !foundValid || pressure > maxPressure {
			maxPressure = pressure
		}
		foundValid = true
	}

	if !foundValid {
		return 0.0, fmt.Errorf("no valid Percent values found in response")
	}

	return maxPressure, nil
}

func parsePressureValue(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return normalizePressure(v), nil
	case float32:
		return normalizePressure(float64(v)), nil
	case int:
		return normalizePressure(float64(v)), nil
	case int32:
		return normalizePressure(float64(v)), nil
	case int64:
		return normalizePressure(float64(v)), nil
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0.0, err
		}
		return normalizePressure(n), nil
	default:
		return 0.0, fmt.Errorf("unsupported pressure field type: %T", raw)
	}
}

func normalizePressure(v float64) float64 {
	return math.Round(v*10) / 10
}

// MultiSourcePressureFetcher fetches pressure from multiple authenticated
// endpoints that share the same OAuth credentials. It fans out concurrently
// and returns the maximum pressure across all sources. Any source error
// causes the entire Fetch to fail.
type MultiSourcePressureFetcher struct {
	sources []PressureFetcher
}

// NewMultiSourcePressureFetcher creates a MultiSourcePressureFetcher that
// polls each URL in dataURLs using shared OAuth credentials. A separate
// AuthenticatedPressureFetcher (with its own token cache) is created per URL.
// If client is nil, a default HTTP client with a 2-second timeout is used.
func NewMultiSourcePressureFetcher(authURL string, dataURLs []string, clientID, clientSecret string, client *http.Client) *MultiSourcePressureFetcher {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	sources := make([]PressureFetcher, len(dataURLs))
	for i, u := range dataURLs {
		sources[i] = NewAuthenticatedPressureFetcher(authURL, u, clientID, clientSecret, client)
	}
	return &MultiSourcePressureFetcher{sources: sources}
}

// Fetch retrieves pressure from all configured sources concurrently and
// waits for all to complete. If any source returns an error, Fetch returns
// that error (wrapping it with the source label). Otherwise it returns the
// maximum pressure value across all sources.
func (f *MultiSourcePressureFetcher) Fetch(ctx context.Context) (float64, error) {
	pressure, _, err := f.FetchWithSourceStatuses(ctx)
	return pressure, err
}

// FetchWithSourceStatuses retrieves pressure and returns per-source telemetry
// for dashboard consumers while preserving Fetch's aggregate success/failure contract.
func (f *MultiSourcePressureFetcher) FetchWithSourceStatuses(ctx context.Context) (float64, []PressureSourceResult, error) {
	if len(f.sources) == 0 {
		return 0, nil, fmt.Errorf("no pressure sources configured")
	}
	results := make([]PressureSourceResult, len(f.sources))
	var wg sync.WaitGroup
	for i, src := range f.sources {
		wg.Add(1)
		go func(i int, src PressureFetcher) {
			defer wg.Done()
			v, err := src.Fetch(ctx)
			results[i] = PressureSourceResult{
				Name:     fmt.Sprintf("src%d", i+1),
				Pressure: v,
				Err:      err,
			}
		}(i, src)
	}
	wg.Wait()

	var maxPressure float64
	var firstErr error
	var foundPressure bool
	for _, r := range results {
		if r.Err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", r.Name, r.Err)
			}
			continue
		}
		if !foundPressure || r.Pressure > maxPressure {
			maxPressure = r.Pressure
		}
		foundPressure = true
	}
	if firstErr != nil {
		return 0, results, firstErr
	}
	return maxPressure, results, nil
}

func (f *AuthenticatedPressureFetcher) getToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if we have a valid token (with 30s buffer)
	if f.accessToken != "" && time.Now().Add(30*time.Second).Before(f.expiresAt) {
		return f.accessToken, nil
	}

	// Need to refresh token
	form := url.Values{
		"client_id":     {f.clientID},
		"client_secret": {f.clientSecret},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.authURL, strings.NewReader(form))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("auth status=%d", resp.StatusCode)
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to decode auth response: %w", err)
	}
	if authResp.AccessToken == "" {
		return "", fmt.Errorf("access_token missing in auth response")
	}
	if !strings.EqualFold(authResp.TokenType, "Bearer") {
		return "", fmt.Errorf("unexpected token_type: %s (expected Bearer)", authResp.TokenType)
	}

	f.accessToken = authResp.AccessToken
	f.expiresAt = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)

	return f.accessToken, nil
}
