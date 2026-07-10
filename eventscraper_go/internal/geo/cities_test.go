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

// TestShippedCatalog validates the real configs/cities.yaml that ships with
// the binary: it must parse, and every entry needs a unique id, a name, a
// country and non-zero coordinates.
func TestShippedCatalog(t *testing.T) {
	cat, err := Load(filepath.Join("..", "..", "configs", "cities.yaml"))
	if err != nil {
		t.Fatalf("Load shipped catalog: %v", err)
	}
	all := cat.All()
	if len(all) < 100 {
		t.Fatalf("shipped catalog has %d cities, expected 100+", len(all))
	}
	seen := map[string]bool{}
	for _, c := range all {
		if seen[c.ID] {
			t.Errorf("duplicate city id %q", c.ID)
		}
		seen[c.ID] = true
		if c.Name == "" || c.Country == "" || c.Lat == 0 || c.Lon == 0 {
			t.Errorf("incomplete city entry %q: %+v", c.ID, c)
		}
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

func TestNearest(t *testing.T) {
	cat, err := Load(writeTempYAML(t, sampleYAML))
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}

	// A point in Sintra (~25km west of Lisbon) must resolve to Lisbon.
	city, km, ok := cat.Nearest(38.8029, -9.3817)
	if !ok || city.ID != "lisbon" {
		t.Fatalf("Nearest(Sintra) = %q ok=%v, want lisbon", city.ID, ok)
	}
	if km < 5 || km > 50 {
		t.Errorf("Nearest(Sintra) distance = %.1fkm, want ~25", km)
	}

	// Potsdam is right next to Berlin.
	if city, _, _ := cat.Nearest(52.3906, 13.0645); city.ID != "berlin" {
		t.Errorf("Nearest(Potsdam) = %q, want berlin", city.ID)
	}

	// Empty catalog reports no match.
	empty := &Catalog{}
	if _, _, ok := empty.Nearest(0, 0); ok {
		t.Error("Nearest on empty catalog should report ok=false")
	}
}

func TestRankedByDistance(t *testing.T) {
	cat, err := Load(writeTempYAML(t, sampleYAML))
	if err != nil {
		t.Fatalf("Load err: %v", err)
	}
	ranked := cat.RankedByDistance(38.8029, -9.3817) // Sintra
	if len(ranked) != 2 {
		t.Fatalf("ranked len = %d, want 2", len(ranked))
	}
	if ranked[0].City.ID != "lisbon" || ranked[1].City.ID != "berlin" {
		t.Errorf("order = %s, %s; want lisbon, berlin", ranked[0].City.ID, ranked[1].City.ID)
	}
	if ranked[0].Km >= ranked[1].Km {
		t.Errorf("distances not ascending: %v", ranked)
	}
	// First entry must agree with Nearest.
	near, km, _ := cat.Nearest(38.8029, -9.3817)
	if near.ID != ranked[0].City.ID || km != ranked[0].Km {
		t.Errorf("Nearest disagrees with RankedByDistance[0]")
	}
}

func TestKmBetween(t *testing.T) {
	// Lisbon → Porto is ~274km as the crow flies.
	d := KmBetween(38.7223, -9.1393, 41.1579, -8.6291)
	if d < 260 || d > 290 {
		t.Errorf("Lisbon-Porto = %.1fkm, want ~274", d)
	}
	if d := KmBetween(38.7, -9.1, 38.7, -9.1); d != 0 {
		t.Errorf("zero distance = %f", d)
	}
}
