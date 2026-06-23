package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewPressureHandler_OK(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)

	newPressureHandler("ok", 0, newPressureState(42, nil, false))(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var body map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if body["pressure"] != 42 {
		t.Fatalf("unexpected pressure: %v", body)
	}
}

func TestNewPressureHandler_Fail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)

	newPressureHandler("fail", 0, newPressureState(0, nil, false))(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestNewPressureHandler_Timeout(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)

	newPressureHandler("timeout", 1, newPressureState(7, nil, false))(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var body map[string]int
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if body["pressure"] != 7 {
		t.Fatalf("unexpected pressure: %v", body)
	}
}

func TestNewPressureHandler_OKSequence(t *testing.T) {
	handler := newPressureHandler("ok", 0, newPressureState(0, []int{20, 95, 30}, false))

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec1.Code)
	}
	var body1 map[string]int
	if err := json.Unmarshal(rec1.Body.Bytes(), &body1); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if body1["pressure"] != 20 {
		t.Fatalf("expected first pressure=20, got %v", body1["pressure"])
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(rec2, req2)
	var body2 map[string]int
	if err := json.Unmarshal(rec2.Body.Bytes(), &body2); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if body2["pressure"] != 95 {
		t.Fatalf("expected second pressure=95, got %v", body2["pressure"])
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(rec3, req3)
	var body3 map[string]int
	if err := json.Unmarshal(rec3.Body.Bytes(), &body3); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if body3["pressure"] != 30 {
		t.Fatalf("expected third pressure=30, got %v", body3["pressure"])
	}
}

func TestNewPressureHandler_PostPressureThenResume(t *testing.T) {
	handler := newPressureHandler("ok", 0, newPressureState(20, nil, false))

	pauseRec := httptest.NewRecorder()
	pauseReq := httptest.NewRequest(http.MethodPost, "/api/pressure", strings.NewReader(`{"pressure":95}`))
	pauseReq.Header.Set("Content-Type", "application/json")
	handler(pauseRec, pauseReq)
	if pauseRec.Code != http.StatusOK {
		t.Fatalf("unexpected status from pause update: %d", pauseRec.Code)
	}

	getPauseRec := httptest.NewRecorder()
	getPauseReq := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(getPauseRec, getPauseReq)
	var pausedBody map[string]int
	if err := json.Unmarshal(getPauseRec.Body.Bytes(), &pausedBody); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if pausedBody["pressure"] != 95 {
		t.Fatalf("expected paused pressure=95, got %v", pausedBody["pressure"])
	}

	resumeRec := httptest.NewRecorder()
	resumeReq := httptest.NewRequest(http.MethodPost, "/api/pressure", strings.NewReader(`{"pressure":20}`))
	resumeReq.Header.Set("Content-Type", "application/json")
	handler(resumeRec, resumeReq)
	if resumeRec.Code != http.StatusOK {
		t.Fatalf("unexpected status from resume update: %d", resumeRec.Code)
	}

	getResumeRec := httptest.NewRecorder()
	getResumeReq := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(getResumeRec, getResumeReq)
	var resumedBody map[string]int
	if err := json.Unmarshal(getResumeRec.Body.Bytes(), &resumedBody); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if resumedBody["pressure"] != 20 {
		t.Fatalf("expected resumed pressure=20, got %v", resumedBody["pressure"])
	}
}

func TestParsePressureSequence(t *testing.T) {
	values, err := parsePressureSequence("20,95,30")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(values) != 3 || values[0] != 20 || values[1] != 95 || values[2] != 30 {
		t.Fatalf("unexpected parsed values: %#v", values)
	}
	if _, err := parsePressureSequence("20,bad,30"); err == nil {
		t.Fatal("expected parse error for invalid sequence token")
	}
}

func TestLoadResponseBodyConfig_SingleObject(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "single.json")
	if err := os.WriteFile(cfgPath, []byte(`{"response_body":{"pressure":42}}`), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	body, err := loadResponseBodyConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	var decoded map[string]int
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if decoded["pressure"] != 42 {
		t.Fatalf("expected pressure=42, got %d", decoded["pressure"])
	}
}

func TestLoadResponseBodyConfig_TwoObjects(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "two.json")
	content := `{"response_body":[{"data":{"Percent":95}},{"data":{"Percent":30}}]}`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	body, err := loadResponseBodyConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(decoded))
	}
}

func TestResponseConfigStore_ReloadUpdatesPressureResponseBody(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "reload.json")
	if err := os.WriteFile(cfgPath, []byte(`{"response_body":{"pressure":20}}`), 0o600); err != nil {
		t.Fatalf("write config failed: %v", err)
	}

	store, err := newResponseConfigStore(cfgPath)
	if err != nil {
		t.Fatalf("new config store failed: %v", err)
	}
	handler := newPressureHandlerWithConfig("ok", 0, newPressureState(20, nil, false), store)

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(rec1, req1)
	var first map[string]int
	if err := json.Unmarshal(rec1.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode first body failed: %v", err)
	}
	if first["pressure"] != 20 {
		t.Fatalf("expected first pressure=20, got %v", first["pressure"])
	}

	if err := os.WriteFile(cfgPath, []byte(`{"response_body":[{"data":{"Percent":95}},{"data":{"Percent":20}},{"data":{"Percent":10}}]}`), 0o600); err != nil {
		t.Fatalf("rewrite config failed: %v", err)
	}
	if err := store.Reload(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/pressure", nil)
	handler(rec2, req2)
	var second []map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second body failed: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("expected 3 objects after reload, got %d", len(second))
	}
}

func TestGetenvAndGetenvInt(t *testing.T) {
	t.Setenv("E2E_ENV_STR", "value")
	if got := getenv("E2E_ENV_STR", "fallback"); got != "value" {
		t.Fatalf("unexpected getenv: %s", got)
	}
	if got := getenv("E2E_ENV_STR_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("unexpected fallback: %s", got)
	}

	t.Setenv("E2E_ENV_INT", "12")
	if got := getenvInt("E2E_ENV_INT", 1); got != 12 {
		t.Fatalf("unexpected getenvInt: %d", got)
	}
	t.Setenv("E2E_ENV_INT", "bad")
	if got := getenvInt("E2E_ENV_INT", 9); got != 9 {
		t.Fatalf("unexpected getenvInt fallback: %d", got)
	}
	os.Unsetenv("E2E_ENV_INT")
	if got := getenvInt("E2E_ENV_INT", 8); got != 8 {
		t.Fatalf("unexpected getenvInt fallback for missing key: %d", got)
	}
}

func TestAuthHandler_ValidCredentials(t *testing.T) {
	os.Setenv("USE_AUTH", "true")
	os.Setenv("AUTH_CLIENT_ID", "test-client")
	os.Setenv("AUTH_CLIENT_SECRET", "test-secret")
	defer os.Unsetenv("USE_AUTH")
	defer os.Unsetenv("AUTH_CLIENT_ID")
	defer os.Unsetenv("AUTH_CLIENT_SECRET")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader("client_id=test-client&client_secret=test-secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	handler := newMux()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp["token_type"] != "Bearer" {
		t.Fatalf("expected token_type Bearer, got %v", resp["token_type"])
	}
	if resp["expires_in"] != float64(3600) {
		t.Fatalf("expected expires_in 3600, got %v", resp["expires_in"])
	}
	if resp["access_token"] == "" {
		t.Fatalf("expected non-empty access_token")
	}
}

func TestAuthHandler_InvalidCredentials(t *testing.T) {
	os.Setenv("USE_AUTH", "true")
	os.Setenv("AUTH_CLIENT_ID", "test-client")
	os.Setenv("AUTH_CLIENT_SECRET", "test-secret")
	defer os.Unsetenv("USE_AUTH")
	defer os.Unsetenv("AUTH_CLIENT_ID")
	defer os.Unsetenv("AUTH_CLIENT_SECRET")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader("client_id=wrong&client_secret=secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	handler := newMux()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func TestDataHandler_WithValidToken(t *testing.T) {
	os.Setenv("USE_AUTH", "true")
	os.Setenv("AUTH_CLIENT_ID", "test-client")
	os.Setenv("AUTH_CLIENT_SECRET", "test-secret")
	os.Setenv("PRESSURE_VALUE_1", "85")
	os.Setenv("PRESSURE_VALUE_2", "72")
	defer os.Unsetenv("USE_AUTH")
	defer os.Unsetenv("AUTH_CLIENT_ID")
	defer os.Unsetenv("AUTH_CLIENT_SECRET")
	defer os.Unsetenv("PRESSURE_VALUE_1")
	defer os.Unsetenv("PRESSURE_VALUE_2")

	authRec := httptest.NewRecorder()
	authReq := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader("client_id=test-client&client_secret=test-secret"))
	authReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	handler := newMux()
	handler.ServeHTTP(authRec, authReq)

	var authResp map[string]any
	json.Unmarshal(authRec.Body.Bytes(), &authResp)
	token := authResp["access_token"].(string)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 data entries, got %d", len(resp))
	}
	percent1 := resp[0]["data"].(map[string]any)["Percent"].(float64)
	percent2 := resp[1]["data"].(map[string]any)["Percent"].(float64)
	if percent1 != 85 || percent2 != 72 {
		t.Fatalf("expected Percent 85 and 72, got %v and %v", percent1, percent2)
	}
}

func TestDataHandler_WithoutToken(t *testing.T) {
	os.Setenv("USE_AUTH", "true")
	defer os.Unsetenv("USE_AUTH")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/data", nil)

	handler := newMux()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}
