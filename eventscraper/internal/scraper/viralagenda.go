package scraper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

// Viralagenda scrapes viralagenda.com/pt, Portugal's cultural events agenda.
// Each district page (e.g. /pt/lisboa) embeds a JSON-LD @graph of schema.org
// Event objects, which is far more stable than the page markup, so we parse
// that instead of the HTML.
type Viralagenda struct {
	HTTP *http.Client
}

func NewViralagenda() *Viralagenda {
	return &Viralagenda{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (v *Viralagenda) Source() model.Source { return model.SourceViralagenda }

const viralagendaBaseURL = "https://www.viralagenda.com/pt/"

// viralagendaSlugOverride maps city ids whose viralagenda district slug differs
// from the id itself. New Portuguese cities are added with their slug as the id
// (e.g. id: aveiro), so they need no entry here.
var viralagendaSlugOverride = map[string]string{
	"lisbon": "lisboa",
}

// viralagendaSlug returns the district slug to scrape for a city, or "" if the
// city is not covered (viralagenda is Portugal-only).
func viralagendaSlug(city geo.City) string {
	if city.Country != "PT" {
		return ""
	}
	if s, ok := viralagendaSlugOverride[city.ID]; ok {
		return s
	}
	return city.ID
}

func (v *Viralagenda) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	slug := viralagendaSlug(city)
	if slug == "" {
		return nil, nil
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", viralagendaBaseURL+slug, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; eventscraper/1.0; +https://github.com/jorgenunes/eventscraper)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "pt-PT,pt;q=0.9,en;q=0.8")

	resp, err := v.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, ErrBlocked
	}
	if resp.StatusCode >= 400 {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	nodes := parseViralagendaLD(body)
	out := make([]model.Event, 0, len(nodes))
	for _, n := range nodes {
		ev, ok := viralagendaToEvent(n, city)
		if !ok {
			continue
		}
		if len(cats) > 0 && !slices.Contains(cats, ev.Category) {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

var viralagendaLDRe = regexp.MustCompile(`(?s)<script[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)

// parseViralagendaLD extracts schema.org Event nodes from every JSON-LD block on
// the page. Each @graph node is decoded independently so one malformed node
// cannot discard the rest.
func parseViralagendaLD(html []byte) []viralagendaNode {
	var out []viralagendaNode
	for _, m := range viralagendaLDRe.FindAllSubmatch(html, -1) {
		var doc struct {
			Graph []json.RawMessage `json:"@graph"`
		}
		if err := json.Unmarshal(m[1], &doc); err != nil || len(doc.Graph) == 0 {
			continue
		}
		for _, raw := range doc.Graph {
			var n viralagendaNode
			if err := json.Unmarshal(raw, &n); err != nil {
				continue
			}
			if !n.Type.contains("Event") {
				continue
			}
			out = append(out, n)
		}
	}
	return out
}

type viralagendaNode struct {
	Type        flexStrings `json:"@type"`
	Name        string      `json:"name"`
	URL         string      `json:"url"`
	StartDate   string      `json:"startDate"`
	EndDate     string      `json:"endDate"`
	Image       flexStrings `json:"image"`
	Description string      `json:"description"`
	Location    struct {
		Name    string `json:"name"`
		Address struct {
			StreetAddress   string `json:"streetAddress"`
			AddressLocality string `json:"addressLocality"`
			AddressCountry  string `json:"addressCountry"`
		} `json:"address"`
	} `json:"location"`
}

// flexStrings decodes a JSON field that may be a string or an array of strings
// (schema.org allows both for @type, image, etc.).
type flexStrings []string

func (f *flexStrings) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = []string{s}
		return nil
	}
	var a []string
	if err := json.Unmarshal(b, &a); err == nil {
		*f = a
		return nil
	}
	return nil // ignore objects/other shapes rather than failing the node
}

func (f flexStrings) first() string {
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

func (f flexStrings) contains(sub string) bool {
	for _, s := range f {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

var viralagendaIDRe = regexp.MustCompile(`/events/(\d+)`)

func viralagendaToEvent(n viralagendaNode, city geo.City) (model.Event, bool) {
	if n.Name == "" || n.StartDate == "" || n.URL == "" {
		return model.Event{}, false
	}
	starts, ok := parseViralagendaTime(n.StartDate)
	if !ok {
		return model.Event{}, false
	}
	var endsPtr *time.Time
	if t, ok := parseViralagendaTime(n.EndDate); ok {
		endsPtr = &t
	}

	sourceID := n.URL
	if m := viralagendaIDRe.FindStringSubmatch(n.URL); m != nil {
		sourceID = m[1]
	}

	cityName := city.Name
	if loc := n.Location.Address.AddressLocality; loc != "" {
		cityName = loc
	}
	country := city.Country
	if c := n.Location.Address.AddressCountry; c != "" {
		country = c
	}

	// viralagenda's JSON-LD carries no per-venue coordinates, so fall back to
	// the district's catalog coordinates. Without this the events would be
	// dropped from the map, which only plots venues with non-zero lat/lon.
	return model.Event{
		ID:       model.MakeID(model.SourceViralagenda, sourceID),
		Source:   model.SourceViralagenda,
		SourceID: sourceID,
		Title:    n.Name,
		Category: viralagendaCategory(n),
		StartsAt: starts.UTC(),
		EndsAt:   endsPtr,
		Venue: model.Venue{
			Name:    n.Location.Name,
			Address: n.Location.Address.StreetAddress,
			Lat:     city.Lat,
			Lon:     city.Lon,
		},
		City:      cityName,
		Country:   country,
		URL:       n.URL,
		ImageURL:  n.Image.first(),
		ScrapedAt: time.Now().UTC(),
	}, true
}

func parseViralagendaTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// viralagendaCategory maps a cultural event onto this project's three buckets.
// viralagenda has no tech/business taxonomy of its own, so we infer from the
// schema.org @type and the title/description, defaulting to music — which is
// this project's arts-and-culture catch-all (see luma.go's category mapping).
func viralagendaCategory(n viralagendaNode) model.Category {
	typ := strings.ToLower(strings.Join(n.Type, " "))
	text := strings.ToLower(n.Name + " " + n.Description)
	switch {
	case strings.Contains(typ, "business") || strings.Contains(typ, "education") ||
		containsAny(text, "conferência", "conferencia", "conference", "summit", "workshop",
			"networking", "negócios", "negocios", "empreend", "startup", "masterclass"):
		return model.CategoryBusiness
	case containsAny(text, "tech", "developer", "hackathon", "programaç", "programac",
		"código", "codigo", "robót", "robot", "inteligência artificial", "data science"):
		return model.CategoryTech
	default:
		return model.CategoryMusic
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
