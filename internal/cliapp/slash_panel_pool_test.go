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
package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
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

// TestPoolPolicyWriteLandsInTheConfigurationFile is the host's half of
// the pool tab: the policy the tab writes is the section the
// configuration file carries, and the endpoints layer is the only
// writer.
func TestPoolPolicyWriteLandsInTheConfigurationFile(t *testing.T) {
	dir := poolIsolate(t)
	accountsIsolate(t)
	if err := endpoints.SaveAccountPool(&accountpool.Config{
		Enabled:  true,
		Strategy: accountpool.StrategyRoundRobin,
	}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "account_pool") {
		t.Errorf("the policy was not written: %s", raw)
	}
}

// TestPoolToggleWritesTheSectionAndTheRoutingSeesIt pins the whole
// point of the tab: a change made in the panel is written out and the
// routing reads it at once — the policy is read per turn, so nothing
// has to be rebuilt.
func TestPoolToggleWritesTheSectionAndTheRoutingSeesIt(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	if err := endpoints.SaveAccountPool(&accountpool.Config{Enabled: true}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}
	if !accountrouting.Enabled() {
		t.Error("the routing does not see the policy the panel wrote")
	}
}
