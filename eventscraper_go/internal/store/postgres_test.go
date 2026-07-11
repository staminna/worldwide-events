package store

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jorgenunes/eventscraper/internal/model"
)

var schemaSeq int64

// newPostgresTestStore returns a Postgres store isolated in its own schema, so
// tests don't collide (even under -parallel) and clean up fully. Skips unless
// TEST_DATABASE_URL points at a PostGIS database.
func newPostgresTestStore(t *testing.T) Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run postgres store tests")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&schemaSeq, 1))
	quoted := pgx.Identifier{schema}.Sanitize()

	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatalf("create schema: %v", err)
	}
	admin.Close()

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	// Put the test schema first, then public so PostGIS types/functions resolve.
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	st := &Postgres{pool: pool}
	if err := st.Init(ctx); err != nil {
		pool.Close()
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		if a, err := pgxpool.New(ctx, url); err == nil {
			_, _ = a.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE")
			a.Close()
		}
	})
	return st
}

func TestStoreSuite_Postgres(t *testing.T) {
	runStoreSuite(t, newPostgresTestStore)
}

// TestPostgresGeomGenerated verifies the PostGIS-specific value-add: the
// generated geom column is populated for located events and NULL otherwise,
// which is what dekart/geosql map queries read.
func TestPostgresGeomGenerated(t *testing.T) {
	st := newPostgresTestStore(t)
	pg := st.(*Postgres)
	ctx := context.Background()
	base := time.Date(2026, 6, 10, 18, 0, 0, 0, time.UTC)

	located := sampleEvent("geo1", "Located", "Lisboa", model.CategoryMusic, model.SourceLuma, base, "https://img/1.jpg")
	located.Venue = model.Venue{Name: "V", Lat: 38.7223, Lon: -9.1393}
	unlocated := sampleEvent("geo2", "Unlocated", "Lisboa", model.CategoryMusic, model.SourceLuma, base, "https://img/2.jpg")

	if err := st.UpsertEvents(ctx, []model.Event{located, unlocated}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	var withGeom, total int
	if err := pg.pool.QueryRow(ctx,
		`SELECT COUNT(geom), COUNT(*) FROM events`,
	).Scan(&withGeom, &total); err != nil {
		t.Fatalf("count geom: %v", err)
	}
	if total != 2 || withGeom != 1 {
		t.Fatalf("geom populated on %d of %d rows, want 1 of 2", withGeom, total)
	}

	var lon, lat float64
	if err := pg.pool.QueryRow(ctx,
		`SELECT ST_X(geom), ST_Y(geom) FROM events WHERE id = $1`, located.ID,
	).Scan(&lon, &lat); err != nil {
		t.Fatalf("read geom point: %v", err)
	}
	if fmt.Sprintf("%.4f", lat) != "38.7223" || fmt.Sprintf("%.4f", lon) != "-9.1393" {
		t.Errorf("geom = (%.4f,%.4f), want (-9.1393,38.7223)", lon, lat)
	}
}
