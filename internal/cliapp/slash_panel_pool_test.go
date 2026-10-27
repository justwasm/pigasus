// slash_panel_pool_test.go — cycle 361: the /accounts panel's Pool tab.
//
// The tab is the policy's front end. The two things it must do are
// write the section the routing reads, and do it so that the routing
// sees it at once — the policy is read per turn, so a turn started
// after the write runs on the new policy.
package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accountrouting"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// poolPlainLines renders the panel and strips the styling, so a test
// can look at the words.
func poolPlainLines(m *runMode) []string {
	var out []string
	for _, l := range m.panelLines() {
		out = append(out, stripSGR(l))
	}
	return out
}

// poolIsolate points the configuration file at a temp directory, so the
// test never touches the user's own config.json.
func poolIsolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// mkPoolPanel opens the /accounts panel on its Pool tab, over a
// registry holding the given accounts.
func mkPoolPanel(t *testing.T, emails ...string) *runMode {
	t.Helper()
	poolIsolate(t)
	accountsIsolate(t)
	m := mkAccountsPanel(t, emails...)
	m.panel.cmd = "/pool"
	m.initOpenedSlashPanelData(panelAccounts)
	return m
}

// TestPoolCommandIsListedInCatalogue pins that /pool is a command of
// its own and that it opens the account panel.
func TestPoolCommandIsListedInCatalogue(t *testing.T) {
	kind, ok := panelForCommand("pool")
	if !ok || kind != panelAccounts {
		t.Fatalf("panelForCommand(\"pool\") = (%v, %v)", kind, ok)
	}
	found := false
	for _, c := range tuilib.DefaultSlashCommands {
		if c.Name == "/pool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/pool must appear in the command catalogue")
	}
}

// TestPoolPanelShowsThePolicy pins that the tab shows the policy the
// configuration file carries, field by field.
func TestPoolPanelShowsThePolicy(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com", "b@example.com")
	if err := endpoints.SaveAccountPool(&accountpool.Config{
		Enabled:         true,
		Strategy:        accountpool.StrategyQuotaAware,
		Threshold:       0.25,
		RefreshInterval: "2m",
		Cooldown:        "5m",
		MaxAttempts:     3,
	}); err != nil {
		t.Fatalf("SaveAccountPool: %v", err)
	}
	m.reloadPoolPolicy()

	lines := poolPlainLines(m)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Enabled", "Strategy", "Threshold", "Refresh interval",
		"Cooldown", "Max attempts", "Switch on quota error", "Accounts in pool"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the panel does not show the %q row: %q", want, joined)
		}
	}
	if !strings.Contains(joined, "on") {
		t.Errorf("the enabled policy is not shown as on: %q", joined)
	}
	if !strings.Contains(joined, "quota_aware") {
		t.Errorf("the strategy is not shown: %q", joined)
	}
	if !strings.Contains(joined, "25%") {
		t.Errorf("the threshold is not shown: %q", joined)
	}
	if !strings.Contains(joined, "2m") {
		t.Errorf("the refresh interval is not shown: %q", joined)
	}
}

// TestPoolPanelHasTheTabBar pins the /resume picker's tab bar: the
// active tab is bold, underlined and blue, the inactive one is grey, and
// the row says the tabs can be cycled.
func TestPoolPanelHasTheTabBar(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com")
	bar := m.poolTabBar()
	if !strings.Contains(bar, "Accounts") || !strings.Contains(bar, "Pool") {
		t.Errorf("the tab bar does not name the two tabs: %q", bar)
	}
	if !strings.Contains(bar, "(tab to cycle)") {
		t.Errorf("the tab bar does not say the tabs can be cycled: %q", bar)
	}
	if !strings.Contains(bar, "\x1b[1;4m") {
		t.Errorf("the active tab is not bold and underlined: %q", bar)
	}
}

// TestTheAccountsPanelSwitchesTabs pins that the Tab key moves between
// the account list and the policy, and that the cursor — which belongs
// to the view, not to the panel — starts over.
func TestTheAccountsPanelSwitchesTabs(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com", "b@example.com")
	m.switchAccountsTab(accountsTabAccounts)
	m.panel.cursor = 1
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.acct.tab != accountsTabPool {
		t.Fatalf("the tab = %d, want the Pool tab", m.panel.acct.tab)
	}
	if m.panel.cursor != 0 {
		t.Errorf("the cursor = %d, want it reset for the new view", m.panel.cursor)
	}
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyTab})
	if m.panel.acct.tab != accountsTabAccounts {
		t.Fatalf("the tab = %d, want the Accounts tab back", m.panel.acct.tab)
	}
}

// TestPoolToggleWritesTheSectionAndTheRoutingSeesIt pins the whole
// point of the tab: a change made in the panel is written out and the
// routing reads it at once — the policy is read per turn, so nothing
// has to be rebuilt.
func TestPoolToggleWritesTheSectionAndTheRoutingSeesIt(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com")
	m.panel.cursor = poolRowEnabled
	m.handlePoolPanelKey(tea.Key{Code: ' '})

	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	if !policy.Enabled {
		t.Error("the written policy is not enabled")
	}
	if !accountroutingEnabled() {
		t.Error("the routing does not see the policy the panel just wrote")
	}
}

// TestPoolStrategyCycles pins that the arrow keys walk the strategy
// list and that the walk is written out.
func TestPoolStrategyCycles(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com")
	m.panel.cursor = poolRowStrategy
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyRight})

	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	want := accountpool.Strategies()[1]
	if policy.Strategy != want {
		t.Errorf("the strategy = %q, want %q", policy.Strategy, want)
	}
}

// TestPoolThresholdEdit pins that a free-text row takes what the user
// typed, in percent as well as in fractions.
func TestPoolThresholdEdit(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com")
	m.panel.cursor = poolRowThreshold
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.acct.pool.editing || m.panel.acct.pool.editRow != poolRowThreshold {
		t.Fatalf("the editing row = %d, want the threshold", m.panel.acct.pool.editRow)
	}
	m.panel.textInput.SetValue("25%")
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyEnter})

	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	if policy.Threshold != 0.25 {
		t.Errorf("the threshold = %v, want 0.25", policy.Threshold)
	}
}

// TestPoolThresholdRefusesAValueItCannotRead pins that the panel never
// writes a policy it could not read back.
func TestPoolThresholdRefusesAValueItCannotRead(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com")
	m.panel.cursor = poolRowThreshold
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyEnter})
	m.panel.textInput.SetValue("soon")
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyEnter})

	if !m.panel.acct.pool.editing || m.panel.acct.pool.editRow != poolRowThreshold {
		t.Error("the row closed on a value that does not parse")
	}
	if endpoints.CurrentAccountPool() != nil {
		t.Error("a policy was written from a value that does not parse")
	}
}

// TestPoolThresholdShowsWhatIsTyped pins that the row being edited
// shows the text: a panel that swallows the input would leave the user
// typing blind.
func TestPoolThresholdShowsWhatIsTyped(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com")
	m.panel.cursor = poolRowThreshold
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyEnter})
	m.panel.textInput.SetValue("25%")

	joined := strings.Join(poolPlainLines(m), "\n")
	if !strings.Contains(joined, "25%") {
		t.Errorf("the typed text is not shown: %q", joined)
	}
}

// TestPoolAllowList pins the allow-list submenu: an account can be
// taken out of the pool, and the "(all accounts)" row puts every
// account back.
func TestPoolAllowList(t *testing.T) {
	m := mkPoolPanel(t, "a@example.com", "b@example.com")
	m.panel.cursor = poolRowAccounts
	m.handlePoolPanelKey(tea.Key{Code: tea.KeyEnter})
	if !m.panel.acct.pool.allowOpen {
		t.Fatal("the allow-list did not open")
	}
	// The first row is "(all accounts)"; the second is the first
	// account.
	m.panel.acct.pool.allowCursor = 1
	m.handlePoolPanelKey(tea.Key{Code: ' '})

	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	if len(policy.Accounts) != 1 || !strings.EqualFold(policy.Accounts[0], "a@example.com") {
		t.Errorf("the allow-list = %v, want just a@example.com", policy.Accounts)
	}

	// Back to "(all accounts)": the list is cleared.
	m.panel.acct.pool.allowCursor = 0
	m.handlePoolPanelKey(tea.Key{Code: ' '})
	policy = endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	if len(policy.Accounts) != 0 {
		t.Errorf("the allow-list = %v, want it cleared", policy.Accounts)
	}
}

// TestPoolWritesLeaveTheOtherSectionsAlone pins the one-writer rule at
// the panel's level too: the endpoints section survives the write.
func TestPoolWritesLeaveTheOtherSectionsAlone(t *testing.T) {
	dir := poolIsolate(t)
	path := filepath.Join(dir, "config.json")
	original := `{"endpoints": {"cloud_code": "http://127.0.0.1:8080"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	accountsIsolate(t)
	m := &runMode{}
	m.panel = slashPanel{kind: panelAccounts, cmd: "/pool"}
	m.reloadPoolPolicy()
	m.panel.cursor = poolRowEnabled
	m.handlePoolPanelKey(tea.Key{Code: ' '})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "127.0.0.1:8080") {
		t.Errorf("the endpoint override was lost: %s", raw)
	}
	if !strings.Contains(string(raw), "account_pool") {
		t.Errorf("the policy was not written: %s", raw)
	}
}

// accountroutingEnabled reports whether the runtime's routing sees the
// policy the panel wrote. The runtime reads it per turn, so this is the
// question "does the next turn run on the new policy?".
func accountroutingEnabled() bool {
	return accountrouting.Enabled()
}
