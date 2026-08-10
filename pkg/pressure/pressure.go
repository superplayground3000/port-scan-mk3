// Package pressure provides HTTP adapters that sample router pressure.
package pressure

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

// Sample contains the aggregate value and optional per-source results from one poll.
type Sample struct {
	// Maximum is the largest normalized pressure from a successful poll.
	// It is zero when a poll returns an error.
	Maximum float64
	// Sources contains OAuth results in configuration order.
	// SimpleHTTP leaves this slice empty.
	Sources []SourceResult
}

// SourceResult contains the result from one pressure endpoint.
type SourceResult struct {
	// Name is the stable srcN label for the configured endpoint.
	Name string
	// Pressure is the normalized endpoint value. Zero and negative values are valid.
	Pressure float64
	// Err contains the endpoint failure. It is nil after a successful request.
	Err error
}

// OAuthConfig contains shared OAuth credentials and pressure endpoints.
type OAuthConfig struct {
	// AuthEndpoint is the absolute HTTP or HTTPS token URL.
	AuthEndpoint string
	// DataEndpoints contains absolute pressure URLs in polling and result order.
	DataEndpoints []string
	// ClientID is the OAuth form client_id value.
	ClientID string
	// ClientSecret is the OAuth form client_secret value.
	ClientSecret string
}

// SimpleHTTP polls one unauthenticated pressure endpoint.
type SimpleHTTP struct {
	endpoint string
	client   *http.Client
}

// OAuthMulti polls authenticated pressure endpoints concurrently.
type OAuthMulti struct {
	sources []*oauthSource
}

type oauthSource struct {
	authEndpoint string
	dataEndpoint string
	clientID     string
	clientSecret string
	client       *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewSimpleHTTP returns an unauthenticated pressure adapter.
// The endpoint must be an absolute HTTP or HTTPS URL. The client must be non-nil.
// NewSimpleHTTP returns a configuration error before it sends a request.
func NewSimpleHTTP(endpoint string, client *http.Client) (*SimpleHTTP, error) {
	if err := validateEndpoint("pressure endpoint", endpoint); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("pressure HTTP client is required")
	}
	return &SimpleHTTP{endpoint: endpoint, client: client}, nil
}

// NewOAuthMulti returns an authenticated multi-source adapter.
// The config requires valid auth and data URLs, at least one data URL,
// and non-empty credentials. The client must be non-nil.
// The constructor copies endpoint values into private source state.
// It returns a configuration error before it sends a request.
func NewOAuthMulti(cfg OAuthConfig, client *http.Client) (*OAuthMulti, error) {
	if err := validateEndpoint("OAuth endpoint", cfg.AuthEndpoint); err != nil {
		return nil, err
	}
	if len(cfg.DataEndpoints) == 0 {
		return nil, fmt.Errorf("at least one pressure endpoint is required")
	}
	for _, endpoint := range cfg.DataEndpoints {
		if err := validateEndpoint("pressure endpoint", endpoint); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, fmt.Errorf("OAuth client ID is required")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, fmt.Errorf("OAuth client secret is required")
	}
	if client == nil {
		return nil, fmt.Errorf("pressure HTTP client is required")
	}

	sources := make([]*oauthSource, len(cfg.DataEndpoints))
	for i, endpoint := range cfg.DataEndpoints {
		sources[i] = &oauthSource{
			authEndpoint: cfg.AuthEndpoint,
			dataEndpoint: endpoint,
			clientID:     cfg.ClientID,
			clientSecret: cfg.ClientSecret,
			client:       client,
		}
	}
	return &OAuthMulti{sources: sources}, nil
}

func validateEndpoint(name, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", name)
	}
	return nil
}

// Sample sends one GET request with the supplied context.
// A successful result contains normalized pressure in Maximum and no source results.
// Zero and negative pressure values are valid.
// Sample returns an error for cancellation, request, status, JSON, field, or value failures.
func (s *SimpleHTTP) Sample(ctx context.Context) (Sample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return Sample{}, fmt.Errorf("create pressure request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Sample{}, fmt.Errorf("get pressure data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return Sample{}, fmt.Errorf("pressure api status=%d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Sample{}, fmt.Errorf("decode pressure response: %w", err)
	}
	raw, ok := body["pressure"]
	if !ok {
		return Sample{}, fmt.Errorf("pressure field missing")
	}
	value, err := parseValue(raw)
	if err != nil {
		return Sample{}, fmt.Errorf("parse pressure value: %w", err)
	}
	return Sample{Maximum: value}, nil
}

// Sample polls all configured endpoints concurrently and waits for every result.
// A successful result contains a new source slice in configuration order.
// Maximum contains the largest normalized value, including zero or negative values.
// If one source fails, Maximum is zero and Sources still contains every result.
// The returned error wraps the first failed source in configuration order.
// Source failures include context, authentication, request, status, and response errors.
func (m *OAuthMulti) Sample(ctx context.Context) (Sample, error) {
	results := make([]SourceResult, len(m.sources))
	var wg sync.WaitGroup
	for i, source := range m.sources {
		wg.Add(1)
		go func(index int, source *oauthSource) {
			defer wg.Done()
			value, err := source.sample(ctx)
			results[index] = SourceResult{
				Name:     fmt.Sprintf("src%d", index+1),
				Pressure: value,
				Err:      err,
			}
		}(i, source)
	}
	wg.Wait()

	sample := Sample{Sources: results}
	for _, result := range results {
		if result.Err != nil {
			return sample, fmt.Errorf("%s: %w", result.Name, result.Err)
		}
	}
	for i, result := range results {
		if i == 0 || result.Pressure > sample.Maximum {
			sample.Maximum = result.Pressure
		}
	}
	return sample, nil
}

func (s *oauthSource) sample(ctx context.Context) (float64, error) {
	token, err := s.token(ctx)
	if err != nil {
		return 0, fmt.Errorf("get token: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.dataEndpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("create data request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get pressure data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return 0, fmt.Errorf("data api status=%d", resp.StatusCode)
	}

	var entries []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return 0, fmt.Errorf("decode pressure data: %w", err)
	}

	var (
		maximum float64
		found   bool
	)
	for _, entry := range entries {
		data, ok := entry["data"].(map[string]any)
		if !ok {
			continue
		}
		value, ok := data["Percent"]
		if !ok {
			continue
		}
		pressure, err := parseValue(value)
		if err != nil {
			continue
		}
		if !found || pressure > maximum {
			maximum = pressure
		}
		found = true
	}
	if !found {
		return 0, fmt.Errorf("no valid Percent values found in response")
	}
	return maximum, nil
}

func (s *oauthSource) token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && time.Now().Add(30*time.Second).Before(s.expiresAt) {
		return s.accessToken, nil
	}

	form := url.Values{
		"client_id":     {s.clientID},
		"client_secret": {s.clientSecret},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.authEndpoint, strings.NewReader(form))
	if err != nil {
		return "", fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("get auth token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("auth status=%d", resp.StatusCode)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode auth response: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("access_token missing in auth response")
	}
	if !strings.EqualFold(result.TokenType, "Bearer") {
		return "", fmt.Errorf("unexpected token_type: %s", result.TokenType)
	}

	s.accessToken = result.AccessToken
	s.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	return s.accessToken, nil
}

func parseValue(raw any) (float64, error) {
	var value float64
	switch typed := raw.(type) {
	case float64:
		value = typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, err
		}
		value = parsed
	default:
		return 0, fmt.Errorf("unsupported pressure field type: %T", raw)
	}
	return math.Round(value*10) / 10, nil
}
