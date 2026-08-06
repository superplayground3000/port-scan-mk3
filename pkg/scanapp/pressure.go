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

// PressureFetcher is the interface that fetches router pressure data. Each
// implementation decides how it gets the pressure: with plain HTTP, or with
// OAuth. The returned value is a percentage (for example, 45.0 for 45%).
type PressureFetcher interface {
	// Fetch gets the current pressure value.
	//
	// # Parameters
	//
	//	ctx: Context for the HTTP request, with a timeout and cancellation.
	//
	// # Returns
	//
	//	The pressure as a percentage (for example, 45.0) on success. Fetch
	//	returns an error on failure.
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
// If client is nil, NewSimplePressureFetcher uses a default HTTP client with a
// 2-second timeout.
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

// Fetch makes an HTTP GET to the configured URL and returns the pressure value.
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

// AuthenticatedPressureFetcher is a PressureFetcher that first gets an OAuth
// bearer token from authURL. It then uses this token to fetch pressure data
// from dataURL. It caches each token and refreshes the token automatically
// before the token expires.
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
// If client is nil, NewAuthenticatedPressureFetcher uses a default HTTP client
// with a 2-second timeout.
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

// Fetch gets pressure data with a cached bearer token. If the token expires in
// 30 seconds or less, Fetch refreshes the token automatically.
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

// MultiSourcePressureFetcher fetches pressure from more than one authenticated
// endpoint. These endpoints share the same OAuth credentials. It fans out
// concurrently and returns the maximum pressure across all sources. If one
// source returns an error, the whole Fetch fails.
type MultiSourcePressureFetcher struct {
	sources []PressureFetcher
}

// NewMultiSourcePressureFetcher creates a MultiSourcePressureFetcher that polls
// each URL in dataURLs with shared OAuth credentials. It creates one separate
// AuthenticatedPressureFetcher for each URL, and each one has its own token
// cache. If client is nil, NewMultiSourcePressureFetcher uses a default HTTP
// client with a 2-second timeout.
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

// Fetch gets the pressure from all configured sources concurrently and waits
// for all of them to complete. If a source returns an error, Fetch returns that
// error and wraps it with the source label. If no source returns an error,
// Fetch returns the maximum pressure value across all sources.
func (f *MultiSourcePressureFetcher) Fetch(ctx context.Context) (float64, error) {
	pressure, _, err := f.FetchWithSourceStatuses(ctx)
	return pressure, err
}

// FetchWithSourceStatuses gets the pressure and returns telemetry for each
// source, for dashboard consumers. It keeps the aggregate success and failure
// contract of Fetch.
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
