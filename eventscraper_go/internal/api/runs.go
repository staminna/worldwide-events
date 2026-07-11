package api

import (
	_ "embed"
	"net/http"
)

// The runs dashboard is a self-contained page (no CDN) that polls the sibling
// /runs.json endpoint (relative URL, so it works behind a reverse-proxy path
// prefix). Embedded so deploying the binary is enough.
//
//go:embed static/runs.html
var runsHTML []byte

func (s *Server) handleRuns(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(runsHTML)
}

// handleRunsJSON returns the live scrape-run snapshot for the dashboard.
// Read-only and public, like the rest of the read API; it exposes no proxy
// URLs or credentials.
func (s *Server) handleRunsJSON(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.scheduler.Snapshot())
}
