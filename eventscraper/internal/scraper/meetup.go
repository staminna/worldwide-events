package scraper

import (
	"context"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

// Meetup removed their free public REST API. The GraphQL endpoint at
// https://api.meetup.com/gql requires a Meetup OAuth client (paid for most
// useful queries). We keep the interface wired so callers can add credentials
// later; for now we return ErrUnconfigured unless MEETUP_OAUTH_TOKEN is set.
type Meetup struct {
	OAuthToken string
}

func NewMeetup(token string) *Meetup { return &Meetup{OAuthToken: token} }

func (m *Meetup) Source() model.Source { return model.SourceMeetup }

func (m *Meetup) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	if m.OAuthToken == "" {
		return nil, ErrUnconfigured
	}
	// TODO: POST to https://api.meetup.com/gql with a `keywordSearch` query
	// over upcoming events filtered by `eventType: PHYSICAL` and the city's
	// lat/lon radius. Map to model.Event. Requires Meetup OAuth dance.
	return nil, ErrUnconfigured
}
