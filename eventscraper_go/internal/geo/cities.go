package geo

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type City struct {
	ID                 string  `yaml:"id"               json:"id"`
	Name               string  `yaml:"name"             json:"name"`
	Country            string  `yaml:"country"          json:"country"`
	Lat                float64 `yaml:"lat"              json:"lat"`
	Lon                float64 `yaml:"lon"              json:"lon"`
	EventbriteSlug     string  `yaml:"eventbrite_slug"  json:"-"`
	SongkickMetro      string  `yaml:"songkick_metro"   json:"-"`
	LumaCitySlug       string  `yaml:"luma_city_slug"   json:"-"`
	TicketmasterMarket int     `yaml:"ticketmaster_market" json:"-"`
}

// LumaSlug returns LumaCitySlug if set, otherwise falls back to the city ID
// (most of our IDs match lu.ma's canonical slugs already).
func (c City) LumaSlug() string {
	if c.LumaCitySlug != "" {
		return c.LumaCitySlug
	}
	return c.ID
}

// KmBetween returns the great-circle (haversine) distance in kilometres
// between two coordinates.
func KmBetween(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(a))
}

type fileShape struct {
	Cities []City `yaml:"cities"`
}

type Catalog struct {
	mu     sync.RWMutex
	cities []City
	byID   map[string]City
}

func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cities yaml: %w", err)
	}
	var fs fileShape
	if err := yaml.Unmarshal(data, &fs); err != nil {
		return nil, fmt.Errorf("parse cities yaml: %w", err)
	}
	c := &Catalog{
		cities: fs.Cities,
		byID:   make(map[string]City, len(fs.Cities)),
	}
	for _, city := range fs.Cities {
		c.byID[city.ID] = city
	}
	return c, nil
}

func (c *Catalog) All() []City {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]City, len(c.cities))
	copy(out, c.cities)
	return out
}

func (c *Catalog) Get(id string) (City, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.byID[strings.ToLower(id)]
	return v, ok
}

// CityDistance pairs a catalog city with its distance from a reference
// coordinate.
type CityDistance struct {
	City City    `json:"city"`
	Km   float64 `json:"km"`
}

// RankedByDistance returns every catalog city sorted by ascending distance
// from the given coordinate.
func (c *Catalog) RankedByDistance(lat, lon float64) []CityDistance {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]CityDistance, 0, len(c.cities))
	for _, city := range c.cities {
		out = append(out, CityDistance{City: city, Km: KmBetween(lat, lon, city.Lat, city.Lon)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Km < out[j].Km })
	return out
}

// Nearest returns the catalog city closest to the given coordinate and its
// distance in kilometres. ok is false only when the catalog is empty.
func (c *Catalog) Nearest(lat, lon float64) (City, float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	best := City{}
	bestKm := math.MaxFloat64
	for _, city := range c.cities {
		if d := KmBetween(lat, lon, city.Lat, city.Lon); d < bestKm {
			best, bestKm = city, d
		}
	}
	return best, bestKm, bestKm != math.MaxFloat64
}
