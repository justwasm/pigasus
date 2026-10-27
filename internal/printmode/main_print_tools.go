package printmode

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/toolrunner"
)

// Cycle 346: this file used to carry a second copy of the tool-running
// machinery — `toolDeclsFromRegistry`, `advertisedToolDecls`,
// `registryRunner`, `permissionRunner`, `sandboxDeniedToolResult` and
// `toolTarget` — the same code as internal/toolrunner, function for
// function. The shared copy is the canonical one: the TUI
// (internal/cliapp) and the harness (cmd/harness) already read it, so
// print mode reads it too and the two copies cannot drift apart.
//
// The permission policy and the sandbox below are print mode's own.

// toolTarget is the target string the permission policy matches on.
func toolTarget(call backend.ToolCall) string { return toolrunner.ToolTarget(call) }

// printModePolicy builds the permission policy for a print-mode session.
func printModePolicy(fs *flag.FlagSet) permissions.Policy {
	mode := flagStringValue(fs, "mode")
	if mode == "" {
		if v := auth.SettingsString("agentMode"); isKnownMode(v) {
			mode = v
		}
	}
	p := permissions.Policy{
		Mode:            mode,
		SkipPermissions: flagBoolValue(fs, "dangerously-skip-permissions"),
		Strict:          permissions.StrictFromEnv(os.Getenv("OPENAGY_PERMISSION_POLICY")),
	}
	if settings, err := permissions.LoadRules(filepath.Join(auth.AntigravityHome(), "settings.json")); err == nil {
		p.Allow = settings.Allow
		p.Deny = settings.Deny
	}
	if p.SkipPermissions {
		printModeSilenceLogger.Println(permissions.SkipPermissionsNotice)
	}
	return p
}

func isKnownMode(v string) bool {
	switch v {
	case permissions.ModeAuto, permissions.ModePlan, permissions.ModeAcceptEdits:
		return true
	}
	return false
}

// printModeSandbox builds the terminal sandbox for a print-mode session.
func printModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	if flagBoolValue(fs, "dangerously-skip-permissions") || os.Getenv("OPENAGY_DISABLE_SANDBOX") == "1" {
		return sandbox.Sandbox{Enabled: false, Cwd: workspace}
	}
	enabled := false
	if v, ok := auth.SettingsBool("enableTerminalSandbox"); ok {
		enabled = v
	}
	return sandbox.Resolve(enabled, workspace, auth.AntigravityHome(), conversationID)
}

// PrintModePolicy builds the permission policy for a session.
func PrintModePolicy(fs *flag.FlagSet) permissions.Policy {
	return printModePolicy(fs)
}

// PrintModeSandbox builds the terminal sandbox for a session.
func PrintModeSandbox(fs *flag.FlagSet, workspace, conversationID string) sandbox.Sandbox {
	return printModeSandbox(fs, workspace, conversationID)
}
