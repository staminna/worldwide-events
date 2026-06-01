package scraper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

// Luma scrapes lu.ma via their public discover API
// (https://api.lu.ma/discover/get-paginated-events). No auth required.
type Luma struct {
	HTTP *http.Client
}

func NewLuma() *Luma {
	return &Luma{HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (l *Luma) Source() model.Source { return model.SourceLuma }

// Luma's discover categories. They use slightly different names per category.
var lumaCategoryFilter = map[model.Category]string{
	model.CategoryTech:     "tech",
	model.CategoryMusic:    "arts-and-music",
	model.CategoryBusiness: "wellness", // closest neutral; lu.ma has no "business" — see also climate, etc.
}

const lumaDiscoverURL = "https://api.lu.ma/discover/get-paginated-events"

// Max pages to walk per (city, category). 5 × 100 = up to 500 events per cat.
const lumaMaxPages = 5

func (l *Luma) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	slug := city.LumaSlug()
	if slug == "" {
		return nil, nil
	}
	if len(cats) == 0 {
		cats = model.AllCategories()
	}
	var out []model.Event
	for _, cat := range cats {
		filter, ok := lumaCategoryFilter[cat]
		if !ok {
			continue
		}
		cursor := ""
		for page := 0; page < lumaMaxPages; page++ {
			q := url.Values{}
			q.Set("pagination_limit", "100")
			q.Set("period", "future")
			q.Set("filter_by_category", filter)
			q.Set("geo_city_slug", slug)
			if cursor != "" {
				q.Set("pagination_cursor", cursor)
			}

			req, _ := http.NewRequestWithContext(ctx, "GET", lumaDiscoverURL+"?"+q.Encode(), nil)
			req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; eventscraper/1.0)")
			req.Header.Set("Accept", "application/json")
			resp, err := l.HTTP.Do(req)
			if err != nil {
				break
			}
			if resp.StatusCode == 403 || resp.StatusCode == 429 {
				resp.Body.Close()
				return out, ErrBlocked
			}
			if resp.StatusCode >= 400 {
				resp.Body.Close()
				break
			}
			var body lumaResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				resp.Body.Close()
				break
			}
			resp.Body.Close()
			for _, entry := range body.Entries {
				if ev, ok := lumaToEvent(entry, city, cat); ok {
					out = append(out, ev)
				}
			}
			if !body.HasMore || body.NextCursor == "" {
				break
			}
			cursor = body.NextCursor
		}
	}
	return out, nil
}

type lumaResponse struct {
	Entries    []lumaEntry `json:"entries"`
	HasMore    bool        `json:"has_more"`
	NextCursor string      `json:"next_cursor"`
}

type lumaEntry struct {
	APIID string    `json:"api_id"`
	Event lumaEvent `json:"event"`
}

type lumaEvent struct {
	APIID      string  `json:"api_id"`
	Name       string  `json:"name"`
	URL        string  `json:"url"`
	CoverURL   string  `json:"cover_url"`
	StartAt    string  `json:"start_at"`
	EndAt      string  `json:"end_at"`
	Timezone   string  `json:"timezone"`
	Visibility string  `json:"visibility"`
	Geo        lumaGeo `json:"geo_address_info"`
	Coord      struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"coordinate"`
}

type lumaGeo struct {
	City        string `json:"city"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Address     string `json:"full_address"`
}

func lumaToEvent(entry lumaEntry, city geo.City, cat model.Category) (model.Event, bool) {
	e := entry.Event
	if e.Name == "" || e.StartAt == "" {
		return model.Event{}, false
	}
	starts, err := time.Parse(time.RFC3339, e.StartAt)
	if err != nil {
		return model.Event{}, false
	}
	var endsPtr *time.Time
	if e.EndAt != "" {
		if t, err := time.Parse(time.RFC3339, e.EndAt); err == nil {
			endsPtr = &t
		}
	}
	cityName := city.Name
	country := city.Country
	if e.Geo.City != "" {
		cityName = e.Geo.City
	}
	if e.Geo.CountryCode != "" {
		country = e.Geo.CountryCode
	}
	url := e.URL
	if url != "" && !strings.HasPrefix(url, "http") {
		url = "https://lu.ma/" + url
	}
	return model.Event{
		ID:        model.MakeID(model.SourceLuma, e.APIID),
		Source:    model.SourceLuma,
		SourceID:  e.APIID,
		Title:     e.Name,
		Category:  cat,
		StartsAt:  starts.UTC(),
		EndsAt:    endsPtr,
		Venue:     model.Venue{Address: e.Geo.Address, Lat: e.Coord.Latitude, Lon: e.Coord.Longitude},
		City:      cityName,
		Country:   country,
		URL:       url,
		ImageURL:  e.CoverURL,
		ScrapedAt: time.Now().UTC(),
	}, true
}

