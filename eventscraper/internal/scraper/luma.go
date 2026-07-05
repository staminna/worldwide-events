package scraper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

// Luma scrapes lu.ma via their public discover API
// (https://api.lu.ma/discover/get-paginated-events). No auth required.
type Luma struct {
	HTTP *http.Client

	mu       sync.Mutex
	placeIDs map[string]string // city ID → discplace api_id; "" = page has none
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
	// CategoryArts is intentionally unmapped: lu.ma's single "arts-and-music"
	// feed is already ingested under music; pointing arts at the same feed
	// would scrape the same events twice with a flip-flopping category.
}

const lumaDiscoverURL = "https://api.lu.ma/discover/get-paginated-events"

// Max pages to walk per (city, category). 5 × 100 = up to 500 events per cat.
const lumaMaxPages = 5

// lumaMaxKm is how far an event may be from the requested city and still be
// kept. Lu.ma's discover feed does not strictly honour geo_city_slug — for
// unknown slugs it falls back to the *caller's* IP location, and for known
// ones it mixes in region-wide events — so we filter by the event's own
// coordinates. 75km keeps a metro area (Cascais–Lisbon ≈ 30km) while dropping
// other cities (Coimbra–Lisbon ≈ 180km).
const lumaMaxKm = 75.0

var lumaPlaceIDRe = regexp.MustCompile(`discplace-[A-Za-z0-9]+`)

// placeID resolves the lu.ma discover place ID for a city by scraping its
// public city page (https://luma.com/<slug>) and caches the result for the
// scraper's lifetime. The discover API silently ignores geo_city_slug and
// falls back to the caller's IP location; discover_place_api_id is the only
// parameter it actually honours. Returns "" when the city has no luma page
// (non-city slugs redirect away or lack the ID).
func (l *Luma) placeID(ctx context.Context, city geo.City) string {
	l.mu.Lock()
	if l.placeIDs == nil {
		l.placeIDs = make(map[string]string)
	}
	id, cached := l.placeIDs[city.ID]
	l.mu.Unlock()
	if cached {
		return id
	}

	slug := city.LumaSlug()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://luma.com/"+slug, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; eventscraper/1.0)")
	resp, err := l.HTTP.Do(req)
	if err != nil {
		// Network failure: don't cache, so the next scrape retries.
		return ""
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		// Follow-redirect landing on a different path means "<slug> is not
		// a city page" (e.g. luma.com/porto 302s to an unrelated page).
		if strings.EqualFold(resp.Request.URL.Path, "/"+slug) {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			id = string(lumaPlaceIDRe.Find(body))
		}
	case resp.StatusCode == http.StatusNotFound:
		// Definitive miss: cache the empty answer.
	default:
		// Throttled (403/429) or transient error: caching "" here would
		// pin the city to the useless slug fallback for the process
		// lifetime. Retry on the next scrape instead.
		return ""
	}
	l.mu.Lock()
	l.placeIDs[city.ID] = id
	l.mu.Unlock()
	return id
}

func (l *Luma) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	slug := city.LumaSlug()
	if slug == "" {
		return nil, nil
	}
	if len(cats) == 0 {
		cats = model.AllCategories()
	}
	placeID := l.placeID(ctx, city)
	var out []model.Event
	first := true
	for _, cat := range cats {
		filter, ok := lumaCategoryFilter[cat]
		if !ok {
			continue
		}
		cursor := ""
		for page := 0; page < lumaMaxPages; page++ {
			// A scrape can issue up to placeID + 3 cats × 5 pages requests;
			// fired back-to-back they trip lu.ma's burst rate limiting
			// (429 → whole scrape reported blocked). Pace them out.
			if !first {
				select {
				case <-ctx.Done():
					return out, nil
				case <-time.After(400 * time.Millisecond):
				}
			}
			first = false
			q := url.Values{}
			q.Set("pagination_limit", "100")
			q.Set("period", "future")
			q.Set("filter_by_category", filter)
			if placeID != "" {
				q.Set("discover_place_api_id", placeID)
			} else {
				// Best effort for cities without a luma page: the API falls
				// back to IP location, and the coordinate filter below drops
				// whatever isn't actually near the requested city.
				q.Set("geo_city_slug", slug)
			}
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
	// Events without coordinates (online-only) or too far from the requested
	// city are noise from lu.ma's loose geo filtering — drop them.
	if e.Coord.Latitude == 0 && e.Coord.Longitude == 0 {
		return model.Event{}, false
	}
	if geo.KmBetween(city.Lat, city.Lon, e.Coord.Latitude, e.Coord.Longitude) > lumaMaxKm {
		return model.Event{}, false
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
