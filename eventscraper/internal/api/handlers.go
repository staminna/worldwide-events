package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/geocode"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

type envelope struct {
	Data any  `json:"data"`
	Meta meta `json:"meta"`
}

type meta struct {
	Total  int    `json:"total"`
	Cached bool   `json:"cached"`
	Age    string `json:"age,omitempty"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) handleCities(w http.ResponseWriter, _ *http.Request) {
	cities := s.cities.All()
	writeJSON(w, 200, envelope{Data: cities, Meta: meta{Total: len(cities)}})
}

// parseLatLon extracts and validates the lat/lon query params shared by the
// /geo/* endpoints. It writes the 400 itself and reports ok=false.
func parseLatLon(w http.ResponseWriter, r *http.Request) (lat, lon float64, ok bool) {
	var errLat, errLon error
	lat, errLat = strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, errLon = strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if errLat != nil || errLon != nil {
		writeErr(w, 400, "lat and lon query params are required numbers")
		return 0, 0, false
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		writeErr(w, 400, "lat must be in [-90,90] and lon in [-180,180]")
		return 0, 0, false
	}
	return lat, lon, true
}

// handleGeoReverse resolves a coordinate to the nearest catalog city —
// reverse geocoding against our own city list, so clients can turn a device
// location into a feed without any external geocoding service. With
// min_events=N it walks cities outward and lands on the first whose feed
// already has N events with coordinates, so an empty catalog city doesn't
// win just by being closest.
func (s *Server) handleGeoReverse(w http.ResponseWriter, r *http.Request) {
	lat, lon, ok := parseLatLon(w, r)
	if !ok {
		return
	}
	minEvents := 0
	if v := r.URL.Query().Get("min_events"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, 400, "min_events must be an integer")
			return
		}
		minEvents = n
	}
	if minEvents <= 0 {
		city, km, ok := s.cities.Nearest(lat, lon)
		if !ok {
			writeErr(w, 404, "no cities in catalog")
			return
		}
		writeJSON(w, 200, envelope{
			Data: map[string]any{
				"city":       city,
				"distanceKm": math.Round(km*10) / 10,
			},
			Meta: meta{Total: 1},
		})
		return
	}

	// Expanding search, bounded so a user in the middle of nowhere doesn't
	// get teleported to another continent.
	const (
		maxCandidates = 15
		maxRadiusKm   = 500.0
	)
	ranked := s.cities.RankedByDistance(lat, lon)
	if len(ranked) == 0 {
		writeErr(w, 404, "no cities in catalog")
		return
	}
	now := time.Now().UTC()
	nearestCount, err := s.store.CountLocatedUpcoming(r.Context(), ranked[0].City.ID, now)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	chosen, chosenCount := ranked[0], nearestCount
	if nearestCount < minEvents {
		for i := 1; i < len(ranked) && i < maxCandidates && ranked[i].Km <= maxRadiusKm; i++ {
			n, err := s.store.CountLocatedUpcoming(r.Context(), ranked[i].City.ID, now)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			if n >= minEvents {
				chosen, chosenCount = ranked[i], n
				break
			}
		}
		// The true nearest city lost on content, not distance — get it
		// scraping in the background so it can win next time.
		nearest := ranked[0].City
		s.kickRefresh(&nearest, "", "")
	}
	writeJSON(w, 200, envelope{
		Data: map[string]any{
			"city":          chosen.City,
			"distanceKm":    math.Round(chosen.Km*10) / 10,
			"locatedEvents": chosenCount,
		},
		Meta: meta{Total: 1},
	})
}

// negativeAddressRetry is how long a cached "no address here" answer is
// trusted before Nominatim gets asked again.
const negativeAddressRetry = 7 * 24 * time.Hour

// handleGeoAddress resolves a coordinate to a street address via Nominatim,
// persistently cached so the 1 req/s upstream budget is spent once per
// venue, ever. With event=<id>, a resolved address is also patched into the
// stored event so feed responses pick it up.
func (s *Server) handleGeoAddress(w http.ResponseWriter, r *http.Request) {
	lat, lon, ok := parseLatLon(w, r)
	if !ok {
		return
	}
	key := geocode.Key(lat, lon)
	addr, resolvedAt, found, err := s.store.GetGeoAddress(r.Context(), key)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !found || (addr == "" && time.Since(resolvedAt) > negativeAddressRetry) {
		// SingleFlight collapses concurrent lookups of the same venue; the
		// leader writes the cache before Do returns, so followers (and the
		// leader) just re-read. Upstream errors are not cached — the re-read
		// then misses and we soft-fail with an empty address.
		_ = s.geoSF.Do(r.Context(), "addr|"+key, func(ctx context.Context) error {
			a, err := s.geocoder.Reverse(ctx, lat, lon)
			if err != nil {
				return err
			}
			return s.store.PutGeoAddress(ctx, key, a)
		})
		addr, _, _, _ = s.store.GetGeoAddress(r.Context(), key)
	}
	if eventID := r.URL.Query().Get("event"); eventID != "" && addr != "" {
		_, _ = s.store.SetVenueAddressIfEmpty(r.Context(), eventID, addr)
	}
	writeJSON(w, 200, envelope{
		Data: map[string]any{"address": addr},
		Meta: meta{Total: 1},
	})
}

type sourceStatus struct {
	ID         model.Source         `json:"id"`
	Configured bool                 `json:"configured"`
	Runs       []store.ScrapeStatus `json:"recentRuns"`
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	scrapes, err := s.store.AllScrapes(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	byID := map[model.Source][]store.ScrapeStatus{}
	for _, sc := range scrapes {
		byID[sc.Source] = append(byID[sc.Source], sc)
	}
	out := []sourceStatus{}
	for _, src := range model.AllSources() {
		_, ok := s.registry.Get(src)
		out = append(out, sourceStatus{ID: src, Configured: ok, Runs: byID[src]})
	}
	writeJSON(w, 200, envelope{Data: out, Meta: meta{Total: len(out)}})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q, cityObj, err := parseQuery(r, s.cities)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	// Trigger background refreshes so the feed keeps filling in.
	// With a specific city → refresh just that city's stale entries.
	// Without a city  → refresh up to the warmup window of cities.
	s.kickRefresh(cityObj, q.Category, q.Source)

	events, total, maxScraped, err := s.store.Query(r.Context(), q)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	etag := ""
	if !maxScraped.IsZero() {
		etag = fmt.Sprintf(`"%d-%d"`, maxScraped.Unix(), total)
		w.Header().Set("ETag", etag)
		if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	age := ""
	if !maxScraped.IsZero() {
		age = time.Since(maxScraped).Truncate(time.Second).String()
	}
	writeJSON(w, 200, envelope{
		Data: events,
		Meta: meta{
			Total:  total,
			Cached: len(events) > 0,
			Age:    age,
			Limit:  q.Limit,
			Offset: q.Offset,
		},
	})
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ev, ok, err := s.store.GetEvent(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if !ok {
		writeErr(w, 404, "event not found")
		return
	}
	writeJSON(w, 200, envelope{Data: ev, Meta: meta{Total: 1}})
}

// createEventInput is the JSON body accepted by POST /events. Coordinates and
// venue fields are optional; without them the event still shows in the feed
// (just not on the map).
type createEventInput struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Category    string       `json:"category"`
	StartsAt    time.Time    `json:"startsAt"`
	EndsAt      *time.Time   `json:"endsAt"`
	CityID      string       `json:"cityId"`
	VenueName   string       `json:"venueName"`
	Address     string       `json:"address"`
	Lat         float64      `json:"lat"`
	Lon         float64      `json:"lon"`
	ImageURL    string       `json:"imageUrl"`
	Price       *model.Price `json:"price"`
}

// handleCreateEvent accepts a user-authored event and stores it under the
// "manual" source. The read API is public, so this write is too — gate it
// behind AdminToken here if abuse becomes a concern (see handleRefresh).
func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var in createEventInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		writeErr(w, 400, "title is required")
		return
	}
	cat := model.Category(in.Category)
	if !cat.Valid() {
		writeErr(w, 400, "invalid category")
		return
	}
	if in.StartsAt.IsZero() {
		writeErr(w, 400, "startsAt is required (RFC3339)")
		return
	}
	// A valid catalog city is required so the event is reachable by the feed's
	// ?city= filter (matching is by cityId, never by name).
	city, ok := s.cities.Get(in.CityID)
	if !ok {
		writeErr(w, 400, "unknown city")
		return
	}

	sourceID, err := randomID()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	ev := model.Event{
		ID:          model.MakeID(model.SourceManual, sourceID),
		Source:      model.SourceManual,
		SourceID:    sourceID,
		Title:       in.Title,
		Description: strings.TrimSpace(in.Description),
		Category:    cat,
		StartsAt:    in.StartsAt.UTC(),
		EndsAt:      in.EndsAt,
		Venue: model.Venue{
			Name:    strings.TrimSpace(in.VenueName),
			Address: strings.TrimSpace(in.Address),
			Lat:     in.Lat,
			Lon:     in.Lon,
		},
		City:      city.Name,
		CityID:    city.ID,
		Country:   city.Country,
		URL:       "",
		ImageURL:  strings.TrimSpace(in.ImageURL),
		Price:     in.Price,
		ScrapedAt: time.Now().UTC(),
	}
	if err := s.store.UpsertEvents(r.Context(), []model.Event{ev}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, envelope{Data: ev, Meta: meta{Total: 1}})
}

// handleGeoSearch forward-geocodes a free-text query to candidate places, for
// the app's map search bar and the add-event venue picker.
func (s *Server) handleGeoSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, 400, "q query param is required")
		return
	}
	results, err := s.geocoder.Search(r.Context(), q)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, envelope{Data: results, Meta: meta{Total: len(results)}})
}

// randomID returns a short random hex string used as the SourceID for manual
// events, so each one's MakeID primary key is unique.
func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AdminToken != "" {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok != s.cfg.AdminToken {
			writeErr(w, 401, "unauthorized")
			return
		}
	}
	cityID := r.URL.Query().Get("city")
	srcStr := r.URL.Query().Get("source")
	city, ok := s.cities.Get(cityID)
	if !ok {
		writeErr(w, 400, "unknown city")
		return
	}
	src := model.Source(srcStr)
	if !src.Valid() {
		writeErr(w, 400, "unknown source")
		return
	}
	go s.scheduler.Run(context.Background(), src, city, model.AllCategories())
	writeJSON(w, 202, map[string]string{"status": "scheduled"})
}

func parseQuery(r *http.Request, cat *geo.Catalog) (store.Query, *geo.City, error) {
	q := store.Query{}
	values := r.URL.Query()
	var cityObj *geo.City
	if cityID := values.Get("city"); cityID != "" {
		c, ok := cat.Get(cityID)
		if !ok {
			return q, nil, errors.New("unknown city")
		}
		q.CityID = c.ID
		cityObj = &c
	}
	if cs := values.Get("category"); cs != "" {
		c := model.Category(cs)
		if !c.Valid() {
			return q, cityObj, errors.New("invalid category")
		}
		q.Category = c
	}
	if ss := values.Get("source"); ss != "" {
		src := model.Source(ss)
		if !src.Valid() {
			return q, cityObj, errors.New("invalid source")
		}
		q.Source = src
	}
	if v := values.Get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return q, cityObj, errors.New("invalid from date (YYYY-MM-DD)")
		}
		q.From = t
	} else {
		// Default: disregard events that already finished. Ongoing events
		// (multi-day festivals, shows without an end time within the grace
		// window) stay visible. An explicit ?from= opts into history.
		q.NotEndedBefore = time.Now().UTC()
	}
	if v := values.Get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return q, cityObj, errors.New("invalid to date (YYYY-MM-DD)")
		}
		q.To = t.Add(24 * time.Hour)
	}
	q.Search = values.Get("q")
	q.Limit, _ = strconv.Atoi(values.Get("limit"))
	if q.Limit <= 0 {
		q.Limit = 100
	}
	if q.Limit > 2000 {
		q.Limit = 2000
	}
	q.Offset, _ = strconv.Atoi(values.Get("offset"))
	if q.Offset < 0 {
		q.Offset = 0
	}
	// Default: hide events with no image. Opt out via ?include_no_image=true.
	q.RequireImage = values.Get("include_no_image") != "true"
	return q, cityObj, nil
}

func (s *Server) kickRefresh(city *geo.City, cat model.Category, src model.Source) {
	// Tests build partial Servers; refresh is best-effort anyway.
	if s.scheduler == nil || s.registry == nil {
		return
	}
	cats := []model.Category{cat}
	if cat == "" {
		cats = model.AllCategories()
	}
	scrapers := s.registry.All()
	if src != "" {
		if one, ok := s.registry.Get(src); ok {
			scrapers = []scraper.Scraper{one}
		}
	}
	if city != nil {
		for _, sc := range scrapers {
			s.scheduler.MaybeRefresh(sc.Source(), *city, cats)
		}
		return
	}
	// Unfiltered feed: refresh the warmup window of cities.
	cities := s.cities.All()
	limit := s.cfg.WarmupCities
	if limit > 0 && limit < len(cities) {
		cities = cities[:limit]
	}
	for _, sc := range scrapers {
		for _, c := range cities {
			s.scheduler.MaybeRefresh(sc.Source(), c, cats)
		}
	}
}
