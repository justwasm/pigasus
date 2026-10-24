// Package remotecontrol — lists.go: pure helper functions for the
// four read-only List… JSON-RPC methods introduced in cycle
// rc-list-readonly.
//
// The four methods are:
//
//	ListRemoteAgentModels           — model catalog (id + label + backend)
//	ListRemoteAgentSlashCommands    — slash-command catalog (built-in + features)
//	ListRemoteAgentConversations    — convstore ListConversations wrapper
//	ListRemoteAgentEnvironments     — host os / cwd / user / capabilities snapshot
//
// All four are read-only — no cascade_id, no state mutation, no
// network. They round-trip cleanly through the WS server (and the
// Unix-socket server, which still drives these via the same
// dispatcher).
//
// Why a ListBackend seam instead of a direct call into models /
// convstore / auth / cli:
//
//   - The dispatcher must NOT import cmd/wagyu/ (the host is the
//     only consumer of those helpers, and a cycle-direction that
//     imports the host would invert the dependency). The seam
//     keeps the package dependency one-way: dispatcher owns the
//     wire surface, host wires the read paths in.
//
//   - Tests inject a static ListBackend without filesystem /
//     network. Production wires closures that reach into
//     internal/models, internal/cli, internal/convstore, and
//     stdlib (os.Hostname, runtime.GOOS, …).
//
//   - A future cycle that wants to gate ListRemoteAgentModels on
//     a network round-trip (the agy reference calls a registry
//     endpoint) replaces the closure without touching the
//     dispatcher.
//
// The default ListBackend (DefaultListBackend below) is the one
// the production wiring installs in cmd/wagyu/remote_control.go.
// Tests may construct their own and inject via Config.ListBackend.
//
// Read-only contract:
//
//	The four list methods must NOT mutate global state. This
//	cycle encodes the rule as ListMethodsNoSideEffects (in
//	list_dispatcher.go). The dispatcher pre-execution hook
//	rejects a ListRemoteAgentModels call whose handler reports
//	it ran a state-changing backend closure; for now the
//	production closures are pure, so the constant is a guardrail
//	rather than an active enforcer. TestListBackend_NoEnvLeak
//	pins the env-leak constraint directly.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"os"
	"sort"
	"strings"
	"time"
)

// ModelInfo is the JSON shape ListRemoteAgentModels emits for one
// catalog row. The fields mirror agy's recovered shape closely:
//
//	{
//	  "id":                "<catalogue-id>",      // e.g. "gemini-3.8-flash-medium"
//	  "label":             "<display-name>",      // e.g. "Gemini 3.8 Flash (Medium)"
//	  "backend":           "gateway" | "shim",
//	  "effort_supported":  true | false,           // whether the model exposes --effort
//	  "default":           true | false            // exactly one row per backend has default=true
//	}
//
// The dashboard renders this verbatim in its "/model" picker;
// effort_supported drives the picker showing / hiding the
// reasoning-level toggle. The "default" row is the one the
// dispatcher picks when the dashboard omits model=.
type ModelInfo struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Backend         string `json:"backend"`
	EffortSupported bool   `json:"effort_supported"`
	Default         bool   `json:"default"`
}

// DefaultModelInfo is the one-row model catalog the default
// ListBackend emits when no models are configured (the typical
// dev / sandbox scenario). It mirrors the cycle-101 shim
// fallback so a dashboard always sees a usable picker.
var DefaultModelInfo = []ModelInfo{
	{ID: "shim-default", Label: "shim default", Backend: "shim", EffortSupported: false, Default: true},
}

// SlashCommandInfo is one slash command's catalog row. The
// dashboard renders a three-column picker (name / scope /
// description). Args is intentionally optional — feature-toggle
// commands carry an "on | off" argument hint; pure actions
// (e.g. /resume, /compact) carry no args.
//
// Field choices:
//
//   - Name is the canonical command (no leading "/");
//   - Description is one line, ≤120 chars;
//   - Scope groups commands by their surface — builtin (always
//     present), feature-toggle (gated by AGY_* flags),
//     panel (opens a TUI panel), prompt-send (sends a model
//     prompt) — so the dashboard can bucket them.
//   - Args is omitted for non-arg commands; em-dash is rendered
//     by the picker in that case.
type SlashCommandInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Scope       string         `json:"scope"`
	Args        []SlashArgSpec `json:"args,omitempty"`
}

// SlashArgSpec is one argument slot a slash command accepts.
// Cycles future-cycle hooks (e.g. /vim, /effort) cite this in
// their JSON description. Today no SlashCommandInfo.Args
// entries are populated; the field exists for forward
// compatibility.
type SlashArgSpec struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}

// Slash-command scopes. The constant group lives here because
// the dispatcher's handler and the default ListBackend both
// emit them; a future cycle that grows the scope vocabulary
// adds a new constant + a new bucket in DefaultSlashCommands.
const (
	SlashScopeBuiltin       = "builtin"
	SlashScopeFeatureToggle = "feature-toggle"
	SlashScopePanel         = "panel"
	SlashScopePromptSend    = "prompt-send"
)

// DefaultSlashCommands is the catalog the default ListBackend
// emits for ListRemoteAgentSlashCommands. It is a static slice
// (not a switch on a registry) because the catalog is part of
// the build; new commands land as new rows here when their
// handlers land in cmd/wagyu/slash_commands.go.
//
// The set is the rc-list-readonly cycle's best-effort
// reconstruction of what agy's TS package emits. The order
// matches the print-mode /help outline:
//
//  1. local action commands (/clear /new /exit /quit /help)
//  2. picker-panel commands (/model /config /effort /mcp /hooks /permissions)
//  3. conversation-shape commands (/resume /fork /rewind /compact /rename /title)
//  4. prompt-send commands (cycle 194 family: /goal /plan /learn /grill-me)
//  5. feature-toggle commands (/vim /feature-diff)
//
// Names are lowercase, no leading "/".
var DefaultSlashCommands = []SlashCommandInfo{
	// --- local action ---
	{Name: "clear", Description: "Wipe the conversation transcript.", Scope: SlashScopeBuiltin},
	{Name: "new", Description: "Start a fresh conversation (same as /clear).", Scope: SlashScopeBuiltin},
	{Name: "exit", Description: "Close the program (alias: /quit).", Scope: SlashScopeBuiltin},
	{Name: "quit", Description: "Close the program (alias: /exit).", Scope: SlashScopeBuiltin},
	{Name: "help", Description: "Open the 3-tab help overlay (general/commands/shortcuts).", Scope: SlashScopePanel},

	// --- picker-panel ---
	{Name: "model", Description: "Open the model picker.", Scope: SlashScopePanel},
	{Name: "config", Description: "Open the configuration picker.", Scope: SlashScopePanel},
	{Name: "effort", Description: "Open the reasoning-effort picker (or set directly: /effort high).", Scope: SlashScopePanel},
	{Name: "mcp", Description: "List the configured MCP servers.", Scope: SlashScopePanel},
	{Name: "hooks", Description: "List configured lifecycle hooks.", Scope: SlashScopePanel},
	{Name: "permissions", Description: "Show the recovered permission rule table.", Scope: SlashScopePanel},

	// --- conversation-shape ---
	{Name: "resume", Description: "Open the conversation picker and load the chosen one.", Scope: SlashScopePanel},
	{Name: "fork", Description: "Fork the active conversation into a fresh db.", Scope: SlashScopeBuiltin},
	{Name: "rewind", Description: "Rewind the conversation to a prior step.", Scope: SlashScopeBuiltin},
	{Name: "compact", Description: "Compact the conversation; the recovered content path applies.", Scope: SlashScopeBuiltin},
	{Name: "rename", Description: "Rename the active conversation; opens a panel on no-arg.", Scope: SlashScopeBuiltin},
	{Name: "title", Description: "Print the active conversation's title.", Scope: SlashScopeBuiltin},
	{Name: "context", Description: "Print the active conversation's message count.", Scope: SlashScopeBuiltin},
	{Name: "diff", Description: "Show the working-copy diff with viewer footer.", Scope: SlashScopeBuiltin},
	{Name: "copy", Description: "Copy the latest model output to the clipboard.", Scope: SlashScopeBuiltin},

	// --- prompt-send ---
	{Name: "goal", Description: "Send the goal-setting prompt to the model.", Scope: SlashScopePromptSend},
	{Name: "plan", Description: "Send the planning prompt to the model.", Scope: SlashScopePromptSend},
	{Name: "teamwork", Description: "Invoke a team of agents to autonomously tackle large projects.", Scope: SlashScopePromptSend},
	{Name: "learn", Description: "Send the learn-this prompt to the model.", Scope: SlashScopePromptSend},
	{Name: "boost", Description: "Invoke the Boost multi-agent orchestrator for complex tasks.", Scope: SlashScopePromptSend},
	{Name: "grill-me", Description: "Send the Socratic follow-up prompt to the model.", Scope: SlashScopePromptSend},
	{Name: "btw", Description: "Side-question shortcut.", Scope: SlashScopePromptSend},

	// --- feature-toggle ---
	{Name: "vim", Description: "Toggle vim keybindings.", Scope: SlashScopeFeatureToggle,
		Args: []SlashArgSpec{{Name: "on|off", Required: true}}},
	{Name: "feature-diff", Description: "Open the feature-diff overlay (alias: /featdiff /diff-agy).", Scope: SlashScopeFeatureToggle},
}

// ConversationInfo is one row of ListRemoteAgentConversations's
// `conversations` array. The fields mirror the convstore-level
// ConversationMeta struct, projected to the wire shape the
// recovered dashboard expects:
//
//	{
//	  "id":         "<conv-id>",
//	  "title":      "<short>",
//	  "created_at": "<RFC3339>",      // inferred when missing
//	  "updated_at": "<RFC3339>",      // convstore ModTime
//	  "model":      "<label>",        // best-effort; empty when unknown
//	}
//
// Cursor: the next_before cursor is the RFC3339 timestamp of the
// OLDEST entry the previous page returned. The next call sends
// `before: "<next_before>"` to page backwards. An empty
// next_before means "no more pages".
type ConversationInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Model     string `json:"model,omitempty"`
}

// EnvironmentInfo is the snapshot the host renders from
// ListRemoteAgentEnvironments. The dashboard uses this to
// diagnose "which machine am I connected to"; ops uses it to
// audit dashboard-side capability.
//
// Fields and conventions:
//
//   - Capabilities: a struct that names the four on/off features
//     the host gates on. `remote_control` is the one the remote
//     dashboard cares about (true when the daemon bound a ws
//     listener with a successful handshake).
//
//   - EnvKeysPresent: NAMES ONLY (never values). A dashboard
//     that wants the value has to read it locally; we never put
//     secrets on the wire. The set is sorted + deduped before
//     emission so the same env each call returns the same array.
type EnvironmentInfo struct {
	OS              string         `json:"os"`
	Arch            string         `json:"arch"`
	Hostname        string         `json:"hostname"`
	Cwd             string         `json:"cwd"`
	User            string         `json:"user"`
	Shell           string         `json:"shell"`
	AntigravityHome string         `json:"antigravity_home"`
	Version         string         `json:"version"`
	EnvKeysPresent  []string       `json:"env_keys_present"`
	Capabilities    map[string]any `json:"capabilities"`
}

// DefaultEnvKeyProbe is the closed set of names
// ListRemoteAgentEnvironments looks for when populating
// EnvKeysPresent. Dashboards use the presence (not the value)
// to decide which integrations are wired:
//
//   - "ANTHROPIC_API_KEY"        — direct Anthropic egress
//   - "OPENAI_API_KEY"           — direct OpenAI egress
//   - "GEMINI_API_KEY"           — direct Gemini egress
//   - "JETSKI_OAUTH_TOKEN"       — Antigravity OAuth bearer (cycle 40)
//   - "OPENAGY_BACKEND"          — backend override (cycle 40 / 44)
//   - "OPENAGY_RC_SOCKET"        — remote-control socket path (rc-ws-transport)
//   - "ANTIGRAVITY_HOME"         — Antigravity config root
//   - "AGY_ADC_AUTH"             — Application Default Credentials flag
var DefaultEnvKeyProbe = []string{
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"GEMINI_API_KEY",
	"JETSKI_OAUTH_TOKEN",
	"OPENAGY_BACKEND",
	"OPENAGY_RC_SOCKET",
	"ANTIGRAVITY_HOME",
	"AGY_ADC_AUTH",
}

// ListBackend is the seam the dispatcher's four list handlers
// call into. Production wiring populates each field with a
// closure that reads from the real package (models / cli /
// convstore / stdlib). Tests populate with a fixture.
//
// Every closure is allowed to return an error; the dispatcher
// maps that to a -32603 envelope. Today the production closures
// return nil error — they're pure reads — but the field type
// keeps future cycles (e.g. an gateway-backed model fetch) free
// to surface a -32603 envelope cleanly.
//
// All fields are optional; a nil closure is treated as "the
// feature is not configured" and the handler returns the
// default slice (DefaultModelInfo / DefaultSlashCommands /
// empty / etc).
type ListBackend struct {
	Models        func(backend, filter string) ([]ModelInfo, error)
	SlashCommands func() ([]SlashCommandInfo, error)
	Conversations func(limit int, before time.Time) ([]ConversationInfo, time.Time, error)
	Environments  func() (EnvironmentInfo, error)
}

// DefaultListBackend is the production wiring's ListBackend.
// It is wired by cmd/wagyu/remote_control.go's StartHostSession
// helper and by the TCP path's maybeStartRemoteControlTCPDaemon;
// the package leaves both wrapping sites untouched in this
// cycle (the cycle's diff to cmd/wagyu/ is *only* the new
// `cfg.ListBackend = …` line in the Start path).
//
// This var is a sentinel-zero value (every closure is nil).
// Wire-start code MUST install a real ListBackend before
// accepting RPCs — the dispatcher's handler falls back to the
// per-method DefaultX slice when a closure is nil, so missing
// wiring returns *something* usable rather than a hard error.
var DefaultListBackend = ListBackend{}

// envKeysPresent returns a sorted, deduped list of names from
// the env probe set that are currently set in the process
// environment. NEVER returns values — only names. The function
// is the read-only invariant the test surface pins via
// TestListBackend_NoEnvLeak.
//
// Trimming: names whose values are empty strings are treated
// as "absent" (matches the dashboard's intent: an
// $OPENAI_API_KEY=” is not a wired integration).
func envKeysPresent(probe []string) []string {
	seen := make(map[string]bool, len(probe))
	var out []string
	for _, name := range probe {
		if seen[name] {
			continue
		}
		seen[name] = true
		if v := os.Getenv(name); v != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// filterModels applies a case-insensitive substring match
// against ModelInfo.ID and ModelInfo.Label. Empty filter is
// pass-through. All callers go through this so the
// filter behaviour is consistent across the four handlers
// (SlashCommands / Conversations don't take a filter today
// but if they grow one, the same helper covers them).
func filterModels(in []ModelInfo, filter string) []ModelInfo {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return in
	}
	needle := strings.ToLower(filter)
	var out []ModelInfo
	for _, m := range in {
		if strings.Contains(strings.ToLower(m.ID), needle) ||
			strings.Contains(strings.ToLower(m.Label), needle) {
			out = append(out, m)
		}
	}
	return out
}

// defaultBackendLabel returns the configured backend label
// for "default model" purposes. Reads $OPENAGY_BACKEND and
// falls back to "gateway" — matches the cycle-44 default the
// host's resolveBackend produces. The fallback here is the
// dispatcher's own fallback (the host may have left the env
// empty); it does NOT call resolveBackend in cmd/wagyu/
// because that would invert the import direction.
func defaultBackendLabel() string {
	if v := strings.TrimSpace(os.Getenv("OPENAGY_BACKEND")); v != "" {
		return strings.ToLower(v)
	}
	return "gateway"
}

// resolveTimeFormat is the RFC3339 formatter the four list
// responses use for any timestamp they emit. Centralised here
// so the dispatcher and the test surfaces can swap layouts
// (e.g. add ".999999999" fractional seconds) without grepping
// every handler.
func resolveTimeFormat(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
