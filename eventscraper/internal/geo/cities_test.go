package geo

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleYAML = `cities:
  - id: berlin
    name: Berlin
    country: DE
    lat: 52.52
    lon: 13.405
    eventbrite_slug: germany--berlin
    songkick_metro: 28443-germany-berlin
    luma_city_slug: berlin
    ticketmaster_market: 0
  - id: lisbon
    name: Lisbon
    country: PT
    lat: 38.7223
    lon: -9.1393
    eventbrite_slug: portugal--lisbon
`

func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cities.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func TestLumaSlug(t *testing.T) {
	if got := (City{ID: "lisbon"}).LumaSlug(); got != "lisbon" {
		t.Errorf("LumaSlug fallback = %q, want lisbon", got)
	}
	if got := (City{ID: "lisbon", LumaCitySlug: "lis"}).LumaSlug(); got != "lis" {
		t.Errorf("LumaSlug override = %q, want lis", got)
	}
}

func TestLoadAndGet(t *testing.T) {
	path := writeTempYAML(t, sampleYAML)
	cat, err := Load(path)
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}
	all := cat.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d cities, want 2", len(all))
	}

	berlin, ok := cat.Get("berlin")
	if !ok {
		t.Fatal("Get(berlin) not found")
	}
	if berlin.Name != "Berlin" || berlin.Country != "DE" {
		t.Errorf("berlin = %+v", berlin)
	}
	if berlin.Lat != 52.52 || berlin.Lon != 13.405 {
		t.Errorf("berlin lat/lon = %v/%v", berlin.Lat, berlin.Lon)
	}
	if berlin.EventbriteSlug != "germany--berlin" {
		t.Errorf("eventbrite_slug = %q", berlin.EventbriteSlug)
	}

	// Get must be case-insensitive on the lookup key.
	if _, ok := cat.Get("BERLIN"); !ok {
		t.Error("Get(BERLIN) should match (case-insensitive)")
	}
	if _, ok := cat.Get("nowhere"); ok {
		t.Error("Get(nowhere) should miss")
	}

	// All() returns a copy — mutating it must not affect catalog.
	all[0].Name = "Mutated"
	again := cat.All()
	if again[0].Name == "Mutated" {
		t.Error("All() must return a defensive copy")
	}
}

func TestLoadFileErrors(t *testing.T) {
	if _, err := Load("/nonexistent/path/cities.yaml"); err == nil {
		t.Error("expected error for missing file")
	}
	// Unbalanced braces — guaranteed to fail YAML parse.
	path := writeTempYAML(t, "cities: [ { id: x, name: y\n  - oops")
	if _, err := Load(path); err == nil {
		t.Error("expected error for invalid yaml")
	}
}
