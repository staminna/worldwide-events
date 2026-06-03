package scraper

import (
	"context"
	"errors"
	"testing"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

type fakeScraper struct{ src model.Source }

func (f fakeScraper) Source() model.Source { return f.src }
func (f fakeScraper) Scrape(ctx context.Context, _ geo.City, _ []model.Category) ([]model.Event, error) {
	return nil, nil
}

func TestRegistryRegisterGetAll(t *testing.T) {
	r := NewRegistry()
	a := fakeScraper{src: model.SourceLuma}
	b := fakeScraper{src: model.SourceEventbrite}

	r.Register(a)
	r.Register(b)

	if got, ok := r.Get(model.SourceLuma); !ok || got.Source() != model.SourceLuma {
		t.Errorf("Get(luma) = %v ok=%v", got, ok)
	}
	if _, ok := r.Get(model.SourceMeetup); ok {
		t.Error("Get(meetup) should miss")
	}
	if len(r.All()) != 2 {
		t.Errorf("All() len = %d, want 2", len(r.All()))
	}

	// Register override on duplicate Source.
	c := fakeScraper{src: model.SourceLuma}
	r.Register(c)
	if len(r.All()) != 2 {
		t.Errorf("duplicate register should not grow registry, got %d", len(r.All()))
	}
}

func TestMeetupRequiresOAuth(t *testing.T) {
	m := NewMeetup("")
	_, err := m.Scrape(context.Background(), geo.City{ID: "lisbon", Name: "Lisbon"}, model.AllCategories())
	if !errors.Is(err, ErrUnconfigured) {
		t.Errorf("err = %v, want ErrUnconfigured", err)
	}
	if m.Source() != model.SourceMeetup {
		t.Errorf("Source() = %v, want meetup", m.Source())
	}

	// Even with a token, the stub still returns ErrUnconfigured (TODO in code).
	m = NewMeetup("tok")
	if _, err := m.Scrape(context.Background(), geo.City{}, nil); !errors.Is(err, ErrUnconfigured) {
		t.Errorf("err = %v, want ErrUnconfigured (stub)", err)
	}
}

func TestTicketmasterRequiresKey(t *testing.T) {
	tm := NewTicketmaster("")
	_, err := tm.Scrape(context.Background(), geo.City{Name: "Lisbon", Country: "PT"}, model.AllCategories())
	if !errors.Is(err, ErrUnconfigured) {
		t.Errorf("err = %v, want ErrUnconfigured", err)
	}
	if tm.Source() != model.SourceTicketmaster {
		t.Errorf("Source() = %v, want ticketmaster", tm.Source())
	}
}

func TestEventbriteNoSlugReturnsEmpty(t *testing.T) {
	eb := NewEventbrite()
	got, err := eb.Scrape(context.Background(), geo.City{ID: "x"}, model.AllCategories())
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d events, want 0 when city has no eventbrite slug", len(got))
	}
	if eb.Source() != model.SourceEventbrite {
		t.Errorf("Source() = %v, want eventbrite", eb.Source())
	}
}

func TestSongkickNoMetroReturnsEmpty(t *testing.T) {
	sk := NewSongkick()
	got, err := sk.Scrape(context.Background(), geo.City{ID: "x"}, []model.Category{model.CategoryMusic})
	if err != nil || len(got) != 0 {
		t.Errorf("expected empty result, got len=%d err=%v", len(got), err)
	}
	if sk.Source() != model.SourceSongkick {
		t.Errorf("Source() = %v, want songkick", sk.Source())
	}
}

func TestSongkickNonMusicCategoriesReturnEmpty(t *testing.T) {
	sk := NewSongkick()
	got, err := sk.Scrape(context.Background(),
		geo.City{ID: "x", SongkickMetro: "1234-pt-lisbon"},
		[]model.Category{model.CategoryTech, model.CategoryBusiness},
	)
	if err != nil || len(got) != 0 {
		t.Errorf("expected empty result for non-music cats, got len=%d err=%v", len(got), err)
	}
}

func TestLumaNoSlugReturnsEmpty(t *testing.T) {
	l := NewLuma()
	got, err := l.Scrape(context.Background(), geo.City{}, model.AllCategories())
	if err != nil || len(got) != 0 {
		t.Errorf("expected empty result, got len=%d err=%v", len(got), err)
	}
	if l.Source() != model.SourceLuma {
		t.Errorf("Source() = %v, want luma", l.Source())
	}
}
