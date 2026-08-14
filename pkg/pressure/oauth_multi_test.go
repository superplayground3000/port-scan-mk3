package pressure_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xuxiping/port-scan-mk3/pkg/pressure"
)

func TestOAuthMultiReturnsOrderedPartialResultsAndAggregateError(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()

	releaseFirst := make(chan struct{})
	defer func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
	}()
	secondFinished := make(chan struct{})
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-releaseFirst:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":42.5}}]`)
	}))
	defer firstServer.Close()

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(secondFinished)
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer secondServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{firstServer.URL, secondServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	type result struct {
		sample pressure.Sample
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		sample, sampleErr := source.Sample(context.Background())
		resultCh <- result{sample: sample, err: sampleErr}
	}()

	select {
	case <-secondFinished:
		close(releaseFirst)
	case <-time.After(time.Second):
		t.Fatal("the second source did not finish")
	}

	var got result
	select {
	case got = <-resultCh:
	case <-time.After(time.Second):
		t.Fatal("Sample() did not return")
	}

	if got.err == nil || !strings.Contains(got.err.Error(), "src2") {
		t.Fatalf("Sample() error = %v, want aggregate src2 error", got.err)
	}
	if got.sample.Maximum != 0 {
		t.Fatalf("Sample().Maximum = %.1f, want 0 after a source error", got.sample.Maximum)
	}
	if len(got.sample.Sources) != 2 {
		t.Fatalf("Sample().Sources = %#v, want two results", got.sample.Sources)
	}
	if first := got.sample.Sources[0]; first.Name != "src1" || first.Pressure != 42.5 || first.Err != nil {
		t.Fatalf("Sample().Sources[0] = %#v, want successful src1", first)
	}
	if second := got.sample.Sources[1]; second.Name != "src2" || second.Err == nil {
		t.Fatalf("Sample().Sources[1] = %#v, want failed src2", second)
	}
}

func TestOAuthMultiReturnsMaximumPressure(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()

	newDataServer := func(value string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `[{"data":{"Percent":%s}}]`, value)
		}))
	}
	firstServer := newDataServer("-5.0")
	defer firstServer.Close()
	secondServer := newDataServer("72.26")
	defer secondServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{firstServer.URL, secondServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	sample, err := source.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if sample.Maximum != 72.3 {
		t.Fatalf("Sample().Maximum = %.1f, want 72.3", sample.Maximum)
	}
}

func TestOAuthMultiRejectsNonFinitePercentWithinOneSource(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":42},"private":"do-not-report-complete-response"},{"data":{"Percent":"NaN"}},{"data":{"Percent":99}}]`)
	}))
	defer dataServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{dataServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	sample, err := source.Sample(context.Background())
	if err == nil {
		t.Fatalf("Sample() = %#v, want a non-finite Percent error", sample)
	}
	for _, detail := range []string{"src1", "Percent", "NaN"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("Sample() error = %q, want detail %q", err, detail)
		}
	}
	if strings.Contains(err.Error(), "do-not-report-complete-response") {
		t.Errorf("Sample() error exposes the complete response: %q", err)
	}
	if sample.Maximum != 0 {
		t.Errorf("Sample().Maximum = %v, want zero after source failure", sample.Maximum)
	}
	if len(sample.Sources) != 1 || sample.Sources[0].Err == nil {
		t.Errorf("Sample().Sources = %#v, want a failed source result", sample.Sources)
	}
}

func TestOAuthMultiRetainsFiniteSourceWhenAnotherSourceIsNonFinite(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()
	finiteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":55}}]`)
	}))
	defer finiteServer.Close()
	nonFiniteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":10}},{"data":{"Percent":"+Inf"}}]`)
	}))
	defer nonFiniteServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{finiteServer.URL, nonFiniteServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	sample, err := source.Sample(context.Background())
	if err == nil {
		t.Fatalf("Sample() = %#v, want a failed complete poll", sample)
	}
	for _, detail := range []string{"src2", "Percent", "positive infinity"} {
		if !strings.Contains(err.Error(), detail) {
			t.Errorf("Sample() error = %q, want detail %q", err, detail)
		}
	}
	if sample.Maximum != 0 {
		t.Errorf("Sample().Maximum = %v, want zero after source failure", sample.Maximum)
	}
	if len(sample.Sources) != 2 {
		t.Fatalf("Sample().Sources = %#v, want two source results", sample.Sources)
	}
	if first := sample.Sources[0]; first.Name != "src1" || first.Pressure != 55 || first.Err != nil {
		t.Errorf("Sample().Sources[0] = %#v, want retained finite source", first)
	}
	if second := sample.Sources[1]; second.Name != "src2" || second.Err == nil {
		t.Errorf("Sample().Sources[1] = %#v, want non-finite source failure", second)
	}
}

func TestOAuthMultiReturnsIndependentResultSlices(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":30}}]`)
	}))
	defer dataServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{dataServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	first, err := source.Sample(context.Background())
	if err != nil {
		t.Fatalf("first Sample() error = %v", err)
	}
	second, err := source.Sample(context.Background())
	if err != nil {
		t.Fatalf("second Sample() error = %v", err)
	}

	first.Sources[0].Name = "changed"
	first.Sources[0].Pressure = 99
	if second.Sources[0].Name != "src1" || second.Sources[0].Pressure != 30 {
		t.Fatalf("second Sample().Sources changed through the first result: %#v", second.Sources)
	}
}

func TestNewOAuthMultiDoesNotRetainDataEndpointsSlice(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":30}}]`)
	}))
	defer dataServer.Close()

	endpoints := []string{dataServer.URL}
	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: endpoints,
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}
	endpoints[0] = "https://invalid.example"

	sample, err := source.Sample(context.Background())
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if sample.Maximum != 30 {
		t.Fatalf("Sample().Maximum = %.1f, want 30", sample.Maximum)
	}
}

func TestOAuthMultiWrapsFirstFailedSourceInConfigurationOrder(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()

	releaseFirst := make(chan struct{})
	defer func() {
		select {
		case <-releaseFirst:
		default:
			close(releaseFirst)
		}
	}()
	secondFinished := make(chan struct{})
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-releaseFirst:
		case <-r.Context().Done():
			return
		}
		http.Error(w, "first failed", http.StatusBadGateway)
	}))
	defer firstServer.Close()
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(secondFinished)
		http.Error(w, "second failed", http.StatusServiceUnavailable)
	}))
	defer secondServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{firstServer.URL, secondServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		_, sampleErr := source.Sample(context.Background())
		errCh <- sampleErr
	}()
	select {
	case <-secondFinished:
		close(releaseFirst)
	case <-time.After(time.Second):
		t.Fatal("the second source did not finish")
	}

	select {
	case err = <-errCh:
	case <-time.After(time.Second):
		t.Fatal("Sample() did not return")
	}
	if err == nil || !strings.Contains(err.Error(), "src1") || !strings.Contains(err.Error(), "status=502") {
		t.Fatalf("Sample() error = %v, want first configured source error", err)
	}
}

func TestOAuthMultiKeepsOneTokenCachePerEndpoint(t *testing.T) {
	var authCalls atomic.Int32
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" {
			http.Error(w, "bad credentials", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"bearer","expires_in":3600}`)
	}))
	defer authServer.Close()
	dataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `[{"data":{"Percent":30}}]`)
	}))
	defer dataServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{dataServer.URL, dataServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}
	for range 2 {
		if _, err := source.Sample(context.Background()); err != nil {
			t.Fatalf("Sample() error = %v", err)
		}
	}
	if got := authCalls.Load(); got != 2 {
		t.Fatalf("OAuth request count = %d, want one request for each endpoint", got)
	}
}

func TestOAuthMultiStopsActiveRequestOnContextCancellation(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer authServer.Close()
	requestStarted := make(chan struct{})
	dataServer := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer dataServer.Close()

	source, err := pressure.NewOAuthMulti(pressure.OAuthConfig{
		AuthEndpoint:  authServer.URL,
		DataEndpoints: []string{dataServer.URL},
		ClientID:      "client",
		ClientSecret:  "secret",
	}, authServer.Client())
	if err != nil {
		t.Fatalf("NewOAuthMulti() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, sampleErr := source.Sample(ctx)
		errCh <- sampleErr
	}()
	select {
	case <-requestStarted:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("the pressure request did not start")
	}

	select {
	case err = <-errCh:
	case <-time.After(time.Second):
		t.Fatal("Sample() did not stop after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Sample() error = %v, want context.Canceled", err)
	}
}
