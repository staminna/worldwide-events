package scheduler

import (
	"sync"
	"time"
)

// recentCap is how many finished runs the tracker keeps for the dashboard.
const recentCap = 50

// RunTracker is the in-memory, live view of scrape activity that feeds the
// /runs dashboard. Every scrape funnels through Scheduler.Run, so instrumenting
// begin/finish there gives a complete picture. State is intentionally not
// persisted — it's a live view, reset on restart; the scrapes table still holds
// terminal per-(source,city) status.
type RunTracker struct {
	mu     sync.Mutex
	active map[string]runInfo
	recent []runInfo // newest first, capped at recentCap

	plan        int // total (source,city) units planned by the current warmup
	done        int // finished units since the last SetPlan
	eventsFound int
	blocked     int
	errors      int

	now func() time.Time // injectable for tests
}

type runInfo struct {
	source     string
	city       string
	startedAt  time.Time
	finishedAt time.Time
	count      int
	status     string
	err        string
}

// NewRunTracker returns an empty tracker.
func NewRunTracker() *RunTracker {
	return &RunTracker{active: map[string]runInfo{}, now: time.Now}
}

func key(source, city string) string { return source + "|" + city }

// SetPlan records the denominator for the progress bar (a warmup's total
// source×city units) and resets the done counter.
func (t *RunTracker) SetPlan(total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.plan = total
	t.done = 0
}

// Skip counts a unit that warmup skipped because its cache entry is still
// fresh — it never runs, but must count toward done so the bar reaches 100%.
func (t *RunTracker) Skip() {
	t.mu.Lock()
	t.done++
	t.mu.Unlock()
}

// Begin marks a (source, city) scrape as in-flight.
func (t *RunTracker) Begin(source, city string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[key(source, city)] = runInfo{
		source:    source,
		city:      city,
		startedAt: t.now(),
	}
}

// Finish moves a run from active to the recent ring and updates counters.
func (t *RunTracker) Finish(source, city string, count int, status, errMsg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(source, city)
	info := t.active[k]
	info.source, info.city = source, city
	if info.startedAt.IsZero() {
		info.startedAt = t.now()
	}
	info.finishedAt = t.now()
	info.count = count
	info.status = status
	info.err = errMsg
	delete(t.active, k)

	t.recent = append([]runInfo{info}, t.recent...)
	if len(t.recent) > recentCap {
		t.recent = t.recent[:recentCap]
	}
	t.done++
	t.eventsFound += count
	switch status {
	case "blocked":
		t.blocked++
	case "error":
		t.errors++
	}
}

// --- JSON views for /runs.json ---

type Snapshot struct {
	Totals Totals    `json:"totals"`
	Active []RunView `json:"active"`
	Recent []RunView `json:"recent"`
}

type Totals struct {
	Plan        int `json:"plan"`
	Done        int `json:"done"`
	Active      int `json:"active"`
	EventsFound int `json:"eventsFound"`
	Blocked     int `json:"blocked"`
	Errors      int `json:"errors"`
}

type RunView struct {
	Source     string `json:"source"`
	City       string `json:"city"`
	StartedAt  string `json:"startedAt"`            // RFC3339
	FinishedAt string `json:"finishedAt,omitempty"` // RFC3339, empty while active
	Count      int    `json:"count"`
	Status     string `json:"status,omitempty"`
	Err        string `json:"err,omitempty"`
}

// Snapshot returns a JSON-friendly copy of the current state. It exposes no
// proxy URLs or credentials — only source/city/status/count.
func (t *RunTracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	active := make([]RunView, 0, len(t.active))
	for _, r := range t.active {
		active = append(active, RunView{
			Source:    r.source,
			City:      r.city,
			StartedAt: r.startedAt.UTC().Format(time.RFC3339),
			Status:    "running",
		})
	}
	recent := make([]RunView, 0, len(t.recent))
	for _, r := range t.recent {
		recent = append(recent, RunView{
			Source:     r.source,
			City:       r.city,
			StartedAt:  r.startedAt.UTC().Format(time.RFC3339),
			FinishedAt: r.finishedAt.UTC().Format(time.RFC3339),
			Count:      r.count,
			Status:     r.status,
			Err:        r.err,
		})
	}
	return Snapshot{
		Totals: Totals{
			Plan:        t.plan,
			Done:        t.done,
			Active:      len(t.active),
			EventsFound: t.eventsFound,
			Blocked:     t.blocked,
			Errors:      t.errors,
		},
		Active: active,
		Recent: recent,
	}
}
