package tui

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
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
	if driver, ok := agentturn.Lookup(backend); ok {
		return runAttachedTUI_Driver(out, fs, driver, args)
	}
	if backend == "pigasus-acp" || backend == "pig-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "pigasus-acp", args)
	}
	if backend == "codex-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "codex-acp", args)
	}
	if backend == "claude-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "claude-acp", args)
	}
	if backend == "agy-acp" || backend == "agy-acp-server" || backend == "antigravity-acp" {
		return runAttachedTUI_ACPAgent(out, fs, "agy-acp", args)
	}
	if backend != "" && backend != "local" {
		return runAttachedTUI_ACPAgent(out, fs, backend, args)
	}
	if strings.TrimSpace(args.Server) == "" {
		return runLiveTUI(out, fs)
	}
	return runAttachedTUI(out, fs, args)
}

// resolveAttachProtocol determines whether the client should speak AHP, ACP, or AgentLink.
func resolveAttachProtocol(addr, proto string) string {
	p := strings.ToLower(strings.TrimSpace(proto))
	if p == "ahp" {
		return "ahp"
	}

	cleanAddr := strings.TrimRight(strings.TrimSpace(addr), "/")
	if u, err := url.Parse(cleanAddr); err == nil && u.Path != "" {
		path := strings.TrimRight(u.Path, "/")
		if strings.HasSuffix(path, "/ahp") {
			return "ahp"
		}
		if strings.HasSuffix(path, "/acp") {
			return "acp"
		}
	}
	if strings.HasSuffix(cleanAddr, "/ahp") {
		return "ahp"
	}
	if strings.HasSuffix(cleanAddr, "/acp") {
		return "acp"
	}

	if p == "agentlink" {
		return "agentlink"
	}
	if p == "" || p == "acp" {
		return "acp"
	}
	return p
}

// runAttachedTUI runs the interactive session against a remote server.
func runAttachedTUI(w io.Writer, fs *flag.FlagSet, args cli.TUIArgs) error {
	addr := normalizeServerAddr(args.Server)
	if addr == "" {
		return fmt.Errorf("tui: no server address")
	}

	switch resolveAttachProtocol(addr, args.Protocol) {
	case "ahp":
		return runAttachedTUI_AHP(w, fs, addr, args)
	case "acp":
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
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}
	// The turn runs on the server: the local engine would run it here.
	m.turn.engine = agentturn.NewRemote(client)
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_AHP runs the interactive session against a remote AHP server.
func runAttachedTUI_AHP(w io.Writer, fs *flag.FlagSet, addr string, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	cliModel := cliflags.StringValue(fs, "model")
	remote, err := agentturn.DialAHPRemote(ctx, addr, strings.TrimSpace(args.Token), cwd, agentturn.AHPRemoteOptions{
		SessionURI: strings.TrimSpace(args.Session),
		Model:      cliModel,
	})
	if err != nil {
		return fmt.Errorf("tui (ahp): cannot reach the server at %s: %w", addr, err)
	}
	defer remote.Close()
	modelName := cliModel
	if modelName == "" {
		if dm, ok := any(remote).(agentturn.DefaultModeler); ok {
			modelName = dm.DefaultModel()
		}
	}
	if modelName == "" {
		modelName = defaultModel
	}
	if canonical := modelcatalog.CanonicalModelLabel(modelName); canonical != "" {
		modelName = canonical
	}

	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	// The turn runs on the AHP server.
	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
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
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	// The turn runs on the ACP server.
	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}

// runAttachedTUI_Driver runs the interactive session against any registered backend driver.
func runAttachedTUI_Driver(w io.Writer, fs *flag.FlagSet, driver agentturn.Driver, args cli.TUIArgs) error {
	ctx, cancel := context.WithTimeout(context.Background(), attachTimeout)
	defer cancel()

	cwd, _ := os.Getwd()
	cliModel := cliflags.StringValue(fs, "model")
	effortFlag := cliflags.StringValue(fs, "effort")

	opts := agentturn.SpawnOptions{
		CWD:    cwd,
		Model:  cliModel,
		Effort: effortFlag,
		Args:   args.Args,
	}

	remote, err := driver.Spawn(ctx, opts)
	if err != nil {
		return fmt.Errorf("tui (%s): failed to initialize engine: %w", driver.Name, err)
	}
	defer remote.Close()

	modelName := cliModel
	if modelName == "" {
		if dm, ok := remote.(agentturn.DefaultModeler); ok {
			modelName = dm.DefaultModel()
		}
	}
	if modelName == "" {
		modelName = driver.DefaultModel
	}
	if modelName == "" {
		modelName = "gemini-3.8-flash-high"
	}
	if canonical := modelcatalog.CanonicalModelLabel(modelName); canonical != "" {
		modelName = canonical
	}

	altScreen := resolveAltScreen(fs)
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
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
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
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
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
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
	m, watchCancel := initTUIRootModel(fs, nil, modelName, "", altScreen)
	defer watchCancel()
	if m.viewport.pluginWatcher != nil {
		defer pluginWatcherPtr.Store(nil)
		defer m.viewport.pluginWatcher.Close()
	}

	m.turn.engine = remote
	m.remote.SetAttached(true)
	setupTUIToolsAndPolicy(m, fs)
	canonicalCwd := m.session.cwd
	setupTUIConversation(m, fs, canonicalCwd)

	return runTUIProgram(m, fs, altScreen, canonicalCwd)
}
