package routing

import (
	"os"
	"testing"
	"time"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// twoWindowSnapshot builds a cached quota with a five-hour and a weekly
// bucket, the shape the server actually answers for a pooled account.
func twoWindowSnapshot(fiveHour, weekly float64) *auth.QuotaSnapshot {
	return &auth.QuotaSnapshot{
		FetchedAt: time.Now(),
		Groups: []auth.QuotaGroupSnapshot{{
			Name: "GEMINI MODELS",
			Buckets: []auth.QuotaBucketSnapshot{
				{DisplayName: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: weekly},
				{DisplayName: "Five Hour Limit Remaining", Window: "5h", RemainingFraction: fiveHour},
			},
		}},
	}
}

// Both figures are read from one snapshot: the five-hour one that decides
// whether the next turn can be served, and the weekly one that decides how much
// longer the account is worth holding.
func TestSnapshotYieldsBothWindows(t *testing.T) {
	snap := twoWindowSnapshot(0.90, 0.10)

	w := windowsForModelFamily(snap, "")
	if !w.Known {
		t.Fatal("the five-hour figure was not read")
	}
	if w.Remaining != 0.90 {
		t.Errorf("Remaining = %v, want the five-hour figure 0.90", w.Remaining)
	}
	if !w.WeeklyKnown || w.Weekly != 0.10 {
		t.Errorf("Weekly = (%v, %v), want (0.10, true)", w.Weekly, w.WeeklyKnown)
	}
}

// The five-hour figure is the low one when the day is spent, whatever the week
// says: the day is what serves the next turn.
func TestFiveHourFigureIsTheLowOne(t *testing.T) {
	snap := twoWindowSnapshot(0.02, 0.90)
	if w := windowsForModelFamily(snap, ""); w.Remaining != 0.02 {
		t.Errorf("Remaining = %v, want the five-hour figure 0.02", w.Remaining)
	}
}

// A snapshot without a five-hour bucket (an older response, a plan without
// one) falls back to the lowest figure rather than reporting "unlimited".
func TestMissingFiveHourBucketFallsBack(t *testing.T) {
	snap := &auth.QuotaSnapshot{
		FetchedAt: time.Now(),
		Groups: []auth.QuotaGroupSnapshot{{
			Name:    "GEMINI MODELS",
			Buckets: []auth.QuotaBucketSnapshot{{DisplayName: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: 0.3}},
		}},
	}
	w := windowsForModelFamily(snap, "")
	if !w.Known || w.Remaining != 0.3 {
		t.Errorf("Remaining = (%v, %v), want the weekly figure as the fallback", w.Remaining, w.Known)
	}
}

// An aggregate pool remembers the account it routed to, so the next turn of
// the same family is answered by the same account.
func TestAggregatePinsTheAccountAcrossTurns(t *testing.T) {
	r := setup(t, map[string]any{
		"enabled":      true,
		"threshold_5h": 0.15,
	},
		accounts.Account{Email: "a@example.com", Quota: twoWindowSnapshot(0.5, 0.5), Token: token("a")},
		accounts.Account{Email: "b@example.com", Quota: twoWindowSnapshot(0.9, 0.9), Token: token("b")},
	)

	first, err := r.DecideForModel(t.Context(), "gemini")
	if err != nil {
		t.Fatalf("DecideForModel: %v", err)
	}
	if first.Email == "" {
		t.Fatal("the pool answered with no account")
	}
	if got := r.Pinned("gemini"); got != first.Email {
		t.Fatalf("Pinned(gemini) = %q, want %q", got, first.Email)
	}

	second, err := r.DecideForModel(t.Context(), "gemini")
	if err != nil {
		t.Fatalf("DecideForModel: %v", err)
	}
	if second.Email != first.Email {
		t.Errorf("second turn went to %q, want the pinned %q (aggregate is sticky, not a rotation)", second.Email, first.Email)
	}
}

// An explicit account switch outranks the pin: the user pressing "switch" in
// /accounts must not be answered with the account they just left. The pool
// sets its own pin when an account runs out — that does not touch the
// registry's Active field — so a self-made pin survives.
func TestExplicitSwitchDropsThePin(t *testing.T) {
	accs := []accounts.Account{
		{Email: "a@example.com", Quota: twoWindowSnapshot(0.9, 0.9), Token: token("a")},
		{Email: "b@example.com", Quota: twoWindowSnapshot(0.9, 0.9), Token: token("b")},
	}
	r := setup(t, map[string]any{"enabled": true}, accs...)

	first, err := r.DecideForModel(t.Context(), "gemini")
	if err != nil {
		t.Fatalf("DecideForModel: %v", err)
	}
	if first.Email != "a@example.com" {
		t.Fatalf("first decision = %q, want a@example.com", first.Email)
	}

	// The user switches to b in /accounts: the registry's Active changes.
	writeRegistry(t, os.Getenv("OPENAGY_ACCOUNTS_CONFIG"), "b@example.com",
		map[string]any{"enabled": true}, accs...)

	d, err := r.DecideForModel(t.Context(), "gemini")
	if err != nil {
		t.Fatalf("DecideForModel: %v", err)
	}
	if d.Email != "b@example.com" {
		t.Errorf("after the switch the decision = %q, want b@example.com", d.Email)
	}
	if got := r.Pinned("gemini"); got != "b@example.com" {
		t.Errorf("Pinned(gemini) = %q, want the switched-to account", got)
	}
}
