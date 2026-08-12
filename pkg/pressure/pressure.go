// Package pressure provides HTTP adapters that sample router pressure.
package pressure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultResponseSizeLimitBytes is the default size for each HTTP response.
	DefaultResponseSizeLimitBytes uint64 = 1_000_000
	// DefaultResponseEntryLimit is the default entry count for each OAuth data array.
	DefaultResponseEntryLimit uint64 = 10_000
)

// ResponseLimits controls the byte count for each HTTP response and the entry count for each OAuth data array.
// A zero maximum disables only that limit.
type ResponseLimits struct {
	MaxBytes   uint64
	MaxEntries uint64
}

// DefaultResponseLimits returns the default byte and OAuth entry limits.
// It does not create an HTTP adapter and cannot return an error.
func DefaultResponseLimits() ResponseLimits {
	return ResponseLimits{MaxBytes: DefaultResponseSizeLimitBytes, MaxEntries: DefaultResponseEntryLimit}
}

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
	limits   ResponseLimits
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
	limits       ResponseLimits

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewSimpleHTTP returns an unauthenticated pressure adapter.
// The endpoint must be an absolute HTTP or HTTPS URL. The client must be non-nil.
// NewSimpleHTTP returns a configuration error before it sends a request.
func NewSimpleHTTP(endpoint string, client *http.Client) (*SimpleHTTP, error) {
	return NewSimpleHTTPWithLimits(endpoint, client, DefaultResponseLimits())
}

// NewSimpleHTTPWithLimits returns an unauthenticated adapter for endpoint.
// The client sends requests, and limits control each response.
// It returns an error for an invalid endpoint or nil client.
func NewSimpleHTTPWithLimits(endpoint string, client *http.Client, limits ResponseLimits) (*SimpleHTTP, error) {
	if err := validateEndpoint("pressure endpoint", endpoint); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("pressure HTTP client is required")
	}
	return &SimpleHTTP{endpoint: endpoint, client: client, limits: limits}, nil
}

// NewOAuthMulti returns an authenticated multi-source adapter.
// The config requires valid auth and data URLs, at least one data URL,
// and non-empty credentials. The client must be non-nil.
// The constructor copies endpoint values into private source state.
// It returns a configuration error before it sends a request.
func NewOAuthMulti(cfg OAuthConfig, client *http.Client) (*OAuthMulti, error) {
	return NewOAuthMultiWithLimits(cfg, client, DefaultResponseLimits())
}

// NewOAuthMultiWithLimits returns an authenticated adapter for all configured endpoints.
// The client sends requests, and limits control each token and data response.
// It returns an error for invalid endpoints, missing credentials, no data endpoints, or a nil client.
func NewOAuthMultiWithLimits(cfg OAuthConfig, client *http.Client, limits ResponseLimits) (*OAuthMulti, error) {
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
			limits:       limits,
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

	decoder, consumed, err := responseDecoder(resp, s.endpoint, "simple response", s.limits.MaxBytes)
	if err != nil {
		return Sample{}, err
	}
	var body struct {
		Pressure json.RawMessage `json:"pressure"`
	}
	if err := decodeCompleteResponse(decoder, consumed, s.endpoint, "simple response", s.limits.MaxBytes, &body); err != nil {
		return Sample{}, fmt.Errorf("decode pressure response: %w", err)
	}
	if len(body.Pressure) == 0 {
		return Sample{}, fmt.Errorf("pressure field missing")
	}
	var raw any
	if err := json.Unmarshal(body.Pressure, &raw); err != nil {
		return Sample{}, fmt.Errorf("decode pressure field: %w", err)
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

	var (
		maximum float64
		found   bool
	)
	decoder, consumed, err := responseDecoder(resp, s.dataEndpoint, "OAuth data response", s.limits.MaxBytes)
	if err != nil {
		return 0, err
	}
	firstToken, err := decoder.Token()
	if err != nil {
		return 0, pressureDecodeError(consumed, s.dataEndpoint, "OAuth data response", s.limits.MaxBytes, err)
	}
	delim, ok := firstToken.(json.Delim)
	if !ok || delim != '[' {
		return 0, fmt.Errorf("decode pressure data: expected JSON array")
	}
	var count uint64
	for decoder.More() {
		if err := incrementResponseCount(&count, "OAuth data entries"); err != nil {
			return 0, fmt.Errorf("pressure endpoint %s OAuth data response: %w", s.dataEndpoint, err)
		}
		if s.limits.MaxEntries > 0 && count > s.limits.MaxEntries {
			return 0, fmt.Errorf("pressure endpoint %s OAuth data response count %d exceeds limit %d; use -pressure-response-entry-limit to override it", s.dataEndpoint, count, s.limits.MaxEntries)
		}
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			return 0, pressureDecodeError(consumed, s.dataEndpoint, "OAuth data response", s.limits.MaxBytes, err)
		}
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
	if _, err := decoder.Token(); err != nil {
		return 0, pressureDecodeError(consumed, s.dataEndpoint, "OAuth data response", s.limits.MaxBytes, err)
	}
	if err := requireResponseEOF(decoder, consumed, s.dataEndpoint, "OAuth data response", s.limits.MaxBytes); err != nil {
		return 0, err
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

	decoder, consumed, err := responseDecoder(resp, s.authEndpoint, "OAuth token response", s.limits.MaxBytes)
	if err != nil {
		return "", err
	}
	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := decodeCompleteResponse(decoder, consumed, s.authEndpoint, "OAuth token response", s.limits.MaxBytes, &result); err != nil {
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

func responseDecoder(resp *http.Response, endpoint, responseType string, maxBytes uint64) (*json.Decoder, *countingReader, error) {
	if maxBytes > 0 && resp.ContentLength >= 0 && uint64(resp.ContentLength) > maxBytes {
		return nil, nil, pressureSizeError(endpoint, responseType, uint64(resp.ContentLength), maxBytes)
	}
	reader := &countingReader{reader: resp.Body}
	var body io.Reader = reader
	if maxBytes > 0 && maxBytes < math.MaxInt64 {
		body = io.LimitReader(reader, int64(maxBytes)+1)
	}
	return json.NewDecoder(body), reader, nil
}

type countingReader struct {
	reader io.Reader
	count  uint64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if uint64(n) > math.MaxUint64-r.count {
		return n, fmt.Errorf("response byte count overflows the supported range")
	}
	r.count += uint64(n)
	return n, err
}

func incrementResponseCount(count *uint64, kind string) error {
	if *count == math.MaxUint64 {
		return fmt.Errorf("%s count overflows the supported range", kind)
	}
	*count++
	return nil
}

func decodeCompleteResponse(decoder *json.Decoder, consumed *countingReader, endpoint, responseType string, maxBytes uint64, target any) error {
	if err := decoder.Decode(target); err != nil {
		return pressureDecodeError(consumed, endpoint, responseType, maxBytes, err)
	}
	return requireResponseEOF(decoder, consumed, endpoint, responseType, maxBytes)
}

func requireResponseEOF(decoder *json.Decoder, consumed *countingReader, endpoint, responseType string, maxBytes uint64) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if maxBytes > 0 && consumed.count > maxBytes {
		return pressureSizeError(endpoint, responseType, consumed.count, maxBytes)
	}
	if !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("pressure endpoint %s %s has trailing JSON content", endpoint, responseType)
		}
		return err
	}
	return nil
}

func pressureDecodeError(consumed *countingReader, endpoint, responseType string, maxBytes uint64, err error) error {
	if maxBytes > 0 && consumed.count > maxBytes {
		return pressureSizeError(endpoint, responseType, consumed.count, maxBytes)
	}
	return err
}

func pressureSizeError(endpoint, responseType string, size, limit uint64) error {
	return fmt.Errorf("pressure endpoint %s %s size %d bytes exceeds limit %d bytes; use -pressure-response-size-limit-mb to override it", endpoint, responseType, size, limit)
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
