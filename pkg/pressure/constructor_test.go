package pressure_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

func TestNewSimpleHTTPRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		client   *http.Client
	}{
		{name: "empty endpoint", client: http.DefaultClient},
		{name: "relative endpoint", endpoint: "/pressure", client: http.DefaultClient},
		{name: "nil client", endpoint: "https://example.com/pressure"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pressure.NewSimpleHTTP(test.endpoint, test.client); err == nil {
				t.Fatal("NewSimpleHTTP() error = nil, want configuration error")
			}
		})
	}
}

func TestNewSimpleHTTPWrapsURLParseError(t *testing.T) {
	_, err := pressure.NewSimpleHTTP("https://example.com/%zz", http.DefaultClient)
	var escapeError url.EscapeError
	if !errors.As(err, &escapeError) {
		t.Fatalf("NewSimpleHTTP() error = %v, want wrapped url.EscapeError", err)
	}
}

func TestNewOAuthMultiRejectsInvalidConfiguration(t *testing.T) {
	valid := pressure.OAuthConfig{
		AuthEndpoint:  "https://example.com/token",
		DataEndpoints: []string{"https://example.com/pressure"},
		ClientID:      "client",
		ClientSecret:  "secret",
	}

	for _, test := range []struct {
		name   string
		change func(*pressure.OAuthConfig)
		client *http.Client
	}{
		{name: "empty auth endpoint", change: func(cfg *pressure.OAuthConfig) { cfg.AuthEndpoint = "" }, client: http.DefaultClient},
		{name: "relative auth endpoint", change: func(cfg *pressure.OAuthConfig) { cfg.AuthEndpoint = "/token" }, client: http.DefaultClient},
		{name: "empty data endpoints", change: func(cfg *pressure.OAuthConfig) { cfg.DataEndpoints = nil }, client: http.DefaultClient},
		{name: "relative data endpoint", change: func(cfg *pressure.OAuthConfig) { cfg.DataEndpoints[0] = "/pressure" }, client: http.DefaultClient},
		{name: "empty client ID", change: func(cfg *pressure.OAuthConfig) { cfg.ClientID = "" }, client: http.DefaultClient},
		{name: "empty client secret", change: func(cfg *pressure.OAuthConfig) { cfg.ClientSecret = "" }, client: http.DefaultClient},
		{name: "nil client", change: func(*pressure.OAuthConfig) {}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			cfg.DataEndpoints = append([]string(nil), valid.DataEndpoints...)
			test.change(&cfg)
			if _, err := pressure.NewOAuthMulti(cfg, test.client); err == nil {
				t.Fatal("NewOAuthMulti() error = nil, want configuration error")
			}
		})
	}
}
