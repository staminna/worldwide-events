package scraper

import (
	"context"
	"errors"
	"sync"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

var (
	ErrUnconfigured = errors.New("scraper not configured (missing credentials)")
	ErrBlocked      = errors.New("scraper blocked by upstream (CAPTCHA or 403)")
)

type Scraper interface {
	Source() model.Source
	Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error)
}

type Registry struct {
	mu       sync.RWMutex
	scrapers map[model.Source]Scraper
}

func NewRegistry() *Registry {
	return &Registry{scrapers: map[model.Source]Scraper{}}
}

func (r *Registry) Register(s Scraper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scrapers[s.Source()] = s
}

func (r *Registry) Get(src model.Source) (Scraper, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.scrapers[src]
	return s, ok
}

func (r *Registry) All() []Scraper {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Scraper, 0, len(r.scrapers))
	for _, s := range r.scrapers {
		out = append(out, s)
	}
	return out
}
