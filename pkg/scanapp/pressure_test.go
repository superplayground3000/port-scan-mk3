package scanapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if pressure != 85.0 {
		t.Errorf("expected 85.0, got %.1f", pressure)
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

func TestSimplePressureFetcher_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	fetcher := NewSimplePressureFetcher(srv.URL, srv.Client())
	ctx := context.Background()

	_, err := fetcher.Fetch(ctx)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status error with 500, got: %v", err)
	}
}

func TestSimplePressureFetcher_StringValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"pressure": "85"}`))
	}))
	defer srv.Close()

	fetcher := NewSimplePressureFetcher(srv.URL, srv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pressure != 85.0 {
		t.Errorf("expected 85.0, got %.1f", pressure)
	}
}

func TestSimplePressureFetcher_FloatValue_UsesOneDecimalPrecision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"pressure": 85.16}`))
	}))
	defer srv.Close()

	fetcher := NewSimplePressureFetcher(srv.URL, srv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := pressure, 85.2; got != want {
		t.Fatalf("expected pressure %.1f, got %.1f", want, got)
	}
}

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
	if got, want := pressure, 42.5; got != want {
		t.Errorf("expected %.1f, got %.1f", want, got)
	}
	if authCalls != 1 {
		t.Errorf("expected 1 auth call, got %d", authCalls)
	}
}

func TestAuthenticatedPressureFetcher_PercentRoundsToOneDecimal(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-round","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-round" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":42.56}}]`))
	}))
	defer dataSrv.Close()

	fetcher := NewAuthenticatedPressureFetcher(authSrv.URL, dataSrv.URL, "test-client", "test-secret", dataSrv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := pressure, 42.6; got != want {
		t.Fatalf("expected rounded pressure %.1f, got %.1f", want, got)
	}
}

func TestAuthenticatedPressureFetcher_AcceptsLowercaseBearerAndCachesToken(t *testing.T) {
	authCalls := 0
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-lower","token_type":"bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-lower" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":33.9}}]`))
	}))
	defer dataSrv.Close()

	fetcher := NewAuthenticatedPressureFetcher(authSrv.URL, dataSrv.URL, "test-client", "test-secret", dataSrv.Client())
	ctx := context.Background()

	first, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("first fetch unexpected error: %v", err)
	}
	second, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("second fetch unexpected error: %v", err)
	}

	if first != 33.9 || second != 33.9 {
		t.Fatalf("expected both fetches to return 33.9, got first=%.1f second=%.1f", first, second)
	}
	if authCalls != 1 {
		t.Fatalf("expected token cache to avoid extra auth calls, got %d", authCalls)
	}
}

func TestAuthenticatedPressureFetcher_ZeroPercentIsValid(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-zero","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-zero" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":0.0}}]`))
	}))
	defer dataSrv.Close()

	fetcher := NewAuthenticatedPressureFetcher(authSrv.URL, dataSrv.URL, "test-client", "test-secret", dataSrv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("unexpected error for zero percent: %v", err)
	}
	if pressure != 0.0 {
		t.Fatalf("expected zero pressure, got %.1f", pressure)
	}
}

func TestMultiSourcePressureFetcher_AllSourcesSucceed_ReturnsMax(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-multi","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":30.0}}]`))
	}))
	defer dataSrv1.Close()

	dataSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":70.0}}]`))
	}))
	defer dataSrv2.Close()

	fetcher := NewMultiSourcePressureFetcher(authSrv.URL, []string{dataSrv1.URL, dataSrv2.URL}, "test-client", "test-secret", authSrv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pressure != 70.0 {
		t.Errorf("expected 70.0, got %.1f", pressure)
	}
}

func TestMultiSourcePressureFetcher_OneSourceErrors_ReturnsFetchError(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-err","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":50.0}}]`))
	}))
	defer dataSrv1.Close()

	dataSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer dataSrv2.Close()

	fetcher := NewMultiSourcePressureFetcher(authSrv.URL, []string{dataSrv1.URL, dataSrv2.URL}, "test-client", "test-secret", authSrv.Client())
	ctx := context.Background()

	_, err := fetcher.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMultiSourcePressureFetcher_OneSourceReturnsEmptyBody_ReturnsFetchError(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-empty","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":40.0}}]`))
	}))
	defer dataSrv1.Close()

	dataSrv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer dataSrv2.Close()

	fetcher := NewMultiSourcePressureFetcher(authSrv.URL, []string{dataSrv1.URL, dataSrv2.URL}, "test-client", "test-secret", authSrv.Client())
	ctx := context.Background()

	_, err := fetcher.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
}

func TestMultiSourcePressureFetcher_EmptySources_ReturnsError(t *testing.T) {
	fetcher := NewMultiSourcePressureFetcher("http://auth", []string{}, "id", "secret", nil)
	ctx := context.Background()
	_, err := fetcher.Fetch(ctx)
	if err == nil {
		t.Fatal("expected error for empty sources, got nil")
	}
}

func TestMultiSourcePressureFetcher_SingleSource_ReturnsPressure(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok-single","token_type":"Bearer","expires_in":3600}`))
	}))
	defer authSrv.Close()

	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"data":{"Percent":55.5}}]`))
	}))
	defer dataSrv.Close()

	fetcher := NewMultiSourcePressureFetcher(authSrv.URL, []string{dataSrv.URL}, "test-client", "test-secret", authSrv.Client())
	ctx := context.Background()

	pressure, err := fetcher.Fetch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pressure != 55.5 {
		t.Errorf("expected 55.5, got %.1f", pressure)
	}
}
