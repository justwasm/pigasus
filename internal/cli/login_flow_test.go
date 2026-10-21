// Glue test for runLogin: ensures the dispatch entrypoint actually
// drives the device-flow library and persists the resulting token
// through SaveTokenUnified without trying to talk to Google's real
// OAuth servers.
//
// The flow is exercised against an httptest server that imitates
// oauth2.googleapis.com's /device/code + /token pair, with HOME
// redirected to a tempdir so SaveTokenUnified writes somewhere
// disposable.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"

	"github.com/justwasm/pigasus/internal/auth"
)

// fakeDeviceServer imitates oauth2.googleapis.com's /device/code +
// /token endpoints and is used by TestRunLogin_EndToEnd below.
type fakeDeviceServer struct {
	srv          *httptest.Server
	polls        atomic.Int32
	approveAt    int32
	captureScope atomic.Value // string
}

func newFakeDeviceServer(t *testing.T) *fakeDeviceServer {
	t.Helper()
	ds := &fakeDeviceServer{approveAt: 1}
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		ds.captureScope.Store(r.Form.Get("scope"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"device_code":               "DEV-FAKE",
			"user_code":                 "ZZZZ-9999",
			"verification_uri":          "https://example.test/device",
			"verification_uri_complete": "https://example.test/device?user_code=ZZZZ-9999",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		ds.polls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if ds.polls.Load() >= ds.approveAt {
			scope, _ := ds.captureScope.Load().(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "ya29.fake-access-token",
				"refresh_token": "1//fake-refresh-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"scope":         scope,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	})
	ds.srv = httptest.NewServer(mux)
	t.Cleanup(ds.srv.Close)
	return ds
}

// withRedirectedDeviceFlow swaps auth.DeviceFlowEndpoint so any
// device-flow call inside the test goes to the fake server instead
// of accounts.google.com.
func withRedirectedDeviceFlow(t *testing.T, ds *fakeDeviceServer) {
	t.Helper()
	saved := auth.DeviceFlowEndpoint
	auth.DeviceFlowEndpoint = oauth2.Endpoint{
		DeviceAuthURL: ds.srv.URL + "/device/code",
		TokenURL:      ds.srv.URL + "/token",
	}
	t.Cleanup(func() { auth.DeviceFlowEndpoint = saved })
}

// withTempHome points $HOME at a fresh tempdir so SaveTokenUnified's
// AllTokenPaths() resolves into the test's own scratch space.
func withTempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, hadOld := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", dir); err != nil {
		t.Fatalf("setenv HOME: %v", err)
	}
	t.Cleanup(func() {
		if hadOld {
			_ = os.Setenv("HOME", old)
		} else {
			_ = os.Unsetenv("HOME")
		}
	})
	return dir
}

// TestRunLogin_EndToEnd drives runLogin against the fake device server
// and asserts:
//   - the verification URL and user code are printed,
//   - SaveTokenUnified writes a token whose AccessToken + RefreshToken
//     match the server's response,
//   - the success banner carries the same suffix bytes the user would
//     see in a real run.
func TestRunLogin_EndToEnd(t *testing.T) {
	// Suppress the side-effect OpenBrowser call so `go test` does not
	// literally launch `open https://example.test/device` in the user's
	// default browser on macOS. The BrowserAutoOpen gate honours
	// $OPENAGY_NO_BROWSER; cli/runLogin consults it before
	// dispatching to the platform launcher.
	t.Setenv("OPENAGY_NO_BROWSER", "1")
	ds := newFakeDeviceServer(t)
	withRedirectedDeviceFlow(t, ds)
	home := withTempHome(t)

	out := &bytes.Buffer{}
	d := newDispatchContext(out, &bytes.Buffer{}, context.Background(), nil, nil)
	if err := runLogin(d); err != nil {
		t.Fatalf("runLogin: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"login",
		"starting OAuth device-code flow",
		"https://example.test/device",
		"ZZZZ-9999",
		"login succeeded",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("runLogin output missing %q\nfull output:\n%s", want, got)
		}
	}

	// The token must have been written to disk at the legacy path.
	written := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	data, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("read %s: %v", written, err)
	}
	if !strings.Contains(string(data), "ya29.fake-access-token") {
		t.Errorf("written token file missing access token:\n%s", string(data))
	}
	if !strings.Contains(string(data), "1//fake-refresh-token") {
		t.Errorf("written token file missing refresh token:\n%s", string(data))
	}
}

// TestRunLogin_DeviceFlowError confirms runLogin surfaces a typed
// error from the device-flow library instead of swallowing it.
func TestRunLogin_DeviceFlowError(t *testing.T) {
	// See TestRunLogin_EndToEnd: runLogin consults
	// BrowserAutoOpen → $OPENAGY_NO_BROWSER before opening the
	// verification URL. The test's fake server returns
	// "https://example.test/device" as the verification URI;
	// without this gate the test would literally launch
	// `open https://example.test/device` on macOS during `go test`.
	t.Setenv("OPENAGY_NO_BROWSER", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"device_code":      "DEV-FAKE",
			"user_code":        "ZZZZ-9999",
			"verification_uri": "https://example.test/device",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	saved := auth.DeviceFlowEndpoint
	auth.DeviceFlowEndpoint = oauth2.Endpoint{
		DeviceAuthURL: srv.URL + "/device/code",
		TokenURL:      srv.URL + "/token",
	}
	t.Cleanup(func() { auth.DeviceFlowEndpoint = saved })
	withTempHome(t)

	out := &bytes.Buffer{}
	d := newDispatchContext(out, &bytes.Buffer{}, context.Background(), nil, nil)
	if err := runLogin(d); err == nil {
		t.Fatal("expected runLogin to error on access_denied")
	} else if !strings.Contains(err.Error(), "denied") {
		t.Errorf("error = %q, want substring 'denied'", err.Error())
	}
}

// TestRunLogin_RegisteredInDispatchTable ensures the dispatch table
// still has a "login" entry after the cycle 170 rewrite — protects
// against accidental removal during future refactors.
func TestRunLogin_RegisteredInDispatchTable(t *testing.T) {
	if h := runXxxByName("login"); h == nil {
		t.Fatal("runXxxByName(\"login\") = nil; the dispatch entry was removed")
	}
}

// TestRenderLoginPrompt confirms the cycle 171 wiring surfaces the
// URL and code in a markdown-rendered block. Asserts:
//   - the heading is emitted as "# Sign in to Pigasus CLI"
//   - the verification URL appears on its own line
//   - the user code is rendered inside an indented block (the only way
//     the walker distinguishes it from the surrounding prose, since
//     mdrender.Render strips the ``` fences in favour of "    " indent)
//   - the "Waiting for you to approve" trailer is preserved so the
//     user knows the flow is still running.
func TestRenderLoginPrompt(t *testing.T) {
	got, err := RenderLoginPrompt("https://example.test/device?u=ABCD", "ABCD-1234")
	if err != nil {
		t.Fatalf("RenderLoginPrompt: %v", err)
	}
	for _, want := range []string{
		"# Sign in to Pigasus CLI",
		"https://example.test/device?u=ABCD",
		"    ABCD-1234", // fenced block → 4-space indent
		"Waiting for you to approve",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing %q\nfull output:\n%s", want, got)
		}
	}
}

// TestRunLogin_RendersMarkdownPrompt asserts the end-to-end glue
// actually exercises RenderLoginPrompt: the user-visible banner should
// carry the rendered "# Sign in to Pigasus CLI" heading rather
// than the legacy "Open this URL in any browser:" line.
func TestRunLogin_RendersMarkdownPrompt(t *testing.T) {
	// See TestRunLogin_EndToEnd: $OPENAGY_NO_BROWSER suppresses
	// the platform-launcher side-effect so the test does not
	// literally open `https://example.test/device` in the user's
	// default browser.
	t.Setenv("OPENAGY_NO_BROWSER", "1")
	ds := newFakeDeviceServer(t)
	withRedirectedDeviceFlow(t, ds)
	withTempHome(t)

	out := &bytes.Buffer{}
	d := newDispatchContext(out, &bytes.Buffer{}, context.Background(), nil, nil)
	if err := runLogin(d); err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "# Sign in to Pigasus CLI") {
		t.Errorf("output missing rendered heading\nfull output:\n%s", got)
	}
	// The verification URL the fake server returned must still be in
	// the output even after the renderer mangled its surroundings.
	if !strings.Contains(got, "https://example.test/device") {
		t.Errorf("output missing verification URL\nfull output:\n%s", got)
	}
	// User code from the fake server: ZZZZ-9999.
	if !strings.Contains(got, "ZZZZ-9999") {
		t.Errorf("output missing user code ZZZZ-9999\nfull output:\n%s", got)
	}
}
