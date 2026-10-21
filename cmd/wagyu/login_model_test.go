package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

func TestLoginModel_InitialView(t *testing.T) {
	m := newLoginModel()
	view := m.View().Content

	if !strings.Contains(view, "Welcome to the") || !strings.Contains(view, "Antigravity CLI") {
		t.Fatalf("expected welcome text in view, got:\n%s", view)
	}
	if !strings.Contains(view, "You are currently not signed in.") {
		t.Fatalf("expected not signed in message, got:\n%s", view)
	}
	if !strings.Contains(view, "Select login method:") {
		t.Fatalf("expected select login method, got:\n%s", view)
	}
	if !strings.Contains(view, "> 1. Google OAuth") {
		t.Fatalf("expected > on option 1, got:\n%s", view)
	}
	if !strings.Contains(view, "2. Use a Google Cloud project") {
		t.Fatalf("expected option 2, got:\n%s", view)
	}
	if !strings.Contains(view, "Navigate") || !strings.Contains(view, "Select") {
		t.Fatalf("expected navigate/select hints, got:\n%s", view)
	}
}

func TestLoginModel_Navigation(t *testing.T) {
	m := newLoginModel()

	// Down arrow moves cursor to 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("expected cursor 1 after down, got %d", m.cursor)
	}
	view := m.View().Content
	if !strings.Contains(view, "> 2. Use a Google Cloud project") {
		t.Fatalf("expected > on option 2, got:\n%s", view)
	}

	// Down arrow again wraps to 0
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 0 {
		t.Fatalf("expected cursor 0 after wrap, got %d", m.cursor)
	}

	// Up arrow wraps to 1
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("expected cursor 1 after up wrap, got %d", m.cursor)
	}
}

func TestLoginModel_OAuthFlowSelection(t *testing.T) {
	t.Setenv("OPENAGY_NO_BROWSER", "1")
	m := newLoginModel()
	m.cursor = 0

	// Press Enter to select Google OAuth
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != loginStateOAuth {
		t.Fatalf("expected state loginStateOAuth, got %v", m.state)
	}
	if m.verifier == "" || m.challenge == "" || m.authURL == "" {
		t.Fatalf("expected PKCE parameters generated, got verifier=%q, challenge=%q, url=%q", m.verifier, m.challenge, m.authURL)
	}

	view := m.View().Content
	if !strings.Contains(view, "Your browser should open automatically. If not:") {
		t.Fatalf("expected browser message in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Click here to authenticate") {
		t.Fatalf("expected authenticate link in view, got:\n%s", view)
	}
	if !strings.Contains(view, "authorization code...") {
		t.Fatalf("expected code placeholder in view, got:\n%s", view)
	}

	// Type code
	for _, ch := range "4/0A12345" {
		m.Update(tea.KeyPressMsg{Text: string(ch)})
	}
	if m.codeInput != "4/0A12345" {
		t.Fatalf("codeInput = %q, want %q", m.codeInput, "4/0A12345")
	}

	// Backspace
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.codeInput != "4/0A1234" {
		t.Fatalf("after backspace codeInput = %q, want %q", m.codeInput, "4/0A1234")
	}

	// Esc goes back to select method
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.state != loginStateSelectMethod {
		t.Fatalf("expected state loginStateSelectMethod after Esc, got %v", m.state)
	}
}

func TestLoginModel_OAuthPaste(t *testing.T) {
	m := newLoginModel()
	m.state = loginStateOAuth

	// 1. Terminal bracketed paste via tea.PasteMsg
	m.Update(tea.PasteMsg{Content: "  4/pasted-code-123  \n"})
	if m.codeInput != "4/pasted-code-123" {
		t.Fatalf("codeInput after PasteMsg = %q, want %q", m.codeInput, "4/pasted-code-123")
	}

	m.codeInput = ""

	// 2. Ctrl+V via clipboardReadText seam
	origClipboard := clipboardReadText
	t.Cleanup(func() { clipboardReadText = origClipboard })
	clipboardReadText = func() string { return "  4/ctrl-v-code-456  \n" }

	m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if m.codeInput != "4/ctrl-v-code-456" {
		t.Fatalf("codeInput after Ctrl+V = %q, want %q", m.codeInput, "4/ctrl-v-code-456")
	}

	m.codeInput = ""

	// 3. Cmd+V (ModSuper)
	m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModSuper})
	if m.codeInput != "4/ctrl-v-code-456" {
		t.Fatalf("codeInput after Cmd+V = %q, want %q", m.codeInput, "4/ctrl-v-code-456")
	}
}

func TestLoginModel_CloudProjectSelection(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	m := newLoginModel()
	m.cursor = 1

	// Press Enter to select Google Cloud project
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != loginStateCloudProject {
		t.Fatalf("expected state loginStateCloudProject, got %v", m.state)
	}

	view := m.View().Content
	if !strings.Contains(view, "Google Cloud sign-in method:") {
		t.Fatalf("expected Google Cloud sign-in header, got:\n%s", view)
	}
	if !strings.Contains(view, "> 1. Continue with Google Cloud") {
		t.Fatalf("expected option 1 highlighted, got:\n%s", view)
	}

	// Enter on option 1 finishes login with ADC
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.authenticated {
		t.Fatal("expected authenticated to be true")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command returned")
	}
	// Clean up environment for subsequent tests
	os.Unsetenv("AGY_ADC_AUTH")
}

func TestLoginModel_ExchangeSuccessAndError(t *testing.T) {
	m := newLoginModel()
	m.state = loginStateExchanging

	// Error msg
	m.Update(exchangeErrMsg(errors.New("invalid authorization code")))
	if m.state != loginStateError {
		t.Fatalf("expected state loginStateError, got %v", m.state)
	}
	errView := m.View().Content
	if !strings.Contains(errView, "Authentication failed: invalid authorization code") {
		t.Fatalf("expected error message in view, got:\n%s", errView)
	}

	// Esc returns to select method
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.state != loginStateSelectMethod {
		t.Fatalf("expected state loginStateSelectMethod after Esc from error, got %v", m.state)
	}

	// Success msg
	m.state = loginStateExchanging
	_, quitCmd := m.Update(exchangeSuccessMsg(&auth.AntigravityToken{}))
	if !m.authenticated {
		t.Fatal("expected authenticated to be true on success")
	}
	if quitCmd == nil {
		t.Fatal("expected tea.Quit on success")
	}
	succView := m.View().Content
	if !strings.Contains(succView, "Authentication successful!") {
		t.Fatalf("expected Authentication successful in view, got:\n%s", succView)
	}
}

func TestSlashLogout_GeminiAPIKeyEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "fake-key")
	t.Setenv("AGY_ADC_AUTH", "")

	got := logoutFeedback()
	want := "The active session uses a Gemini API key from the environment (GEMINI_API_KEY). To change it, update or unset the environment variable."
	if got != want {
		t.Fatalf("logoutFeedback() with GEMINI_API_KEY = %q, want %q", got, want)
	}
}

func TestSlashLogout_AdcAuthEnv(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "1")

	got := logoutFeedback()
	want := "The active session uses Application Default Credentials (AGY_ADC_AUTH). To change accounts, run 'gcloud auth application-default login' or unset the variable."
	if got != want {
		t.Fatalf("logoutFeedback() with AGY_ADC_AUTH = %q, want %q", got, want)
	}
}

func TestSlashLogout_FullClear(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("AGY_ADC_AUTH", "")

	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	tokenFile := filepath.Join(home, "antigravity-oauth-token")
	if err := os.WriteFile(tokenFile, []byte(`{"token":{"access_token":"foo"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	keychainCleared := false
	auth.ClearKeychainTokenFn = func() error {
		keychainCleared = true
		return nil
	}
	defer func() { auth.ClearKeychainTokenFn = nil }()

	m := newSlashModel(t)
	m.handleSlashCommand("/logout")

	wantPrefix := "Signed out from server:"
	output := strings.Join(afterEcho(m), "\n")
	if !strings.HasPrefix(output, wantPrefix) {
		t.Fatalf("/logout output = %q, want prefix %q", output, wantPrefix)
	}
	if _, err := os.Stat(tokenFile); !os.IsNotExist(err) {
		t.Fatalf("expected tokenFile %s to be deleted, got err: %v", tokenFile, err)
	}
	if !keychainCleared {
		t.Fatal("expected keychain to be cleared by /logout")
	}
}

func TestSlashLogin_Handler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")

	m := newSlashModel(t)
	handled, cmd := m.handleSlashCommand("/login")
	if !handled {
		t.Fatal("expected /login to be handled")
	}
	if m.login == nil {
		t.Fatal("expected m.login to be active without restarting CLI")
	}
	if cmd == nil {
		t.Fatal("expected login init command returned")
	}

	// Verify view switches to login view
	view := m.View().Content
	if !strings.Contains(view, "Welcome to the") || !strings.Contains(view, "You are currently not signed in.") {
		t.Fatalf("expected login view in m.View(), got:\n%s", view)
	}

	// Simulate successful auth
	m.Update(exchangeSuccessMsg(&auth.AntigravityToken{Token: auth.AntigravityAccessToken{AccessToken: "tok123"}}))
	if m.login != nil {
		t.Fatal("expected m.login to be cleared after successful auth")
	}
	out := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(out, "Authentication successful!") {
		t.Fatalf("expected 'Authentication successful!' after login, got: %q", out)
	}
}

func TestOAuthPKCE_GenerationAndExchange(t *testing.T) {
	verifier, challenge, state, err := auth.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}
	if len(verifier) == 0 || len(challenge) == 0 || len(state) == 0 {
		t.Fatalf("expected non-empty PKCE parameters, got verifier=%q challenge=%q state=%q", verifier, challenge, state)
	}

	authURL := auth.BuildAuthorizationURL(challenge, state)
	if !strings.HasPrefix(authURL, "https://accounts.google.com/o/oauth2/auth?") {
		t.Fatalf("expected google oauth URL, got %q", authURL)
	}
	for _, expectedParam := range []string{
		"access_type=offline",
		"code_challenge=" + challenge,
		"code_challenge_method=S256",
		"prompt=consent",
		"redirect_uri=https%3A%2F%2Fantigravity.google%2Foauth-callback",
		"state=" + state,
	} {
		if !strings.Contains(authURL, expectedParam) {
			t.Errorf("authURL missing %q:\n%s", expectedParam, authURL)
		}
	}

	// Test code exchange with mock token server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "valid-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"access_token": "ya29.test-access-token",
			"refresh_token": "1//test-refresh-token",
			"expires_in": 3600,
			"token_type": "Bearer",
			"id_token": "test-id-token"
		}`))
	}))
	defer srv.Close()

	origTokenURL := auth.TokenURL
	auth.TokenURL = srv.URL
	defer func() { auth.TokenURL = origTokenURL }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", home)

	// Exchange valid code
	tok, err := auth.ExchangeAuthorizationCode(t.Context(), "valid-code", verifier)
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode failed: %v", err)
	}
	if tok.Token.AccessToken != "ya29.test-access-token" {
		t.Fatalf("got access token %q, want ya29.test-access-token", tok.Token.AccessToken)
	}
	if tok.Token.RefreshToken != "1//test-refresh-token" {
		t.Fatalf("got refresh token %q, want 1//test-refresh-token", tok.Token.RefreshToken)
	}

	// Exchange invalid code
	_, err = auth.ExchangeAuthorizationCode(t.Context(), "bad-code", verifier)
	if err == nil {
		t.Fatal("expected error for bad-code, got nil")
	}
}
