package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jorgenunes/eventscraper/internal/model"
)

// Postgres is a PostGIS-backed Store. It mirrors the SQLite implementation
// method-for-method (same filters, ordering, and payload round-trip) but adds
// a generated `geom geometry(Point,4326)` column so the same table can be read
// directly by dekart/geosql map queries (`SELECT geom AS geometry, ...`).
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres opens a pooled connection to the given DSN (postgres://...).
func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() error { p.pool.Close(); return nil }

// pgSchema is created idempotently by Init. lat/lon/geom/starts_at_ts are
// STORED generated columns derived straight from the jsonb payload, so they
// stay correct on every upsert and jsonb_set without any extra bookkeeping.
// geom is generated from the payload (not from lat/lon) because Postgres
// forbids a generated column referencing another generated column.
const pgSchema = `
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS events (
    id          text PRIMARY KEY,
    source      text   NOT NULL,
    source_id   text   NOT NULL,
    title       text   NOT NULL,
    category    text   NOT NULL,
    starts_at   bigint NOT NULL,
    ends_at     bigint,
    city        text   NOT NULL,
    city_id     text   NOT NULL DEFAULT '',
    country     text   NOT NULL,
    url         text   NOT NULL,
    payload     jsonb  NOT NULL,
    scraped_at  bigint NOT NULL,
    lat  double precision GENERATED ALWAYS AS ((payload->'venue'->>'lat')::double precision) STORED,
    lon  double precision GENERATED ALWAYS AS ((payload->'venue'->>'lon')::double precision) STORED,
    geom geometry(Point,4326) GENERATED ALWAYS AS (
        CASE
          WHEN (payload->'venue'->>'lat') IS NOT NULL
           AND (payload->'venue'->>'lon') IS NOT NULL
          THEN ST_SetSRID(ST_MakePoint(
                 (payload->'venue'->>'lon')::double precision,
                 (payload->'venue'->>'lat')::double precision), 4326)
        END
    ) STORED,
    starts_at_ts timestamptz GENERATED ALWAYS AS (to_timestamp(starts_at)) STORED
);
CREATE INDEX IF NOT EXISTS idx_events_city_cat_start   ON events (city, category, starts_at);
CREATE INDEX IF NOT EXISTS idx_events_cityid_cat_start ON events (city_id, category, starts_at);
CREATE INDEX IF NOT EXISTS idx_events_source_city      ON events (source, city);
CREATE INDEX IF NOT EXISTS idx_events_starts_at        ON events (starts_at);
CREATE INDEX IF NOT EXISTS idx_events_scraped_at       ON events (scraped_at);
CREATE INDEX IF NOT EXISTS idx_events_geom             ON events USING GIST (geom);

CREATE TABLE IF NOT EXISTS scrapes (
    source      text   NOT NULL,
    city_id     text   NOT NULL,
    last_run_at bigint NOT NULL,
    expires_at  bigint NOT NULL,
    status      text   NOT NULL,
    err_message text   NOT NULL DEFAULT '',
    PRIMARY KEY (source, city_id)
);

CREATE TABLE IF NOT EXISTS geo_addresses (
    key         text   PRIMARY KEY,
    address     text   NOT NULL,
    resolved_at bigint NOT NULL
);
`

func (p *Postgres) Init(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, pgSchema)
	return err
}

func (p *Postgres) UpsertEvents(ctx context.Context, events []model.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	const q = `
        INSERT INTO events (id, source, source_id, title, category, starts_at, ends_at, city, city_id, country, url, payload, scraped_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13)
        ON CONFLICT (id) DO UPDATE SET
            title=EXCLUDED.title,
            category=EXCLUDED.category,
            starts_at=EXCLUDED.starts_at,
            ends_at=EXCLUDED.ends_at,
            city=EXCLUDED.city,
            city_id=EXCLUDED.city_id,
            country=EXCLUDED.country,
            url=EXCLUDED.url,
            payload=EXCLUDED.payload,
            scraped_at=EXCLUDED.scraped_at`
	batch := &pgx.Batch{}
	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return err
		}
		var endsAt *int64
		if e.EndsAt != nil {
			v := e.EndsAt.Unix()
			endsAt = &v
		}
		batch.Queue(q,
			e.ID, string(e.Source), e.SourceID, e.Title, string(e.Category),
			e.StartsAt.Unix(), endsAt, e.City, e.CityID, e.Country, e.URL,
			string(payload), e.ScrapedAt.Unix(),
		)
	}
	br := tx.SendBatch(ctx, batch)
	for range events {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetEvent(ctx context.Context, id string) (model.Event, bool, error) {
	var payload []byte
	err := p.pool.QueryRow(ctx, `SELECT payload FROM events WHERE id = $1`, id).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Event{}, false, nil
	}
	if err != nil {
		return model.Event{}, false, err
	}
	var e model.Event
	if err := json.Unmarshal(payload, &e); err != nil {
		return model.Event{}, false, err
	}
	return e, true, nil
}

func (p *Postgres) Query(ctx context.Context, q Query) ([]model.Event, int, time.Time, error) {
	var (
		conds []string
		args  []any
	)
	// ph appends an arg and returns its $N placeholder.
	ph := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if q.CityID != "" {
		conds = append(conds, "city_id = "+ph(q.CityID))
	}
	if q.City != "" {
		conds = append(conds, "city = "+ph(q.City))
	}
	if q.Category != "" {
		conds = append(conds, "category = "+ph(string(q.Category)))
	}
	if q.Source != "" {
		conds = append(conds, "source = "+ph(string(q.Source)))
	}
	if !q.From.IsZero() {
		conds = append(conds, "starts_at >= "+ph(q.From.Unix()))
	}
	if !q.NotEndedBefore.IsZero() {
		conds = append(conds, "COALESCE(ends_at, starts_at + "+ph(int64(noEndGrace.Seconds()))+") >= "+ph(q.NotEndedBefore.Unix()))
	}
	if !q.To.IsZero() {
		conds = append(conds, "starts_at <= "+ph(q.To.Unix()))
	}
	if q.Search != "" {
		conds = append(conds, "lower(title) LIKE "+ph("%"+strings.ToLower(q.Search)+"%"))
	}
	if q.RequireImage {
		// Manually added events are exempt — they carry coordinates and are
		// worth showing even without an image.
		conds = append(conds, "(payload->>'imageUrl' <> '' OR source = 'manual')")
	}
	if q.RequireCoords {
		// lat/lon are generated from the payload; absent coords ⇒ NULL.
		conds = append(conds, "lat IS NOT NULL", "lon IS NOT NULL")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	var maxScraped *int64
	if err := p.pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*), MAX(scraped_at) FROM events %s`, where), args...,
	).Scan(&total, &maxScraped); err != nil {
		return nil, 0, time.Time{}, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	pagedArgs := append(append([]any{}, args...), limit, q.Offset)
	limPh := "$" + strconv.Itoa(len(pagedArgs)-1)
	offPh := "$" + strconv.Itoa(len(pagedArgs))

	rows, err := p.pool.Query(ctx, fmt.Sprintf(`
        SELECT payload FROM events %s
        ORDER BY starts_at ASC, id ASC
        LIMIT %s OFFSET %s`, where, limPh, offPh), pagedArgs...)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	defer rows.Close()

	out := make([]model.Event, 0, limit)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, 0, time.Time{}, err
		}
		var e model.Event
		if err := json.Unmarshal(payload, &e); err != nil {
			return nil, 0, time.Time{}, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, time.Time{}, err
	}
	var maxT time.Time
	if maxScraped != nil && *maxScraped > 0 {
		maxT = time.Unix(*maxScraped, 0).UTC()
	}
	return out, total, maxT, nil
}

func (p *Postgres) CountLocatedUpcoming(ctx context.Context, cityID string, notEndedBefore time.Time) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `
        SELECT COUNT(*) FROM events
         WHERE city_id = $1
           AND COALESCE(ends_at, starts_at + $2) >= $3
           AND lat IS NOT NULL
           AND lon IS NOT NULL
    `, cityID, int64(noEndGrace.Seconds()), notEndedBefore.Unix()).Scan(&n)
	return n, err
}

func (p *Postgres) GetGeoAddress(ctx context.Context, key string) (string, time.Time, bool, error) {
	var addr string
	var resolved int64
	err := p.pool.QueryRow(ctx,
		`SELECT address, resolved_at FROM geo_addresses WHERE key = $1`, key,
	).Scan(&addr, &resolved)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	return addr, time.Unix(resolved, 0).UTC(), true, nil
}

func (p *Postgres) PutGeoAddress(ctx context.Context, key, addr string) error {
	return p.putGeoAddress(ctx, key, addr, time.Now().Unix())
}

// putGeoAddress upserts a geo-address row with an explicit resolved_at, letting
// the migration preserve the original timestamp instead of stamping now.
func (p *Postgres) putGeoAddress(ctx context.Context, key, addr string, resolvedAt int64) error {
	_, err := p.pool.Exec(ctx, `
        INSERT INTO geo_addresses (key, address, resolved_at)
        VALUES ($1, $2, $3)
        ON CONFLICT (key) DO UPDATE SET
            address     = EXCLUDED.address,
            resolved_at = EXCLUDED.resolved_at
    `, key, addr, resolvedAt)
	return err
}

func (p *Postgres) SetVenueAddressIfEmpty(ctx context.Context, eventID, addr string) (bool, error) {
	ct, err := p.pool.Exec(ctx, `
        UPDATE events
           SET payload = jsonb_set(payload, '{venue,address}', to_jsonb($1::text), true)
         WHERE id = $2
           AND COALESCE(payload->'venue'->>'address', '') = ''
    `, addr, eventID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func (p *Postgres) MarkScrape(ctx context.Context, st ScrapeStatus) error {
	_, err := p.pool.Exec(ctx, `
        INSERT INTO scrapes (source, city_id, last_run_at, expires_at, status, err_message)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (source, city_id) DO UPDATE SET
            last_run_at = EXCLUDED.last_run_at,
            expires_at  = EXCLUDED.expires_at,
            status      = EXCLUDED.status,
            err_message = EXCLUDED.err_message
    `, string(st.Source), st.CityID, st.LastRunAt.Unix(), st.ExpiresAt.Unix(), st.Status, st.ErrMessage)
	return err
}

func (p *Postgres) GetScrape(ctx context.Context, src model.Source, cityID string) (ScrapeStatus, bool, error) {
	var (
		last, exp      int64
		status, errMsg string
	)
	err := p.pool.QueryRow(ctx, `
        SELECT last_run_at, expires_at, status, err_message
        FROM scrapes WHERE source = $1 AND city_id = $2
    `, string(src), cityID).Scan(&last, &exp, &status, &errMsg)
	if errors.Is(err, pgx.ErrNoRows) {
		return ScrapeStatus{}, false, nil
	}
	if err != nil {
		return ScrapeStatus{}, false, err
	}
	return ScrapeStatus{
		Source:     src,
		CityID:     cityID,
		LastRunAt:  time.Unix(last, 0).UTC(),
		ExpiresAt:  time.Unix(exp, 0).UTC(),
		Status:     status,
		ErrMessage: errMsg,
	}, true, nil
}

func (p *Postgres) ClearImageURLsMatching(ctx context.Context, patterns []string) (int, error) {
	if len(patterns) == 0 {
		return 0, nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var total int64
	for _, pat := range patterns {
		// ILIKE, not LIKE: SQLite's LIKE is case-insensitive by default, so the
		// Postgres equivalent must fold case to match behavior.
		ct, err := tx.Exec(ctx, `
            UPDATE events
               SET payload = jsonb_set(payload, '{imageUrl}', '""'::jsonb, true)
             WHERE payload->>'imageUrl' ILIKE $1
        `, pat)
		if err != nil {
			return int(total), err
		}
		total += ct.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return int(total), err
	}
	return int(total), nil
}

func (p *Postgres) AllScrapes(ctx context.Context) ([]ScrapeStatus, error) {
	rows, err := p.pool.Query(ctx, `
        SELECT source, city_id, last_run_at, expires_at, status, err_message
        FROM scrapes ORDER BY last_run_at DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ScrapeStatus
	for rows.Next() {
		var src, city, status, errMsg string
		var last, exp int64
		if err := rows.Scan(&src, &city, &last, &exp, &status, &errMsg); err != nil {
			return nil, err
		}
		out = append(out, ScrapeStatus{
			Source:     model.Source(src),
			CityID:     city,
			LastRunAt:  time.Unix(last, 0).UTC(),
			ExpiresAt:  time.Unix(exp, 0).UTC(),
			Status:     status,
			ErrMessage: errMsg,
		})
	}
	return out, rows.Err()
}
