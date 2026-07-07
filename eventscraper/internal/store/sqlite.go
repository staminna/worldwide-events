package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLite, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	return &SQLite{db: db}, nil
}

const schema = `
CREATE TABLE IF NOT EXISTS events (
    id           TEXT PRIMARY KEY,
    source       TEXT NOT NULL,
    source_id    TEXT NOT NULL,
    title        TEXT NOT NULL,
    category     TEXT NOT NULL,
    starts_at    INTEGER NOT NULL,
    ends_at      INTEGER,
    city         TEXT NOT NULL,
    city_id      TEXT NOT NULL DEFAULT '',
    country      TEXT NOT NULL,
    url          TEXT NOT NULL,
    payload      TEXT NOT NULL,
    scraped_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_city_cat_start ON events(city, category, starts_at);
CREATE INDEX IF NOT EXISTS idx_events_cityid_cat_start ON events(city_id, category, starts_at);
CREATE INDEX IF NOT EXISTS idx_events_source_city    ON events(source, city);
CREATE INDEX IF NOT EXISTS idx_events_starts_at      ON events(starts_at);
CREATE INDEX IF NOT EXISTS idx_events_scraped_at     ON events(scraped_at);

CREATE TABLE IF NOT EXISTS scrapes (
    source        TEXT NOT NULL,
    city_id       TEXT NOT NULL,
    last_run_at   INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    status        TEXT NOT NULL,
    err_message   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (source, city_id)
);

CREATE TABLE IF NOT EXISTS geo_addresses (
    key          TEXT PRIMARY KEY,
    address      TEXT NOT NULL,
    resolved_at  INTEGER NOT NULL
);
`

func (s *SQLite) Init(ctx context.Context) error {
	// Databases created before the city_id column existed need the ALTER
	// first, or the CREATE INDEX on city_id in schema fails.
	var hasCityID int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('events') WHERE name = 'city_id'`,
	).Scan(&hasCityID)
	if err == nil && hasCityID == 0 {
		var hasEvents int
		_ = s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'events'`,
		).Scan(&hasEvents)
		if hasEvents > 0 {
			if _, err := s.db.ExecContext(ctx,
				`ALTER TABLE events ADD COLUMN city_id TEXT NOT NULL DEFAULT ''`,
			); err != nil {
				return fmt.Errorf("migrate city_id: %w", err)
			}
		}
	}
	_, err = s.db.ExecContext(ctx, schema)
	return err
}

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) UpsertEvents(ctx context.Context, events []model.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO events (id, source, source_id, title, category, starts_at, ends_at, city, city_id, country, url, payload, scraped_at)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
            title=excluded.title,
            category=excluded.category,
            starts_at=excluded.starts_at,
            ends_at=excluded.ends_at,
            city=excluded.city,
            city_id=excluded.city_id,
            country=excluded.country,
            url=excluded.url,
            payload=excluded.payload,
            scraped_at=excluded.scraped_at
    `)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range events {
		payload, err := json.Marshal(e)
		if err != nil {
			return err
		}
		var endsAt sql.NullInt64
		if e.EndsAt != nil {
			endsAt = sql.NullInt64{Int64: e.EndsAt.Unix(), Valid: true}
		}
		if _, err := stmt.ExecContext(ctx,
			e.ID, string(e.Source), e.SourceID, e.Title, string(e.Category),
			e.StartsAt.Unix(), endsAt, e.City, e.CityID, e.Country, e.URL, string(payload), e.ScrapedAt.Unix(),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) GetEvent(ctx context.Context, id string) (model.Event, bool, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM events WHERE id = ?`, id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Event{}, false, nil
	}
	if err != nil {
		return model.Event{}, false, err
	}
	var e model.Event
	if err := json.Unmarshal([]byte(payload), &e); err != nil {
		return model.Event{}, false, err
	}
	return e, true, nil
}

// noEndGrace is how long an event without an explicit end time is assumed to
// keep running after it starts, for the purposes of Query.NotEndedBefore.
const noEndGrace = 3 * time.Hour

func (s *SQLite) Query(ctx context.Context, q Query) ([]model.Event, int, time.Time, error) {
	var (
		conds []string
		args  []any
	)
	if q.CityID != "" {
		conds = append(conds, "city_id = ?")
		args = append(args, q.CityID)
	}
	if q.City != "" {
		conds = append(conds, "city = ?")
		args = append(args, q.City)
	}
	if q.Category != "" {
		conds = append(conds, "category = ?")
		args = append(args, string(q.Category))
	}
	if q.Source != "" {
		conds = append(conds, "source = ?")
		args = append(args, string(q.Source))
	}
	if !q.From.IsZero() {
		conds = append(conds, "starts_at >= ?")
		args = append(args, q.From.Unix())
	}
	if !q.NotEndedBefore.IsZero() {
		conds = append(conds, "COALESCE(ends_at, starts_at + ?) >= ?")
		args = append(args, int64(noEndGrace.Seconds()), q.NotEndedBefore.Unix())
	}
	if !q.To.IsZero() {
		conds = append(conds, "starts_at <= ?")
		args = append(args, q.To.Unix())
	}
	if q.Search != "" {
		conds = append(conds, "lower(title) LIKE ?")
		args = append(args, "%"+strings.ToLower(q.Search)+"%")
	}
	if q.RequireImage {
		conds = append(conds, "json_extract(payload, '$.imageUrl') != ''")
	}
	if q.RequireCoords {
		// Venue lat/lon carry omitempty, so zero coords are absent from the
		// payload JSON and json_extract yields NULL.
		conds = append(conds, "json_extract(payload, '$.venue.lat') IS NOT NULL",
			"json_extract(payload, '$.venue.lon') IS NOT NULL")
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	var total int
	var maxScraped sql.NullInt64
	row := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*), COALESCE(MAX(scraped_at), 0) FROM events %s`, where), args...)
	if err := row.Scan(&total, &maxScraped); err != nil {
		return nil, 0, time.Time{}, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	pagedArgs := append(append([]any{}, args...), limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
        SELECT payload FROM events %s
        ORDER BY starts_at ASC, id ASC
        LIMIT ? OFFSET ?
    `, where), pagedArgs...)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	defer rows.Close()

	out := make([]model.Event, 0, limit)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, 0, time.Time{}, err
		}
		var e model.Event
		if err := json.Unmarshal([]byte(p), &e); err != nil {
			return nil, 0, time.Time{}, err
		}
		out = append(out, e)
	}
	var maxT time.Time
	if maxScraped.Valid && maxScraped.Int64 > 0 {
		maxT = time.Unix(maxScraped.Int64, 0).UTC()
	}
	return out, total, maxT, rows.Err()
}

func (s *SQLite) CountLocatedUpcoming(ctx context.Context, cityID string, notEndedBefore time.Time) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
        SELECT COUNT(*) FROM events
         WHERE city_id = ?
           AND COALESCE(ends_at, starts_at + ?) >= ?
           AND json_extract(payload, '$.venue.lat') IS NOT NULL
           AND json_extract(payload, '$.venue.lon') IS NOT NULL
    `, cityID, int64(noEndGrace.Seconds()), notEndedBefore.Unix()).Scan(&n)
	return n, err
}

func (s *SQLite) GetGeoAddress(ctx context.Context, key string) (string, time.Time, bool, error) {
	var addr string
	var resolved int64
	err := s.db.QueryRowContext(ctx,
		`SELECT address, resolved_at FROM geo_addresses WHERE key = ?`, key,
	).Scan(&addr, &resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	return addr, time.Unix(resolved, 0).UTC(), true, nil
}

func (s *SQLite) PutGeoAddress(ctx context.Context, key, addr string) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO geo_addresses (key, address, resolved_at)
        VALUES (?, ?, ?)
        ON CONFLICT(key) DO UPDATE SET
            address     = excluded.address,
            resolved_at = excluded.resolved_at
    `, key, addr, time.Now().Unix())
	return err
}

func (s *SQLite) SetVenueAddressIfEmpty(ctx context.Context, eventID, addr string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
        UPDATE events
           SET payload = json_set(payload, '$.venue.address', ?)
         WHERE id = ?
           AND COALESCE(json_extract(payload, '$.venue.address'), '') = ''
    `, addr, eventID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *SQLite) MarkScrape(ctx context.Context, st ScrapeStatus) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO scrapes (source, city_id, last_run_at, expires_at, status, err_message)
        VALUES (?, ?, ?, ?, ?, ?)
        ON CONFLICT(source, city_id) DO UPDATE SET
            last_run_at = excluded.last_run_at,
            expires_at  = excluded.expires_at,
            status      = excluded.status,
            err_message = excluded.err_message
    `, string(st.Source), st.CityID, st.LastRunAt.Unix(), st.ExpiresAt.Unix(), st.Status, st.ErrMessage)
	return err
}

func (s *SQLite) GetScrape(ctx context.Context, src model.Source, cityID string) (ScrapeStatus, bool, error) {
	var (
		last, exp int64
		status    string
		errMsg    string
	)
	err := s.db.QueryRowContext(ctx, `
        SELECT last_run_at, expires_at, status, err_message
        FROM scrapes WHERE source = ? AND city_id = ?
    `, string(src), cityID).Scan(&last, &exp, &status, &errMsg)
	if errors.Is(err, sql.ErrNoRows) {
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

func (s *SQLite) ClearImageURLsMatching(ctx context.Context, patterns []string) (int, error) {
	if len(patterns) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var total int64
	for _, p := range patterns {
		res, err := tx.ExecContext(ctx, `
            UPDATE events
               SET payload = json_set(payload, '$.imageUrl', '')
             WHERE json_extract(payload, '$.imageUrl') LIKE ?
        `, p)
		if err != nil {
			return int(total), err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	if err := tx.Commit(); err != nil {
		return int(total), err
	}
	return int(total), nil
}

func (s *SQLite) AllScrapes(ctx context.Context) ([]ScrapeStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
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
