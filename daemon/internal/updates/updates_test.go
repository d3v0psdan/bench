package updates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestReadsTheReleaseAndTreats404AsNone(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/none" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://github.com/d3v0psdan/bench/releases/tag/v0.2.0","published_at":"2026-10-01T00:00:00Z"}`))
	}))
	defer gh.Close()
	rel, err := Latest(context.Background(), gh.Client(), gh.URL+"/latest")
	if err != nil || rel.Version != "0.2.0" || rel.URL == "" {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	if _, err := Latest(context.Background(), gh.Client(), gh.URL+"/none"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("404: %v, want ErrNoRelease", err)
	}
}
