package model

import (
	"crypto/sha1"
	"encoding/hex"
	"time"
)

type Source string

const (
	SourceEventbrite   Source = "eventbrite"
	SourceSongkick     Source = "songkick"
	SourceLuma         Source = "luma"
	SourceTicketmaster Source = "ticketmaster"
	SourceMeetup       Source = "meetup"
	SourceViralagenda  Source = "viralagenda"
)

func (s Source) Valid() bool {
	switch s {
	case SourceEventbrite, SourceSongkick, SourceLuma, SourceTicketmaster, SourceMeetup, SourceViralagenda:
		return true
	}
	return false
}

func AllSources() []Source {
	return []Source{SourceEventbrite, SourceSongkick, SourceLuma, SourceTicketmaster, SourceMeetup, SourceViralagenda}
}

type Category string

const (
	CategoryTech     Category = "tech"
	CategoryMusic    Category = "music"
	CategoryArts     Category = "arts"
	CategoryBusiness Category = "business"
)

func (c Category) Valid() bool {
	switch c {
	case CategoryTech, CategoryMusic, CategoryArts, CategoryBusiness:
		return true
	}
	return false
}

func AllCategories() []Category {
	return []Category{CategoryTech, CategoryMusic, CategoryArts, CategoryBusiness}
}

type Venue struct {
	Name    string  `json:"name,omitempty"`
	Address string  `json:"address,omitempty"`
	Lat     float64 `json:"lat,omitempty"`
	Lon     float64 `json:"lon,omitempty"`
}

type Price struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Currency string  `json:"currency"`
	Free     bool    `json:"free"`
}

type Event struct {
	ID          string     `json:"id"`
	Source      Source     `json:"source"`
	SourceID    string     `json:"sourceId"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Category    Category   `json:"category"`
	StartsAt    time.Time  `json:"startsAt"`
	EndsAt      *time.Time `json:"endsAt,omitempty"`
	Venue       Venue      `json:"venue"`
	City        string     `json:"city"`
	// CityID is the catalog city the event was scraped for (see
	// configs/cities.yaml), while City holds the venue's own locality for
	// display — e.g. a Carnaxide event scraped for Lisbon keeps City
	// "Carnaxide" with CityID "lisbon". Queries filter on CityID so venue
	// spellings ("Lisboa", "Lisbon") don't fragment a city's feed.
	CityID    string    `json:"cityId,omitempty"`
	Country   string    `json:"country"`
	URL       string    `json:"url"`
	ImageURL  string    `json:"imageUrl,omitempty"`
	Price     *Price    `json:"price,omitempty"`
	ScrapedAt time.Time `json:"scrapedAt"`
}

func MakeID(src Source, sourceID string) string {
	h := sha1.Sum([]byte(string(src) + "|" + sourceID))
	return hex.EncodeToString(h[:])
}
