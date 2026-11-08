package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// isolateAccounts points the registry and the shared credential at a
// throwaway home. The tests never touch the real accounts.json.
func isolateAccounts(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "accounts.json"))
	t.Setenv("HOME", dir)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(dir, "antigravity"))
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	return filepath.Join(dir, "accounts.json")
}

func runAccountsCmd(t *testing.T, fn func(*JetskiCLI, []string) error, args ...string) (string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	err := fn(newParityJetski(&out, &errb), args)
	return out.String(), err
}

func loadAccounts(t *testing.T) *accounts.Store {
	t.Helper()
	st, err := accounts.Load()
	if err != nil {
		t.Fatalf("load accounts: %v", err)
	}
	return st
}

func TestAccountsAdd_StoresCredentialWithoutActivating(t *testing.T) {
	isolateAccounts(t)
	out, err := runAccountsCmd(t, runAccountsAdd,
		"me@example.com", "--refresh-token", "rt-1", "--plan-tier", "Google AI Pro")
	if err != nil {
		t.Fatalf("accounts add: %v", err)
	}
	if !strings.Contains(out, "added account me@example.com") {
		t.Fatalf("out = %q", out)
	}
	st := loadAccounts(t)
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %+v", st.Accounts)
	}
	acc := st.Accounts[0]
	if acc.Email != "me@example.com" || acc.Token == nil || acc.Token.Token.RefreshToken != "rt-1" {
		t.Fatalf("stored account = %+v token=%+v", acc, acc.Token)
	}
	if acc.PlanTier != "Google AI Pro" {
		t.Errorf("plan tier = %q", acc.PlanTier)
	}
	if st.Active != "" {
		t.Errorf("active = %q, want empty (add must not switch)", st.Active)
	}
}

func TestAccountsAdd_MergesIntoAnExistingAccount(t *testing.T) {
	isolateAccounts(t)
	if _, err := runAccountsCmd(t, runAccountsAdd,
		"me@example.com", "--refresh-token", "rt-1", "--plan-tier", "Google AI Pro"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	first := loadAccounts(t).Accounts[0]

	out, err := runAccountsCmd(t, runAccountsAdd,
		"me@example.com", "--refresh-token", "rt-2")
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	if !strings.Contains(out, "updated account me@example.com") {
		t.Fatalf("out = %q", out)
	}
	st := loadAccounts(t)
	if len(st.Accounts) != 1 {
		t.Fatalf("accounts = %+v", st.Accounts)
	}
	acc := st.Accounts[0]
	if acc.Token == nil || acc.Token.Token.RefreshToken != "rt-2" {
		t.Errorf("refresh token = %+v, want rt-2", acc.Token)
	}
	if acc.PlanTier != "Google AI Pro" {
		t.Errorf("a second add dropped the plan tier: %q", acc.PlanTier)
	}
	if !acc.AddedAt.Equal(first.AddedAt) {
		t.Errorf("AddedAt moved: %v -> %v", first.AddedAt, acc.AddedAt)
	}
}

func TestAccountsAdd_TokenFileDerivesTheEmail(t *testing.T) {
	isolateAccounts(t)
	claims, _ := json.Marshal(map[string]string{"email": "file@example.com"})
	idToken := "x." + base64.RawURLEncoding.EncodeToString(claims) + ".y"
	path := filepath.Join(t.TempDir(), "token.json")
	body := `{"token":{"refresh_token":"rt-file"},"auth_method":"consumer","id_token":"` + idToken + `"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runAccountsCmd(t, runAccountsAdd, "--token-file", path)
	if err != nil {
		t.Fatalf("accounts add: %v", err)
	}
	if !strings.Contains(out, "added account file@example.com") {
		t.Fatalf("out = %q", out)
	}
	st := loadAccounts(t)
	if len(st.Accounts) != 1 || st.Accounts[0].Email != "file@example.com" {
		t.Fatalf("accounts = %+v", st.Accounts)
	}
	if st.Accounts[0].Token.Token.RefreshToken != "rt-file" {
		t.Errorf("token = %+v", st.Accounts[0].Token)
	}
}

func TestAccountsAdd_ExpandsEnvironmentCredentials(t *testing.T) {
	isolateAccounts(t)
	t.Setenv("MY_REFRESH", "rt-from-env")
	if _, err := runAccountsCmd(t, runAccountsAdd,
		"me@example.com", "--refresh-token", "$MY_REFRESH"); err != nil {
		t.Fatalf("accounts add: %v", err)
	}
	st := loadAccounts(t)
	if st.Accounts[0].Token.Token.RefreshToken != "rt-from-env" {
		t.Errorf("token = %+v", st.Accounts[0].Token)
	}

	t.Setenv("MISSING_REFRESH", "")
	os.Unsetenv("MISSING_REFRESH")
	if _, err := runAccountsCmd(t, runAccountsAdd,
		"me@example.com", "--refresh-token", "$MISSING_REFRESH"); err == nil {
		t.Fatal("a missing environment variable was accepted")
	}
}

// TestAccountsAdd_BrowserFlow pins the default form: no credential flags
// means the same Google OAuth sign-in the /accounts panel's `a` key runs.
// The OAuth primitives are stubbed, so the test opens no browser and calls
// no server.
func TestAccountsAdd_BrowserFlow(t *testing.T) {
	isolateAccounts(t)

	savedPKCE := accountsGeneratePKCE
	savedURL := accountsBuildAuthURL
	savedOpen := accountsOpenBrowser
	savedExchange := accountsExchangeCode
	savedInput := accountsLoginInput
	t.Cleanup(func() {
		accountsGeneratePKCE = savedPKCE
		accountsBuildAuthURL = savedURL
		accountsOpenBrowser = savedOpen
		accountsExchangeCode = savedExchange
		accountsLoginInput = savedInput
	})

	accountsGeneratePKCE = func() (string, string, string, error) {
		return "verifier", "challenge", "state", nil
	}
	accountsBuildAuthURL = func(verifier, state string) string {
		if verifier != "verifier" || state != "state" {
			t.Errorf("BuildAuthorizationURL(%q, %q)", verifier, state)
		}
		return "https://accounts.example/auth"
	}
	opened := ""
	accountsOpenBrowser = func(url string) error {
		opened = url
		return nil
	}
	accountsExchangeCode = func(ctx context.Context, code, verifier string) (*auth.AntigravityToken, error) {
		if code != "the-code" || verifier != "verifier" {
			t.Errorf("ExchangeAuthorizationCode(%q, %q)", code, verifier)
		}
		return &auth.AntigravityToken{
			Token:        auth.AntigravityAccessToken{AccessToken: "at", RefreshToken: "rt"},
			AuthMethod:   "consumer",
			IDTokenEmail: "browser@example.com",
		}, nil
	}
	accountsLoginInput = strings.NewReader("the-code\n")

	out, err := runAccountsCmd(t, runAccountsAdd, "--plan-tier", "Google AI Pro")
	if err != nil {
		t.Fatalf("accounts add: %v", err)
	}
	if opened != "https://accounts.example/auth" {
		t.Errorf("opened %q, want the auth URL", opened)
	}
	for _, want := range []string{
		"https://accounts.example/auth",
		"Authorization code:",
		"added account browser@example.com",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	st := loadAccounts(t)
	if len(st.Accounts) != 1 || st.Accounts[0].Email != "browser@example.com" {
		t.Fatalf("accounts = %+v", st.Accounts)
	}
	// The sign-in is the active account, like /login and the panel's a key.
	if st.Active != "browser@example.com" {
		t.Errorf("active = %q", st.Active)
	}
	if st.Accounts[0].PlanTier != "Google AI Pro" {
		t.Errorf("plan tier = %q", st.Accounts[0].PlanTier)
	}
}

func TestAccountsShow_NeverPrintsTheCredential(t *testing.T) {
	isolateAccounts(t)
	if _, err := runAccountsCmd(t, runAccountsAdd,
		"me@example.com", "--refresh-token", "rt-secret", "--access-token", "at-secret",
		"--plan-tier", "Google AI Pro", "--expiry", "2030-01-02T03:04:05Z"); err != nil {
		t.Fatalf("accounts add: %v", err)
	}
	out, err := runAccountsCmd(t, runAccountsShow, "me@example.com")
	if err != nil {
		t.Fatalf("accounts show: %v", err)
	}
	for _, want := range []string{
		"email: me@example.com",
		"plan: Google AI Pro",
		"auth: consumer",
		"expires: 2030-01-02T03:04:05Z",
		"active: false",
		"registry: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "rt-secret") || strings.Contains(out, "at-secret") {
		t.Errorf("show printed a credential:\n%s", out)
	}
}

func TestAccountsParent_HelpFlag(t *testing.T) {
	isolateAccounts(t)
	out, err := runAccountsCmd(t, runAccountsParent, "-h")
	if err != nil {
		t.Fatalf("accounts -h: %v", err)
	}
	for _, want := range []string{"  add ", "  show ", "  switch "} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
}

// TestAccountsParent_NoArgsShowsHelp pins the provider-style default: the
// bare command is a help screen, not the account list.
func TestAccountsParent_NoArgsShowsHelp(t *testing.T) {
	isolateAccounts(t)
	var out, errb bytes.Buffer
	err := runAccountsParent(newParityJetski(&out, &errb), nil)
	if err == nil {
		t.Fatal("bare accounts returned success; want the usage error")
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing (help goes to stderr)", out.String())
	}
	if !strings.Contains(errb.String(), "  list ") || !strings.Contains(errb.String(), "  add ") {
		t.Errorf("stderr lacks the usage:\n%s", errb.String())
	}
}

func TestAccountsTUI_Usage(t *testing.T) {
	isolateAccounts(t)
	usage := usageAccountsTUI("pigasus")
	if !strings.Contains(usage, "Usage: pigasus accounts tui") {
		t.Errorf("unexpected usage: %s", usage)
	}
}

func TestAccountsParent_TUIListedInHelp(t *testing.T) {
	isolateAccounts(t)
	out, err := runAccountsCmd(t, runAccountsParent, "-h")
	if err != nil {
		t.Fatalf("accounts -h: %v", err)
	}
	if !strings.Contains(out, "  tui ") {
		t.Errorf("help lacks tui subcommand:\n%s", out)
	}
}
