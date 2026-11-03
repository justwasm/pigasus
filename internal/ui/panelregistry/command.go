// command.go — the slash commands that open a panel.
//
// The command names are the reference's contract: the table below is the
// set of commands the reference answers with a panel, and the key of
// each row is the command the user types.

package panelregistry

// commandPanels is the command → panel table.
//
// The names are the reference's own: "/codesearch" and not
// "/code-search", "/add-dir" and not "/adddir", "/feature-diff" and not
// "/feat-diff".
var commandPanels = map[string]Kind{
	"mcp":          MCP,
	"tasks":        Tasks,
	"effort":       Effort,
	"hooks":        Hooks,
	"permissions":  Permissions,
	"agents":       Agents,
	"credits":      Credits,
	"codesearch":   CodeSearch,
	"artifact":     Artifact,
	"feedback":     Feedback,
	"rewind":       Rewind,
	"context":      Context,
	"model":        Model,
	"config":       Config,
	"plugin":       Plugin,
	"skills":       Skills,
	"usage":        Usage,
	"changelog":    Changelog,
	"diff":         Diff,
	"add-dir":      AddDir,
	"rename":       Rename,
	"feature-diff": FeatDiff,
	// Superset configuration and multi-account panels, listed in
	// internal/ui/slashcommands' catalogue.
	"provider": Provider,
	"accounts": Accounts,
	// /pool is the /accounts panel's Pool tab on its own: the policy
	// that decides which account serves a turn.
	"pool": Accounts,
}

// CommandPanel returns the panel a slash command opens.
//
// The second value is false for every command that opens no panel.
func CommandPanel(cmd string) (Kind, bool) {
	kind, ok := commandPanels[cmd]
	return kind, ok
}
