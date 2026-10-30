package agyproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/accountrouting"
	"github.com/justwasm/pigasus/internal/auth"
)

func TestAggregateQuota_Calculation(t *testing.T) {
	router := accountrouting.NewRouter()
	router.SetCandidateAccounts(func(ctx context.Context) ([]accounts.Account, error) {
		return []accounts.Account{
			{Email: "alpha@example.com"},
			{Email: "beta@example.com"},
		}, nil
	})
	t1 := true
	router.SetPolicy(func() *accountpool.Config {
		return &accountpool.Config{
			Enabled:            true,
			Strategy:           accountpool.StrategyAggregate,
			Threshold:          0.15,
			MaxAttempts:        3,
			SwitchOnQuotaError: &t1,
			Aggregate:          true,
		}
	})

	srv, err := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  "http://127.0.0.1:9", // won't be called directly for intercepted quota
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

	resetTimeA := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	resetTimeB := time.Now().Add(4 * time.Hour).UTC().Truncate(time.Second)

	summaries := map[string]*auth.QuotaSummary{
		"alpha@example.com": {
			Description: "Google CloudCode Quota",
			Groups: []auth.QuotaGroup{
				{
					Name:        "Gemini Models",
					Description: "Gemini models description",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							Window:            "weekly",
							RemainingFraction: 1.0,
							ResetTime:         resetTimeA,
						},
					},
				},
			},
		},
		"beta@example.com": {
			Description: "Google CloudCode Quota",
			Groups: []auth.QuotaGroup{
				{
					Name:        "Gemini Models",
					Description: "Gemini models description",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							Window:            "weekly",
							RemainingFraction: 0.0, // exhausted on beta
							ResetTime:         resetTimeB,
						},
					},
				},
			},
		},
	}

	merged := srv.mergeQuotaSummaries(accs, summaries)
	if len(merged.Groups) != 1 {
		t.Fatalf("merged groups count = %d, want 1", len(merged.Groups))
	}
	grp := merged.Groups[0]
	if grp.DisplayName != "Gemini Models" {
		t.Errorf("group name = %q, want Gemini Models", grp.DisplayName)
	}
	if len(grp.Buckets) != 1 {
		t.Fatalf("merged buckets count = %d, want 1", len(grp.Buckets))
	}

	b := grp.Buckets[0]
	// alpha=1.0, beta=0.0 -> average = 0.50 (50%)
	if b.RemainingFraction != 0.50 {
		t.Errorf("RemainingFraction = %f, want 0.50", b.RemainingFraction)
	}
	// As long as alpha has quota, disabled should be FALSE
	if b.Disabled {
		t.Errorf("Disabled = true, want false since alpha has quota")
	}
	if !strings.Contains(b.Description, "1/2 accounts ready") {
		t.Errorf("Description = %q, want mention of 1/2 accounts ready", b.Description)
	}
	if !strings.Contains(b.Description, "50% capacity") {
		t.Errorf("Description = %q, want mention of 50%% capacity", b.Description)
	}
}

func TestAggregateQuota_AllExhausted(t *testing.T) {
	router := accountrouting.NewRouter()
	srv, _ := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  "http://127.0.0.1:9",
		Aggregate: true,
		Router:    router,
	})

	accs := []accounts.Account{
		{Email: "alpha@example.com"},
		{Email: "beta@example.com"},
	}

	summaries := map[string]*auth.QuotaSummary{
		"alpha@example.com": {
			Groups: []auth.QuotaGroup{
				{
					Name: "Gemini Models",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							RemainingFraction: 0.0,
						},
					},
				},
			},
		},
		"beta@example.com": {
			Groups: []auth.QuotaGroup{
				{
					Name: "Gemini Models",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							RemainingFraction: 0.0,
						},
					},
				},
			},
		},
	}

	merged := srv.mergeQuotaSummaries(accs, summaries)
	b := merged.Groups[0].Buckets[0]
	if b.RemainingFraction != 0.0 {
		t.Errorf("RemainingFraction = %f, want 0.0", b.RemainingFraction)
	}
	if !b.Disabled {
		t.Errorf("Disabled = false, want true when all pool accounts are exhausted")
	}
	if !strings.Contains(b.Description, "0/2 accounts ready") {
		t.Errorf("Description = %q, want mention of 0/2 accounts ready", b.Description)
	}
}

func TestAggregateQuota_WithCooldown(t *testing.T) {
	router := accountrouting.NewRouter()
	srv, _ := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  "http://127.0.0.1:9",
		Aggregate: true,
		Router:    router,
	})

	accs := []accounts.Account{
		{Email: "alpha@example.com"},
		{Email: "beta@example.com"},
	}

	// Put alpha in cooldown
	router.Exclude("alpha@example.com")

	summaries := map[string]*auth.QuotaSummary{
		"alpha@example.com": {
			Groups: []auth.QuotaGroup{
				{
					Name: "Gemini Models",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							RemainingFraction: 1.0, // but excluded by cooldown!
						},
					},
				},
			},
		},
		"beta@example.com": {
			Groups: []auth.QuotaGroup{
				{
					Name: "Gemini Models",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							RemainingFraction: 0.6,
						},
					},
				},
			},
		},
	}

	merged := srv.mergeQuotaSummaries(accs, summaries)
	b := merged.Groups[0].Buckets[0]
	// alpha is excluded -> 0.0, beta is 0.6 -> avg = (0.0 + 0.6)/2 = 0.30
	if b.RemainingFraction != 0.30 {
		t.Errorf("RemainingFraction = %f, want 0.30 due to cooldown", b.RemainingFraction)
	}
	if b.Disabled {
		t.Errorf("Disabled = true, want false since beta is active")
	}
	if !strings.Contains(b.Description, "1/2 accounts ready") {
		t.Errorf("Description = %q, want 1/2 accounts ready", b.Description)
	}
}

func TestAggregateCodeAssist_TierResolution(t *testing.T) {
	srv, _ := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  "http://127.0.0.1:9",
		Aggregate: true,
	})

	cases := []struct {
		name     string
		accs     []accounts.Account
		wantTier string
		wantID   string
	}{
		{
			name: "All Free",
			accs: []accounts.Account{
				{Email: "a@example.com", PlanTier: "Antigravity Starter Quota"},
			},
			wantTier: "Antigravity Starter Quota",
			wantID:   "free-tier",
		},
		{
			name: "One Pro",
			accs: []accounts.Account{
				{Email: "a@example.com", PlanTier: "Antigravity Starter Quota"},
				{Email: "b@example.com", PlanTier: "Google AI Pro"},
			},
			wantTier: "Google AI Pro",
			wantID:   "pro-tier",
		},
		{
			name: "One Ultra Over Pro",
			accs: []accounts.Account{
				{Email: "a@example.com", PlanTier: "Google AI Pro"},
				{Email: "b@example.com", PlanTier: "Google AI Ultra"},
			},
			wantTier: "Google AI Ultra",
			wantID:   "ultra-tier",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTier, gotID := srv.bestPlanTier(tc.accs)
			if gotTier != tc.wantTier || gotID != tc.wantID {
				t.Errorf("bestPlanTier = (%q, %q), want (%q, %q)", gotTier, gotID, tc.wantTier, tc.wantID)
			}
		})
	}
}

func TestAggregateHTTPDispatch_Intercept(t *testing.T) {
	// Set up an upstream server that echoes 200 with raw response
	upstreamHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"upstream": true}`))
	}))
	defer upstream.Close()

	router := accountrouting.NewRouter()
	router.SetCandidateAccounts(func(ctx context.Context) ([]accounts.Account, error) {
		return []accounts.Account{
			{Email: "alpha@example.com"},
			{Email: "beta@example.com"},
		}, nil
	})
	t1 := true
	router.SetPolicy(func() *accountpool.Config {
		return &accountpool.Config{
			Enabled:            true,
			Strategy:           accountpool.StrategyAggregate,
			Threshold:          0.15,
			MaxAttempts:        3,
			SwitchOnQuotaError: &t1,
			Aggregate:          true,
		}
	})

	srv, err := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  upstream.URL,
		Aggregate: true,
		Router:    router,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Mock quota fetcher to return custom quota for two accounts
	srv.SetQuotaFetcher(func(ctx context.Context, email string) (*auth.QuotaSummary, error) {
		return &auth.QuotaSummary{
			Description: "Mock Quota",
			Groups: []auth.QuotaGroup{
				{
					Name: "Gemini Models",
					Buckets: []auth.QuotaBucket{
						{
							ID:                "gemini-weekly",
							DisplayName:       "Weekly Limit Remaining",
							RemainingFraction: 0.8,
						},
					},
				},
			},
		}, nil
	})

	// Dispatch retrieveUserQuotaSummary
	req := httptest.NewRequest(http.MethodPost, "/v1internal:retrieveUserQuotaSummary", strings.NewReader(`{"project":"test"}`))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Openagy-Aggregate-Mode") != "true" {
		t.Errorf("X-Openagy-Aggregate-Mode header missing or not true")
	}

	var resp aggregatedQuotaWire
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !strings.Contains(resp.Description, "Unified Pool") {
		t.Errorf("description = %q, want Unified Pool", resp.Description)
	}
}

func TestAggregateHTTPDispatch_CodeAssist(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"currentTier": {"id": "free-tier", "name": "Antigravity"},
			"allowedTiers": [{"id": "free-tier", "name": "Antigravity"}],
			"paidTier": {"id": "free-tier", "name": "Antigravity Starter Quota"}
		}`))
	}))
	defer upstream.Close()

	router := accountrouting.NewRouter()
	router.SetCandidateAccounts(func(ctx context.Context) ([]accounts.Account, error) {
		return []accounts.Account{
			{Email: "free@example.com", PlanTier: "Antigravity Starter Quota"},
			{Email: "pro@example.com", PlanTier: "Google AI Pro"},
		}, nil
	})

	srv, err := New(Config{
		Addr:      "127.0.0.1:0",
		Upstream:  upstream.URL,
		Aggregate: true,
		Router:    router,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1internal:loadCodeAssist", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Openagy-Aggregate-Mode") != "true" {
		t.Errorf("X-Openagy-Aggregate-Mode header missing or not true")
	}
	if rec.Header().Get("X-Openagy-Aggregate-Tier") != "Google AI Pro" {
		t.Errorf("X-Openagy-Aggregate-Tier = %q, want 'Google AI Pro'", rec.Header().Get("X-Openagy-Aggregate-Tier"))
	}

	var root map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	paidTier, ok := root["paidTier"].(map[string]any)
	if !ok || paidTier["name"] != "Google AI Pro" {
		t.Errorf("paidTier = %v, want name 'Google AI Pro'", paidTier)
	}
}

func TestAggregateAdminCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	opts := AdminOptions{
		Prog: "pigasus",
		Out:  &out,
		Err:  &errOut,
	}

	// Test aggregate on
	code := Admin(opts, []string{"aggregate", "on"})
	if code != 0 {
		t.Fatalf("Admin aggregate on failed (%d): %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "aggregate: on") {
		t.Errorf("output = %q, want 'aggregate: on'", out.String())
	}

	// Test aggregate query
	out.Reset()
	code = Admin(opts, []string{"aggregate"})
	if code != 0 {
		t.Fatalf("Admin aggregate query failed: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "aggregate: on") {
		t.Errorf("output = %q, want 'aggregate: on'", out.String())
	}

	// Test aggregate off
	out.Reset()
	code = Admin(opts, []string{"aggregate", "off"})
	if code != 0 {
		t.Fatalf("Admin aggregate off failed: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "aggregate: off") {
		t.Errorf("output = %q, want 'aggregate: off'", out.String())
	}
}
