package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

const citiesYAML = `cities:
  - id: lisbon
    name: Lisbon
    country: PT
    lat: 38.7
    lon: -9.14
    eventbrite_slug: portugal--lisbon
  - id: porto
    name: Porto
    country: PT
    lat: 41.15
    lon: -8.61
`

func newCatalog(t *testing.T) *geo.Catalog {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(citiesYAML), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cat, err := geo.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cat
}

func TestParseQueryDefaults(t *testing.T) {
	cat := newCatalog(t)
	r := httptest.NewRequest("GET", "/events", nil)
	q, city, err := parseQuery(r, cat)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if city != nil {
		t.Errorf("city should be nil when not specified")
	}
	if q.Limit != 100 {
		t.Errorf("default Limit = %d, want 100", q.Limit)
	}
	if q.Offset != 0 {
		t.Errorf("default Offset = %d, want 0", q.Offset)
	}
	if !q.RequireImage {
		t.Errorf("default RequireImage should be true")
	}
	if q.From.IsZero() {
		t.Errorf("default From should be set to today UTC")
	}
	// From defaults to UTC start-of-day, so hour/min/sec should all be zero.
	if q.From.Hour() != 0 || q.From.Minute() != 0 || q.From.Second() != 0 {
		t.Errorf("default From not midnight UTC: %v", q.From)
	}
}

func TestParseQueryFilters(t *testing.T) {
	cat := newCatalog(t)
	r := httptest.NewRequest("GET",
		"/events?city=lisbon&category=music&source=luma&q=jazz&limit=20&offset=10&include_no_image=true&from=2026-06-01&to=2026-06-30", nil)
	q, city, err := parseQuery(r, cat)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if city == nil || city.ID != "lisbon" {
		t.Errorf("city = %+v", city)
	}
	if q.City != "Lisbon" {
		t.Errorf("q.City = %q, want Lisbon (resolved name)", q.City)
	}
	if q.Category != model.CategoryMusic {
		t.Errorf("Category = %v", q.Category)
	}
	if q.Source != model.SourceLuma {
		t.Errorf("Source = %v", q.Source)
	}
	if q.Search != "jazz" {
		t.Errorf("Search = %q", q.Search)
	}
	if q.Limit != 20 || q.Offset != 10 {
		t.Errorf("limit/offset = %d/%d", q.Limit, q.Offset)
	}
	if q.RequireImage {
		t.Errorf("include_no_image=true should disable RequireImage")
	}
	if !q.From.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("From = %v", q.From)
	}
	// `to` is rolled forward by 24h so the day is inclusive.
	if !q.To.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("To = %v, want 2026-07-01 UTC", q.To)
	}
}

func TestParseQueryLimitClamp(t *testing.T) {
	cat := newCatalog(t)
	r := httptest.NewRequest("GET", "/events?limit=99999&offset=-5", nil)
	q, _, err := parseQuery(r, cat)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if q.Limit != 2000 {
		t.Errorf("Limit = %d, want clamped to 2000", q.Limit)
	}
	if q.Offset != 0 {
		t.Errorf("Offset = %d, want clamped to 0", q.Offset)
	}
}

func TestParseQueryErrors(t *testing.T) {
	cat := newCatalog(t)
	cases := []string{
		"/events?city=nowhere",
		"/events?category=food",
		"/events?source=twitter",
		"/events?from=oops",
		"/events?to=oops",
	}
	for _, p := range cases {
		r := httptest.NewRequest("GET", p, nil)
		if _, _, err := parseQuery(r, cat); err == nil {
			t.Errorf("%s: expected error", p)
		}
	}
}
