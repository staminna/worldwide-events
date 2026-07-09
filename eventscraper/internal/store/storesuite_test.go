package store

import (
	"context"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

// runStoreSuite exercises the Store contract against a backend built fresh per
// scenario by newStore. Running it against both SQLite and Postgres is the
// parity guarantee that the two implementations behave identically.
func runStoreSuite(t *testing.T, newStore func(t *testing.T) Store) {
	base := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)

	t.Run("upsert/get/overwrite", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		ev := sampleEvent("a1", "Concert", "Lisbon", model.CategoryMusic, model.SourceLuma, base, "https://img/c.jpg")
		if err := st.UpsertEvents(ctx, []model.Event{ev}); err != nil {
			t.Fatalf("UpsertEvents: %v", err)
		}
		got, ok, err := st.GetEvent(ctx, ev.ID)
		if err != nil || !ok || got.Title != "Concert" || got.ImageURL != "https://img/c.jpg" {
			t.Fatalf("GetEvent: ok=%v err=%v got=%+v", ok, err, got)
		}
		ev.Title = "Concert Redux"
		if err := st.UpsertEvents(ctx, []model.Event{ev}); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}
		got, _, _ = st.GetEvent(ctx, ev.ID)
		if got.Title != "Concert Redux" {
			t.Errorf("title after upsert = %q", got.Title)
		}
		if _, ok, err := st.GetEvent(ctx, "deadbeef"); ok || err != nil {
			t.Errorf("missing id: ok=%v err=%v", ok, err)
		}
		if err := st.UpsertEvents(ctx, nil); err != nil {
			t.Errorf("empty upsert: %v", err)
		}
	})

	t.Run("query filters + pagination + maxScraped", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		events := []model.Event{
			sampleEvent("m1", "Jazz Night", "Lisbon", model.CategoryMusic, model.SourceLuma, base.Add(1*time.Hour), "https://img/1"),
			sampleEvent("m2", "Rock Show", "Lisbon", model.CategoryMusic, model.SourceSongkick, base.Add(2*time.Hour), "https://img/2"),
			sampleEvent("t1", "Go Meetup", "Porto", model.CategoryTech, model.SourceLuma, base.Add(3*time.Hour), "https://img/3"),
			sampleEvent("t2", "AI Conf", "Lisbon", model.CategoryTech, model.SourceLuma, base.Add(4*time.Hour), ""),
		}
		if err := st.UpsertEvents(ctx, events); err != nil {
			t.Fatalf("UpsertEvents: %v", err)
		}
		got, total, _, err := st.Query(ctx, Query{City: "Lisbon"})
		if err != nil || total != 3 || len(got) != 3 {
			t.Fatalf("city filter: total=%d len=%d err=%v", total, len(got), err)
		}
		if !(got[0].StartsAt.Before(got[1].StartsAt) && got[1].StartsAt.Before(got[2].StartsAt)) {
			t.Errorf("not ordered ascending by starts_at")
		}
		if _, total, _, _ := st.Query(ctx, Query{Category: model.CategoryMusic}); total != 2 {
			t.Errorf("category total = %d, want 2", total)
		}
		if _, total, _, _ := st.Query(ctx, Query{Source: model.SourceSongkick}); total != 1 {
			t.Errorf("source total = %d, want 1", total)
		}
		if _, total, _, _ := st.Query(ctx, Query{From: base.Add(2 * time.Hour), To: base.Add(3 * time.Hour)}); total != 2 {
			t.Errorf("date range total = %d, want 2", total)
		}
		if _, total, _, _ := st.Query(ctx, Query{Search: "JAZZ"}); total != 1 {
			t.Errorf("search total = %d, want 1", total)
		}
		if _, total, _, _ := st.Query(ctx, Query{RequireImage: true}); total != 3 {
			t.Errorf("require image total = %d, want 3", total)
		}
		p1, total, _, _ := st.Query(ctx, Query{Limit: 2, Offset: 0})
		p2, _, _, _ := st.Query(ctx, Query{Limit: 2, Offset: 2})
		if total != 4 || len(p1) != 2 || len(p2) != 2 || p1[0].ID == p2[0].ID {
			t.Errorf("pagination: total=%d p1=%d p2=%d overlap=%v", total, len(p1), len(p2), p1[0].ID == p2[0].ID)
		}
		if _, _, maxT, _ := st.Query(ctx, Query{}); maxT.IsZero() {
			t.Errorf("expected non-zero max scraped time")
		}
	})

	t.Run("empty store maxScraped is zero", func(t *testing.T) {
		st := newStore(t)
		_, total, maxT, err := st.Query(context.Background(), Query{})
		if err != nil || total != 0 || !maxT.IsZero() {
			t.Errorf("empty store: total=%d maxT=%v err=%v", total, maxT, err)
		}
	})

	t.Run("notEndedBefore grace window", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
		withEnd := func(e model.Event, ends time.Time) model.Event { e.EndsAt = &ends; return e }
		events := []model.Event{
			withEnd(sampleEvent("done", "Finished Gig", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-30*time.Hour), "https://img/a"), now.Add(-26*time.Hour)),
			withEnd(sampleEvent("fest", "Festival", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-48*time.Hour), "https://img/b"), now.Add(24*time.Hour)),
			sampleEvent("live", "Live Now", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-time.Hour), "https://img/c"),
			sampleEvent("old", "Old Show", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(-24*time.Hour), "https://img/d"),
			sampleEvent("next", "Tomorrow", "Lisbon", model.CategoryMusic, model.SourceLuma, now.Add(24*time.Hour), "https://img/e"),
		}
		if err := st.UpsertEvents(ctx, events); err != nil {
			t.Fatalf("UpsertEvents: %v", err)
		}
		got, total, _, err := st.Query(ctx, Query{NotEndedBefore: now})
		if err != nil || total != 3 {
			t.Fatalf("notEndedBefore total=%d err=%v", total, err)
		}
		want := []string{"Festival", "Live Now", "Tomorrow"}
		for i := range want {
			if i >= len(got) || got[i].Title != want[i] {
				t.Fatalf("titles = %v, want %v", got, want)
			}
		}
		if _, total, _, _ := st.Query(ctx, Query{}); total != 5 {
			t.Errorf("unfiltered total = %d, want 5", total)
		}
	})

	t.Run("scrape status roundtrip + order", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)
		if err := st.MarkScrape(ctx, ScrapeStatus{Source: model.SourceLuma, CityID: "lisbon", LastRunAt: now, ExpiresAt: now.Add(time.Hour), Status: "ok"}); err != nil {
			t.Fatalf("MarkScrape: %v", err)
		}
		got, ok, err := st.GetScrape(ctx, model.SourceLuma, "lisbon")
		if err != nil || !ok || !got.LastRunAt.Equal(now) || got.Status != "ok" {
			t.Fatalf("GetScrape: ok=%v err=%v got=%+v", ok, err, got)
		}
		if err := st.MarkScrape(ctx, ScrapeStatus{Source: model.SourceLuma, CityID: "lisbon", LastRunAt: now.Add(time.Hour), ExpiresAt: now.Add(2 * time.Hour), Status: "error", ErrMessage: "boom"}); err != nil {
			t.Fatalf("MarkScrape 2: %v", err)
		}
		got, _, _ = st.GetScrape(ctx, model.SourceLuma, "lisbon")
		if got.Status != "error" || got.ErrMessage != "boom" {
			t.Errorf("after update: %+v", got)
		}
		if _, ok, _ := st.GetScrape(ctx, model.SourceLuma, "nowhere"); ok {
			t.Error("expected miss for unknown city")
		}
		for i, c := range []string{"a", "b", "cc"} {
			_ = st.MarkScrape(ctx, ScrapeStatus{Source: model.SourceSongkick, CityID: c, LastRunAt: now.Add(time.Duration(i) * time.Minute), ExpiresAt: now.Add(time.Hour), Status: "ok"})
		}
		all, err := st.AllScrapes(ctx)
		if err != nil || len(all) != 4 {
			t.Fatalf("AllScrapes len=%d err=%v", len(all), err)
		}
		for i := 1; i < len(all); i++ {
			if all[i-1].LastRunAt.Before(all[i].LastRunAt) {
				t.Errorf("AllScrapes not ordered DESC")
			}
		}
	})

	t.Run("clearImageURLs (case-insensitive)", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		events := []model.Event{
			sampleEvent("a", "x", "Lisbon", model.CategoryMusic, model.SourceLuma, base, "https://cdn.songkick.com/DEFAULT-EVENT.png"), // uppercase → ILIKE must match
			sampleEvent("b", "y", "Lisbon", model.CategoryMusic, model.SourceLuma, base, "https://cdn.example.com/real.png"),
		}
		if err := st.UpsertEvents(ctx, events); err != nil {
			t.Fatalf("UpsertEvents: %v", err)
		}
		n, err := st.ClearImageURLsMatching(ctx, []string{"%default-event%"})
		if err != nil || n != 1 {
			t.Fatalf("clear: n=%d err=%v (case-insensitive match expected)", n, err)
		}
		gotA, _, _ := st.GetEvent(ctx, events[0].ID)
		gotB, _, _ := st.GetEvent(ctx, events[1].ID)
		if gotA.ImageURL != "" || gotB.ImageURL == "" {
			t.Errorf("A=%q B=%q", gotA.ImageURL, gotB.ImageURL)
		}
		if n, err := st.ClearImageURLsMatching(ctx, nil); err != nil || n != 0 {
			t.Errorf("empty patterns: n=%d err=%v", n, err)
		}
	})

	t.Run("setVenueAddressIfEmpty", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		blank := sampleEvent("noaddr", "No Address", "Lisboa", model.CategoryMusic, model.SourceLuma, base, "https://img/1.jpg")
		blank.Venue = model.Venue{Name: "LAV", Lat: 38.72, Lon: -9.1}
		kept := sampleEvent("hasaddr", "Has Address", "Lisboa", model.CategoryMusic, model.SourceLuma, base, "https://img/2.jpg")
		kept.Venue = model.Venue{Name: "Coliseu", Address: "Rua das Portas 96", Lat: 38.71, Lon: -9.14}
		if err := st.UpsertEvents(ctx, []model.Event{blank, kept}); err != nil {
			t.Fatalf("UpsertEvents: %v", err)
		}
		changed, err := st.SetVenueAddressIfEmpty(ctx, blank.ID, "Av. Infante D. Henrique, Lisboa")
		if err != nil || !changed {
			t.Fatalf("fill: changed=%v err=%v", changed, err)
		}
		got, _, _ := st.GetEvent(ctx, blank.ID)
		if got.Venue.Address != "Av. Infante D. Henrique, Lisboa" || got.Venue.Name != "LAV" || got.Venue.Lat != 38.72 {
			t.Errorf("patch disturbed venue: %+v", got.Venue)
		}
		if changed, _ := st.SetVenueAddressIfEmpty(ctx, kept.ID, "WRONG"); changed {
			t.Error("must not overwrite existing address")
		}
		if changed, err := st.SetVenueAddressIfEmpty(ctx, "deadbeef", "X"); err != nil || changed {
			t.Errorf("unknown id: changed=%v err=%v", changed, err)
		}
	})

	t.Run("geo address cache + negative cache", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		if _, _, found, err := st.GetGeoAddress(ctx, "k1"); found || err != nil {
			t.Fatalf("miss: found=%v err=%v", found, err)
		}
		if err := st.PutGeoAddress(ctx, "k1", "Rua Augusta 1"); err != nil {
			t.Fatalf("put: %v", err)
		}
		addr, resolvedAt, found, err := st.GetGeoAddress(ctx, "k1")
		if err != nil || !found || addr != "Rua Augusta 1" || time.Since(resolvedAt) > time.Minute {
			t.Fatalf("hit: addr=%q resolved=%v found=%v err=%v", addr, resolvedAt, found, err)
		}
		if err := st.PutGeoAddress(ctx, "k2", ""); err != nil {
			t.Fatalf("negative put: %v", err)
		}
		if addr, _, found, _ := st.GetGeoAddress(ctx, "k2"); !found || addr != "" {
			t.Errorf("negative cache: found=%v addr=%q", found, addr)
		}
	})

	t.Run("countLocatedUpcoming + requireCoords", func(t *testing.T) {
		st := newStore(t)
		ctx := context.Background()
		now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
		located := func(id string, starts time.Time, city string) model.Event {
			e := sampleEvent(id, "E-"+id, "Lisboa", model.CategoryMusic, model.SourceLuma, starts, "https://img/x.jpg")
			e.CityID = city
			e.Venue = model.Venue{Name: "V", Lat: 38.72, Lon: -9.13}
			return e
		}
		noCoords := sampleEvent("nocoords", "Unlocated", "Lisboa", model.CategoryMusic, model.SourceLuma, now.Add(2*time.Hour), "https://img/y.jpg")
		noCoords.CityID = "lisbon"
		events := []model.Event{
			located("up1", now.Add(1*time.Hour), "lisbon"),
			located("up2", now.Add(4*time.Hour), "lisbon"),
			located("ended", now.Add(-30*time.Hour), "lisbon"),
			located("porto1", now.Add(2*time.Hour), "porto"),
			noCoords,
		}
		if err := st.UpsertEvents(ctx, events); err != nil {
			t.Fatalf("UpsertEvents: %v", err)
		}
		if n, err := st.CountLocatedUpcoming(ctx, "lisbon", now); err != nil || n != 2 {
			t.Errorf("lisbon located upcoming = %d err=%v, want 2", n, err)
		}
		if n, _ := st.CountLocatedUpcoming(ctx, "porto", now); n != 1 {
			t.Errorf("porto = %d, want 1", n)
		}
		if n, _ := st.CountLocatedUpcoming(ctx, "berlin", now); n != 0 {
			t.Errorf("berlin = %d, want 0", n)
		}
		if _, total, _, err := st.Query(ctx, Query{CityID: "lisbon", RequireCoords: true}); err != nil || total != 3 {
			t.Errorf("RequireCoords total = %d err=%v, want 3", total, err)
		}
	})
}

func TestStoreSuite_SQLite(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) Store { return newTestStore(t) })
}
