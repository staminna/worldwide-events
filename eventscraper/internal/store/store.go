package store

import (
	"context"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

type Query struct {
	// CityID matches the catalog city an event was scraped for (exact,
	// e.g. "lisbon"). This is what the API and CLI filter on.
	CityID string
	// City matches the stored display city — the venue's own locality
	// string (e.g. "Carnaxide"). Used by the MCP server for free-text
	// city input that doesn't resolve to a catalog entry.
	City     string
	Category model.Category
	Source   model.Source
	From     time.Time
	To       time.Time
	// NotEndedBefore, when non-zero, hides events that already finished by
	// that instant: an event is kept while its ends_at (or, lacking one,
	// starts_at plus a grace window) is still in the future.
	NotEndedBefore time.Time
	Search         string
	Limit          int
	Offset         int
	RequireImage   bool
}

type ScrapeStatus struct {
	Source     model.Source
	CityID     string
	LastRunAt  time.Time
	ExpiresAt  time.Time
	Status     string
	ErrMessage string
}

type Store interface {
	Init(ctx context.Context) error
	UpsertEvents(ctx context.Context, events []model.Event) error
	GetEvent(ctx context.Context, id string) (model.Event, bool, error)
	Query(ctx context.Context, q Query) ([]model.Event, int, time.Time, error)
	MarkScrape(ctx context.Context, s ScrapeStatus) error
	GetScrape(ctx context.Context, src model.Source, cityID string) (ScrapeStatus, bool, error)
	AllScrapes(ctx context.Context) ([]ScrapeStatus, error)
	// ClearImageURLsMatching strips imageUrl from any stored payload whose
	// JSON imageUrl field matches the given LIKE-style patterns. Returns the
	// number of rows updated.
	ClearImageURLsMatching(ctx context.Context, patterns []string) (int, error)
	Close() error
}
