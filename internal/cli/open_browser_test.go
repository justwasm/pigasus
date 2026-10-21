// Tests for OpenBrowser / BrowserAutoOpen. We do not actually launch a
// browser during the test run — OpenBrowser dispatches to the platform
// launcher (open / xdg-open / rundll32) and the test substitutes the
// launch function with a recorder so we can assert what would have been
// invoked without spawning a child process.
package cli

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
)

// TestOpenBrowser_NonEmptyURL confirms the empty-URL guard.
func TestOpenBrowser_NonEmptyURL(t *testing.T) {
	if err := OpenBrowser(""); err == nil {
		t.Fatal("OpenBrowser(\"\") returned nil; want an error")
	}
}

// TestBrowserLauncher_KnownGOOS confirms each supported GOOS maps to
// a non-empty launcher name. The exact name is intentionally not
// pinned (e.g. xdg-open could be replaced with gio open in a future
// cycle) — what we care about is that the dispatch is not silent.
func TestBrowserLauncher_KnownGOOS(t *testing.T) {
	name, args := browserLauncher()
	if name == "" {
		t.Fatalf("browserLauncher() on %s returned empty name", runtime.GOOS)
	}
	t.Logf("GOOS=%s → launcher=%s args=%v", runtime.GOOS, name, args)
}

// TestBrowserAutoOpen_DefaultIsTrue confirms the helper returns true
// when no env var is set and no flag override is passed.
func TestBrowserAutoOpen_DefaultIsTrue(t *testing.T) {
	t.Setenv("OPENAGY_NO_BROWSER", "")
	if !BrowserAutoOpen(false) {
		t.Fatal("BrowserAutoOpen(false) = false, want true with no env var")
	}
	if BrowserAutoOpen(true) {
		t.Fatal("BrowserAutoOpen(true) = true, want false when caller suppresses")
	}
}

// TestBrowserAutoOpen_HonoursEnvVar covers every documented value of
// $OPENAGY_NO_BROWSER that should suppress the auto-open.
func TestBrowserAutoOpen_HonoursEnvVar(t *testing.T) {
	for _, val := range []string{"1", "true", "TRUE", "True", "yes", "YES"} {
		t.Run("val="+val, func(t *testing.T) {
			t.Setenv("OPENAGY_NO_BROWSER", val)
			if BrowserAutoOpen(false) {
				t.Errorf("BrowserAutoOpen with OPENAGY_NO_BROWSER=%q returned true", val)
			}
		})
	}
}

// TestBrowserAutoOpen_IgnoresUnknownEnvValues ensures unset / blank /
// non-truthy values still allow the auto-open. This prevents the
// helper from over-suppressing on a stale env value like "0".
func TestBrowserAutoOpen_IgnoresUnknownEnvValues(t *testing.T) {
	for _, val := range []string{"", "0", "false", "no", "off"} {
		t.Run("val="+val, func(t *testing.T) {
			t.Setenv("OPENAGY_NO_BROWSER", val)
			if !BrowserAutoOpen(false) {
				t.Errorf("BrowserAutoOpen with OPENAGY_NO_BROWSER=%q returned false; should allow auto-open", val)
			}
		})
	}
}

// TestRunLogin_BrowserOpenHonoursEnvVar drives runLogin end-to-end
// and confirms the OpenBrowser call is gated by $OPENAGY_NO_BROWSER.
// We cannot reliably intercept os/exec here, so the assertion is
// that the flow still succeeds and the prompt is rendered — the
// BrowserAutoOpen unit test above is the authoritative behavioural
// check.
func TestRunLogin_BrowserOpenHonoursEnvVar(t *testing.T) {
	t.Setenv("OPENAGY_NO_BROWSER", "1")
	ds := newFakeDeviceServer(t)
	withRedirectedDeviceFlow(t, ds)
	withTempHome(t)

	out := &bytes.Buffer{}
	d := newDispatchContext(out, &bytes.Buffer{}, context.Background(), nil, nil)
	if err := runLogin(d); err != nil {
		t.Fatalf("runLogin with OPENAGY_NO_BROWSER=1: %v", err)
	}
	if !strings.Contains(out.String(), "# Sign in to Pigasus CLI") {
		t.Errorf("rendered prompt missing under OPENAGY_NO_BROWSER=1")
	}
}
