package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// quotaTestSnapshot is the shape the registry holds: one group, the
// two windows the reference's accounts have.
func quotaTestSnapshot() *auth.QuotaSnapshot {
	return &auth.QuotaSnapshot{
		FetchedAt:   time.Now(),
		Description: "Quota for the signed-in account.",
		Groups: []auth.QuotaGroupSnapshot{
			{
				Name:        "Gemini Models",
				Description: "The Gemini models the account can use.",
				Buckets: []auth.QuotaBucketSnapshot{
					{
						DisplayName:       "Weekly Limit Remaining",
						Window:            "weekly",
						RemainingFraction: 0.452,
						ResetTime:         time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC),
					},
					{
						DisplayName:       "Five Hour Limit Remaining",
						Window:            "5h",
						RemainingFraction: 0.913,
						ResetTime:         time.Date(2026, 10, 26, 14, 7, 0, 0, time.UTC),
					},
				},
			},
		},
	}
}

// quotaTestDispatcher swaps the two package seams for a snapshot and
// a fetcher, and restores them when the test ends.
func quotaTestDispatcher(t *testing.T, snap *auth.QuotaSnapshot, fetched *auth.QuotaSummary) *Dispatcher {
	t.Helper()
	oldSource, oldFetcher := quotaSnapshotSource, quotaSummaryFetcher
	quotaSnapshotSource = func() (string, *auth.QuotaSnapshot) { return "alpha@example.com", snap }
	quotaSummaryFetcher = func(context.Context) *auth.QuotaSummary { return fetched }
	t.Cleanup(func() {
		quotaSnapshotSource, quotaSummaryFetcher = oldSource, oldFetcher
	})
	d, err := NewDispatcher(Config{Session: NewSessionState("test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// quotaTestDecode decodes the route's answer into the shape the Web
// UI's usage panel reads.
func quotaTestDecode(t *testing.T, d *Dispatcher, payload string) map[string]any {
	t.Helper()
	resText, err := d.DispatchDirect(context.Background(), "RetrieveUserQuotaSummary", payload)
	if err != nil {
		t.Fatalf("DispatchDirect RetrieveUserQuotaSummary: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	response, ok := res["response"].(map[string]any)
	if !ok {
		t.Fatalf("expected a response object, got %v", res["response"])
	}
	return response
}

func TestQuota_RetrieveUserQuotaSummaryShape(t *testing.T) {
	d := quotaTestDispatcher(t, quotaTestSnapshot(), nil)

	response := quotaTestDecode(t, d, "{}")

	groups, ok := response["groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("expected one group, got %v", response["groups"])
	}
	group := groups[0].(map[string]any)
	if group["displayName"] != "Gemini Models" {
		t.Errorf("group displayName = %v, want %q", group["displayName"], "Gemini Models")
	}
	buckets, ok := group["buckets"].([]any)
	if !ok || len(buckets) != 2 {
		t.Fatalf("expected two buckets, got %v", group["buckets"])
	}
	bucket := buckets[0].(map[string]any)
	if bucket["displayName"] != "Weekly Limit Remaining" {
		t.Errorf("bucket displayName = %v", bucket["displayName"])
	}
	if bucket["bucketId"] != "weekly" {
		t.Errorf("bucketId = %v, want %q", bucket["bucketId"], "weekly")
	}
	// remaining_fraction is a member of the `remaining` oneof, so
	// proto-JSON writes it at the bucket's top level under its JSON
	// name — not nested under "remaining".
	if _, nested := bucket["remaining"]; nested {
		t.Errorf("the oneof member must not be nested: %v", bucket["remaining"])
	}
	fraction, ok := bucket["remainingFraction"].(float64)
	if !ok || fraction != 0.452 {
		t.Errorf("remainingFraction = %v, want 0.452", bucket["remainingFraction"])
	}
	// The Web UI only computes the "Resets in ..." countdown when the
	// description is non-empty.
	if desc, _ := bucket["description"].(string); desc == "" {
		t.Errorf("the bucket description must not be empty")
	}
	if reset, _ := bucket["resetTime"].(string); reset != "2026-10-29T12:00:00Z" {
		t.Errorf("resetTime = %v, want the RFC3339 stamp", bucket["resetTime"])
	}
}

func TestQuota_RetrieveUserQuotaSummaryEmpty(t *testing.T) {
	d := quotaTestDispatcher(t, nil, nil)

	response := quotaTestDecode(t, d, "{}")

	groups, ok := response["groups"].([]any)
	if !ok || len(groups) != 0 {
		t.Fatalf("expected no groups, got %v", response["groups"])
	}
}

func TestQuota_RetrieveUserQuotaSummaryRefreshes(t *testing.T) {
	live := &auth.QuotaSummary{
		Description: "Fresh.",
		Groups: []auth.QuotaGroup{
			{
				Name: "Gemini Models",
				Buckets: []auth.QuotaBucket{
					{DisplayName: "Weekly Limit Remaining", Window: "weekly", RemainingFraction: 0.25},
				},
			},
		},
	}
	d := quotaTestDispatcher(t, nil, live)

	response := quotaTestDecode(t, d, `{"forceRefresh":true}`)

	groups := response["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %v", response["groups"])
	}
	buckets := groups[0].(map[string]any)["buckets"].([]any)
	if len(buckets) != 1 {
		t.Fatalf("expected one bucket, got %v", buckets)
	}
	fraction, ok := buckets[0].(map[string]any)["remainingFraction"].(float64)
	if !ok || fraction != 0.25 {
		t.Errorf("remainingFraction = %v, want 0.25", buckets[0].(map[string]any)["remainingFraction"])
	}
}

func TestQuota_RetrieveUserQuotaSummaryKeepsCacheOnFailure(t *testing.T) {
	d := quotaTestDispatcher(t, quotaTestSnapshot(), nil)

	// A live read that fails leaves the cached numbers in place.
	response := quotaTestDecode(t, d, `{"forceRefresh":true}`)

	groups := response["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected the cached group, got %v", response["groups"])
	}
}
