// panel_test.go — the /accounts panel.
//
// The panel is tested here, not in the host: it owns the account list,
// the pool policy, the tab bar, the keys and the writes, and a test that
// drives those things belongs with the code that answers them.
//
// The host's own tests keep what is the host's: the panel's catalogue
// entry, the session's account identity and the /usage panel's cache.

package accountspanel

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// ---- the test's own isolation ---------------------------------------------

// accountsFakeToken builds a token whose id_token carries an email
// claim, the way Google's does.
func accountsFakeToken(t *testing.T, email, access string) *auth.AntigravityToken {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"email": email})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  access,
			TokenType:    "Bearer",
			RefreshToken: "rt-" + access,
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		AuthMethod: "consumer",
		IDToken:    "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig",
	}
}

// accountsIsolate points the account registry and the shared credential
// paths at a temp directory, so the test never touches the real
// keychain or the real ~/.config.
func accountsIsolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(home, "accounts.json"))
	t.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")
	return home
}

// poolIsolate points the configuration file at a temp directory, so the
// test never touches the user's own config.json.
func poolIsolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("OPENAGY_CONFIG_HOME", dir)
	return dir
}

// ---- the panel under test -------------------------------------------------

// testPanel is a panel plus the record of what it handed the host.
type testPanel struct {
	*Panel
	// applied is the last account the panel handed to ApplyAccount.
	applied string
	// started counts the sign-in modals the panel opened.
	started int
	// closed counts the Close calls the panel made.
	closed int
	// tiers are the tier the session claims to have read, per account.
	tiers map[string]string
}

// newTestPanel builds the panel with the seams a test needs.
//
// The seams mirror the host's: the session's account identity, the
// sign-in modal, the plan-tier fetch, the quota fetch and the way out of
// the panel.
func newTestPanel(t *testing.T) *testPanel {
	t.Helper()
	tp := &testPanel{tiers: map[string]string{}}
	tp.Panel = New(Seams{
		ApplyAccount: func(email string) {
			tp.applied = email
		},
		StartLogin: func() tea.Cmd {
			tp.started++
			return nil
		},
		RefreshTier: func() tea.Cmd { return nil },
		QuotaCmd: func(email string) tea.Cmd {
			return func() tea.Msg { return nil }
		},
		SessionTier: func(email string) (string, bool) {
			tier, ok := tp.tiers[email]
			return tier, ok
		},
		Close: func() tea.Cmd {
			tp.closed++
			return nil
		},
	})
	return tp
}

// mkPanel opens the panel over a registry holding the given accounts,
// with the first one active.
func mkPanel(t *testing.T, emails ...string) *testPanel {
	t.Helper()
	for _, email := range emails {
		if _, _, err := accounts.Register(accountsFakeToken(t, email, "at-"+email)); err != nil {
			t.Fatalf("Register(%s): %v", email, err)
		}
	}
	if len(emails) > 0 {
		// Register makes each account active in turn, so the last one
		// would be the active account. The tests want the first.
		if _, err := accounts.Activate(t.Context(), emails[0]); err != nil {
			t.Fatalf("Activate(%s): %v", emails[0], err)
		}
	}
	p := newTestPanel(t)
	p.Reload()
	return p
}

// plainLines renders the panel and strips the colour from every line.
func plainLines(p *Panel) []string {
	var out []string
	for _, l := range p.Lines() {
		out = append(out, stripSGR(l))
	}
	return out
}

// stripSGR removes the SGR escape sequences from a rendered line.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// ---- the account list -----------------------------------------------------

// TestPanelLayout pins the account list's shape: the title, the tab
// bar, the rows with the active one marked, and the legend.
func TestPanelLayout(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	lines := plainLines(p.Panel)
	if len(lines) < 6 {
		t.Fatalf("too few lines: %q", lines)
	}
	if lines[0] != "Accounts" {
		t.Errorf("title = %q, want Accounts", lines[0])
	}
	if !strings.Contains(lines[3], "a@example.com") {
		t.Errorf("the first row is not the first account: %q", lines[3])
	}
	if !strings.Contains(lines[3], "(active)") {
		t.Errorf("the active account is not marked: %q", lines[3])
	}
	if strings.Contains(lines[4], "(active)") {
		t.Errorf("the inactive account is marked active: %q", lines[4])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "Keyboard: ") {
		t.Errorf("legend = %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-1], "enter Switch") {
		t.Errorf("legend is missing the switch action: %q", lines[len(lines)-1])
	}
}

// TestPanelEmptyState pins the empty list's own line.
func TestPanelEmptyState(t *testing.T) {
	accountsIsolate(t)
	p := newTestPanel(t)
	p.Reload()
	lines := plainLines(p.Panel)
	if !strings.Contains(lines[3], "No accounts") {
		t.Fatalf("empty state = %q", lines[3])
	}
}

// TestPanelNavigation pins that ↑/↓ move the cursor and that it clamps
// at the top.
func TestPanelNavigation(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	if p.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyUp})
	if p.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", p.Cursor())
	}
	// The cursor clamps at the top.
	p.HandleKey(tea.Key{Code: tea.KeyUp})
	if p.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", p.Cursor())
	}
}

// TestPanelSwitchRewritesTheSharedCredential pins that a switch writes
// the shared credential — the file the reference reads — so the
// installed `agy` uses the same account from its next start-up.
func TestPanelSwitchRewritesTheSharedCredential(t *testing.T) {
	home := accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if !strings.Contains(p.Notice(), "Switched to b@example.com") {
		t.Fatalf("notice = %q", p.Notice())
	}
	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	if err != nil {
		t.Fatalf("read the shared token: %v", err)
	}
	if !strings.Contains(string(raw), "at-b@example.com") {
		t.Fatalf("the shared credential did not follow the switch: %s", raw)
	}
	// The session is told about the switch through the seam.
	if p.applied != "b@example.com" {
		t.Fatalf("the applied account = %q, want b@example.com", p.applied)
	}
}

// TestPanelSwitchToTheActiveAccountIsANoOp pins that switching to the
// account that is already active changes nothing.
func TestPanelSwitchToTheActiveAccountIsANoOp(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !strings.Contains(p.Notice(), "already the active account") {
		t.Fatalf("notice = %q", p.Notice())
	}
}

// TestPanelRemove pins that the "d" key forgets the account under the
// cursor.
func TestPanelRemove(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if !strings.Contains(p.Notice(), "Removed b@example.com") {
		t.Fatalf("notice = %q", p.Notice())
	}
	st, err := accounts.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(st.Accounts))
	}
	if st.Accounts[0].Email != "a@example.com" {
		t.Fatalf("the wrong account was removed: %v", st.Emails())
	}
}

// TestPanelRemoveTheActiveAccountContinuesOnAnother is the point of the
// feature: signing one account out does not end the session when another
// account is left.
func TestPanelRemoveTheActiveAccountContinuesOnAnother(t *testing.T) {
	home := accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if !strings.Contains(p.Notice(), "Signed out of a@example.com") {
		t.Fatalf("notice = %q", p.Notice())
	}
	if !strings.Contains(p.Notice(), "Now signed in as") {
		t.Fatalf("notice = %q", p.Notice())
	}
	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	if err != nil {
		t.Fatalf("read the shared token: %v", err)
	}
	if !strings.Contains(string(raw), "at-b@example.com") {
		t.Fatalf("the session did not continue on the other account: %s", raw)
	}
	if p.applied != "b@example.com" {
		t.Fatalf("the applied account = %q, want b@example.com", p.applied)
	}
}

// TestPanelAddAccountOpensTheSignInModal pins that the "a" key opens the
// sign-in modal the /login command opens.
func TestPanelAddAccountOpensTheSignInModal(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.HandleKey(tea.Key{Code: 'a', Text: "a"})
	if p.started != 1 {
		t.Fatalf("the sign-in modal was opened %d times, want 1", p.started)
	}
}

// TestPanelLearnsAnAccountActivatedOutside keeps the panel's list
// complete: an account that became active outside the clone is unknown
// to the registry, and it has to show up as the active one.
func TestPanelLearnsAnAccountActivatedOutside(t *testing.T) {
	accountsIsolate(t)
	mkPanel(t, "a@example.com")
	if err := auth.SaveTokenUnified(accountsFakeToken(t, "b@example.com", "at-b@example.com")); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}
	p := newTestPanel(t)
	p.Reload()
	if p.Active() != "b@example.com" {
		t.Fatalf("active = %q, want b@example.com", p.Active())
	}
	if len(p.Rows()) != 2 {
		t.Fatalf("rows = %d, want 2", len(p.Rows()))
	}
}

// TestPanelShowsTheCachedTierAndQuota is the panel's promise: every
// account carries its tier and its quota, so the user does not have to
// open /usage for the numbers.
func TestPanelShowsTheCachedTierAndQuota(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	if err := accounts.SetPlanTier("a@example.com", "Google AI Pro"); err != nil {
		t.Fatalf("SetPlanTier: %v", err)
	}
	snap := &auth.QuotaSnapshot{
		FetchedAt: time.Now(),
		Groups: []auth.QuotaGroupSnapshot{{
			Name: "GEMINI MODELS",
			Buckets: []auth.QuotaBucketSnapshot{
				{DisplayName: "Weekly Limit Remaining", RemainingFraction: 0.256},
				{DisplayName: "Five Hour Limit Remaining", RemainingFraction: 0.9595},
			},
		}},
	}
	if err := accounts.SetQuota("a@example.com", snap); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	p.Reload()
	lines := plainLines(p.Panel)
	row := lines[3]
	if !strings.Contains(row, "Google AI Pro") {
		t.Errorf("the row does not carry the tier: %q", row)
	}
	// The percentages are right-aligned in a fixed-width column, so the
	// numbers line up down the list.
	if !strings.Contains(row, "wk  25.6%") {
		t.Errorf("the row does not carry the weekly remaining: %q", row)
	}
	if !strings.Contains(row, "5h  96.0%") {
		t.Errorf("the row does not carry the five-hour remaining: %q", row)
	}
	// The account that was never queried shows dashes, not zeroes: a
	// zero would read as an exhausted quota.
	if !strings.Contains(lines[4], "wk      -") {
		t.Errorf("the account without a snapshot does not show a dash: %q", lines[4])
	}
	if strings.Contains(lines[4], "0.0%") {
		t.Errorf("the account without a snapshot shows a percentage: %q", lines[4])
	}
}

// TestPanelQueuesEveryAccountForAQuotaFetch pins that the panel queries
// every account, one at a time, and that the active account goes first
// — its snapshot is the one the /usage panel reuses.
func TestPanelQueuesEveryAccountForAQuotaFetch(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	cmd := p.QueueQuota()
	if cmd == nil {
		t.Fatal("QueueQuota() = nil")
	}
	if len(p.QuotaQueue()) != 2 {
		t.Fatalf("queue = %v, want both accounts", p.QuotaQueue())
	}
	if p.QuotaQueue()[0] != "a@example.com" {
		t.Errorf("the active account is not first: %v", p.QuotaQueue())
	}
}

// TestPanelRefreshKeyRequeuesTheQuota pins that the r key asks for a
// fresh quota for every account.
func TestPanelRefreshKeyRequeuesTheQuota(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.quotaQueue = nil
	_, cmd := p.HandleKey(tea.Key{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("the r key returned no command")
	}
	if len(p.QuotaQueue()) != 2 {
		t.Fatalf("queue = %v, want both accounts", p.QuotaQueue())
	}
}

// TestPanelRendersTheFetchedSessionTier pins that once the tier has
// been read from the server, the active account's row shows it even
// when the registry does not carry it yet.
func TestPanelRendersTheFetchedSessionTier(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.tiers["a@example.com"] = "Google AI Pro"
	lines := plainLines(p.Panel)
	if !strings.Contains(lines[3], "Google AI Pro") {
		t.Errorf("the fetched tier is not rendered: %q", lines[3])
	}
}

// TestPanelDoesNotRenderAnotherAccountsTier pins that the session's
// tier belongs to the account it was read for, so a session that signed
// in as another account must not show the previous account's tier
// against the new row.
func TestPanelDoesNotRenderAnotherAccountsTier(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	// The tier was read for another account.
	p.tiers["b@example.com"] = "Google AI Plus"
	lines := plainLines(p.Panel)
	if strings.Contains(lines[3], "Google AI Plus") {
		t.Errorf("another account's tier is rendered against this row: %q", lines[3])
	}
}

// TestPanelEscCloses pins that Esc leaves the panel.
func TestPanelEscCloses(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.closed != 1 {
		t.Fatalf("the panel closed %d times, want 1", p.closed)
	}
}

// ---- the pool tab ---------------------------------------------------------

// TestPoolPanelShowsThePolicy pins that the tab shows the policy the
// configuration file carries, field by field.
func TestPoolPanelShowsThePolicy(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
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
	p.SetTab(TabPool)

	joined := strings.Join(plainLines(p.Panel), "\n")
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
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	bar := p.tabBar()
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

// TestPanelSwitchesTabs pins that the Tab key moves between the account
// list and the policy, and that the cursor — which belongs to the view,
// not to the panel — starts over.
func TestPanelSwitchesTabs(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.SetTab(TabAccounts)
	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.Tab() != TabPool {
		t.Fatalf("the tab = %d, want the Pool tab", p.Tab())
	}
	if p.Cursor() != 0 {
		t.Errorf("the cursor = %d, want it reset for the new view", p.Cursor())
	}
	p.HandleKey(tea.Key{Code: tea.KeyTab})
	if p.Tab() != TabAccounts {
		t.Fatalf("the tab = %d, want the Accounts tab back", p.Tab())
	}
}

// TestPoolToggleWritesTheSection pins that a change made in the panel
// is written out — the policy is read per turn, so nothing has to be
// rebuilt.
func TestPoolToggleWritesTheSection(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowEnabled)
	p.HandleKey(tea.Key{Code: ' '})

	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	if !policy.Enabled {
		t.Error("the written policy is not enabled")
	}
}

// TestPoolStrategyCycles pins that the arrow keys walk the strategy list
// and that the walk is written out.
func TestPoolStrategyCycles(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowStrategy)
	p.HandleKey(tea.Key{Code: tea.KeyRight})

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
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowThreshold)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.pool.editing || p.pool.editRow != poolRowThreshold {
		t.Fatalf("the editing row = %d, want the threshold", p.pool.editRow)
	}
	p.textInput.SetValue("25%")
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

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
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowThreshold)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.textInput.SetValue("soon")
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if !p.pool.editing || p.pool.editRow != poolRowThreshold {
		t.Error("the row closed on a value that does not parse")
	}
	if endpoints.CurrentAccountPool() != nil {
		t.Error("a policy was written from a value that does not parse")
	}
}

// TestPoolThresholdShowsWhatIsTyped pins that the row being edited shows
// the text: a panel that swallows the input would leave the user typing
// blind.
func TestPoolThresholdShowsWhatIsTyped(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowThreshold)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	p.textInput.SetValue("25%")

	joined := strings.Join(plainLines(p.Panel), "\n")
	if !strings.Contains(joined, "25%") {
		t.Errorf("the typed text is not shown: %q", joined)
	}
}

// TestPoolAllowList pins the allow-list submenu: an account can be
// taken out of the pool, and the "(all accounts)" row puts every account
// back.
func TestPoolAllowList(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowAccounts)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.pool.allowOpen {
		t.Fatal("the allow-list did not open")
	}
	// The first row is "(all accounts)"; the second is the first
	// account.
	p.pool.allowCursor = 1
	p.HandleKey(tea.Key{Code: ' '})

	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		t.Fatal("the policy was not written")
	}
	if len(policy.Accounts) != 1 || !strings.EqualFold(policy.Accounts[0], "a@example.com") {
		t.Errorf("the allow-list = %v, want just a@example.com", policy.Accounts)
	}

	// Back to "(all accounts)": the list is cleared.
	p.pool.allowCursor = 0
	p.HandleKey(tea.Key{Code: ' '})
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
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)
	p.SetCursor(poolRowEnabled)
	p.HandleKey(tea.Key{Code: ' '})

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

// TestPoolRowLineStyling pins that the focused row keeps its value
// unblued and that toggles format with brackets and styling.
func TestPoolRowLineStyling(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com")
	p.SetTab(TabPool)

	// Focused enabled row.
	focused := p.poolRowLine(poolRowEnabled, true)
	if !strings.HasPrefix(focused, "\x1b[38;2;38;139;210m> Enabled") {
		t.Errorf("focused row missing blue cursor prefix: %q", focused)
	}
	if !strings.Contains(focused, "‹") || !strings.Contains(focused, "›") {
		t.Errorf("focused toggle row missing choice chevrons: %q", focused)
	}

	// Unfocused row has neutral prefix.
	unfocused := p.poolRowLine(poolRowStrategy, false)
	if !strings.HasPrefix(unfocused, "  Strategy") {
		t.Errorf("unfocused row missing two-space indent: %q", unfocused)
	}

	// Toggle formatting.
	if on := poolOnOff(true); !strings.Contains(on, "[ on ]") {
		t.Errorf("poolOnOff(true) = %q, want [ on ]", on)
	}
	if off := poolOnOff(false); !strings.Contains(off, "[ off ]") {
		t.Errorf("poolOnOff(false) = %q, want [ off ]", off)
	}
}

// TestPoolRowCount pins that the Pool tab's cursor bound is its own row
// count, not the account list's.
func TestPoolRowCount(t *testing.T) {
	poolIsolate(t)
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	if got := p.RowCount(); got != 2 {
		t.Errorf("the account list's row count = %d, want 2", got)
	}
	p.SetTab(TabPool)
	if got := p.RowCount(); got != poolRowCount {
		t.Errorf("the Pool tab's row count = %d, want %d", got, poolRowCount)
	}
}

// TestPanelSwitchClearsThePreviousQuotaError: the panel's error line is
// one line for the whole list, not one per row. A quota failure read
// against the previous account must not stay under the new one after a
// switch — otherwise a disabled old account labels the new account
// "disabled" too.
func TestPanelSwitchClearsThePreviousQuotaError(t *testing.T) {
	accountsIsolate(t)
	p := mkPanel(t, "a@example.com", "b@example.com")
	p.quotaErr = "account has been disabled"

	p.SetCursor(1)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p.QuotaErr() != "" {
		t.Fatalf("the previous account's quota error survived the switch: %q", p.QuotaErr())
	}
	if p.applied != "b@example.com" {
		t.Fatalf("applied = %q, want b@example.com", p.applied)
	}
}
