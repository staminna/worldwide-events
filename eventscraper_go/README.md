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
| GET | `/geo/reverse?lat=…&lon=…` | Reverse geocode: nearest catalog city + `distanceKm`. With `min_events=N`, walks cities outward (≤15 candidates, ≤500 km) and returns the first with N located upcoming events (+ `locatedEvents`). Powers the app's "near me". |
| GET | `/geo/address?lat=…&lon=…` | Street address for a coordinate via Nominatim, cached in SQLite forever (negatives retry weekly). Optional `event=<id>` persists the address into that event. Upstream budget is 1 req/s globally (Nominatim policy); per-IP limits are a deliberate non-goal for now. |
| GET | `/events.geojson` | The feed as a GeoJSON FeatureCollection (same filters as `/events`, only located events, no image requirement, default limit 2000). Feeds `/viz`. |
| GET | `/viz` | Embedded kepler.gl page over `/events.geojson`: dark basemap, points by category, time-range playback. |
| GET | `/sources` | Source registration + recent scrape status |
| GET | `/events` | Filterable list. Query params: `city, category, source, from, to, q, limit, offset` |
| GET | `/events/{id}` | One event |
| POST | `/refresh?source=…&city=…` | Force re-scrape. Gated by `Authorization: Bearer $ADMIN_TOKEN` if set. |

Response envelope: `{ "data": [...], "meta": { "total": N, "cached": bool, "age": "12m" } }`.
`/events` sends an `ETag` derived from `(maxScrapedAt, total)`; clients that pass
`If-None-Match` get a `304`.

### Chat & live location

Anonymous identities: `POST /chat/register {"name":"…"}` returns `{id, name, token}`;
the opaque token is the bearer for everything below (no passwords, possession is
the identity). Groups are either the public room of an event (created lazily on
first join) or private invite-code groups.

| Method | Path | Description |
|---|---|---|
| POST | `/chat/register` | Create an identity; returns the bearer token |
| GET | `/chat/groups` | My groups (member count, last message) |
| POST | `/chat/groups` | Create a private group; returns its 6-char `inviteCode` |
| POST | `/chat/groups/join` | Join by `{"code":"…"}` |
| POST | `/chat/events/{id}/join` | Join (get-or-create) an event's public room |
| POST | `/chat/groups/{id}/leave` | Leave a group |
| GET | `/chat/groups/{id}/messages?before=…&limit=…` | History, newest first; `before` is a message id cursor |
| POST | `/chat/groups/{id}/messages` | Send over HTTP (fallback; fans out to live sockets too) |
| GET | `/chat/ws?token=…` | WebSocket: live messages, presence, and location shares for all my groups |

The socket multiplexes every group with JSON envelopes discriminated by `type`
(`message`, `location`, `location_stop`, `sub`, `presence`, `join`, `leave`,
`error`). Location shares are **ephemeral** — kept only in server memory, swept
after 2 min without a fix (clients heartbeat every 20 s) and hard-capped at 3 h;
nothing is ever written to the DB. Message sends are rate-limited per connection
(1/s, burst 5) and capped at 2000 chars.

**Deploying behind nginx** (the reverse proxy on the host must upgrade the WS
path; everything else is unchanged):

```nginx
location /eventscraper/chat/ws {
    proxy_pass http://127.0.0.1:8090/chat/ws;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 90s;  # > the server's 30s ping interval
    proxy_send_timeout 90s;
}
```

Smoke test: `websocat "wss://api.iamjorgenunes.com/eventscraper/chat/ws?token=…"`.

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
