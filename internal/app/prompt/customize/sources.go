package customize

// sources.go — per-command Source implementations.
//
// Each customization slash command the AGY changelog calls out —
// /customize, /config, /agents, /hooks, /mcp, /plugin, /skills,
// /commands — has a Source here. The Source contract is the one
// in token.go: a Name, a Priority, and a Render function that
// returns the current payload.
//
// Why Source and not just NewToken(name, content, priority)? Two
// reasons:
//
//   1. Tests can swap in a Source whose Render returns a fixed
//      string without rebuilding the token-by-token trimmer
//      wiring. The /customize gap is "tokens are heavy when
//      rendered"; we want a unit test to assert the trimmer
//      drops /customize before /agents when both are big, and
//      Source makes the setup trivial.
//   2. The context-assembly step in cmd/wagyu can iterate the
//      CommandNames slice, ask each Source to Render, and hand
//      the resulting tokens to the Trimmer without importing the
//      slash-command handlers themselves. The current bridge
//      lives in cmd/wagyu/customize_bridge.go and uses this
//      indirection.
//
// Render returns "" when the command has nothing to contribute.
// The trimmer treats an empty Content as a zero-token
// customization that is always kept, so a /mcp installation with
// no configured servers still reserves its slot but spends no
// budget.

// CustomizeSource is the /customize payload. In the reference this
// is the user's global customization override; in the clone (where
// the override surface is largely stubbed) it is a short fixed
// string that documents the override syntax. The body is small in
// the happy path and large when the user has filled in a custom
// section; the trimmer drops it first because the same
// customization can be re-derived from settings.json.
type CustomizeSource struct {
	// Body is the rendered override payload.
	Body string
}

// Name implements Source.
func (c *CustomizeSource) Name() string { return "customize" }

// Priority implements Source.
func (c *CustomizeSource) Priority() int { return int(PriorityCustomize) }

// Render implements Source.
func (c *CustomizeSource) Render() string { return c.Body }

// ConfigSource is the /config payload. The body is the
// "key\tvalue" table the cycle-102 print handler renders — the
// same data the model sees on every turn.
type ConfigSource struct {
	Body string
}

// Name implements Source.
func (c *ConfigSource) Name() string { return "config" }

// Priority implements Source.
func (c *ConfigSource) Priority() int { return int(PriorityConfig) }

// Render implements Source.
func (c *ConfigSource) Render() string { return c.Body }

// AgentsSource is the /agents payload: the per-agent definition
// block. A heavy /agents body is the second-most-common cause of
// "LLM has less room for actual work" complaints, which is why
// its Priority sits above /customize but below /config.
type AgentsSource struct {
	Body string
}

// Name implements Source.
func (a *AgentsSource) Name() string { return "agents" }

// Priority implements Source.
func (a *AgentsSource) Priority() int { return int(PriorityAgents) }

// Render implements Source.
func (a *AgentsSource) Render() string { return a.Body }

// HooksSource is the /hooks payload: the table of event handlers
// the agent fires before/after each tool call.
type HooksSource struct {
	Body string
}

// Name implements Source.
func (h *HooksSource) Name() string { return "hooks" }

// Priority implements Source.
func (h *HooksSource) Priority() int { return int(PriorityHooks) }

// Render implements Source.
func (h *HooksSource) Render() string { return h.Body }

// MCPSource is the /mcp payload: the configured MCP server list
// plus any inline tool descriptions the runtime exposes.
type MCPSource struct {
	Body string
}

// Name implements Source.
func (m *MCPSource) Name() string { return "mcp" }

// Priority implements Source.
func (m *MCPSource) Priority() int { return int(PriorityMCP) }

// Render implements Source.
func (m *MCPSource) Render() string { return m.Body }

// PluginSource is the /plugin payload: the installed-plugin list.
type PluginSource struct {
	Body string
}

// Name implements Source.
func (p *PluginSource) Name() string { return "plugin" }

// Priority implements Source.
func (p *PluginSource) Priority() int { return int(PriorityPlugin) }

// Render implements Source.
func (p *PluginSource) Render() string { return p.Body }

// SkillsSource is the /skills payload: the skill catalog the
// resource loader discovered.
type SkillsSource struct {
	Body string
}

// Name implements Source.
func (s *SkillsSource) Name() string { return "skills" }

// Priority implements Source.
func (s *SkillsSource) Priority() int { return int(PrioritySkills) }

// Render implements Source.
func (s *SkillsSource) Render() string { return s.Body }

// CommandsSource is the /commands payload: the slash-command
// catalog the picker overlay reads from. Trimmed last because
// the picker itself depends on it.
type CommandsSource struct {
	Body string
}

// Name implements Source.
func (c *CommandsSource) Name() string { return "commands" }

// Priority implements Source.
func (c *CommandsSource) Priority() int { return int(PriorityCommands) }

// Render implements Source.
func (c *CommandsSource) Render() string { return c.Body }

// RegisterSources walks CommandNames in canonical order and
// appends a Token to the trimmer for each source. Sources whose
// Render returns "" are skipped so the trimmer does not have to
// pay the bookkeeping cost of a zero-token entry (which would
// always keep anyway).
//
// The function is the bridge the cmd/wagyu context-assembly step
// calls; it accepts the canonical Source implementations so the
// handler side does not need to know the trimmer's wiring.
func RegisterSources(t *Trimmer, sources ...Source) {
	for _, s := range sources {
		if s == nil {
			continue
		}
		body := s.Render()
		if body == "" {
			continue
		}
		t.Add(s.Name(), body)
	}
}
