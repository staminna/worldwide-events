package scraper

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
	"github.com/gocolly/colly/v2/proxy"

	"github.com/jorgenunes/eventscraper/internal/geo"
	"github.com/jorgenunes/eventscraper/internal/model"
)

type Songkick struct {
	pool *ProxyPool
}

// NewSongkick builds the scraper. A non-empty proxy pool is round-robined
// across colly requests; a nil/empty pool means direct connections.
func NewSongkick(pool *ProxyPool) *Songkick { return &Songkick{pool: pool} }

func (s *Songkick) Source() model.Source { return model.SourceSongkick }

func (s *Songkick) Scrape(ctx context.Context, city geo.City, cats []model.Category) ([]model.Event, error) {
	if city.SongkickMetro == "" {
		return nil, nil
	}
	if len(cats) > 0 && !slices.Contains(cats, model.CategoryMusic) {
		// Songkick is music-only
		return nil, nil
	}
	url := fmt.Sprintf("https://www.songkick.com/metro-areas/%s", city.SongkickMetro)

	c := colly.NewCollector(
		colly.Async(true),
		colly.AllowedDomains("www.songkick.com", "songkick.com"),
	)
	if s.pool != nil && s.pool.Len() > 0 {
		if sw, err := proxy.RoundRobinProxySwitcher(s.pool.URLStrings()...); err == nil {
			c.SetProxyFunc(sw)
		}
	}
	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*songkick*",
		Parallelism: 4,
		Delay:       500 * time.Millisecond,
		RandomDelay: 800 * time.Millisecond,
	})

	var (
		mu      sync.Mutex
		events  []model.Event
		blocked bool
	)

	c.OnResponse(func(r *colly.Response) {
		if r.StatusCode == 403 || r.StatusCode == 429 {
			blocked = true
		}
	})

	c.OnHTML("li.event-listings-element", func(el *colly.HTMLElement) {
		datetime := el.ChildAttr("time", "datetime")
		if datetime == "" {
			return
		}
		starts, err := time.Parse(time.RFC3339, datetime)
		if err != nil {
			t, err2 := time.Parse("2006-01-02", datetime)
			if err2 != nil {
				return
			}
			starts = t
		}
		linkRel := el.ChildAttr("a.event-link", "href")
		if linkRel == "" {
			linkRel = el.ChildAttr("a", "href")
		}
		if linkRel == "" {
			return
		}
		url := linkRel
		if strings.HasPrefix(linkRel, "/") {
			url = "https://www.songkick.com" + linkRel
		}
		title := strings.TrimSpace(el.ChildText("p.artists strong"))
		if title == "" {
			title = strings.TrimSpace(el.ChildText("p.artists"))
		}
		if title == "" {
			title = strings.TrimSpace(el.ChildText(".summary"))
		}
		if title == "" {
			return
		}
		venue := strings.TrimSpace(el.ChildText("p.venue-name a"))
		if venue == "" {
			venue = strings.TrimSpace(el.ChildText(".venue-name"))
		}
		sourceID := url
		if i := strings.LastIndex(strings.TrimRight(url, "/"), "/"); i > 0 {
			sourceID = strings.TrimRight(url, "/")[i+1:]
		}
		ev := model.Event{
			ID:        model.MakeID(model.SourceSongkick, sourceID),
			Source:    model.SourceSongkick,
			SourceID:  sourceID,
			Title:     title,
			Category:  model.CategoryMusic,
			StartsAt:  starts.UTC(),
			Venue:     model.Venue{Name: venue},
			City:      city.Name,
			Country:   city.Country,
			URL:       url,
			ScrapedAt: time.Now().UTC(),
		}
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})

	c.OnRequest(func(r *colly.Request) {
		r.Headers.Set("User-Agent", RandomUA(nil))
		r.Headers.Set("Accept-Language", "en-US,en;q=0.9")
	})

	if err := c.Visit(url); err != nil {
		return events, nil
	}
	c.Wait()
	if blocked {
		return events, ErrBlocked
	}
	return events, nil
}
