// Package enrich backfills missing event metadata by fetching the event page
// and reading its Open Graph / Twitter card tags. License-free, no API key.
package enrich

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/jorgenunes/eventscraper/internal/model"
)

// Known placeholder/default image URL patterns that we should treat as "no
// image" rather than store as a real cover.
var placeholderRe = regexp.MustCompile(`(?i)(default[_-]event|default[_-]images|/placeholder|no[_-]image|empty[_-]image)`)

// IsPlaceholderURL returns true for upstream URLs known to be generic site
// placeholders (e.g. Songkick's default-event.png).
func IsPlaceholderURL(u string) bool {
	if u == "" {
		return true
	}
	return placeholderRe.MatchString(u)
}

// Doer is the subset of *http.Client the enricher needs, so the shared stealth
// client can be injected for og:image fetches (they hit the same third-party
// event sites the scrapers do).
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type Enricher struct {
	HTTP       Doer
	Concurrent int
}

func New() *Enricher {
	return &Enricher{
		HTTP:       &http.Client{Timeout: 6 * time.Second},
		Concurrent: 6,
	}
}

// BackfillImages walks the input slice and populates ImageURL on any event
// missing one, by extracting og:image / twitter:image from the event URL.
// The events slice is mutated in place; safe to call with zero events.
func (e *Enricher) BackfillImages(ctx context.Context, events []model.Event) {
	if len(events) == 0 {
		return
	}
	conc := e.Concurrent
	if conc <= 0 {
		conc = 4
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i := range events {
		// Drop placeholder URLs that any scraper accidentally captured.
		if IsPlaceholderURL(events[i].ImageURL) {
			events[i].ImageURL = ""
		}
		if events[i].ImageURL != "" || events[i].URL == "" {
			continue
		}
		idx := i
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if img := e.fetchOGImage(ctx, events[idx].URL); img != "" {
				events[idx].ImageURL = img
			}
		}()
	}
	wg.Wait()
}

func (e *Enricher) fetchOGImage(ctx context.Context, url string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; eventscraper/1.0; +https://github.com/jorgenunes/eventscraper)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := e.HTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return ""
	}
	// Priority order: og:image, og:image:secure_url, twitter:image, twitter:image:src,
	// then the first <img> with a meaningful src.
	for _, sel := range []string{
		`meta[property="og:image"]`,
		`meta[property="og:image:secure_url"]`,
		`meta[name="twitter:image"]`,
		`meta[name="twitter:image:src"]`,
	} {
		if val, ok := doc.Find(sel).First().Attr("content"); ok {
			img := strings.TrimSpace(val)
			if img != "" && !IsPlaceholderURL(img) {
				return img
			}
		}
	}
	return ""
}
