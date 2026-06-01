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
)

func (s Source) Valid() bool {
	switch s {
	case SourceEventbrite, SourceSongkick, SourceLuma, SourceTicketmaster, SourceMeetup:
		return true
	}
	return false
}

func AllSources() []Source {
	return []Source{SourceEventbrite, SourceSongkick, SourceLuma, SourceTicketmaster, SourceMeetup}
}

type Category string

const (
	CategoryTech     Category = "tech"
	CategoryMusic    Category = "music"
	CategoryBusiness Category = "business"
)

func (c Category) Valid() bool {
	switch c {
	case CategoryTech, CategoryMusic, CategoryBusiness:
		return true
	}
	return false
}

func AllCategories() []Category {
	return []Category{CategoryTech, CategoryMusic, CategoryBusiness}
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
	Country     string     `json:"country"`
	URL         string     `json:"url"`
	ImageURL    string     `json:"imageUrl,omitempty"`
	Price       *Price     `json:"price,omitempty"`
	ScrapedAt   time.Time  `json:"scrapedAt"`
}

func MakeID(src Source, sourceID string) string {
	h := sha1.Sum([]byte(string(src) + "|" + sourceID))
	return hex.EncodeToString(h[:])
}
