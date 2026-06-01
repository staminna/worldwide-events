# Worldwide Events

A two-part app for browsing upcoming events from across the world, pulled from
license-free sources only.

- **`eventscraper/`** — Go backend. Scrapes Eventbrite, Songkick and Luma,
  caches everything in SQLite with TTL-based stale-while-revalidate, exposes a
  JSON HTTP API, and includes an image-proxy that fixes CORS / hotlinking /
  missing Content-Type problems on the public CDNs.
- **`eventscraper_app/`** — Flutter client (Web / iOS / Android / macOS).
  Responsive 1 / 2 / 3-column grid, persistent search, filter sheet (city,
  category, source, date range), interactive OpenStreetMap view of every
  geocoded event, and a polished event detail screen.

## Prerequisites

| Tool    | Version          | Install                                          |
|---------|------------------|--------------------------------------------------|
| Go      | 1.22 or newer    | https://go.dev/dl/ (or `brew install go`)        |
| Flutter | 3.41 or newer    | https://docs.flutter.dev/get-started/install     |
| Chrome  | any recent       | for `flutter run -d chrome`                      |
| SQLite  | bundled (`modernc.org/sqlite`, no CGO required)             |

No API keys, accounts, or paid services are required to run the default
configuration.

## Quick start

Two terminals.

### Terminal 1 — backend

```bash
cd eventscraper
cp .env.example .env          # optional, all defaults work
go run ./cmd/eventscraper serve
```

The first cold start kicks off a background warm-up that scrapes all 80
configured cities across all three free sources. You can use the API
immediately — events appear in the cache as the warm-up progresses, and the
Flutter app auto-polls until they show up.

Verify it's up:

```bash
curl http://localhost:8080/healthz
curl 'http://localhost:8080/events?limit=3' | jq .
```

### Terminal 2 — Flutter app

```bash
cd eventscraper_app
flutter pub get
flutter run -d chrome --dart-define=API_BASE=http://localhost:8080
```

For native targets, use `-d ios`, `-d android`, or `-d macos`. When testing on
a mobile device against your laptop, point `API_BASE` at your LAN IP, e.g.
`--dart-define=API_BASE=http://192.168.1.10:8080`.

## Configuration

All configuration is environment-variable based — see `eventscraper/.env.example`.

| Variable               | Default                          | Purpose                                                    |
|------------------------|----------------------------------|------------------------------------------------------------|
| `PORT`                 | `8080`                           | HTTP listen port                                           |
| `DB_PATH`              | `./eventscraper.db`              | SQLite file                                                |
| `CITIES_PATH`          | `./configs/cities.yaml`          | City catalog                                               |
| `ALLOWED_ORIGIN`       | `*`                              | CORS allow-origin                                          |
| `WARMUP_CITIES`        | `0` (= all 80)                   | How many cities to warm up on startup                      |
| `FREE_ONLY`            | `true`                           | When `false`, also registers Ticketmaster / Meetup         |
| `TICKETMASTER_API_KEY` | unset                            | Required if you flip `FREE_ONLY=false` and want TM         |
| `MEETUP_OAUTH_TOKEN`   | unset                            | Required if you want Meetup (paid OAuth client needed)     |
| `ADMIN_TOKEN`          | unset                            | When set, `POST /refresh` requires `Authorization: Bearer` |

## CLI

```bash
go run ./cmd/eventscraper serve                                   # default: HTTP API
go run ./cmd/eventscraper list cities
go run ./cmd/eventscraper list sources
go run ./cmd/eventscraper scrape --source eventbrite --city berlin
go run ./cmd/eventscraper scrape --source luma       --city paris --category tech
go run ./cmd/eventscraper scrape --source songkick   --city london --category music
```

A one-shot `scrape` writes events to the same SQLite DB that `serve` reads, so
you can pre-populate the cache before starting the server.

## HTTP API

| Method | Path               | Description                                                                 |
|--------|--------------------|-----------------------------------------------------------------------------|
| GET    | `/healthz`         | Liveness                                                                    |
| GET    | `/cities`          | Configured cities                                                           |
| GET    | `/sources`         | Source registration + last scrape per (source, city)                        |
| GET    | `/events`          | Filterable list. Params: `city, category, source, from, to, q, limit, offset` |
| GET    | `/events/{id}`     | Single event                                                                |
| GET    | `/img?u=<url>`     | CORS-friendly image proxy (used by the Flutter client)                      |
| POST   | `/refresh`         | Force re-scrape. Gated by `ADMIN_TOKEN` if set.                             |

Response envelope: `{"data": [...], "meta": {"total": N, "cached": bool, "age": "12m", "limit": L, "offset": O}}`.
`/events` sends an `ETag` derived from `(maxScrapedAt, total)`; clients passing
`If-None-Match` get a `304`.

## Project layout

```
2026/go/
├── eventscraper/                       Go backend
│   ├── cmd/eventscraper/main.go        CLI entry (cobra)
│   ├── internal/
│   │   ├── api/                        chi router, handlers, image proxy
│   │   ├── cache/                      TTL helpers + single-flight
│   │   ├── config/                     env parsing
│   │   ├── enrich/                     og:image / twitter:image backfill
│   │   ├── geo/                        cities catalog loader
│   │   ├── model/                      canonical Event, Source, Category
│   │   ├── scheduler/                  warm-up + Run + MaybeRefresh
│   │   ├── scraper/                    Eventbrite, Songkick, Luma,
│   │   │                                Ticketmaster, Meetup
│   │   └── store/                      SQLite store
│   ├── configs/cities.yaml             80 cities incl. all EU capitals
│   ├── .env.example
│   └── README.md
│
└── eventscraper_app/                   Flutter client
    ├── lib/
    │   ├── main.dart                   ProviderScope + GoRouter
    │   ├── models/event.dart           canonical types
    │   ├── api/event_api.dart          dio client + proxiedImage()
    │   ├── state/providers.dart        riverpod providers
    │   ├── screens/
    │   │   ├── home_shell.dart         NavigationBar: Feed / Map
    │   │   ├── home_screen.dart        responsive grid + search bar
    │   │   ├── filters_sheet.dart      city / cat / source / date range
    │   │   ├── map_screen.dart         flutter_map (OSM)
    │   │   └── event_detail.dart       hero + mini map
    │   └── widgets/event_card.dart
    └── pubspec.yaml
```

## What changes when

- The catalog of cities lives in `eventscraper/configs/cities.yaml`. Add a new
  city by appending an entry with `id`, `name`, `country`, `lat`, `lon`,
  `eventbrite_slug`, `songkick_metro`, `luma_city_slug`. Restart the server.
- Scrape recipes live in `eventscraper/internal/scraper/*.go`. Eventbrite reads
  `window.__SERVER_DATA__`; Songkick walks HTML cards; Luma hits the public
  `api.lu.ma/discover/get-paginated-events` JSON endpoint.
- Image enrichment runs after every scrape and on the CLI `scrape` command.
  Known placeholder URLs (`default-event`, `default_images`, `placeholder`,
  `no-image`) are rejected and stripped from the DB on every server start.

## Troubleshooting

- **The feed shows "Building your feed…" for too long.** Tail the server log;
  warming all 80 cities takes 5–12 minutes on a first cold start. Reduce with
  `WARMUP_CITIES=10`.
- **Flutter Web shows broken images.** Make sure you launched with
  `--dart-define=API_BASE=…`. The Flutter client routes every image through
  the backend's `/img?u=` proxy to fix CORS; if it points at the wrong host
  the proxy isn't reachable.
- **CAPTCHA / 403 from Eventbrite or Songkick.** Both sites occasionally block
  bot-shaped traffic. The scraper returns `ErrBlocked` and marks the scrape
  status accordingly; the cache keeps serving the last good results. Retry
  later or run the scraper from a residential connection.
- **Resetting the cache.** Stop the server, delete `eventscraper.db`, restart.
  Or call `POST /refresh?source=…&city=…` to refresh a single (source, city).

## Licensing of upstream data

The scraper only pulls publicly-served HTML or public unauthenticated JSON
endpoints. Event content (titles, dates, venue addresses, images) belongs to
the respective platforms and their listing partners. This project is for
personal / educational use; for production deployment, check each platform's
terms of service before redistributing their data.
