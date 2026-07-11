package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jorgenunes/eventscraper/internal/model"
)

func TestTTL(t *testing.T) {
	if got := TTL(model.CategoryMusic); got != 12*time.Hour {
		t.Errorf("TTL(music) = %v, want 12h", got)
	}
	if got := TTL(model.CategoryTech); got != 6*time.Hour {
		t.Errorf("TTL(tech) = %v, want 6h", got)
	}
	if got := TTL(model.CategoryBusiness); got != 6*time.Hour {
		t.Errorf("TTL(business) = %v, want 6h", got)
	}
	if got := TTL(model.Category("")); got != 6*time.Hour {
		t.Errorf("TTL(default) = %v, want 6h", got)
	}
}

func TestSingleFlightDeduplicates(t *testing.T) {
	sf := NewSingleFlight()
	var calls int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	const goroutines = 20
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_ = sf.Do(context.Background(), "k", func(ctx context.Context) error {
				atomic.AddInt32(&calls, 1)
				time.Sleep(50 * time.Millisecond)
				return nil
			})
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("fn called %d times, want exactly 1", got)
	}
}

func TestSingleFlightDifferentKeys(t *testing.T) {
	sf := NewSingleFlight()
	var calls int32

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sf.Do(context.Background(), key, func(ctx context.Context) error {
				atomic.AddInt32(&calls, 1)
				return nil
			})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Errorf("fn called %d times, want 5 (one per distinct key)", got)
	}
}

func TestSingleFlightCancelWaiter(t *testing.T) {
	sf := NewSingleFlight()
	// First call blocks until released.
	release := make(chan struct{})
	go func() {
		_ = sf.Do(context.Background(), "k", func(ctx context.Context) error {
			<-release
			return nil
		})
	}()
	// Give the goroutine time to register the in-flight call.
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sf.Do(ctx, "k", func(context.Context) error {
		t.Fatal("waiter must not invoke fn")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	close(release)
}

func TestSingleFlightReleasesAfterRun(t *testing.T) {
	sf := NewSingleFlight()
	var calls int32
	for i := 0; i < 3; i++ {
		err := sf.Do(context.Background(), "k", func(context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		})
		if err != nil {
			t.Fatalf("Do err: %v", err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("fn called %d times, want 3 sequential", got)
	}
}
