package scraper

import (
	"sync"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

func TestParseEBDate(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want time.Time
	}{
		{"2026-06-10T18:30:00Z", true, time.Date(2026, 6, 10, 18, 30, 0, 0, time.UTC)},
		{"2026-06-10T18:30:00", true, time.Date(2026, 6, 10, 18, 30, 0, 0, time.UTC)},
		{"2026-06-10", true, time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)},
		{"", false, time.Time{}},
		{"not-a-date", false, time.Time{}},
	}
	for _, c := range cases {
		got, ok := parseEBDate(c.in)
		if ok != c.ok {
			t.Errorf("parseEBDate(%q) ok = %v, want %v", c.in, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("parseEBDate(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestToFloat(t *testing.T) {
	if got := toFloat(3.14); got != 3.14 {
		t.Errorf("toFloat(float64) = %v", got)
	}
	if got := toFloat("2.5"); got != 2.5 {
		t.Errorf("toFloat(string) = %v", got)
	}
	if got := toFloat(nil); got != 0 {
		t.Errorf("toFloat(nil) = %v, want 0", got)
	}
	if got := toFloat("nope"); got != 0 {
		t.Errorf("toFloat(invalid) = %v, want 0", got)
	}
	if got := toFloat(7); got != 0 {
		// int is intentionally unsupported by the helper
		t.Errorf("toFloat(int) = %v, want 0 (not handled)", got)
	}
}

func TestBuildEventbriteEventMinimumFields(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT"}

	// Missing required fields → not built.
	if _, ok := buildEventbriteEvent(ebItem{}, city, model.CategoryMusic); ok {
		t.Error("empty item must not build")
	}
	if _, ok := buildEventbriteEvent(ebItem{Name: "x"}, city, model.CategoryMusic); ok {
		t.Error("missing url/startDate must not build")
	}
	if _, ok := buildEventbriteEvent(ebItem{Name: "x", URL: "u", StartDate: "bad"}, city, model.CategoryMusic); ok {
		t.Error("unparseable date must not build")
	}
}

func TestBuildEventbriteEventFull(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT"}
	it := ebItem{
		Name:      "Jazz Night",
		URL:       "https://www.eventbrite.com/e/jazz-night-tickets-12345",
		StartDate: "2026-06-10T20:00:00Z",
		EndDate:   "2026-06-10T23:00:00Z",
		Image:     "https://img/cover.jpg",
	}
	it.Location.Name = "Hot Clube"
	it.Location.Address.StreetAddress = "Praça da Alegria"
	it.Location.Address.AddressLocality = "Lisbon"
	it.Location.Address.AddressCountry = "PT"
	it.Location.Geo.Latitude = 38.7
	it.Location.Geo.Longitude = -9.14

	ev, ok := buildEventbriteEvent(it, city, model.CategoryMusic)
	if !ok {
		t.Fatal("buildEventbriteEvent returned !ok")
	}
	if ev.Source != model.SourceEventbrite {
		t.Errorf("Source = %v", ev.Source)
	}
	if ev.SourceID != "12345" {
		t.Errorf("SourceID = %q, want 12345 (trailing -N segment)", ev.SourceID)
	}
	if ev.Title != "Jazz Night" || ev.City != "Lisbon" || ev.Country != "PT" {
		t.Errorf("ev = %+v", ev)
	}
	if ev.EndsAt == nil || !ev.EndsAt.Equal(time.Date(2026, 6, 10, 23, 0, 0, 0, time.UTC)) {
		t.Errorf("EndsAt = %v", ev.EndsAt)
	}
	if ev.Venue.Lat != 38.7 || ev.Venue.Lon != -9.14 {
		t.Errorf("venue geo = %v/%v", ev.Venue.Lat, ev.Venue.Lon)
	}
	if ev.ImageURL != "https://img/cover.jpg" {
		t.Errorf("ImageURL = %q", ev.ImageURL)
	}
	if ev.ID != model.MakeID(model.SourceEventbrite, "12345") {
		t.Errorf("ID = %q", ev.ID)
	}
}

func TestBuildEventbriteEventImageArrayAndFallbackCountry(t *testing.T) {
	city := geo.City{Name: "Berlin", Country: "DE"}
	it := ebItem{
		Name:      "Tech Talk",
		URL:       "https://www.eventbrite.com/e/tech-99",
		StartDate: "2026-06-10",
		Image:     []any{"https://img/a.jpg", "https://img/b.jpg"},
	}
	// no AddressCountry → should fall back to city.Country.
	ev, ok := buildEventbriteEvent(it, city, model.CategoryTech)
	if !ok {
		t.Fatal("not built")
	}
	if ev.ImageURL != "https://img/a.jpg" {
		t.Errorf("ImageURL = %q, want first array element", ev.ImageURL)
	}
	if ev.Country != "DE" {
		t.Errorf("Country = %q, want fallback DE", ev.Country)
	}
}

func TestParseEventbritePageNoServerData(t *testing.T) {
	var mu sync.Mutex
	var out []model.Event
	parseEventbritePage([]byte("<html>no server data here</html>"),
		geo.City{Name: "X"}, model.CategoryTech, &mu, &out)
	if len(out) != 0 {
		t.Errorf("got %d events from page with no SERVER_DATA, want 0", len(out))
	}
}

func TestParseEventbritePageBuildsEvents(t *testing.T) {
	// The regex used to extract __SERVER_DATA__ matches up to the first `;` on
	// the same line, so the JSON blob must be on one line.
	body := []byte(`<html><script>window.__SERVER_DATA__ = {"jsonld":[{"@type":"ItemList","itemListElement":[{"item":{"@type":"Event","name":"Jazz Night","url":"https://www.eventbrite.com/e/jazz-12345","startDate":"2026-06-10T20:00:00Z","image":"https://img/x.jpg"}},{"item":{"@type":"Event","name":"","url":"https://www.eventbrite.com/e/empty-1","startDate":"2026-06-10T20:00:00Z"}}]}]};</script></html>`)

	var mu sync.Mutex
	var out []model.Event
	parseEventbritePage(body, geo.City{Name: "Lisbon", Country: "PT"}, model.CategoryMusic, &mu, &out)
	if len(out) != 1 {
		t.Fatalf("got %d events, want 1 (invalid item filtered)", len(out))
	}
	if out[0].Title != "Jazz Night" || out[0].SourceID != "12345" {
		t.Errorf("event = %+v", out[0])
	}
}
