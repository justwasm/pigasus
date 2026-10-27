package browsercdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeChrome is a DevTools endpoint that answers like a Chrome does,
// without being one.
//
// It serves the two requests the clone makes: the version document (the
// websocket url) and the new-tab request.
type fakeChrome struct {
	*httptest.Server
	newTabID string
	putCode  int
}

// newFakeChrome starts a fake Chrome and returns it with its port.
func newFakeChrome(t *testing.T) *fakeChrome {
	t.Helper()
	f := &fakeChrome{newTabID: "TARGET-1"}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/json/version":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"webSocketDebuggerUrl": "ws://127.0.0.1:1/devtools/browser/fake",
			})
		case r.URL.Path == "/json/new":
			if f.putCode != 0 {
				w.WriteHeader(f.putCode)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": f.newTabID,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// port is the port the fake Chrome listens on.
func (f *fakeChrome) port(t *testing.T) int {
	t.Helper()
	u, err := url.Parse(f.URL)
	if err != nil {
		t.Fatalf("parse %s: %v", f.URL, err)
	}
	n, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse the port of %s: %v", f.URL, err)
	}
	return n
}

// TestResolveRunningChrome_FindsTheUsersChrome pins that the clone finds
// the Chrome that is already running: the DevToolsActivePort file of the
// standard profile comes first, and the configured port is the fallback.
func TestResolveRunningChrome_FindsTheUsersChrome(t *testing.T) {
	f := newFakeChrome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")

	// The file Chrome writes when remote debugging is on.
	dir := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	port := strconv.Itoa(f.port(t))
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte(port+"\n/devtools/browser/x\n"), 0o644); err != nil {
		t.Fatalf("write the DevToolsActivePort file: %v", err)
	}

	gotPort, wsURL, err := resolveRunningChrome(context.Background(), Config{Port: DefaultCDPPort})
	if err != nil {
		t.Fatalf("resolveRunningChrome failed: %v", err)
	}
	if gotPort != port {
		t.Errorf("port = %q, want %q (the port of the running Chrome)", gotPort, port)
	}
	if !strings.Contains(wsURL, "/devtools/browser/") {
		t.Errorf("wsURL = %q, want the browser's websocket url", wsURL)
	}
}

// TestResolveRunningChrome_NothingRunning pins that the error names the
// port it looked at.
func TestResolveRunningChrome_NothingRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")
	_, _, err := resolveRunningChrome(context.Background(), Config{Port: 9222})
	if err == nil {
		t.Fatal("resolveRunningChrome succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "9222") {
		t.Errorf("error = %q, want it to name the port", err)
	}
}

// TestCreateTab pins that the clone opens a tab of its own in a Chrome
// that is already running, and that it names the tab's id.
func TestCreateTab(t *testing.T) {
	f := newFakeChrome(t)
	id, err := createTab(context.Background(), strconv.Itoa(f.port(t)))
	if err != nil {
		t.Fatalf("createTab failed: %v", err)
	}
	if string(id) != f.newTabID {
		t.Errorf("id = %q, want %q", id, f.newTabID)
	}
}

// TestCreateTab_Refused pins that a browser that refuses the request is
// an error the caller can fall back from.
func TestCreateTab_Refused(t *testing.T) {
	f := newFakeChrome(t)
	f.putCode = http.StatusMethodNotAllowed
	if _, err := createTab(context.Background(), strconv.Itoa(f.port(t))); err == nil {
		t.Fatal("createTab succeeded, want an error")
	}
}

// TestLaunchArgs pins the arguments the clone starts Chrome with: the
// reference's own ones, and a window unless headless was asked for.
func TestLaunchArgs(t *testing.T) {
	visible := launchArgs(Config{Headless: false}, "/tmp/profile")
	joined := strings.Join(visible, " ")
	for _, want := range []string{"--no-first-run", "--user-data-dir=/tmp/profile", "--remote-debugging-port=0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the arguments %v carry no %q", visible, want)
		}
	}
	if strings.Contains(joined, "--headless=new") {
		t.Errorf("the arguments %v start a headless browser, want a visible one", visible)
	}

	headless := launchArgs(Config{Headless: true}, "/tmp/profile")
	if !strings.Contains(strings.Join(headless, " "), "--headless=new") {
		t.Errorf("the arguments %v carry no --headless=new", headless)
	}
}

// TestSeedUserSession_CopiesTheSession pins that the user's own session
// — the cookies, the passwords, the key they are encrypted with — lands in
// the profile the clone's Chrome uses.
func TestSeedUserSession_CopiesTheSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")

	src := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	writeFile(t, filepath.Join(src, "Local State"), `{"profile":{"info_cache":{"Default":{},"Profile 1":{}}}}`)
	writeFile(t, filepath.Join(src, "Default", "Network", "Cookies"), "cookies")
	writeFile(t, filepath.Join(src, "Default", "Login Data"), "passwords")
	writeFile(t, filepath.Join(src, "Default", "Preferences"), "{}")
	writeFile(t, filepath.Join(src, "Profile 1", "Cookies"), "cookies of profile 1")

	dst := filepath.Join(t.TempDir(), "browser-profile")
	if got := seedUserSession(dst); got != src {
		t.Errorf("seedUserSession seeded %q, want %q", got, src)
	}

	for _, rel := range []string{
		"Local State",
		"Default/Network/Cookies",
		"Default/Login Data",
		"Profile 1/Cookies",
	} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s was not seeded: %v", rel, err)
		}
	}
	// The preferences are the chrome around the session, not the session.
	if _, err := os.Stat(filepath.Join(dst, "Default", "Preferences")); !os.IsNotExist(err) {
		t.Errorf("the preferences were seeded: %v", err)
	}
}

// TestSeedUserSession_NoProfile pins that a machine without a readable
// Chrome profile is not an error: the browser starts with an empty
// session.
func TestSeedUserSession_NoProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")

	dst := filepath.Join(t.TempDir(), "browser-profile")
	if got := seedUserSession(dst); got != "" {
		t.Errorf("seedUserSession seeded %q, want nothing", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "Local State")); !os.IsNotExist(err) {
		t.Errorf("a Local State file appeared out of nowhere: %v", err)
	}
}

// TestProfileNames pins that the profile names come from the Local State
// file, and that a missing file falls back to the default profile.
func TestProfileNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Local State"), `{"profile":{"info_cache":{"Profile 2":{},"Default":{}}}}`)
	names := profileNames(dir)
	if len(names) != 2 || names[0] != "Default" || names[1] != "Profile 2" {
		t.Errorf("profileNames = %v, want [Default Profile 2]", names)
	}

	if got := profileNames(t.TempDir()); len(got) != 1 || got[0] != "Default" {
		t.Errorf("profileNames = %v, want [Default]", got)
	}
}

// writeFile writes a file, creating the dirs above it.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestSessionIsTheUsersOwn documents the policy the clone follows: the
// user's Chrome comes first, and the session is copied into the profile
// the clone starts when it has to start one.
func TestSessionIsTheUsersOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")

	// Nothing is running: the clone starts its own browser, and the
	// session it starts with is the user's, wherever it can be read.
	cfg := CurrentConfig()
	if cfg.UseLocalChrome {
		t.Errorf("UseLocalChrome = true, want false")
	}
	if cfg.Headless {
		t.Errorf("Headless = true, want false (the browser shows a window)")
	}
	if got := fmt.Sprintf("%d", cfg.Port); got != strconv.Itoa(DefaultCDPPort) {
		t.Errorf("Port = %s, want %d", got, DefaultCDPPort)
	}
}
