package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

func newTestStore(t *testing.T) *SQLite {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func sampleEvent(id, title, city string, cat model.Category, src model.Source, starts time.Time, image string) model.Event {
	return model.Event{
		ID:        model.MakeID(src, id),
		Source:    src,
		SourceID:  id,
		Title:     title,
		Category:  cat,
		StartsAt:  starts,
		City:      city,
		Country:   "PT",
		URL:       "https://example.com/" + id,
		ImageURL:  image,
		ScrapedAt: starts.Add(-time.Hour),
	}
}

func TestUpsertAndGetEvent(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	starts := time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC)
	ev := sampleEvent("a1", "Concert", "Lisbon", model.CategoryMusic, model.SourceLuma, starts, "https://img/cover.jpg")

	if err := st.UpsertEvents(ctx, []model.Event{ev}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	got, ok, err := st.GetEvent(ctx, ev.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if !ok {
		t.Fatal("GetEvent should find inserted event")
	}
	if got.Title != "Concert" || got.City != "Lisbon" || got.ImageURL != "https://img/cover.jpg" {
		t.Errorf("got = %+v", got)
	}

	// Upsert again with a changed title — must overwrite, not duplicate.
	ev.Title = "Concert Redux"
	if err := st.UpsertEvents(ctx, []model.Event{ev}); err != nil {
		t.Fatalf("UpsertEvents reapply: %v", err)
	}
	got, _, _ = st.GetEvent(ctx, ev.ID)
	if got.Title != "Concert Redux" {
		t.Errorf("title after upsert = %q, want Concert Redux", got.Title)
	}

	// Missing ID returns not found, no error.
	_, ok, err = st.GetEvent(ctx, "deadbeef")
	if err != nil || ok {
		t.Errorf("GetEvent missing: ok=%v err=%v", ok, err)
	}
}

func TestUpsertEventsEmptyNoop(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpsertEvents(context.Background(), nil); err != nil {
		t.Errorf("empty upsert err: %v", err)
	}
}

func TestQueryFiltersAndPagination(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	events := []model.Event{
		sampleEvent("m1", "Jazz Night", "Lisbon", model.CategoryMusic, model.SourceLuma, base.Add(1*time.Hour), "https://img/1"),
		sampleEvent("m2", "Rock Show", "Lisbon", model.CategoryMusic, model.SourceSongkick, base.Add(2*time.Hour), "https://img/2"),
		sampleEvent("t1", "Go Meetup", "Porto", model.CategoryTech, model.SourceLuma, base.Add(3*time.Hour), "https://img/3"),
		sampleEvent("t2", "AI Conf", "Lisbon", model.CategoryTech, model.SourceLuma, base.Add(4*time.Hour), ""), // no image
	}
	if err := st.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	t.Run("city filter", func(t *testing.T) {
		got, total, _, err := st.Query(ctx, Query{City: "Lisbon", RequireImage: false})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if total != 3 {
			t.Errorf("total Lisbon = %d, want 3", total)
		}
		if len(got) != 3 {
			t.Errorf("got %d rows, want 3", len(got))
		}
		// ordered by starts_at ascending
		if !(got[0].StartsAt.Before(got[1].StartsAt) && got[1].StartsAt.Before(got[2].StartsAt)) {
			t.Errorf("rows not ordered ascending by starts_at")
		}
	})

	t.Run("category filter", func(t *testing.T) {
		_, total, _, _ := st.Query(ctx, Query{Category: model.CategoryMusic, RequireImage: false})
		if total != 2 {
			t.Errorf("total music = %d, want 2", total)
		}
	})

	t.Run("source filter", func(t *testing.T) {
		_, total, _, _ := st.Query(ctx, Query{Source: model.SourceSongkick, RequireImage: false})
		if total != 1 {
			t.Errorf("total songkick = %d, want 1", total)
		}
	})

	t.Run("date range", func(t *testing.T) {
		_, total, _, _ := st.Query(ctx, Query{
			From: base.Add(2 * time.Hour),
			To:   base.Add(3 * time.Hour),
		})
		if total != 2 {
			t.Errorf("total in [2h,3h] = %d, want 2 (m2 & t1)", total)
		}
	})

	t.Run("search", func(t *testing.T) {
		_, total, _, _ := st.Query(ctx, Query{Search: "JAZZ"})
		if total != 1 {
			t.Errorf("search jazz = %d, want 1", total)
		}
	})

	t.Run("require image hides empty", func(t *testing.T) {
		_, total, _, _ := st.Query(ctx, Query{RequireImage: true})
		if total != 3 {
			t.Errorf("require image total = %d, want 3 (t2 hidden)", total)
		}
	})

	t.Run("limit and offset", func(t *testing.T) {
		got, total, _, _ := st.Query(ctx, Query{Limit: 2, Offset: 0})
		if total != 4 {
			t.Errorf("total = %d, want 4", total)
		}
		if len(got) != 2 {
			t.Errorf("got %d rows, want 2", len(got))
		}
		second, _, _, _ := st.Query(ctx, Query{Limit: 2, Offset: 2})
		if len(second) != 2 {
			t.Errorf("page2 got %d, want 2", len(second))
		}
		if got[0].ID == second[0].ID {
			t.Errorf("page2 should not start with page1[0]")
		}
	})

	t.Run("max scraped is returned", func(t *testing.T) {
		_, _, maxT, _ := st.Query(ctx, Query{})
		if maxT.IsZero() {
			t.Errorf("expected non-zero max scraped time")
		}
	})
}

func TestQueryNotEndedBefore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	withEnd := func(e model.Event, ends time.Time) model.Event {
		e.EndsAt = &ends
		return e
	}
	events := []model.Event{
		// Finished yesterday → hidden.
		withEnd(sampleEvent("done", "Finished Gig", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-30*time.Hour), "https://img/a"), now.Add(-26*time.Hour)),
		// Multi-day festival: started 2 days ago, ends tomorrow → shown.
		withEnd(sampleEvent("fest", "Festival", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-48*time.Hour), "https://img/b"), now.Add(24*time.Hour)),
		// No end time, started 1h ago → within grace window, shown.
		sampleEvent("live", "Live Now", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-time.Hour), "https://img/c"),
		// No end time, started yesterday → past grace window, hidden.
		sampleEvent("old", "Old Show", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-24*time.Hour), "https://img/d"),
		// Starts tomorrow → shown.
		sampleEvent("next", "Tomorrow", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(24*time.Hour), "https://img/e"),
	}
	if err := st.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	got, total, _, err := st.Query(ctx, Query{NotEndedBefore: now})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (fest, live, next)", total)
	}
	titles := make([]string, len(got))
	for i, e := range got {
		titles[i] = e.Title
	}
	// Still sorted by start date ascending: ongoing first, then upcoming.
	want := []string{"Festival", "Live Now", "Tomorrow"}
	for i := range want {
		if i >= len(titles) || titles[i] != want[i] {
			t.Fatalf("titles = %v, want %v", titles, want)
		}
	}

	// Without the filter every event is returned.
	_, total, _, _ = st.Query(ctx, Query{})
	if total != 5 {
		t.Errorf("unfiltered total = %d, want 5", total)
	}
}

func TestQueryEmptyMaxScraped(t *testing.T) {
	st := newTestStore(t)
	_, total, maxT, err := st.Query(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 0 {
		t.Errorf("empty total = %d, want 0", total)
	}
	if !maxT.IsZero() {
		t.Errorf("empty maxT = %v, want zero", maxT)
	}
}

func TestMarkScrapeAndGetScrape(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	stt := ScrapeStatus{
		Source:     model.SourceLuma,
		CityID:     "lisbon",
		LastRunAt:  now,
		ExpiresAt:  now.Add(6 * time.Hour),
		Status:     "ok",
		ErrMessage: "",
	}
	if err := st.MarkScrape(ctx, stt); err != nil {
		t.Fatalf("MarkScrape: %v", err)
	}
	got, ok, err := st.GetScrape(ctx, model.SourceLuma, "lisbon")
	if err != nil || !ok {
		t.Fatalf("GetScrape: ok=%v err=%v", ok, err)
	}
	if !got.LastRunAt.Equal(now) || got.Status != "ok" {
		t.Errorf("got = %+v", got)
	}

	// Upsert with new status.
	stt2 := stt
	stt2.Status = "error"
	stt2.ErrMessage = "boom"
	stt2.LastRunAt = now.Add(time.Hour)
	if err := st.MarkScrape(ctx, stt2); err != nil {
		t.Fatalf("MarkScrape 2: %v", err)
	}
	got, _, _ = st.GetScrape(ctx, model.SourceLuma, "lisbon")
	if got.Status != "error" || got.ErrMessage != "boom" {
		t.Errorf("after update got = %+v", got)
	}

	// Miss.
	_, ok, _ = st.GetScrape(ctx, model.SourceLuma, "nowhere")
	if ok {
		t.Error("expected miss for unknown city")
	}
}

func TestAllScrapes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for i, c := range []string{"lisbon", "porto", "berlin"} {
		s := ScrapeStatus{
			Source:    model.SourceLuma,
			CityID:    c,
			LastRunAt: now.Add(time.Duration(i) * time.Minute),
			ExpiresAt: now.Add(time.Hour),
			Status:    "ok",
		}
		if err := st.MarkScrape(ctx, s); err != nil {
			t.Fatalf("MarkScrape: %v", err)
		}
	}
	all, err := st.AllScrapes(ctx)
	if err != nil {
		t.Fatalf("AllScrapes: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all len = %d, want 3", len(all))
	}
	// Ordered last_run_at DESC.
	if !(all[0].LastRunAt.After(all[1].LastRunAt) && all[1].LastRunAt.After(all[2].LastRunAt)) {
		t.Errorf("AllScrapes not ordered DESC: %+v", all)
	}
}

func TestClearImageURLsMatching(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	events := []model.Event{
		sampleEvent("a", "x", "Lisbon", model.CategoryMusic, model.SourceLuma, base, "https://cdn.songkick.com/default-event.png"),
		sampleEvent("b", "y", "Lisbon", model.CategoryMusic, model.SourceLuma, base, "https://cdn.example.com/real.png"),
	}
	if err := st.UpsertEvents(ctx, events); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}
	n, err := st.ClearImageURLsMatching(ctx, []string{"%default-event%"})
	if err != nil {
		t.Fatalf("ClearImageURLsMatching: %v", err)
	}
	if n != 1 {
		t.Errorf("cleared rows = %d, want 1", n)
	}
	gotA, _, _ := st.GetEvent(ctx, events[0].ID)
	gotB, _, _ := st.GetEvent(ctx, events[1].ID)
	if gotA.ImageURL != "" {
		t.Errorf("A image should be cleared, got %q", gotA.ImageURL)
	}
	if gotB.ImageURL == "" {
		t.Errorf("B image should be untouched, got empty")
	}

	// Empty pattern slice is a no-op.
	if n, err := st.ClearImageURLsMatching(ctx, nil); err != nil || n != 0 {
		t.Errorf("empty patterns: n=%d err=%v", n, err)
	}
}

// Regression: events are stored with the venue's own locality in City
// ("Lisboa", "Carnaxide"), so a feed keyed on the catalog name missed most
// of a city's events. Queries must go through CityID instead.
func TestQueryByCityID(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC)

	lisboa := sampleEvent("v1", "Fado Night", "Lisboa", model.CategoryMusic, model.SourceViralagenda, base, "https://img/1.jpg")
	lisboa.CityID = "lisbon"
	carnaxide := sampleEvent("v2", "Teatro", "Carnaxide", model.CategoryArts, model.SourceViralagenda, base, "https://img/2.jpg")
	carnaxide.CityID = "lisbon"
	porto := sampleEvent("v3", "Indie Gig", "Porto", model.CategoryMusic, model.SourceViralagenda, base, "https://img/3.jpg")
	porto.CityID = "porto"

	if err := st.UpsertEvents(ctx, []model.Event{lisboa, carnaxide, porto}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	got, total, _, err := st.Query(ctx, Query{CityID: "lisbon"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("lisbon: total=%d len=%d, want 2 (venue spellings must not fragment the feed)", total, len(got))
	}
	for _, e := range got {
		if e.CityID != "lisbon" {
			t.Errorf("event %q cityId = %q, want lisbon", e.Title, e.CityID)
		}
	}

	// Display-city match still works for free-text lookups.
	got, _, _, err = st.Query(ctx, Query{City: "Carnaxide"})
	if err != nil {
		t.Fatalf("Query by City: %v", err)
	}
	if len(got) != 1 || got[0].Title != "Teatro" {
		t.Errorf("City=Carnaxide got %d events", len(got))
	}
}

// A database created before the city_id column existed must be migrated in
// place by Init.
func TestInitMigratesCityID(t *testing.T) {
	dir := t.TempDir()
	st, err := NewSQLite(dir + "/old.db")
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	defer st.Close()
	ctx := context.Background()

	// Recreate the pre-city_id schema by hand.
	if _, err := st.db.ExecContext(ctx, `
		CREATE TABLE events (
			id TEXT PRIMARY KEY, source TEXT NOT NULL, source_id TEXT NOT NULL,
			title TEXT NOT NULL, category TEXT NOT NULL, starts_at INTEGER NOT NULL,
			ends_at INTEGER, city TEXT NOT NULL, country TEXT NOT NULL,
			url TEXT NOT NULL, payload TEXT NOT NULL, scraped_at INTEGER NOT NULL
		)`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	if err := st.Init(ctx); err != nil {
		t.Fatalf("Init on legacy db: %v", err)
	}

	ev := sampleEvent("m1", "Migrated", "Lisboa", model.CategoryMusic, model.SourceLuma, time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC), "")
	ev.CityID = "lisbon"
	if err := st.UpsertEvents(ctx, []model.Event{ev}); err != nil {
		t.Fatalf("UpsertEvents after migration: %v", err)
	}
	got, total, _, err := st.Query(ctx, Query{CityID: "lisbon"})
	if err != nil || total != 1 || len(got) != 1 {
		t.Fatalf("query after migration: total=%d len=%d err=%v", total, len(got), err)
	}
}
