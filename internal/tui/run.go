package tui

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/clirunner"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	browserflags "github.com/justwasm/pigasus/internal/core/cliflags/browser"
	logfileflags "github.com/justwasm/pigasus/internal/core/cliflags/logfile"
	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
	"github.com/justwasm/pigasus/internal/core/registry"
	"github.com/justwasm/pigasus/internal/core/settings"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
	persistentrc "github.com/justwasm/pigasus/internal/infra/remote/persistent"
	"github.com/justwasm/pigasus/internal/rchost"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
	"github.com/justwasm/pigasus/render"
	"github.com/justwasm/pigasus/urlchunk"
	"golang.org/x/term"
)

// ---- run.go ----

// Command pigasus is the openagy recovered 1:1 parse-parity shell of Google
// Antigravity CLI v1.3.4 (the v4 binary).
//
// Cycle 121 rename: the directory was `cmd/antigravity`; it was renamed to
// `cmd/wagyu` in cycle 121, and renamed to `cmd/pigasus` to align with the
// project module name github.com/justwasm/pigasus.
// The Go package is `main` and nothing imports it, so only the directory and
// the references to it changed.

const cliVersion = cli.ReferenceVersion

func init() {
	lipgloss.Writer.Profile = termcolor.Resolve(os.Environ())
	registry.SetTUIRunner(RunLiveTUI)
	registry.SetTUISnapshotRenderer(RenderTUI)
	cli.RegisterServerHandler(runServer)
	cli.RegisterTUIHandler(runTUIHandler)
	cli.RegisterACPHandler(runAcpHandler)
}

// Run is the main CLI entrypoint (delegates to internal/clirunner).
func Run(args []string) int {
	return clirunner.Run(args)
}

// SetupCLIParser delegates to clirunner.SetupCLIParser for backwards compatibility.
func SetupCLIParser(prog string) *cli.Parser {
	return clirunner.SetupCLIParser(prog)
}

const defaultModel = modelcatalog.DefaultModel

type permissionRunner struct {
	inner   backend.ToolRunner
	policy  *permissions.Policy
	sandbox sandbox.Sandbox
	Denials []string
	// hook is the documented PreToolUse gate. It is nil when the
	// session armed no documented hooks.
	hook func(ctx context.Context, call backend.ToolCall) hookToolGate
	// post is the documented PostToolUse dispatch. It is nil when the
	// session armed no documented hooks.
	post func(ctx context.Context, call backend.ToolCall, err error)
	// ask is the optional approval seam. Nil means the TUI's own
	// permission prompt; a headless caller passes a seam that answers
	// without a user, so a Strict policy's Ask becomes the documented
	// soft-deny instead of blocking forever.
	ask func(ctx context.Context, call backend.ToolCall) (bool, error)
}

func (r *permissionRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	policy := r.policy
	if r.hook != nil {
		gate := r.hook(ctx, call)
		if gate.Denied {
			r.Denials = append(r.Denials, call.Name)
			return gate.Reason, nil
		}
		if len(gate.Overwrite) > 0 {
			call.Arguments = mergeHookArgs(call.Arguments, gate.Overwrite)
		}
		if gate.Approved {
			policy = approvedPolicy(r.policy)
		}
	}
	denials := len(r.Denials)
	runner := toolrunner.PermissionRunner{
		Inner:   r.inner,
		Policy:  policy,
		Sandbox: r.sandbox,
		Denials: r.Denials,
		PromptUser: func(ctx context.Context, call backend.ToolCall) (bool, error) {
			if r.ask != nil {
				return r.ask(ctx, call)
			}
			kind := permissionprompt.KindDefault
			switch call.Name {
			case "write_to_file", "write_file":
				kind = permissionprompt.KindFileCreate
			case "replace_file_content", "edit_file":
				kind = permissionprompt.KindFileEdit
			}
			var args map[string]any
			if len(call.Arguments) > 0 {
				_ = json.Unmarshal(call.Arguments, &args)
			}
			req := permissionprompt.Request{
				Tool: call.Name,
				Kind: kind,
				Args: args,
			}
			p := RaisePermissionPrompt(req)
			res, err := p.Wait(ctx)
			if err != nil {
				return false, err
			}
			return res.IsApproved(), nil
		},
	}
	res, err := runner.Run(ctx, call)
	r.Denials = runner.Denials
	// The documented PostToolUse event fires after a tool completes: a
	// call the policy blocked never ran, so it has no completion.
	if r.post != nil && len(r.Denials) == denials {
		r.post(ctx, call, err)
	}
	return res, err
}

// RenderTUI prints the static TUI snapshot to w.
func RenderTUI(w io.Writer) error {
	return renderTUI(w)
}

// renderTUI prints the static TUI snapshot.
func renderTUI(w io.Writer) error {
	return renderTUIWithFlags(w, nil)
}

func renderTUIWithFlags(w io.Writer, fs *flag.FlagSet) error {
	email := auth.EffectiveUserEmail()
	if email == "" {
		_, email = accounts.Current()
	}
	model := "Gemini 3.8 Flash (High)"
	// The tier is filled in once the server names it; until then the
	// banner shows the email alone.
	tier := ""
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "/"
	}
	renderBanner(w, email, model, tier, cwd)
	fmt.Fprintln(w, separator)
	if fs != nil && cliflags.BoolValue(fs, "remote-control") {
		if auth.IsBusinessAccount() {
			fmt.Fprintln(w, cWarningBlueOpen+"> /remote-control"+cWarningBlueClose)
			fmt.Fprintln(w, uirows.NoticeIndented(rchost.SlashADCErrorMessage()))
			fmt.Fprintln(w, "")
		} else {
			convID := cliflags.StringValue(fs, "conversation")
			if convID == "" && cliflags.ResolveBoolAlias(fs, "continue", "c") {
				sel := resolveConversationSelection(fs, io.Discard)
				convID = sel.ID
			}
			uuid := persistentrc.InstallationUUID()
			hostname := persistentrc.ResolveInstanceName("")
			if hostname == "" {
				hn, err := os.Hostname()
				if err == nil && hn != "" {
					hostname = hn
				}
			}
			if hostname == "" {
				hostname = "localhost"
			}
			chunkWidth := rcURLDefaultWidth
			if f, ok := w.(*os.File); ok {
				if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
					chunkWidth = cols
				}
			} else if cols, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && cols > 0 {
				chunkWidth = cols
			}
			targetChunk := chunkWidth - len(rcURLIndent) - 1
			if targetChunk < 1 {
				targetChunk = 1
			}
			msg, extra := buildRemoteControlCard(convID, uuid, hostname, func(u string) []string {
				return urlchunk.Rows(u, rcURLIndent, targetChunk, render.HyperlinkCaps{})
			})
			if runtime.GOOS != "darwin" {
				extra = append(extra,
					"  Note: this platform has no launchd to auto-start the background daemon.",
					"  Run `pigasus remote-control run` in another terminal to connect.")
			}
			fmt.Fprintln(w, cWarningBlueOpen+"> /remote-control"+cWarningBlueClose)
			fmt.Fprintln(w, uirows.NoticeIndented(msg))
			for _, line := range extra {
				fmt.Fprintln(w, line)
			}
			fmt.Fprintln(w, "")
		}
	}
	fmt.Fprintln(w, "\033[38;2;138;180;248m>\033[39m")
	fmt.Fprintln(w, separator)
	fmt.Fprintln(w, statusrow.Static(model, separatorWidth))
	return nil
}

// ---- merged from main_backend.go ----
// main_backend.go — the transport dispatch: which backend serves a model.
//
// The clone has three transports:
//
//   - cloudcode — the reference's own. It serves the six models the server
//     advertises and needs an OAuth token.
//   - gateway  — one configured endpoint, reached with the endpoint, the
//     credential and the upstream model id the user's provider
//     configuration names. It serves the models the user added.
//   - shim     — the deterministic stand-in.
//
// The default is "gateway", and the default *model* decides what that
// means: a model in the reference's catalogue goes to CloudCode, a model the
// user configured goes to its own endpoint. An explicit `--backend gateway`
// forces the configured endpoint even for a reference model.
//
// The rule itself now lives in internal/infra/backend/select; the wrappers here
// keep the clone's own vocabulary (and its tests').

type gatewayBackend interface {
	GatewayConfig() gateway.GatewayConfig
}

// ---- session_flags.go ----

// applySessionFlags applies the flags a session starts with: the browser's
// policy, the project the session runs in and the CLI's log file.
//
// The policies live below the host — internal/core/cliflags/browser,
// internal/core/cliflags/projects and internal/core/cliflags/logfile own
// them — and the host keeps the wiring: where they are applied, and what
// the session needs from them.
//
// A new project's id is a conversation id, so the generator comes from the
// conversation's own package.
func applySessionFlags(fs *flag.FlagSet) {
	browserflags.Apply(fs)
	projectflags.Apply(fs, projectflags.Options{NewID: agyprompt.NewConversationID})
	logfileflags.Apply(fs)
}

// ---- windowtitle.go ----

// windowtitle.go — the host's side of the window title.
//
// The store itself is internal/ui/views/statusline's: the title script
// runs on the tick goroutine and the renderer reads the title on the
// model's goroutine, so the value needs its own lock. The host keeps the
// lazily created store and the accessor the status area reads it through.

// titleStoreFor returns the model's title store, creating it if the model
// was built without one.
func (m *RootModel) titleStoreFor() *statusline.TitleStore {
	if m == nil {
		return nil
	}
	return m.statusLine.TitleStore()
}

// ---- seasonal.go ----

// Cycle 381: the themed startup banner — its art scene, its four
// information rows and its seasonal sentence — moved to
// internal/ui/views/seasonal, the package that already owned the event,
// the art and the sentences. What stayed here is the theme's own state:
// the clock, the opt-in, the spinner and the refresh.

// seasonalNow is the clock the seasonal theme reads. The tests pin it so
// the /config row does not appear (or disappear) with the calendar.
var seasonalNow = time.Now

// seasonalSpinnerFPS is the themed spinner's frame rate.
//
// 【推测】 The reference's moon phase turns at the same rate as its
// plain spinner: the live capture in out/changelog-147/ref/turn/ shows
// the frame advancing by one step per capture, which is what a 10 fps
// spinner sampled every half second does. The binary does not carry
// the rate in a readable form.
const seasonalSpinnerFPS = time.Second / 10

// seasonalTheme returns the event the session is themed with, or nil
// when no event is in season or the user has not opted in.
//
// 【一手】 agy 1.4.7: the opt-in lives in settings.json as
// `seasonalThemesOptIn` and carries the event's own id
// ("halloween-2026"); an absent key means off.
func seasonalTheme() *seasonal.Event {
	ev := seasonal.Current(seasonalNow())
	if ev == nil {
		return nil
	}
	raw, err := settings.Field(seasonal.OptInKey)
	if err != nil {
		return nil
	}
	if !seasonal.Enabled(configpanel.RawString(raw), seasonalNow()) {
		return nil
	}
	return ev
}

// refreshSeasonalTheme re-reads the seasonal opt-in and swaps the
// streaming spinner for the themed one.
//
// 【一手】 agy 1.4.7: the theme follows the setting, and the spinner
// turns into the event's own moon phase while it is on.
func (m *RootModel) refreshSeasonalTheme() {
	m.viewport.seasonal = seasonalTheme()
	frames := m.viewport.seasonal.SpinnerFrames()
	if len(frames) == 0 {
		m.conversation.spinner = sp.New(sp.WithSpinner(sp.Dot))
		return
	}
	m.conversation.spinner = sp.New(sp.WithSpinner(sp.Spinner{
		Frames: frames,
		FPS:    seasonalSpinnerFPS,
	}))
}
