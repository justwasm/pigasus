package agyproxy

import (
	"strings"
	"testing"
	"time"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// The merged view has to say which window the pool is routed on, and which
// account the next turn will spend: an aggregate pool is pinned to one
// account, so "how much is left" means that account's five-hour window.
func TestAggregateQuota_NamesTheWindowAndThePinnedAccount(t *testing.T) {
	router := accountrouting.NewRouter()
	srv, err := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  "http://127.0.0.1:9",
		Aggregate: true,
		Router:    router,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	accs := []accounts.Account{
		{Email: "alpha@example.com"},
		{Email: "beta@example.com"},
	}

	// One group, both windows, so the five-hour bucket and the weekly one
	// are both in the merged document.
	bucket := func(id, name, window string, remaining float64) auth.QuotaBucket {
		return auth.QuotaBucket{
			ID:                id,
			DisplayName:       name,
			Window:            window,
			RemainingFraction: remaining,
			ResetTime:         time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		}
	}
	summaries := map[string]*auth.QuotaSummary{
		"alpha@example.com": {Groups: []auth.QuotaGroup{{
			Name: "Gemini Models",
			Buckets: []auth.QuotaBucket{
				bucket("gemini-weekly", "Weekly Limit Remaining", "weekly", 0.10),
				bucket("gemini-5h", "Five Hour Limit Remaining", "5h", 0.80),
			},
		}}},
		"beta@example.com": {Groups: []auth.QuotaGroup{{
			Name: "Gemini Models",
			Buckets: []auth.QuotaBucket{
				bucket("gemini-weekly", "Weekly Limit Remaining", "weekly", 0.90),
				bucket("gemini-5h", "Five Hour Limit Remaining", "5h", 0.40),
			},
		}}},
	}

	merged := srv.mergeQuotaSummaries(accs, summaries, "alpha@example.com")
	if len(merged.Groups) != 1 {
		t.Fatalf("merged groups = %d, want 1", len(merged.Groups))
	}
	if !strings.Contains(merged.Description, "Pinned to alpha@example.com") {
		t.Errorf("Description = %q, want it to name the pinned account", merged.Description)
	}

	var five, weekly *aggregatedBucketWire
	for i := range merged.Groups[0].Buckets {
		b := &merged.Groups[0].Buckets[i]
		switch auth.QuotaWindow(b.DisplayName, b.Window) {
		case auth.QuotaWindowFiveHour:
			five = b
		case auth.QuotaWindowWeekly:
			weekly = b
		}
	}
	if five == nil || weekly == nil {
		t.Fatalf("the merged view lost a window: five=%v weekly=%v", five != nil, weekly != nil)
	}
	// (0.80 + 0.40) / 2 = 0.60 — the two windows stay separate, so a spent
	// week on one account does not read as a spent five hours.
	if five.RemainingFraction != 0.60 {
		t.Errorf("five-hour RemainingFraction = %v, want 0.60", five.RemainingFraction)
	}
	if weekly.RemainingFraction != 0.50 {
		t.Errorf("weekly RemainingFraction = %v, want 0.50", weekly.RemainingFraction)
	}
	if !strings.Contains(five.Description, "routed on") {
		t.Errorf("five-hour Description = %q, want it to name the window as the routed one", five.Description)
	}
	if !strings.Contains(five.Description, "Next turn uses alpha@example.com") {
		t.Errorf("five-hour Description = %q, want the pinned account", five.Description)
	}
}
