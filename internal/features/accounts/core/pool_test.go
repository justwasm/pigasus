package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/accountpool"
)

// poolIsolate points both files at a scratch directory: the registry (where
// the policy lives) and the configuration document (where it used to).
func poolIsolate(t *testing.T) (accountsPath, configPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	accountsPath = filepath.Join(dir, "accounts.json")
	configPath = filepath.Join(dir, "config.json")
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", accountsPath)
	t.Setenv("OPENAGY_CONFIG", configPath)
	t.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	poolCache.Store(nil)
	t.Cleanup(func() { poolCache.Store(nil) })
	return accountsPath, configPath
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The policy lives beside the accounts it is about: writing it touches
// accounts.json and leaves config.json alone.
func TestSaveAccountPoolWritesTheRegistry(t *testing.T) {
	accountsPath, _ := poolIsolate(t)
	writeFile(t, accountsPath, `{"version":1,"active":"a@example.com","accounts":[{"email":"a@example.com","token":null}]}`)

	policy := &accountpool.Config{ThresholdFiveHour: 0.25}
	policy.SetEnabled(true)
	if err := SaveAccountPool(policy); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}

	raw, err := os.ReadFile(accountsPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"account_pool"`) {
		t.Fatalf("the section was not written:\n%s", text)
	}
	if !strings.Contains(text, `"a@example.com"`) {
		t.Errorf("the accounts were lost:\n%s", text)
	}

	got := CurrentAccountPool()
	if got == nil {
		t.Fatal("the written policy was not read back")
	}
	if !got.Enabled || got.ThresholdFiveHour != 0.25 {
		t.Errorf("the policy = %+v, want the one written", got)
	}
}

// A registry without the section is a pool that has never been configured.
func TestCurrentAccountPoolWithoutASection(t *testing.T) {
	accountsPath, _ := poolIsolate(t)
	writeFile(t, accountsPath, `{"version":1,"accounts":[]}`)
	if got := CurrentAccountPool(); got != nil {
		t.Errorf("the policy = %+v, want nil", got)
	}
}

// TestASaveKeepsThePoolOn is the regression: a background save — a cached
// token, a quota snapshot — rewrites the whole registry, and the policy has to
// come back exactly as the user wrote it.
//
// A section that never named "enabled" used to come back with
// `"enabled": false`, because a bool cannot say "not said". The next read then
// saw an explicit off, and a live pool switched itself off on the first quota
// refresh.
func TestASaveKeepsThePoolOn(t *testing.T) {
	accountsPath, _ := poolIsolate(t)
	writeFile(t, accountsPath, `{
  "version": 1,
  "active": "a@example.com",
  "accounts": [{"email": "a@example.com", "token": null}],
  "account_pool": {"threshold_5h": 0.05, "threshold_weekly": 0.1, "accounts": ["a@example.com"]}
}`)

	if err := Update("a@example.com", func(a *Account) { a.PlanTier = "Google AI Pro" }); err != nil {
		t.Fatalf("Update: %v", err)
	}

	raw, err := os.ReadFile(accountsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"enabled"`) {
		t.Fatalf("a background save wrote the switch:\n%s", raw)
	}
	got := CurrentAccountPool()
	if got == nil {
		t.Fatal("the pool section was lost by a background save")
	}
	if !got.RoutingEnabled() {
		t.Fatal("the pool is off after a background save")
	}
	if got.ThresholdFiveHour != 0.05 || got.ThresholdWeekly != 0.1 {
		t.Errorf("the thresholds = %v / %v", got.ThresholdFiveHour, got.ThresholdWeekly)
	}
}
