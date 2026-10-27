package pigrt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/builtin"
	"github.com/MichaelKinsy/PiG/coding/extension/factoryload"
	"github.com/MichaelKinsy/PiG/coding/mcpext"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/modelctx"
	"github.com/justwasm/pigasus/retry"
)

// Options configures one runtime. CWD and Model are required; AgentDir
// defaults to the agy config home.
type Options struct {
	// CWD is the working directory the session is anchored to.
	CWD string
	// AgentDir is the global config directory. Empty uses DefaultAgentHome()
	// — the reference's ~/.gemini/antigravity-cli.
	AgentDir string
	// Model is the LLM the session calls. REQUIRED. Build it from the clone's
	// provider with (*piagentbridge.Provider).Model(label) so the gateway
	// transport stays in the loop.
	Model *ai.Model
	// SystemPrompt is the initial instruction block. Empty uses the
	// Antigravity CLI's system prompt (internal/agyprompt), which is what the
	// reference sends; set it explicitly to override that.
	SystemPrompt string
	// AgentBody is the Markdown body of a --agent custom agent. Non-empty
	// selects the custom-agent system prompt (agyprompt.BuildAgent) instead of
	// the default one when SystemPrompt is also empty.
	AgentBody string
	// CustomSubagents are the markdown agents discovered on this host. The
	// reference lists them in the default agent's <subagents> section; a
	// custom agent's prompt has no such section, so BuildAgent ignores them.
	CustomSubagents []agyprompt.Subagent
	// ConversationID identifies the conversation in the system prompt's
	// <user_information> section and in the artifact directory it names.
	// Empty mints a fresh UUID at New() time.
	ConversationID string
	// PigPrompt restores PiG's own coding prompt instead of the reference's.
	// It exists as an escape hatch for hosts that need PiG's tool-guidance
	// sections; the default is the reference behaviour.
	PigPrompt bool
	// ExtraTools are the clone's tools, appended to PiG's built-ins. Nil means
	// the session runs on PiG's built-in tools plus MCP.
	ExtraTools []agent.AgentTool
	// SkillPaths are directories PiG discovers skills from. Nil uses
	// AgySkillPaths(AgentDir); pass an explicit (possibly empty) slice to
	// override.
	SkillPaths []string
	// MCPConfigPath is the agy mcp.json to translate. Empty disables MCP
	// registration entirely.
	MCPConfigPath string
	// AlwaysLoadToolsByServer is the AGY 1.3.3 (line 42) dynamic
	// counterpart to the static per-server `alwaysLoad` slice in
	// mcp.json: tools listed here join the runtime's always-load
	// registration alongside the static hints, with dynamic
	// winning on a (server, tool) collision. Use this when the
	// (server, tool) pair was discovered out of band — a custom
	// MCP client, a `/mcp` panel toggle, a third-party plugin.
	//
	// The map is read once at New() time. Mutating it after New()
	// does not change the session's tool surface; rebuild the
	// session if the configuration has changed.
	//
	// The static always-load is read from the file at
	// MCPConfigPath independently, so this map may add entries a
	// server did not declare — those survive a merge as
	// Source="dynamic" and end up in AlwaysLoadedTools().
	AlwaysLoadToolsByServer map[string][]string
	// CreateTransport overrides how an MCP server's transport is built. Nil
	// uses PiG's default (stdio from command/args, streamable HTTP from url).
	// Tests use it to drive the extension over an in-process transport
	// instead of spawning a child process.
	CreateTransport mcpext.TransportFactory
	// NoContextFiles suppresses AGENTS.md/CLAUDE.md discovery.
	NoContextFiles bool
	// SkipBuiltinTools omits PiG's built-in coding tools (read, bash, edit,
	// write, …) while keeping ExtraTools, extension tools (MCP) and caller
	// hooks. The migration sets it so a session's tool surface is the
	// reference's own 60 names plus whatever MCP contributes — PiG's tools are
	// a later, deliberate step, not an accidental widening of the surface the
	// parity captures pin.
	SkipBuiltinTools bool
	// BeforeToolCall hooks run before each tool call and may block it. The
	// clone uses one to keep its recovered permission policy in front of every
	// call, including MCP tools PiG would otherwise run unattended.
	BeforeToolCall []agent.BeforeToolCallHook
	// ErrorSlot, when set, receives the original transport error of a failed
	// provider Stream so Run can report its type instead of PiG's flattened
	// message. See errors.go; combine it with RecordProvider.
	ErrorSlot *ErrorSlot
	// NoSession disables on-disk session persistence. Useful for one-shot
	// print runs that must not leave a conversation behind.
	NoSession bool
	// OpenURL and Notify surface the extension host's user-facing events (MCP
	// OAuth, load failures). Both may be nil.
	OpenURL func(url string)
	Notify  func(message, level string)
	// CompactEnabled, when non-nil, overrides PiG's default compaction
	// enable bit (true). Set to a *bool pointing at false to disable
	// auto-compaction entirely. When nil, the runtime uses PiG's default
	// (true) — PiG's coding/session.go:752 calls checkPromptCompaction
	// unconditionally on every Send, so "no override" still gives the
	// upstream behaviour.
	//
	// Cycle 227: AGY 1.4.0 always runs auto-compact driven by
	// cascade_config.checkpoint_config.max_token_limit / summaryCacheFillEffort,
	// and the clone's silent "default on" is the missing row in TODO §4.
	CompactEnabled *bool
	// CompactThreshold, when > 0, derives ReserveTokens as
	// `int((1 - threshold) * 200_000)`. A value of 0.5 yields 100 000
	// tokens of headroom; 0.92 (PiG's default) yields 16 000. The
	// settings-store value is rounded; the runtime only overrides
	// ReserveTokens, not KeepRecentTokens (it stays at PiG's 20 000).
	// 0 / negative / >= 1 leaves the runtime at the upstream default.
	CompactThreshold float64

	// Effort sets the agent's reasoning-effort (thinking) level for the
	// session. Empty keeps the model's default; "low"/"medium"/"high" and
	// any other ai.ThinkingLevel value PiG accepts ("xhigh", "max", "off",
	// "minimal") are forwarded through PiG's Session.SetThinkingLevel
	// after the MCP extensions bind, so the next Send uses the new level.
	//
	// Cycle effort-picker: the wagyu CLI accepts `--effort <level>` and a
	// `/effort <level>` slash command on top of the cycle-208 picker; the
	// picker takes effect through the same SetThinkingLevel path. PiG
	// clamps an over-range value against the active model's
	// ThinkingLevelMap, so an invalid level does not error — it silently
	// narrows to whatever the model supports.
	Effort string

	// AutoCont enables the cycle auto-continue-interrupt recovery. When
	// true, a stream-interrupt (StopReasonError / Aborted / Length) or
	// iteration-cap (agent.ErrMaxTurnsReached) is recovered by injecting
	// the verbatim AGY recovery copy and re-running via
	// Session.Agent().Continue(). The default zero value (false) keeps the
	// pre-cycle behaviour: a stream failure is surfaced as a typed error
	// the caller can match. The print path defaults this to true in
	// runPigPrintTurn so heads-up users see the same "auto-continuing"
	// tail the reference emits; tests and offline harnesses opt out.
	//
	// Cycle auto-continue-interrupt recovery depth is bounded by
	// AutoContMaxDepth (=3): three consecutive failures return
	// ErrAutoContMaxDepth and the caller is expected to surface a
	// user-input-required message. See auto_continue.go.
	AutoCont bool

	// RetryBudgetCap is the cumulative-wait cap on PiG's auto-retry
	// backoff per turn (agent.AutoRetryStartEvent). AGY 1.2.13's
	// changelog pins this at 30s: retries that fit inside the budget
	// run with the usual exponential backoff; the retry that would
	// overflow the budget is aborted via Session.AbortRetry() and the
	// underlying error surfaces to the user.
	//
	// Zero (the default) means retrybudget.DefaultPerTurnCap (=30s).
	// Negative disables the cap entirely — useful for tests that need
	// to drive the retry loop past the AGY limit without the observer
	// aborting the in-flight wait. PiG's own RetryPolicy.MaxDelayMs is
	// unchanged either way; this option is layered on top.
	//
	// Cycle retry-30s-cap-stays: see retrybudget and
	// docs/cycles/retry-30s-cap-stays/.
	RetryBudgetCap time.Duration
}

// DefaultAgentHome is the reference's config directory:
// $ANTIGRAVITY_HOME, else $HOME/.gemini/antigravity-cli. It mirrors
// auth.AntigravityConfigHome without importing internal/auth.
func DefaultAgentHome() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// normalizeOptions ensures required options are present and defaults are set.
func normalizeOptions(opts *Options) error {
	if opts.Model == nil {
		return errors.New("pigrt: Options.Model is required")
	}
	if opts.CWD == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("pigrt: resolve cwd: %w", err)
		}
		opts.CWD = wd
	}
	if opts.AgentDir == "" {
		opts.AgentDir = DefaultAgentHome()
	}
	return nil
}

// builtinExtensions assembles PiG's built-in extension set, with the MCP
// extension reading the agy mcp.json instead of its own.
func builtinExtensions(opts Options) ([]extension.Extension, *extension.ExtensionRuntime, error) {
	var o builtin.Options
	o.ConfigureMcp(opts.AgentDir, codingConfigDirName(), opts.OpenURL)
	path := opts.MCPConfigPath
	o.Mcp.LoadConfig = func(context.Context) mcpext.LoadedMcpConfig {
		return LoadAgyMCP(path)
	}
	if opts.CreateTransport != nil {
		o.Mcp.CreateTransport = opts.CreateTransport
	}
	if opts.Notify != nil {
		o.Mcp.OpenURL = opts.OpenURL
	}

	extRuntime := extension.CreateExtensionRuntime()
	var exts []extension.Extension
	for _, entry := range builtin.All(o) {
		if entry.Name == "mcp" && path == "" {
			continue
		}
		ext, err := factoryload.LoadExtensionFromFactory(entry.Factory, opts.CWD, nil, extRuntime, entry.Path())
		if err != nil {
			return nil, nil, fmt.Errorf("pigrt: builtin extension %q: %w", entry.Name, err)
		}
		exts = append(exts, ext)
	}
	return exts, extRuntime, nil
}

// codingConfigDirName is the per-project config directory name.
func codingConfigDirName() string { return ".pig" }

// ContextWindow is the fallback context window for a model the clone has no
// captured limit for.
const ContextWindow = modelctx.DefaultContextWindow

const compactionDefaultKeepRecent = 20000

// deriveCompactionReserve calculates the token reserve based on model capabilities and threshold.
func deriveCompactionReserve(opts Options) int {
	window := ContextWindow
	reserve := modelctx.DefaultReserveTokens
	if opts.Model != nil {
		if opts.Model.Capabilities.ContextWindow > 0 {
			window = opts.Model.Capabilities.ContextWindow
		}
		reserve = modelctx.For(opts.Model.ID).ReserveTokens
	}
	if opts.CompactThreshold > 0 && opts.CompactThreshold < 1 {
		if derived := int((1 - opts.CompactThreshold) * float64(window)); derived > 0 {
			reserve = derived
		}
	}
	return reserve
}

// buildSettingsPayload creates the JSON settings payload for compaction and retry.
func buildSettingsPayload(enabled bool, reserve int) string {
	return fmt.Sprintf(
		`{"compaction":{"enabled":%s,"reserveTokens":%s,"keepRecentTokens":%s},`+
			`"retry":{"maxRetries":%d,"baseDelayMs":%d,"maxAgentDelayMs":%d}}`,
		strconv.FormatBool(enabled),
		strconv.FormatInt(int64(reserve), 10),
		strconv.Itoa(compactionDefaultKeepRecent),
		retry.MaxAttempts,
		retry.InitialIntervalMs,
		retry.MaxIntervalMs,
	)
}

// buildDefaultSettingsManager assembles the SettingsManager the runtime hands to coding.NewServices.
func buildDefaultSettingsManager(opts Options) *coding.SettingsManager {
	enabled := true
	if opts.CompactEnabled != nil {
		enabled = *opts.CompactEnabled
	}
	reserve := deriveCompactionReserve(opts)
	payload := buildSettingsPayload(enabled, reserve)
	var settings coding.Settings
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return coding.NewInMemorySettingsManager(coding.Settings{})
	}
	return coding.NewInMemorySettingsManager(settings)
}
