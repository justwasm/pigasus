// Package remotecontrol — plugins_ls.go: the language server's own
// plugin surface.
//
// # What this file is
//
// The Web UI's plugin panels are driven by the language server's
// plugin RPCs. The clone answered two of them with an empty list and
// let the rest fall through to "Method not found", so the panels had
// nothing to show.
//
// This file closes them against the state the clone actually has:
//
//   - the installed plugins — ~/.gemini/config/plugins/<name>/plugin.json
//   - the marketplace catalogues —
//     ~/.gemini/antigravity-cli/cache/marketplaces/<mkt>/marketplace.json
//
// The answers are proto-JSON in the schema's own field names
// (camelCase), and the lsproto codec puts them in the schema's
// declaration order on the way out.
//
// # The methods
//
// GetAllPlugins / GetPlugin / DeletePlugin — the installed plugins.
//
// ResolveCommandPlugin — which plugin owns a slash command.
//
// GetCustomizationStates — what the language server loaded: one state
// per plugin and per skill.
//
// SubscribeToPlugins — the stream of the same list (see ls_streams.go).
//
// SearchMarketplace / SearchMarketplaceCustomizations /
// GetMarketplaceCustomization / GetMarketplaceBundles /
// GetMarketplaceFeatured / GetSkillMarketplaceLink — the marketplace
// catalogues.
//
// GetAvailableCascadePlugins — the installed plugins as the cascade's
// plugin templates.
//
// # What this file does not do
//
// The cloud-only surfaces stay empty, on purpose: a plugin
// authentication provider, a "build with Google" plugin and a
// marketplace *install* all need the reference's cloud, and the clone
// has no account there. Answering them with an empty list is the
// honest answer; a fabricated one would be worse.
package remotecontrol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/features/plugins/core"
)

// lsPluginCatalog is the plugin state the Web UI's plugin surfaces
// read: the installed plugins and the marketplace catalogues.
type lsPluginCatalog struct {
	// Plugins are the installed plugins, in the reference's order.
	Plugins []core.InstalledPlugin
	// Marketplaces are the marketplace catalogues.
	Marketplaces []core.MarketplaceDoc
}

// readLSPluginCatalog reads the plugin state from disk.
//
// Every call re-reads: the Web UI's plugin panel is the surface a user
// installs a plugin from, and a list frozen at start-up would be a
// list that never changes.
func readLSPluginCatalog() lsPluginCatalog {
	return lsPluginCatalog{
		Plugins:      core.InstalledPlugins(),
		Marketplaces: core.MarketplaceDocs(),
	}
}

// ---- the installed plugins -------------------------------------------------

// routeLanguageServerPluginMethods is the language server's own plugin
// table.
//
// It is consulted before the reference-surface alias table, so the
// IDE's names win: the alias table knows nothing about these methods
// and would answer "Method not found" for them.
//
// The methods are the ones the Web UI's plugin panel calls.
func (d *Dispatcher) routeLanguageServerPluginMethods(method string, params json.RawMessage) (any, *rpcErrorBody, bool) {
	switch method {
	case "GetAllPlugins":
		return d.handleLSGetAllPlugins(params)
	case "GetPlugin":
		return d.handleLSGetPlugin(params)
	case "DeletePlugin":
		return d.handleLSDeletePlugin(params)
	case "ResolveCommandPlugin":
		return d.handleLSResolveCommandPlugin(params)
	case "GetCustomizationStates":
		return d.handleLSGetCustomizationStates(params)
	case "GetAllSkills":
		return d.handleLSGetAllSkills(params)
	case "DeleteSkill":
		return d.handleLSDeleteSkill(params)
	case "GetAllRules":
		return d.handleLSGetAllRules(params)
	case "GetAgentScripts":
		return d.handleLSGetAgentScripts(params)
	case "SubscribeToPlugins":
		// The server-streaming method answers the same list; the
		// stream itself is served by ls_streams.go.
		catalog := readLSPluginCatalog()
		plugins := make([]any, 0, len(catalog.Plugins))
		for _, p := range catalog.Plugins {
			plugins = append(plugins, lsPluginJSON(p, catalog.Marketplaces))
		}
		return map[string]any{"plugins": plugins}, nil, true
	case "SearchMarketplace":
		return d.handleLSSearchMarketplace(params)
	case "SearchMarketplaceCustomizations":
		return d.handleLSSearchMarketplaceCustomizations(params)
	case "GetMarketplaceCustomization":
		return d.handleLSGetMarketplaceCustomization(params)
	case "GetMarketplaceBundles":
		return d.handleLSGetMarketplaceBundles(params)
	case "GetMarketplaceFeatured":
		return d.handleLSGetMarketplaceFeatured(params)
	case "GetSkillMarketplaceLink":
		return d.handleLSGetSkillMarketplaceLink(params)
	case "GetAvailableCascadePlugins":
		return d.handleLSGetAvailableCascadePlugins(params)
	case "GetBuildWithGooglePlugins":
		return d.handleLSGetBuildWithGooglePlugins(params)
	case "ListPluginAuthProviders":
		return d.handleLSListPluginAuthProviders(params)
	case "InstallCustomization":
		return d.handleLSInstallCustomization(params)
	case "ListRemotePlugins":
		return d.handleLSListRemotePlugins(params)
	case "DisconnectRemotePlugin":
		return d.handleLSDisconnectRemotePlugin(params)
	case "DownloadBuildWithGooglePlugin":
		return d.handleLSDownloadBuildWithGooglePlugin(params)
	case "StartPluginAuthSignIn":
		return d.handleLSStartPluginAuthSignIn(params)
	case "SignInPluginAuthProvider":
		return d.handleLSSignInPluginAuthProvider(params)
	case "SignOutPluginAuthProvider":
		return d.handleLSSignOutPluginAuthProvider(params)
	case "SetDefaultPluginAuthAccount":
		return d.handleLSSetDefaultPluginAuthAccount(params)
	case "SetPluginAuthAccountLabel":
		return d.handleLSSetPluginAuthAccountLabel(params)
	}
	return nil, nil, false
}

// handleLSGetAllPlugins answers GetAllPlugins.
//
// 【反编译】 exa.language_server_pb.GetAllPluginsResponse carries one
// repeated member, `plugins`.
func (d *Dispatcher) handleLSGetAllPlugins(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	catalog := readLSPluginCatalog()
	plugins := make([]any, 0, len(catalog.Plugins))
	for _, p := range catalog.Plugins {
		plugins = append(plugins, lsPluginJSON(p, catalog.Marketplaces))
	}
	return map[string]any{"plugins": plugins}, nil, true
}

// handleLSGetPlugin answers GetPlugin.
//
// An unknown plugin id is not an error: the reference answers with an
// empty plugin, and the Web UI renders "not installed".
func (d *Dispatcher) handleLSGetPlugin(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		PluginID string `json:"pluginId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.PluginID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "plugin_id is required"}, true
	}
	catalog := readLSPluginCatalog()
	for _, p := range catalog.Plugins {
		if p.Name == id {
			return map[string]any{"plugin": lsPluginJSON(p, catalog.Marketplaces)}, nil, true
		}
	}
	return map[string]any{"plugin": map[string]any{}}, nil, true
}

// handleLSDeletePlugin answers DeletePlugin.
//
// The deletion is the user's own action from the Web UI's plugin
// panel, and it removes the plugin's directory.
//
// The name is checked against the plugins tree before anything is
// removed: a name that escapes it is refused, so a crafted id cannot
// delete something else on the machine.
func (d *Dispatcher) handleLSDeletePlugin(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		PluginID string `json:"pluginId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.PluginID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "plugin_id is required"}, true
	}
	dir, ok := lsPluginDir(id)
	if !ok {
		return map[string]any{"success": false, "message": "Plugin not found: " + id}, nil, true
	}
	if _, err := os.Stat(dir); err != nil {
		return map[string]any{"success": false, "message": "Plugin not found: " + id}, nil, true
	}
	if err := os.RemoveAll(dir); err != nil {
		return map[string]any{"success": false, "message": err.Error()}, nil, true
	}
	return map[string]any{"success": true, "message": "Deleted " + id}, nil, true
}

// lsPluginDir resolves a plugin name to its directory, and whether the
// name is one the plugins tree can hold.
//
// The name is the directory's name — the reference addresses a plugin
// by it — so a name carrying a separator is not a plugin name.
func lsPluginDir(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return "", false
	}
	return filepath.Join(core.StateDir(), name), true
}

// handleLSResolveCommandPlugin answers ResolveCommandPlugin.
//
// The request carries the command line the user typed. The command is
// its first word — a slash command is "/<name>" — and the answer is
// the plugin that provides it.
//
// A plugin's skills are its slash commands: the reference registers
// every skill of every loaded plugin under the skill's own name.
func (d *Dispatcher) handleLSResolveCommandPlugin(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		CascadeID   string `json:"cascadeId"`
		CommandLine string `json:"commandLine"`
		ShellName   string `json:"shellName"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	command := lsCommandName(req.CommandLine)
	if command == "" {
		return map[string]any{"plugin": map[string]any{}}, nil, true
	}
	for _, p := range readLSPluginCatalog().Plugins {
		for _, skill := range core.PluginSkills(p.Name) {
			if skill.Name == command {
				return map[string]any{"plugin": lsPluginRefJSON(p)}, nil, true
			}
		}
	}
	return map[string]any{"plugin": map[string]any{}}, nil, true
}

// lsCommandName returns the command's name out of a command line.
//
// The line is "/name arg1 arg2" or "name arg1 arg2"; the name is the
// first word, without its leading slash.
func lsCommandName(commandLine string) string {
	line := strings.TrimSpace(commandLine)
	if line == "" {
		return ""
	}
	if i := strings.IndexAny(line, " \t\n"); i >= 0 {
		line = line[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(line), "/")
}

// handleLSGetCustomizationStates answers GetCustomizationStates.
//
// A state is one loaded customization: a plugin, a skill, a rule, an
// agent. The reference's own state carries the type, the name, the
// path, the scope and the status.
func (d *Dispatcher) handleLSGetCustomizationStates(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	catalog := readLSPluginCatalog()
	states := make([]any, 0, len(catalog.Plugins))
	for _, p := range catalog.Plugins {
		states = append(states, lsCustomizationStateJSON("REFRESH_CUSTOMIZATION_TYPE_PLUGIN", p.Name, p.Path, p.Disabled, ""))
		for _, skill := range core.PluginSkills(p.Name) {
			states = append(states, lsCustomizationStateJSON("REFRESH_CUSTOMIZATION_TYPE_SKILL", skill.Name, skill.Path, p.Disabled, p.Name))
		}
		for _, rule := range lsPluginRules(p) {
			states = append(states, lsCustomizationStateJSON("REFRESH_CUSTOMIZATION_TYPE_RULE", rule.Name, rule.Path, p.Disabled, p.Name))
		}
		for _, agent := range core.Agents(p.Name) {
			states = append(states, lsCustomizationStateJSON("REFRESH_CUSTOMIZATION_TYPE_AGENT", agent, lsAgentPath(p, agent), p.Disabled, p.Name))
		}
	}
	return map[string]any{"states": states}, nil, true
}

// ---- the skills -------------------------------------------------------------

// handleLSGetAllSkills answers GetAllSkills.
//
// 【反编译】 exa.language_server_pb.GetAllSkillsResponse carries a
// repeated exa.cortex_pb.WorkflowSpec.
//
// The skills are the ones the installed plugins contribute — the
// skill directories under each plugin. The request's workspace URIs
// do not narrow the answer: the clone's plugins are the user's own
// tree, and the reference's own workspace skills live in the
// workspace, which the clone does not scan here.
func (d *Dispatcher) handleLSGetAllSkills(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	skills := make([]any, 0)
	for _, p := range readLSPluginCatalog().Plugins {
		for _, s := range core.PluginSkills(p.Name) {
			skills = append(skills, lsWorkflowSpecJSON(p, s))
		}
	}
	return map[string]any{"skills": skills}, nil, true
}

// handleLSGetAllRules answers GetAllRules.
//
// 【反编译】 exa.language_server_pb.GetAllRulesResponse carries one
// repeated member, `memories` — a repeated exa.cortex_pb.CortexMemory.
//
// The memories are the rules the installed plugins contribute: one
// memory per rule file under a plugin's rules/ directory. The
// request's workspace URIs do not narrow the answer — the clone's
// rules live in the plugins tree, the user's own tree, and the
// reference's own workspace rules live in the workspace, which the
// clone does not scan here.
//
// The memory carries a `metadata` whose `tags` name it a plugin rule:
// the Web UI's own rules list filters the plugin rules out
// (`metadata?.tags.includes("plugin_rule")`), so a plugin's rule is
// shown in the plugin panel and not twice.
func (d *Dispatcher) handleLSGetAllRules(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	memories := make([]any, 0)
	for _, p := range readLSPluginCatalog().Plugins {
		memories = append(memories, lsPluginRulesJSON(p)...)
	}
	return map[string]any{"memories": memories}, nil, true
}

// handleLSGetAgentScripts answers GetAgentScripts.
//
// 【反编译】 exa.language_server_pb.GetAgentScriptsResponse carries
// `agent_scripts` (a repeated exa.cortex_pb.AgentScriptItem) and
// `agent_customizations` (a repeated exa.cortex_pb.AgentCustomization).
//
// The scripts are the agents the installed plugins contribute: one
// agent per file under a plugin's agents/ directory. A customization
// is the same agent with the path it was found at; the script is the
// agent itself.
//
// The Web UI's agent picker reads the customizations (path, the
// agent's name and description, is_global) and keeps the scripts for
// the agent's own configuration.
func (d *Dispatcher) handleLSGetAgentScripts(params json.RawMessage) (any, *rpcErrorBody, bool) {
	_ = params
	scripts := make([]any, 0)
	customizations := make([]any, 0)
	for _, p := range readLSPluginCatalog().Plugins {
		for _, agent := range core.Agents(p.Name) {
			script := lsAgentScriptJSON(p, agent)
			scripts = append(scripts, script)
			customizations = append(customizations, map[string]any{
				"path":      filepath.Join(p.Path, "agents", agent+".md"),
				"agent":     script,
				"isGlobal":  true,
				"isBuiltin": false,
			})
		}
	}
	return map[string]any{
		"agentScripts":        scripts,
		"agentCustomizations": customizations,
	}, nil, true
}

// handleLSDeleteSkill answers DeleteSkill.
//
// 【反编译】 exa.language_server_pb.DeleteSkillResponse carries success
// (1) and message (2).
//
// The skill id is the skill's path — the Web UI passes the path the
// skill list carried. The skill's directory is removed, and only a
// directory inside the plugins tree: a path that escapes it is
// refused, so a crafted id cannot delete something else on the
// machine.
func (d *Dispatcher) handleLSDeleteSkill(params json.RawMessage) (any, *rpcErrorBody, bool) {
	var req struct {
		SkillID string `json:"skillId"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: err.Error()}, true
		}
	}
	id := strings.TrimSpace(req.SkillID)
	if id == "" {
		return nil, &rpcErrorBody{Code: -32602, Message: "Invalid params", Data: "skill_id is required"}, true
	}
	dir, ok := lsSkillDir(id)
	if !ok {
		return map[string]any{"success": false, "message": "Skill not found: " + id}, nil, true
	}
	if err := os.RemoveAll(dir); err != nil {
		return map[string]any{"success": false, "message": err.Error()}, nil, true
	}
	return map[string]any{"success": true, "message": "Deleted " + id}, nil, true
}

// lsSkillDir resolves a skill id to the directory to remove, and
// whether the id names a skill inside the plugins tree.
//
// The id is the SKILL.md's path — "<plugins>/<plugin>/skills/<skill>/
// SKILL.md" — so the directory is the path without its file name.
// The path has to sit inside the plugins tree, and the directory has
// to sit inside a plugin's skills directory.
func lsSkillDir(id string) (string, bool) {
	root := core.StateDir()
	abs := filepath.Clean(id)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, abs)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	// The id may point at the SKILL.md itself or at the directory.
	dir := abs
	if filepath.Ext(abs) == ".md" {
		dir = filepath.Dir(abs)
	}
	if dir == rootAbs || !strings.HasPrefix(dir, rootAbs+string(filepath.Separator)) {
		return "", false
	}
	// …/<plugin>/skills/<skill>
	parts := strings.Split(filepath.ToSlash(dir), "/")
	if len(parts) < 3 || parts[len(parts)-2] != "skills" {
		return "", false
	}
	return dir, true
}

// ---- the plugin model ------------------------------------------------------

// lsPluginJSON renders one installed plugin in the language server's
// own Plugin shape.
//
// 【反编译】 exa.language_server_pb.Plugin declares name (1), path (2),
// is_global (3), skills (4), agents (5), mcp_servers (6), rules (7),
// hooks_json (8), workflows (26), disabled (9), description (10),
// readme (11), logo (12), sidecars (13), installed_from (14),
// user_config (15), version (16), suggested_prompts (17),
// display_name (18), is_builtin (19), force_disabled (20),
// disabled_reason (21), auth_required (22), authorize_uri (23),
// is_authenticated (24), accounts (25), themes (27), author (28),
// homepage (29), repository (30).
func lsPluginJSON(p core.InstalledPlugin, markets []core.MarketplaceDoc) map[string]any {
	m := p.Manifest
	out := map[string]any{
		"name":             p.Name,
		"path":             p.Path,
		"isGlobal":         true,
		"skills":           lsPluginSkillsJSON(p),
		"agents":           lsPluginAgentsJSON(p),
		"mcpServers":       lsPluginMCPServersJSON(p),
		"rules":            lsPluginRulesJSON(p),
		"hooksJson":        "",
		"workflows":        []any{},
		"disabled":         p.Disabled,
		"description":      m.Description,
		"readme":           lsPluginReadme(p),
		"logo":             m.Logo,
		"sidecars":         lsPluginSidecarsJSON(p),
		"version":          m.Version,
		"suggestedPrompts": lsStringsJSON(m.SuggestedPrompts),
		"displayName":      lsPluginDisplayName(p),
		"isBuiltin":        false,
		"forceDisabled":    false,
		"disabledReason":   "",
		"authRequired":     false,
		"authorizeUri":     "",
		"isAuthenticated":  true,
		"author":           lsAuthorJSON(m.Author),
		"homepage":         m.Homepage,
		"repository":       m.Repository,
	}
	if from, ok := lsInstalledFromJSON(p.Name, markets); ok {
		out["installedFrom"] = from
	}
	return out
}

// lsPluginDisplayName returns the plugin's display name: the
// manifest's, else the stock table's, else the plugin's own name.
func lsPluginDisplayName(p core.InstalledPlugin) string {
	if p.Manifest.DisplayName != "" {
		return p.Manifest.DisplayName
	}
	return p.Name
}

// lsPluginRefJSON renders the plugin reference the command resolution
// answers with.
//
// 【反编译】 exa.codeium_common_pb.PluginRef declares name (1),
// display_name (2), logo (3).
func lsPluginRefJSON(p core.InstalledPlugin) map[string]any {
	return map[string]any{
		"name":        p.Name,
		"displayName": lsPluginDisplayName(p),
		"logo":        p.Manifest.Logo,
	}
}

// lsAuthorJSON renders an author, or nothing when there is none.
func lsAuthorJSON(a core.Author) map[string]any {
	out := map[string]any{}
	if a.Name != "" {
		out["name"] = a.Name
	}
	if a.URL != "" {
		out["url"] = a.URL
	}
	return out
}

// lsInstalledFromJSON reports the marketplace a plugin was installed
// from, when the catalogue knows it.
//
// The catalogue is the only place the marketplace's own name survives:
// the installed plugin's plugin.json carries the archive URL, not the
// marketplace it came from.
func lsInstalledFromJSON(name string, markets []core.MarketplaceDoc) (map[string]any, bool) {
	entry, doc, ok := core.MarketplaceEntryByID(markets, name)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"marketplace": doc.Name,
		"id":          entry.Name,
		"version":     entry.Version,
	}, true
}

// lsPluginSkillsJSON renders the plugin's skills.
//
// 【反编译】 a skill is an exa.cortex_pb.WorkflowSpec: path (1),
// name (2), description (3), content (4), is_builtin (6), base_dir (8),
// plugin_name (10), discovered_in (12), display_name (17),
// plugin (18).
func lsPluginSkillsJSON(p core.InstalledPlugin) []any {
	skills := core.PluginSkills(p.Name)
	out := make([]any, 0, len(skills))
	for _, s := range skills {
		out = append(out, lsWorkflowSpecJSON(p, s))
	}
	return out
}

// lsWorkflowSpecJSON renders one skill in the language server's own
// WorkflowSpec shape.
//
// 【反编译】 exa.cortex_pb.WorkflowSpec declares path (1), name (2),
// description (3), content (4), is_builtin (6), scope (7),
// base_dir (8), disable_model_invocation (9), plugin_name (10),
// disable_slash_command (11), discovered_in (12),
// discovery_category (13), icon (14), logo (15), installed_from (16),
// display_name (17), plugin (18) and depot_path (19).
func lsWorkflowSpecJSON(p core.InstalledPlugin, s core.SkillInfo) map[string]any {
	return map[string]any{
		"path":         s.Path,
		"name":         s.Name,
		"description":  s.Description,
		"content":      lsSkillContent(s.Path),
		"isBuiltin":    false,
		"baseDir":      filepath.Dir(s.Path),
		"pluginName":   p.Name,
		"discoveredIn": p.Name,
		"displayName":  s.Name,
		"plugin":       lsPluginRefJSON(p),
	}
}

// lsSkillContent reads the skill's markdown body.
//
// The frontmatter is the header, not the content: the body is
// everything after the closing fence.
func lsSkillContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return string(data)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return string(data)
}

// lsPluginAgentsJSON renders the plugin's agents.
//
// 【反编译】 an agent is an exa.cortex_pb.AgentCustomization: path (1),
// agent (2), is_global (3), is_builtin (4).
func lsPluginAgentsJSON(p core.InstalledPlugin) []any {
	agents := core.Agents(p.Name)
	out := make([]any, 0, len(agents))
	for _, a := range agents {
		out = append(out, map[string]any{
			"path":      lsAgentPath(p, a),
			"agent":     map[string]any{"name": a},
			"isGlobal":  true,
			"isBuiltin": false,
		})
	}
	return out
}

// lsPluginMCPServersJSON renders the plugin's MCP servers.
//
// 【反编译】 a server is an exa.cortex_pb.McpServerSpec: server_name
// (1), command (2), args (3), env (4), server_url (6), disabled (7),
// server_index (5), plugin_name (17), plugin_root (24),
// plugin_data_dir (25), config_path (26), is_global (27).
//
// The plugin's mcp_config.json is the standard MCP configuration
// shape: {"mcpServers": {"<name>": {"command": …, "args": […]}}}.
func lsPluginMCPServersJSON(p core.InstalledPlugin) []any {
	servers, err := lsReadMCPServers(p.Path)
	if err != nil {
		return []any{}
	}
	out := make([]any, 0, len(servers))
	for i, s := range servers {
		entry := map[string]any{
			"serverName":         s.Name,
			"command":            s.Command,
			"args":               lsStringsJSON(s.Args),
			"disabled":           false,
			"serverIndex":        i,
			"pluginName":         p.Name,
			"pluginRoot":         p.Path,
			"configPath":         filepath.Join(p.Path, "mcp_config.json"),
			"isGlobal":           true,
			"pluginVersion":      p.Manifest.Version,
			"originalServerName": s.Name,
		}
		out = append(out, entry)
	}
	return out
}

// lsMCPConfig is the plugin's mcp_config.json.
type lsMCPConfig struct {
	Servers map[string]struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url"`
	} `json:"mcpServers"`
}

// lsReadMCPServers reads a plugin's mcp_config.json.
func lsReadMCPServers(pluginDir string) (map[string]lsMCPConfigServer, error) {
	data, err := os.ReadFile(filepath.Join(pluginDir, "mcp_config.json"))
	if err != nil {
		return nil, err
	}
	var doc lsMCPConfig
	if err := json.Unmarshal(core.StripBOM(data), &doc); err != nil {
		return nil, err
	}
	out := map[string]lsMCPConfigServer{}
	for name, s := range doc.Servers {
		out[name] = lsMCPConfigServer{Name: name, Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL}
	}
	return out, nil
}

// lsMCPConfigServer is one server of the plugin's mcp_config.json.
type lsMCPConfigServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
}

// lsPluginRulesJSON renders the plugin's rules.
//
// 【反编译】 a rule is an exa.cortex_pb.CortexMemory: memory_id (1),
// title (6), source (3), scope (4), text_memory (5),
// absolute_path (7), discovered_in (8), discovery_category (9).
//
// The text lives in a CortexMemoryText — a message with a single
// member, `content` (1). The member is *not* `text`: a rule answered
// with `{"textMemory":{"text":…}}` decodes without an error (the
// Web UI ignores unknown members) and arrives with **no text at all**,
// because the member it wrote is not the member the schema knows.
//
// The scope is the rule's own file, in the global scope: the plugin
// tree is the user's own tree, not the workspace's.
func lsPluginRulesJSON(p core.InstalledPlugin) []any {
	rules := lsPluginRules(p)
	out := make([]any, 0, len(rules))
	for _, r := range rules {
		out = append(out, map[string]any{
			"memoryId":     r.Name,
			"title":        r.Name,
			"absolutePath": r.Path,
			"discoveredIn": p.Name,
			"scope": map[string]any{
				"globalScope": map[string]any{"absoluteFilePath": r.Path},
			},
			// The tags name the rule a plugin's rule: the Web UI's
			// own rules list filters them out, so a plugin's rule is
			// shown in the plugin panel and not twice.
			//
			// The tags have to be there: the filter reads
			// `metadata?.tags.includes(...)`, and a metadata message
			// without a tags member would throw in the browser.
			"metadata":          map[string]any{"tags": []any{"plugin_rule"}},
			"discoveryCategory": "DISCOVERY_CATEGORY_INSTALLED",
			"textMemory":        map[string]any{"content": r.Text},
		})
	}
	return out
}

// lsPluginRules reads the plugin's rule files.
func lsPluginRules(p core.InstalledPlugin) []lsRuleFile {
	rules, err := lsReadRuleFiles(p.Path)
	if err != nil {
		return nil
	}
	return rules
}

// lsAgentScriptJSON renders one agent as an
// exa.cortex_pb.AgentScriptItem.
//
// 【反编译】 the message declares name (1), description (8), disabled
// (9), hidden (10), subagent (12), main_agent (13), command_spec (3),
// python_spec (4), config_path (6), config (7), cascade_config (11),
// disable_model_selection (14), model_tier (15), input_placeholder
// (16) and plugin_name (17).
//
// The clone knows the agent's name, the description its markdown
// definition's frontmatter carries, the plugin it belongs to and
// whether that plugin is disabled. The five `source` members
// (command_spec, python_spec, config_path, config, cascade_config)
// are oneofs the clone does not have a value for — a plugin's agent is
// a markdown definition, not a script — so they stay unset.
//
// The agent is a subagent (the clone's own reader calls the plugin's
// agents/*.md "subagent definitions") and never the main agent.
func lsAgentScriptJSON(p core.InstalledPlugin, agent string) map[string]any {
	out := map[string]any{
		"name":       agent,
		"disabled":   p.Disabled,
		"hidden":     false,
		"subagent":   true,
		"mainAgent":  false,
		"pluginName": p.Name,
	}
	if description := lsAgentDescription(p, agent); description != "" {
		out["description"] = description
	}
	return out
}

// lsAgentDescription returns the description an agent's markdown
// frontmatter carries, or "" when the file has none.
//
// The name is *not* taken from the frontmatter: the clone's own
// readers take the file's stem as the agent's name (the reference
// prefers the frontmatter's `name:`), so the two agree on the file and
// differ only where a plugin ships a different name in the
// frontmatter.
func lsAgentDescription(p core.InstalledPlugin, agent string) string {
	_, description, ok := core.AgentFrontmatter(lsAgentPath(p, agent))
	if !ok {
		return ""
	}
	return description
}

// lsAgentPath returns the file an agent is defined in.
//
// A plugin may declare its agents in agents.json, where the entry
// carries the path; an agent that is only a markdown file lives under
// the plugin's agents/ directory.
func lsAgentPath(p core.InstalledPlugin, agent string) string {
	entries, _ := core.ReadAgentsJSON(p.Name)
	for _, e := range entries {
		if e.Name != agent || e.Path == "" {
			continue
		}
		if filepath.IsAbs(e.Path) {
			return e.Path
		}
		return filepath.Join(p.Path, e.Path)
	}
	return filepath.Join(p.Path, "agents", agent+".md")
}

// lsRuleFile is one rule file of a plugin.
type lsRuleFile struct {
	Name string
	Path string
	Text string
}

// lsReadRuleFiles reads a plugin's rules directory.
//
// The reference's plugins keep their rules as markdown files under
// rules/; the file's own name is the rule's name.
func lsReadRuleFiles(pluginDir string) ([]lsRuleFile, error) {
	dir := filepath.Join(pluginDir, "rules")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []lsRuleFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		out = append(out, lsRuleFile{Name: name, Path: filepath.Join(dir, e.Name()), Text: string(data)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// lsPluginSidecarsJSON renders the plugin's sidecars.
//
// 【反编译】 a sidecar is an exa.language_server_pb.PluginSidecar:
// sidecar_id (1), config (2).
func lsPluginSidecarsJSON(p core.InstalledPlugin) []any {
	entries, err := os.ReadDir(filepath.Join(p.Path, "sidecars"))
	if err != nil {
		return []any{}
	}
	out := make([]any, 0, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, map[string]any{
			"sidecarId": e.Name(),
			"config":    map[string]any{},
		})
	}
	return out
}

// lsPluginReadme returns the plugin's README, when it has one.
func lsPluginReadme(p core.InstalledPlugin) string {
	if p.Manifest.Readme != "" {
		return p.Manifest.Readme
	}
	data, err := os.ReadFile(filepath.Join(p.Path, "README.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// lsCustomizationStateJSON renders one loaded customization.
//
// 【反编译】 exa.cortex_pb.LoadedCustomizations.LoadedCustomization
// declares type (1), name (2), path (3), scope (4), status (5),
// plugin_name (6), error_detail (7), version (8), installed_from (9).
//
// The type is the *full* enum name —
// REFRESH_CUSTOMIZATION_TYPE_PLUGIN, REFRESH_CUSTOMIZATION_TYPE_SKILL —
// because the field is a
// exa.codeium_common_pb.RefreshCustomizationType. A short name
// ("PLUGIN") is not a value the enum knows: protojson drops the
// message that carries it, so the whole `states` list would reach
// the Web UI empty and without an error.
//
// The scope is SCOPE_GLOBAL (2) — the plugins tree is the user's own
// tree — and the status is STATUS_ENABLED (1) or STATUS_DISABLED (2).
func lsCustomizationStateJSON(kind, name, path string, disabled bool, pluginName string) map[string]any {
	status := "STATUS_ENABLED"
	if disabled {
		status = "STATUS_DISABLED"
	}
	return map[string]any{
		"type":       kind,
		"name":       name,
		"path":       path,
		"scope":      "SCOPE_GLOBAL",
		"status":     status,
		"pluginName": pluginName,
	}
}

// lsStringsJSON renders a string list, never a nil one.
func lsStringsJSON(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}
