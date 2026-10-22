// Command wagyu is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
//
// Cycle 121 rename: the directory was `cmd/antigravity`; it is `cmd/wagyu`
// now, so the command no longer collides with the reference binary's own name.
// The Go package is `main` and nothing imports it, so only the directory and
// the references to it changed.
package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/daemon"
)

// printModeSilenceLogger is the destination for the recovered debug-log lines.
var printModeSilenceLogger = log.New(io.Discard, "", 0)

// glamourTTYCheck decides whether the print-mode text-format chunk handler routes
// through glamour-on-Done (TTY) or the historical live-stream path (piped).
var glamourTTYCheck = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// glamourRenderWidth resolves the print-mode glamour render width when the write target is an io.Writer.
var glamourRenderWidth = func(out io.Writer) int {
	type fdWriter interface {
		Fd() uintptr
	}
	if w, ok := out.(fdWriter); ok {
		fd := int(w.Fd())
		if term.IsTerminal(fd) {
			if cols, _, err := term.GetSize(fd); err == nil && cols > 0 {
				return cols
			}
		}
	}
	return 0
}

var recoveredTools = []string{
	"run_command", "create_file", "edit_file", "read_file", "delete_file",
	"list_dir", "search_files", "grep_search", "codesearch", "web_search",
	"web_fetch", "git_status", "git_diff", "git_commit", "git_push",
	"git_pull", "git_checkout", "git_branch", "git_merge", "git_log",
	"todo_write", "todo_read", "spawn_agent", "kill_agent", "list_agents",
	"mcp_call", "mcp_list_tools", "ask_user", "show_diff", "apply_patch",
	"run_tests", "lint_code",
}

var recoveredServices = []string{
	"CortexService/GenerateChat", "CortexService/StreamGenerateChat",
	"CortexService/FetchConfig", "CortexService/FetchLicenses",
	"CortexService/RefreshToken", "LanguageServerService/SelectLicense",
	"LanguageServerService/SelfAssignLicense", "ToolsService/Invoke",
	"ToolsService/ListTools", "WorkspaceService/Init",
	"WorkspaceService/Status", "ProjectService/Get",
	"ProjectService/List", "OAuthService/StartDeviceFlow",
	"OAuthService/PollDeviceCode", "OAuthService/RefreshToken",
	"RemoteControlService/Start", "RemoteControlService/Status",
	"RemoteControlService/Stop", "PluginService/Install",
	"PluginService/Uninstall", "MCPService/AddServer",
}

const cliVersion = cli.ReferenceVersion

func init() {
	isWorkspaceTrusted = func(dir string) (bool, error) {
		return auth.IsWorkspaceTrusted(dir)
	}
	addTrustedWorkspace = func(dir string) error {
		return auth.AddTrustedWorkspace(dir)
	}
	lipgloss.Writer.Profile = resolveColorProfile(os.Environ())
}

func main() {
	daemon.InstallSignalHandlers()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		daemon.Global().Shutdown(ctx)
	}()

	_, _ = auth.WriteCompatDefaults()
	_ = cli.RunPluginCleanupOnStartup(auth.AntigravityConfigHome())

	p := setupCLIParser()
	if err := p.Execute(); err != nil {
		os.Exit(p.ExitCode())
	}
}

func setupCLIParser() *cli.Parser {
	p := cli.NewParser(
		cli.StandardFlags(),
		cli.StandardSubcommands(),
		filepath.Base(os.Args[0]),
		"Recovered "+branding.Current().CLIName+" (1:1 parse parity with v4 binary)",
		"pigasus is a 1:1 parse-parity shell of Google Antigravity CLI v1.3.3.\n"+
			"Top-level flags, subcommands and sub-subcommands match the v4 binary\n"+
			"so wrapper scripts and operator tooling are portable across both.\n"+
			"Runtime behavior is recovered from Ghidra 12.1.4 + REA 6.4.0 seeds.",
		cli.ExtraFlags()...,
	)

	p.Root().Version = cliVersion + "\n"
	p.Root().RootRun = func(args []string) error {
		return runRoot(args, p)
	}

	cli.SetChatBridge(runLiveTUI)
	cli.RegisterForegroundHandler(runRemoteControlForeground)
	cli.RegisterServeHandler(runRemoteControlServe)
	cli.RegisterLogoHandler(runLogoSubcommand)

	cli.RegisterAgentsLister(func() []string {
		agents := agent.Discover()
		names := make([]string, 0, len(agents))
		for _, a := range agents {
			names = append(names, a.Name)
		}
		return names
	})
	return p
}

// runRoot handles the top-level commands and flags.
func runRoot(args []string, p *cli.Parser) error {
	out := p.Root().Out
	fs := p.Root().FlagSet()
	if fs == nil {
		return nil
	}
	if done, err := checkRootShortCircuits(fs, p, out); done || err != nil {
		return err
	}
	printMode := resolvePrintMode(fs)
	if err := validateRootFlags(fs, printMode); err != nil {
		return err
	}
	if printMode {
		return dispatchPrintMode(fs, out)
	}
	if flagWasSet(fs, "prompt-interactive") || flagWasSet(fs, "i") {
		return dispatchInteractiveMode(fs, out)
	}
	if resolveBoolAlias(fs, "continue", "c") {
		return dispatchInteractiveMode(fs, out)
	}
	return dispatchDefaultRoot(args, fs, out)
}
