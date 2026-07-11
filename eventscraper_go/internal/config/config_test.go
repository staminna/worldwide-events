package config

import (
	"strings"
	"testing"
)

func TestAtoiOr(t *testing.T) {
	cases := []struct {
		in   string
		def  int
		want int
	}{
		{"", 7, 7},
		{"0", 7, 7},
		{"abc", 7, 7},
		{"12", 7, 12},
		{"12a", 7, 7},
		{"  4", 7, 7},
		{"99", 1, 99},
	}
	for _, c := range cases {
		if got := atoiOr(c.in, c.def); got != c.want {
			t.Errorf("atoiOr(%q,%d) = %d, want %d", c.in, c.def, got, c.want)
		}
	}
}

func TestAtoiOrZero(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"0", 0},
		{"abc", 0},
		{"5", 5},
		{"10x", 0},
		{"123", 123},
	}
	for _, c := range cases {
		if got := atoiOrZero(c.in); got != c.want {
			t.Errorf("atoiOrZero(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestGetenv(t *testing.T) {
	t.Setenv("ESC_TEST_VAR", "hello")
	if got := getenv("ESC_TEST_VAR", "fallback"); got != "hello" {
		t.Errorf("getenv set value = %q, want hello", got)
	}
	t.Setenv("ESC_TEST_VAR", "")
	if got := getenv("ESC_TEST_VAR", "fallback"); got != "fallback" {
		t.Errorf("getenv empty value = %q, want fallback", got)
	}
}

func TestFromEnvDefaults(t *testing.T) {
	// Clear any inherited values so we exercise the defaults path.
	for _, k := range []string{
		"PORT", "DB_PATH", "CITIES_PATH", "ADMIN_TOKEN",
		"TICKETMASTER_API_KEY", "MEETUP_OAUTH_TOKEN",
		"ALLOWED_ORIGIN", "FREE_ONLY", "WARMUP_CITIES",
	} {
		t.Setenv(k, "")
	}
	c := FromEnv()
	if c.Port != "8080" {
		t.Errorf("Port = %q, want 8080", c.Port)
	}
	if !strings.HasSuffix(c.DBPath, "eventscraper.db") {
		t.Errorf("DBPath = %q, want suffix eventscraper.db", c.DBPath)
	}
	if !strings.HasSuffix(c.CitiesPath, "cities.yaml") {
		t.Errorf("CitiesPath = %q, want suffix cities.yaml", c.CitiesPath)
	}
	if c.AllowedOrigin != "*" {
		t.Errorf("AllowedOrigin = %q, want *", c.AllowedOrigin)
	}
	if !c.FreeOnly {
		t.Errorf("FreeOnly = false, want true (default)")
	}
	if c.WarmupCities != 0 {
		t.Errorf("WarmupCities = %d, want 0", c.WarmupCities)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DB_PATH", "/tmp/x.db")
	t.Setenv("CITIES_PATH", "/tmp/c.yaml")
	t.Setenv("ADMIN_TOKEN", "tok")
	t.Setenv("TICKETMASTER_API_KEY", "tmk")
	t.Setenv("MEETUP_OAUTH_TOKEN", "mtk")
	t.Setenv("ALLOWED_ORIGIN", "https://example.com")
	t.Setenv("FREE_ONLY", "false")
	t.Setenv("WARMUP_CITIES", "12")

	c := FromEnv()
	if c.Port != "9090" {
		t.Errorf("Port = %q", c.Port)
	}
	if c.DBPath != "/tmp/x.db" {
		t.Errorf("DBPath = %q", c.DBPath)
	}
	if c.CitiesPath != "/tmp/c.yaml" {
		t.Errorf("CitiesPath = %q", c.CitiesPath)
	}
	if c.AdminToken != "tok" {
		t.Errorf("AdminToken = %q", c.AdminToken)
	}
	if c.TicketmasterKey != "tmk" {
		t.Errorf("TicketmasterKey = %q", c.TicketmasterKey)
	}
	if c.MeetupOAuthToken != "mtk" {
		t.Errorf("MeetupOAuthToken = %q", c.MeetupOAuthToken)
	}
	if c.AllowedOrigin != "https://example.com" {
		t.Errorf("AllowedOrigin = %q", c.AllowedOrigin)
	}
	if c.FreeOnly {
		t.Errorf("FreeOnly = true, want false")
	}
	if c.WarmupCities != 12 {
		t.Errorf("WarmupCities = %d, want 12", c.WarmupCities)
	}
}
