// quota_test.go — the payload's quota buckets.
//
// The map is the reference's own shape: the bucket id is the key, the
// remaining fraction and the reset time are the values.
package statusline_test

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// TestQuotaBuckets_NilSummary pins that a session without a quota
// summary has no buckets at all.
func TestQuotaBuckets_NilSummary(t *testing.T) {
	if got := statusline.QuotaBuckets(nil, time.Now()); got != nil {
		t.Errorf("QuotaBuckets(nil) = %v, want nil", got)
	}
}

// TestQuotaBuckets_EmptySummary pins that a summary without buckets
// produces no map: the payload's `quota` member is omitted when it is
// empty.
func TestQuotaBuckets_EmptySummary(t *testing.T) {
	if got := statusline.QuotaBuckets(&auth.QuotaSummary{}, time.Now()); got != nil {
		t.Errorf("QuotaBuckets(empty) = %v, want nil", got)
	}
}

// TestQuotaBuckets_Buckets pins the map's keys and values.
func TestQuotaBuckets_Buckets(t *testing.T) {
	now := time.Date(2026, 10, 25, 21, 7, 17, 0, time.UTC)
	reset := now.Add(90 * time.Second)
	got := statusline.QuotaBuckets(&auth.QuotaSummary{
		Groups: []auth.QuotaGroup{{
			Name: "gpt",
			Buckets: []auth.QuotaBucket{
				{ID: "3p-5h", RemainingFraction: 1},
				{ID: "3p-weekly", RemainingFraction: 0.25, ResetTime: reset},
			},
		}},
	}, now)
	if len(got) != 2 {
		t.Fatalf("buckets = %v, want two", got)
	}
	if b := got["3p-5h"]; b.RemainingFraction != 1 {
		t.Errorf("3p-5h = %v, want a full bucket", b)
	}
	b := got["3p-weekly"]
	if b.RemainingFraction != 0.25 {
		t.Errorf("3p-weekly fraction = %v, want 0.25", b.RemainingFraction)
	}
	if b.ResetTime == nil || !b.ResetTime.Equal(reset) {
		t.Errorf("3p-weekly reset = %v, want %v", b.ResetTime, reset)
	}
	if b.ResetInSeconds != 90 {
		t.Errorf("3p-weekly reset in = %d seconds, want 90", b.ResetInSeconds)
	}
}

// TestQuotaBuckets_NoResetTime pins that a bucket without a reset time
// reports no reset at all.
func TestQuotaBuckets_NoResetTime(t *testing.T) {
	got := statusline.QuotaBuckets(&auth.QuotaSummary{
		Groups: []auth.QuotaGroup{{
			Buckets: []auth.QuotaBucket{{ID: "gemini-5h", RemainingFraction: 0.5}},
		}},
	}, time.Now())
	b := got["gemini-5h"]
	if b.ResetTime != nil {
		t.Errorf("reset = %v, want nil", b.ResetTime)
	}
	if b.ResetInSeconds != 0 {
		t.Errorf("reset in = %d seconds, want 0", b.ResetInSeconds)
	}
}
