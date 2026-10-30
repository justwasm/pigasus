package core

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

// quotaAccount builds an account whose cached quota carries the given
// weekly and five-hour remaining fractions. A negative fraction means
// the window has no bucket.
func quotaAccount(weekly, fiveHour float64) Account {
	acc := Account{Email: "a@example.com"}
	if weekly < 0 && fiveHour < 0 {
		return acc
	}
	snap := &auth.QuotaSnapshot{Groups: []auth.QuotaGroupSnapshot{{
		Name: "GEMINI MODELS",
	}}}
	if weekly >= 0 {
		snap.Groups[0].Buckets = append(snap.Groups[0].Buckets, auth.QuotaBucketSnapshot{
			DisplayName: "Weekly Limit Remaining", RemainingFraction: weekly,
		})
	}
	if fiveHour >= 0 {
		snap.Groups[0].Buckets = append(snap.Groups[0].Buckets, auth.QuotaBucketSnapshot{
			DisplayName: "Five Hour Limit Remaining", RemainingFraction: fiveHour,
		})
	}
	acc.Quota = snap
	return acc
}

// TestQuotaColumnsAreRightAligned: the percentages are right-aligned
// in a fixed-width column, so the numbers line up down the list
// instead of hanging off their left edge.
func TestQuotaColumnsAreRightAligned(t *testing.T) {
	acc := quotaAccount(0.01, 0.96)
	weekly, fiveHour := acc.QuotaColumns()
	if weekly != "wk   1.0%" {
		t.Errorf("weekly column = %q, want %q", weekly, "wk   1.0%")
	}
	if fiveHour != "5h  96.0%" {
		t.Errorf("five-hour column = %q, want %q", fiveHour, "5h  96.0%")
	}
}

// TestQuotaColumnsAlignAcrossAccounts: the percent sign of the weekly
// column is at the same column for every account, whatever the value.
func TestQuotaColumnsAlignAcrossAccounts(t *testing.T) {
	cases := []struct {
		fraction float64
		want     string
	}{
		{0.01, "wk   1.0%"},
		{0.5, "wk  50.0%"},
		{1, "wk 100.0%"},
	}
	for _, tc := range cases {
		weekly, _ := quotaAccount(tc.fraction, 0).QuotaColumns()
		if weekly != tc.want {
			t.Errorf("weekly column = %q, want %q", weekly, tc.want)
		}
	}
	// Every value ends at the same column.
	width := len("wk 100.0%")
	for _, tc := range cases {
		weekly, _ := quotaAccount(tc.fraction, 0).QuotaColumns()
		if len(weekly) != width {
			t.Errorf("weekly column %q is %d wide, want %d", weekly, len(weekly), width)
		}
	}
}

// TestQuotaColumnsShowADashForAMissingBucket: a window the account has
// no bucket for shows a dash, padded like a number so the column keeps
// its shape.
func TestQuotaColumnsShowADashForAMissingBucket(t *testing.T) {
	acc := quotaAccount(-1, -1)
	weekly, fiveHour := acc.QuotaColumns()
	if weekly != "wk      -" {
		t.Errorf("weekly column = %q, want %q", weekly, "wk      -")
	}
	if fiveHour != "5h      -" {
		t.Errorf("five-hour column = %q, want %q", fiveHour, "5h      -")
	}
	// An account with no cached quota at all reads the same way.
	empty := Account{Email: "b@example.com"}
	weekly, fiveHour = empty.QuotaColumns()
	if weekly != "wk      -" || fiveHour != "5h      -" {
		t.Errorf("the account without a snapshot = (%q, %q), want dashes", weekly, fiveHour)
	}
}

// TestQuotaCellCarriesBothColumns: the panel's cell holds the weekly
// and the five-hour column, one after the other.
func TestQuotaCellCarriesBothColumns(t *testing.T) {
	acc := quotaAccount(0.256, 0.9595)
	cell := acc.QuotaCell()
	if cell != "wk  25.6%  5h  96.0%" {
		t.Errorf("the quota cell = %q, want %q", cell, "wk  25.6%  5h  96.0%")
	}
	if !strings.Contains(cell, "wk") || !strings.Contains(cell, "5h") {
		t.Errorf("the quota cell does not carry both labels: %q", cell)
	}
}
