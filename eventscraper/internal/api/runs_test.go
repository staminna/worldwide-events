package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jorgenunes/eventscraper/internal/scheduler"
)

func TestHandleRunsServesHTML(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleRuns(w, httptest.NewRequest(http.MethodGet, "/runs", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(w.Body.String(), "Scrape Runs") {
		t.Errorf("runs page body missing expected title")
	}
}

func TestHandleRunsJSONShape(t *testing.T) {
	sch := scheduler.New(nil, nil, nil) // Snapshot only reads the tracker
	s := &Server{scheduler: sch}
	w := httptest.NewRecorder()
	s.handleRunsJSON(w, httptest.NewRequest(http.MethodGet, "/runs.json", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var snap scheduler.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("runs.json not valid Snapshot JSON: %v", err)
	}
	if snap.Totals.Active != 0 || len(snap.Active) != 0 || len(snap.Recent) != 0 {
		t.Errorf("fresh scheduler snapshot should be empty, got %+v", snap.Totals)
	}
}
