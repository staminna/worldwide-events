package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jorgenunes/eventscraper/internal/geo"
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
		q.City = c.Name
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
		// Default: only show events that haven't started yet (UTC day).
		now := time.Now().UTC()
		q.From = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
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
