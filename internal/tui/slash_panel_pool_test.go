// slash_panel_pool_test.go — the host's side of the /accounts panel's
// Pool tab.
//
// The tab is the policy's front end. The two things it must do are
// write the section the routing reads, and do it so that the routing
// sees it at once — the policy is read per turn, so a turn started
// after the write runs on the new policy.
//
// The tab's own behaviour is pinned in the package's tests. What is
// left here is the host's half: the command's catalogue entry and the
// routing's answer.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/accountpool"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// poolIsolate points the configuration file at a temp directory, so the
// test never touches the user's own config.json.
func poolIsolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENAGY_CONFIG_HOME", dir)
	return dir
}

// TestPoolCommandIsListedInCatalogue pins that /pool is a command of
// its own and that it opens the account panel.
func TestPoolCommandIsListedInCatalogue(t *testing.T) {
	kind, ok := slashcommands.PanelFor("pool")
	if !ok || kind != panelregistry.Accounts {
		t.Fatalf("slashcommands.PanelFor(\"pool\") = (%v, %v)", kind, ok)
	}
	found := false
	for _, c := range slashcommands.Default {
		if c.Name == "/pool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/pool must appear in the command catalogue")
	}
}

// TestPoolPolicyWriteLandsInTheRegistry is the host's half of the pool tab:
// the policy the tab writes is the section the **registry** carries — beside
// the accounts it is about — and the configuration document is not touched.
func TestPoolPolicyWriteLandsInTheRegistry(t *testing.T) {
	dir := poolIsolate(t)
	home := accountsIsolate(t)
	if err := accounts.SaveAccountPool(&accountpool.Config{
		Enabled:   true,
		Aggregate: &accountpool.Aggregate{Enabled: accountpool.Bool(true)},
	}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "accounts.json"))
	if err != nil {
		t.Fatalf("the registry was not written: %v", err)
	}
	if !strings.Contains(string(raw), "account_pool") {
		t.Errorf("the policy was not written into the registry: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		t.Error("the configuration document was written; the policy does not live there")
	}
}

// TestPoolToggleWritesTheSectionAndTheRoutingSeesIt pins the whole
// point of the tab: a change made in the panel is written out and the
// routing reads it at once — the policy is read per turn, so nothing
// has to be rebuilt.
func TestPoolToggleWritesTheSectionAndTheRoutingSeesIt(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	if err := accounts.SaveAccountPool(&accountpool.Config{Enabled: true}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}
	if !accountrouting.Enabled() {
		t.Error("the routing does not see the policy the panel wrote")
	}
}
