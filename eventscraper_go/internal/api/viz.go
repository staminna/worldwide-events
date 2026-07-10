package api

import (
	_ "embed"
	"net/http"
)

// The viz page is a self-contained kepler.gl app that feeds on the sibling
// /events.geojson endpoint (fetched with a relative URL so it works behind
// reverse-proxy path prefixes). Embedded so deploying the binary is enough.
//
//go:embed static/viz.html
var vizHTML []byte

func (s *Server) handleViz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(vizHTML)
}
