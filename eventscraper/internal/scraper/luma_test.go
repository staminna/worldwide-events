package scraper

import (
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

func TestLumaToEventValid(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT", Lat: 38.7223, Lon: -9.1393}
	entry := lumaEntry{
		Event: lumaEvent{
			APIID:   "evt_123",
			Name:    "AI Lisbon Meetup",
			URL:     "ai-meetup",
			StartAt: "2026-06-10T18:00:00Z",
			EndAt:   "2026-06-10T20:00:00Z",
			Geo: lumaGeo{
				City:        "Lisboa",
				CountryCode: "PT",
				Address:     "Rua A, Lisboa",
			},
		},
	}
	entry.Event.Coord.Latitude = 38.7
	entry.Event.Coord.Longitude = -9.14

	ev, ok := lumaToEvent(entry, city, model.CategoryTech)
	if !ok {
		t.Fatal("lumaToEvent !ok")
	}
	if ev.Source != model.SourceLuma || ev.SourceID != "evt_123" {
		t.Errorf("source/id = %v/%q", ev.Source, ev.SourceID)
	}
	if ev.ID != model.MakeID(model.SourceLuma, "evt_123") {
		t.Errorf("ID hash mismatch")
	}
	if ev.URL != "https://lu.ma/ai-meetup" {
		t.Errorf("URL = %q, want https://lu.ma/ai-meetup (relative slug rewritten)", ev.URL)
	}
	if !ev.StartsAt.Equal(time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC)) {
		t.Errorf("StartsAt = %v", ev.StartsAt)
	}
	if ev.EndsAt == nil || !ev.EndsAt.Equal(time.Date(2026, 6, 10, 20, 0, 0, 0, time.UTC)) {
		t.Errorf("EndsAt = %v", ev.EndsAt)
	}
	if ev.City != "Lisboa" || ev.Country != "PT" {
		t.Errorf("city/country = %q/%q (should prefer geo)", ev.City, ev.Country)
	}
	if ev.Venue.Lat != 38.7 || ev.Venue.Lon != -9.14 {
		t.Errorf("venue geo = %v/%v", ev.Venue.Lat, ev.Venue.Lon)
	}
}

func TestLumaToEventAbsoluteURLPreserved(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT", Lat: 38.7223, Lon: -9.1393}
	entry := lumaEntry{Event: lumaEvent{
		APIID:   "a",
		Name:    "n",
		URL:     "https://lu.ma/foo",
		StartAt: "2026-06-10T18:00:00Z",
	}}
	entry.Event.Coord.Latitude = 38.7
	entry.Event.Coord.Longitude = -9.14
	ev, ok := lumaToEvent(entry, city, model.CategoryTech)
	if !ok {
		t.Fatal("not built")
	}
	if ev.URL != "https://lu.ma/foo" {
		t.Errorf("URL = %q, want untouched absolute URL", ev.URL)
	}
}

func TestLumaToEventFallbackToCityFields(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT", Lat: 38.7223, Lon: -9.1393}
	entry := lumaEntry{Event: lumaEvent{
		APIID:   "a",
		Name:    "n",
		URL:     "x",
		StartAt: "2026-06-10T18:00:00Z",
	}}
	entry.Event.Coord.Latitude = 38.7
	entry.Event.Coord.Longitude = -9.14
	ev, _ := lumaToEvent(entry, city, model.CategoryTech)
	if ev.City != "Lisbon" || ev.Country != "PT" {
		t.Errorf("city/country = %q/%q (should fall back to catalog city)", ev.City, ev.Country)
	}
}

func TestLumaToEventGeoFilter(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT", Lat: 38.7223, Lon: -9.1393}
	base := lumaEvent{APIID: "a", Name: "n", URL: "x", StartAt: "2026-06-10T18:00:00Z"}

	// No coordinates (online-only event) → dropped.
	if _, ok := lumaToEvent(lumaEntry{Event: base}, city, model.CategoryTech); ok {
		t.Error("event without coordinates should be dropped")
	}

	// Coimbra is ~180km from Lisbon → dropped. Lu.ma's discover feed mixes in
	// region-wide (and, for unknown slugs, IP-located) events.
	far := base
	far.Coord.Latitude, far.Coord.Longitude = 40.2033, -8.4103
	if _, ok := lumaToEvent(lumaEntry{Event: far}, city, model.CategoryTech); ok {
		t.Error("event ~180km away should be dropped")
	}

	// Cascais is ~25km from Lisbon → kept.
	near := base
	near.Coord.Latitude, near.Coord.Longitude = 38.6979, -9.4215
	if _, ok := lumaToEvent(lumaEntry{Event: near}, city, model.CategoryTech); !ok {
		t.Error("event ~25km away should be kept")
	}
}

func TestLumaToEventInvalid(t *testing.T) {
	cases := []lumaEntry{
		{Event: lumaEvent{Name: "n", StartAt: ""}},                 // missing start
		{Event: lumaEvent{Name: "", StartAt: "2026-06-10T18:00Z"}}, // missing name
		{Event: lumaEvent{Name: "n", StartAt: "not-rfc3339"}},      // bad date
	}
	for i, e := range cases {
		if _, ok := lumaToEvent(e, geo.City{}, model.CategoryTech); ok {
			t.Errorf("case %d: expected !ok, got ok", i)
		}
	}
}
