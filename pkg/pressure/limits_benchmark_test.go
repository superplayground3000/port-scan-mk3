package pressure

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func BenchmarkSimpleResponseOneMB(b *testing.B) {
	body := `{"pressure":42.5,"padding":"` + strings.Repeat("x", 999_966) + `"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	b.Cleanup(server.Close)
	source, err := NewSimpleHTTP(server.URL, server.Client())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		if _, err := source.Sample(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
