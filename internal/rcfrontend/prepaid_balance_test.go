// prepaid_balance_test.go — AGY 1.5.17: the Remote Control browser view's
// `Organization prepaid wallet is empty` notice.
//
//	"Fixed turns on Gemini Enterprise accounts with an empty prepaid wallet
//	 retrying repeatedly … (with an `Organization prepaid wallet is empty`
//	 notice in the Remote Control browser view)."
//
// # Where the strings come from
//
// The notice is not invented here: it is the reference's own copy, recovered
// from the 1.5.17 web bundle (out/extracted-1.5.17/web-assets/main.js), which
// renders it as a non-dismissible warning:
//
//	case "prepaidBalanceExhausted": return {
//	  id: c, icon: "warning",
//	  title: "Organization prepaid wallet is empty",
//	  message: "Your organization has run out of Antigravity funds. Prompt
//	            generation is paused until your administrator adds funds.",
//	  dismissible: false};
//
// # What is actually wired
//
// The notice is *data*, not a string patched into the bundle: the accounts
// endpoint carries `prepaidBalanceExhausted`, the widget's script is served in
// the page, and the script draws the notice from the payload. So the tests
// below check, in order: the widget carries the copy and the drawing code, the
// endpoint carries the flag, and the deliberate absence of a dismiss control
// holds (the reference's notice is non-dismissible).
package rcfrontend

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/apierror"
)

// TestAccountsWidget_CarriesThePrepaidWalletNotice pins the notice in the
// served page: the recovered title, the explanation, the drawing helper, and
// the two places it can appear (beside the titlebar button and at the top of
// the panel).
func TestAccountsWidget_CarriesThePrepaidWalletNotice(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d, want 200", rr.Code)
	}
	body := rr.Body.String()

	// The recovered title is the same literal the CLI-side constant
	// carries: one condition, one sentence, whichever view shows it.
	if !strings.Contains(body, `var WALLET_TITLE = "`+apierror.PrepaidBalanceNotice+`";`) {
		t.Errorf("the page does not carry the recovered notice title %q", apierror.PrepaidBalanceNotice)
	}
	for _, want := range []string{
		"Your organization has run out of Antigravity funds. Prompt generation is paused until your administrator adds funds.",
		// The drawing code: the panel block and the page-level notice.
		"function renderWalletBlock()",
		"function renderWalletNotice(host)",
		"agy-accounts-wallet",
		"agy-accounts-wallet-title",
		"agy-accounts-wallet-body",
		// The server's flag is what drives it.
		"prepaidBalanceExhausted",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the widget script does not carry %q", want)
		}
	}

	// The notice is non-dismissible, like the reference's: it carries no
	// close control. The widget's *other* notices (the toasts) do have an
	// auto-remove timer, and the wallet block must not have borrowed it.
	start := strings.Index(body, "function renderWalletBlock()")
	if start < 0 {
		t.Fatal("renderWalletBlock is not in the page")
	}
	end := strings.Index(body[start:], "function renderWalletNotice")
	if end <= 0 {
		t.Fatal("renderWalletBlock has no body")
	}
	block := body[start : start+end]
	if strings.Contains(block, "remove(") || strings.Contains(block, "setTimeout") ||
		strings.Contains(block, "close") || strings.Contains(block, "dismiss") {
		t.Errorf("the wallet notice carries a dismiss control; the reference's does not:\n%s", block)
	}
	if !strings.Contains(block, `wrap.setAttribute("role", "alert")`) {
		t.Error("the wallet notice is not announced as an alert")
	}
}

// TestAccountsAPI_CarriesThePrepaidWalletFlag pins the data path: the flag
// rides on the account list the widget already reads, so the notice appears
// without a second endpoint and survives a re-render.
func TestAccountsAPI_CarriesThePrepaidWalletFlag(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{PrepaidBalanceExhausted: true})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s returned status %d: %s", accountsAPIPath, rr.Code, rr.Body.String())
	}
	var payload accountsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	if !payload.PrepaidBalanceExhausted {
		t.Errorf("the list answer does not carry the notice flag: %s", rr.Body.String())
	}
}

// TestAccountsAPI_PrepaidWalletFlagIsOffByDefault pins the default: a server
// that was not told about the condition says nothing about it. The field is
// omitempty, so an unaffected page sees exactly the JSON it saw before.
func TestAccountsAPI_PrepaidWalletFlagIsOffByDefault(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s returned status %d: %s", accountsAPIPath, rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "prepaidBalanceExhausted") {
		t.Errorf("an unaffected server emitted the flag: %s", rr.Body.String())
	}
}

// TestPrepaidWalletNotice_TurnsOnAtRuntime pins the mid-session path: the
// condition is learned when a turn fails, after the server is already running,
// so the flag has to be settable and visible to the very next answer.
func TestPrepaidWalletNotice_TurnsOnAtRuntime(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	if srv.PrepaidBalanceExhausted() {
		t.Fatal("a fresh server claims the wallet is empty")
	}
	srv.NotePrepaidBalanceExhausted()
	if !srv.PrepaidBalanceExhausted() {
		t.Fatal("NotePrepaidBalanceExhausted did not take")
	}
	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	var payload accountsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	if !payload.PrepaidBalanceExhausted {
		t.Errorf("the notice did not reach the next answer: %s", rr.Body.String())
	}

	// A fresh quota read is the only thing that clears it.
	srv.ClearPrepaidBalanceExhausted()
	if srv.PrepaidBalanceExhausted() {
		t.Error("ClearPrepaidBalanceExhausted did not take")
	}
}

// TestPrepaidWalletNotice_FromTheTurnError pins the wiring the turn loop uses:
// it hands the server the error it already has, and the notice follows without
// the caller classifying anything.
func TestPrepaidWalletNotice_FromTheTurnError(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")

	prepaid := apierror.NewHTTPError(429, []byte(`{"error":{"message":"You have exhausted your quota on this model.",`+
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"PREPAID_BALANCE_EXHAUSTED"}]}}`), "")
	wrapped := fmt.Errorf("agent: open stream: %w", prepaid)

	if !srv.NotePrepaidBalanceError(wrapped) {
		t.Fatal("NotePrepaidBalanceError did not recognise the prepaid failure")
	}
	rr := callAccounts(t, srv, http.MethodGet, accountsAPIPath, "", token)
	var payload accountsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	if !payload.PrepaidBalanceExhausted {
		t.Errorf("the notice did not reach the next answer: %s", rr.Body.String())
	}
}

// TestPrepaidWalletNotice_OtherErrorsDoNotRaiseIt is the negative half of the
// wiring: an ordinary quota failure — the very sentence the changelog quotes as
// the WRONG message — must not raise the notice.
func TestPrepaidWalletNotice_OtherErrorsDoNotRaiseIt(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{})

	ordinary := []error{
		nil,
		errors.New("You have exhausted your quota on this model."),
		apierror.NewHTTPError(429, []byte(`{"error":{"message":"You have exhausted your quota on this model."}}`), ""),
		apierror.NewHTTPError(429, []byte(`{"error":{"message":"Too many requests","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"RATE_LIMIT_EXCEEDED"}]}}`), ""),
	}
	for _, err := range ordinary {
		if srv.NotePrepaidBalanceError(err) {
			t.Errorf("NotePrepaidBalanceError(%v) = true, want false", err)
		}
	}
	if srv.PrepaidBalanceExhausted() {
		t.Error("an ordinary failure raised the notice")
	}
}

// TestPrepaidWalletNotice_SurvivesASwitch pins the reason the flag lives on
// the payload rather than in the widget's local state: the switch re-reads the
// list, and a notice kept only in the DOM would vanish on it. The notice is a
// property of the organization, not of the account that was selected.
func TestPrepaidWalletNotice_SurvivesASwitch(t *testing.T) {
	isolateAccounts(t)
	srv, token := newAccountsServer(t, Options{PrepaidBalanceExhausted: true})
	registerFakeAccount(t, "a@example.com", "at-a", "rt-a")
	registerFakeAccount(t, "b@example.com", "at-b", "rt-b")

	rr := callAccounts(t, srv, http.MethodPost, accountsSwitchPath, `{"email":"b@example.com"}`, token)
	if rr.Code != http.StatusOK {
		t.Fatalf("switch returned status %d: %s", rr.Code, rr.Body.String())
	}
	var payload accountsPayload
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	if !payload.PrepaidBalanceExhausted {
		t.Errorf("the notice was dropped by the switch answer: %s", rr.Body.String())
	}
	if payload.Active != "b@example.com" {
		t.Errorf("active = %q, want b@example.com", payload.Active)
	}
}

// TestPrepaidWalletNotice_DisabledWithTheSwitcher pins the flag's scope: the
// notice belongs to the account widget, so a page served without the widget
// has no notice either — and, more importantly, does not grow a second copy of
// the copy that would drift from the widget's.
func TestPrepaidWalletNotice_DisabledWithTheSwitcher(t *testing.T) {
	isolateAccounts(t)
	srv, _ := newAccountsServer(t, Options{PrepaidBalanceExhausted: true, DisableAccountSwitcher: true})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(rr.Body.String(), "WALLET_TITLE") {
		t.Error("a page without the account widget carried the wallet notice")
	}
}
