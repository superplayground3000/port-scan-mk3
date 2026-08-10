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

func TestSimpleHTTPReturnsNormalizedPressure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"pressure":85.16}`)
	}))
	defer server.Close()

	source, err := pressure.NewSimpleHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewSimpleHTTP() error = %v", err)
	}

	sample, err := source.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if sample.Maximum != 85.2 {
		t.Fatalf("Sample().Maximum = %.1f, want 85.2", sample.Maximum)
	}
	if len(sample.Sources) != 0 {
		t.Fatalf("Sample().Sources = %#v, want no source results", sample.Sources)
	}
}

func TestSimpleHTTPAcceptsNumericString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"pressure":"-5.04"}`)
	}))
	defer server.Close()

	source, err := pressure.NewSimpleHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewSimpleHTTP() error = %v", err)
	}
	sample, err := source.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if sample.Maximum != -5 {
		t.Fatalf("Sample().Maximum = %.1f, want -5.0", sample.Maximum)
	}
}

func TestSimpleHTTPReturnsResponseErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{name: "server status", statusCode: http.StatusInternalServerError, body: `failed`, want: "status=500"},
		{name: "invalid JSON", statusCode: http.StatusOK, body: `{`, want: "decode pressure response"},
		{name: "missing field", statusCode: http.StatusOK, body: `{"load":50}`, want: "pressure field missing"},
		{name: "invalid type", statusCode: http.StatusOK, body: `{"pressure":true}`, want: "unsupported pressure field type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()

			source, err := pressure.NewSimpleHTTP(server.URL, server.Client())
			if err != nil {
				t.Fatalf("NewSimpleHTTP() error = %v", err)
			}
			_, err = source.Sample(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Sample() error = %v, want text %q", err, test.want)
			}
		})
	}
}
