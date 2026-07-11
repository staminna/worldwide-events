// Package mcpserver exposes the eventscraper's read-only data over the Model
// Context Protocol, so MCP clients (Claude, IDEs, etc.) can search scraped
// events and inspect the configured cities and sources.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

// Version is reported to MCP clients as the server implementation version.
const Version = "v1.0.0"

// New builds an MCP server that exposes the scraper's data as tools. The
// returned server still needs to be run over a transport, e.g.
//
//	srv.Run(ctx, &mcp.StdioTransport{})
//
// runsURL + adminToken configure the scrape_status tool, which fetches the
// backend's private /runs.json.
func New(st store.Store, cat *geo.Catalog, reg *scraper.Registry, runsURL, adminToken string) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "eventscraper", Version: Version}, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_events",
		Description: "Search scraped events by city, category, source and/or free text. Returns matching events plus the total number available before the limit was applied.",
	}, searchEvents(st, cat))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_cities",
		Description: "List every city the scraper is configured to cover.",
	}, listCities(cat))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_sources",
		Description: "List every event source and whether it is currently enabled.",
	}, listSources(reg))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "scrape_status",
		Description: "Live scrape run status from the eventscraper backend: warmup progress, currently active runs, recent runs, and session totals (events found, blocked, errors). Ask again for fresh numbers.",
	}, scrapeStatus(runsURL, adminToken))

	return srv
}

// --- scrape_status ---

var statusClient = &http.Client{Timeout: 10 * time.Second}

// ScrapeStatusInput optionally trims how many recent runs are returned.
type ScrapeStatusInput struct {
	RecentLimit int `json:"recentLimit,omitempty" jsonschema:"max recent runs to include (default 15)"`
}

// runsSnapshot mirrors scheduler.Snapshot's JSON shape (kept local to avoid a
// dependency on the scheduler package).
type runsSnapshot struct {
	Totals runsTotals `json:"totals"`
	Active []runsView `json:"active"`
	Recent []runsView `json:"recent"`
}

type runsTotals struct {
	Plan        int `json:"plan"`
	Done        int `json:"done"`
	Active      int `json:"active"`
	EventsFound int `json:"eventsFound"`
	Blocked     int `json:"blocked"`
	Errors      int `json:"errors"`
}

type runsView struct {
	Source     string `json:"source"`
	City       string `json:"city"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Count      int    `json:"count"`
	Status     string `json:"status,omitempty"`
	Err        string `json:"err,omitempty"`
}

func scrapeStatus(runsURL, token string) mcp.ToolHandlerFor[ScrapeStatusInput, runsSnapshot] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ScrapeStatusInput) (*mcp.CallToolResult, runsSnapshot, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, runsURL, nil)
		if err != nil {
			return nil, runsSnapshot{}, fmt.Errorf("build request: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := statusClient.Do(req)
		if err != nil {
			return nil, runsSnapshot{}, fmt.Errorf("fetch %s: %w", runsURL, err)
		}
		defer resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, runsSnapshot{}, fmt.Errorf("unauthorized (check ADMIN_TOKEN for %s)", runsURL)
		case resp.StatusCode != http.StatusOK:
			return nil, runsSnapshot{}, fmt.Errorf("runs endpoint returned %d", resp.StatusCode)
		}
		var snap runsSnapshot
		if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
			return nil, runsSnapshot{}, fmt.Errorf("decode runs snapshot: %w", err)
		}
		limit := in.RecentLimit
		if limit <= 0 {
			limit = 15
		}
		if len(snap.Recent) > limit {
			snap.Recent = snap.Recent[:limit]
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: formatSnapshot(snap)}},
		}, snap, nil
	}
}

func formatSnapshot(s runsSnapshot) string {
	var b strings.Builder
	t := s.Totals
	if t.Plan > 0 {
		pct := t.Done * 100 / t.Plan
		fmt.Fprintf(&b, "Warmup: %d%% (%d/%d units)\n", pct, t.Done, t.Plan)
	} else {
		fmt.Fprintf(&b, "Runs done this session: %d\n", t.Done)
	}
	fmt.Fprintf(&b, "Active: %d · Events found: %d · Blocked: %d · Errors: %d\n",
		t.Active, t.EventsFound, t.Blocked, t.Errors)
	if len(s.Active) > 0 {
		b.WriteString("\nActive now:\n")
		for _, r := range s.Active {
			fmt.Fprintf(&b, "  • %s / %s (since %s)\n", r.Source, r.City, r.StartedAt)
		}
	}
	if len(s.Recent) > 0 {
		b.WriteString("\nRecent:\n")
		for _, r := range s.Recent {
			line := fmt.Sprintf("  • %s / %s — %d events [%s]", r.Source, r.City, r.Count, r.Status)
			if r.Err != "" {
				line += " " + r.Err
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// SearchEventsInput mirrors the filterable fields of store.Query. Empty fields
// are treated as "no filter".
type SearchEventsInput struct {
	City        string `json:"city,omitempty"        jsonschema:"filter by city name, e.g. London"`
	Category    string `json:"category,omitempty"    jsonschema:"one of: tech, music, arts, business"`
	Source      string `json:"source,omitempty"      jsonschema:"one of: eventbrite, songkick, luma, ticketmaster, meetup"`
	Search      string `json:"search,omitempty"      jsonschema:"free-text query over event title and description"`
	Limit       int    `json:"limit,omitempty"       jsonschema:"maximum events to return (1-100, default 20)"`
	IncludePast bool   `json:"includePast,omitempty" jsonschema:"include events that already ended (default false: only upcoming and ongoing events, soonest first)"`
}

// SearchEventsOutput reports the matched events along with the total count
// available before the limit was applied.
type SearchEventsOutput struct {
	Count  int           `json:"count"`
	Total  int           `json:"total"`
	Events []model.Event `json:"events"`
}

// resolveCity maps free-text city input onto a catalog city ID ("London",
// "new york" → "new-york"). Returns "" when nothing in the catalog matches.
func resolveCity(cat *geo.Catalog, input string) string {
	slug := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(input)), " ", "-")
	if c, ok := cat.Get(slug); ok {
		return c.ID
	}
	for _, c := range cat.All() {
		if strings.EqualFold(c.Name, input) {
			return c.ID
		}
	}
	return ""
}

func searchEvents(st store.Store, cat *geo.Catalog) mcp.ToolHandlerFor[SearchEventsInput, SearchEventsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchEventsInput) (*mcp.CallToolResult, SearchEventsOutput, error) {
		limit := in.Limit
		switch {
		case limit <= 0:
			limit = 20
		case limit > 100:
			limit = 100
		}

		catFilter := model.Category(in.Category)
		if in.Category != "" && !catFilter.Valid() {
			return nil, SearchEventsOutput{}, fmt.Errorf("invalid category %q", in.Category)
		}
		src := model.Source(in.Source)
		if in.Source != "" && !src.Valid() {
			return nil, SearchEventsOutput{}, fmt.Errorf("invalid source %q", in.Source)
		}

		q := store.Query{
			Category: catFilter,
			Source:   src,
			Search:   in.Search,
			Limit:    limit,
		}
		if in.City != "" {
			// Prefer the catalog city ID (matches everything scraped for
			// that city regardless of venue spelling); fall back to the
			// stored display city for places not in the catalog.
			if id := resolveCity(cat, in.City); id != "" {
				q.CityID = id
			} else {
				q.City = in.City
			}
		}
		if !in.IncludePast {
			q.NotEndedBefore = time.Now().UTC()
		}
		events, total, _, err := st.Query(ctx, q)
		if err != nil {
			return nil, SearchEventsOutput{}, fmt.Errorf("query events: %w", err)
		}
		return nil, SearchEventsOutput{Count: len(events), Total: total, Events: events}, nil
	}
}

// ListCitiesOutput is the result of the list_cities tool.
type ListCitiesOutput struct {
	Count  int        `json:"count"`
	Cities []geo.City `json:"cities"`
}

func listCities(cat *geo.Catalog) mcp.ToolHandlerFor[struct{}, ListCitiesOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListCitiesOutput, error) {
		cities := cat.All()
		return nil, ListCitiesOutput{Count: len(cities), Cities: cities}, nil
	}
}

// SourceStatus reports whether a given source is wired up in the registry.
type SourceStatus struct {
	Source  model.Source `json:"source"`
	Enabled bool         `json:"enabled"`
}

// ListSourcesOutput is the result of the list_sources tool.
type ListSourcesOutput struct {
	Sources []SourceStatus `json:"sources"`
}

func listSources(reg *scraper.Registry) mcp.ToolHandlerFor[struct{}, ListSourcesOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListSourcesOutput, error) {
		all := model.AllSources()
		out := ListSourcesOutput{Sources: make([]SourceStatus, 0, len(all))}
		for _, src := range all {
			_, ok := reg.Get(src)
			out.Sources = append(out.Sources, SourceStatus{Source: src, Enabled: ok})
		}
		return nil, out, nil
	}
}
