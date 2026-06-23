package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	useAuth           func() bool
	validClientID     func() string
	validClientSecret func() string
	pressureValue1    func() string
	pressureValue2    func() string
	responseConfig    func() string
)

func init() {
	useAuth = func() bool { return getenv("USE_AUTH", "false") == "true" }
	validClientID = func() string { return getenv("AUTH_CLIENT_ID", "test-client") }
	validClientSecret = func() string { return getenv("AUTH_CLIENT_SECRET", "test-secret") }
	pressureValue1 = func() string { return getenv("PRESSURE_VALUE_1", "85") }
	pressureValue2 = func() string { return getenv("PRESSURE_VALUE_2", "72") }
	responseConfig = func() string { return getenv("PRESSURE_RESPONSE_CONFIG", "") }
}

var validTokens = struct {
	sync.RWMutex
	tokens map[string]time.Time
}{tokens: make(map[string]time.Time)}

var configStore *responseConfigStore

type pressureState struct {
	mu       sync.Mutex
	current  int
	sequence []int
	index    int
	loop     bool
}

func newPressureState(initial int, sequence []int, loop bool) *pressureState {
	copied := append([]int(nil), sequence...)
	return &pressureState{
		current:  initial,
		sequence: copied,
		loop:     loop,
	}
}

func (s *pressureState) next() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sequence) == 0 {
		return s.current
	}

	value := s.sequence[s.index]
	if s.index < len(s.sequence)-1 {
		s.index++
	} else if s.loop {
		s.index = 0
	}
	s.current = value
	return value
}

func (s *pressureState) setCurrent(v int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = v
	s.sequence = nil
	s.index = 0
	s.loop = false
}

func (s *pressureState) setSequence(values []int, loop bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence = append([]int(nil), values...)
	s.index = 0
	s.loop = loop
}

func parsePressureSequence(raw string) ([]int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			return nil, fmt.Errorf("pressure sequence contains empty token")
		}
		value, err := strconv.Atoi(token)
		if err != nil {
			return nil, fmt.Errorf("invalid pressure sequence token %q: %w", token, err)
		}
		values = append(values, value)
	}
	return values, nil
}

type responseBodyConfig struct {
	ResponseBody json.RawMessage `json:"response_body"`
}

type responseConfigStore struct {
	mu   sync.RWMutex
	path string
	body json.RawMessage
}

func newResponseConfigStore(path string) (*responseConfigStore, error) {
	body, err := loadResponseBodyConfig(path)
	if err != nil {
		return nil, err
	}
	return &responseConfigStore{
		path: path,
		body: cloneRawJSON(body),
	}, nil
}

func loadResponseBodyConfig(path string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config failed: %w", err)
	}

	var cfg responseBodyConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config failed: %w", err)
	}

	trimmed := bytes.TrimSpace(cfg.ResponseBody)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("response_body is required")
	}
	if !json.Valid(trimmed) {
		return nil, fmt.Errorf("response_body is not valid json")
	}
	return cloneRawJSON(trimmed), nil
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func (s *responseConfigStore) Reload() error {
	body, err := loadResponseBodyConfig(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = cloneRawJSON(body)
	return nil
}

func (s *responseConfigStore) Body() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneRawJSON(s.body)
}

func (s *responseConfigStore) Path() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path
}

func main() {
	mode := getenv("MODE", "ok")
	addr := getenv("ADDR", ":8080")
	pressure := getenvInt("PRESSURE", 20)
	delayMS := getenvInt("DELAY_MS", 5000)
	sequence, err := parsePressureSequence(getenv("PRESSURE_SEQUENCE", ""))
	if err != nil {
		log.Fatalf("invalid PRESSURE_SEQUENCE: %v", err)
	}
	state := newPressureState(pressure, sequence, getenv("PRESSURE_SEQUENCE_LOOP", "false") == "true")

	if path := responseConfig(); path != "" {
		store, err := newResponseConfigStore(path)
		if err != nil {
			log.Fatalf("load PRESSURE_RESPONSE_CONFIG failed: %v", err)
		}
		configStore = store
		log.Printf("loaded response config from %s", path)
	}

	mux := newMux()
	mux.HandleFunc("/api/pressure", newPressureHandler(mode, delayMS, state))

	log.Printf("mock pressure api listening on %s mode=%s useAuth=%v", addr, mode, useAuth())
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", handleAuth)
	mux.HandleFunc("/data", handleData)
	mux.HandleFunc("/admin/config", handleConfigInfo)
	mux.HandleFunc("/admin/config/reload", handleConfigReload)
	return mux
}

func newPressureHandler(mode string, delayMS int, state *pressureState) http.HandlerFunc {
	return newPressureHandlerWithConfig(mode, delayMS, state, configStore)
}

func newPressureHandlerWithConfig(mode string, delayMS int, state *pressureState, store *responseConfigStore) http.HandlerFunc {
	writeBody := func(w http.ResponseWriter) {
		if store != nil {
			body := store.Body()
			if len(body) > 0 {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
				return
			}
		}

		pressure := 0
		if state != nil {
			pressure = state.next()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"pressure": pressure})
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handlePressureUpdate(w, r, state)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		switch mode {
		case "ok":
			writeBody(w)
		case "fail":
			http.Error(w, "mock fail", http.StatusInternalServerError)
		case "timeout":
			time.Sleep(time.Duration(delayMS) * time.Millisecond)
			writeBody(w)
		default:
			http.Error(w, "unknown mode", http.StatusInternalServerError)
		}
	}
}

type pressureUpdateRequest struct {
	Pressure *int  `json:"pressure"`
	Sequence []int `json:"sequence"`
	Loop     *bool `json:"loop"`
}

func handlePressureUpdate(w http.ResponseWriter, r *http.Request, state *pressureState) {
	if state == nil {
		http.Error(w, "pressure state unavailable", http.StatusInternalServerError)
		return
	}

	var req pressureUpdateRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	if req.Pressure != nil && req.Sequence != nil {
		http.Error(w, "pressure and sequence are mutually exclusive", http.StatusBadRequest)
		return
	}

	switch {
	case req.Pressure != nil:
		state.setCurrent(*req.Pressure)
	case req.Sequence != nil:
		if len(req.Sequence) == 0 {
			http.Error(w, "sequence cannot be empty", http.StatusBadRequest)
			return
		}
		loop := false
		if req.Loop != nil {
			loop = *req.Loop
		}
		state.setSequence(req.Sequence, loop)
	default:
		http.Error(w, "either pressure or sequence is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleConfigInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if configStore == nil {
		http.Error(w, "config mode not enabled", http.StatusServiceUnavailable)
		return
	}

	body := configStore.Body()
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		http.Error(w, "current config body is invalid", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"path":          configStore.Path(),
		"response_body": decoded,
	})
}

func handleConfigReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if configStore == nil {
		http.Error(w, "config mode not enabled", http.StatusServiceUnavailable)
		return
	}
	if err := configStore.Reload(); err != nil {
		http.Error(w, fmt.Sprintf("reload failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		http.Error(w, "invalid content type", http.StatusBadRequest)
		return
	}

	if !useAuth() {
		http.Error(w, "auth not enabled", http.StatusServiceUnavailable)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}

	clientID := r.Form.Get("client_id")
	clientSecret := r.Form.Get("client_secret")

	if clientID != validClientID() || clientSecret != validClientSecret() {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token := "mock-token-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	validTokens.Lock()
	validTokens.tokens[token] = time.Now().Add(3600 * time.Second)
	validTokens.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func handleData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !useAuth() {
		http.Error(w, "auth not enabled", http.StatusServiceUnavailable)
		return
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	validTokens.RLock()
	expiry, ok := validTokens.tokens[token]
	validTokens.RUnlock()

	if !ok || time.Now().After(expiry) {
		http.Error(w, "invalid or expired token", http.StatusUnauthorized)
		return
	}

	p1, err := strconv.Atoi(pressureValue1())
	if err != nil {
		log.Printf("WARNING: invalid PRESSURE_VALUE_1 %q — using 0: %v", pressureValue1(), err)
	}
	p2, err := strconv.Atoi(pressureValue2())
	if err != nil {
		log.Printf("WARNING: invalid PRESSURE_VALUE_2 %q — using 0: %v", pressureValue2(), err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode([]map[string]any{
		{"data": map[string]any{"Percent": p1}},
		{"data": map[string]any{"Percent": p2}},
	})
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
