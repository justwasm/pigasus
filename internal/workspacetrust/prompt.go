// Package workspacetrust implements the on-startup workspace trust prompt.
//
// Changelog cycle 125: the on-startup workspace-trust dialog.
// The reference shows this overlay the first time you launch it from
// a directory that isn't already in settings.json's `trustedWorkspaces` list.
package workspacetrust

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cliflags"
)

// Result is the decision returned by RunTrustPrompt.
type Result int

const (
	Accept Result = iota + 1
	Deny
)

// RenderTrustPrompt writes the trust dialog to w.
func RenderTrustPrompt(cwd string, w io.Writer) error {
	const (
		reset  = "\x1b[0m"
		bold   = "\x1b[1m"
		invert = "\x1b[7m"
		dim    = "\x1b[2m"
		yellow = "\x1b[33m"
	)
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	if home, hErr := os.UserHomeDir(); hErr == nil && strings.HasPrefix(abs, home) {
		abs = "~" + strings.TrimPrefix(abs, home)
	}
	fmt.Fprintln(w, bold+"\n  Trust this folder?"+reset)
	fmt.Fprintln(w, yellow+"  Do you trust the contents of this project?"+reset)
	fmt.Fprintln(w, dim+"    "+abs+reset)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+invert+" Yes, I trust this folder "+reset+"  "+dim+"Esc/n = exit"+reset)
	return nil
}

// RunTrustPrompt renders the dialog to out and reads a single byte from in.
func RunTrustPrompt(cwd string, in io.Reader, out io.Writer) (Result, error) {
	if err := RenderTrustPrompt(cwd, out); err != nil {
		return Deny, err
	}
	br := bufio.NewReader(in)
	b, err := br.ReadByte()
	if err != nil {
		return Deny, nil
	}
	switch b {
	case 'y', 'Y', '\r', '\n', ' ', '1':
		return Accept, nil
	default:
		return Deny, nil
	}
}

// IsWorkspaceTrusted is the lookup hook for trusted workspaces.
var IsWorkspaceTrusted = auth.IsWorkspaceTrusted

// AddTrustedWorkspace is the persistence hook for trusted workspaces.
var AddTrustedWorkspace = auth.AddTrustedWorkspace

// Gate decides whether the on-startup trust dialog should be shown.
// Returns (canonicalCwd, skipPrompt, error).
func Gate(fs *flag.FlagSet) (string, bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false, err
	}
	cleanCwd := canonicalisePath(cwd)

	// (1) --trust-cwd test hook
	if fs != nil && flagWasSet(fs, "trust-cwd") {
		return cleanCwd, true, nil
	}

	// (2) OPENAGY_TRUST test hook
	if env := strings.TrimSpace(os.Getenv("OPENAGY_TRUST")); env != "" {
		switch strings.ToLower(env) {
		case "1", "true", "always", "all":
			return cleanCwd, true, nil
		}
		cleanEnv := canonicalisePath(env)
		if cleanEnv == cleanCwd || strings.HasPrefix(cleanCwd, cleanEnv+string(filepath.Separator)) {
			return cleanCwd, true, nil
		}
	}

	// (3) settings.json already trusts cwd
	trusted, terr := IsWorkspaceTrusted(cleanCwd)
	if terr != nil {
		return cleanCwd, false, terr
	}
	if trusted {
		return cleanCwd, true, nil
	}
	return cleanCwd, false, nil
}

func canonicalisePath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	return cliflags.WasSet(fs, name)
}
