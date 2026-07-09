# geosql stack — PostGIS + Dekart

Runs the events data in Postgres/PostGIS so the app writes to it and Dekart
(geosql) reads it for maps. Local-dev credentials are baked in; not for prod.

```
DSN (app, owner):   postgres://eventscraper:eventscraper@localhost:5432/eventscraper?sslmode=disable
DSN (dekart, read): postgres://dekart_ro:dekart_ro@db:5432/eventscraper   (inside compose network)
Dekart UI:          http://localhost:8080
```

## Bring it up

```bash
cd eventscraper/deploy/geosql
docker compose up -d db        # PostGIS only (fast); add dekart when ready
docker compose up -d           # + Dekart
```

## Migrate existing SQLite data → Postgres

From the `eventscraper/` module root (idempotent, re-runnable):

```bash
DATABASE_URL='postgres://eventscraper:eventscraper@localhost:5432/eventscraper?sslmode=disable' \
  go run ./cmd/eventscraper migrate-postgres --from ./eventscraper.db
```

## Run the app against Postgres

```bash
DATABASE_URL='postgres://eventscraper:eventscraper@localhost:5432/eventscraper?sslmode=disable' \
  go run ./cmd/eventscraper serve
```

Leave `DATABASE_URL` unset to keep using the embedded SQLite.

## Dekart

`dekart init` (localhost) points the CLI at http://localhost:8080. The Postgres
datasource is wired by compose env (`DEKART_DATASOURCE=PG`), so no connector
setup is needed. First map query:

```sql
SELECT geom AS geometry, title, category, source, city, starts_at_ts
FROM events
WHERE geom IS NOT NULL;
```

## Tests against real PostGIS

```bash
TEST_DATABASE_URL='postgres://eventscraper:eventscraper@localhost:5432/eventscraper?sslmode=disable' \
  go test ./internal/store/...
```
Without `TEST_DATABASE_URL` the Postgres store tests skip.
