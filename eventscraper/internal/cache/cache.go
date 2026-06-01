package cache

import (
	"context"
	"sync"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

func TTL(cat model.Category) time.Duration {
	switch cat {
	case model.CategoryMusic:
		return 12 * time.Hour
	default:
		return 6 * time.Hour
	}
}

type SingleFlight struct {
	mu      sync.Mutex
	running map[string]chan struct{}
}

func NewSingleFlight() *SingleFlight {
	return &SingleFlight{running: map[string]chan struct{}{}}
}

func (sf *SingleFlight) Do(ctx context.Context, key string, fn func(context.Context) error) error {
	sf.mu.Lock()
	if ch, ok := sf.running[key]; ok {
		sf.mu.Unlock()
		select {
		case <-ch:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ch := make(chan struct{})
	sf.running[key] = ch
	sf.mu.Unlock()

	defer func() {
		sf.mu.Lock()
		delete(sf.running, key)
		sf.mu.Unlock()
		close(ch)
	}()
	return fn(ctx)
}
