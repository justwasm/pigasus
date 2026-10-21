// --mode handling.
//
// The v1.3.6 binary accepts exactly three values for the global `--mode`
// flag and warns on anything else. Both warning strings were recovered
// byte-for-byte from the reference (agy 1.3.6, SHA-256 827ba22b…):
//
//	$ agy --mode bogus models
//	warning: unrecognized --mode value "bogus" (valid: accept-edits, auto, plan)
//	$ agy --mode auto --print hi
//	warning: --mode auto is not available for this account; continuing in the default mode.
//
// Both are format strings in the binary — `warning: unrecognized --mode
// value %q (valid: %s)`, `warning: --mode %s is not available for this
// account; continuing in the default mode.` and `Set the agent execution
// mode for this session (%s)` — so the value list lives once, in
// internal/permissions, and is substituted into all three.
//
// Emission surface (verified by probing the reference over a scratch HOME):
//
//   - The *unrecognized* warning fires on the agent-session entry points
//     only: `models`, `agents`/`agent` (with no arguments), `--print`, and
//     the interactive paths (no args, -i, --prompt-interactive, --continue).
//     It does NOT fire for `changelog`, `plugin`, `mcp`, `update`,
//     `install`, `remote-control` or `--help`.
//   - It is emitted *after* argument validation, so
//     `agy --mode bogus models extra` prints only the errors, no warning.
//   - The *auto-unavailable* warning fires only on `--print` — the
//     interactive paths die before session creation when there is no TTY,
//     so the reference never reaches that check there. Its wording ("not
//     available for this account") reflects an entitlement lookup; with no
//     entitlement configured the reference always reports it unavailable,
//     and so does this clone.
package cli

import (
	"fmt"
	"io"

	"github.com/justwasm/pigasus/internal/permissions"
)

// Accepted --mode values, re-exported from internal/permissions so callers
// can reach them through the CLI package too.
const (
	// ModeAcceptEdits auto-approves file edits.
	ModeAcceptEdits = permissions.ModeAcceptEdits
	// ModeAuto auto-approves edits and safe commands.
	ModeAuto = permissions.ModeAuto
	// ModePlan keeps the agent read-only until a plan is approved.
	ModePlan = permissions.ModePlan
)

// ModeValues lists the accepted --mode values, in the order the reference
// prints them. It is internal/permissions' list.
var ModeValues = permissions.Modes

// ModeList is ModeValues rendered the way the reference prints it.
func ModeList() string { return permissions.ModeList() }

// ModeWarning returns the reference's stderr line for an unrecognized
// --mode value, or "" when the value is accepted (including the empty
// string, i.e. the flag not being passed).
func ModeWarning(mode string) string {
	if mode == "" || permissions.ValidMode(mode) {
		return ""
	}
	return fmt.Sprintf("warning: unrecognized --mode value %q (valid: %s)", mode, ModeList())
}

// AutoUnavailableWarning returns the reference's stderr line for a mode
// that is not enabled for the account. The binary's format string is
// `warning: --mode %s is not available for this account; continuing in
// the default mode.`; with no entitlement source configured the reference
// always reports `auto` this way over a fresh HOME, and so does this
// clone.
func AutoUnavailableWarning(mode string) string {
	return fmt.Sprintf("warning: --mode %s is not available for this account; continuing in the default mode.", mode)
}

// WarnMode writes the unrecognized---mode warning to w when mode is not
// an accepted value. It is the shared hook for every agent-session entry
// point.
func WarnMode(w io.Writer, mode string) {
	if msg := ModeWarning(mode); msg != "" {
		fmt.Fprintln(w, msg)
	}
}

// FlagMode reads the global --mode flag from the parsed flag set, or ""
// when it is unset or absent. It tolerates a nil flag set so handlers can
// call it unconditionally.
func (j *JetskiCLI) FlagMode() string {
	if j == nil {
		return ""
	}
	fs := j.FlagSet()
	if fs == nil {
		return ""
	}
	f := fs.Lookup("mode")
	if f == nil {
		return ""
	}
	return f.Value.String()
}
