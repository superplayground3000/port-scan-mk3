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

func TestSimpleHTTPRejectsNonFiniteNumericStrings(t *testing.T) {
	tests := []struct {
		value    string
		wantKind string
	}{
		{value: "NaN", wantKind: "NaN"},
		{value: "nan", wantKind: "NaN"},
		{value: "+Inf", wantKind: "positive infinity"},
		{value: "-inf", wantKind: "negative infinity"},
		{value: "Infinity", wantKind: "positive infinity"},
		{value: "+infinity", wantKind: "positive infinity"},
		{value: "-Infinity", wantKind: "negative infinity"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"pressure":%q}`, tt.value)
			}))
			defer server.Close()

			source, err := pressure.NewSimpleHTTP(server.URL, server.Client())
			if err != nil {
				t.Fatalf("NewSimpleHTTP() error = %v", err)
			}
			sample, err := source.Sample(context.Background())
			if err == nil {
				t.Fatalf("Sample() = %#v, want a non-finite pressure error", sample)
			}
			for _, detail := range []string{"pressure", tt.wantKind} {
				if !strings.Contains(err.Error(), detail) {
					t.Errorf("Sample() error = %q, want detail %q", err, detail)
				}
			}
		})
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
