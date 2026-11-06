// slash_panel_accounts_test.go — the host's side of the /accounts panel.
//
// The panel itself lives in internal/ui/views/accountspanel: it owns
// the account list, the pool policy, the tab bar, the keys and the
// writes. Its own behaviour is pinned in that package's tests.
//
// What is left here is what the host owns: the command's catalogue
// entry, the session's account identity (the banner's email, the
// transport and the tier the session runs at), the sign-out, and the
// /usage panel's quota cache.
package tui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
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
	m.overlays.panel = slashPanel{kind: panelregistry.Accounts, cmd: "/accounts"}
	m.initOpenedSlashPanelData(panelregistry.Accounts)
	return m
}

func TestAccountsPanelIsListedInCatalogue(t *testing.T) {
	kind, ok := slashcommands.PanelFor("accounts")
	if !ok || kind != panelregistry.Accounts {
		t.Fatalf("slashcommands.PanelFor(\"accounts\") = (%v, %v)", kind, ok)
	}
	found := false
	for _, c := range slashcommands.Default {
		if c.Name == "/accounts" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("/accounts must appear in the command catalogue")
	}
}

// TestAccountsPanelSwitchUpdatesTheSession is the seam test: the panel
// owns the switch, and the session's own state — the banner's email —
// follows it.
//
// The tier is dropped with it: the tier belongs to the account it was
// read for, so the new account's row must not show the old account's
// tier.
func TestAccountsPanelSwitchUpdatesTheSession(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.session.tier = "Google AI Pro"
	m.session.tierFetched = true
	m.session.tierAccount = "a@example.com"

	m.accountsPanel().SetCursor(1)
	m.accountsPanel().HandleKey(tea.Key{Code: tea.KeyEnter})

	if m.session.email != "b@example.com" {
		t.Fatalf("m.session.email = %q, want b@example.com", m.session.email)
	}
	if m.session.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.session.tier)
	}
	if m.session.tierFetched {
		t.Error("the session kept the previous account's fetched tier")
	}
}

// TestAccountsPanelRemoveSignsTheSessionOut is the seam test for the
// sign-out: removing the active account signs the session out and
// continues on another account when one is left.
func TestAccountsPanelRemoveSignsTheSessionOut(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")

	m.accountsPanel().HandleKey(tea.Key{Code: 'd', Text: "d"})

	if m.session.email != "b@example.com" {
		t.Fatalf("m.session.email = %q, want b@example.com", m.session.email)
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
	m := &runMode{session: sessionState{email: "a@example.com"}}

	// The shared credential now names another account.
	if err := auth.SaveTokenUnified(accountsFakeToken(t, "b@example.com", "at-b@example.com")); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}

	m.syncAccountIdentity()
	if m.session.email != "b@example.com" {
		t.Fatalf("m.session.email = %q, want b@example.com", m.session.email)
	}

	// A sync that finds nothing to change leaves the session alone.
	m.syncAccountIdentity()
	if m.session.email != "b@example.com" {
		t.Fatalf("m.session.email = %q, want it unchanged", m.session.email)
	}
}

// TestAccountsPanelDoesNotRenderThePlaceholderTier: a tier that was
// never read from the server is not an account's tier. The session
// used to start with a placeholder ("Antigravity Starter Quota") and
// replace it with the real one, which made the row change under the
// user's eyes; an account without a fetched tier shows a dash.
//
// The placeholder lives in the session's state, so this is the host's
// test: the session's tier is the seam's answer.
func TestAccountsPanelDoesNotRenderThePlaceholderTier(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.session.tier = "Antigravity Starter Quota"
	m.session.tierFetched = false
	lines := plainLines(m)
	if strings.Contains(lines[3], "Starter Quota") {
		t.Errorf("the placeholder tier is rendered as the account's tier: %q", lines[3])
	}
	if !strings.Contains(lines[3], "-") {
		t.Errorf("the account without a tier does not show a dash: %q", lines[3])
	}
}

// TestTheSessionShowsNoTierUntilTheServerNamesOne: the banner shows
// the email alone while the tier is unknown. A placeholder would be a
// value that is not the account's, and it would flash into the real
// tier under the user's eyes.
func TestTheSessionShowsNoTierUntilTheServerNamesOne(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com")
	m.session.tier = ""
	m.session.tierFetched = false
	if strings.Contains(m.session.tier, "Starter") {
		t.Fatalf("the session's tier = %q, want none", m.session.tier)
	}
	m.forgetSessionTier()
	if m.session.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.session.tier)
	}
	var b bytes.Buffer
	renderBanner(&b, "a@example.com", "Gemini 3.8 Flash (High)", m.session.tier, "/tmp")
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
	m.session.tier = "Google AI Pro"
	m.session.tierFetched = true
	m.session.tierAccount = "a@example.com"
	lines := plainLines(m)
	if !strings.Contains(lines[3], "Google AI Pro") {
		t.Errorf("the fetched tier is not rendered: %q", lines[3])
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
	m.session.tier = "Google AI Plus"
	m.session.tierFetched = true
	// The tier was read for another account.
	m.session.tierAccount = "b@example.com"
	lines := plainLines(m)
	if strings.Contains(lines[3], "Google AI Plus") {
		t.Errorf("another account's tier is rendered against this row: %q", lines[3])
	}
}

// TestSignInFetchesTheTierOfTheNewAccount: a session that signs in as
// another account has to ask about *that* account — otherwise the
// previous account's tier would be shown against the new row.
func TestSignInFetchesTheTierOfTheNewAccount(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com")
	m.session.tier = "Google AI Plus"
	m.session.tierFetched = true
	m.session.tierAccount = "a@example.com"
	cmd := m.finishLogin(nil)
	if cmd == nil {
		t.Fatal("finishLogin returned no command")
	}
	if m.session.tierAccount != "" {
		t.Errorf("the session kept the previous account's tier: %q", m.session.tierAccount)
	}
	if m.session.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.session.tier)
	}
}

// TestAccountsPanelShowsNoTierAfterASignIn: a session that signs in as
// a new account forgets the previous account's tier, so the new row
// shows a dash instead of the old tier.
func TestAccountsPanelShowsNoTierAfterASignIn(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com", "b@example.com")
	m.session.tier = "Google AI Plus"
	m.session.tierFetched = true
	m.session.tierAccount = "a@example.com"
	// The sign-in switched the session to another account.
	m.forgetSessionTier()
	lines := plainLines(m)
	if strings.Contains(lines[3], "Google AI Plus") {
		t.Errorf("the previous account's tier survived the sign-in: %q", lines[3])
	}
	if m.session.tier != "" {
		t.Errorf("the session's tier = %q, want none", m.session.tier)
	}
}

// TestUsageReusesTheCachedQuotaSnapshot is the "no second query"
// promise: the /usage panel shows the snapshot the /accounts panel
// already fetched.
//
// The snapshot lives in the registry — the fetch writes it there — so
// the /usage panel reads it back from there.
func TestUsageReusesTheCachedQuotaSnapshot(t *testing.T) {
	accountsIsolate(t)
	if _, _, err := accounts.Register(accountsFakeToken(t, "a@example.com", "at-a@example.com")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	m := &runMode{session: sessionState{email: "a@example.com"}}
	summary := &auth.QuotaSummary{Description: "cached"}
	if err := accounts.SetQuota("a@example.com", summary.Snapshot()); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
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
	if msg.summary == nil {
		t.Fatal("the cached snapshot was not reused")
	}
	if msg.summary.Description != summary.Description {
		t.Errorf("the cached snapshot was not reused: %+v", msg.summary)
	}
}

// TestUsageRefetchesAStaleQuotaSnapshot: a snapshot that is too old
// to render must not be reused.
func TestUsageRefetchesAStaleQuotaSnapshot(t *testing.T) {
	accountsIsolate(t)
	if _, _, err := accounts.Register(accountsFakeToken(t, "a@example.com", "at-a@example.com")); err != nil {
		t.Fatalf("Register: %v", err)
	}
	m := &runMode{session: sessionState{email: "a@example.com"}}
	stale := &auth.QuotaSnapshot{
		FetchedAt: time.Now().Add(-10 * time.Minute),
		Groups:    []auth.QuotaGroupSnapshot{{Name: "gpt"}},
	}
	if err := accounts.SetQuota("a@example.com", stale); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	if got := m.cachedQuota("a@example.com"); got != nil {
		t.Errorf("a stale snapshot was reused: %+v", got)
	}
}

// TestAccountsPanelQuotaAnswerLandsInTheRegistry is the seam test for
// the quota: the answer is cached in the registry, which is where the
// /usage panel reads it from.
func TestAccountsPanelQuotaAnswerLandsInTheRegistry(t *testing.T) {
	accountsIsolate(t)
	m := mkAccountsPanel(t, "a@example.com")
	summary := &auth.QuotaSummary{Description: "fetched"}
	// The fetch writes the snapshot into the registry before the
	// answer reaches the panel.
	if err := accounts.SetQuota("a@example.com", summary.Snapshot()); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	m.accountsPanel().QuotaDone("a@example.com", true, nil)
	got := m.cachedQuota("a@example.com")
	if got == nil {
		t.Fatal("the quota answer was not cached in the registry")
	}
	if got.Description != summary.Description {
		t.Errorf("the registry holds %q, want %q", got.Description, summary.Description)
	}
}
