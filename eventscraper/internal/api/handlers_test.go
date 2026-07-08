package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/cache"
	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/geocode"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/store"
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

func TestHandleGeoReverse(t *testing.T) {
	s := &Server{cities: newCatalog(t)}

	// Cascais (~25km west of Lisbon) resolves to Lisbon.
	rec := httptest.NewRecorder()
	s.handleGeoReverse(rec, httptest.NewRequest("GET", "/geo/reverse?lat=38.6979&lon=-9.4207", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Data struct {
			City       geo.City `json:"city"`
			DistanceKm float64  `json:"distanceKm"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.City.ID != "lisbon" {
		t.Errorf("city = %q, want lisbon", resp.Data.City.ID)
	}
	if resp.Data.DistanceKm <= 0 || resp.Data.DistanceKm > 60 {
		t.Errorf("distanceKm = %v, want ~25", resp.Data.DistanceKm)
	}

	// Missing/garbage/out-of-range params are 400s.
	for _, q := range []string{"", "?lat=38.7", "?lat=abc&lon=-9.1", "?lat=91&lon=0", "?lat=0&lon=181"} {
		rec := httptest.NewRecorder()
		s.handleGeoReverse(rec, httptest.NewRequest("GET", "/geo/reverse"+q, nil))
		if rec.Code != 400 {
			t.Errorf("query %q: status = %d, want 400", q, rec.Code)
		}
	}
}

// newTestStore builds a real SQLite store in a temp dir; handler tests that
// exercise store-backed paths use it instead of stubbing.
func newTestStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "api-test.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func locatedEvent(t *testing.T, st store.Store, sourceID, cityID string, lat, lon float64) model.Event {
	t.Helper()
	ev := model.Event{
		ID:        model.MakeID(model.SourceLuma, sourceID),
		Source:    model.SourceLuma,
		SourceID:  sourceID,
		Title:     "Event " + sourceID,
		Category:  model.CategoryMusic,
		StartsAt:  time.Now().UTC().Add(24 * time.Hour),
		Venue:     model.Venue{Name: "Venue " + sourceID, Lat: lat, Lon: lon},
		City:      cityID,
		CityID:    cityID,
		Country:   "PT",
		URL:       "https://example.com/" + sourceID,
		ImageURL:  "https://img/" + sourceID + ".jpg",
		ScrapedAt: time.Now().UTC(),
	}
	if err := st.UpsertEvents(context.Background(), []model.Event{ev}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	return ev
}

func TestHandleGeoReverseMinEvents(t *testing.T) {
	st := newTestStore(t)
	s := &Server{cities: newCatalog(t), store: st}

	// Porto has 3 located events; Lisbon none. Request from Sintra (near
	// Lisbon) with min_events=3 must skip Lisbon and land on Porto.
	locatedEvent(t, st, "p1", "porto", 41.15, -8.61)
	locatedEvent(t, st, "p2", "porto", 41.16, -8.60)
	locatedEvent(t, st, "p3", "porto", 41.14, -8.62)

	get := func(url string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		s.handleGeoReverse(rec, httptest.NewRequest("GET", url, nil))
		var body struct {
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.Data
	}

	code, data := get("/geo/reverse?lat=38.8029&lon=-9.3817&min_events=3")
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	city := data["city"].(map[string]any)
	if city["id"] != "porto" {
		t.Errorf("city = %v, want porto (lisbon has no located events)", city["id"])
	}
	if data["locatedEvents"].(float64) != 3 {
		t.Errorf("locatedEvents = %v, want 3", data["locatedEvents"])
	}

	// Unsatisfiable minimum falls back to the plain nearest city.
	code, data = get("/geo/reverse?lat=38.8029&lon=-9.3817&min_events=99")
	if code != 200 {
		t.Fatalf("fallback status = %d", code)
	}
	if city := data["city"].(map[string]any); city["id"] != "lisbon" {
		t.Errorf("fallback city = %v, want lisbon", city["id"])
	}
	if data["locatedEvents"].(float64) != 0 {
		t.Errorf("fallback locatedEvents = %v, want 0", data["locatedEvents"])
	}

	// Nearest city already qualifying wins without expansion.
	code, data = get("/geo/reverse?lat=41.2&lon=-8.6&min_events=2")
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	if city := data["city"].(map[string]any); city["id"] != "porto" {
		t.Errorf("city = %v, want porto", city["id"])
	}

	// Without min_events the response keeps its original shape.
	_, data = get("/geo/reverse?lat=38.8029&lon=-9.3817")
	if _, present := data["locatedEvents"]; present {
		t.Error("locatedEvents must be absent without min_events")
	}

	// Garbage min_events is a 400.
	if code, _ := get("/geo/reverse?lat=38.8&lon=-9.3&min_events=abc"); code != 400 {
		t.Errorf("min_events=abc status = %d, want 400", code)
	}
}

func TestHandleGeoAddress(t *testing.T) {
	var upstreamHits int
	nominatim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		if strings.HasPrefix(r.URL.Query().Get("lat"), "0.") {
			_, _ = w.Write([]byte(`{"error":"Unable to geocode"}`))
			return
		}
		_, _ = w.Write([]byte(`{"address":{"road":"Rua Augusta","house_number":"1","postcode":"1100-048","city":"Lisboa"}}`))
	}))
	defer nominatim.Close()

	st := newTestStore(t)
	s := &Server{
		cities: newCatalog(t),
		store:  st,
		geocoder: &geocode.Client{
			HTTP:        nominatim.Client(),
			BaseURL:     nominatim.URL,
			MinInterval: time.Millisecond,
			MaxWait:     time.Second,
		},
		geoSF: cache.NewSingleFlight(),
	}
	ev := locatedEvent(t, st, "e1", "lisbon", 38.7223, -9.1393)

	get := func(url string) (int, string) {
		rec := httptest.NewRecorder()
		s.handleGeoAddress(rec, httptest.NewRequest("GET", url, nil))
		var body struct {
			Data struct {
				Address string `json:"address"`
			} `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.Data.Address
	}

	// First lookup goes upstream, second is served from the cache.
	code, addr := get("/geo/address?lat=38.7223&lon=-9.1393")
	if code != 200 || addr != "Rua Augusta 1, 1100-048 Lisboa" {
		t.Fatalf("first: code=%d addr=%q", code, addr)
	}
	if _, addr2 := get("/geo/address?lat=38.7223&lon=-9.1393"); addr2 != addr {
		t.Errorf("second lookup = %q, want cached %q", addr2, addr)
	}
	if upstreamHits != 1 {
		t.Errorf("upstream hits = %d, want 1 (second call must hit cache)", upstreamHits)
	}

	// event= patches the stored payload so the feed picks the address up.
	_, _ = get("/geo/address?lat=38.7223&lon=-9.1393&event=" + ev.ID)
	got, _, _ := st.GetEvent(context.Background(), ev.ID)
	if got.Venue.Address != addr {
		t.Errorf("patched venue address = %q, want %q", got.Venue.Address, addr)
	}

	// A "nothing here" answer is negative-cached: empty address, single hit.
	before := upstreamHits
	if _, addr := get("/geo/address?lat=0.5&lon=0.5"); addr != "" {
		t.Errorf("ocean addr = %q, want empty", addr)
	}
	_, _ = get("/geo/address?lat=0.5&lon=0.5")
	if upstreamHits != before+1 {
		t.Errorf("negative not cached: %d extra hits", upstreamHits-before)
	}

	// Param validation mirrors /geo/reverse.
	if code, _ := get("/geo/address?lat=abc&lon=1"); code != 400 {
		t.Errorf("bad lat status = %d, want 400", code)
	}
}

func TestHandleEventsGeoJSON(t *testing.T) {
	st := newTestStore(t)
	s := &Server{cities: newCatalog(t), store: st}

	locatedEvent(t, st, "g1", "lisbon", 38.72, -9.13)
	// Event without coordinates must be excluded.
	noCoords := model.Event{
		ID: model.MakeID(model.SourceLuma, "g2"), Source: model.SourceLuma, SourceID: "g2",
		Title: "Unlocated", Category: model.CategoryMusic,
		StartsAt: time.Now().UTC().Add(24 * time.Hour),
		City:     "lisbon", CityID: "lisbon", Country: "PT",
		URL: "https://example.com/g2", ScrapedAt: time.Now().UTC(),
	}
	if err := st.UpsertEvents(context.Background(), []model.Event{noCoords}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	rec := httptest.NewRecorder()
	s.handleEventsGeoJSON(rec, httptest.NewRequest("GET", "/events.geojson", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/geo+json") {
		t.Errorf("content-type = %q", ct)
	}
	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Type     string `json:"type"`
			Geometry struct {
				Type        string     `json:"type"`
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 1 {
		t.Fatalf("type=%q features=%d, want FeatureCollection with 1", fc.Type, len(fc.Features))
	}
	f := fc.Features[0]
	// GeoJSON is [lon, lat] — the classic transposition bug.
	if f.Geometry.Coordinates[0] != -9.13 || f.Geometry.Coordinates[1] != 38.72 {
		t.Errorf("coordinates = %v, want [-9.13 38.72] (lon first)", f.Geometry.Coordinates)
	}
	for _, key := range []string{"id", "title", "category", "startsAt", "source", "venueName", "city", "country", "url", "free"} {
		if _, ok := f.Properties[key]; !ok {
			t.Errorf("properties missing %q", key)
		}
	}
}

func TestHandleCreateEvent(t *testing.T) {
	st := newTestStore(t)
	s := &Server{cities: newCatalog(t), store: st}

	post := func(body string) (int, model.Event) {
		rec := httptest.NewRecorder()
		s.handleCreateEvent(rec, httptest.NewRequest("POST", "/events", strings.NewReader(body)))
		var resp struct {
			Data model.Event `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec.Code, resp.Data
	}

	// A valid event is stored under the manual source and echoed back.
	code, ev := post(`{"title":"My Gig","category":"music","startsAt":"2099-08-01T20:00:00Z","cityId":"lisbon","venueName":"Coliseu","lat":38.72,"lon":-9.14}`)
	if code != 201 {
		t.Fatalf("status = %d, want 201", code)
	}
	if ev.Source != model.SourceManual || ev.ID == "" || ev.CityID != "lisbon" || ev.Country != "PT" {
		t.Fatalf("stored event = %+v", ev)
	}

	// It appears in a subsequent source-filtered query, image-less notwithstanding.
	got, _, _, _ := s.store.Query(context.Background(), store.Query{Source: model.SourceManual, RequireImage: true})
	if len(got) != 1 || got[0].ID != ev.ID {
		t.Errorf("manual query returned %d events, want the created one", len(got))
	}

	// Validation failures are 400s.
	for _, body := range []string{
		`{"category":"music","startsAt":"2099-08-01T20:00:00Z","cityId":"lisbon"}`,        // no title
		`{"title":"x","category":"food","startsAt":"2099-08-01T20:00:00Z","cityId":"lisbon"}`, // bad category
		`{"title":"x","category":"music","cityId":"lisbon"}`,                              // no startsAt
		`{"title":"x","category":"music","startsAt":"2099-08-01T20:00:00Z","cityId":"nowhere"}`, // unknown city
		`not json`,
	} {
		if code, _ := post(body); code != 400 {
			t.Errorf("body %q: status = %d, want 400", body, code)
		}
	}
}

func TestHandleGeoSearch(t *testing.T) {
	nominatim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"display_name":"Coliseu dos Recreios, Lisboa","lat":"38.7166","lon":"-9.1399"}]`))
	}))
	defer nominatim.Close()

	s := &Server{geocoder: &geocode.Client{
		HTTP: nominatim.Client(), BaseURL: nominatim.URL,
		MinInterval: time.Millisecond, MaxWait: time.Second,
	}}

	rec := httptest.NewRecorder()
	s.handleGeoSearch(rec, httptest.NewRequest("GET", "/geo/search?q=coliseu", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var resp struct {
		Data []geocode.SearchResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].Lat != 38.7166 || resp.Data[0].Lon != -9.1399 {
		t.Errorf("results = %+v", resp.Data)
	}

	// Empty query is a 400.
	rec = httptest.NewRecorder()
	s.handleGeoSearch(rec, httptest.NewRequest("GET", "/geo/search?q=", nil))
	if rec.Code != 400 {
		t.Errorf("empty q status = %d, want 400", rec.Code)
	}
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
	if !q.From.IsZero() {
		t.Errorf("default From should be zero (hide-ended filter is used instead), got %v", q.From)
	}
	// Without an explicit from, already-finished events are hidden.
	if q.NotEndedBefore.IsZero() {
		t.Errorf("default NotEndedBefore should be set to now")
	}
	if d := time.Since(q.NotEndedBefore); d < 0 || d > time.Minute {
		t.Errorf("default NotEndedBefore not ~now: %v", q.NotEndedBefore)
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
	if q.CityID != "lisbon" {
		t.Errorf("q.CityID = %q, want lisbon (catalog id; venue spellings must not fragment the feed)", q.CityID)
	}
	if q.City != "" {
		t.Errorf("q.City = %q, want empty (display-city match is only for free-text MCP lookups)", q.City)
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
	// An explicit from opts into history: no hide-ended filter.
	if !q.NotEndedBefore.IsZero() {
		t.Errorf("NotEndedBefore should be zero with explicit from, got %v", q.NotEndedBefore)
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
