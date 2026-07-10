package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jorgenunes/eventscraper/internal/cache"
	"github.com/jorgenunes/eventscraper/internal/config"
	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/geocode"
	"github.com/jorgenunes/eventscraper/internal/scheduler"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

type Server struct {
	cfg       config.Config
	store     store.Store
	cities    *geo.Catalog
	registry  *scraper.Registry
	scheduler *scheduler.Scheduler
	geocoder  *geocode.Client
	geoSF     *cache.SingleFlight
}

func NewServer(cfg config.Config, st store.Store, cities *geo.Catalog, reg *scraper.Registry, sch *scheduler.Scheduler) *Server {
	return &Server{
		cfg:       cfg,
		store:     st,
		cities:    cities,
		registry:  reg,
		scheduler: sch,
		geocoder:  geocode.New(),
		geoSF:     cache.NewSingleFlight(),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(corsMiddleware(s.cfg.AllowedOrigin))

	r.Get("/healthz", s.handleHealthz)
	r.Get("/cities", s.handleCities)
	r.Get("/geo/reverse", s.handleGeoReverse)
	r.Get("/geo/address", s.handleGeoAddress)
	r.Get("/geo/search", s.handleGeoSearch)
	r.Get("/events.geojson", s.handleEventsGeoJSON)
	r.Get("/viz", s.handleViz)
	// Ops surface — private, gated by ADMIN_TOKEN (see requireAdmin).
	r.Get("/runs", s.requireAdmin(s.handleRuns))
	r.Get("/runs.json", s.requireAdmin(s.handleRunsJSON))
	r.Get("/sources", s.handleSources)
	r.Get("/events", s.handleEvents)
	r.Get("/events/{id}", s.handleEvent)
	r.Post("/events", s.handleCreateEvent)
	r.Post("/upload", s.handleUpload)
	r.Get("/uploads/{name}", s.handleUploadServe)
	r.Get("/img", s.handleImg)
	r.Post("/refresh", s.requireAdmin(s.handleRefresh))
	return r
}

// requireAdmin gates a handler behind ADMIN_TOKEN. When the token is unset
// (local dev) the gate is open, matching the historical /refresh behavior.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AdminToken != "" {
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if tok != s.cfg.AdminToken {
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}
		next(w, r)
	}
}

func (s *Server) Serve(ctx context.Context) error {
	srv := &http.Server{
		Addr:         ":" + s.cfg.Port,
		Handler:      s.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 35 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	return srv.ListenAndServe()
}

func corsMiddleware(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match, Authorization")
			w.Header().Set("Access-Control-Expose-Headers", "ETag")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
