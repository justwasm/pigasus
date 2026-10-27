// Package rchost — rctoken_test.go: the secret the remote-control
// surface demands, and where it lives.
//
// Cycle rc-auth-bearer-required.
package rchost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveRCToken_EnvWins pins that a pinned secret is used as it
// is: the operator chose it, and the file is left alone.
func TestResolveRCToken_EnvWins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("ANTIGRAVITY_RC_TOKEN", "pinned-secret-0123456789abcd")

	token, warn, err := resolveRCToken()
	if err != nil {
		t.Fatalf("resolveRCToken: %v", err)
	}
	if warn != "" {
		t.Errorf("warning = %q, want none", warn)
	}
	if token != "pinned-secret-0123456789abcd" {
		t.Errorf("token = %q, want the pinned one", token)
	}
	if _, err := os.Stat(filepath.Join(home, rcSecretFileName)); err == nil {
		t.Errorf("the pinned secret was written to the file")
	}
}

// TestResolveRCToken_BlankEnvIsUnset pins that a blank variable means
// "no pinned secret", not "the empty secret".
func TestResolveRCToken_BlankEnvIsUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("ANTIGRAVITY_RC_TOKEN", "   ")

	token, _, err := resolveRCToken()
	if err != nil {
		t.Fatalf("resolveRCToken: %v", err)
	}
	if token == "" {
		t.Errorf("the token is empty")
	}
}

// TestResolveRCToken_TheFileSurvivesARestart pins that the secret is
// stable: the persistent daemon's operator hands the same URL out
// again after a restart.
func TestResolveRCToken_TheFileSurvivesARestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("ANTIGRAVITY_RC_TOKEN", "")

	first, _, err := resolveRCToken()
	if err != nil {
		t.Fatalf("resolveRCToken: %v", err)
	}
	second, _, err := resolveRCToken()
	if err != nil {
		t.Fatalf("resolveRCToken: %v", err)
	}
	if first != second {
		t.Errorf("the secret changed between two calls: %q then %q", first, second)
	}
}

// TestWriteRCSecret_Permissions pins that the secret file is readable
// by its owner only.
func TestWriteRCSecret_Permissions(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "nested", rcSecretFileName)
	if err := writeRCSecret(path, "secret-value-0123456789abcd"); err != nil {
		t.Fatalf("writeRCSecret: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
	got, err := readRCSecret(path)
	if err != nil {
		t.Fatalf("readRCSecret: %v", err)
	}
	if got != "secret-value-0123456789abcd" {
		t.Errorf("readRCSecret = %q, want the written value", got)
	}
}

// TestAnnounceRCToken pins that the operator is told the secret and
// the URL that carries it — a client from another machine has to
// present it, and the server has no other channel to hand it over.
func TestAnnounceRCToken(t *testing.T) {
	var buf strings.Builder
	announceRCToken(&buf, "pigasus web", "192.168.1.5:8787", "secret-value-0123456789abcd")
	out := buf.String()
	if !strings.Contains(out, "secret-value-0123456789abcd") {
		t.Errorf("the announcement does not carry the secret: %q", out)
	}
	if !strings.Contains(out, "http://192.168.1.5:8787/?token=secret-value-0123456789abcd") {
		t.Errorf("the announcement does not carry the URL: %q", out)
	}
}

// TestAnnounceRCSecretPath pins that the persistent daemon points at
// the file instead of printing the secret into its log.
func TestAnnounceRCSecretPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	var buf strings.Builder
	announceRCSecretPath(&buf, "remote-control-foreground")
	out := buf.String()
	if !strings.Contains(out, rcSecretPath()) {
		t.Errorf("the announcement does not carry the path: %q", out)
	}
}
