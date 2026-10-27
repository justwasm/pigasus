package loginui_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/loginui"
)

func TestLoginModel_InitialView(t *testing.T) {
	m := loginui.New()
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
	m := loginui.New()

	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.Cursor != 1 {
		t.Fatalf("expected cursor 1 after down, got %d", m.Cursor)
	}
	view := m.View().Content
	if !strings.Contains(view, "> 2. Use a Google Cloud project") {
		t.Fatalf("expected > on option 2, got:\n%s", view)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.Cursor != 0 {
		t.Fatalf("expected cursor 0 after wrap, got %d", m.Cursor)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.Cursor != 1 {
		t.Fatalf("expected cursor 1 after up wrap, got %d", m.Cursor)
	}
}

func TestLoginModel_OAuthFlowSelection(t *testing.T) {
	t.Setenv("OPENAGY_NO_BROWSER", "1")
	m := loginui.New()
	m.Cursor = 0
	// The screen is scrollable: the code prompt sits below the fold
	// on a short terminal, the same way it does in the reference.
	m.Height = 40

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.State != loginui.StateOAuth {
		t.Fatalf("expected state StateOAuth, got %v", m.State)
	}
	if m.Verifier == "" || m.Challenge == "" || m.AuthURL == "" {
		t.Fatalf("expected PKCE parameters generated, got verifier=%q, challenge=%q, url=%q", m.Verifier, m.Challenge, m.AuthURL)
	}

	view := m.View().Content
	if !strings.Contains(view, "Your browser should open automatically. If not:") {
		t.Fatalf("expected browser message in view, got:\n%s", view)
	}
	if !strings.Contains(view, "authorization code...") {
		t.Fatalf("expected code placeholder in view, got:\n%s", view)
	}

	for _, ch := range "4/0A12345" {
		m.Update(tea.KeyPressMsg{Text: string(ch)})
	}
	if m.CodeInput != "4/0A12345" {
		t.Fatalf("codeInput = %q, want %q", m.CodeInput, "4/0A12345")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.CodeInput != "4/0A1234" {
		t.Fatalf("after backspace codeInput = %q, want %q", m.CodeInput, "4/0A1234")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.State != loginui.StateSelectMethod {
		t.Fatalf("expected state StateSelectMethod after Esc, got %v", m.State)
	}
}

func TestLoginModel_OAuthPaste(t *testing.T) {
	m := loginui.New()
	m.State = loginui.StateOAuth

	m.Update(tea.PasteMsg{Content: "  4/pasted-code-123  \n"})
	if m.CodeInput != "4/pasted-code-123" {
		t.Fatalf("codeInput after PasteMsg = %q, want %q", m.CodeInput, "4/pasted-code-123")
	}

	m.CodeInput = ""

	origClipboard := loginui.ClipboardReadText
	t.Cleanup(func() { loginui.ClipboardReadText = origClipboard })
	loginui.ClipboardReadText = func() string { return "  4/ctrl-v-code-456  \n" }

	m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if m.CodeInput != "4/ctrl-v-code-456" {
		t.Fatalf("codeInput after Ctrl+V = %q, want %q", m.CodeInput, "4/ctrl-v-code-456")
	}

	m.CodeInput = ""

	m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModSuper})
	if m.CodeInput != "4/ctrl-v-code-456" {
		t.Fatalf("codeInput after Cmd+V = %q, want %q", m.CodeInput, "4/ctrl-v-code-456")
	}
}

func TestLoginModel_CloudProjectSelection(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	m := loginui.New()
	m.Cursor = 1

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.State != loginui.StateCloudProject {
		t.Fatalf("expected state StateCloudProject, got %v", m.State)
	}

	view := m.View().Content
	if !strings.Contains(view, "Google Cloud sign-in method:") {
		t.Fatalf("expected Google Cloud sign-in header, got:\n%s", view)
	}
	if !strings.Contains(view, "> 1. Continue with Google Cloud") {
		t.Fatalf("expected option 1 highlighted, got:\n%s", view)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.Authenticated {
		t.Fatal("expected authenticated to be true")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command returned")
	}
	os.Unsetenv("AGY_ADC_AUTH")
}

func TestOAuthPKCE_Generation(t *testing.T) {
	verifier, challenge, state, err := auth.GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}
	if len(verifier) == 0 || len(challenge) == 0 || len(state) == 0 {
		t.Fatalf("expected non-empty PKCE parameters, got verifier=%q challenge=%q state=%q", verifier, challenge, state)
	}

	authURL := auth.BuildAuthorizationURL(verifier, state)
	if !strings.HasPrefix(authURL, endpoints.Current().OAuth2Auth+"?") {
		t.Fatalf("expected the oauth URL, got %q", authURL)
	}
	for _, expectedParam := range []string{
		"access_type=offline",
		"code_challenge=" + challenge,
		"code_challenge_method=S256",
		"prompt=consent",
		"redirect_uri=" + url.QueryEscape(endpoints.Current().OAuthRedirect),
		"state=" + state,
	} {
		if !strings.Contains(authURL, expectedParam) {
			t.Errorf("authURL missing %q:\n%s", expectedParam, authURL)
		}
	}
}

func TestOAuthPKCE_Exchange(t *testing.T) {
	verifier, _, _, _ := auth.GeneratePKCE()
	srv := newMockTokenServer()
	defer srv.Close()

	origTokenURL := auth.TokenURL
	auth.TokenURL = srv.URL
	defer func() { auth.TokenURL = origTokenURL }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", home)

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

	_, err = auth.ExchangeAuthorizationCode(t.Context(), "bad-code", verifier)
	if err == nil {
		t.Fatal("expected error for bad-code, got nil")
	}
}

func newMockTokenServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.ParseForm() != nil || r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "valid-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"ya29.test-access-token","refresh_token":"1//test-refresh-token","expires_in":3600,"token_type":"Bearer","id_token":"test-id-token"}`))
	}))
}
