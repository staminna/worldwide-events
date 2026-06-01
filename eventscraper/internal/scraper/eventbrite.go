package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/extensions"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

type Eventbrite struct{}

func NewEventbrite() *Eventbrite { return &Eventbrite{} }

func (e *Eventbrite) Source() model.Source { return model.SourceEventbrite }

var eventbriteCategoryPath = map[model.Category]string{
	model.CategoryTech:     "science-and-tech",
	model.CategoryMusic:    "music",
	model.CategoryBusiness: "business",
}

// Eventbrite embeds its event list as a JSON blob assigned to
// window.__SERVER_DATA__. Inside, data.jsonld[0].itemListElement[*].item is the
// schema.org Event payload.
var serverDataRe = regexp.MustCompile(`window\.__SERVER_DATA__\s*=\s*(\{.*?\});`)

func (e *Eventbrite) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	if city.EventbriteSlug == "" {
		return nil, nil
	}
	if len(cats) == 0 {
		cats = model.AllCategories()
	}
	var (
		mu     sync.Mutex
		events []model.Event
	)

	for _, cat := range cats {
		select {
		case <-ctx.Done():
			return events, ctx.Err()
		default:
		}
		path, ok := eventbriteCategoryPath[cat]
		if !ok {
			continue
		}
		url := fmt.Sprintf("https://www.eventbrite.com/d/%s/%s--events/", city.EventbriteSlug, path)

		c := colly.NewCollector(
			colly.Async(true),
			colly.AllowedDomains("www.eventbrite.com", "eventbrite.com", "www.eventbrite.co.uk", "eventbrite.co.uk"),
		)
		extensions.RandomUserAgent(c)
		extensions.Referer(c)
		_ = c.Limit(&colly.LimitRule{
			DomainGlob:  "*eventbrite*",
			Parallelism: 4,
			Delay:       500 * time.Millisecond,
			RandomDelay: 800 * time.Millisecond,
		})

		var blocked bool
		c.OnResponse(func(r *colly.Response) {
			if r.StatusCode == 403 || r.StatusCode == 429 {
				blocked = true
				return
			}
			parseEventbritePage(r.Body, city, cat, &mu, &events)
		})

		c.OnRequest(func(r *colly.Request) {
			r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
		})

		if err := c.Visit(url); err != nil {
			continue
		}
		c.Wait()
		if blocked {
			return events, ErrBlocked
		}
		time.Sleep(time.Duration(150+rand.Intn(250)) * time.Millisecond)
	}
	return events, nil
}

type ebItem struct {
	Type        any    `json:"@type"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	StartDate   string `json:"startDate"`
	EndDate     string `json:"endDate"`
	Description string `json:"description"`
	Image       any    `json:"image"`
	Location    struct {
		Name    string `json:"name"`
		Address struct {
			StreetAddress   string `json:"streetAddress"`
			AddressLocality string `json:"addressLocality"`
			AddressRegion   string `json:"addressRegion"`
			AddressCountry  string `json:"addressCountry"`
			PostalCode      string `json:"postalCode"`
		} `json:"address"`
		Geo struct {
			Latitude  any `json:"latitude"`
			Longitude any `json:"longitude"`
		} `json:"geo"`
	} `json:"location"`
}

type ebListItem struct {
	Item ebItem `json:"item"`
}

type ebListBlock struct {
	Type            string       `json:"@type"`
	ItemListElement []ebListItem `json:"itemListElement"`
}

type ebServerData struct {
	JSONLD []ebListBlock `json:"jsonld"`
}

func parseEventbritePage(body []byte, city geo.City, cat model.Category, mu *sync.Mutex, out *[]model.Event) {
	m := serverDataRe.FindSubmatch(body)
	if len(m) < 2 {
		return
	}
	var sd ebServerData
	if err := json.Unmarshal(m[1], &sd); err != nil {
		return
	}
	for _, block := range sd.JSONLD {
		for _, it := range block.ItemListElement {
			if ev, ok := buildEventbriteEvent(it.Item, city, cat); ok {
				mu.Lock()
				*out = append(*out, ev)
				mu.Unlock()
			}
		}
	}
}

func buildEventbriteEvent(e ebItem, city geo.City, cat model.Category) (model.Event, bool) {
	if e.Name == "" || e.URL == "" || e.StartDate == "" {
		return model.Event{}, false
	}
	starts, ok := parseEBDate(e.StartDate)
	if !ok {
		return model.Event{}, false
	}
	var endsPtr *time.Time
	if e.EndDate != "" {
		if t, ok := parseEBDate(e.EndDate); ok {
			endsPtr = &t
		}
	}
	// Source ID from trailing number in /e/title-tickets-12345...
	sourceID := e.URL
	if i := strings.LastIndex(strings.TrimRight(e.URL, "/"), "-"); i > 0 {
		sourceID = strings.TrimRight(e.URL, "/")[i+1:]
	}
	img := ""
	switch v := e.Image.(type) {
	case string:
		img = v
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				img = s
			}
		}
	}
	addr := strings.TrimSpace(strings.Join([]string{
		e.Location.Address.StreetAddress,
		e.Location.Address.AddressLocality,
		e.Location.Address.AddressRegion,
		e.Location.Address.PostalCode,
	}, ", "))
	country := e.Location.Address.AddressCountry
	if country == "" {
		country = city.Country
	}
	return model.Event{
		ID:          model.MakeID(model.SourceEventbrite, sourceID),
		Source:      model.SourceEventbrite,
		SourceID:    sourceID,
		Title:       e.Name,
		Description: e.Description,
		Category:    cat,
		StartsAt:    starts,
		EndsAt:      endsPtr,
		Venue: model.Venue{
			Name:    e.Location.Name,
			Address: addr,
			Lat:     toFloat(e.Location.Geo.Latitude),
			Lon:     toFloat(e.Location.Geo.Longitude),
		},
		City:      city.Name,
		Country:   country,
		URL:       e.URL,
		ImageURL:  img,
		ScrapedAt: time.Now().UTC(),
	}, true
}

func parseEBDate(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case string:
		var f float64
		_, _ = fmt.Sscanf(n, "%f", &f)
		return f
	}
	return 0
}
