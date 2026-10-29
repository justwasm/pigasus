// Static v1.3.5-binary specs consumed by NewParser / ParseForTest.
//
// Cycle 38: switched from "v1.3.4 / 31 flag / 10 subcommand" to
// v1.3.5 actual --help (21 flag / 12 subcommand). The cycle 33
// inventory was a guess extrapolated from the ghidra symbol table;
// the actual --help list is much smaller.
//
// We re-extracted the spec from `/workspace/agy/install/antigravity --help`
// on 2026-10-14 (md5 b7f7ed7c66bc835ee21bd9b7304b8f74, version "1.3.5").
//
// Cycle 41: the 12 v1.3.5-visible subcommands stay 1:1 in --help
// (the v1.3.5 binary's 39-line help output is the parse-parity
// contract). We add 10 hidden subcommands (version, login, logout,
// auth, chat, acp, http, hooks, migrate, stats) that wire to the
// runXxx handlers in dispatch.go. The hidden flag keeps --help at
// 39 lines; the new subcommands are invokable as
// `antigravity <name>` and route to real internal/ handlers.
package cli

import (
	"context"
	"io"
	"os"
)

// StandardSubcommands returns the 12 subcommands the v1.3.5 binary lists
// in `agy --help`:
//
//	agent, agents, changelog, help, install, mcp, mic-serve, models,
//	plugin, plugins, remote-control, update
//
// `agent` and `agents` share one implementation (both print the same
// "Usage: <prog> agent [flags]" block), and `plugins` is an alias of
// `plugin`.
func StandardSubcommands() []SubcommandSpec {
	pluginSubs := standardPluginSubs()
	mcpSubs := standardMCPSubs()
	rcSubs := standardRCSubs()

	var subs []SubcommandSpec
	subs = append(subs, pluginAndMCPSubcommands(pluginSubs, mcpSubs)...)
	subs = append(subs, rcAndMediaSubcommands(rcSubs)...)
	subs = append(subs, generalSubcommands()...)
	subs = append(subs, hiddenUtilitySubcommands()...)
	subs = append(subs, providerSubcommand(), modelSubcommand(), accountsSubcommand())
	subs = append(subs, halloweenSubcommand())
	return subs
}

// halloweenSubcommand is the clone's own command for printing the
// seasonal banner art. The reference has no such command — the art only
// ever appears in its startup banner — so it is a pigasus addition, next
// to the other openagy commands.
func halloweenSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "halloween",
		Short:  "Print the seasonal banner art",
		HelpFn: usageHalloween,
		RunJ:   runHalloween,
	}
}

func standardPluginSubs() []SubSubSpec {
	return []SubSubSpec{
		{Name: "list", Short: "List installed plugins", RunJ: runPluginList},
		{Name: "import", Short: "Import plugins from gemini or claude", RunJ: runPluginImport},
		{Name: "install", Short: "Install a plugin", RunJ: runPluginInstall},
		{Name: "uninstall", Short: "Uninstall a plugin", RunJ: runPluginUninstall},
		{Name: "enable", Short: "Enable a plugin", RunJ: runPluginToggle("enable")},
		{Name: "disable", Short: "Disable a plugin", RunJ: runPluginToggle("disable")},
		{Name: "validate", Short: "Validate a plugin", RunJ: runPluginValidate},
		{Name: "link", Short: "Generate link to a marketplace", RunJ: runPluginLink},
		{Name: "help", Short: "Show plugin command help", RunJ: runPluginHelp},
	}
}

func standardMCPSubs() []SubSubSpec {
	return []SubSubSpec{
		{Name: "add", Short: "Add or update an MCP server configuration",
			RunJ: runMCPAdd, HelpFn: usageMCPAdd},
		{Name: "remove", Short: "Remove an MCP server configuration",
			RunJ: runMCPRemove, HelpFn: func(n string) string {
				return usageMCPSimple(n, "remove", "Remove an MCP server configuration", "name")
			}},
		{Name: "list", Short: "List all configured MCP servers",
			RunJ: runMCPList, HelpFn: func(n string) string {
				return usageMCPSimple(n, "list", "List all configured MCP servers", "")
			}},
		{Name: "enable", Short: "Enable an MCP server",
			RunJ: runMCPSetEnabled(true), HelpFn: func(n string) string {
				return usageMCPSimple(n, "enable", "Enable an MCP server", "name")
			}},
		{Name: "disable", Short: "Disable an MCP server",
			RunJ: runMCPSetEnabled(false), HelpFn: func(n string) string {
				return usageMCPSimple(n, "disable", "Disable an MCP server", "name")
			}},
	}
}

func standardRCSubs() []SubSubSpec {
	return []SubSubSpec{
		{Name: "start", Short: "Register and start the remote-control daemon",
			RunJ: runRCStart, HelpFn: usageRCStart},
		{Name: "serve", Short: "Run the remote-control daemon in the foreground (OAuth login inline)",
			RunJ: runRCServe, HelpFn: usageRCServe},
		// `run` is the clone's own, platform-independent spelling of
		// `serve`: the same daemon, in the foreground, on every
		// platform. It is hidden — the reference's `remote-control
		// --help` lists start, status and stop only, and that block
		// is the reference's, byte for byte.
		{Name: "run", Short: "Run the remote-control daemon in the foreground (same on every platform)",
			RunJ: runRCRun, HelpFn: usageRCRun},
		{Name: "status", Short: "Show the daemon's status",
			RunJ: runRCStatus, HelpFn: usageRCStatus},
		{Name: "stop", Short: "Stop the daemon and unregister it",
			RunJ: runRCStop, HelpFn: usageRCStop},
	}
}

func pluginAndMCPSubcommands(pluginSubs, mcpSubs []SubSubSpec) []SubcommandSpec {
	return []SubcommandSpec{
		{
			Name:            "plugin",
			Short:           "Manage plugins (install, uninstall, list, enable, disable)",
			Subs:            pluginSubs,
			HelpFn:          usagePlugin,
			UnknownSubError: "Error: unknown command: %s",
			RunJ:            runPluginParent,
		},
		{
			Name:            "plugins",
			Aliases:         []string{"plugin"},
			Short:           "Alias for plugin",
			Subs:            pluginSubs,
			HelpFn:          usagePlugin,
			UnknownSubError: "Error: unknown command: %s",
			RunJ:            runPluginParent,
		},
		{
			Name:   "mcp",
			Short:  "Manage MCP servers (add, remove, list, enable, disable)",
			Subs:   mcpSubs,
			HelpFn: usageMCP,
			RunJ:   runMCPParent,
		},
		{
			Name:   "install",
			Short:  "Configure environment paths and shell settings",
			HelpFn: usageInstall,
			RunJ: func(j *JetskiCLI, args []string) error {
				return dispatchRunXxx("install", j.Out, j.ErrOut, context.Background(), nil, args)
			},
		},
	}
}

func rcAndMediaSubcommands(rcSubs []SubSubSpec) []SubcommandSpec {
	return []SubcommandSpec{
		{
			Name:   "mic-serve",
			Short:  "Serve this machine's microphone to a CLI on another host",
			HelpFn: usageMicServe,
			RunJ:   runMicServe,
		},
		{
			Name:   "remote-control",
			Short:  "Manage the remote-control background daemon (start, status, stop)",
			Subs:   rcSubs,
			HelpFn: usageRemoteControl,
			RunJ:   runRCParent,
		},
		{
			Name:   "remote-control-foreground",
			Short:  "Foreground subcommand the persistent daemon invokes (hidden)",
			Hidden: true,
			RunJ:   runRCForegroundPlaceholder,
		},
	}
}

func generalSubcommands() []SubcommandSpec {
	return []SubcommandSpec{
		{
			Name:   "agent",
			Short:  "List available agents",
			HelpFn: usageAgent,
			RunJ:   runAgents,
		},
		{
			Name:    "agents",
			Aliases: []string{"agent"},
			Short:   "List available agents",
			HelpFn:  usageAgent,
			RunJ:    runAgents,
		},
		{
			Name:   "changelog",
			Short:  "Show changelog and release notes",
			HelpFn: usageChangelog,
			RunJ:   runChangelog,
		},
		{
			Name:     "update",
			Short:    "Update CLI",
			Help:     "Usage of update:\n",
			HelpExit: 2,
			Run:      RunUpdate,
		},
		{
			Name:   "models",
			Short:  "List available models",
			HelpFn: usageModels,
			RunJ:   runModelsParity,
		},
		{
			Name:  "help",
			Short: "Show help for subcommands",
			RunJ:  runHelpCommand,
		},
	}
}

func hiddenUtilitySubcommands() []SubcommandSpec {
	wrapRunXxx := func(name string) func(w io.Writer, args []string) error {
		return func(w io.Writer, args []string) error {
			return dispatchRunXxx(name, w, os.Stderr, context.Background(), nil, args)
		}
	}

	return []SubcommandSpec{
		{Name: "login", Short: "Start the OAuth device-code login flow", Run: wrapRunXxx("login"), Hidden: true},
		{Name: "logout", Short: "Clear the stored OAuth token", Run: wrapRunXxx("logout"), Hidden: true},
		{Name: "auth", Short: "Set up the mTLS bypass proxy", Run: wrapRunXxx("auth"), Hidden: true},
		{Name: "chat", Short: "Launch the recovered TUI chat", Run: wrapRunXxx("chat"), Hidden: true},
		{Name: "acp", Short: "Start the Agent Client Protocol listener", Run: wrapRunXxx("acp"), Hidden: true},
		{Name: "http", Short: "Start the recovered HTTP server", Run: wrapRunXxx("http"), Hidden: true},
		{Name: "hooks", Short: "List configured lifecycle hooks", Run: wrapRunXxx("hooks"), Hidden: true},
		{Name: "migrate", Short: "Migrate legacy config to the v4 layout", Run: wrapRunXxx("migrate"), Hidden: true},
		{Name: "stats", Short: "Show on-disk session statistics", Run: wrapRunXxx("stats"), Hidden: true},
		{Name: "logo", Short: "Print the startup banner logo (hidden)", HelpFn: UsageLogo, Run: runLogoHandler, Hidden: true},
	}
}

func providerSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:    "provider",
		Short:   "Manage the configured model providers",
		Aliases: []string{"providers"},
		Subs: []SubSubSpec{
			{Name: "list", Short: "List the configured providers", RunJ: runProviderList, HelpFn: func(n string) string {
				return "Usage: " + n + " provider list\n"
			}},
			{Name: "show", Short: "Show one provider in detail", RunJ: runProviderShow, HelpFn: usageProviderShow},
			{Name: "add", Short: "Create or update a provider", RunJ: runProviderAdd, HelpFn: usageProviderAdd},
			{Name: "remove", Short: "Remove a provider and its models", RunJ: runProviderRemove, HelpFn: usageProviderRemove},
			{Name: "test", Short: "Probe the provider's endpoint and credential", RunJ: runProviderTest, HelpFn: usageProviderTest},
		},
		RunJ: runProviderParent,
	}
}

func modelSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:  "model",
		Short: "Manage the configured models",
		Subs: []SubSubSpec{
			{Name: "list", Short: "List the configured models", RunJ: runModelList, HelpFn: func(n string) string {
				return "Usage: " + n + " model list [--provider <provider>]\n"
			}},
			{Name: "show", Short: "Show one model in detail", RunJ: runModelShow, HelpFn: usageModelShow},
			{Name: "add", Short: "Create or update a model", RunJ: runModelAdd, HelpFn: usageModelAdd},
			{Name: "remove", Short: "Remove a model", RunJ: runModelRemove, HelpFn: usageModelRemove},
		},
		RunJ: runModelParent,
	}
}

func accountsSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:  "accounts",
		Short: "Manage the signed-in Google accounts",
		Subs: []SubSubSpec{
			{Name: "list", Short: "List the known accounts", RunJ: runAccountsList, HelpFn: func(n string) string {
				return "Usage: " + n + " accounts list\n"
			}},
			{Name: "switch", Short: "Make one account the active one", RunJ: runAccountsSwitch, HelpFn: usageAccountsSwitch},
			{Name: "remove", Short: "Forget one account", RunJ: runAccountsRemove, HelpFn: usageAccountsRemove},
		},
		RunJ: runAccountsParent,
	}
}
