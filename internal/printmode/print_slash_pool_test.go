// print_slash_pool_test.go — cycle 361: the print mode's /pool.
//
// The print mode is one-shot and has no panel, so /pool *shows* the
// account-routing policy. The point of the test is that the command is
// consumed locally: an unknown command is forwarded to the model as
// literal text, and a policy the user wants to read is not a prompt.

package printmode

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/accountpool"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
)

// TestPrintSlashPoolShowsThePolicy pins that /pool answers with the
// policy the configuration file carries.
func TestPrintSlashPoolShowsThePolicy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "accounts.json"))
	if err := accounts.SaveAccountPool(&accountpool.Config{
		Enabled:           true,
		Aggregate:         &accountpool.Aggregate{Enabled: accountpool.Bool(true)},
		ThresholdFiveHour: 0.25,
		RefreshInterval:   "2m",
		Cooldown:          "5m",
		MaxAttempts:       3,
	}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}

	res := handlePrintSlashPool()
	if !res.Handled {
		t.Fatal("/pool was not handled locally")
	}
	for _, want := range []string{"on", "aggregate", "25%", "5h", "2m", "5m"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("the output does not carry %q: %q", want, res.Output)
		}
	}
}

// TestPrintSlashPoolWithoutAPolicy pins that a file without the section
// is a pool that is switched off — not an error.
func TestPrintSlashPoolWithoutAPolicy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "accounts.json"))

	res := handlePrintSlashPool()
	if !res.Handled {
		t.Fatal("/pool was not handled locally")
	}
	if !strings.Contains(res.Output, "off") {
		t.Errorf("the output does not say the routing is off: %q", res.Output)
	}
}
