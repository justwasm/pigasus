// plugins_ls_test.go — the language server's plugin surface.
//
// The Web UI's plugin panels are driven by these RPCs, so the tests
// here check two things: that the answers carry the state on disk,
// and that the answers survive the reference's own parser.
//
// The second one matters more than it looks. The Web UI decodes the
// answer with the reference's own descriptor and
// `ignoreUnknownFields: true` — an unknown member is fine, but a
// *known* member with the wrong JSON type fails the whole decode, and
// the panel renders an error instead of the plugin list.
//
// 【一手】 2026-10-30: the shapes are read off this machine's
// ~/.gemini/config/plugins/*/plugin.json and
// ~/.gemini/antigravity-cli/cache/marketplaces/*/marketplace.json.
package remotecontrol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/lsproto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// seedPluginTree writes a plugins tree and a marketplace catalogue
// under a temporary $ANTIGRAVITY_HOME, and returns the two roots.
//
// The tree is the reference's own shape:
//
//	<gemini>/config/plugins/<plugin>/plugin.json
//	<gemini>/config/plugins/<plugin>/skills/<skill>/SKILL.md
//	<antigravity>/cache/marketplaces/<mkt>/marketplace.json
func seedPluginTree(t *testing.T) (pluginsRoot, marketplaceRoot string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))

	pluginsRoot = filepath.Join(home, ".gemini", "config", "plugins")
	marketplaceRoot = filepath.Join(home, ".gemini", "antigravity-cli", "cache", "marketplaces")

	// A plugin with a skill and an agent.
	writePlugin(t, pluginsRoot, "demo-plugin", map[string]any{
		"name":        "demo-plugin",
		"displayName": "Demo Plugin",
		"version":     "1.2.3",
		"description": "A demo plugin.",
		"logo":        "https://example.invalid/logo.svg",
		"author":      map[string]any{"name": "Demo Author", "url": "https://example.invalid"},
		"homepage":    "https://example.invalid",
		"repository":  "https://example.invalid/repo",
		"suggestedPrompts": []string{
			"Do the demo thing",
		},
	})
	writeFile(t, filepath.Join(pluginsRoot, "demo-plugin", "skills", "demo-skill", "SKILL.md"),
		"---\nname: demo-skill\ndescription: The demo skill's description.\n---\n\nThe body.\n")
	writeFile(t, filepath.Join(pluginsRoot, "demo-plugin", "agents", "reviewer.md"), "# reviewer\n")
	writeFile(t, filepath.Join(pluginsRoot, "demo-plugin", "rules", "AGENTS.md"), "Be careful.\n")

	// A second plugin with an MCP server and a sidecar.
	writePlugin(t, pluginsRoot, "mcp-plugin", map[string]any{
		"name":        "mcp-plugin",
		"displayName": "MCP Plugin",
		"version":     "0.1.0",
		"description": "A plugin with an MCP server.",
	})
	writeFile(t, filepath.Join(pluginsRoot, "mcp-plugin", "mcp_config.json"),
		`{"mcpServers":{"demo-server":{"command":"demo","args":["--flag"]}}}`)
	if err := os.MkdirAll(filepath.Join(pluginsRoot, "mcp-plugin", "sidecars", "demo-sidecar"), 0o755); err != nil {
		t.Fatalf("mkdir sidecar: %v", err)
	}

	// The marketplace catalogue.
	if err := os.MkdirAll(filepath.Join(marketplaceRoot, "demo-market"), 0o755); err != nil {
		t.Fatalf("mkdir marketplace: %v", err)
	}
	writeFile(t, filepath.Join(marketplaceRoot, "demo-market", "marketplace.json"), `{
  "name": "demo-market",
  "description": "The demo marketplace.",
  "plugins": [
    {
      "name": "demo-plugin",
      "displayName": "Demo Plugin",
      "version": "1.2.3",
      "description": "A demo plugin.",
      "logo": "https://example.invalid/logo.svg",
      "author": {"name": "Demo Author"},
      "homepage": "https://example.invalid",
      "repository": "https://example.invalid/repo",
      "keywords": ["demo"],
      "source": {"type": "archive", "url": "https://example.invalid/demo.zip", "sha256": "abc"}
    },
    {
      "name": "other-plugin",
      "displayName": "Other Plugin",
      "version": "2.0.0",
      "description": "Something else entirely.",
      "source": {"type": "archive", "url": "https://example.invalid/other.zip", "sha256": "def"}
    }
  ]
}`)
	return pluginsRoot, marketplaceRoot
}

// writePlugin writes a plugin's plugin.json and creates its directory.
func writePlugin(t *testing.T, root, name string, manifest map[string]any) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	writeFile(t, filepath.Join(dir, "plugin.json"), string(data))
}

// writeFile writes a file, creating its directory.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// pluginTestDispatcher builds a dispatcher over the seeded tree.
func pluginTestDispatcher(t *testing.T) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(Config{Session: NewSessionState("plugin-test")})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// callPluginRPC dispatches one method and returns the decoded answer.
func callPluginRPC(t *testing.T, d *Dispatcher, method, params string) map[string]any {
	t.Helper()
	res, rpcErr, ok := d.routeLanguageServerPluginMethods(method, json.RawMessage(params))
	if !ok {
		t.Fatalf("%s is not routed", method)
	}
	if rpcErr != nil {
		t.Fatalf("%s: rpc error %+v", method, rpcErr)
	}
	out, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("%s: answer is %T, want a map", method, res)
	}
	return out
}

// TestGetAllPlugins_ListsTheInstalledPlugins pins the installed list.
func TestGetAllPlugins_ListsTheInstalledPlugins(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetAllPlugins", "{}")
	plugins, ok := answer["plugins"].([]any)
	if !ok {
		t.Fatalf("answer has no plugins list: %#v", answer)
	}
	if len(plugins) != 2 {
		t.Fatalf("got %d plugins, want 2: %#v", len(plugins), plugins)
	}
	first, _ := plugins[0].(map[string]any)
	if first["name"] != "demo-plugin" {
		t.Errorf("the first plugin is %v, want demo-plugin", first["name"])
	}
	if first["displayName"] != "Demo Plugin" {
		t.Errorf("displayName = %v, want Demo Plugin", first["displayName"])
	}
	if first["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", first["version"])
	}
	if first["path"] == "" {
		t.Error("the plugin carries no path")
	}
	author, _ := first["author"].(map[string]any)
	if author["name"] != "Demo Author" {
		t.Errorf("author.name = %v, want Demo Author", author["name"])
	}
}

// TestGetAllPlugins_CarriesTheSkillsAgentsAndRules pins the plugin's
// own members.
func TestGetAllPlugins_CarriesTheSkillsAgentsAndRules(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetAllPlugins", "{}")
	plugins := answer["plugins"].([]any)
	var demo map[string]any
	for _, p := range plugins {
		m := p.(map[string]any)
		if m["name"] == "demo-plugin" {
			demo = m
		}
	}
	if demo == nil {
		t.Fatalf("demo-plugin is missing from the answer: %#v", plugins)
	}
	skills, _ := demo["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("got %d skills, want 1: %#v", len(skills), skills)
	}
	skill := skills[0].(map[string]any)
	if skill["name"] != "demo-skill" {
		t.Errorf("skill.name = %v, want demo-skill", skill["name"])
	}
	if skill["description"] != "The demo skill's description." {
		t.Errorf("skill.description = %v", skill["description"])
	}
	agents, _ := demo["agents"].([]any)
	if len(agents) != 1 {
		t.Errorf("got %d agents, want 1: %#v", len(agents), agents)
	}
	rules, _ := demo["rules"].([]any)
	if len(rules) != 1 {
		t.Errorf("got %d rules, want 1: %#v", len(rules), rules)
	}

	// The MCP plugin's server.
	var mcp map[string]any
	for _, p := range plugins {
		m := p.(map[string]any)
		if m["name"] == "mcp-plugin" {
			mcp = m
		}
	}
	if mcp == nil {
		t.Fatalf("mcp-plugin is missing from the answer")
	}
	servers, _ := mcp["mcpServers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("got %d MCP servers, want 1: %#v", len(servers), servers)
	}
	server := servers[0].(map[string]any)
	if server["serverName"] != "demo-server" {
		t.Errorf("serverName = %v, want demo-server", server["serverName"])
	}
	if server["command"] != "demo" {
		t.Errorf("command = %v, want demo", server["command"])
	}
}

// TestGetPlugin_OneAndTheUnknownOne pins both answers.
func TestGetPlugin_OneAndTheUnknownOne(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetPlugin", `{"pluginId":"demo-plugin"}`)
	plugin, _ := answer["plugin"].(map[string]any)
	if plugin["name"] != "demo-plugin" {
		t.Errorf("plugin.name = %v, want demo-plugin", plugin["name"])
	}

	unknown := callPluginRPC(t, d, "GetPlugin", `{"pluginId":"nope"}`)
	p, ok := unknown["plugin"].(map[string]any)
	if !ok {
		t.Fatalf("the unknown answer carries no plugin: %#v", unknown)
	}
	if len(p) != 0 {
		t.Errorf("the unknown plugin is %v, want an empty one", p)
	}
}

// TestDeletePlugin_RemovesTheDirectory pins the deletion and the
// refusal for an unknown id.
func TestDeletePlugin_RemovesTheDirectory(t *testing.T) {
	pluginsRoot, _ := seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "DeletePlugin", `{"pluginId":"mcp-plugin"}`)
	if answer["success"] != true {
		t.Fatalf("the delete failed: %#v", answer)
	}
	if _, err := os.Stat(filepath.Join(pluginsRoot, "mcp-plugin")); !os.IsNotExist(err) {
		t.Errorf("the plugin directory survived the delete: %v", err)
	}

	unknown := callPluginRPC(t, d, "DeletePlugin", `{"pluginId":"nope"}`)
	if unknown["success"] != false {
		t.Errorf("the unknown delete claims success: %#v", unknown)
	}
}

// TestDeletePlugin_RefusesAPathOutsideTheTree pins the guard: a crafted
// id must not delete anything else on the machine.
func TestDeletePlugin_RefusesAPathOutsideTheTree(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "DeletePlugin", `{"pluginId":"../../etc"}`)
	if answer["success"] != false {
		t.Errorf("a traversing id was accepted: %#v", answer)
	}
}

// TestResolveCommandPlugin_ResolvesASkill pins the command resolution.
func TestResolveCommandPlugin_ResolvesASkill(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "ResolveCommandPlugin", `{"commandLine":"/demo-skill"}`)
	plugin, _ := answer["plugin"].(map[string]any)
	if plugin["name"] != "demo-plugin" {
		t.Errorf("plugin.name = %v, want demo-plugin", plugin["name"])
	}
	if plugin["displayName"] != "Demo Plugin" {
		t.Errorf("plugin.displayName = %v, want Demo Plugin", plugin["displayName"])
	}

	unknown := callPluginRPC(t, d, "ResolveCommandPlugin", `{"commandLine":"/nope"}`)
	p, _ := unknown["plugin"].(map[string]any)
	if len(p) != 0 {
		t.Errorf("the unknown command resolved to %v, want nothing", p)
	}
}

// TestGetAllRules_ListsThePluginRules pins the rules read.
//
// The memory carries the rule's text under `textMemory.content` — the
// member the schema knows — and the `plugin_rule` tag the Web UI's own
// rules list filters on.
func TestGetAllRules_ListsThePluginRules(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetAllRules", "{}")
	memories, ok := answer["memories"].([]any)
	if !ok {
		t.Fatalf("the answer carries no memories list: %#v", answer)
	}
	if len(memories) != 1 {
		t.Fatalf("got %d memories, want 1: %#v", len(memories), memories)
	}
	memory, _ := memories[0].(map[string]any)
	if memory["memoryId"] != "AGENTS" {
		t.Errorf("the rule's id is %v, want AGENTS", memory["memoryId"])
	}
	text, _ := memory["textMemory"].(map[string]any)
	if text["content"] != "Be careful.\n" {
		t.Errorf("the rule's text is %q, want %q", text["content"], "Be careful.\n")
	}
	metadata, _ := memory["metadata"].(map[string]any)
	tags, _ := metadata["tags"].([]any)
	if len(tags) != 1 || tags[0] != "plugin_rule" {
		t.Errorf("the rule's tags are %v, want [plugin_rule]", tags)
	}
	if memory["discoveryCategory"] != "DISCOVERY_CATEGORY_INSTALLED" {
		t.Errorf("the rule's category is %v, want DISCOVERY_CATEGORY_INSTALLED", memory["discoveryCategory"])
	}
}

// TestGetAgentScripts_ListsThePluginAgents pins the agent read.
//
// The Web UI's agent picker reads the customizations: the path, the
// agent's name, its description and whether it is global.
func TestGetAgentScripts_ListsThePluginAgents(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetAgentScripts", "{}")
	scripts, ok := answer["agentScripts"].([]any)
	if !ok {
		t.Fatalf("the answer carries no agentScripts list: %#v", answer)
	}
	customizations, ok := answer["agentCustomizations"].([]any)
	if !ok {
		t.Fatalf("the answer carries no agentCustomizations list: %#v", answer)
	}
	if len(scripts) != 1 || len(customizations) != 1 {
		t.Fatalf("got %d scripts and %d customizations, want 1 and 1: %#v %#v", len(scripts), len(customizations), scripts, customizations)
	}
	script, _ := scripts[0].(map[string]any)
	if script["name"] != "reviewer" {
		t.Errorf("the agent's name is %v, want reviewer", script["name"])
	}
	if script["pluginName"] != "demo-plugin" {
		t.Errorf("the agent's plugin is %v, want demo-plugin", script["pluginName"])
	}
	customization, _ := customizations[0].(map[string]any)
	if customization["isGlobal"] != true {
		t.Errorf("the agent's customization is not global: %#v", customization)
	}
	agent, _ := customization["agent"].(map[string]any)
	if agent["name"] != "reviewer" {
		t.Errorf("the customization's agent is %v, want reviewer", agent["name"])
	}
}

// TestGetCustomizationStates_ListsThePluginsAndSkills pins the loaded
// customization list.
//
// The list has one entry per loaded customization: the two installed
// plugins, the plugin's skill, its rule and its agent.
func TestGetCustomizationStates_ListsThePluginsAndSkills(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetCustomizationStates", "{}")
	states, _ := answer["states"].([]any)
	if len(states) != 5 {
		t.Fatalf("got %d states, want 5 (two plugins, a skill, a rule, an agent): %#v", len(states), states)
	}
	var sawPlugin, sawSkill, sawRule, sawAgent bool
	for _, s := range states {
		m := s.(map[string]any)
		switch m["type"] {
		case "REFRESH_CUSTOMIZATION_TYPE_PLUGIN":
			sawPlugin = true
			if m["scope"] != "SCOPE_GLOBAL" {
				t.Errorf("a plugin's scope is %v, want SCOPE_GLOBAL", m["scope"])
			}
			if m["status"] != "STATUS_ENABLED" {
				t.Errorf("a plugin's status is %v, want STATUS_ENABLED", m["status"])
			}
		case "REFRESH_CUSTOMIZATION_TYPE_SKILL":
			sawSkill = true
			if m["pluginName"] != "demo-plugin" {
				t.Errorf("the skill's plugin is %v, want demo-plugin", m["pluginName"])
			}
		case "REFRESH_CUSTOMIZATION_TYPE_RULE":
			sawRule = true
			if m["pluginName"] != "demo-plugin" {
				t.Errorf("the rule's plugin is %v, want demo-plugin", m["pluginName"])
			}
		case "REFRESH_CUSTOMIZATION_TYPE_AGENT":
			sawAgent = true
			if m["pluginName"] != "demo-plugin" {
				t.Errorf("the agent's plugin is %v, want demo-plugin", m["pluginName"])
			}
		}
	}
	if !sawPlugin || !sawSkill || !sawRule || !sawAgent {
		t.Errorf("the list is missing a plugin, a skill, a rule or an agent: %#v", states)
	}
}

// TestSearchMarketplace_FindsTheEntries pins the catalogue search.
func TestSearchMarketplace_FindsTheEntries(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "SearchMarketplace", `{"query":"demo"}`)
	customizations, _ := answer["customizations"].([]any)
	if len(customizations) != 1 {
		t.Fatalf("got %d customizations, want 1: %#v", len(customizations), customizations)
	}
	c := customizations[0].(map[string]any)
	if c["id"] != "demo-plugin" {
		t.Errorf("id = %v, want demo-plugin", c["id"])
	}
	if c["marketplace"] != "demo-market" {
		t.Errorf("marketplace = %v, want demo-market", c["marketplace"])
	}
	source, _ := c["source"].(map[string]any)
	archive, _ := source["archive"].(map[string]any)
	if archive["sha256"] != "abc" {
		t.Errorf("the archive's sha256 is %v, want abc", archive["sha256"])
	}

	groups, _ := answer["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1: %#v", len(groups), groups)
	}
	group := groups[0].(map[string]any)
	if group["id"] != "demo-market" {
		t.Errorf("the group's id is %v, want demo-market", group["id"])
	}
}

// TestGetMarketplaceCustomization_OneAndTheUnknownOne pins both.
func TestGetMarketplaceCustomization_OneAndTheUnknownOne(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetMarketplaceCustomization", `{"id":"demo-plugin"}`)
	c, _ := answer["customization"].(map[string]any)
	if c["displayName"] != "Demo Plugin" {
		t.Errorf("displayName = %v, want Demo Plugin", c["displayName"])
	}
	// The entry is installed here, so the answer carries the plugin.
	if _, ok := answer["plugin"].(map[string]any); !ok {
		t.Errorf("the answer carries no plugin: %#v", answer)
	}

	unknown := callPluginRPC(t, d, "GetMarketplaceCustomization", `{"id":"nope"}`)
	if len(unknown) != 0 {
		t.Errorf("the unknown answer is %v, want an empty one", unknown)
	}
}

// TestGetMarketplaceBundlesAndFeatured_AnswerEmpty pins the two
// cloud-curated surfaces: the local catalogue carries neither.
func TestGetMarketplaceBundlesAndFeatured_AnswerEmpty(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	bundles := callPluginRPC(t, d, "GetMarketplaceBundles", `{"ids":["demo-plugin"]}`)
	if list, _ := bundles["bundles"].([]any); len(list) != 0 {
		t.Errorf("the bundles are %v, want none", list)
	}
	if list, _ := bundles["customizations"].([]any); len(list) != 1 {
		t.Errorf("the customizations are %v, want the requested one", list)
	}

	featured := callPluginRPC(t, d, "GetMarketplaceFeatured", "{}")
	if list, _ := featured["featured"].([]any); len(list) != 0 {
		t.Errorf("the featured cards are %v, want none", list)
	}
}

// TestGetSkillMarketplaceLink_ResolvesThePlugin pins the link.
func TestGetSkillMarketplaceLink_ResolvesThePlugin(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	answer := callPluginRPC(t, d, "GetSkillMarketplaceLink",
		`{"skillPath":"/tmp/x/demo-plugin/skills/demo-skill/SKILL.md"}`)
	url, _ := answer["marketplaceUrl"].(string)
	if url == "" {
		t.Errorf("the answer carries no marketplace URL: %#v", answer)
	}
}

// pluginRPCCases is the list of plugin methods and the parameters each
// one is called with.
//
// Every method the clone answers is in here, so the two schema tests
// below cover the whole surface.
func pluginRPCCases() []struct {
	method string
	params string
} {
	return []struct {
		method string
		params string
	}{
		{"GetAllPlugins", "{}"},
		{"GetPlugin", `{"pluginId":"demo-plugin"}`},
		{"DeletePlugin", `{"pluginId":"nope"}`},
		{"ResolveCommandPlugin", `{"commandLine":"/demo-skill"}`},
		{"GetCustomizationStates", "{}"},
		{"SubscribeToPlugins", "{}"},
		{"GetAllSkills", "{}"},
		{"DeleteSkill", `{"skillId":"nope"}`},
		{"GetAllRules", "{}"},
		{"GetAgentScripts", "{}"},
		{"SearchMarketplace", `{"query":"demo"}`},
		{"SearchMarketplaceCustomizations", `{"query":"demo"}`},
		{"GetMarketplaceCustomization", `{"id":"demo-plugin"}`},
		{"GetMarketplaceBundles", `{"ids":["demo-plugin"]}`},
		{"GetMarketplaceFeatured", "{}"},
		{"GetSkillMarketplaceLink", `{"skillPath":"/tmp/x/demo-plugin/skills/demo-skill/SKILL.md"}`},
		{"GetAvailableCascadePlugins", "{}"},
		{"GetBuildWithGooglePlugins", "{}"},
		{"ListPluginAuthProviders", "{}"},
		{"InstallCustomization", `{"id":"demo-plugin"}`},
		{"ListRemotePlugins", "{}"},
		{"DisconnectRemotePlugin", `{"pluginId":"mcp-bridge"}`},
		{"DownloadBuildWithGooglePlugin", `{"pluginId":"x"}`},
		{"StartPluginAuthSignIn", `{"plugin":"demo-plugin","provider":"p"}`},
		{"SignInPluginAuthProvider", `{"plugin":"demo-plugin","provider":"p"}`},
		{"SignOutPluginAuthProvider", `{"plugin":"demo-plugin","provider":"p"}`},
		{"SetDefaultPluginAuthAccount", `{"plugin":"demo-plugin","provider":"p"}`},
		{"SetPluginAuthAccountLabel", `{"plugin":"demo-plugin","provider":"p"}`},
	}
}

// TestThePluginAnswersParseWithTheReferenceSchema pins the thing the
// Web UI actually breaks on: a *known* member with the wrong JSON type
// fails the decode.
//
// 【反编译】 the descriptor is the reference's own — the
// FileDescriptorProto of third_party/jetski/language_server_pb/
// language_server.proto, read out of the agy 1.4.8 binary and
// committed at lsproto/descriptor/language_server.protoset.
func TestThePluginAnswersParseWithTheReferenceSchema(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	for _, c := range pluginRPCCases() {
		answer := callPluginRPC(t, d, c.method, c.params)
		body, err := json.Marshal(answer)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.method, err)
		}
		encoded, err := codec.EncodeResponse(c.method, body)
		if err != nil {
			t.Errorf("%s: EncodeResponse: %v", c.method, err)
			continue
		}
		if _, err := codec.DecodeResponse(c.method, encoded); err != nil {
			t.Errorf("%s: DecodeResponse: %v", c.method, err)
		}
	}
}

// TestThePluginAnswersOnlyUseMembersTheSchemaKnows pins the quieter
// half of the same failure.
//
// A member the schema does **not** know is dropped by the Web UI's
// decoder without an error — the page renders, and the member's data
// is simply not there. So a misspelled member is invisible: the
// plugin panel shows a rule with no text, and nothing says why.
//
// This walks every answer against the descriptor and reports a member
// the schema does not know, at every level.
//
// It found one: the plugin's rules answered
// `{"textMemory":{"text":…}}`, and the schema's CortexMemoryText has
// a single member, `content` (1). The rule's text was being dropped
// on the floor.
func TestThePluginAnswersOnlyUseMembersTheSchemaKnows(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	for _, c := range pluginRPCCases() {
		md, ok := codec.OutputMessage(c.method)
		if !ok {
			t.Errorf("%s: the schema carries no answer for it", c.method)
			continue
		}
		answer := callPluginRPC(t, d, c.method, c.params)
		t.Run(c.method, func(t *testing.T) {
			assertMembersKnown(t, md, answer, c.method)
		})
	}
}

// assertMembersKnown walks a proto-JSON answer against its descriptor
// and fails on a member the schema does not know, and on an enum value
// the schema does not know.
//
// A member is known when the descriptor has a field with that JSON
// name (or with that protobuf name — proto-JSON accepts both).
//
// An enum value is known when the enum names it, or when it is the
// number of one of the enum's values.
//
// A map field's keys are the map's own, not the schema's, so the walk
// descends into the map's *values*.
func assertMembersKnown(t *testing.T, md protoreflect.MessageDescriptor, obj map[string]any, path string) {
	t.Helper()
	for key, val := range obj {
		fd := findSchemaField(md, key)
		if fd == nil {
			t.Errorf("%s: the schema does not know the member %q", path, key)
			continue
		}
		if fd.Kind() == protoreflect.EnumKind {
			// A repeated enum is a list of names, not a name: the
			// walk has to descend into it (the app state's
			// postOnboarding.completedSteps is one).
			if fd.IsList() {
				list, ok := val.([]any)
				if !ok {
					continue
				}
				for _, item := range list {
					assertEnumValueKnown(t, fd, item, path+"."+key+"[]")
				}
				continue
			}
			assertEnumValueKnown(t, fd, val, path+"."+key)
			continue
		}
		if fd.Kind() != protoreflect.MessageKind {
			continue
		}
		if fd.IsMap() {
			values, ok := val.(map[string]any)
			if !ok {
				continue
			}
			for _, item := range values {
				if m, ok := item.(map[string]any); ok {
					assertMembersKnown(t, fd.MapValue().Message(), m, path+"."+key+"{}")
				}
			}
			continue
		}
		if fd.IsList() {
			list, ok := val.([]any)
			if !ok {
				continue
			}
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					assertMembersKnown(t, fd.Message(), m, path+"."+key+"[]")
				}
			}
			continue
		}
		if m, ok := val.(map[string]any); ok {
			assertMembersKnown(t, fd.Message(), m, path+"."+key)
		}
	}
}

// assertEnumValueKnown fails on an enum value the schema does not
// name.
//
// An unknown enum name is *worse* than an unknown member: protojson
// rejects the whole message that carries it, so the member's **list**
// is dropped with it. A customization list whose entries name their
// type "PLUGIN" instead of
// "REFRESH_CUSTOMIZATION_TYPE_PLUGIN" arrives at the Web UI as an
// empty list — with no error anywhere.
func assertEnumValueKnown(t *testing.T, fd protoreflect.FieldDescriptor, val any, path string) {
	t.Helper()
	switch v := val.(type) {
	case string:
		if fd.Enum().Values().ByName(protoreflect.Name(v)) == nil {
			t.Errorf("%s: the enum does not know the value %q", path, v)
		}
	case float64:
		if fd.Enum().Values().ByNumber(protoreflect.EnumNumber(int32(v))) == nil {
			t.Errorf("%s: the enum does not know the number %v", path, v)
		}
	case nil:
	default:
		t.Errorf("%s: the enum member is a %T, want a name or a number", path, val)
	}
}

// findSchemaField returns the descriptor's field with that JSON name,
// or its protobuf name.
func findSchemaField(md protoreflect.MessageDescriptor, key string) protoreflect.FieldDescriptor {
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.JSONName() == key || string(fd.Name()) == key {
			return fd
		}
	}
	return nil
}

// TestSubscribeToPlugins_EmitsTheInstalledPlugins pins the stream's
// first frame.
func TestSubscribeToPlugins_EmitsTheInstalledPlugins(t *testing.T) {
	seedPluginTree(t)
	d := pluginTestDispatcher(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var frames []map[string]any
	handled, err := d.streamSubscribeToPlugins(ctx, "{}", func(raw json.RawMessage) error {
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil {
			return err
		}
		frames = append(frames, frame)
		cancel()
		return nil
	})
	if !handled {
		t.Fatal("the stream was not handled")
	}
	if err != nil {
		t.Fatalf("the stream failed: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	plugins, _ := frames[0]["plugins"].([]any)
	if len(plugins) != 2 {
		t.Errorf("the frame carries %d plugins, want 2", len(plugins))
	}
}
