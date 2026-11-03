package auth

import (
	"testing"
	"time"
)

// quotaTestSummary is a two-group summary shaped like the live
// retrieveUserQuotaSummary response: a weekly and a five-hour bucket
// per group, with the Claude group the more constrained one.
func quotaTestSummary() *QuotaSummary {
	reset := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	return &QuotaSummary{
		Description: "note",
		Groups: []QuotaGroup{
			{
				Name:        "GEMINI MODELS",
				Description: "gem",
				Buckets: []QuotaBucket{
					{DisplayName: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: 0.25, ResetTime: reset},
					{DisplayName: "Five Hour Limit Remaining", Window: "5h", RemainingFraction: 0.5},
				},
			},
			{
				Name:        "CLAUDE AND GPT MODELS",
				Description: "claude",
				Buckets: []QuotaBucket{
					{DisplayName: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: 0.1},
					{DisplayName: "Five Hour Limit Remaining", Window: "5h", RemainingFraction: 0.9},
				},
			},
		},
	}
}

// TestQuotaSnapshotRoundTrip pins the cache entry: the snapshot holds
// the same numbers as the live summary and renders back into one.
func TestQuotaSnapshotRoundTrip(t *testing.T) {
	live := quotaTestSummary()
	snap := live.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() = nil")
	}
	if snap.FetchedAt.IsZero() {
		t.Error("the snapshot has no fetch timestamp")
	}
	if len(snap.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(snap.Groups))
	}
	back := snap.Summary()
	if back == nil {
		t.Fatal("Summary() = nil")
	}
	if back.Description != live.Description {
		t.Errorf("description = %q, want %q", back.Description, live.Description)
	}
	if len(back.Groups) != len(live.Groups) {
		t.Fatalf("groups = %d, want %d", len(back.Groups), len(live.Groups))
	}
	for i, g := range back.Groups {
		want := live.Groups[i]
		if g.Name != want.Name {
			t.Errorf("group %d name = %q, want %q", i, g.Name, want.Name)
		}
		if len(g.Buckets) != len(want.Buckets) {
			t.Fatalf("group %d buckets = %d, want %d", i, len(g.Buckets), len(want.Buckets))
		}
		for j, b := range g.Buckets {
			wb := want.Buckets[j]
			if b.DisplayName != wb.DisplayName {
				t.Errorf("bucket %d/%d name = %q, want %q", i, j, b.DisplayName, wb.DisplayName)
			}
			if b.RemainingFraction != wb.RemainingFraction {
				t.Errorf("bucket %d/%d remaining = %v, want %v", i, j, b.RemainingFraction, wb.RemainingFraction)
			}
			if !b.ResetTime.Equal(wb.ResetTime) {
				t.Errorf("bucket %d/%d reset = %v, want %v", i, j, b.ResetTime, wb.ResetTime)
			}
		}
	}
}

// TestQuotaSnapshotRemainingIsTheLowestAcrossGroups is the panel's
// rule: the number it shows is the most constrained window, not the
// first group's.
func TestQuotaSnapshotRemainingIsTheLowestAcrossGroups(t *testing.T) {
	snap := quotaTestSummary().Snapshot()
	weekly, ok := snap.WeeklyRemaining()
	if !ok {
		t.Fatal("WeeklyRemaining() found no weekly bucket")
	}
	if weekly != 0.1 {
		t.Errorf("weekly = %v, want 0.1 (the lowest across the groups)", weekly)
	}
	fiveHour, ok := snap.FiveHourRemaining()
	if !ok {
		t.Fatal("FiveHourRemaining() found no five-hour bucket")
	}
	if fiveHour != 0.5 {
		t.Errorf("five hour = %v, want 0.5 (the lowest across the groups)", fiveHour)
	}
}

// TestQuotaSnapshotWithoutBucketsReportsNoWindow: a response without
// a window must not be rendered as 0% — that would read as an
// exhausted quota.
func TestQuotaSnapshotWithoutBucketsReportsNoWindow(t *testing.T) {
	snap := &QuotaSnapshot{Groups: []QuotaGroupSnapshot{{Name: "GEMINI MODELS"}}}
	if _, ok := snap.WeeklyRemaining(); ok {
		t.Error("WeeklyRemaining() found a weekly bucket in an empty summary")
	}
	if _, ok := snap.FiveHourRemaining(); ok {
		t.Error("FiveHourRemaining() found a five-hour bucket in an empty summary")
	}
}

// TestQuotaWindowClassification pins the two windows and their
// fallbacks: the display name first, the wire's window field second.
func TestQuotaWindowClassification(t *testing.T) {
	cases := []struct {
		display string
		window  string
		want    string
	}{
		{"Weekly Limit Remaining", "", QuotaWindowWeekly},
		{"Five Hour Limit Remaining", "", QuotaWindowFiveHour},
		{"weekly limit", "", QuotaWindowWeekly},
		{"", "weekly", QuotaWindowWeekly},
		{"", "5h", QuotaWindowFiveHour},
		{"", "FIVE_HOUR", QuotaWindowFiveHour},
		{"Something else", "something", ""},
	}
	for _, c := range cases {
		if got := QuotaWindow(c.display, c.window); got != c.want {
			t.Errorf("QuotaWindow(%q, %q) = %q, want %q", c.display, c.window, got, c.want)
		}
	}
}
