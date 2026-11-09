// accounts_cmds.go — the scripting side of the multi-account registry.
//
// `accounts add` defaults to the same Google OAuth sign-in as `/login`
// and the `/accounts` panel's `a` key: print the URL, open the browser,
// read the pasted code, exchange it and register the account. With
// --token-file or the individual credential flags it takes the
// non-interactive path, so a script can import credentials without a
// browser.
//
// The credential is the reference's own token JSON. Adding never prints
// it, and the credential form does not touch the shared credential
// without --activate.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/palette"
	"golang.org/x/term"
)

type accountsAddFlags struct {
	email        string
	tokenFile    string
	refreshToken string
	accessToken  string
	idToken      string
	authMethod   string
	planTier     string
	expiry       string
	activate     bool
}

var accountsAddValueFlags = map[string]bool{
	"email":         true,
	"token-file":    true,
	"token_file":    true,
	"refresh-token": true,
	"refresh_token": true,
	"access-token":  true,
	"access_token":  true,
	"id-token":      true,
	"id_token":      true,
	"auth-method":   true,
	"auth_method":   true,
	"plan-tier":     true,
	"plan_tier":     true,
	"expiry":        true,
}

func newAccountsAddFlagSet(errOut io.Writer, f *accountsAddFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("accounts add", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&f.email, "email", "", "account email")
	fs.StringVar(&f.tokenFile, "token-file", "", "credential JSON file, or - for stdin")
	fs.StringVar(&f.tokenFile, "token_file", "", "credential JSON file, or - for stdin")
	fs.StringVar(&f.refreshToken, "refresh-token", "", "OAuth refresh token")
	fs.StringVar(&f.refreshToken, "refresh_token", "", "OAuth refresh token")
	fs.StringVar(&f.accessToken, "access-token", "", "OAuth access token")
	fs.StringVar(&f.accessToken, "access_token", "", "OAuth access token")
	fs.StringVar(&f.idToken, "id-token", "", "OIDC id token; carries the email")
	fs.StringVar(&f.idToken, "id_token", "", "OIDC id token; carries the email")
	fs.StringVar(&f.authMethod, "auth-method", "consumer", "auth method recorded with the token")
	fs.StringVar(&f.authMethod, "auth_method", "consumer", "auth method recorded with the token")
	fs.StringVar(&f.planTier, "plan-tier", "", "cached plan tier shown by accounts list")
	fs.StringVar(&f.planTier, "plan_tier", "", "cached plan tier shown by accounts list")
	fs.StringVar(&f.expiry, "expiry", "", "access-token expiry (RFC 3339)")
	fs.BoolVar(&f.activate, "activate", false, "make the account active after adding it")
	return fs
}

// resolveCredential turns the flags into the token the registry stores.
// A token file wins over the individual flags: it is the reference's own
// on-disk format, so copying a file is the least lossy path.
func (f *accountsAddFlags) resolveCredential() (*auth.AntigravityToken, error) {
	if f.tokenFile != "" {
		var raw []byte
		var err error
		if f.tokenFile == "-" {
			raw, err = io.ReadAll(os.Stdin)
		} else {
			raw, err = os.ReadFile(f.tokenFile)
		}
		if err != nil {
			return nil, fmt.Errorf("accounts add: read token: %w", err)
		}
		var tok auth.AntigravityToken
		if err := json.Unmarshal(raw, &tok); err != nil {
			return nil, fmt.Errorf("accounts add: parse token: %w", err)
		}
		if strings.TrimSpace(tok.AuthMethod) == "" {
			tok.AuthMethod = "consumer"
		}
		return &tok, nil
	}

	refresh, err := credentialFlag("--refresh-token", f.refreshToken)
	if err != nil {
		return nil, err
	}
	access, err := credentialFlag("--access-token", f.accessToken)
	if err != nil {
		return nil, err
	}
	idToken, err := credentialFlag("--id-token", f.idToken)
	if err != nil {
		return nil, err
	}
	tok := &auth.AntigravityToken{
		AuthMethod: strings.TrimSpace(f.authMethod),
		IDToken:    idToken,
	}
	tok.Token.AccessToken = access
	tok.Token.RefreshToken = refresh
	tok.Token.Expiry = strings.TrimSpace(f.expiry)
	if tok.AuthMethod == "" {
		tok.AuthMethod = "consumer"
	}
	return tok, nil
}

// credentialFlag expands the provider convention: a value that starts
// with $ is the name of an environment variable.
func credentialFlag(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "$") || value == "$" {
		return value, nil
	}
	env := strings.TrimPrefix(value, "$")
	got, ok := os.LookupEnv(env)
	if !ok {
		return "", fmt.Errorf("Error: %s: environment variable %s is not set", name, env)
	}
	return got, nil
}

// hasCredentialFlags reports whether the caller supplied a credential
// instead of asking for the browser sign-in.
func (f *accountsAddFlags) hasCredentialFlags() bool {
	for _, v := range []string{f.tokenFile, f.refreshToken, f.accessToken, f.idToken, f.expiry} {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// The OAuth primitives are variables so the browser flow can be tested
// without opening a browser or calling Google.
var (
	accountsGeneratePKCE           = auth.GeneratePKCE
	accountsBuildAuthURL           = auth.BuildAuthorizationURL
	accountsOpenBrowser            = auth.OpenBrowser
	accountsExchangeCode           = auth.ExchangeAuthorizationCode
	accountsLoginInput   io.Reader = os.Stdin
)

// runAccountsBrowserLogin is `accounts add` without a credential: the
// same Google OAuth sign-in the `/accounts` panel's `a` key opens.
//
// It skips the panel's method menu and goes straight to Google OAuth,
// which is the default the command promises. The URL is printed even
// when the browser opens, so a headless/SSH user can copy it.
func runAccountsBrowserLogin(j *JetskiCLI, planTier string) error {
	verifier, _, state, err := accountsGeneratePKCE()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	authURL := accountsBuildAuthURL(verifier, state)
	fmt.Fprintln(j.Out, "Open this URL to sign in:")
	fmt.Fprintln(j.Out, authURL)
	fmt.Fprintln(j.Out)
	if err := accountsOpenBrowser(authURL); err != nil {
		fmt.Fprintf(j.ErrOut, "warning: open browser: %v\n", err)
	}
	fmt.Fprint(j.Out, "Authorization code: ")

	code, err := readAuthorizationCode(accountsLoginInput)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if code == "" {
		return errExitCodeMsg(1, "Error: authentication interrupted")
	}
	fmt.Fprintln(j.Out)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := accountsExchangeCode(ctx, code, verifier)
	if err != nil {
		return errExitCodeMsg(1, fmt.Sprintf("authentication failed: %v", err))
	}
	acc, existed, err := accounts.Register(tok)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	verb := "added"
	if existed {
		verb = "updated"
	}
	fmt.Fprintf(j.Out, "%s account %s (auth=%s)\n", verb, acc.Email, tok.AuthMethod)
	if tier := strings.TrimSpace(planTier); tier != "" {
		if err := accounts.SetPlanTier(acc.Email, tier); err != nil {
			return errExitCodeMsg(1, err.Error())
		}
		fmt.Fprintf(j.Out, "plan: %s\n", tier)
	}
	return nil
}

// readAuthorizationCode reads one line, ignoring a trailing newline.
func readAuthorizationCode(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("no input")
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// runAccountsAdd registers an account.
//
// With no credential flags it runs the browser sign-in; with
// --token-file or the individual token flags it takes the credential
// directly, which is the scriptable path.
func runAccountsAdd(j *JetskiCLI, args []string) error {
	var f accountsAddFlags
	fs := newAccountsAddFlagSet(j.ErrOut, &f)
	email, rest := splitPositionalID(args, accountsAddValueFlags)
	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errExitCode(1)
	}
	if email == "" {
		email = strings.TrimSpace(f.email)
	}

	if !f.hasCredentialFlags() {
		if email != "" {
			// The browser decides which account signs in; an email here
			// can only be a hint, so do not pretend it is a constraint.
			fmt.Fprintf(j.ErrOut, "warning: signing in through the browser; %s is only a hint\n", email)
		}
		return runAccountsBrowserLogin(j, f.planTier)
	}

	tok, err := f.resolveCredential()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if email == "" {
		email = strings.TrimSpace(tok.UserEmail())
	}
	if email == "" {
		return usageFail(j, usageAccountsAdd(j.Name), "Error: accounts add: --email or a token carrying an email is required")
	}
	if strings.TrimSpace(tok.Token.AccessToken) == "" && strings.TrimSpace(tok.Token.RefreshToken) == "" {
		return errExitCodeMsg(1, "Error: accounts add: --refresh-token, --access-token or --token-file is required")
	}
	if tok.UserEmail() == "" {
		tok.IDTokenEmail = email
	}

	acc, existed, err := accounts.Add(email, tok)
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	if tier := strings.TrimSpace(f.planTier); tier != "" {
		if err := accounts.SetPlanTier(acc.Email, tier); err != nil {
			return errExitCodeMsg(1, err.Error())
		}
		acc.PlanTier = tier
	}

	verb := "added"
	if existed {
		verb = "updated"
	}
	fmt.Fprintf(j.Out, "%s account %s (auth=%s)\n", verb, acc.Email, tok.AuthMethod)
	if f.activate {
		active, err := accounts.Activate(context.Background(), acc.Email)
		if err != nil {
			return errExitCodeMsg(1, err.Error())
		}
		fmt.Fprintf(j.Out, "active account: %s\n", active.Email)
	} else {
		fmt.Fprintln(j.Out, "  not activated; pass --activate or run `accounts switch <email>`")
	}
	return nil
}

// runAccountsShow prints one registry entry. Credentials are never shown.
func runAccountsShow(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageAccountsShow(j.Name), "Error: usage: accounts show <email>")
	}
	st, err := accounts.Load()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	email := strings.TrimSpace(args[0])
	acc, ok := st.Find(email)
	if !ok {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no account named %q", email))
	}

	active := ""
	if _, current := accounts.Current(); current != "" {
		active = current
	} else if st.Active != "" {
		active = st.Active
	}
	fmt.Fprintf(j.Out, "email: %s\n", acc.Email)
	if tier := strings.TrimSpace(acc.PlanTier); tier != "" {
		fmt.Fprintf(j.Out, "plan: %s\n", tier)
	}
	if acc.Token != nil {
		fmt.Fprintf(j.Out, "auth: %s\n", acc.Token.AuthMethod)
		if expiry := strings.TrimSpace(acc.Token.Token.Expiry); expiry != "" {
			fmt.Fprintf(j.Out, "expires: %s\n", expiry)
		}
	}
	fmt.Fprintf(j.Out, "active: %t\n", strings.EqualFold(acc.Email, active))
	if acc.Disabled {
		tag := "[Disabled]"
		low := strings.ToLower(acc.DisabledReason)
		if strings.Contains(low, "terms of service") || strings.Contains(low, "tos") || strings.Contains(low, "disabled") {
			tag = "[ToS Banned]"
		}
		fmt.Fprintf(j.Out, "status: %s%s%s", palette.ToolFailedSuffixOpen, tag, palette.ToolBulletClose)
		if acc.DisabledReason != "" {
			fmt.Fprintf(j.Out, " (%s)", acc.DisabledReason)
		}
		fmt.Fprintln(j.Out)
	}
	if !acc.AddedAt.IsZero() {
		fmt.Fprintf(j.Out, "added: %s\n", acc.AddedAt.Format(time.RFC3339))
	}
	if !acc.LastUsed.IsZero() {
		fmt.Fprintf(j.Out, "last used: %s\n", acc.LastUsed.Format(time.RFC3339))
	}
	weekly, fiveHour := acc.QuotaColumns()
	fmt.Fprintf(j.Out, "quota: %s %s\n", weekly, fiveHour)
	fmt.Fprintf(j.Out, "registry: %s\n", accounts.Path())
	return nil
}

// runAccountsEnable clears the disabled / quarantined status of an account.
func runAccountsEnable(j *JetskiCLI, args []string) error {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return usageFail(j, usageAccountsEnable(j.Name), "Error: usage: accounts enable <email>")
	}
	email := strings.TrimSpace(args[0])
	st, err := accounts.Load()
	if err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	acc, ok := st.Find(email)
	if !ok {
		return errExitCodeMsg(1, fmt.Sprintf("Error: no account named %q", email))
	}
	if err := accounts.ClearDisabled(acc.Email); err != nil {
		return errExitCodeMsg(1, err.Error())
	}
	accountrouting.Shared().Unquarantine(acc.Email)
	fmt.Fprintf(j.Out, "enabled account: %s (quarantine cleared)\n", acc.Email)
	return nil
}

func usageAccountsEnable(n string) string {
	return "Usage: " + n + " accounts enable <email>\n" +
		"\n" +
		"Clear quarantine / disabled status of an account\n"
}

func usageAccountsShow(n string) string {
	return "Usage: " + n + " accounts show <email>\n" +
		"\n" +
		"Show one account in detail\n"
}

func usageAccountsAdd(n string) string {
	return "Usage: " + n + " accounts add [flags]\n" +
		"\n" +
		"Sign in to another Google account\n" +
		"\n" +
		"The command opens Google OAuth in the browser, prints the URL, reads\n" +
		"the authorization code from the terminal, exchanges it and stores the\n" +
		"account. It skips the /accounts panel's method menu: the method is\n" +
		"Google OAuth.\n" +
		"\n" +
		"Non-interactive form (from an existing credential):\n" +
		"\n" +
		"  --email          Account email; defaults to the token's email claim\n" +
		"  --token-file     Credential JSON file (agy's token format), or - for stdin\n" +
		"  --refresh-token  OAuth refresh token; $NAME reads it from the environment\n" +
		"  --access-token   OAuth access token; $NAME reads it from the environment\n" +
		"  --id-token       OIDC id token; carries the email claim\n" +
		"  --auth-method    Auth method recorded with the token (default consumer)\n" +
		"  --plan-tier      Cached plan tier shown by accounts list\n" +
		"  --expiry         Access-token expiry, RFC 3339\n" +
		"  --activate       Make the account active after adding it\n" +
		"\n" +
		"Notes:\n" +
		"  The browser flow makes the new account active, like /login and the\n" +
		"  panel's a key. The credential form adds without switching unless\n" +
		"  --activate is passed; --token-file takes precedence over the other\n" +
		"  token flags.\n"
}

// isInteractiveTerminal reports whether stdout is connected to a TTY and stdin is interactive.
func isInteractiveTerminal(out io.Writer) bool {
	fOut, okOut := out.(*os.File)
	if !okOut {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(fOut.Fd()))
}

// runAccountsTUI launches the interactive account and routing management TUI.
func runAccountsTUI(j *JetskiCLI, args []string) error {
	out := j.Out
	if out == nil {
		out = os.Stdout
	}
	seams := accountspanel.Seams{
		Close: func() tea.Cmd {
			return tea.Quit
		},
	}
	return accountspanel.RunStandalone(os.Stdin, out, seams)
}

func usageAccountsTUI(n string) string {
	return "Usage: " + n + " accounts tui\n" +
		"\n" +
		"Launch the interactive accounts and routing management TUI\n"
}
