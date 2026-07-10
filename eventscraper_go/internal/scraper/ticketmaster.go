package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

type Ticketmaster struct {
	APIKey string
	client *StealthClient
}

// NewTicketmaster builds the scraper with the shared stealth client. A nil
// client (tests) falls back to a plain direct client.
func NewTicketmaster(apiKey string, client *StealthClient) *Ticketmaster {
	if client == nil {
		client = NewStealthClient(StealthConfig{})
	}
	return &Ticketmaster{APIKey: apiKey, client: client}
}

func (t *Ticketmaster) Source() model.Source { return model.SourceTicketmaster }

// Ticketmaster Discovery API segment IDs (stable per their docs).
var tmSegmentIDs = map[model.Category]string{
	model.CategoryMusic:    "KZFzniwnSyZfZ7v7nJ",
	model.CategoryArts:     "KZFzniwnSyZfZ7v7na", // Arts & Theatre
	model.CategoryBusiness: "KZFzniwnSyZfZ7v7n1", // Miscellaneous (closest fit; Discovery has no "business" segment)
}

const ticketmasterBaseURL = "https://app.ticketmaster.com/discovery/v2/events.json"

func (t *Ticketmaster) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	if t.APIKey == "" {
		return nil, ErrUnconfigured
	}
	if len(cats) == 0 {
		cats = model.AllCategories()
	}
	var out []model.Event
	for _, cat := range cats {
		segID, ok := tmSegmentIDs[cat]
		if !ok {
			// Ticketmaster doesn't really cover "tech" — skip.
			continue
		}
		q := url.Values{}
		q.Set("apikey", t.APIKey)
		q.Set("size", "50")
		q.Set("sort", "date,asc")
		q.Set("segmentId", segID)
		if city.TicketmasterMarket > 0 {
			q.Set("marketId", strconv.Itoa(city.TicketmasterMarket))
		} else {
			q.Set("city", city.Name)
			q.Set("countryCode", city.Country)
		}
		q.Set("startDateTime", time.Now().UTC().Format("2006-01-02T15:04:05Z"))

		req, _ := http.NewRequestWithContext(ctx, "GET", ticketmasterBaseURL+"?"+q.Encode(), nil)
		req.Header.Set("Accept", "application/json")
		resp, err := t.client.Do(req)
		if err != nil {
			continue
		}
		var body tmResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return out, ErrUnconfigured
		}
		if resp.StatusCode >= 400 {
			return out, fmt.Errorf("ticketmaster: status %d", resp.StatusCode)
		}
		for _, e := range body.Embedded.Events {
			out = append(out, tmToEvent(e, city, cat))
		}
	}
	return out, nil
}

type tmResponse struct {
	Embedded struct {
		Events []tmEvent `json:"events"`
	} `json:"_embedded"`
}

type tmEvent struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	URL   string `json:"url"`
	Info  string `json:"info"`
	Dates struct {
		Start struct {
			LocalDate string `json:"localDate"`
			LocalTime string `json:"localTime"`
			DateTime  string `json:"dateTime"`
		} `json:"start"`
	} `json:"dates"`
	Images []struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"images"`
	PriceRanges []struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
	} `json:"priceRanges"`
	Embedded struct {
		Venues []struct {
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
		} `json:"venues"`
	} `json:"_embedded"`
}

func tmToEvent(e tmEvent, city geo.City, cat model.Category) model.Event {
	var starts time.Time
	if e.Dates.Start.DateTime != "" {
		t, err := time.Parse(time.RFC3339, e.Dates.Start.DateTime)
		if err == nil {
			starts = t.UTC()
		}
	}
	if starts.IsZero() && e.Dates.Start.LocalDate != "" {
		layout := "2006-01-02"
		val := e.Dates.Start.LocalDate
		if e.Dates.Start.LocalTime != "" {
			layout = "2006-01-02 15:04:05"
			val = e.Dates.Start.LocalDate + " " + e.Dates.Start.LocalTime
		}
		if t, err := time.Parse(layout, val); err == nil {
			starts = t.UTC()
		}
	}
	venue := model.Venue{}
	cityName := city.Name
	country := city.Country
	if len(e.Embedded.Venues) > 0 {
		v := e.Embedded.Venues[0]
		venue.Name = v.Name
		venue.Address = v.Address.Line1
		if lat, err := strconv.ParseFloat(v.Location.Latitude, 64); err == nil {
			venue.Lat = lat
		}
		if lon, err := strconv.ParseFloat(v.Location.Longitude, 64); err == nil {
			venue.Lon = lon
		}
		if v.City.Name != "" {
			cityName = v.City.Name
		}
		if v.Country.CountryCode != "" {
			country = v.Country.CountryCode
		}
	}
	img := ""
	bestW := 0
	for _, im := range e.Images {
		if im.Width > bestW {
			bestW = im.Width
			img = im.URL
		}
	}
	var price *model.Price
	if len(e.PriceRanges) > 0 {
		p := e.PriceRanges[0]
		price = &model.Price{Min: p.Min, Max: p.Max, Currency: p.Currency, Free: p.Min == 0 && p.Max == 0}
	}
	return model.Event{
		ID:          model.MakeID(model.SourceTicketmaster, e.ID),
		Source:      model.SourceTicketmaster,
		SourceID:    e.ID,
		Title:       e.Name,
		Description: e.Info,
		Category:    cat,
		StartsAt:    starts,
		Venue:       venue,
		City:        cityName,
		Country:     country,
		URL:         e.URL,
		ImageURL:    img,
		Price:       price,
		ScrapedAt:   time.Now().UTC(),
	}
}
