package enrich

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

func TestIsPlaceholderURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", true},
		{"https://example.com/default-event.png", true},
		{"https://cdn.songkick.com/default_event.jpg", true},
		{"https://x.com/default_images/cover.jpg", true},
		{"https://x.com/some/placeholder.png", true},
		{"https://x.com/no-image.png", true},
		{"https://x.com/empty_image.png", true},
		{"https://x.com/real-cover.png", false},
		{"https://x.com/event/123.jpg", false},
	}
	for _, c := range cases {
		if got := IsPlaceholderURL(c.in); got != c.want {
			t.Errorf("IsPlaceholderURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBackfillImagesSkipsAlreadySet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected fetch for event with imageUrl set: %s", r.URL)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	e := New()
	events := []model.Event{
		{URL: srv.URL, ImageURL: "https://existing/img.png"},
	}
	e.BackfillImages(context.Background(), events)
	if events[0].ImageURL != "https://existing/img.png" {
		t.Errorf("ImageURL changed unexpectedly: %q", events[0].ImageURL)
	}
}

func TestBackfillImagesStripsPlaceholderThenFetches(t *testing.T) {
	const html = `<html><head>
<meta property="og:image" content="https://cdn/real.jpg">
</head></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	e := New()
	events := []model.Event{
		// placeholder gets stripped, then og:image filled in
		{URL: srv.URL, ImageURL: "https://x/default-event.png"},
	}
	e.BackfillImages(context.Background(), events)
	if events[0].ImageURL != "https://cdn/real.jpg" {
		t.Errorf("ImageURL = %q, want fetched og:image", events[0].ImageURL)
	}
}

func TestBackfillImagesTwitterFallback(t *testing.T) {
	const html = `<html><head>
<meta name="twitter:image" content="https://cdn/twit.jpg">
</head></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	e := New()
	events := []model.Event{{URL: srv.URL}}
	e.BackfillImages(context.Background(), events)
	if events[0].ImageURL != "https://cdn/twit.jpg" {
		t.Errorf("ImageURL = %q, want twitter:image fallback", events[0].ImageURL)
	}
}

func TestBackfillImagesIgnoresPlaceholderOgImage(t *testing.T) {
	const html = `<html><head>
<meta property="og:image" content="https://cdn/default-event.png">
</head></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	e := New()
	events := []model.Event{{URL: srv.URL}}
	e.BackfillImages(context.Background(), events)
	if events[0].ImageURL != "" {
		t.Errorf("ImageURL = %q, want empty (placeholder ignored)", events[0].ImageURL)
	}
}

func TestBackfillImagesEmptySliceAndMissingURL(t *testing.T) {
	e := New()
	// Empty slice — must not panic and must return promptly.
	done := make(chan struct{})
	go func() {
		e.BackfillImages(context.Background(), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("BackfillImages(nil) hung")
	}

	// Event with no URL is left untouched (no fetch attempted).
	events := []model.Event{{}}
	e.BackfillImages(context.Background(), events)
	if events[0].ImageURL != "" {
		t.Errorf("ImageURL = %q, want empty", events[0].ImageURL)
	}
}
