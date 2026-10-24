package cliapp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/tuilib"
)

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

// mkAccountsPanel opens the /accounts panel over a registry holding the
// given accounts, with the first one active.
func mkAccountsPanel(t *testing.T, emails ...string) *runMode {
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
	m := &runMode{}
	m.panel = slashPanel{kind: panelAccounts, cmd: "/accounts"}
	m.reloadAccountsRows()
	return m
}

func TestAccountsPanelIsInvocableButNotListed(t *testing.T) {
	kind, ok := panelForCommand("accounts")
	if !ok || kind != panelAccounts {
		t.Fatalf("panelForCommand(\"accounts\") = (%v, %v)", kind, ok)
	}
	// The picker's catalogue entry count is byte-pinned to the
	// reference's 99, so a clone-only command must not be listed.
	for _, c := range tuilib.DefaultSlashCommands {
		if c.Name == "/accounts" {
			t.Fatal("/accounts must not appear in the command catalogue")
		}
	}
}

func TestAccountsPanelLayout(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	lines := plainLines(m)
	if len(lines) < 6 {
		t.Fatalf("too few lines: %q", lines)
	}
	if lines[0] != "Accounts" {
		t.Errorf("title = %q, want Accounts", lines[0])
	}
	if !strings.Contains(lines[2], "a@example.com") {
		t.Errorf("the first row is not the first account: %q", lines[2])
	}
	if !strings.Contains(lines[2], "(active)") {
		t.Errorf("the active account is not marked: %q", lines[2])
	}
	if strings.Contains(lines[3], "(active)") {
		t.Errorf("the inactive account is marked active: %q", lines[3])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "Keyboard: ") {
		t.Errorf("legend = %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[len(lines)-1], "enter Switch") {
		t.Errorf("legend is missing the switch action: %q", lines[len(lines)-1])
	}
}

func TestAccountsPanelEmptyState(t *testing.T) {
	accountsIsolate(t)
	m := &runMode{}
	m.panel = slashPanel{kind: panelAccounts, cmd: "/accounts"}
	m.reloadAccountsRows()
	lines := plainLines(m)
	if !strings.Contains(lines[2], "No accounts") {
		t.Fatalf("empty state = %q", lines[2])
	}
}

func TestAccountsPanelNavigation(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.panel.cursor)
	}
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyDown})
	if m.panel.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.panel.cursor)
	}
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyUp})
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.panel.cursor)
	}
	// The cursor clamps at the top.
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyUp})
	if m.panel.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.panel.cursor)
	}
}

func TestAccountsPanelSwitchRewritesTheSharedCredential(t *testing.T) {
	home := accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.panel.cursor = 1
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyEnter})

	if !strings.Contains(m.panel.acct.notice, "Switched to b@example.com") {
		t.Fatalf("notice = %q", m.panel.acct.notice)
	}
	// The shared credential — the file the reference reads — must now
	// hold the second account.
	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	if err != nil {
		t.Fatalf("read the shared token: %v", err)
	}
	if !strings.Contains(string(raw), "at-b@example.com") {
		t.Fatalf("the shared credential did not follow the switch: %s", raw)
	}
	// The session state follows too.
	if m.email != "b@example.com" {
		t.Fatalf("m.email = %q, want b@example.com", m.email)
	}
}

func TestAccountsPanelSwitchToTheActiveAccountIsANoOp(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.handleAccountsPanelKey(tea.Key{Code: tea.KeyEnter})
	if !strings.Contains(m.panel.acct.notice, "already the active account") {
		t.Fatalf("notice = %q", m.panel.acct.notice)
	}
}

func TestAccountsPanelRemove(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.panel.cursor = 1
	m.handleAccountsPanelKey(tea.Key{Code: 'd', Text: "d"})
	if !strings.Contains(m.panel.acct.notice, "Removed b@example.com") {
		t.Fatalf("notice = %q", m.panel.acct.notice)
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

// TestAccountsPanelRemoveTheActiveAccountContinuesOnAnother is the
// point of the feature: signing one account out does not end the
// session when another account is left.
func TestAccountsPanelRemoveTheActiveAccountContinuesOnAnother(t *testing.T) {
	home := accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.handleAccountsPanelKey(tea.Key{Code: 'd', Text: "d"})
	if !strings.Contains(m.panel.acct.notice, "Signed out of a@example.com") {
		t.Fatalf("notice = %q", m.panel.acct.notice)
	}
	if !strings.Contains(m.panel.acct.notice, "Now signed in as") {
		t.Fatalf("notice = %q", m.panel.acct.notice)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"))
	if err != nil {
		t.Fatalf("read the shared token: %v", err)
	}
	if !strings.Contains(string(raw), "at-b@example.com") {
		t.Fatalf("the session did not continue on the other account: %s", raw)
	}
	if m.email != "b@example.com" {
		t.Fatalf("m.email = %q, want b@example.com", m.email)
	}
}

func TestLogoutForgetsTheActiveAccount(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com", "b@example.com")
	m := &runMode{}
	m.slashLogout("")
	st, err := accounts.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The signed-out account is gone; the other one stays in the
	// registry, so /accounts can switch back to it without a sign-in.
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1 after the sign-out", len(st.Accounts))
	}
	if st.Accounts[0].Email != "b@example.com" {
		t.Fatalf("the wrong account was forgotten: %v", st.Emails())
	}
}

// TestSyncAccountIdentityFollowsAnOutsideSwitch is the fix for a stale
// /usage panel: the account can be switched outside the TUI — the
// `agy accounts` command, a sign-in in another terminal — and the
// session has to notice it.
func TestSyncAccountIdentityFollowsAnOutsideSwitch(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")
	m := &runMode{email: "a@example.com"}

	// The shared credential now names another account.
	if err := auth.SaveTokenUnified(accountsFakeToken(t, "b@example.com", "at-b@example.com")); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}

	m.syncAccountIdentity()
	if m.email != "b@example.com" {
		t.Fatalf("m.email = %q, want b@example.com", m.email)
	}

	// A sync that finds nothing to change leaves the session alone.
	m.syncAccountIdentity()
	if m.email != "b@example.com" {
		t.Fatalf("m.email = %q, want it unchanged", m.email)
	}
}

// TestAccountsPanelLearnsAnAccountActivatedOutside keeps the panel's
// list complete: an account that became active outside the clone is
// unknown to the registry, and it has to show up as the active one.
func TestAccountsPanelLearnsAnAccountActivatedOutside(t *testing.T) {
	accountsIsolate(t)
	mkAccountsPanel(t, "a@example.com")
	if err := auth.SaveTokenUnified(accountsFakeToken(t, "b@example.com", "at-b@example.com")); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}
	m := &runMode{}
	m.panel = slashPanel{kind: panelAccounts, cmd: "/accounts"}
	m.reloadAccountsRows()
	if m.panel.acct.active != "b@example.com" {
		t.Fatalf("active = %q, want b@example.com", m.panel.acct.active)
	}
	if len(m.panel.acct.rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(m.panel.acct.rows))
	}
}

// TestAccountsPanelShowsTheCachedTierAndQuota is the panel's promise:
// every account carries its tier and its quota, so the user does not
// have to open /usage for the numbers.
func TestAccountsPanelShowsTheCachedTierAndQuota(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
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
	m.reloadAccountsRows()
	lines := plainLines(m)
	row := lines[2]
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
	if !strings.Contains(lines[3], "wk      -") {
		t.Errorf("the account without a snapshot does not show a dash: %q", lines[3])
	}
	if strings.Contains(lines[3], "0.0%") {
		t.Errorf("the account without a snapshot shows a percentage: %q", lines[3])
	}
}

// TestAccountsPanelQueuesEveryAccountForAQuotaFetch: the panel
// queries every account, one at a time, and the active account goes
// first — its snapshot is the one the /usage panel reuses.
func TestAccountsPanelQueuesEveryAccountForAQuotaFetch(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	cmd := m.queueAccountsQuota()
	if cmd == nil {
		t.Fatal("queueAccountsQuota() = nil")
	}
	if len(m.panel.acct.quotaQueue) != 2 {
		t.Fatalf("queue = %v, want both accounts", m.panel.acct.quotaQueue)
	}
	if m.panel.acct.quotaQueue[0] != "a@example.com" {
		t.Errorf("the active account is not first: %v", m.panel.acct.quotaQueue)
	}
}

// TestAccountsPanelRefreshKeyRequeuesTheQuota: the r key asks for a
// fresh quota for every account.
func TestAccountsPanelRefreshKeyRequeuesTheQuota(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.panel.acct.quotaQueue = nil
	_, cmd := m.handleAccountsPanelKey(tea.Key{Code: 'r', Text: "r"})
	if cmd == nil {
		t.Fatal("the r key returned no command")
	}
	if len(m.panel.acct.quotaQueue) != 2 {
		t.Fatalf("queue = %v, want both accounts", m.panel.acct.quotaQueue)
	}
}

// TestAccountsPanelDoesNotRenderThePlaceholderTier: a tier that was
// never read from the server is not an account's tier. The session
// used to start with a placeholder ("Antigravity Starter Quota") and
// replace it with the real one, which made the row change under the
// user's eyes; an account without a fetched tier shows a dash.
func TestAccountsPanelDoesNotRenderThePlaceholderTier(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.tier = "Antigravity Starter Quota"
	m.tierFetched = false
	lines := plainLines(m)
	if strings.Contains(lines[2], "Starter Quota") {
		t.Errorf("the placeholder tier is rendered as the account's tier: %q", lines[2])
	}
	if !strings.Contains(lines[2], "-") {
		t.Errorf("the account without a tier does not show a dash: %q", lines[2])
	}
}

// TestTheSessionShowsNoTierUntilTheServerNamesOne: the banner shows
// the email alone while the tier is unknown. A placeholder would be a
// value that is not the account's, and it would flash into the real
// tier under the user's eyes.
func TestTheSessionShowsNoTierUntilTheServerNamesOne(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com")
	m.tier = ""
	m.tierFetched = false
	if strings.Contains(m.tier, "Starter") {
		t.Fatalf("the session's tier = %q, want none", m.tier)
	}
	m.forgetSessionTier()
	if m.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.tier)
	}
	var b bytes.Buffer
	renderBanner(&b, "a@example.com", "Gemini 3.8 Flash (High)", m.tier, "/tmp")
	out := ansi.Strip(b.String())
	// The banner's second line carries the email (and the tier, when
	// there is one); the model line below it has parentheses of its
	// own, so only that line is inspected.
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("the banner has %d lines: %q", len(lines), out)
	}
	emailLine := lines[1]
	if !strings.Contains(emailLine, "a@example.com") {
		t.Errorf("the banner does not carry the email: %q", emailLine)
	}
	if strings.Contains(emailLine, "(") {
		t.Errorf("the banner carries a parenthesised tier: %q", emailLine)
	}
}

// TestAccountsPanelRendersTheFetchedSessionTier: once the tier has
// been read from the server, the active account's row shows it even
// when the registry does not carry it yet.
func TestAccountsPanelRendersTheFetchedSessionTier(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.tier = "Google AI Pro"
	m.tierFetched = true
	m.tierAccount = "a@example.com"
	lines := plainLines(m)
	if !strings.Contains(lines[2], "Google AI Pro") {
		t.Errorf("the fetched tier is not rendered: %q", lines[2])
	}
}

// TestAccountsPanelDoesNotRenderAnotherAccountsTier is the fix for the
// account that was just added: the session's tier belongs to the
// account it was read for, so a session that signed in as another
// account must not show the previous account's tier against the new
// row.
func TestAccountsPanelDoesNotRenderAnotherAccountsTier(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.tier = "Google AI Plus"
	m.tierFetched = true
	// The tier was read for another account.
	m.tierAccount = "b@example.com"
	lines := plainLines(m)
	if strings.Contains(lines[2], "Google AI Plus") {
		t.Errorf("another account's tier is rendered against this row: %q", lines[2])
	}
}

// TestSignInFetchesTheTierOfTheNewAccount: a session that signs in as
// another account has to ask about *that* account — otherwise the
// previous account's tier would be shown against the new row.
func TestSignInFetchesTheTierOfTheNewAccount(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com")
	m.tier = "Google AI Plus"
	m.tierFetched = true
	m.tierAccount = "a@example.com"
	cmd := m.finishLogin(nil)
	if cmd == nil {
		t.Fatal("finishLogin returned no command")
	}
	if m.tierAccount != "" {
		t.Errorf("the session kept the previous account's tier: %q", m.tierAccount)
	}
	if m.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.tier)
	}
}

// TestAccountsPanelShowsNoTierAfterASignIn: a session that signs in as
// a new account forgets the previous account's tier, so the new row
// shows a dash instead of the old tier.
func TestAccountsPanelShowsNoTierAfterASignIn(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.tier = "Google AI Plus"
	m.tierFetched = true
	m.tierAccount = "a@example.com"
	// The sign-in switched the session to another account.
	m.forgetSessionTier()
	lines := plainLines(m)
	if strings.Contains(lines[2], "Google AI Plus") {
		t.Errorf("the previous account's tier survived the sign-in: %q", lines[2])
	}
	if m.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.tier)
	}
}

// TestUsageReusesTheCachedQuotaSnapshot is the "no second query"
// promise: the /usage panel shows the snapshot the /accounts panel
// already fetched.
func TestUsageReusesTheCachedQuotaSnapshot(t *testing.T) {
	accountsIsolate(t)
	m := &runMode{email: "a@example.com"}
	summary := &auth.QuotaSummary{Description: "cached"}
	m.cacheQuota("a@example.com", summary)
	cmd := m.fetchQuotaSummaryCmd()
	if cmd == nil {
		t.Fatal("fetchQuotaSummaryCmd() = nil")
	}
	msg, ok := cmd().(quotaSummaryMsg)
	if !ok {
		t.Fatalf("msg = %T, want quotaSummaryMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("err = %v", msg.err)
	}
	if msg.summary != summary {
		t.Errorf("the cached snapshot was not reused: %+v", msg.summary)
	}
}

// TestUsageRefetchesAStaleQuotaSnapshot: a snapshot that is too old
// to render must not be reused.
func TestUsageRefetchesAStaleQuotaSnapshot(t *testing.T) {
	accountsIsolate(t)
	m := &runMode{email: "a@example.com"}
	m.quotaCache = map[string]quotaCacheEntry{
		"a@example.com": {summary: &auth.QuotaSummary{}, fetchedAt: time.Now().Add(-10 * time.Minute)},
	}
	if got := m.cachedQuota("a@example.com"); got != nil {
		t.Errorf("a stale snapshot was reused: %+v", got)
	}
}
