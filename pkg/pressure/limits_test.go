package pressure_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

func TestSimpleHTTPEnforcesContentLengthAndStreamLimit(t *testing.T) {
	t.Parallel()

	for _, chunked := range []bool{false, true} {
		chunked := chunked
		t.Run(fmt.Sprintf("chunked=%t", chunked), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if chunked {
					w.Header().Set("Transfer-Encoding", "chunked")
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}
				_, _ = fmt.Fprint(w, `{"pressure":85.1}`)
			}))
			defer server.Close()

			source, err := pressure.NewSimpleHTTPWithLimits(server.URL, server.Client(), pressure.ResponseLimits{MaxBytes: 8})
			if err != nil {
				t.Fatal(err)
			}
			_, err = source.Sample(context.Background())
			if err == nil || !strings.Contains(err.Error(), server.URL) || !strings.Contains(err.Error(), "simple response") || !strings.Contains(err.Error(), "-pressure-response-size-limit-mb") {
				t.Fatalf("Sample() error = %v, want endpoint, type, and override flag", err)
			}
		})
	}
}

func TestOAuthHTTPEnforcesTokenBytesAndIncrementalDataEntries(t *testing.T) {
	t.Parallel()

	var tokenOversized bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if tokenOversized {
				_, _ = fmt.Fprint(w, `{"access_token":"too-large","token_type":"Bearer","expires_in":3600}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"access_token":"x","token_type":"Bearer","expires_in":0}`)
		case "/data":
			_, _ = fmt.Fprint(w, `[{"data":{"Percent":1}},{"data":{"Percent":2}}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := pressure.OAuthConfig{AuthEndpoint: server.URL + "/token", DataEndpoints: []string{server.URL + "/data"}, ClientID: "id", ClientSecret: "secret"}
	tokenOversized = true
	source, err := pressure.NewOAuthMultiWithLimits(cfg, server.Client(), pressure.ResponseLimits{MaxBytes: 20, MaxEntries: 0})
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Sample(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/token") || !strings.Contains(err.Error(), "OAuth token response") {
		t.Fatalf("token error = %v, want endpoint and response type", err)
	}

	tokenOversized = false
	source, err = pressure.NewOAuthMultiWithLimits(cfg, server.Client(), pressure.ResponseLimits{MaxBytes: 1_000, MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Sample(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/data") || !strings.Contains(err.Error(), "count 2") || !strings.Contains(err.Error(), "-pressure-response-entry-limit") {
		t.Fatalf("entry error = %v, want endpoint, count, and override flag", err)
	}
}
