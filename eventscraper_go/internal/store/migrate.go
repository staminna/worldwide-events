package store

import (
	"context"
	"fmt"
)

// MigrateStats reports how many rows a SQLite→Postgres migration copied.
type MigrateStats struct {
	Events       int
	Scrapes      int
	GeoAddresses int
}

// MigrateSQLiteToPostgres copies events, scrape status, and cached geo-addresses
// from a SQLite store into a Postgres store. It is idempotent (all writes are
// upserts), so it is safe to re-run. Events are read through the existing
// offset-paged Query (stable since nothing writes concurrently) and geo-address
// rows are read directly so their original resolved_at is preserved.
func MigrateSQLiteToPostgres(ctx context.Context, src *SQLite, dst *Postgres, batch int) (MigrateStats, error) {
	if batch <= 0 {
		batch = 500
	}
	var stats MigrateStats

	// Events: an unfiltered Query returns every event (incl. past/imageless
	// ones), ordered by starts_at,id — deterministic for offset paging.
	for off := 0; ; off += batch {
		rows, _, _, err := src.Query(ctx, Query{Limit: batch, Offset: off})
		if err != nil {
			return stats, fmt.Errorf("read events: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		if err := dst.UpsertEvents(ctx, rows); err != nil {
			return stats, fmt.Errorf("write events: %w", err)
		}
		stats.Events += len(rows)
		if len(rows) < batch {
			break
		}
	}

	// Scrape status.
	scrapes, err := src.AllScrapes(ctx)
	if err != nil {
		return stats, fmt.Errorf("read scrapes: %w", err)
	}
	for _, s := range scrapes {
		if err := dst.MarkScrape(ctx, s); err != nil {
			return stats, fmt.Errorf("write scrapes: %w", err)
		}
		stats.Scrapes++
	}

	// Cached reverse-geocoded addresses. Older SQLite files predate this table;
	// skip cleanly when it's absent.
	var hasGeo int
	_ = src.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='geo_addresses'`,
	).Scan(&hasGeo)
	if hasGeo == 0 {
		return stats, nil
	}

	// Read raw so resolved_at survives (PutGeoAddress would stamp it to now);
	// putGeoAddress preserves it.
	grows, err := src.db.QueryContext(ctx, `SELECT key, address, resolved_at FROM geo_addresses`)
	if err != nil {
		return stats, fmt.Errorf("read geo_addresses: %w", err)
	}
	defer grows.Close()
	for grows.Next() {
		var key, addr string
		var resolved int64
		if err := grows.Scan(&key, &addr, &resolved); err != nil {
			return stats, err
		}
		if err := dst.putGeoAddress(ctx, key, addr, resolved); err != nil {
			return stats, fmt.Errorf("write geo_addresses: %w", err)
		}
		stats.GeoAddresses++
	}
	return stats, grows.Err()
}
