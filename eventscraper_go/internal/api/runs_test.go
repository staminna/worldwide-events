package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jorgenunes/eventscraper/internal/config"
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

func TestRunsRequireAdmin(t *testing.T) {
	sch := scheduler.New(nil, nil, nil)

	do := func(s *Server, path, auth string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		s.Router().ServeHTTP(w, req)
		return w.Code
	}

	t.Run("token set: ops gated, public endpoints open", func(t *testing.T) {
		s := &Server{cfg: config.Config{AdminToken: "secret", AllowedOrigin: "*"}, scheduler: sch}
		if code := do(s, "/runs.json", ""); code != http.StatusUnauthorized {
			t.Errorf("/runs.json without token = %d, want 401", code)
		}
		if code := do(s, "/runs", "Bearer wrong"); code != http.StatusUnauthorized {
			t.Errorf("/runs with wrong token = %d, want 401", code)
		}
		if code := do(s, "/runs.json", "Bearer secret"); code != http.StatusOK {
			t.Errorf("/runs.json with token = %d, want 200", code)
		}
		// A public endpoint stays open with no token.
		if code := do(s, "/healthz", ""); code != http.StatusOK {
			t.Errorf("/healthz without token = %d, want 200 (must stay public)", code)
		}
	})

	t.Run("token unset: ops open (dev)", func(t *testing.T) {
		s := &Server{cfg: config.Config{AllowedOrigin: "*"}, scheduler: sch}
		if code := do(s, "/runs.json", ""); code != http.StatusOK {
			t.Errorf("/runs.json with no admin token = %d, want 200 (open in dev)", code)
		}
	})
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
