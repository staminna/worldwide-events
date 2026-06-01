package geo

import (
	"fmt"
	"os"
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
