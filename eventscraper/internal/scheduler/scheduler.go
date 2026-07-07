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
	"github.com/jorgenunes/eventscraper/internal/geocode"
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
			// Stamp the catalog city this scrape ran for. Scrapers keep the
			// venue's own locality in City ("Lisboa", "Carnaxide"), so this
			// ID is the only reliable per-city query key.
			for i := range events {
				events[i].CityID = city.ID
			}
			// Backfill missing images via og:image / twitter:image.
			s.enricher.BackfillImages(ctx, events)
			// Re-apply street addresses already resolved for these venues.
			// Upserts replace the payload wholesale, so without this a
			// /geo/address patch would be wiped on the next scrape. Cache
			// lookups only — never the network.
			s.backfillAddresses(ctx, events)
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

// backfillAddresses fills empty venue addresses from the geo_addresses
// cache for events that carry coordinates.
func (s *Scheduler) backfillAddresses(ctx context.Context, events []model.Event) {
	for i := range events {
		v := &events[i].Venue
		if v.Address != "" || (v.Lat == 0 && v.Lon == 0) {
			continue
		}
		addr, _, found, err := s.store.GetGeoAddress(ctx, geocode.Key(v.Lat, v.Lon))
		if err == nil && found && addr != "" {
			v.Address = addr
		}
	}
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
