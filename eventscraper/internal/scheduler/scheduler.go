package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"time"

	"github.com/jorgenunes/eventscraper/internal/cache"
	"github.com/jorgenunes/eventscraper/internal/enrich"
	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

type Scheduler struct {
	store    store.Store
	registry *scraper.Registry
	cities   *geo.Catalog
	single   *cache.SingleFlight
	enricher *enrich.Enricher
}

func New(st store.Store, reg *scraper.Registry, cat *geo.Catalog) *Scheduler {
	return &Scheduler{
		store:    st,
		registry: reg,
		cities:   cat,
		single:   cache.NewSingleFlight(),
		enricher: enrich.New(),
	}
}

// Run executes a scrape for (src, city) covering the given categories,
// deduplicated across concurrent callers. Errors are recorded but not returned.
func (s *Scheduler) Run(ctx context.Context, src model.Source, city geo.City, cats []model.Category) {
	sc, ok := s.registry.Get(src)
	if !ok {
		return
	}
	if len(cats) == 0 {
		cats = model.AllCategories()
	}
	key := string(src) + "|" + city.ID
	_ = s.single.Do(ctx, key, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		events, err := sc.Scrape(ctx, city, cats)
		now := time.Now().UTC()
		var ttl time.Duration
		for _, c := range cats {
			if t := cache.TTL(c); t > ttl {
				ttl = t
			}
		}
		status := "ok"
		errMsg := ""
		if err != nil {
			if errors.Is(err, scraper.ErrUnconfigured) {
				status = "unconfigured"
			} else if errors.Is(err, scraper.ErrBlocked) {
				status = "blocked"
			} else {
				status = "error"
			}
			errMsg = err.Error()
		}
		if len(events) > 0 {
			// Backfill missing images via og:image / twitter:image.
			s.enricher.BackfillImages(ctx, events)
			if upErr := s.store.UpsertEvents(ctx, events); upErr != nil {
				slog.Error("upsert events", "src", src, "city", city.ID, "err", upErr)
			}
		}
		_ = s.store.MarkScrape(ctx, store.ScrapeStatus{
			Source:     src,
			CityID:     city.ID,
			LastRunAt:  now,
			ExpiresAt:  now.Add(ttl),
			Status:     status,
			ErrMessage: errMsg,
		})
		slog.Info("scrape complete",
			"src", src, "city", city.ID, "count", len(events), "status", status,
		)
		return nil
	})
}

// MaybeRefresh fires a background scrape for the (src, city) if the cached
// entry is stale or missing. Always returns immediately.
func (s *Scheduler) MaybeRefresh(src model.Source, city geo.City, cats []model.Category) {
	sc, ok := s.registry.Get(src)
	if !ok {
		return
	}
	go func() {
		ctx := context.Background()
		st, ok, err := s.store.GetScrape(ctx, sc.Source(), city.ID)
		if err == nil && ok && time.Now().Before(st.ExpiresAt) {
			return
		}
		s.Run(ctx, src, city, cats)
	}()
}

// Warmup runs scrapes across the first `cityLimit` cities and all registered
// sources, serialised with a small jitter so we don't hammer any host. It
// honours existing TTL entries — restarting the server within a TTL window
// does no extra work.
func (s *Scheduler) Warmup(ctx context.Context, cityLimit int) {
	cities := s.cities.All()
	if cityLimit > 0 && cityLimit < len(cities) {
		cities = cities[:cityLimit]
	}
	scrapers := s.registry.All()
	slog.Info("warmup starting", "cities", len(cities), "sources", len(scrapers))
	for _, sc := range scrapers {
		for _, city := range cities {
			select {
			case <-ctx.Done():
				return
			default:
			}
			st, ok, err := s.store.GetScrape(ctx, sc.Source(), city.ID)
			if err == nil && ok && time.Now().Before(st.ExpiresAt) {
				continue
			}
			s.Run(ctx, sc.Source(), city, model.AllCategories())
			// brief jitter so we don't fire requests in lockstep
			time.Sleep(time.Duration(200+rand.Intn(300)) * time.Millisecond)
		}
	}
	slog.Info("warmup complete")
}
