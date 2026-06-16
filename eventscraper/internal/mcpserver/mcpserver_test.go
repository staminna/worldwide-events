package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
	"github.com/jorgenunes/eventscraper/internal/scraper"
	"github.com/jorgenunes/eventscraper/internal/store"
)

const citiesYAML = `cities:
  - id: lisbon
    name: Lisbon
    country: PT
    lat: 38.7
    lon: -9.14
`

func newCatalog(t *testing.T) *geo.Catalog {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte(citiesYAML), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	cat, err := geo.Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cat
}

func newStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.NewSQLite(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	if err := st.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// decodeStructured re-marshals the (interface-typed) structured content from a
// tool result back into a typed value.
func decodeStructured(t *testing.T, v any, dst any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(b, dst); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
}

// connect wires an in-memory client to the mcpserver and returns the session.
func connect(t *testing.T, st store.Store, cat *geo.Catalog, reg *scraper.Registry) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()

	srv := New(st, cat, reg)
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestListTools(t *testing.T) {
	cs := connect(t, newStore(t), newCatalog(t), scraper.NewRegistry())

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"search_events", "list_cities", "list_sources"} {
		if !got[want] {
			t.Errorf("missing tool %q; got %v", want, got)
		}
	}
}

func TestSearchEvents(t *testing.T) {
	st := newStore(t)
	ev := model.Event{
		Source: model.SourceLuma, SourceID: "abc", Title: "GoLisbon Meetup",
		Category: model.CategoryTech, City: "Lisbon", Country: "PT",
		URL: "https://example.com/e", StartsAt: time.Now().Add(24 * time.Hour),
	}
	ev.ID = model.MakeID(ev.Source, ev.SourceID)
	if err := st.UpsertEvents(context.Background(), []model.Event{ev}); err != nil {
		t.Fatalf("UpsertEvents: %v", err)
	}

	cs := connect(t, st, newCatalog(t), scraper.NewRegistry())

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_events",
		Arguments: map[string]any{"city": "Lisbon", "category": "tech"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool returned error: %v", res.Content)
	}

	var out SearchEventsOutput
	decodeStructured(t, res.StructuredContent, &out)
	if out.Count != 1 || len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got count=%d len=%d", out.Count, len(out.Events))
	}
	if out.Events[0].Title != "GoLisbon Meetup" {
		t.Errorf("unexpected title %q", out.Events[0].Title)
	}
}

func TestSearchEventsInvalidCategory(t *testing.T) {
	cs := connect(t, newStore(t), newCatalog(t), scraper.NewRegistry())

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "search_events",
		Arguments: map[string]any{"category": "bogus"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error for invalid category")
	}
}

func TestListSourcesReflectsRegistry(t *testing.T) {
	reg := scraper.NewRegistry()
	reg.Register(scraper.NewLuma()) // only luma enabled

	cs := connect(t, newStore(t), newCatalog(t), reg)

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_sources"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ListSourcesOutput
	decodeStructured(t, res.StructuredContent, &out)
	enabled := map[model.Source]bool{}
	for _, s := range out.Sources {
		enabled[s.Source] = s.Enabled
	}
	if !enabled[model.SourceLuma] {
		t.Errorf("expected luma enabled")
	}
	if enabled[model.SourceEventbrite] {
		t.Errorf("expected eventbrite disabled")
	}
}
