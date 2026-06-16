package scraper

import (
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

// A trimmed copy of a real viralagenda.com/pt/lisboa JSON-LD block, with a
// second script tag and a non-event @graph node to exercise the filtering.
const viralagendaHTML = `<html><head>
<script type="application/ld+json">
{"@context":"https://schema.org","@graph":[
  {"@type":"Event","name":"ÉDOUARD LOUIS","url":"https://www.viralagenda.com/pt/events/1800632/edouard-louis","startDate":"2026-06-18T18:00:00+01:00","endDate":"2026-06-18","image":"https://cdn.viralagenda.com/img/a.jpg","location":{"@type":"Place","name":"São Luiz Teatro Municipal","address":{"@type":"PostalAddress","streetAddress":"Rua António Maria Cardoso, 38","addressLocality":"Lisboa","addressCountry":"PT"}},"description":"ÉDOUARD LOUIS @ São Luiz"},
  {"@type":["BusinessEvent"],"name":"Startup Summit 2026","url":"https://www.viralagenda.com/pt/events/1811999/startup-summit","startDate":"2026-07-01T09:00:00+01:00","image":["https://cdn.viralagenda.com/img/b.jpg"],"location":{"@type":"Place","name":"LX Factory","address":{"streetAddress":"Rua X","addressLocality":"Lisboa","addressCountry":"PT"}},"description":"networking conference"},
  {"@type":"Organization","name":"Not an event"}
]}
</script>
<script type="application/ld+json">{"@type":"WebSite","name":"Viral Agenda"}</script>
</head></html>`

func TestParseViralagendaLD(t *testing.T) {
	nodes := parseViralagendaLD([]byte(viralagendaHTML))
	if len(nodes) != 2 {
		t.Fatalf("got %d event nodes, want 2 (Organization/WebSite filtered out)", len(nodes))
	}
	if nodes[0].Name != "ÉDOUARD LOUIS" {
		t.Errorf("node[0].Name = %q", nodes[0].Name)
	}
	// @type given as an array must still be recognised as an event.
	if !nodes[1].Type.contains("Event") {
		t.Errorf("node[1] @type not recognised: %v", nodes[1].Type)
	}
	// image given as an array must yield the first URL.
	if got := nodes[1].Image.first(); got != "https://cdn.viralagenda.com/img/b.jpg" {
		t.Errorf("node[1].Image.first() = %q", got)
	}
}

func TestViralagendaToEvent(t *testing.T) {
	city := geo.City{ID: "lisbon", Name: "Lisbon", Country: "PT", Lat: 38.7223, Lon: -9.1393}
	nodes := parseViralagendaLD([]byte(viralagendaHTML))

	ev, ok := viralagendaToEvent(nodes[0], city)
	if !ok {
		t.Fatal("viralagendaToEvent !ok")
	}
	if ev.Source != model.SourceViralagenda || ev.SourceID != "1800632" {
		t.Errorf("source/id = %v/%q (want id parsed from URL)", ev.Source, ev.SourceID)
	}
	if ev.ID != model.MakeID(model.SourceViralagenda, "1800632") {
		t.Errorf("ID hash mismatch")
	}
	if !ev.StartsAt.Equal(time.Date(2026, 6, 18, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("StartsAt = %v (want 17:00 UTC from +01:00)", ev.StartsAt)
	}
	if ev.Category != model.CategoryMusic {
		t.Errorf("category = %q, want music (cultural catch-all)", ev.Category)
	}
	if ev.Venue.Name != "São Luiz Teatro Municipal" || ev.City != "Lisboa" {
		t.Errorf("venue/city = %q/%q", ev.Venue.Name, ev.City)
	}
	// No coords in JSON-LD → fall back to the district's catalog coordinates so
	// the event still plots on the map.
	if ev.Venue.Lat != 38.7223 || ev.Venue.Lon != -9.1393 {
		t.Errorf("venue coords = %v/%v, want city fallback", ev.Venue.Lat, ev.Venue.Lon)
	}

	// Business inference from @type + keywords.
	bz, _ := viralagendaToEvent(nodes[1], city)
	if bz.Category != model.CategoryBusiness {
		t.Errorf("category = %q, want business", bz.Category)
	}
}

func TestViralagendaToEventInvalid(t *testing.T) {
	city := geo.City{Name: "Lisbon", Country: "PT"}
	cases := []viralagendaNode{
		{Name: "", StartDate: "2026-06-18T18:00:00Z", URL: "u"}, // no name
		{Name: "n", StartDate: "", URL: "u"},                    // no start
		{Name: "n", StartDate: "2026-06-18T18:00:00Z", URL: ""}, // no url
		{Name: "n", StartDate: "garbage", URL: "u"},             // bad date
	}
	for i, n := range cases {
		if _, ok := viralagendaToEvent(n, city); ok {
			t.Errorf("case %d: expected !ok", i)
		}
	}
}

func TestViralagendaSlug(t *testing.T) {
	cases := []struct {
		city geo.City
		want string
	}{
		{geo.City{ID: "lisbon", Country: "PT"}, "lisboa"}, // override
		{geo.City{ID: "porto", Country: "PT"}, "porto"},   // id == slug
		{geo.City{ID: "faro", Country: "PT"}, "faro"},     // new district, id == slug
		{geo.City{ID: "london", Country: "GB"}, ""},       // non-PT, skipped
	}
	for _, c := range cases {
		if got := viralagendaSlug(c.city); got != c.want {
			t.Errorf("viralagendaSlug(%q/%q) = %q, want %q", c.city.ID, c.city.Country, got, c.want)
		}
	}
}
