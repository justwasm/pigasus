package rcfrontend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
)

// isolateAccounts points the account registry and the shared credential
// paths at a temp directory, so a test never touches the real keychain
// or the real ~/.config.
func isolateAccounts(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(home, "accounts.json"))
	t.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")
}

// fakeAccountsToken builds a token whose id_token carries an email
// claim, the way Google's id_token does.
func fakeAccountsToken(t *testing.T, email, access, refresh string) *auth.AntigravityToken {
	t.Helper()
	claims, err := json.Marshal(map[string]any{"email": email, "sub": "1"})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	id := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	return &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  access,
			TokenType:    "Bearer",
			RefreshToken: refresh,
			Expiry:       time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		AuthMethod: "consumer",
		IDToken:    id,
	}
}

// registerFakeAccount adds an account to the registry and makes it the
// active one.
func registerFakeAccount(t *testing.T, email, access, refresh string) {
	t.Helper()
	if _, _, err := accounts.RegisterWithEmail(email, fakeAccountsToken(t, email, access, refresh)); err != nil {
		t.Fatalf("RegisterWithEmail(%s): %v", email, err)
	}
}

// newAccountsServer returns a server and the token its page carries.
func newAccountsServer(t *testing.T, opts Options) (*Server, string) {
	t.Helper()
	srv := NewServer(opts)
	return srv, srv.acct.token
}

// callAccounts performs a request against the accounts API.
func callAccounts(t *testing.T, srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if token != "" {
		req.Header.Set(accountsTokenHeader, token)
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr
}

// decodeAccountsPayload reads the list answer.
func decodeAccountsPayload(t *testing.T, rr *httptest.ResponseRecorder) accountsPayload {
	t.Helper()
	var payload accountsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return payload
}

func TestAccountsAPI_ListAnswersTheRegistry(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s returned status %d, want 200: %s", accountsAPIPath, rr.Code, rr.Body.String())
	}
	payload := decodeAccountsPayload(t, rr)
	if len(payload.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(payload.Accounts))
	}
	if payload.Accounts[0].Email != "a@example.com" {
		t.Fatalf("email = %q, want a@example.com", payload.Accounts[0].Email)
	}
	if !payload.Accounts[0].Active {
		t.Error("the only account is not marked active")
	}
	if payload.Active != "a@example.com" {
		t.Errorf("active = %q, want a@example.com", payload.Active)
	}
	if payload.Registry != accounts.Path() {
		t.Errorf("registryPath = %q, want %q", payload.Registry, accounts.Path())
	}
}

// TestAccountsAPI_RefusesACallerWithoutTheToken pins the guard: the
// endpoint mutates the credential, so only a client that loaded the
// server's page may act.
func TestAccountsAPI_RefusesACallerWithoutTheToken(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("GET %s without a token returned status %d, want 403", accountsAPIPath, rr.Code)
	}

	rr = callAccounts(t, srv, http.MethodPost, accountsSwitchPath, `{"email":"a@example.com"}`, "wrong")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("a switch with a wrong token returned status %d, want 403", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "error") {
		t.Errorf("the refusal does not carry an error: %s", rr.Body.String())
	}
}

func TestAccountsAPI_SwitchActivatesTheAccount(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")
	registerFakeAccount(t, "b@example.com", "at-b", "rt-b")

	rr := callAccounts(t, srv, http.MethodPost, accountsSwitchPath, `{"email":"b@example.com"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("the switch returned status %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeAccountsPayload(t, rr)
	if payload.Active != "b@example.com" {
		t.Fatalf("active = %q, want b@example.com", payload.Active)
	}
	for _, acc := range payload.Accounts {
		want := acc.Email == "b@example.com"
		if acc.Active != want {
			t.Errorf("%s active = %v, want %v", acc.Email, acc.Active, want)
		}
	}
	// The shared credential follows the switch, so the running session
	// and the installed `agy` use the new account.
	_, email := accounts.Current()
	if email != "b@example.com" {
		t.Fatalf("the shared credential is %q, want b@example.com", email)
	}
}

func TestAccountsAPI_RemoveForgetsAnAccount(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")
	registerFakeAccount(t, "b@example.com", "at-b", "rt-b")

	rr := callAccounts(t, srv, http.MethodPost, accountsRemovePath, `{"email":"b@example.com"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("the removal returned status %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeAccountsPayload(t, rr)
	if len(payload.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(payload.Accounts))
	}
	if payload.Accounts[0].Email != "a@example.com" {
		t.Fatalf("email = %q, want a@example.com", payload.Accounts[0].Email)
	}
}

// TestAccountsAPI_RemoveTheActiveAccountSignsOutForReal pins the TUI's
// rule: removing the active account signs out — the shared credential
// is cleared — and the session carries on with another account.
func TestAccountsAPI_RemoveTheActiveAccountSignsOutForReal(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")
	registerFakeAccount(t, "b@example.com", "at-b", "rt-b")

	rr := callAccounts(t, srv, http.MethodPost, accountsRemovePath, `{"email":"b@example.com"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("the removal returned status %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeAccountsPayload(t, rr)
	if payload.Active != "a@example.com" {
		t.Fatalf("active = %q, want a@example.com", payload.Active)
	}
	_, email := accounts.Current()
	if email != "a@example.com" {
		t.Fatalf("the shared credential is %q, want a@example.com", email)
	}
}

func TestAccountsAPI_UnknownAccountIsAnError(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})

	rr := callAccounts(t, srv, http.MethodPost, accountsSwitchPath, `{"email":"nobody@example.com"}`, token)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("the switch returned status %d, want 500: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "nobody@example.com") {
		t.Errorf("the error does not name the account: %s", rr.Body.String())
	}
}

// TestAccountsAPI_SwitchWithoutAnEmailIsRejected pins the parameter
// check: a switch without an account is a client mistake, not a server
// error.
func TestAccountsAPI_SwitchWithoutAnEmailIsRejected(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})

	rr := callAccounts(t, srv, http.MethodPost, accountsSwitchPath, `{}`, token)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("the switch returned status %d, want 400: %s", rr.Code, rr.Body.String())
	}
}

// TestAccountsAPI_AnswersAPostFromTheTunnel pins the tunnel's shape:
// every proxied request is a POST, so the list endpoint answers one.
func TestAccountsAPI_AnswersAPostFromTheTunnel(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	rr := callAccounts(t, srv, http.MethodPost, accountsAPIPath, "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST %s returned status %d, want 200: %s", accountsAPIPath, rr.Code, rr.Body.String())
	}
	if len(decodeAccountsPayload(t, rr).Accounts) != 1 {
		t.Error("the POST did not answer the account list")
	}
}

// TestAccountsWidget_IsInThePage pins the widget's mount: the page the
// server serves carries the script, and the script carries the token
// the API checks.
func TestAccountsWidget_IsInThePage(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{EnableTouchPatches: true})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"agy-accounts-button",
		"pane-titlebar-toolbar",
		accountsTokenHeader,
		token,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}
}

// TestAccountsWidget_CanBeTurnedOff pins the opt-out: a server that
// was asked not to show the widget serves neither the script nor the
// API.
func TestAccountsWidget_CanBeTurnedOff(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{DisableAccountSwitcher: true})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rr.Body.String(), "agy-accounts-button") {
		t.Error("the page carries the widget although it was disabled")
	}

	rr = callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	if rr.Code == http.StatusOK {
		t.Error("the API answers although the widget was disabled")
	}
}

// TestAccountsAPI_TokenIsPerServer pins the guard's strength: two
// servers do not share a token, so one server's page cannot drive the
// other server's API.
func TestAccountsAPI_TokenIsPerServer(t *testing.T) {
	isolateAccounts(t)
	first, firstToken := newAccountsServer(t, Options{})
	second, _ := newAccountsServer(t, Options{})
	if firstToken == "" {
		t.Fatal("the server has no token")
	}
	_ = first

	rr := callAccounts(t, second, http.MethodGet, accountsAPIPath, "", firstToken)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("another server's token returned status %d, want 403", rr.Code)
	}
}

// TestAccountsAPI_AddStartsASignIn pins the add endpoint's first half:
// it answers the authorization URL the user is sent to, and keeps the
// PKCE verifier on the server.
func TestAccountsAPI_AddStartsASignIn(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})

	rr := callAccounts(t, srv, http.MethodPost, accountsAddPath, `{}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("the add returned status %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload signinPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	if payload.ID == "" {
		t.Error("the sign-in has no id")
	}
	if payload.Status != signinPending {
		t.Errorf("status = %q, want %q", payload.Status, signinPending)
	}
	if !strings.HasPrefix(payload.AuthURL, endpoints.Current().OAuth2Auth+"?") {
		t.Errorf("authUrl = %q, want the authorization page", payload.AuthURL)
	}
	if !strings.Contains(payload.AuthURL, "code_challenge=") {
		t.Error("the authorization URL carries no PKCE challenge")
	}
	// The verifier never leaves the server: the answer has no field
	// for it.
	if strings.Contains(rr.Body.String(), "verifier") {
		t.Errorf("the answer leaks the verifier: %s", rr.Body.String())
	}
}

// TestAccountsAPI_AddNeedsTheToken pins the guard on the add endpoint:
// it writes the shared credential, so it is guarded like the others.
func TestAccountsAPI_AddNeedsTheToken(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{})

	rr := callAccounts(t, srv, http.MethodPost, accountsAddPath, `{}`, "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("the add without a token returned status %d, want 403", rr.Code)
	}
}

// TestAccountsAPI_AddWithAnUnknownSignIn pins the answer for a
// sign-in the server does not know: the state query and the exchange
// both refuse it.
func TestAccountsAPI_AddWithAnUnknownSignIn(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})

	rr := callAccounts(t, srv, http.MethodPost, accountsAddPath, `{"id":"nope"}`, token)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("the state query returned status %d, want 404: %s", rr.Code, rr.Body.String())
	}

	rr = callAccounts(t, srv, http.MethodPost, accountsAddPath, `{"id":"nope","code":"abc"}`, token)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("the exchange returned status %d, want 404: %s", rr.Code, rr.Body.String())
	}
}

// cacheQuota gives an account a quota snapshot, the way the /accounts
// panel's background fetcher does.
func cacheQuota(t *testing.T, email string) {
	t.Helper()
	err := accounts.Update(email, func(a *accounts.Account) {
		a.Quota = &auth.QuotaSnapshot{
			FetchedAt: time.Now(),
			Groups: []auth.QuotaGroupSnapshot{{
				Name: "GEMINI MODELS",
				Buckets: []auth.QuotaBucketSnapshot{{
					DisplayName:       "Weekly Limit Remaining",
					Window:            "weekly",
					RemainingFraction: 0.452,
					ResetTime:         time.Now().Add(72 * time.Hour),
				}},
			}},
		}
	})
	if err != nil {
		t.Fatalf("cache the quota of %s: %v", email, err)
	}
}

// TestAccountsAPI_QuotaAnswersTheCachedQuota pins the quota endpoint:
// it answers the account's remaining quota in the shape the web UI's
// quota section renders — one group per model family, one bucket per
// window.
func TestAccountsAPI_QuotaAnswersTheCachedQuota(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")
	cacheQuota(t, "a@example.com")

	rr := callAccounts(t, srv, http.MethodGet, accountsQuotaPath, "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s returned status %d, want 200: %s", accountsQuotaPath, rr.Code, rr.Body.String())
	}
	var payload accountsQuotaPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	if payload.Email != "a@example.com" {
		t.Errorf("email = %q, want a@example.com", payload.Email)
	}
	if payload.Usage == nil || len(payload.Usage.Groups) != 1 {
		t.Fatalf("usage = %+v, want one group", payload.Usage)
	}
	buckets := payload.Usage.Groups[0].Buckets
	if len(buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(buckets))
	}
	if buckets[0].Label != "Weekly Limit Remaining" {
		t.Errorf("label = %q, want Weekly Limit Remaining", buckets[0].Label)
	}
	if buckets[0].Window != auth.QuotaWindowWeekly {
		t.Errorf("window = %q, want %q", buckets[0].Window, auth.QuotaWindowWeekly)
	}
	if buckets[0].RemainingFraction != 0.452 {
		t.Errorf("remainingFraction = %v, want 0.452", buckets[0].RemainingFraction)
	}
	if buckets[0].ResetAt == "" {
		t.Error("the bucket has no reset time")
	}
}

// TestAccountsAPI_QuotaNeedsTheToken pins the guard: the quota is the
// account's own usage, so only the page's own client may read it.
func TestAccountsAPI_QuotaNeedsTheToken(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	rr := callAccounts(t, srv, http.MethodGet, accountsQuotaPath, "", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("GET %s without a token returned status %d, want 403", accountsQuotaPath, rr.Code)
	}
}

// TestAccountsAPI_QuotaWithoutAnAccount pins the answer for a session
// that is not signed in: there is no quota to report.
func TestAccountsAPI_QuotaWithoutAnAccount(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})

	rr := callAccounts(t, srv, http.MethodGet, accountsQuotaPath, "", token)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET %s returned status %d, want 404: %s", accountsQuotaPath, rr.Code, rr.Body.String())
	}
}

// TestAccountsAPI_ListCarriesTheUsage pins the account list: every row
// carries its own quota, so the widget can show which account is left
// how much without a query per account.
func TestAccountsAPI_ListCarriesTheUsage(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")
	cacheQuota(t, "a@example.com")

	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	payload := decodeAccountsPayload(t, rr)
	if len(payload.Accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(payload.Accounts))
	}
	usage := payload.Accounts[0].Usage
	if usage == nil || len(usage.Groups) != 1 {
		t.Fatalf("usage = %+v, want one group", usage)
	}
	if payload.Accounts[0].Quota == "" {
		t.Error("the row carries no quota columns")
	}
}

// TestSigninStatusAnswersTheLiveEmail pins the signin endpoint: the
// address it answers is the account that is signed in *now*, not the
// one the server was told when it started. A switch changes the shared
// credential without restarting the server.
func TestSigninStatusAnswersTheLiveEmail(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{AccountEmail: "stale@example.com"})
	registerFakeAccount(t, "live@example.com", "at-live", "rt-live")
	// The switch writes the shared credential; that is what the
	// endpoint reads.
	if _, err := accounts.Activate(context.Background(), "live@example.com"); err != nil {
		t.Fatalf("activate live@example.com: %v", err)
	}

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/__agy/api/signin/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /__agy/api/signin/status returned status %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "live@example.com") {
		t.Errorf("the answer does not carry the live address: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "stale@example.com") {
		t.Errorf("the answer carries the start-up address: %s", rr.Body.String())
	}
}

// TestAccountsWidget_CarriesTheQuotaAndTheFixups pins the widget's
// script: it reads the quota, and it puts the live account where the
// app shows the address it was told at start-up.
//
// The app's own quota panel is *not* patched: the
// RetrieveUserQuotaSummary route answers it (the dispatcher, not the
// widget), so the app renders its own rows and ring gauges. A widget
// that drew its own rows would replace the app's component with a
// hand-made one.
func TestAccountsWidget_CarriesTheQuotaAndTheFixups(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rr.Body.String()
	for _, want := range []string{
		`var QUOTA = API + "/quota";`,
		"settings-nav-item-Account",
		"agy-accounts-toast",
		// The quota is drawn as a bar per window, the percentage on
		// the right and the countdown below — the density of the
		// TUI's /usage panel — and the other model groups are behind
		// a disclosure.
		"agy-accounts-quota-bar",
		"agy-accounts-quota-pct",
		"agy-accounts-group-toggle",
		"Refreshes in ",
		// The list scrolls on its own: the panel is rebuilt on every
		// change, and a rebuild that dropped the scroll position
		// would leave a scrollbar that cannot be scrolled.
		"overscroll-behavior:contain",
		"list.scrollTop = keepTop",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not carry %q", want)
		}
	}
	for _, unwanted := range []string{
		"No quota information available.",
		"agy-usage-row",
		"agy-usage-pct",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the page still carries the hand-made quota rows (%q)", unwanted)
		}
	}
}
