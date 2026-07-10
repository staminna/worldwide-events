package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port             string
	DBPath           string
	CitiesPath       string
	UploadDir        string
	AdminToken       string
	TicketmasterKey  string
	MeetupOAuthToken string
	AllowedOrigin    string
	FreeOnly         bool
	WarmupCities     int

	// DatabaseURL, when set (postgres://...), switches the store from the
	// default embedded SQLite to Postgres/PostGIS.
	DatabaseURL string

	// RunsURL is where the MCP scrape_status tool fetches the live run
	// snapshot from (the now-private /runs.json). AdminToken authenticates it.
	RunsURL string

	// Stealth / pacing knobs. All optional; the defaults give a polite,
	// bounded engine even with no proxies configured.
	ScrapeConcurrency int // max simultaneous (source,city) scrapes
	ScrapeMinDelayMS  int // min stagger before each scrape unit
	ScrapeMaxDelayMS  int // max stagger before each scrape unit
	ScrapeMaxRetries  int // per-request retries on block/5xx

	// Proxy sources (checked in this order; first non-empty wins). Absent ⇒
	// direct connections.
	ProxyListURL  string // Webshare tokenized proxy-list download URL
	ProxyListPath string // local file, one proxy per line
	ProxiesInline string // comma/newline list in the env var itself
}

func FromEnv() Config {
	wd, _ := os.Getwd()
	c := Config{
		Port:             getenv("PORT", "8080"),
		DBPath:           getenv("DB_PATH", filepath.Join(wd, "eventscraper.db")),
		CitiesPath:       getenv("CITIES_PATH", filepath.Join(wd, "configs", "cities.yaml")),
		UploadDir:        getenv("UPLOAD_DIR", filepath.Join(wd, "uploads")),
		AdminToken:       os.Getenv("ADMIN_TOKEN"),
		TicketmasterKey:  os.Getenv("TICKETMASTER_API_KEY"),
		MeetupOAuthToken: os.Getenv("MEETUP_OAUTH_TOKEN"),
		AllowedOrigin:    getenv("ALLOWED_ORIGIN", "*"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		RunsURL: getenv("RUNS_URL",
			"https://api.iamjorgenunes.com/eventscraper/runs.json"),
		FreeOnly:         getenv("FREE_ONLY", "true") != "false",
		// 0 means "all cities in the catalog".
		WarmupCities: atoiOrZero(os.Getenv("WARMUP_CITIES")),

		ScrapeConcurrency: atoiOrDefault(os.Getenv("SCRAPE_CONCURRENCY"), 4),
		ScrapeMinDelayMS:  atoiOrDefault(os.Getenv("SCRAPE_MIN_DELAY_MS"), 300),
		ScrapeMaxDelayMS:  atoiOrDefault(os.Getenv("SCRAPE_MAX_DELAY_MS"), 1200),
		ScrapeMaxRetries:  atoiOrDefault(os.Getenv("SCRAPE_MAX_RETRIES"), 3),

		ProxyListURL:  os.Getenv("WEBSHARE_PROXY_LIST_URL"),
		ProxyListPath: os.Getenv("PROXY_LIST_PATH"),
		ProxiesInline: os.Getenv("PROXIES"),
	}
	return c
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

// atoiOrDefault parses a non-negative int, returning def only when the input
// is empty or non-numeric. Unlike atoiOr it accepts an explicit 0 (e.g.
// SCRAPE_MAX_RETRIES=0 must mean "no retries", not "use the default").
func atoiOrDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// atoiOrZero parses an env var and returns 0 if empty or invalid.
// Callers treat 0 as "no limit".
func atoiOrZero(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
