// temp_home_test.go: the guard that keeps a test from replacing the
// user's launchd daemon.
//
// The launchd label is global to the login session, so a registration
// replaces the daemon that is already registered under it. A
// registration from a temporary home would therefore stop the user's
// daemon and replace it with one whose log lives in a directory that
// is deleted with the test.

package persistent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefusesTemporaryHome pins the rule itself: a path inside the
// operating system's temporary directory is refused, a path outside it
// is not.
func TestRefusesTemporaryHome(t *testing.T) {
	inside := filepath.Join(os.TempDir(), "pigasus-test", "Library", "LaunchAgents", "x.plist")
	err := refusesTemporaryHome(inside)
	if err == nil {
		t.Fatalf("a path inside the temporary directory was accepted: %s", inside)
	}
	var te *TemporaryHomeError
	if !errors.As(err, &te) {
		t.Fatalf("error = %v (%T), want a *TemporaryHomeError", err, err)
	}
	if !strings.Contains(err.Error(), "temporary") {
		t.Errorf("the error does not say why: %v", err)
	}

	outside := filepath.Join("/Users", "someone", "Library", "LaunchAgents", "x.plist")
	if err := refusesTemporaryHome(outside); err != nil {
		t.Errorf("a path outside the temporary directory was refused: %v", err)
	}

	// A path whose name merely starts with the temporary directory's
	// name, in the same parent, is outside it.
	tmp := os.TempDir()
	sibling := filepath.Join(filepath.Dir(filepath.Dir(tmp)), filepath.Base(tmp)+"-sibling")
	if err := refusesTemporaryHome(sibling); err != nil {
		t.Errorf("a sibling of the temporary directory was refused: %v", err)
	}
}

// TestStartOfficialDaemon_RefusesATemporaryHome is the end-to-end
// guard: with $HOME pointing at a temporary tree, the registration is
// refused — and nothing is written and no launchctl call is made.
func TestStartOfficialDaemon_RefusesATemporaryHome(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("the launchd path is macOS-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	err := StartOfficialDaemon(context.Background(), "pigasus-test", false, false)
	if err == nil {
		t.Fatal("the registration from a temporary home was accepted")
	}
	var te *TemporaryHomeError
	if !errors.As(err, &te) {
		t.Fatalf("error = %v (%T), want a *TemporaryHomeError", err, err)
	}

	// Nothing was written.
	plist := filepath.Join(home, "Library", "LaunchAgents")
	if _, statErr := os.Stat(plist); statErr == nil {
		t.Errorf("the plist directory was created: %s", plist)
	}
}
