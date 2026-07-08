// Package geocode reverse-geocodes coordinates to street addresses via the
// public Nominatim (OpenStreetMap) API. Nominatim's usage policy allows at
// most one request per second with an identifying User-Agent, so the client
// enforces a global rate limit and callers are expected to cache results
// (see store.GetGeoAddress / PutGeoAddress).
package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const userAgent = "Mozilla/5.0 (compatible; eventscraper/1.0; +https://github.com/jorgenunes/eventscraper)"

type Client struct {
	HTTP    *http.Client
	BaseURL string
	// MinInterval is the enforced spacing between upstream requests.
	MinInterval time.Duration
	// MaxWait bounds how long one call may queue behind the limiter before
	// giving up, so a burst of cache misses can't pile up for 30s inside
	// request handlers.
	MaxWait time.Duration

	mu     sync.Mutex
	nextAt time.Time
}

func New() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 6 * time.Second},
		BaseURL:     "https://nominatim.openstreetmap.org",
		MinInterval: 1100 * time.Millisecond,
		MaxWait:     5 * time.Second,
	}
}

// Key is the cache key for a coordinate: 5 decimal places (~1 m), enough
// that two sources publishing the same venue usually collide onto one entry.
func Key(lat, lon float64) string {
	return fmt.Sprintf("%.5f,%.5f", lat, lon)
}

type nominatimResponse struct {
	Error       string `json:"error"`
	DisplayName string `json:"display_name"`
	Address     struct {
		HouseNumber  string `json:"house_number"`
		Road         string `json:"road"`
		Postcode     string `json:"postcode"`
		City         string `json:"city"`
		Town         string `json:"town"`
		Village      string `json:"village"`
		Municipality string `json:"municipality"`
		Suburb       string `json:"suburb"`
	} `json:"address"`
}

// Reverse resolves a coordinate to a short street address. It returns
// ("", nil) when Nominatim answers successfully but knows nothing about the
// spot (open water, wilderness) — a cacheable negative — and ("", err) on
// transport or HTTP failures, which must NOT be cached.
func (c *Client) Reverse(ctx context.Context, lat, lon float64) (string, error) {
	if err := c.waitSlot(ctx); err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("format", "jsonv2")
	q.Set("lat", fmt.Sprintf("%f", lat))
	q.Set("lon", fmt.Sprintf("%f", lon))
	q.Set("addressdetails", "1")
	q.Set("zoom", "18")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/reverse?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nominatim status %d", res.StatusCode)
	}
	var body nominatimResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return "", err
	}
	// Nominatim reports "nothing here" as 200 + {"error": "Unable to geocode"}.
	if body.Error != "" {
		return "", nil
	}
	return composeAddress(body), nil
}

// SearchResult is one candidate place from a forward-geocoding query.
type SearchResult struct {
	DisplayName string  `json:"displayName"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
}

// searchItem mirrors one entry of Nominatim's /search array (lat/lon come
// back as strings).
type searchItem struct {
	DisplayName string `json:"display_name"`
	Lat         string `json:"lat"`
	Lon         string `json:"lon"`
}

// Search forward-geocodes a free-text query ("Coliseu dos Recreios, Lisbon")
// to a ranked list of candidate places. Shares the same 1 req/s upstream
// budget as Reverse via waitSlot. Returns an empty slice (not an error) when
// Nominatim finds nothing.
func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if err := c.waitSlot(ctx); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("format", "jsonv2")
	q.Set("addressdetails", "0")
	q.Set("limit", "6")
	q.Set("q", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nominatim status %d", res.StatusCode)
	}
	var items []searchItem
	if err := json.NewDecoder(res.Body).Decode(&items); err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(items))
	for _, it := range items {
		lat, errLat := strconv.ParseFloat(it.Lat, 64)
		lon, errLon := strconv.ParseFloat(it.Lon, 64)
		if errLat != nil || errLon != nil {
			continue
		}
		out = append(out, SearchResult{DisplayName: it.DisplayName, Lat: lat, Lon: lon})
	}
	return out, nil
}

// composeAddress builds a compact European-style line: "Road 12, 1100-048
// Lisboa". Without a road (parks, large venues) it falls back to the first
// few segments of display_name, which is otherwise unpleasantly long.
func composeAddress(r nominatimResponse) string {
	locality := firstNonEmpty(
		r.Address.City, r.Address.Town, r.Address.Village,
		r.Address.Municipality, r.Address.Suburb,
	)
	if r.Address.Road != "" {
		street := strings.TrimSpace(r.Address.Road + " " + r.Address.HouseNumber)
		place := strings.TrimSpace(r.Address.Postcode + " " + locality)
		if place == "" {
			return street
		}
		return street + ", " + place
	}
	if r.DisplayName != "" {
		parts := strings.Split(r.DisplayName, ",")
		if len(parts) > 4 {
			parts = parts[:4]
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// waitSlot reserves the next upstream slot, sleeping until it opens. It
// fails fast when the queue already stretches past MaxWait.
func (c *Client) waitSlot(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.nextAt
	if start.Before(now) {
		start = now
	}
	wait := start.Sub(now)
	if wait > c.MaxWait {
		c.mu.Unlock()
		return errors.New("geocode: rate-limit queue full, try again shortly")
	}
	c.nextAt = start.Add(c.MinInterval)
	c.mu.Unlock()
	if wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
