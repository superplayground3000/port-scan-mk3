package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeCounterHandler_CountsConnectionsAndResets(t *testing.T) {
	counter := &probeCounter{}
	counter.add()
	counter.add()
	server := httptest.NewServer(probeCounterHandler(counter))
	defer server.Close()

	if got := getCounterResponse(t, server.URL+"/count"); got != "2\n" {
		t.Fatalf("count response = %q, want 2", got)
	}
	if got := getCounterResponse(t, server.URL+"/reset"); got != "0\n" {
		t.Fatalf("reset response = %q, want 0", got)
	}
	if got := getCounterResponse(t, server.URL+"/count"); got != "0\n" {
		t.Fatalf("count after reset = %q, want 0", got)
	}
}

func getCounterResponse(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
