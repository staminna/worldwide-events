package scraper

import (
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

func makeTMEvent() tmEvent {
	e := tmEvent{
		ID:   "G5v",
		Name: "Rock Show",
		URL:  "https://ticketmaster.com/event/G5v",
	}
	e.Dates.Start.DateTime = "2026-06-10T19:00:00Z"
	e.Images = []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}{
		{URL: "https://img/small.jpg", Width: 100},
		{URL: "https://img/large.jpg", Width: 1200},
		{URL: "https://img/medium.jpg", Width: 600},
	}
	e.PriceRanges = []struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
	}{{Min: 25, Max: 75, Currency: "EUR"}}
	venue := struct {
		Name    string `json:"name"`
		Address struct {
			Line1 string `json:"line1"`
		} `json:"address"`
		City struct {
			Name string `json:"name"`
		} `json:"city"`
		Country struct {
			CountryCode string `json:"countryCode"`
		} `json:"country"`
		Location struct {
			Latitude  string `json:"latitude"`
			Longitude string `json:"longitude"`
		} `json:"location"`
	}{}
	venue.Name = "Altice Arena"
	venue.Address.Line1 = "Rossio"
	venue.City.Name = "Lisboa"
	venue.Country.CountryCode = "PT"
	venue.Location.Latitude = "38.7"
	venue.Location.Longitude = "-9.14"
	e.Embedded.Venues = append(e.Embedded.Venues, venue)
	return e
}

func TestTmToEventFull(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT"}
	ev := tmToEvent(makeTMEvent(), city, model.CategoryMusic)

	if ev.Source != model.SourceTicketmaster || ev.SourceID != "G5v" {
		t.Errorf("source/id = %v/%q", ev.Source, ev.SourceID)
	}
	if ev.ID != model.MakeID(model.SourceTicketmaster, "G5v") {
		t.Errorf("ID hash mismatch")
	}
	if !ev.StartsAt.Equal(time.Date(2026, 6, 10, 19, 0, 0, 0, time.UTC)) {
		t.Errorf("StartsAt = %v", ev.StartsAt)
	}
	if ev.ImageURL != "https://img/large.jpg" {
		t.Errorf("ImageURL = %q, want widest image", ev.ImageURL)
	}
	if ev.Venue.Lat != 38.7 || ev.Venue.Lon != -9.14 {
		t.Errorf("venue geo = %v/%v", ev.Venue.Lat, ev.Venue.Lon)
	}
	if ev.City != "Lisboa" || ev.Country != "PT" {
		t.Errorf("city/country = %q/%q (should prefer venue)", ev.City, ev.Country)
	}
	if ev.Price == nil || ev.Price.Min != 25 || ev.Price.Max != 75 || ev.Price.Currency != "EUR" || ev.Price.Free {
		t.Errorf("price = %+v", ev.Price)
	}
}

func TestTmToEventLocalDateFallback(t *testing.T) {
	e := tmEvent{Name: "x", URL: "u", ID: "id1"}
	e.Dates.Start.LocalDate = "2026-06-10"
	e.Dates.Start.LocalTime = "19:00:00"
	ev := tmToEvent(e, geo.City{Name: "L", Country: "PT"}, model.CategoryMusic)
	if ev.StartsAt.IsZero() {
		t.Error("StartsAt should be populated from LocalDate+LocalTime")
	}
}

func TestTmToEventLocalDateOnly(t *testing.T) {
	e := tmEvent{Name: "x", URL: "u", ID: "id2"}
	e.Dates.Start.LocalDate = "2026-06-10"
	ev := tmToEvent(e, geo.City{Name: "L", Country: "PT"}, model.CategoryMusic)
	if ev.StartsAt.IsZero() {
		t.Error("StartsAt should be populated from LocalDate alone")
	}
}

func TestTmToEventFreeWhenAllZero(t *testing.T) {
	e := makeTMEvent()
	e.PriceRanges[0].Min = 0
	e.PriceRanges[0].Max = 0
	ev := tmToEvent(e, geo.City{Name: "L", Country: "PT"}, model.CategoryMusic)
	if ev.Price == nil || !ev.Price.Free {
		t.Errorf("expected Free=true price, got %+v", ev.Price)
	}
}

func TestTmToEventCityFallback(t *testing.T) {
	e := makeTMEvent()
	e.Embedded.Venues = nil
	ev := tmToEvent(e, geo.City{Name: "Lisbon", Country: "PT"}, model.CategoryMusic)
	if ev.City != "Lisbon" || ev.Country != "PT" {
		t.Errorf("fallback city/country = %q/%q", ev.City, ev.Country)
	}
	if ev.Venue.Name != "" {
		t.Errorf("venue should be empty, got %+v", ev.Venue)
	}
}
