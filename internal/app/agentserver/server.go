// Package agentserver hosts the agent for a client.
//
// The server is the other half of the agentlink protocol: it owns the
// workspace, the transport, the credentials and the tools, and it runs
// the turns a client asks for. A client — the interactive session, or a
// third party speaking the protocol — sends the transcript and receives
// the turn's frames.
//
// The server is deliberately thin. It does not keep a conversation: the
// transcript travels with the request, the way it travels in the
// interactive session, and the server replays it. What the server *does*
// keep is the session's own state — the workspace, the tool set, the
// permission policy — because those belong to the machine the tools run
// on, not to the conversation.
package agentserver

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	browsersubagent "github.com/justwasm/pigasus/internal/app/subagent/browser"
	"github.com/justwasm/pigasus/internal/core/backend"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/version"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
)

// Version is the server's version. It rides the hello frame, so a
// client can tell what it is talking to.
//
// It is the clone's own version, the same one the rest of the clone
// reports. The fact lives in tier 1 (internal/core/version): a tier
// package must not reach into a host to ask for it.
var Version = version.Reference

// Config configures a server.
type Config struct {
	// Addr is the address to listen on. ResolveAddr maps the server's
	// own address options onto one — the default bind address is the
	// server's fact, not the caller's.
	Addr string
	// Token is the bearer token the server demands. Empty mints one.
	Token string
	// Workspace is the folder the session's tools act on. Empty means
	// the process's own directory.
	Workspace string
	// Model is the model the turns run under.
	Model string
	// Effort is the reasoning effort the turns ask for.
	Effort string
	// PermissionMode is the session's permission mode ("auto", "plan",
	// "acceptEdits"). Empty means the configured default.
	PermissionMode string
	// SkipPermissions turns the permission gate off.
	SkipPermissions bool
	// AddDirs are extra workspaces the session's tools may act on.
	AddDirs []string
	// AgentBody is a custom agent's body. Empty means the default
	// agent.
	AgentBody string
	// BackendFor returns the transport for a model. It is asked per
	// turn, so a client may switch the model without reconnecting.
	BackendFor func(model string) backend.Client
	// ACPVersion specifies the ACP protocol version to serve ("auto", "1", "2").
	// Default is "auto", enabling dual v1 & v2 draft negotiation via router.
	ACPVersion string
	// Log receives server event logs (connections, turns). Optional.
	Log io.Writer
}

// Server hosts the agent.
type Server struct {
	cfg  Config
	link *agentlink.Server

	mu       sync.Mutex
	prepared bool
	ahpHost  *AHPHost
}

// New returns a server.
//
// The bearer token is minted when the configuration leaves it empty, so
// a server always demands one: it runs shell commands against its own
// workspace.
func New(cfg Config) (*Server, error) {
	srv := &Server{cfg: cfg}
	link, err := agentlink.NewServer(agentlink.ServerConfig{
		Addr:  cfg.Addr,
		Token: cfg.Token,
		Log:   cfg.Log,
		Handlers: map[string]http.HandlerFunc{
			"/acp": func(w http.ResponseWriter, r *http.Request) {
				_ = srv.ServeACPWebSocket(w, r)
			},
			"/acp/": func(w http.ResponseWriter, r *http.Request) {
				_ = srv.ServeACPWebSocket(w, r)
			},
			"/ahp": func(w http.ResponseWriter, r *http.Request) {
				_ = srv.ServeAHPWebSocket(w, r)
			},
			"/ahp/": func(w http.ResponseWriter, r *http.Request) {
				_ = srv.ServeAHPWebSocket(w, r)
			},
		},
	})
	if err != nil {
		return nil, err
	}
	srv.link = link
	return srv, nil
}

// Token is the bearer token the server demands.
func (s *Server) Token() string { return s.link.Token() }

// Addr is the address the server listens on. It is empty until the
// listener is open.
func (s *Server) Addr() string { return s.link.Addr() }

// AHPHost returns the server's AHP host instance.
func (s *Server) AHPHost() *AHPHost {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ahpHost == nil {
		s.ahpHost = NewAHPHost(s)
	}
	return s.ahpHost
}

// Listen opens the server's listener and returns it.
//
// The listener is opened separately from the serving so a caller that
// asked for port zero can learn the port the kernel picked before it
// prints the address a client has to dial.
func (s *Server) Listen() (net.Listener, error) {
	return s.link.Listen()
}

// Serve serves on an existing listener until the context is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.prepare()
	return s.link.Serve(ctx, ln, agentlink.HandlerFunc(s.serve))
}

// ListenAndServe opens the listener and serves until the context is
// cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	s.prepare()
	return s.link.ListenAndServe(ctx, agentlink.HandlerFunc(s.serve))
}

// prepare does the process-wide setup the session's tools need.
//
// The extra workspaces and the MCP pool are process-wide — the tool
// registry reads them — so they are set once, before the first client
// connects.
func (s *Server) prepare() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prepared {
		return
	}
	s.prepared = true
	if len(s.cfg.AddDirs) > 0 {
		extra := make([]string, 0, len(s.cfg.AddDirs))
		for _, dir := range s.cfg.AddDirs {
			if abs, err := filepath.Abs(dir); err == nil {
				extra = append(extra, abs)
			}
		}
		tools.SetExtraWorkspaces(extra)
	}
	// The three MCP tools (call_mcp_tool, list_resources, read_resource)
	// need a live MCP connection; the pool is lazy, so this costs
	// nothing when no server is configured.
	setup.InstallMCPProvider(auth.AntigravityHome(), s.cfg.Workspace)
}

func (s *Server) logf(format string, args ...any) {
	if s.cfg.Log == nil {
		return
	}
	fmt.Fprintf(s.cfg.Log, "[agentserver %s] ", time.Now().Format("15:04:05"))
	fmt.Fprintf(s.cfg.Log, format+"\n", args...)
}

// serve answers one client connection.
func (s *Server) serve(ctx context.Context, conn *agentlink.Conn) error {
	session, err := s.session()
	if err != nil {
		s.logf("session init failed: %v", err)
		_ = conn.Send(ctx, agentlink.Frame{Kind: agentlink.KindResult, Error: err.Error()})
		return err
	}
	engine := agentturn.NewLocal(session.env)
	defer engine.Close()

	var (
		mu     sync.Mutex
		cancel context.CancelFunc
	)
	defer func() {
		mu.Lock()
		if cancel != nil {
			cancel()
		}
		mu.Unlock()
	}()

	for {
		f, err := conn.Next(ctx)
		if err != nil {
			return err
		}
		switch f.Kind {
		case agentlink.KindHello:
			s.logf("client hello (session=%s, model=%s)", session.ConversationID, session.Model)
			if err := conn.Send(ctx, agentlink.Frame{
				Kind:      agentlink.KindHello,
				Version:   Version,
				Workspace: session.Workspace,
				Model:     session.Model,
				Session:   session.ConversationID,
			}); err != nil {
				return err
			}
		case agentlink.KindPrompt:
			turnModel := f.Model
			if turnModel == "" {
				turnModel = session.Model
			}
			s.logf("turn started (id=%s, model=%s)", f.ID, turnModel)
			turnCtx, cancelTurn := context.WithCancel(ctx)
			mu.Lock()
			if cancel != nil {
				cancel()
			}
			cancel = cancelTurn
			mu.Unlock()
			go func(f agentlink.Frame) {
				defer cancelTurn()
				// The frames are written with the *connection's*
				// context, not the turn's: a cancelled turn still
				// owes the client its terminator, and a write on a
				// cancelled context would fail exactly then.
				s.runTurn(turnCtx, ctx, conn, engine, f)
			}(f)
		case agentlink.KindCancel:
			s.logf("turn cancelled (id=%s)", f.ID)
			mu.Lock()
			if cancel != nil {
				cancel()
			}
			mu.Unlock()
		}
	}
}

// runTurn runs one turn and forwards its frames to the client.
//
// Every frame carries the turn's identifier, so a client that has two
// turns in flight can tell them apart.
//
// The turn runs under turnCtx — the cancel frame cancels it — but the
// frames are written under writeCtx, the connection's own context: a
// cancelled turn still owes the client its terminator, and a write on
// the cancelled context would fail exactly when it is needed.
func (s *Server) runTurn(turnCtx, writeCtx context.Context, conn *agentlink.Conn, engine agentturn.Engine, f agentlink.Frame) {
	frames, err := engine.Turn(turnCtx, agentturn.Turn{
		Model:   f.Model,
		History: f.History,
	})
	if err != nil {
		s.logf("turn failed (id=%s): %v", f.ID, err)
		_ = conn.Send(writeCtx, agentlink.Frame{Kind: agentlink.KindResult, ID: f.ID, Error: err.Error()})
		return
	}
	terminated := false
	for frame := range frames {
		frame.ID = f.ID
		if frame.Kind == agentlink.KindResult {
			terminated = true
			if frame.Error != "" {
				s.logf("turn finished with error (id=%s): %s", f.ID, frame.Error)
			} else {
				s.logf("turn finished successfully (id=%s)", f.ID)
			}
		}
		if err := conn.Send(writeCtx, frame); err != nil {
			return
		}
	}
	if !terminated {
		// The turn ended without a terminator. The client waits for
		// one, so it is sent here rather than left hanging.
		s.logf("turn finished (id=%s)", f.ID)
		_ = conn.Send(writeCtx, agentlink.Frame{Kind: agentlink.KindResult, ID: f.ID})
	}
}

// Session is one client's session on the server: the workspace, the
// transport, the tools and the prompt.
type Session struct {
	// Workspace is the folder the session's tools act on.
	Workspace string
	// ConversationID names the session's conversation.
	ConversationID string
	// Model is the model the turns run under.
	Model string
	// Prompt is the system prompt the turns carry.
	Prompt string
	// Tools are the declarations the model may call.
	Tools []backend.ToolDecl
	// Runner executes the model's tool calls.
	Runner backend.ToolRunner
	// Effort is the reasoning effort the turns ask for.
	Effort string
	// BackendFor returns the transport for a model.
	BackendFor func(model string) backend.Client
}

// env builds the environment one turn runs in.
//
// The transport is asked for per turn, so a client that asks for
// another model gets another transport without reconnecting.
func (s *Session) env(ctx context.Context, model string) (agentturn.Env, func()) {
	if model == "" {
		model = s.Model
	}
	var be backend.Client
	if s.BackendFor != nil {
		be = s.BackendFor(model)
	}
	return agentturn.Env{
		Backend:      be,
		Model:        model,
		SystemPrompt: s.Prompt,
		Tools:        s.Tools,
		ToolRunner:   s.Runner,
		Effort:       s.Effort,
	}, func() {}
}

// session builds the session one connection runs on.
//
// Every connection gets its own conversation: the conversation's
// identifier rides the prompt, and two clients must not share one.
func (s *Server) session() (*Session, error) {
	return s.sessionFor(s.cfg.Workspace, s.cfg.Model)
}

func (s *Server) sessionFor(ws, model string) (*Session, error) {
	workspace := strings.TrimSpace(ws)
	if workspace == "" {
		workspace = s.cfg.Workspace
	}
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("agentserver: cannot read the working directory: %w", err)
		}
		workspace = wd
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("agentserver: cannot resolve %q: %w", workspace, err)
	}
	workspace = abs

	if model == "" {
		model = s.cfg.Model
	}

	conversationID := agyprompt.NewConversationID()
	agentDir := confhome.Antigravity()

	catalog := surface.RuntimeCatalog(workspace)
	if s.cfg.AgentBody != "" {
		catalog = agenttools.Registry(catalog)
	}
	policy := setup.PolicyFrom(s.cfg.PermissionMode, s.cfg.SkipPermissions)
	sandbox := setup.SandboxFrom(s.cfg.SkipPermissions, workspace, conversationID)
	runner := &toolrunner.PermissionRunner{
		Inner:   &toolrunner.RegistryRunner{Reg: catalog},
		Policy:  &policy,
		Sandbox: sandbox,
	}

	return &Session{
		Workspace:      workspace,
		ConversationID: conversationID,
		Model:          model,
		Prompt:         s.prompt(workspace, agentDir, conversationID),
		Tools:          toolrunner.AdvertisedToolDecls(s.cfg.AgentBody, catalog),
		Runner:         runner,
		Effort:         s.cfg.Effort,
		BackendFor:     s.cfg.BackendFor,
	}, nil
}

// prompt builds the system prompt the session's turns carry.
//
// The prompt is the reference's, byte for byte: the same sections, the
// same order, the same inputs — the workspace, the configuration home,
// the conversation's own identifier.
func (s *Server) prompt(workspace, agentDir, conversationID string) string {
	opts := agyprompt.DefaultOptions(workspace, agentDir, conversationID)
	for _, dir := range s.cfg.AddDirs {
		if abs, err := filepath.Abs(dir); err == nil {
			opts.Workspaces = append(opts.Workspaces, abs)
		}
	}
	opts.UserRules = agyprompt.DiscoverUserRules(workspace, agentDir)
	opts.Skills = agyprompt.DiscoverSkills(agyprompt.AgySkillPaths(agentDir, workspace))
	if s.cfg.AgentBody == "" {
		// The browser agent is one of the built-in subagent types, and
		// it comes before the user's own agents.
		opts.Subagents = append(opts.Subagents, browsersubagent.Subagent())
		opts.Subagents = append(opts.Subagents, setup.CustomSubagents()...)
	}
	if s.cfg.AgentBody != "" {
		return agyprompt.BuildAgent(s.cfg.AgentBody, opts)
	}
	return agyprompt.Build(opts)
}

