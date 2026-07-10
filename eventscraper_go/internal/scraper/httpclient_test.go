package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(retries int) *StealthClient {
	return NewStealthClient(StealthConfig{
		MaxRetries: retries,
		Sleep:      func(time.Duration) {}, // no real backoff in tests
	})
}

func TestStealthClientRetriesThenSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // 429 first
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(2)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 after retry", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hit %d times, want 2 (one retry)", got)
	}
}

func TestStealthClientGivesUpAfterMaxRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusForbidden) // always 403
	}))
	defer srv.Close()

	c := newTestClient(2)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 3 { // initial + 2 retries
		t.Errorf("server hit %d times, want 3 (initial + 2 retries)", got)
	}
}

func TestStealthClientInjectsBrowserHeaders(t *testing.T) {
	var ua, accept, secFetch string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		accept = r.Header.Get("Accept")
		secFetch = r.Header.Get("Sec-Fetch-Mode")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(0)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	// A caller-set semantic header must be preserved, not overwritten.
	req.Header.Set("Accept", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()

	if ua == "" || ua == "Go-http-client/1.1" {
		t.Errorf("User-Agent not rotated to a browser UA: %q", ua)
	}
	if accept != "application/json" {
		t.Errorf("caller Accept overwritten: %q", accept)
	}
	if secFetch == "" {
		t.Errorf("browser Sec-Fetch-Mode header not injected")
	}
}
