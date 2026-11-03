// Package cliapp — tui_attach.go: the `pigasus tui` subcommand.
//
// The interactive session is the client half of the client/server split.
// Without --server it is the ordinary local session; with it, the turn
// runs on the server — on that machine's workspace, tools and
// credentials — and the session renders the server's frames here.
//
// The client owns the conversation: the transcript travels with every
// prompt, so the server replays what the client sends and never has to
// remember a conversation of its own.
package cliapp

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
)

// attachTimeout is how long the client waits for the handshake.
const attachTimeout = 20 * time.Second

// normalizeServerAddr maps the address the user typed onto a websocket
// URL.
//
// A bare "host:port" is the common way to name a server, and the
// websocket scheme is the only one the protocol speaks.
func normalizeServerAddr(addr string) string {
	a := strings.TrimSpace(addr)
	if a == "" {
		return ""
	}
	if !strings.Contains(a, "://") {
		return "ws://" + a
	}
	return a
}

// runTUIHandler is the `pigasus tui` handler.
//
// The session's own flags are the root command's flags, so they are
// parsed here — the subcommand owns only --server and --token.
func runTUIHandler(j *cli.JetskiCLI, args cli.TUIArgs) error {
	fs := cli.NewFlagSet("tui", append(cli.StandardFlags(), cli.ExtraFlags()...))
	if j != nil && j.ErrOut != nil {
		fs.SetOutput(j.ErrOut)
	} else {
		fs.SetOutput(os.Stderr)
	}
	if err := fs.Parse(args.Args); err != nil {
		return err
	}
	out := io.Writer(os.Stdout)
	if j != nil && j.Out != nil {
		out = j.Out
	}
	if strings.TrimSpace(args.ACPCmd) != "" {
		return runAttachedTUI_ACPCmd(out, fs, args)
	}
	if strings.TrimSpace(args.ACPManifest) != "" {
		return runAttachedTUI_ACPManifest(out, fs, args)
	}
	backend := strings.ToLower(strings.TrimSpace(args.Backend))
	if backend == "pigasus" {
		return runAttachedTUI_Pigasus(out, fs, args)
	}
	if backend == "codex" {
		return runAttachedTUI_Codex(out, fs, args)
	}
	if backend == "claude" {
		return runAttachedTUI_Claude(out, fs, args)
	}
	if backend == "agy" {
		return runAttachedTUI_Agy(out, fs, args)
	}
	if backend == "codex-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "codex-acp", args)
	}
	if backend == "claude-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "claude-acp", args)
	}
	if backend == "agy-acp" || backend == "agy-acp-server" || backend == "antigravity-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "agy-acp-server", args)
	}
	if backend != "" && backend != "local" {
		return runAttachedTUI_ACPAgent(out, fs, backend, args)
	}
	if strings.TrimSpace(args.Server) == "" {
		return runLiveTUI(out, fs)
	}
	return runAttachedTUI(out, fs, args)
}

// runAttachedTUI runs the interactive session against a remote server.
func runAttachedTUI(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	addr := normalizeServerAddr(args.Server)
	if addr == "" {
		return fmt.Errorf("tui: no server address")
	}

	proto := strings.ToLower(strings.TrimSpace(args.Protocol))
	if proto == "" || proto == "acp" || strings.HasSuffix(strings.TrimRight(addr, "/"), "/acp") {
		return runAttachedTUI_ACP(w, fs, addr, args)
	}

	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	client, err := agentlink.Dial(ctx, addr, agentlink.DialOptions{Token: strings.TrimSpace(args.Token)})
	if err != nil {
		return fmt.Errorf("tui: cannot reach the server at %s: %w", addr, err)
	}
	defer client.Close()

	// The handshake names the server's workspace and the model it
	// resolved, so the session shows the server's own.
	if err := client.Send(ctx, agentlink.Frame{Kind: agentlink.KindHello}); err != nil {
		return fmt.Errorf("tui: cannot greet the server: %w", err)
	}
	f, err := client.Next(ctx)
	if err != nil {
		return fmt.Errorf("tui: the server did not answer the greeting: %w", err)
	}
	if f.Kind != agentlink.KindHello {
		return fmt.Errorf("tui: the server answered the greeting with %q", f.Kind)
	}

	modelName := strings.TrimSpace(f.Model)
	if modelName == "" {
		modelName = defaultModel
	}
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}
	// The turn runs on the server: the local engine would run it here.
	m.engine = agentturn.NewRemote(client)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACP runs the interactive session against a remote ACP server.
func runAttachedTUI_ACP(w io.Writer, fs *flag.FlagSet, addr string, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.DialACPRemote(ctx, addr, strings.TrimSpace(args.Token), cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui (acp): cannot reach the server at %s: %w", addr, err)
	}
	defer remote.Close()

	modelName := defaultModel
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	// The turn runs on the ACP server.
	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_Codex runs the interactive session against a local or remote Codex app-server.
func runAttachedTUI_Codex(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	cliModel := cliflags.StringValue(fs, "model")
	remote, err := agentturn.SpawnCodexRemote(ctx, "", cwd, cliModel)
	if err != nil {
		return fmt.Errorf("tui (codex): failed to start codex app-server: %w", err)
	}
	defer remote.Close()

	modelName := cliModel
	if modelName == "" {
		modelName = remote.DefaultModel()
	}
	if modelName == "" {
		modelName = "gemini-3.8-flash-high"
	}
	if canonical := modelcatalog.CanonicalModelLabel(modelName); canonical != "" {
		modelName = canonical
	}
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	// The turn runs on Codex app-server.
	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_Claude runs the interactive session against Claude Code via stdio stream-json.
func runAttachedTUI_Claude(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnClaudeRemote(ctx, "", cwd, "", "")
	if err != nil {
		return fmt.Errorf("tui (claude): failed to start claude process: %w", err)
	}
	defer remote.Close()

	modelName := "claude"
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	// The turn runs on Claude Code.
	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_Pigasus runs the interactive session against a pigasus subprocess via stdio stream-json.
func runAttachedTUI_Pigasus(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	modelFlag := cliflags.StringValue(fs, "model")
	effortFlag := cliflags.StringValue(fs, "effort")
	remote, err := agentturn.SpawnPigasusRemote(ctx, "", cwd, modelFlag, effortFlag, "")
	if err != nil {
		return fmt.Errorf("tui (pigasus): failed to start pigasus process: %w", err)
	}
	defer remote.Close()

	modelName := "pigasus"
	if modelFlag != "" {
		modelName = modelFlag
	}
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	// The turn runs on pigasus subprocess over stream-json.
	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_Agy runs the interactive session against Google agy via stdio stream-json.
func runAttachedTUI_Agy(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	modelFlag := cliflags.StringValue(fs, "model")
	effortFlag := cliflags.StringValue(fs, "effort")
	remote, err := agentturn.SpawnAgyRemote(ctx, "", cwd, modelFlag, effortFlag, "")
	if err != nil {
		return fmt.Errorf("tui (agy): failed to start agy process: %w", err)
	}
	defer remote.Close()

	modelName := "agy"
	if modelFlag != "" {
		modelName = modelFlag
	}
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	// The turn runs on agy.
	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACPAgent runs the interactive session against an ACP agent (built-in or manifest).
func runAttachedTUI_ACPAgent(w io.Writer, fs *flag.FlagSet, agentID string, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnACPAgent(ctx, agentID, nil, cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui (%s): failed to spawn ACP agent: %w", agentID, err)
	}
	defer remote.Close()

	modelName := agentID
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACPCmd runs the interactive session against a custom ACP command over stdio.
func runAttachedTUI_ACPCmd(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cmdParts := strings.Fields(strings.TrimSpace(args.ACPCmd))
	if len(cmdParts) == 0 {
		return fmt.Errorf("tui: empty --acp-cmd")
	}

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnACPRemote(ctx, cmdParts[0], cmdParts[1:], cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui (acp): failed to spawn ACP command %q: %w", args.ACPCmd, err)
	}
	defer remote.Close()

	modelName := filepath.Base(cmdParts[0])
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_ACPManifest runs the interactive session against an ACP agent described by an agent.json manifest.
func runAttachedTUI_ACPManifest(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	remote, err := agentturn.SpawnACPAgent(ctx, strings.TrimSpace(args.ACPManifest), nil, cwd, args.ACPVersion)
	if err != nil {
		return fmt.Errorf("tui: failed to spawn ACP manifest %q: %w", args.ACPManifest, err)
	}
	defer remote.Close()

	modelName := "acp-agent"
	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRunMode(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.pluginWatcher.Close()
	}

	m.engine = remote
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}
