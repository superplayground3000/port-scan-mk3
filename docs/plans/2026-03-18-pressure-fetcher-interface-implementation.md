# Pressure Fetcher Interface Implementation Plan

**Status:** Historical

**Current architecture:** [port-scan design](../apps/port-scan/DESIGN.md)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor `fetchPressure` into an interface with two implementations (simple and authenticated) to allow swappable fetching logic.

**Architecture:** Create `PressureFetcher` interface in new `pressure.go`, implement `SimplePressureFetcher` (current behavior) and `AuthenticatedPressureFetcher` (new auth flow). Inject via `RunOptions`.

**Tech Stack:** Go 1.24.x, standard library `net/http`, `encoding/json`, `context`

---

### Task 1: Create PressureFetcher interface and SimplePressureFetcher

**Files:**
- Create: `pkg/scanapp/pressure.go`
- Test: `pkg/scanapp/pressure_test.go`

**Step 1: Write the failing test**

```go
// pkg/scanapp/pressure_test.go
package scanapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimplePressureFetcher_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"pressure": 85}`))
	}))
	defer srv.Close()

	fetcher := NewSimplePressureFetcher(srv.URL, srv.Client())
	ctx := context.Background()
	
	pressure, err := fetcher.Fetch(ctx)
	
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pressure != 85 {
		t.Errorf("expected 85, got %d", pressure)
	}
}

func TestSimplePressureFetcher_MissingField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"load": 50}`))
	}))
	defer srv.Close()

	fetcher := NewSimplePressureFetcher(srv.URL, srv.Client())
	ctx := context.Background()
	
	_, err := fetcher.Fetch(ctx)
	
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/... -run TestSimplePressure -v`
Expected: FAIL - undefined: NewSimplePressureFetcher

**Step 3: Write minimal implementation**

```go
// pkg/scanapp/pressure.go
package scanapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// PressureFetcher defines the interface for fetching pressure data.
type PressureFetcher interface {
	Fetch(ctx context.Context) (int, error)
}

// SimplePressureFetcher makes plain HTTP GET requests.
type SimplePressureFetcher struct {
	url    string
	client *http.Client
}

// NewSimplePressureFetcher creates a SimplePressureFetcher.
func NewSimplePressureFetcher(url string, client *http.Client) PressureFetcher {
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	return &SimplePressureFetcher{url: url, client: client}
}

// Fetch retrieves pressure value from the configured URL.
func (f *SimplePressureFetcher) Fetch(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("pressure api status=%d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	raw, ok := body["pressure"]
	if !ok {
		return 0, fmt.Errorf("pressure field missing")
	}
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported pressure field type: %T", raw)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp/... -run TestSimplePressure -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/pressure.go pkg/scanapp/pressure_test.go
git commit -m "feat: add PressureFetcher interface with SimplePressureFetcher"
```

---

### Task 2: Create AuthenticatedPressureFetcher

**Files:**
- Modify: `pkg/scanapp/pressure.go`
- Test: `pkg/scanapp/pressure_test.go`

**Step 1: Write the failing test**

```go
func TestAuthenticatedPressureFetcher_Success(t *testing.T) {
	authCalls := 0
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		r.ParseForm()
		if r.Form.Get("client_id") != "test-client" || r.Form.Get("client_secret") != "test-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok123","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":42.5},"deviceIP":"5.6.7.8","site":"ALL","protocol":"vxlan"}]`))
	}))
	defer dataSrv.Close()

	fetcher := NewAuthenticatedPressureFetcher(authSrv.URL, dataSrv.URL, "test-client", "test-secret", dataSrv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pressure != 42 {
		t.Errorf("expected 42, got %d", pressure)
	}
	if authCalls != 1 {
		t.Errorf("expected 1 auth call, got %d", authCalls)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/... -run TestAuthenticatedPressure -v`
Expected: FAIL - undefined: NewAuthenticatedPressureFetcher

**Step 3: Write minimal implementation**

Add to `pkg/scanapp/pressure.go`:

```go
import (
	"strings"
	"sync"
	"time"
)

// AuthenticatedPressureFetcher handles OAuth-style auth flow.
type AuthenticatedPressureFetcher struct {
	authURL      string
	dataURL      string
	clientID     string
	clientSecret string
	client       *http.Client

	mu           sync.Mutex
	accessToken  string
	expiresAt    time.Time
}

// NewAuthenticatedPressureFetcher creates an AuthenticatedPressureFetcher.
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

// Fetch retrieves pressure value with token authentication.
func (f *AuthenticatedPressureFetcher) Fetch(ctx context.Context) (int, error) {
	// Get valid token (refresh if needed)
	token, err := f.getToken(ctx)
	if err != nil {
		return 0, fmt.Errorf("auth failed: %w", err)
	}

	// Make data request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.dataURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("data api status=%d", resp.StatusCode)
	}

	// Parse array response
	var data []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("no data entries in response")
	}

	// Extract Percent from first element's "data" field
	first := data[0]
	dataObj, ok := first["data"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("data field missing or not object")
	}
	raw, ok := dataObj["Percent"]
	if !ok {
		return 0, fmt.Errorf("Percent field missing")
	}
	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported Percent type: %T", raw)
	}
}

func (f *AuthenticatedPressureFetcher) getToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if we have a valid token (with 30s buffer)
	if f.accessToken != "" && time.Now().Add(30*time.Second).Before(f.expiresAt) {
		return f.accessToken, nil
	}

	// Need to refresh token
	form := fmt.Sprintf("client_id=%s&client_secret=%s", f.clientID, f.clientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.authURL, strings.NewReader(form))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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

	f.accessToken = authResp.AccessToken
	f.expiresAt = time.Now().Add(time.Duration(authResp.ExpiresIn) * time.Second)

	return f.accessToken, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./pkg/scanapp/... -run TestAuthenticatedPressure -v`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/scanapp/pressure.go pkg/scanapp/pressure_test.go
git commit -m "feat: add AuthenticatedPressureFetcher with token caching"
```

---

### Task 3: Update pollPressureAPI to use interface

**Files:**
- Modify: `pkg/scanapp/pressure_monitor.go`
- Test: `pkg/scanapp/pressure_monitor_test.go` (may need to create)

**Step 1: Write the failing test**

```go
func TestPollPressureAPI_UsesInjectedFetcher(t *testing.T) {
	callCount := 0
	mockFetcher := &mockPressureFetcher{pressure: 50, err: nil}
	
	mockFetcher.FetchFn = func(ctx context.Context) (int, error) {
		callCount++
		return mockFetcher.pressure, mockFetcher.err
	}

	cfg := config.Config{}
	opts := RunOptions{
		PressureFetcher: mockFetcher,
		PressureLimit:   40,
	}
	
	ctrl := speedctrl.NewController()
	logger := newLogger("debug", false, &bytes.Buffer{})
	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go pollPressureAPI(ctx, cfg, opts, ctrl, logger, errCh)

	// Wait for at least one fetch
	time.Sleep(50 * time.Millisecond)
	
	if callCount == 0 {
		t.Error("expected at least one fetch call")
	}
	
	// Should be paused since 50 >= 40
	if !ctrl.APIPaused() {
		t.Error("expected API paused")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/scanapp/... -run TestPollPressureAPI_UsesInjectedFetcher -v`
Expected: FAIL - undefined: pollPressureAPI (it's unexported) - need to test differently

**Step 3: Write minimal implementation**

Update `pkg/scanapp/pressure_monitor.go`:

```go
// In pollPressureAPI function, replace direct fetchPressure call:
func pollPressureAPI(ctx context.Context, cfg config.Config, opts RunOptions, ctrl *speedctrl.Controller, logger *scanLogger, errCh chan<- error) {
	interval := cfg.PressureInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	threshold := opts.PressureLimit
	if threshold <= 0 {
		threshold = defaultPressureLimit
	}

	// Determine fetcher: use injected, or create from config
	var fetcher PressureFetcher
	if opts.PressureFetcher != nil {
		fetcher = opts.PressureFetcher
	} else {
		// Backward compatible: use simple fetcher with config
		client := opts.PressureHTTP
		if client == nil {
			client = &http.Client{Timeout: 2 * time.Second}
		}
		fetcher = NewSimplePressureFetcher(cfg.PressureAPI, client)
	}

	var consecutiveFailures int
	var prevPaused bool
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pressure, err := fetcher.Fetch(ctx)
			// ... rest unchanged
```

**Step 4: Run test to verify it passes**

Run: `go build ./...`
Expected: SUCCESS

**Step 5: Commit**

```bash
git add pkg/scanapp/pressure_monitor.go
git commit -m "refactor: pollPressureAPI accepts PressureFetcher interface"
```

---

### Task 4: Update RunOptions to include PressureFetcher

**Files:**
- Modify: `pkg/scanapp/scan.go`

**Step 1: Modify RunOptions struct**

```go
type RunOptions struct {
	Dial             DialFunc
	ResumeStatePath  string
	PressureLimit    int
	DisableKeyboard  bool
	PressureHTTP     *http.Client
	PressureFetcher  PressureFetcher  // NEW
	ProgressInterval int
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add pkg/scanapp/scan.go
git commit -m "feat: add PressureFetcher to RunOptions"
```

---

### Task 5: Add config flags for authenticated fetcher

**Files:**
- Modify: `pkg/config/config.go`

**Step 1: Add new config fields**

```go
type Config struct {
	// ... existing fields
	
	// Pressure API authentication
	PressureAuthURL      string
	PressureDataURL      string
	PressureClientID     string
	PressureClientSecret string
	PressureUseAuth      bool
}
```

**Step 2: Add CLI flags in flag definition**

In the config loading code, add:

```go
flag.StringVar(&cfg.PressureAuthURL, "pressure-auth-url", "", "Auth endpoint URL for authenticated pressure fetcher")
flag.StringVar(&cfg.PressureDataURL, "pressure-data-url", "", "Data endpoint URL for authenticated pressure fetcher")
flag.StringVar(&cfg.PressureClientID, "pressure-client-id", "", "OAuth client_id for pressure API")
flag.StringVar(&cfg.PressureClientSecret, "pressure-client-secret", "", "OAuth client_secret for pressure API")
flag.BoolVar(&cfg.PressureUseAuth, "pressure-use-auth", false, "Use authenticated pressure fetcher")
```

**Step 3: Run build to verify**

Run: `go build ./...`
Expected: SUCCESS

**Step 4: Commit**

```bash
git add pkg/config/config.go
git commit -m "feat: add config flags for authenticated pressure fetcher"
```

---

### Task 6: Wire up fetcher creation in main scan flow

**Files:**
- Modify: `pkg/scanapp/scan.go` (in Run function)

**Step 1: Create fetcher based on config**

Add logic in Run function where pollPressureAPI is called:

```go
// In Run function, before calling pollPressureAPI:
var fetcher PressureFetcher
if !cfg.DisableAPI && cfg.PressureAPI != "" {
	var pressureHTTP *http.Client
	if opts.PressureHTTP != nil {
		pressureHTTP = opts.PressureHTTP
	} else {
		pressureHTTP = &http.Client{Timeout: 2 * time.Second}
	}

	if cfg.PressureUseAuth {
		// Validate required auth flags
		if cfg.PressureAuthURL == "" || cfg.PressureDataURL == "" || 
		   cfg.PressureClientID == "" || cfg.PressureClientSecret == "" {
			return fmt.Errorf("authenticated pressure fetcher requires: pressure-auth-url, pressure-data-url, pressure-client-id, pressure-client-secret")
		}
		fetcher = NewAuthenticatedPressureFetcher(
			cfg.PressureAuthURL,
			cfg.PressureDataURL,
			cfg.PressureClientID,
			cfg.PressureClientSecret,
			pressureHTTP,
		)
	} else {
		fetcher = NewSimplePressureFetcher(cfg.PressureAPI, pressureHTTP)
	}

	// Merge into opts for pollPressureAPI
	opts.PressureFetcher = fetcher
	
	go pollPressureAPI(ctx, cfg, opts, ctrl, logger, errCh)
}
```

**Step 2: Run build to verify**

Run: `go build ./...`
Expected: SUCCESS

**Step 3: Commit**

```bash
git add pkg/scanapp/scan.go
git commit -m "feat: wire up PressureFetcher creation in scan flow"
```

---

### Task 7: Update tests for backward compatibility

**Files:**
- Modify: `pkg/scanapp/scan_test.go`, `pkg/scanapp/scan_helpers_test.go`

**Step 1: Run existing tests**

Run: `go test ./pkg/scanapp/... -v 2>&1 | head -100`
Expected: Should pass (backward compatible - nil fetcher uses simple)

**Step 2: If any tests fail, fix them**

(Address any compilation or test failures)

**Step 3: Commit**

```bash
git add pkg/scanapp/scan_test.go pkg/scanapp/scan_helpers_test.go
git commit -m "test: ensure backward compatibility"
```

---

### Task 8: Run full test suite

**Step 1: Run all tests**

Run: `go test ./... -v 2>&1 | tail -50`
Expected: All tests pass

**Step 2: Run linter**

Run: `go vet ./...`
Expected: No errors

**Step 3: Final commit if needed**

```bash
git add -A
git commit -m "feat: implement PressureFetcher interface with auth support"
```

---

## Plan complete

Two execution options:

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

Which approach?
