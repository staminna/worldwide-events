package model

import "testing"

func TestSourceValid(t *testing.T) {
	for _, s := range AllSources() {
		if !s.Valid() {
			t.Errorf("AllSources contained invalid source %q", s)
		}
	}
	if Source("").Valid() {
		t.Error("empty source must not be valid")
	}
	if Source("twitter").Valid() {
		t.Error("unknown source must not be valid")
	}
}

func TestCategoryValid(t *testing.T) {
	for _, c := range AllCategories() {
		if !c.Valid() {
			t.Errorf("AllCategories contained invalid category %q", c)
		}
	}
	if Category("").Valid() {
		t.Error("empty category must not be valid")
	}
	if Category("food").Valid() {
		t.Error("unknown category must not be valid")
	}
}

func TestAllSourcesCovers(t *testing.T) {
	want := map[Source]struct{}{
		SourceEventbrite:   {},
		SourceSongkick:     {},
		SourceLuma:         {},
		SourceTicketmaster: {},
		SourceMeetup:       {},
		SourceViralagenda:  {},
	}
	got := map[Source]struct{}{}
	for _, s := range AllSources() {
		got[s] = struct{}{}
	}
	if len(got) != len(want) {
		t.Fatalf("AllSources length = %d, want %d", len(got), len(want))
	}
	for s := range want {
		if _, ok := got[s]; !ok {
			t.Errorf("AllSources missing %q", s)
		}
	}
}

func TestMakeIDStableAndUnique(t *testing.T) {
	a := MakeID(SourceEventbrite, "abc")
	b := MakeID(SourceEventbrite, "abc")
	if a != b {
		t.Errorf("MakeID not deterministic: %s vs %s", a, b)
	}
	if len(a) != 40 {
		t.Errorf("MakeID length = %d, want 40 hex chars", len(a))
	}
	if MakeID(SourceEventbrite, "abc") == MakeID(SourceLuma, "abc") {
		t.Error("MakeID must differ across sources")
	}
	if MakeID(SourceEventbrite, "abc") == MakeID(SourceEventbrite, "abd") {
		t.Error("MakeID must differ across source IDs")
	}
	if MakeID(SourceEventbrite, "") == MakeID(SourceLuma, "") {
		t.Error("MakeID with empty source IDs must still differ by source")
	}
}
