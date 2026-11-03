// Package setup — the flag-free cores a session's host needs.
//
// Three hosts run a session in this clone: the interactive TUI
// (`internal/cliapp`), the headless print path (`internal/printmode`)
// and the agent server (`internal/app/agentserver`). Each of them has
// to answer the same four questions before a turn can run:
//
//   - which permission policy does the session run under?
//   - which terminal sandbox does the session run in?
//   - which subagents does the prompt carry?
//   - which MCP servers do the tools talk to?
//
// The answers are the same wherever the session runs, so the cores that
// compute them live here, **below** the hosts. That placement is what
// lets the server reuse them: the server cannot import `internal/cliapp`
// (that would be an import cycle), and it must not import
// `internal/printmode` either — an entry surface is *above* a tier
// package, not beside it.
//
// The flag reading is the caller's business: `PolicyFrom` takes the
// mode's value, `SandboxFrom` takes the permission switch, and each
// host reads its own flags.
package setup

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// silenceLogger is the logger the policy's notice goes to.
//
// It is the print mode's silent logger, carried over with the core
// unchanged: `log.New(io.Discard, …)`, never reassigned, so the notice
// does not surface anywhere. It stays that way on purpose — the print
// path's stdout *is* its protocol, and a line on stderr would change
// bytes the reference does not print.
var silenceLogger = log.New(io.Discard, "", 0)

// PolicyFrom builds the permission policy from explicit values.
//
// The policy is the same wherever the session runs — the interactive
// one, the headless one and the server's — so the values that make it
// are named here and the flag reading is the caller's business.
//
// An empty mode falls back to the account's own `agentMode` setting;
// an unknown one is kept as it is, because the policy's own validation
// is what rejects it.
//
// The settings.json contributes three things: the three rule lists (allow,
// ask and deny), the `toolPermission` policy and the terminal sandbox's own
// switch. The sandbox's *enabled* flag is read through the same helper
// SandboxFrom uses, so the flag has one owner.
func PolicyFrom(mode string, skipPermissions bool) permissions.Policy {
	if strings.TrimSpace(mode) == "" {
		if v := settings.String("agentMode"); isKnownMode(v) {
			mode = v
		}
	}
	p := permissions.Policy{
		Mode:            mode,
		SkipPermissions: skipPermissions,
		Strict:          permissions.StrictFromEnv(os.Getenv("OPENAGY_PERMISSION_POLICY")),
		ToolPermission:  settings.String("toolPermission"),
		SandboxEnabled:  sandboxEnabled(skipPermissions),
	}
	if rules, err := permissions.LoadRules(filepath.Join(auth.AntigravityHome(), "settings.json")); err == nil {
		p.Allow = rules.Allow
		p.Ask = rules.Ask
		p.Deny = rules.Deny
	}
	if p.SkipPermissions {
		silenceLogger.Println(permissions.SkipPermissionsNotice)
	}
	return p
}

// isKnownMode reports whether v is one of the policy's own modes.
func isKnownMode(v string) bool {
	switch v {
	case permissions.ModeAuto, permissions.ModePlan, permissions.ModeAcceptEdits:
		return true
	}
	return false
}

// SandboxFrom builds the terminal sandbox from explicit values.
//
// The sandbox is the session's, not the mode's: the interactive
// session, the headless one and the server's all resolve it the same
// way, so the values that make it are named here.
func SandboxFrom(skipPermissions bool, workspace, conversationID string) sandbox.Sandbox {
	if !sandboxEnabled(skipPermissions) {
		return sandbox.Sandbox{Enabled: false, Cwd: workspace}
	}
	return sandbox.Resolve(true, workspace, auth.AntigravityHome(), conversationID)
}

// sandboxEnabled reports whether the terminal sandbox is on for this
// session.
//
// Both the sandbox itself and the permission policy read it here: the
// policy needs it for the `proceed-in-sandbox` toolPermission, which
// auto-approves a command only while the sandbox runs it. One owner for
// the flag.
func sandboxEnabled(skipPermissions bool) bool {
	if skipPermissions || os.Getenv("OPENAGY_DISABLE_SANDBOX") == "1" {
		return false
	}
	v, ok := settings.Bool("enableTerminalSandbox")
	return ok && v
}
