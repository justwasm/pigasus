package customize

// Priority is the canonical ordering the trimmer uses to decide
// which customization payload to drop first.
//
// Lower values are trimmed first; /customize — the most generic
// override surface — sits at zero because it can always be
// re-derived from the user's settings.json. /commands sits last
// because it is the comprehensive catalog the picker overlay
// itself reads from; dropping it would break the slash menu.
//
// The numeric spacing is uniform on purpose: future commands can
// slot in (e.g. between Hooks and MCP) without renumbering the
// existing constants.
type Priority int

const (
	// PriorityCustomize is the /customize payload. Trimmed first.
	PriorityCustomize Priority = 0
	// PriorityConfig is the /config (settings) payload.
	PriorityConfig Priority = 10
	// PriorityAgents is the /agents (custom-agent definitions)
	// payload. Trimmed after /customize so a loaded agent
	// definition survives a tighter context window than the
	// generic /customize override.
	PriorityAgents Priority = 20
	// PriorityHooks is the /hooks (event-handler table) payload.
	PriorityHooks Priority = 30
	// PriorityMCP is the /mcp (server list) payload.
	PriorityMCP Priority = 40
	// PriorityPlugin is the /plugin (installed-plugin list)
	// payload.
	PriorityPlugin Priority = 50
	// PrioritySkills is the /skills (skill catalog) payload.
	PrioritySkills Priority = 60
	// PriorityCommands is the /commands (slash-command catalog)
	// payload. Trimmed last.
	PriorityCommands Priority = 70
)

// PriorityFor returns the canonical Priority for a slash command
// name. Unknown names get PriorityCommands (the most conservative
// default — least likely to be trimmed). The reverse lookup is not
// needed because the canonical ordering is numeric and stable.
func PriorityFor(name string) Priority {
	switch name {
	case "customize":
		return PriorityCustomize
	case "config":
		return PriorityConfig
	case "agents":
		return PriorityAgents
	case "hooks":
		return PriorityHooks
	case "mcp":
		return PriorityMCP
	case "plugin":
		return PriorityPlugin
	case "skills":
		return PrioritySkills
	case "commands":
		return PriorityCommands
	default:
		// Unknown sources inherit the most-conservative value so
		// a future slash command the trimmer has not been taught
		// about is treated as "expensive, drop last". The
		// handler ordering is preserved regardless.
		return PriorityCommands
	}
}

// CommandNames is the canonical order the trimmer uses to walk the
// registered tokens. The slice is what tests rely on for the
// "/customize trimmed first" and "/agents trimmed after /customize"
// assertions: the comment in NewToken already says lower-priority
// tokens are trimmed first; this slice makes the order literal.
var CommandNames = []string{
	"customize", // 0 — trimmed first
	"config",    // 10
	"agents",    // 20 — trimmed after /customize
	"hooks",     // 30
	"mcp",       // 40
	"plugin",    // 50
	"skills",    // 60
	"commands",  // 70 — trimmed last
}
