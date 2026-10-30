// Package workspacetrust implements the on-startup workspace trust prompt.
//
// Deprecated: this package is a backward-compatible facade for
// internal/core/policy/trust. New callers should import internal/core/policy/trust directly.
package workspacetrust

import (
	"flag"
	"io"

	"github.com/justwasm/pigasus/internal/core/policy/trust"
)

// Result is the decision returned by RunTrustPrompt.
type Result = trust.Result

const (
	Accept = trust.Accept
	Deny   = trust.Deny
)

// RenderTrustPrompt writes the trust dialog to w.
func RenderTrustPrompt(cwd string, w io.Writer) error {
	return trust.RenderTrustPrompt(cwd, w)
}

// RunTrustPrompt renders the dialog to out and reads a single byte from in.
func RunTrustPrompt(cwd string, in io.Reader, out io.Writer) (Result, error) {
	return trust.RunTrustPrompt(cwd, in, out)
}

// IsWorkspaceTrusted is the lookup hook for trusted workspaces.
var IsWorkspaceTrusted = trust.IsWorkspaceTrusted

// AddTrustedWorkspace is the persistence hook for trusted workspaces.
var AddTrustedWorkspace = trust.AddTrustedWorkspace

// Gate decides whether the on-startup trust dialog should be shown.
func Gate(fs *flag.FlagSet) (string, bool, error) {
	return trust.Gate(fs)
}
