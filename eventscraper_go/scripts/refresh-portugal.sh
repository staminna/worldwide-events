#!/usr/bin/env bash
# Force-refresh eventscraper data for all Portuguese cities.
#
# Deployed to the Hetzner VPS at ~/eventscraper/refresh-portugal.sh and run
# from the varrho user's crontab every 6 hours (matches the shortest scrape
# TTL, so feeds stay fresh even with zero app traffic):
#
#   10 */6 * * * /home/varrho/eventscraper/refresh-portugal.sh
#
# Cities are paced 30s apart so concurrent lu.ma scrapes don't overlap and
# trip its burst rate limiting (the scraper itself paces requests 400ms
# apart, but only within a single city scrape).
set -u

API="http://127.0.0.1:8090"
ENV_FILE="$HOME/eventscraper/.env"
LOG="$HOME/eventscraper/refresh-portugal.log"

CITIES="lisbon porto aveiro beja braga braganca castelo-branco coimbra evora faro guarda leiria portalegre santarem setubal viana-do-castelo vila-real viseu acores madeira"
SOURCES="viralagenda luma songkick eventbrite"

TOKEN=$(grep '^ADMIN_TOKEN=' "$ENV_FILE" | cut -d= -f2)

# Fresh log each run; last run's output is enough for debugging.
exec > "$LOG" 2>&1
echo "=== refresh-portugal run $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="

if ! curl -sf -m 5 "$API/healthz" > /dev/null; then
    echo "backend not reachable at $API, aborting"
    exit 1
fi

for city in $CITIES; do
    for src in $SOURCES; do
        code=$(curl -s -o /dev/null -w "%{http_code}" -m 10 -X POST \
            -H "Authorization: Bearer $TOKEN" \
            "$API/refresh?city=$city&source=$src")
        echo "$city/$src: $code"
    done
    sleep 30
done
echo "=== done $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="
