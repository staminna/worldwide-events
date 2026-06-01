# eventscraper — worldwide upcoming events

Go backend (Colly + chi + SQLite cache) that pulls upcoming events worldwide
from **Eventbrite, Songkick, Ticketmaster, and Meetup** across **Tech / Music /
Business** categories, and exposes them as a JSON HTTP API for the
[`eventscraper_app`](../eventscraper_app) Flutter client.

## Architecture

```
 ┌─────────────────┐    HTTP     ┌────────────────────────────┐
 │  Flutter app    │ ──────────▶ │  Go HTTP API (chi)         │
 │  (riverpod+dio) │ ◀───────── │   • /events, /cities, etc.  │
 └─────────────────┘    JSON     │   • ETag, stale-while-     │
                                 │     revalidate, CORS       │
                                 └──────────┬─────────────────┘
                                            │
                              ┌─────────────┴──────────────┐
                              │ SQLite cache (TTL per cat) │
                              └─────────────┬──────────────┘
                                            │ (single-flight)
                                            ▼
                         ┌──────────────────────────────────────┐
                         │  Scraper registry                    │
                         │   • Eventbrite (Colly + JSON-LD)     │
                         │   • Songkick   (Colly + CSS)         │
                         │   • Ticketmaster (Discovery API)     │
                         │   • Meetup    (GraphQL — stub)       │
                         └──────────────────────────────────────┘
```

Cache strategy: `GET /events` returns whatever is already in SQLite *immediately*
(sub-50ms). If the (`source`, `city`) row in the `scrapes` table is past its
TTL, a background re-scrape is triggered single-flighted. TTL defaults: 6h for
tech/business, 12h for music.

## Source caveats (honest read)

| Source | How we hit it | Reliability | Notes |
|---|---|---|---|
| Eventbrite | Colly → `/d/{slug}/{category}--events/` → parses JSON-LD `<script>` blocks | OK from residential IPs; 403/CAPTCHA possible from clouds | We degrade to `ErrBlocked` if 403/429. |
| Songkick | Colly → `/metro-areas/{id}-{slug}` → `li.event-listings-element` | OK; selectors may drift over time | Music only. |
| Ticketmaster | Official Discovery API via `net/http` | Best | Needs `TICKETMASTER_API_KEY` (free at developer.ticketmaster.com). Has no "tech" segment — tech is skipped. |
| Meetup | GraphQL stub — returns `ErrUnconfigured` unless `MEETUP_OAUTH_TOKEN` | Disabled by default | Meetup removed free public scraping/REST. Their GraphQL endpoint needs OAuth + a paid tier for useful queries. |

Selectors and JSON shapes on real sites change — keep an eye on the `internal/scraper/*.go` parsers.

## Running

```bash
cd eventscraper
cp .env.example .env                          # then fill in API keys you have
go run ./cmd/eventscraper serve               # :8080
```

Other CLI commands:

```bash
go run ./cmd/eventscraper list cities
go run ./cmd/eventscraper list sources
go run ./cmd/eventscraper scrape --source eventbrite --city berlin
go run ./cmd/eventscraper scrape --source ticketmaster --city london --category music
```

## HTTP endpoints

| Method | Path | Description |
|---|---|---|
| GET | `/healthz` | Liveness probe |
| GET | `/cities` | Catalog of configured cities |
| GET | `/sources` | Source registration + recent scrape status |
| GET | `/events` | Filterable list. Query params: `city, category, source, from, to, q, limit, offset` |
| GET | `/events/{id}` | One event |
| POST | `/refresh?source=…&city=…` | Force re-scrape. Gated by `Authorization: Bearer $ADMIN_TOKEN` if set. |

Response envelope: `{ "data": [...], "meta": { "total": N, "cached": bool, "age": "12m" } }`.
`/events` sends an `ETag` derived from `(maxScrapedAt, total)`; clients that pass
`If-None-Match` get a `304`.

## Running the Flutter app

```bash
cd ../eventscraper_app
flutter pub get
flutter run -d chrome --dart-define=API_BASE=http://localhost:8080
```

For iOS/Android substitute `-d ios`, `-d android` etc. For mobile dev hitting
the host machine, point `API_BASE` at your LAN IP, e.g.
`--dart-define=API_BASE=http://192.168.1.10:8080`.

## Project layout

```
eventscraper/
├── cmd/eventscraper/        # CLI entry (cobra)
├── internal/
│   ├── api/                 # chi router + handlers
│   ├── cache/               # TTL + single-flight
│   ├── config/              # env parsing
│   ├── geo/                 # cities catalog loader
│   ├── model/               # canonical Event, Source, Category
│   ├── scraper/             # Scraper interface + 4 implementations
│   └── store/               # Store interface + SQLite (modernc.org/sqlite)
├── configs/cities.yaml      # ~50 top global cities
└── .env.example
```

## Roadmap

- Wire real Meetup GraphQL queries once OAuth credentials are available.
- Add `/events/near?lat=&lon=&km=` once we have spatial indexing.
- Per-source health alerts when scrape returns 0 events for N consecutive runs.
- Pagination cursors instead of offsets.
