package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type geoJSONPoint struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"` // [lon, lat] per the GeoJSON spec
}

type geoJSONFeature struct {
	Type       string         `json:"type"`
	Geometry   geoJSONPoint   `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

// handleEventsGeoJSON exports the (filterable) event feed as a GeoJSON
// FeatureCollection for map visualizations like the /viz page. Only events
// with venue coordinates are included, and the image requirement is dropped
// because a dot on a map doesn't need a cover photo.
func (s *Server) handleEventsGeoJSON(w http.ResponseWriter, r *http.Request) {
	q, cityObj, err := parseQuery(r, s.cities)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	q.RequireImage = false
	q.RequireCoords = true
	if r.URL.Query().Get("limit") == "" {
		q.Limit = 2000
	}
	s.kickRefresh(cityObj, q.Category, q.Source)

	events, total, _, err := s.store.Query(r.Context(), q)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	features := make([]geoJSONFeature, 0, len(events))
	for _, e := range events {
		if e.Venue.Lat == 0 && e.Venue.Lon == 0 {
			continue
		}
		features = append(features, geoJSONFeature{
			Type: "Feature",
			Geometry: geoJSONPoint{
				Type:        "Point",
				Coordinates: [2]float64{e.Venue.Lon, e.Venue.Lat},
			},
			Properties: map[string]any{
				"id":        e.ID,
				"title":     e.Title,
				"category":  string(e.Category),
				"startsAt":  e.StartsAt.UTC().Format(time.RFC3339),
				"source":    string(e.Source),
				"venueName": e.Venue.Name,
				"city":      e.City,
				"country":   e.Country,
				"url":       e.URL,
				"free":      e.Price != nil && e.Price.Free,
			},
		})
	}
	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":     "FeatureCollection",
		"features": features,
		// Foreign member (allowed by RFC 7946): how many matched before the
		// limit, so viz clients can tell when they're seeing a truncation.
		"meta": map[string]any{"total": total, "returned": len(features)},
	})
}
