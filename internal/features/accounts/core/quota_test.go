package core_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	core "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
)

func TestAccessToken_NoStoredCredentials(t *testing.T) {
	acc := core.Account{
		Email: "empty@example.com",
	}
	_, err := core.AccessToken(context.Background(), "empty@example.com", acc)
	if err == nil {
		t.Fatalf("expected error for account without stored credentials")
	}
}

func TestRegisterCurrent_EmptyEmail(t *testing.T) {
	// Should not panic or error on empty email
	core.RegisterCurrent("")
	core.RegisterCurrent("   ")
}

func TestFetchQuota_UnknownAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "core.json"))

	_, err := core.FetchQuota(context.Background(), "missing@example.com")
	if err == nil {
		t.Fatalf("expected error for unknown account")
	}
}

func TestAccessToken_QuietRefresh(t *testing.T) {
	acc := core.Account{
		Email: "inactive@example.com",
		Token: &auth.AntigravityToken{
			Token: auth.AntigravityAccessToken{
				AccessToken:  "valid-token",
				RefreshToken: "refresh-token",
				Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	tok, err := core.AccessToken(context.Background(), "inactive@example.com", acc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == nil || tok.Token.AccessToken != "valid-token" {
		t.Fatalf("expected valid token, got %v", tok)
	}
}

// quotaTestToken builds a stored credential for the registry.
func quotaTestToken() *auth.AntigravityToken {
	return &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  "at-probe",
			TokenType:    "Bearer",
			RefreshToken: "rt-probe",
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
}

// quotaTestRegister registers an account in an isolated registry.
func quotaTestRegister(t *testing.T, email string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "core.json"))
	if _, _, err := core.RegisterWithEmail(email, quotaTestToken()); err != nil {
		t.Fatalf("RegisterWithEmail(%s): %v", email, err)
	}
}

// TestFreshQuotaReadsTheRegistrySnapshot: the registry owns the
// snapshot, so the reader that renders it finds it there — and the
// snapshot renders back into the summary it was made from.
func TestFreshQuotaReadsTheRegistrySnapshot(t *testing.T) {
	quotaTestRegister(t, "a@example.com")
	summary := &auth.QuotaSummary{
		Description: "note",
		Groups: []auth.QuotaGroup{{
			Name:        "GEMINI MODELS",
			Description: "gem",
			Buckets: []auth.QuotaBucket{{
				ID:                "3p-5h",
				DisplayName:       "Five Hour Limit Remaining",
				Window:            "5h",
				RemainingFraction: 0.5,
			}},
		}},
	}
	if err := core.SetQuota("a@example.com", summary.Snapshot()); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	got := core.FreshQuota("a@example.com")
	if got == nil {
		t.Fatal("FreshQuota() = nil, want the cached summary")
	}
	if got.Description != "note" {
		t.Errorf("description = %q, want %q", got.Description, "note")
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Buckets) != 1 {
		t.Fatalf("groups = %+v, want one group with one bucket", got.Groups)
	}
	bucket := got.Groups[0].Buckets[0]
	if bucket.ID != "3p-5h" {
		t.Errorf("bucket id = %q, want %q", bucket.ID, "3p-5h")
	}
	if bucket.DisplayName != "Five Hour Limit Remaining" {
		t.Errorf("bucket name = %q, want %q", bucket.DisplayName, "Five Hour Limit Remaining")
	}
	if bucket.RemainingFraction != 0.5 {
		t.Errorf("remaining = %v, want 0.5", bucket.RemainingFraction)
	}
}

// TestFreshQuotaSkipsAStaleSnapshot: the freshness rule belongs to the
// registry, and a snapshot past it is not reused.
func TestFreshQuotaSkipsAStaleSnapshot(t *testing.T) {
	quotaTestRegister(t, "a@example.com")
	stale := &auth.QuotaSnapshot{
		FetchedAt: time.Now().Add(-core.QuotaFreshness - time.Minute),
		Groups:    []auth.QuotaGroupSnapshot{{Name: "GEMINI MODELS"}},
	}
	if err := core.SetQuota("a@example.com", stale); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	if got := core.FreshQuota("a@example.com"); got != nil {
		t.Errorf("FreshQuota() = %+v, want nil for a stale snapshot", got)
	}
}

// TestFreshQuotaForAnUnknownAccount: an account the registry does not
// hold has no snapshot, so the reader asks the server instead.
func TestFreshQuotaForAnUnknownAccount(t *testing.T) {
	quotaTestRegister(t, "a@example.com")
	if got := core.FreshQuota("missing@example.com"); got != nil {
		t.Errorf("FreshQuota() = %+v, want nil for an unknown account", got)
	}
	if got := core.FreshQuota(""); got != nil {
		t.Errorf("FreshQuota(\"\") = %+v, want nil", got)
	}
}

// TestQuotaPolicy pins the two budgets the registry owns: the fetch's
// budget and the snapshot's freshness window.
func TestQuotaPolicy(t *testing.T) {
	if core.QuotaTimeout != 45*time.Second {
		t.Errorf("QuotaTimeout = %v, want 45s", core.QuotaTimeout)
	}
	if core.QuotaFreshness != 2*time.Minute {
		t.Errorf("QuotaFreshness = %v, want 2m", core.QuotaFreshness)
	}
}
