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
	// RequireCoords keeps only events whose venue has coordinates (used by
	// the GeoJSON export, where unlocated events are useless).
	RequireCoords bool
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
	// CountLocatedUpcoming counts a city's events that have venue
	// coordinates and haven't ended by the given instant.
	CountLocatedUpcoming(ctx context.Context, cityID string, notEndedBefore time.Time) (int, error)
	// GetGeoAddress / PutGeoAddress cache reverse-geocoded street addresses
	// by rounded-coordinate key. An empty stored address is a negative-cache
	// entry ("looked up, nothing there"); resolvedAt lets callers decide
	// when a negative is stale enough to retry.
	GetGeoAddress(ctx context.Context, key string) (addr string, resolvedAt time.Time, found bool, err error)
	PutGeoAddress(ctx context.Context, key, addr string) error
	// SetVenueAddressIfEmpty patches the stored event's venue.address only
	// when it is currently empty. Returns whether a row was changed.
	SetVenueAddressIfEmpty(ctx context.Context, eventID, addr string) (bool, error)
	Close() error
}
