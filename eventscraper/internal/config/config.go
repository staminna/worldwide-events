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
		FreeOnly:         getenv("FREE_ONLY", "true") != "false",
		// 0 means "all cities in the catalog".
		WarmupCities: atoiOrZero(os.Getenv("WARMUP_CITIES")),
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
