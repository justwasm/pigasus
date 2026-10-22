// slash_panels.go — cycles 185 + 187 + 189 + 190 + 193 + 197 +
// changelog-pagination: modal reference-style panels for the slash
// commands whose `agy` counterpart is a full-screen picker rather than
// a printed line. Cycle 193 adds the seven Class-A panels (/model
// /config /plugin /skills /usage /changelog /diff) recovered from
// /tmp/agycmp/panels/ppref.{model,config,plugin,skills,usage,changelog,diff}.raw.txt.
//
// Cycle 201 implements the *key actions* those panels announce but the
// earlier cycles only drew: the /usage and /changelog viewer windows now
// scroll, /plugin's tabs cycle, /skills' `enter View` opens a detail
// view, /diff's `←/→ switch view` moves the active tab, and /feedback's
// `enter`/`1`-`6` advance to the free-text stage. See the TODO.md P4
// item "panel 内的按键动作".
//
// Cycle 206 adds the alias resolution in openSlashPanel and the
// "own every key" router for the four list panels (/tasks /artifact
// /codesearch /context); both live in slash_list_panels.go.
//
// Why a separate file: the panels are pure presentation over static (or
// trivially derived) data, so they can live entirely outside tui_run.go.
// The only hooks in tui_run.go are (a) the `panel` field on runMode,
// (b) one dispatch call before handleSlashCommand, (c) one key branch in
// Update, and (d) one emit in viewBody.
//
// Evidence: /tmp/agycmp/panels/ppref.<cmd>.raw.txt — REF v1.4.0 driven at
// 220×30 through a private tmux socket (`tmux -L agycmp`). Every byte
// below is transcribed from those captures; see
// docs/cycles/cycle-185/ and TODO.md §2.1/§3.
//
// Colour caveat (recorded, not hidden): `capture-pane -e` normalises
// 24-bit RGB down to a 256-colour index, so a capture cannot tell whether
// REF emits `\x1b[38;2;66;133;244m` or `\x1b[38;5;69m`. Where the repo
// already recovered the true-colour value (cBlueOpen, cDimGrayOpen) the
// panel code uses it; the indices REF shows for the effort slider and a
// few accents have no recovered RGB, so they are emitted as 256-colour
// codes exactly as captured. TODO.md §6 lists this as a known limit.

package main

import (
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
)

// panelKind identifies which reference panel is open on the runMode.
type panelKind int

const (
	panelNone panelKind = iota
	panelMCP
	panelTasks
	panelEffort
	panelHooks
	panelPermissions
	panelAgents
	panelCredits
	panelCodeSearch
	panelArtifact
	panelFeedback
	panelRewind
	panelContext
	panelModel     // /model picker (cycle 193)
	panelConfig    // /config settings (cycle 193)
	panelPlugin    // /plugin picker (cycle 193)
	panelSkills    // /skills picker (cycle 193)
	panelUsage     // /usage quota viewer (cycle 193)
	panelChangelog // /changelog viewer (cycle 193)
	panelDiff      // /diff viewer (cycle 193)
	panelAddDir    // /add-dir picker (cycle 223) — last A-class panel
	panelRename    // /rename interactive panel (cycle charmland-component-adoption/02)
	panelFeatDiff  // /feature-diff viewer (cycle 236) — feature parity panel
)

// Palette. Indices are the xterm-256 codes REF emits (post-tmux
// normalisation). cBlueOpen / cDimGrayOpen / cWarningBlueOpen are the
// repo's recovered true-colour constants and are reused rather than
// duplicated.
const (
	panelGray238  = "\x1b[38;2;88;110;117m" // dim descriptions ("No MCP servers configured.")
	panelGray238C = "\x1b[39m"

	panelTitleOpen  = "\x1b[1m\x1b[39m" // panel title: bold + default fg
	panelTitleClose = "\x1b[0m"

	panelBoldBlueOpen  = "\x1b[1m\x1b[38;5;69m" // key glyphs in the "· " hint rows
	panelBoldBlueClose = "\x1b[0m"

	panelEffLowOpen  = "\x1b[38;2;133;153;0m" // effort slider, level 0 (green)
	panelEffMedOpen  = "\x1b[38;5;172m"       // effort slider, level 1 (orange)
	panelEffHighOpen = "\x1b[38;5;166m"       // effort slider, level 2 (red-orange)

	panelGreen34 = "\x1b[38;5;34m" // the ● next to an available agent

	// panelGray166 is the orange-red foreground used by the cycle-194
	// "Individual quota reached" warning line on /schedule and /browser
	// (recovered verbatim from /tmp/agycmp/panels/ppref.schedule.raw.txt and
	// ppref.browser.raw.txt — both lines were captured at fg 166; cycle 217).
	panelGray166  = "\x1b[38;5;166m"
	panelGray166C = "\x1b[39m"
)

// panelHints is the per-panel keyboard legend, in the "Keyboard: ..."
// form REF uses for most panels.
type panelHintDef struct {
	keys   []string
	labels []string
}

// slashPanel is the live panel state on the runMode.
type slashPanel struct {
	kind   panelKind
	cmd    string // "/mcp" — the Esc notice quotes it
	cursor int    // selected index into the panel's row list
	window int    // index of the first visible row in scrollable pickers
	// /agents: the discovered custom-agent rows and the prepared switch.
	// The reference keeps the panel open after Enter, switches on Esc, and
	// forks the conversation at that point. agentPreparedName == "" means
	// the pending selection is the built-in default agent.
	agents            []agent.Agent
	agentPreparedSet  bool
	agentPreparedName string
	agentPreparedBody string
	effortCursor      int // /model effort slider position (0..2); only used when kind == panelModel
	// /settings / /config: search query and inline submenu
	configQuery          string
	configSubmenu        bool
	configSubmenuCursor  int
	configSubmenuOrigVal string
	// configMemory holds the values of /config rows the reference never
	// persists (Enable Telemetry toggles without touching settings.json),
	// keyed by settings key.
	configMemory map[string]string
	// pluginTab is /plugin's active tab (panelInstalled / panelDiscover) and
	// pluginExpanded holds the plugins whose detail block is open, keyed by
	// plugin name. Cycle 198.
	pluginTab      int
	pluginExpanded map[string]bool
	// Cycle 204: pluginSearch holds the Discover search box's focus and
	// pluginQuery the typed text. They are only consulted for the Discover tab
	// (space / a printable key focuses the box, which filters the list live).
	pluginSearch bool
	pluginQuery  string
	// Cycle 219: pluginQueryCommitted is the Installed tab's focused-mode
	// commit flag. While pluginSearch is true on the Installed tab and the
	// flag is false, typing more characters does not live-filter; pressing
	// Enter commits the query (sets this flag to true), at which point the
	// rows below filter live and the legend switches from `enter Search` to
	// `enter Expand Details`. The flag is reset to false whenever
	// pluginSearch is left.
	pluginQueryCommitted bool
	// Cycle 219: pluginQueryAsleep tracks the cursor-asleep state the
	// reference shows immediately after Enter commits on the Installed
	// tab. The reference hides the blinking cursor block for as long
	// as the user does not type another character (verified at 220x50
	// with 100 captures over 2.5 s after Enter — none have the
	// reverse-video span; the cursor returns the moment a character
	// is typed, see p3-typed.out). Set to true on Enter, cleared on
	// any printable key (or backspace). Reset by enterPluginSearch /
	// exitPluginSearch alongside pluginQueryCommitted.
	pluginQueryAsleep bool
	// Cycle dpl (AGY 1.2.7): pluginEnabledOnly is the Installed tab's
	// "enabled-only customization query" toggle. When true, the row list
	// filters out entries whose plugins.<name>.enabled is false. The
	// toggle lives only in panel state — re-creating the panel reads the
	// on-disk install tree fresh (installedPluginRowsFiltered returns the
	// full list when the field is false), which is exactly what makes the
	// disabled rows come back after panel close: closePanel assigns a
	// zero-valued slashPanel so the field resets, and the next open
	// reads everything again. See slash_plugin_disabled_filter.go.
	pluginEnabledOnly bool
	// Cycle 212: pluginLocalInstall is the inline path prompt the
	// "+ Install from local directory..." row opens. The prompt owns every
	// keypress while it is active and is mutually exclusive with
	// pluginSearch. pluginLocalError records the last submit failure, shown
	// in the panel title (the reference paints it red — see p3.out row 13).
	pluginLocalInstall pluginLocalInstall
	pluginLocalError   string
	// pluginBlinkOn is the blink phase of the search / install cursor. REF
	// toggles it on a ~530 ms tick; the cycle-212 changes ship the same
	// machinery for both call sites (cycle 184 already had it for /resume).
	pluginBlinkOn bool

	// Cycle 201 key-action state: /diff's active view, /feedback's stage-2
	// form and /skills' detail sub-view. (/plugin's tab lives in pluginTab.)
	tab    int    // /diff: 0 = All Changes, 1 = Per Turn, 2 = Commit Tree.
	stage  int    // /feedback: 0 = category picker, 1 = free-text stage 2.
	detail bool   // /skills: a row's detail sub-view is open.
	text   string // /feedback stage-2 input buffer.

	// Cycle charmland-component-adoption/02: the rename panel's
	// single-line textinput. Embedded as a value field because the
	// bubbles textinput.Model is value-typed — Update() returns a new
	// model and the panel assigns it back. The field is zero-valid
	// for every other panel kind (textinput.New() returns a usable
	// default), so a non-rename panel does not pay any cost.
	textInput textinput.Model

	// Cycle 226: /diff's enter-view-diff subview. diffView is 0 in the
	// default file-list state; 1 = inline diff of diffViewPath (a working-copy
	// file, from All Changes); 2 = inline diff of diffViewPath (a commit
	// hash, from Commit Tree). The two are mutually exclusive with /skills'
	// `detail` because they live on different panels (m.panel.kind).
	diffView     int    // /diff: 0 = file list, 1 = file inline, 2 = commit inline.
	diffViewPath string // /diff: path (All Changes) or commit hash (Commit Tree).

	// Cycle diff-folding (AGY 1.3.5): the inline-diff subview's row
	// cursor and per-hunk fold state.
	//
	// diffInlineRow indexes the body lines of the inline subview (the
	// styled @@, +/-, and context rows the renderer emits below the
	// title). The cursor is inert on the pre-header / blank / legend
	// rows; renderDiffBodyWithFolds clamps it into the visible body
	// range before rendering so a fold toggle cannot strand it on a
	// missing row. The list-view cursor (m.panel.cursor) is preserved
	// unchanged so Esc returns to the same row the user left.
	diffInlineRow int
	// diffFolded is the per-hunk fold state of the inline diff, keyed
	// by hunk index in document order. A true entry collapses the
	// hunk's body into the @@ header + a dim grey `+N` indicator. The
	// map is reset (nil-ed) when the subview reopens on a new file
	// (diffOpenInline) or returns to the list (diffCloseInline), so a
	// new diff starts unfolded.
	diffFolded map[int]bool

	// act holds the cycle-205 action-panel state (/permissions, /hooks,
	// /agents). Kept in one field so the wiring here stays one line;
	// see slash_action_panels.go.
	act actionPanelState

	// cs holds the cycle-213 /codesearch result list state. The zero value
	// is the argument-less usage form (and also what a closed panel leaves
	// behind — codesearchStart overwrites it).
	cs csState

	// Cycle 225: /usage quota fetch state. The zero value's state is
	// quotaLoading, which the height-aware renderer reads to pick the
	// "Loading quota summary..." spinner row; the height-0 renderer (unit
	// tests, fallback) bypasses this and emits the cycle-193 placeholder
	// doc byte-exact.
	quota quotaState
}

// Effort levels. The reference orders them low, medium, high and the
// clone's recovered /effort output is "high".
const (
	effortLow = iota
	effortMedium
	effortHigh
)

// quotaState is the /usage panel's fetch state. REF 1.4.0 shows a
// "Loading quota summary..." spinner while a backend quota call is in
// flight, then the two model groups; the clone has no live quota
// endpoint (the print-mode `/usage` reply in print_slash.go is a
// frozen placeholder — see printSlashUsagePayload), so this is a
// deterministic stub whose countdown ticks once a second to mimic
// live data. 【推测】(values, not the layout).
//
// Cycle 225: the height-0 fallback (unit tests) bypasses this state
// and renders the cycle-193 placeholder doc byte-exact. The live
// terminal path (m.height > 0) reads `state` to pick between the
// spinner row and the bars.
type quotaState struct {
	state    int                // quotaLoading / quotaLoaded
	loadedAt time.Time          // when state flipped to loaded (start of the countdown)
	refreshG time.Time          // legacy stub: Gemini weekly countdown
	refreshO time.Time          // legacy stub: Claude/GPT weekly countdown
	summary  *auth.QuotaSummary // live retrieveUserQuotaSummary response
	fetchErr string             // last fetch failure (kept for diagnostics)
}

const (
	quotaLoading = iota
	quotaLoaded
)

// effortDescriptions are the three blurbs the /effort slider shows. All
// three were captured by driving REF's /effort panel with ←/→
// (/tmp/agycmp/effort_probe.py).
var effortDescriptions = [3]string{
	"Faster responses, lighter reasoning — great for simpler tasks",
	"Balanced speed and reasoning quality for most tasks",
	"Deepest reasoning for complex problems — slower but strongest",
}

// panelExitNotice reports whether Escaping the panel leaves the
// "⎿  Exited /<cmd> command" line in the transcript. REF does this for
// most panels but closes /mcp, /permissions, /codesearch, /feedback,
// /rewind, /context, /plugin, /changelog and /diff silently
// (/tmp/agycmp/panels/esc-ref-*.txt, /tmp/agycmp/esc2.log,
// /tmp/agycmp/esc3.log). Cycle 193 added the four
// /plugin /changelog /diff silent-pairings (and verified the others by
// driving REF's panel and looking for an Exited echo row).
func (p panelKind) exitNotice() bool {
	switch p {
	case panelMCP, panelPermissions, panelCodeSearch, panelFeedback,
		panelRewind, panelContext, panelPlugin, panelChangelog, panelDiff,
		panelRename, panelFeatDiff:
		return false
	}
	// Cycle 205 note: /agents keeps the pre-existing notice behaviour.
	// The reference's Esc-from-/agents is ambiguous — a 220x50 live probe
	// shows the panel surviving two Esc presses, while the cycle-195
	// capture esc-ref-agents-Esc.txt shows a closed, echo-free screen — so
	// the guarded pre-cycle behaviour was left untouched (存疑).
	return true
}

// keepStatusHint reports whether the panel leaves "? for shortcuts" in
// the status row. Every panel blanks it except /codesearch, which the
// reference renders with the idle hint still visible
// (/tmp/agycmp/panels/ppref.codesearch.txt line 5).
func (p panelKind) keepStatusHint() bool { return p == panelCodeSearch }

// openSlashPanel opens the modal panel for cmd if the reference renders
// one. Returns false for every other command so the caller can fall
// through to the normal handler.
func (m *runMode) openSlashPanel(cmd string) bool {
	// Cycle 206: the reference resolves the catalog's aliases first, so
	// `/cs` and `/search` open the Code Search panel like `/codesearch`
	// (and `/settings` / `/quota` / `/undo` open their panels). See
	// slash_list_panels.go.
	kind, ok := panelForCommand(canonicalPanelCommand(cmd))
	if !ok {
		return false
	}
	m.panel = slashPanel{kind: kind, cmd: "/" + cmd}
	// /effort opens on the *current* effort (REF shows the ◉ on "high"
	// and "  (current)" beside the blurb).
	if kind == panelEffort {
		m.panel.cursor = m.currentEffort()
	}
	// Cycle 197: /model opens with effortCursor seeded from settings.json's
	// persisted model, so the (current) annotation lands on the right
	// (model, effort) pair on first render. Cursor defaults to 0 (the
	// first row in the model list) regardless.
	if kind == panelModel {
		m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
	}
	// Cycle 203: /skills opens with no row selected (the reference shows
	// no "› " cursor until the first ↑/↓); the cursor indexes skills and
	// -1 is the "none" state.
	if kind == panelSkills {
		m.panel.cursor = -1
	}
	if kind == panelAgents {
		workspace := m.cwd
		if workspace == "" {
			workspace, _ = os.Getwd()
		}
		m.panel.agents = agent.DiscoverWith(agent.DiscoverOptions{Workspace: workspace})
		m.panel.cursor = 0
	}
	// Cycle 225: /usage opens in the loading state on a real terminal.
	// The first usageQuotaTickMsg flips it to quotaLoaded and stamps the
	// two stub bucket refill times; subsequent ticks tick the countdown.
	// The height-0 fallback (unit tests) ignores this and renders the
	// cycle-193 placeholder doc byte-exact (see panelUsageLines).
	if kind == panelUsage && m.height > 0 {
		// Drop any summary from a previous open; the async fetch below
		// fills it in.
		m.panel.quota = quotaState{state: quotaLoading}
	}
	return true
}

// initialEffortCursorFromSettings returns the cursor index (0..2) that
// matches the model the CLI would actually run. Cycle 197 revised this
// to resolve through resolveEffectiveModel first, so a settings value
// that is no longer available (e.g. "MiniMax-M3" on the 1.4.0
// catalogue) lands on the fallback model's effort — REF marks the
// fallback row current and parks the slider on its tier. Returns 1
// (medium) when the label carries no low/medium/high tier.
func initialEffortCursorFromSettings() int {
	lbl, _ := resolveEffectiveModel("")
	return effortCursorForLabel(lbl)
}

// effortCursorForLabel maps a model display label to the /model slider
// index: "(Low)"→0, "(Medium)"→1, "(High)"→2. Anything else — Claude's
// "(Thinking)", GPT-OSS, MiniMax M3, or a bare name — defaults to high,
// which matches the reference's "fallback row returns to High" behaviour
// when an unavailable model parks the slider (TestCycle208).
func effortCursorForLabel(label string) int {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "(low)"):
		return 0
	case strings.Contains(l, "(medium)"):
		return 1
	}
	return 2 // high / unknown (includes MiniMax M3)
}

// panelCurrentModelLabel is the display label of the model the TUI is
// actually using: the running model when known (m.model is set from
// resolveEffectiveModel at startup), otherwise settings → default.
// Cycle 197: `(current)` and the effort slider both derive from this so
// an unavailable settings value still marks — and parks on — the
// fallback model, matching REF's ppref.model.raw.txt capture.
func (m *runMode) panelCurrentModelLabel() string {
	if lbl := strings.TrimSpace(m.model); lbl != "" {
		return lbl
	}
	lbl, _ := resolveEffectiveModel("")
	return lbl
}

