// Package rchost — rctoken.go: the secret the remote-control
// surface demands, and how the operator learns it.
//
// Cycle rc-auth-bearer-required.
//
// `pigasus remote-control serve` and `pigasus web` both bind 0.0.0.0
// on purpose — the persistent daemon refuses a loopback bind
// (internal/persistentrc.ValidateAddr) because the point of both
// commands is to be reached from another machine. Until this cycle
// nothing on that surface asked for a credential, so any host that
// could reach the port could drive the agent.
//
// The secret lives in three places, in this order:
//
//  1. $ANTIGRAVITY_RC_TOKEN — the operator pinned it, so it is the
//     same on every machine and every restart;
//  2. $ANTIGRAVITY_HOME/rc_secret — the file the secret is kept in,
//     written with 0600 permissions the way the OAuth token is;
//  3. a fresh secret, minted for this process.
//
// The file is what makes the persistent daemon usable: its output goes
// to a log file, not to a terminal, and a secret that only existed in
// the process would be unreachable from the network.
package rchost

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/rctoken"
)

// rcSecretFileName is the name of the file the secret is kept in,
// under $ANTIGRAVITY_HOME.
const rcSecretFileName = "rc_secret"

// resolveRCToken returns the secret the remote-control surface
// demands, plus a warning when the secret could not be persisted.
//
// A blank environment variable counts as unset: an operator who
// exported `ANTIGRAVITY_RC_TOKEN=` meant "no pinned secret", not "the
// empty secret".
//
// A failure to write the secret file is not fatal — the server still
// runs with the minted secret, it is just not discoverable after a
// restart. The caller learns about it through the returned warning.
func resolveRCToken() (string, string, error) {
	if tok := rctoken.FromEnv(); tok != "" {
		return tok, "", nil
	}
	path := rcSecretPath()
	if tok, err := readRCSecret(path); err == nil && tok != "" {
		return tok, "", nil
	}
	tok, err := rctoken.Generate()
	if err != nil {
		return "", "", err
	}
	if err := writeRCSecret(path, tok); err != nil {
		return tok, fmt.Sprintf("the secret could not be written to %s: %v", path, err), nil
	}
	return tok, "", nil
}

// rcSecretPath returns the file the secret is kept in.
func rcSecretPath() string {
	return filepath.Join(auth.AntigravityHome(), rcSecretFileName)
}

// readRCSecret reads the secret file. An unreadable file is an error
// the caller turns into "mint a new one".
func readRCSecret(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// writeRCSecret writes the secret file with 0600 permissions.
//
// The write is atomic (a temporary file plus a rename) so a crash
// mid-write cannot leave a half-written secret behind.
func writeRCSecret(path, secret string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return atomicfile.WriteFile(path, []byte(secret+"\n"), 0o600)
}

// announceRCToken prints the secret and the URL that carries it.
//
// The secret has to reach the operator: it is the one credential a
// client from another machine has to present, and the server has no
// other channel to hand it over.
func announceRCToken(out io.Writer, label, addr, token string) {
	if out == nil || token == "" {
		return
	}
	fmt.Fprintf(out, "%s: secret: %s\n", label, token)
	fmt.Fprintf(out, "%s: remote URL: http://%s/?token=%s\n", label, addr, token)
}

// announceRCSecretPath prints where the secret is kept.
//
// The persistent daemon's output goes to a log file, not to a
// terminal, so the path is what its operator needs.
func announceRCSecretPath(out io.Writer, label string) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "%s: secret file: %s\n", label, rcSecretPath())
}
