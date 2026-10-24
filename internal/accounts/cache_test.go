package accounts

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// cacheIsolate points the registry at a temp file, so the test never
// touches the real ~/.config.
func cacheIsolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(home, "accounts.json"))
	t.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	return home
}

// cacheFakeToken builds a token whose id_token carries an email claim.
func cacheFakeToken(t *testing.T, email string) *auth.AntigravityToken {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"email": email})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  "at-" + email,
			TokenType:    "Bearer",
			RefreshToken: "rt-" + email,
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		AuthMethod: "consumer",
		IDToken:    "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig",
	}
}

// TestSetPlanTierCachesTheTier is the tier cache the /accounts panel
// reads: the tier of an account that is not active any more survives
// the session that learned it.
func TestSetPlanTierCachesTheTier(t *testing.T) {
	cacheIsolate(t)
	if _, _, err := Register(cacheFakeToken(t, "a@example.com")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := SetPlanTier("a@example.com", "Google AI Pro"); err != nil {
		t.Fatalf("SetPlanTier: %v", err)
	}
	st, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	acc, ok := st.Find("a@example.com")
	if !ok {
		t.Fatal("the account is gone")
	}
	if acc.PlanTier != "Google AI Pro" {
		t.Errorf("plan tier = %q, want Google AI Pro", acc.PlanTier)
	}
	// The tier is cached in the file, not just in memory.
	raw, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "accounts.json"))
	if err != nil {
		t.Fatalf("read the registry: %v", err)
	}
	if !strings.Contains(string(raw), "Google AI Pro") {
		t.Errorf("the tier is not in the registry file: %s", raw)
	}
}

// TestSetQuotaCachesTheSnapshot pins the quota cache entry.
func TestSetQuotaCachesTheSnapshot(t *testing.T) {
	cacheIsolate(t)
	if _, _, err := Register(cacheFakeToken(t, "a@example.com")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	snap := &auth.QuotaSnapshot{
		FetchedAt: time.Now(),
		Groups: []auth.QuotaGroupSnapshot{{
			Name: "GEMINI MODELS",
			Buckets: []auth.QuotaBucketSnapshot{{
				DisplayName:       "Weekly Limit Remaining",
				RemainingFraction: 0.25,
			}},
		}},
	}
	if err := SetQuota("a@example.com", snap); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	acc, ok, err := Get("a@example.com")
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v)", ok, err)
	}
	if acc.Quota == nil {
		t.Fatal("the quota snapshot was not cached")
	}
	weekly, ok := acc.Quota.WeeklyRemaining()
	if !ok || weekly != 0.25 {
		t.Errorf("weekly = (%v, %v), want (0.25, true)", weekly, ok)
	}
	// The cached snapshot survives a reload: that is what the panel
	// reads when it opens.
	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	again, _ := reloaded.Find("a@example.com")
	if again.Quota == nil {
		t.Fatal("the snapshot did not survive the reload")
	}
}

// TestUpdateKeepsTheOtherFields is the merge rule: a cache write
// touches one field and leaves the rest of the entry alone.
func TestUpdateKeepsTheOtherFields(t *testing.T) {
	cacheIsolate(t)
	if _, _, err := Register(cacheFakeToken(t, "a@example.com")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := SetPlanTier("a@example.com", "Google AI Plus"); err != nil {
		t.Fatalf("SetPlanTier: %v", err)
	}
	if err := SetQuota("a@example.com", &auth.QuotaSnapshot{}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	acc, _, err := Get("a@example.com")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if acc.PlanTier != "Google AI Plus" {
		t.Errorf("plan tier = %q, want Google AI Plus", acc.PlanTier)
	}
	if acc.Quota == nil {
		t.Error("the quota snapshot was dropped")
	}
	if acc.Token == nil || acc.Token.Token.AccessToken != "at-a@example.com" {
		t.Errorf("the stored token was lost: %+v", acc.Token)
	}
	if acc.AddedAt.IsZero() {
		t.Error("the added-at stamp was lost")
	}
}

// TestUpdateRejectsAnUnknownAccount: a cache entry for an account the
// registry does not hold would come back on the next read.
func TestUpdateRejectsAnUnknownAccount(t *testing.T) {
	cacheIsolate(t)
	err := SetPlanTier("nobody@example.com", "Google AI Pro")
	if err == nil {
		t.Fatal("SetPlanTier on an unknown account = nil error")
	}
}

// TestGetRejectsAnEmptyEmail keeps the API's contract explicit.
func TestGetRejectsAnEmptyEmail(t *testing.T) {
	cacheIsolate(t)
	if _, _, err := Get("  "); err == nil {
		t.Fatal("Get(\"  \") = nil error")
	}
}
