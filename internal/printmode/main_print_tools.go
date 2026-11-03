package printmode

import (
	"flag"

	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
)

// Cycle 346: this file used to carry a second copy of the tool-running
// machinery — `toolDeclsFromRegistry`, `advertisedToolDecls`,
// `registryRunner`, `permissionRunner`, `sandboxDeniedToolResult` and
// `toolTarget` — the same code as internal/features/tools/toolrunner, function for
// function. The shared copy is the canonical one: the TUI
// (internal/cliapp) and the harness (cmd/harness) already read it, so
// print mode reads it too and the two copies cannot drift apart.
//
// The permission policy and the sandbox are the session's, not print
// mode's: they live in internal/app/session/setup, below every host,
// and this file reads the flags and hands the values over.

// toolTarget is the target string the permission policy matches on.
func toolTarget(call backend.ToolCall) string { return toolrunner.ToolTarget(call) }

// printModePolicy builds the permission policy for a print-mode session.
func printModePolicy(fs *flag.FlagSet) permissions.Policy {
	return setup.PolicyFrom(flagStringValue(fs, "mode"), flagBoolValue(fs, "dangerously-skip-permissions"))
}

// printModeSandbox builds the terminal sandbox for a print-mode session.
func printModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	return setup.SandboxFrom(flagBoolValue(fs, "dangerously-skip-permissions"), workspace, conversationID)
}

// PrintModePolicy builds the permission policy for a session.
func PrintModePolicy(fs *flag.FlagSet) permissions.Policy {
	return printModePolicy(fs)
}

// PrintModeSandbox builds the terminal sandbox for a session.
func PrintModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	return printModeSandbox(fs, workspace, conversationID)
}
