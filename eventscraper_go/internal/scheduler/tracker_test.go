package scheduler

import (
	"testing"
	"time"
)

func TestTrackerBeginFinishSnapshot(t *testing.T) {
	tr := NewRunTracker()
	tr.Begin("luma", "lisbon")

	s := tr.Snapshot()
	if s.Totals.Active != 1 || len(s.Active) != 1 {
		t.Fatalf("after Begin: active=%d len=%d", s.Totals.Active, len(s.Active))
	}
	if s.Active[0].Source != "luma" || s.Active[0].City != "lisbon" {
		t.Errorf("active run = %+v", s.Active[0])
	}

	tr.Finish("luma", "lisbon", 12, "ok", "")
	s = tr.Snapshot()
	if s.Totals.Active != 0 || len(s.Active) != 0 {
		t.Errorf("after Finish still active: %+v", s.Totals)
	}
	if s.Totals.Done != 1 || s.Totals.EventsFound != 12 {
		t.Errorf("totals after finish = %+v", s.Totals)
	}
	if len(s.Recent) != 1 || s.Recent[0].Count != 12 || s.Recent[0].Status != "ok" {
		t.Errorf("recent = %+v", s.Recent)
	}
}

func TestTrackerCountsBlockedAndErrors(t *testing.T) {
	tr := NewRunTracker()
	tr.Finish("eventbrite", "porto", 0, "blocked", "429")
	tr.Finish("songkick", "porto", 0, "error", "boom")
	tr.Finish("luma", "porto", 3, "ok", "")
	s := tr.Snapshot()
	if s.Totals.Blocked != 1 || s.Totals.Errors != 1 {
		t.Errorf("blocked/errors = %d/%d, want 1/1", s.Totals.Blocked, s.Totals.Errors)
	}
	if s.Totals.EventsFound != 3 {
		t.Errorf("eventsFound = %d, want 3", s.Totals.EventsFound)
	}
}

func TestTrackerRecentRingCap(t *testing.T) {
	tr := NewRunTracker()
	for i := 0; i < recentCap+10; i++ {
		tr.Finish("luma", "c", 1, "ok", "")
	}
	s := tr.Snapshot()
	if len(s.Recent) != recentCap {
		t.Errorf("recent len = %d, want cap %d", len(s.Recent), recentCap)
	}
	if s.Totals.Done != recentCap+10 {
		t.Errorf("done = %d, want %d (counter is not capped)", s.Totals.Done, recentCap+10)
	}
}

func TestTrackerPlanAndSkip(t *testing.T) {
	tr := NewRunTracker()
	tr.SetPlan(4)
	tr.Skip()
	tr.Finish("luma", "a", 2, "ok", "")
	s := tr.Snapshot()
	if s.Totals.Plan != 4 || s.Totals.Done != 2 {
		t.Errorf("plan/done = %d/%d, want 4/2", s.Totals.Plan, s.Totals.Done)
	}
}

func TestTrackerBeginRecordsStartTime(t *testing.T) {
	tr := NewRunTracker()
	fixed := time.Unix(1_700_000_000, 0).UTC()
	tr.now = func() time.Time { return fixed }
	tr.Begin("luma", "lisbon")
	s := tr.Snapshot()
	if s.Active[0].StartedAt != fixed.Format(time.RFC3339) {
		t.Errorf("startedAt = %q, want %q", s.Active[0].StartedAt, fixed.Format(time.RFC3339))
	}
}
