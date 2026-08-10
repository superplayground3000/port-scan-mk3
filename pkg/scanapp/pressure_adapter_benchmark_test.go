package scanapp_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
	"github.com/xuxiping/port-scan-mk3/pkg/scanapp"
)

func BenchmarkPressureSampleSimple(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"pressure":42.5}`)
	}))
	b.Cleanup(server.Close)

	legacy := scanapp.NewSimplePressureFetcher(server.URL, server.Client())
	current, err := pressure.NewSimpleHTTP(server.URL, server.Client())
	if err != nil {
		b.Fatalf("NewSimpleHTTP() error = %v", err)
	}

	b.Run("legacy", func(b *testing.B) {
		for b.Loop() {
			if _, err := legacy.Fetch(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("current", func(b *testing.B) {
		for b.Loop() {
			if _, err := current.Sample(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkPressureSampleOAuthMulti(b *testing.B) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	b.Cleanup(authServer.Close)
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":42.5}}]`)
	}))
	b.Cleanup(dataServer.Close)
	endpoints := []string{dataServer.URL, dataServer.URL}

	legacy := scanapp.NewMultiSourcePressureFetcher(
		authServer.URL,
		endpoints,
		"client",
		"secret",
		authServer.Client(),
	)
	current, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: endpoints,
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		b.Fatalf("NewOAuthMulti() error = %v", err)
	}

	b.Run("legacy", func(b *testing.B) {
		for b.Loop() {
			if _, err := legacy.Fetch(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("current", func(b *testing.B) {
		for b.Loop() {
			if _, err := current.Sample(context.Background()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
