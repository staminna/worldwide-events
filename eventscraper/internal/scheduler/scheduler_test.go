package scheduler

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

type fakeScraper struct {
	src     model.Source
	calls   int32
	events  []model.Event
	err     error
	respond chan struct{} // optional, if non-nil scrape blocks until signaled
}

func (f *fakeScraper) Source() model.Source { return f.src }
func (f *fakeScraper) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.respond != nil {
		<-f.respond
	}
	return f.events, f.err
}

func newSchedulerWithStore(t *testing.T) (*Scheduler, *scraper.Registry, store.Store) {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reg := scraper.NewRegistry()
	sch := New(st, reg, nil)
	return sch, reg, st
}

func TestRunUnknownSourceIsNoop(t *testing.T) {
	sch, _, _ := newSchedulerWithStore(t)
	// Nothing registered → Run should return without panicking.
	sch.Run(context.Background(), model.SourceLuma, geo.City{ID: "lisbon", Name: "Lisbon"}, model.AllCategories())
}

func TestRunPersistsEventsAndMarksOK(t *testing.T) {
	sch, reg, st := newSchedulerWithStore(t)
	ev := model.Event{
		ID:        model.MakeID(model.SourceLuma, "x1"),
		Source:    model.SourceLuma,
		SourceID:  "x1",
		Title:     "Test",
		Category:  model.CategoryMusic,
		StartsAt:  time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC),
		City:      "Lisbon",
		Country:   "PT",
		URL:       "https://lu.ma/x1",
		ImageURL:  "https://img/cover.jpg",
		ScrapedAt: time.Now().UTC(),
	}
	fs := &fakeScraper{src: model.SourceLuma, events: []model.Event{ev}}
	reg.Register(fs)

	sch.Run(context.Background(), model.SourceLuma,
		geo.City{ID: "lisbon", Name: "Lisbon"}, []model.Category{model.CategoryMusic})

	if atomic.LoadInt32(&fs.calls) != 1 {
		t.Errorf("scraper called %d times, want 1", fs.calls)
	}
	got, ok, err := st.GetEvent(context.Background(), ev.ID)
	if err != nil || !ok {
		t.Fatalf("event not persisted: ok=%v err=%v", ok, err)
	}
	if got.Title != "Test" {
		t.Errorf("persisted title = %q", got.Title)
	}
	stt, ok, err := st.GetScrape(context.Background(), model.SourceLuma, "lisbon")
	if err != nil || !ok {
		t.Fatalf("scrape status not persisted: ok=%v err=%v", ok, err)
	}
	if stt.Status != "ok" {
		t.Errorf("status = %q, want ok", stt.Status)
	}
	// Music category TTL is 12h.
	if stt.ExpiresAt.Sub(stt.LastRunAt) != 12*time.Hour {
		t.Errorf("expires-last = %v, want 12h", stt.ExpiresAt.Sub(stt.LastRunAt))
	}
}

func TestRunRecordsErrorStatus(t *testing.T) {
	sch, reg, st := newSchedulerWithStore(t)
	fs := &fakeScraper{src: model.SourceLuma, err: scraper.ErrBlocked}
	reg.Register(fs)

	sch.Run(context.Background(), model.SourceLuma,
		geo.City{ID: "lisbon", Name: "Lisbon"}, []model.Category{model.CategoryTech})

	stt, ok, _ := st.GetScrape(context.Background(), model.SourceLuma, "lisbon")
	if !ok {
		t.Fatal("scrape status missing")
	}
	if stt.Status != "blocked" {
		t.Errorf("status = %q, want blocked", stt.Status)
	}
	if stt.ErrMessage == "" {
		t.Errorf("ErrMessage should be set")
	}
}

func TestRunUnconfiguredStatus(t *testing.T) {
	sch, reg, st := newSchedulerWithStore(t)
	fs := &fakeScraper{src: model.SourceTicketmaster, err: scraper.ErrUnconfigured}
	reg.Register(fs)

	sch.Run(context.Background(), model.SourceTicketmaster,
		geo.City{ID: "lisbon", Name: "Lisbon"}, model.AllCategories())

	stt, _, _ := st.GetScrape(context.Background(), model.SourceTicketmaster, "lisbon")
	if stt.Status != "unconfigured" {
		t.Errorf("status = %q, want unconfigured", stt.Status)
	}
}

func TestMaybeRefreshSkipsIfFresh(t *testing.T) {
	sch, reg, st := newSchedulerWithStore(t)
	now := time.Now().UTC()
	// Pre-seed a fresh scrape so MaybeRefresh should be a no-op.
	_ = st.MarkScrape(context.Background(), store.ScrapeStatus{
		Source:    model.SourceLuma,
		CityID:    "lisbon",
		LastRunAt: now,
		ExpiresAt: now.Add(2 * time.Hour),
		Status:    "ok",
	})
	fs := &fakeScraper{src: model.SourceLuma}
	reg.Register(fs)

	sch.MaybeRefresh(model.SourceLuma, geo.City{ID: "lisbon", Name: "Lisbon"}, model.AllCategories())
	// Goroutine fires in background; give it time to either run or skip.
	time.Sleep(50 * time.Millisecond)

	if got := atomic.LoadInt32(&fs.calls); got != 0 {
		t.Errorf("MaybeRefresh fired %d scrapes despite fresh entry, want 0", got)
	}
}

func TestMaybeRefreshFiresIfStale(t *testing.T) {
	sch, reg, st := newSchedulerWithStore(t)
	now := time.Now().UTC()
	_ = st.MarkScrape(context.Background(), store.ScrapeStatus{
		Source:    model.SourceLuma,
		CityID:    "lisbon",
		LastRunAt: now.Add(-24 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour),
		Status:    "ok",
	})
	fs := &fakeScraper{src: model.SourceLuma}
	reg.Register(fs)

	sch.MaybeRefresh(model.SourceLuma, geo.City{ID: "lisbon", Name: "Lisbon"}, []model.Category{model.CategoryTech})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&fs.calls) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("MaybeRefresh did not fire scrape within 2s, calls=%d", fs.calls)
}
