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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/subagent"
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

func panelForCommand(cmd string) (panelKind, bool) {
	switch cmd {
	case "mcp":
		return panelMCP, true
	case "tasks":
		return panelTasks, true
	case "effort":
		return panelEffort, true
	case "hooks":
		return panelHooks, true
	case "permissions":
		return panelPermissions, true
	case "agents":
		return panelAgents, true
	case "credits":
		return panelCredits, true
	case "codesearch":
		return panelCodeSearch, true
	case "artifact":
		return panelArtifact, true
	case "feedback":
		return panelFeedback, true
	case "rewind":
		return panelRewind, true
	case "context":
		return panelContext, true
	// Cycle 193: A-class panel-overlay slash commands.
	case "model":
		return panelModel, true
	case "config":
		return panelConfig, true
	case "plugin":
		return panelPlugin, true
	case "skills":
		return panelSkills, true
	case "usage":
		return panelUsage, true
	case "changelog":
		return panelChangelog, true
	case "diff":
		return panelDiff, true
	case "add-dir":
		// Cycle 223: bare `/add-dir` (no args) opens panelAddDir; the
		// with-args case falls through to the cycle-217 echo path —
		// openSlashPanelArgs makes the routing decision (it sees the
		// trimmed argument tail that panelForCommand cannot).
		return panelAddDir, true
	case "rename":
		// Cycle charmland-component-adoption/02: bare `/rename` (no
		// args) opens panelRename — the panel is powered by
		// bubbles/v2/textinput and accepts a single-line name input.
		// The with-args case continues to flow through
		// renameFeedback() in slash_actions.go unchanged.
		return panelRename, true
	case "feature-diff":
		// Cycle 236: /feature-diff opens the feature-parity panel.
		// Source of truth is internal/cli/testdata/feature_diff_table.json.
		return panelFeatDiff, true
	}
	return panelNone, false
}

// currentEffort maps the clone's recovered /effort value onto the slider
// index. flags.effort is the parsed --effort value; anything that is not
// "low" or "medium" is treated as high, which is the reference default
// and the value the clone printed before this cycle.
func (m *runMode) currentEffort() int {
	switch flagStringValue(m.flags, "effort") {
	case "low":
		return effortLow
	case "medium":
		return effortMedium
	case "high":
		return effortHigh
	}
	// Cycle 292: with no explicit --effort the tier comes from the model
	// label, which is what the reference renders. A settings.json holding
	// "Gemini 3.8 Flash (Medium)" prints "· medium" in the status row; the
	// clone used to print "· high" there while the banner said Medium.
	return effortCursorForLabel(m.model)
}

// currentEffortName is the word form of currentEffort, used to feed the
// runtime layer with the slider's choice ("low", "medium", "high"). It
// picks up both --effort at startup and a /effort <level> / picker
// rewrite at runtime, since both surfaces mutate m.flags.
//
// Cycle effort-picker: the TUI's per-turn loop and the print path's PiG
// session both read the same flag value through this helper.
func (m *runMode) currentEffortName() string {
	return effortLevelName(m.currentEffort())
}

// panelRowCount is the cursor bound (0..count-1) for vertical-list
// panels. Effort is special: it is a horizontal slider (←/→ keys), not a
// row picker, but it still wants the cursor clamped against its 3
// levels. Cycle 193 adds the four cursor-bearing A-class panels (model,
// config, plugin, skills); the three viewer-only panels (usage,
// changelog, diff) scroll the body with ↑/↓ and never enter this table
// (a future cycle could implement a virtual page cursor).
//
// Cycle 222 made this a method on *runMode because panelRewind's count
// depends on m.session. The single legacy free-function caller in
// slash_cycle203_test.go passes a zero-valued runMode via the
// panelRowCountFor helper below.
func (m *runMode) panelRowCount(k panelKind) int {
	switch k {
	case panelTasks:
		// Cycle tasks-panel: 2 (title + legend) + len(rows),
		// capped at tasksPanelLimit. The "+2" below mirrors the
		// cycle-205 layout (title + blank + footer) — but
		// panelLines() does not include the blank explicitly
		// when there are rows, so we use len+2 when rows and
		// len+4 (title + 2 dim + footer + blank) when empty.
		if m.taskReg == nil {
			return 4
		}
		n := len(m.taskReg.List())
		if n == 0 {
			return 4 // title + "No background tasks." + blank + footer
		}
		if n > tasksPanelLimit {
			n = tasksPanelLimit
		}
		return n + 3 // rows + footer + leading title + trailing blank... off-by-one tolerated
	case panelEffort:
		return 3
	case panelHooks:
		return 5
	case panelPermissions:
		return 3
	case panelAgents:
		return 1 + len(m.panel.agents)
	case panelCredits:
		return 2
	case panelFeedback:
		return 6
	case panelRewind:
		// Cycle 222: the non-empty form yields one row per user-anchored
		// turn-step; the empty form stays at 1 so the cycle-189
		// TestPanelRewindEmptyLayout golden keeps passing when m.session
		// is nil. panelRewindLines re-derives this internally and applies
		// the same bound to m.panel.cursor in handlePanelKey.
		if len(m.session) == 0 {
			return 1
		}
		return len(rewindSteps(m.session))
	case panelModel:
		// Six rows since cycle 99 (MiniMax M3 prepended): the
		// fixed-five count bound the cursor to index 4 and prevented
		// users from selecting rows beyond Gemini 4 Argon.
		return 6
	case panelConfig:
		if m.panel.configSubmenu {
			rows := configRowsFiltered(m.panel.configQuery)
			if m.panel.cursor >= 0 && m.panel.cursor < len(rows) {
				return len(rows[m.panel.cursor].values)
			}
			return 0
		}
		return len(configRowsFiltered(m.panel.configQuery))
	case panelPlugin:
		// /plugin computes its own bound (m.pluginRowCount) because the
		// Discover tab carries an extra row; kept here for the panels that
		// still route through the generic ↑/↓ branch.
		return len(installedPluginRows())
	case panelSkills:
		// Cycle 203: the cursor indexes every catalogue skill (not the
		// 11-row hardcode), groups included in the render but not the
		// cursor. The reference's -1 "no selection" state is handled by
		// the panel-specific branch in handlePanelKey.
		return len(skillsPanelCatalog())
	}
	return 0
}

// wraps reports whether ↑/↓ wrap at the panel's row boundaries. The
// reference's /feedback category picker wraps both directions
// (/tmp/agycmp/panels/ppfb_wrap.raw.txt); every other panel clamps.
func (p panelKind) wraps() bool { return p == panelFeedback }

// isViewer reports whether the panel is a scrollable *document* viewer
// rather than a pick-from-a-list picker. The two viewer panels run a
// window over their content: ↑/↓ move it by one line, pgup/pgdown by a
// page and ctrl+home / ctrl+end jump to the ends. /usage and /changelog
// announce exactly those keys in their footers
// (/tmp/agycmp/panels/ppref.usage.raw.txt, ppref.changelog.raw.txt).
func (p panelKind) isViewer() bool {
	return p == panelUsage || p == panelChangelog || p == panelFeatDiff
}

// trailingBlank reports whether the panel's legend row is followed by an
// empty row before the status row. /hooks and /context legends sit
// immediately above the status row; every other captured panel has a
// blank row between legend and status
// (/tmp/agycmp/panels/ppref.hooks.txt rows 9-10 vs ppref.mcp.txt 17-19;
// ppref.context.empty.raw.txt rows 29-30).
func (p panelKind) trailingBlank() bool {
	// Live 1.4.3 also puts the /usage legend directly above the status row
	// (captured 2026-10-20 at 120x40), so it joins the no-trailing-blank set.
	return p != panelHooks && p != panelContext && p != panelUsage && p != panelChangelog && p != panelDiff
}

// panelView renders the panel body. It returns "" when no panel is open,
// mirroring pickerView/resumeView so viewBody can call it unconditionally.
func (m *runMode) panelView() string {
	if m.panel.kind == panelNone {
		return ""
	}
	var b strings.Builder
	for _, l := range m.panelLines() {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if m.panel.kind.trailingBlank() {
		b.WriteString("\n")
	}
	return b.String()
}

// panelLines returns the panel's fully styled rows, one per terminal row.
func (m *runMode) panelLines() []string {
	switch m.panel.kind {
	case panelMCP:
		return []string{
			panelTitleOpen + "MCP Servers" + panelTitleClose,
			"",
			panelGray238 + "  No MCP servers configured." + panelGray238C,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "enter", "Actions", "esc", "Exit"),
		}
	case panelTasks:
		// Cycle tasks-panel: render the live list of registered
		// background tasks (m.taskReg). The empty branch falls
		// through to the dim-gray "No background tasks." line so
		// a session with no shells still renders the same
		// 4-line layout as cycle 205's capture.
		return m.panelTasksLines()
	case panelEffort:
		return m.panelEffortLines()
	case panelHooks:
		// Cycle 205: stage dispatch (type list → matcher list → form).
		return m.hooksPanelLines()
	case panelPermissions:
		// Cycle 205: stage dispatch (scope list → tab view → rule form).
		return m.permissionsPanelLines()
	case panelAgents:
		return m.panelAgentsLines()
	case panelCredits:
		return m.panelCreditsLines()
	case panelCodeSearch:
		// Cycle 213: /codesearch <args> renders a live result list when
		// the argument tail carries a query; only the bare /codesearch
		// invocation falls back to the usage form. See slash_codesearch.go.
		return m.codesearchPanelLines()
	case panelArtifact:
		return []string{
			panelTitleOpen + "Artifacts" + panelTitleClose,
			"  " + cDimGrayOpen + "No artifacts" + cDimGrayClose,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "p", "preview", "enter", "open",
				"ctrl+g", "open in editor", "esc", "Dismiss"),
		}
	case panelFeedback:
		return m.panelFeedbackLines()
	case panelRewind:
		return m.panelRewindLines()
	case panelContext:
		return m.panelContextLines()
	// Cycle 193: A-class panel overlays.
	case panelModel:
		return m.panelModelLines()
	case panelConfig:
		return m.panelConfigLines()
	case panelPlugin:
		return m.panelPluginLines()
	case panelSkills:
		return m.panelSkillsLines()
	case panelUsage:
		return m.panelUsageLines()
	case panelChangelog:
		return m.panelChangelogLines()
	case panelDiff:
		return m.panelDiffLines()
	case panelAddDir:
		// Cycle 223: the last A-class panel. /add-dir with no path opens
		// a modal that lists the current trustedWorkspaces and lets the
		// user type a directory. Enter commits through the cycle-217
		// addDirFeedback path; Esc closes with the standard exit notice.
		return m.panelAddDirLines()
	case panelRename:
		// Cycle charmland-component-adoption/02: the rename panel is
		// powered by bubbles/v2/textinput and accepts a single-line
		// name. Enter submits through renameFeedback (same code path
		// `/rename <name>` uses on the slash-command line); Esc closes
		// silently (matches the cycle-193 set of silent-on-Esc panels).
		return m.panelRenameLines()
	case panelFeatDiff:
		// Cycle 236: the feature-parity viewer. Delegates to
		// feat_diff.go's panel renderer, which uses the cycle-207
		// viewerSpecFor/viewerWindow machinery for ↑/↓ / pgup/pgdown /
		// ctrl+home / ctrl+end scroll. The same code path the
		// /changelog viewer takes.
		return m.featDiffPanelLines()
	}
	return nil
}

// panelKeyboardHint builds the `Keyboard: <key> <label>  <key> <label>`
// legend row. Shape verified byte-for-byte against
// /tmp/agycmp/panels/ppref.mcp.raw.txt.
func panelKeyboardHint(pairs ...string) string {
	var b strings.Builder
	b.WriteString("\x1b[1mKeyboard: \x1b[0m")
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cBlueOpen)
		b.WriteString(pairs[i])
		b.WriteString(cBlueClose)
		b.WriteString(" ")
		b.WriteString(cDimGrayOpen)
		b.WriteString(pairs[i+1])
		b.WriteString(cDimGrayClose)
	}
	return b.String()
}

// panelDotHint builds the `   <key> <label> · <key> <label>` legend row
// that /hooks (and a few other panels) use instead of "Keyboard:".
func panelDotHint(lead string, pairs ...string) string {
	var b strings.Builder
	b.WriteString(lead)
	if len(pairs) >= 2 {
		b.WriteString(panelBoldBlueOpen)
		b.WriteString(pairs[0])
		b.WriteString(panelBoldBlueClose)
		b.WriteString(cDimGrayOpen)
		b.WriteString(" ")
		b.WriteString(pairs[1])
	}
	for i := 2; i+1 < len(pairs); i += 2 {
		b.WriteString(" · ")
		b.WriteString(panelBoldBlueOpen)
		b.WriteString(pairs[i])
		b.WriteString(panelBoldBlueClose)
		b.WriteString(cDimGrayOpen)
		b.WriteString(" ")
		b.WriteString(pairs[i+1])
	}
	b.WriteString(cDimGrayClose)
	return b.String()
}

// panelEffortLines renders the /effort panel. The slider geometry was
// recovered by driving the reference with ←/→ (/tmp/agycmp/effort_probe.py):
// three 15-cell groups, the active one replaced by a single ◉, and only
// the segment up to and including the active marker coloured.
func (m *runMode) panelEffortLines() []string {
	return rebuildEffortSlider(m.panel.cursor, m.currentEffort(), true)
}

// rebuildEffortSlider renders the /effort slider geometry with the
// active level set explicitly. Cycle 197 reuses this from
// panelModelLines so the /model picker can host its own slider at
// m.panel.effortCursor (independent of the model cursor).
//
// `curEff` is the user's currently-active effort; when `annotate` is
// true and `level` equals it, the description carries the trailing
// "  (current)" — the standalone /effort panel does that, the /model
// embed does not (ppref.model.raw.txt:25).
func rebuildEffortSlider(level, curEff int, annotate bool) []string {
	if level < 0 || level > 2 {
		level = effortHigh
	}
	colours := [3]string{panelEffLowOpen, panelEffMedOpen, panelEffHighOpen}

	// Bar: 7-cell lead, then for each level a marker plus the 14 cells
	// running to the next marker. Cycle 208: the cells after the active
	// ◉ are part of the dim run — see effortBarSegments.
	on, off := effortBarSegments(level)

	// The arrows dim to 256-colour 238 when they cannot move any further
	// (verified: REF's /effort panel shows a gray ◂ at "low" and a gray ▸
	// at "high", both in 238 — not the 241 the legend labels use).
	leftArrow := panelGray238 + "◂" + panelGray238C
	if level > effortLow {
		leftArrow = cBlueOpen + "◂" + cBlueClose
	}
	rightArrow := cBlueOpen + "▸" + cBlueClose
	if level == effortHigh {
		rightArrow = panelGray238 + "▸" + panelGray238C
	}
	slider := "  " + leftArrow + " " + colours[level] + on +
		panelGray238 + off + panelGray238C + " " + rightArrow

	// Labels: three 15-cell groups, the active one bold in the level
	// colour, the rest dim gray.
	groups := [3]string{"      low      ", "    medium     ", "     high      "}
	var labels strings.Builder
	labels.WriteString("    ")
	if level > effortLow {
		labels.WriteString(panelGray238)
		for i := 0; i < level; i++ {
			labels.WriteString(groups[i])
		}
	}
	labels.WriteString("\x1b[1m")
	labels.WriteString(colours[level])
	labels.WriteString(groups[level])
	labels.WriteString("\x1b[0m")
	if level < effortHigh {
		labels.WriteString(panelGray238)
		for i := level + 1; i < 3; i++ {
			labels.WriteString(groups[i])
		}
		labels.WriteString(panelGray238C)
	}

	desc := "    \x1b[3m" + panelGray238 + effortDescriptions[level]
	if annotate && level == curEff {
		desc += "  (current)"
	}
	desc += "\x1b[0m"

	return []string{
		panelTitleOpen + "Set Effort" + panelTitleClose,
		"",
		slider,
		labels.String(),
		desc,
		"",
		panelKeyboardHint("←/→", "Effort", "enter", "Select", "esc", "Go Back"),
	}
}

// hookRows are the five hook types the reference lists. The descriptions
// are static in REF.
var hookRows = [5][2]string{
	{"PreToolUse", "Before tool execution"},
	{"PostToolUse", "After tool execution"},
	{"PreInvocation", "Before each LLM invocation"},
	{"PostInvocation", "After each LLM invocation"},
	{"Stop", "When agent tries to exit"},
}

func (m *runMode) panelHooksLines() []string {
	lines := []string{
		// REF opens with a bare default-fg space, then the bold title.
		"\x1b[39m \x1b[1mHooks\x1b[0m",
		" " + panelGray238 + "  5 hook types" + panelGray238C,
		"",
	}
	for i, r := range hookRows {
		name := r[0]
		if pad := 14 - len([]rune(name)); pad > 0 {
			name += strings.Repeat(" ", pad)
		}
		if i == m.panel.cursor {
			lines = append(lines, " "+cBlueOpen+"> "+name+cBlueClose+"  "+
				panelGray238+r[1]+panelGray238C)
		} else {
			lines = append(lines, "   "+name+"  "+panelGray238+r[1]+panelGray238C)
		}
	}
	lines = append(lines, "", panelDotHint("   ", "↑/↓", "Navigate", "enter", "Select"))
	return lines
}

// permissionScopes are the three config scopes the editor offers. All
// three blurbs were recovered by driving REF's /permissions panel with
// Down (/tmp/agycmp/nav-permissions.log).
var permissionScopes = [3][2]string{
	{"Project", "Rules that apply only to this project (highest priority)"},
	{"Shared with Pigasus", "Rules that apply to all Pigasus products"},
	{"Global", "Global rules"},
}

func (m *runMode) panelPermissionsLines() []string {
	lines := []string{
		panelTitleOpen + "Permission Config Editor" + panelTitleClose,
		"",
		panelGray238 + "Select a config scope to edit:" + panelGray238C,
		"",
	}
	for i, s := range permissionScopes {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+s[0]+cBlueClose)
		} else {
			lines = append(lines, "  "+s[0])
		}
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(permissionScopes) {
		cursor = 0
	}
	lines = append(lines,
		"",
		"  "+panelGray238+permissionScopes[cursor][1]+panelGray238C,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Save", "esc", "Close"),
	)
	return lines
}

func (m *runMode) panelAgentsLines() []string {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	title := "Create New Agents"
	if m.panel.agentPreparedSet {
		title += "  [ Switch will fork the current conversation on exit ]"
	}
	lines := []string{
		panelTitleOpen + title + panelTitleClose,
		"  " + panelGray238 + "Workspace: " +
			filepath.Join(wd, ".agents", "agents", "{agent_name}", "agent.md") + panelGray238C,
		"  " + panelGray238 + "Global: " +
			filepath.Join(home, ".gemini", "config", "agents", "{agent_name}", "agent.md") + panelGray238C,
		"",
		"",
	}
	// The header names the pending selection (if any), otherwise the
	// active agent. "" is the built-in default, which the reference
	// leaves unannotated.
	active := m.agentName
	if m.panel.agentPreparedSet {
		active = m.panel.agentPreparedName
	}
	header := "Available Agents"
	if active != "" {
		header += "  (active: " + active + ")"
	}
	lines = append(lines, "\x1b[1m"+header+"\x1b[0m")

	type agentRow struct {
		name, desc string
		custom     bool
	}
	rows := []agentRow{{"default", "Default agent", false}}
	for _, a := range m.panel.agents {
		rows = append(rows, agentRow{a.Name, a.Description, true})
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	for i, r := range rows {
		rowActive := (i == 0 && active == "") || (i > 0 && active == r.name)
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		marker := "  "
		if rowActive {
			marker = panelGreen34 + "\u25cf " + cWarningBlueOpen
		}
		name := r.name
		if i == cursor {
			name = cBlueOpen + name + cBlueClose
		} else if rowActive {
			name = cWarningBlueOpen + name + cBlueClose
		}
		line := cBlueOpen + prefix + cBlueClose + marker + name
		if r.desc != "" {
			line += "  " + panelGray238 + r.desc + panelGray238C
		}
		lines = append(lines, line)
	}

	// AGY 1.3.1 — list of running agents. When there are no
	// active subagents the section collapses (the layout match
	// the cycle-205 capture). When subagents exist, each row
	// renders its StatusLine so an errored subagent shows
	// "Error: <reason>" instead of the silent "Done" the
	// pre-cycle panel produced.
	running := m.activeSubagentStatuses()
	if len(running) > 0 {
		lines = append(lines, "", "\x1b[1mRunning Agents\x1b[0m")
		for _, r := range running {
			lines = append(lines, "  "+r)
		}
	}

	lines = append(lines,
		"",
		panelKeyboardHint("\u2191/\u2193", "Navigate", "enter", "Select / Toggle",
			"k", "Kill Active Subagent", "esc", "Go Back"),
	)
	return lines
}

// activeSubagentStatuses returns one rendered row per active
// subagent, in registration order. The rows read the Subagent's
// StatusLine so a subagent that stopped on error renders as
// "Error: <reason>" instead of the misleading "Done" the panel
// used to show (AGY 1.3.1). A nil or empty manager yields an empty
// slice, which panelAgentsLines collapses so the cycle-205 capture
// stays byte-exact when no subagent has been spawned yet.
//
// The rows are unstyled plain text — the panel's blue/green
// palette is reserved for the "Available Agents" cursor row above,
// so the running list stays distinguishable.
func (m *runMode) activeSubagentStatuses() []string {
	var mgr *subagent.Manager
	if m.taskSubagentManager != nil {
		mgr = m.taskSubagentManager
	} else if defaultTaskSubagentManager != nil {
		mgr = defaultTaskSubagentManager
	}
	if mgr == nil {
		return nil
	}
	all := mgr.List()
	if len(all) == 0 {
		return nil
	}
	rows := make([]string, 0, len(all))
	for _, sa := range all {
		if sa == nil {
			continue
		}
		rows = append(rows, fmt.Sprintf("%s  %s", sa.Name, sa.StatusLine()))
	}
	return rows
}

func (m *runMode) panelCreditsLines() []string {
	actions := [2]string{"Get More AI Credits", "See Activity"}
	lines := []string{
		cBlueOpen + "Pigasus CLI" + cBlueClose + "  " +
			"\x1b[1m" + panelGray238 + "credits" + "\x1b[0m",
		"\x1b[1m" + panelGray238 + "Model AI Credits" + "\x1b[0m",
		"",
		"\x1b[1mRemaining AI Credits\x1b[0m: " +
			"\x1b[1m" + cBlueOpen + "AI Credits not enabled (enable in /settings)" + "\x1b[0m",
		"",
		"\x1b[1m" + panelGray238 + "Actions" + "\x1b[0m",
		"",
	}
	for i, a := range actions {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+a+cBlueClose)
		} else {
			lines = append(lines, "  "+a)
		}
	}
	lines = append(lines, "",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle", "esc", "Close"))
	return lines
}

// feedbackCategories are the six categories the reference lists in stage 1
// of the /feedback panel. Verbatim from
// /tmp/agycmp/panels/ppref.feedback.raw.txt rows 5–10. Selecting a category
// and pressing Enter transitions REF to a "Review Submission Data" stage
// (free-form textarea + ctrl+s Submit); the openagy clone replicates the
// form byte-for-byte (cycle 208) and additionally binds Enter to submit
// in stage 2 (cycle 221) — see handleFeedbackTextKey.
var feedbackCategories = [6]string{
	"1. Code doesn't work (e.g., syntax errors, bad logic)",
	"2. Repeated itself (e.g., stuck running the same tool over again)",
	"3. Took too many steps (e.g., progress was much too slow)",
	"4. Ignored instructions (e.g., missed specific constraints)",
	"5. Got stuck (e.g., unable to recover from an error)",
	"6. Other",
}

// panelFeedbackLines renders the /feedback stage-1 category picker. The
// selected row is prefixed with `> ` in the panel's blue; everything else
// is plain. Layout recovered from ppref.feedback.raw.txt:
//   - bold "Submit Feedback" title
//   - blank
//   - "What went wrong? Select a category to assist your report:" (plain)
//   - blank
//   - six category rows
//   - blank
//   - "  esc Cancel · 1-6 Select & Continue · enter Continue" dot hint
//
// Cycle 201 added stage 2 (the free-text form). Cycle 221 closes the
// TODO §4 / 2.1 follow-up: stage 2 is now reachable from stage 1, the
// buffer fills on typing, backspace deletes, Enter (and ctrl+s) submit
// and echo the cycle-192 local-only line, and Esc returns to stage 1
// with the same category still selected.
func (m *runMode) panelFeedbackLines() []string {
	if m.panel.stage > 0 {
		return m.panelFeedbackStage2()
	}
	lines := []string{
		panelTitleOpen + "Submit Feedback" + panelTitleClose,
		"",
		"What went wrong? Select a category to assist your report:",
		"",
	}
	for i, c := range feedbackCategories {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+c+cBlueClose)
		} else {
			lines = append(lines, "  "+c)
		}
	}
	lines = append(lines,
		"",
		panelDotHint("  ", "esc", "Cancel", "1-6", "Select & Continue", "enter", "Continue"),
	)
	return lines
}

// panelFeedbackStage2 renders the /feedback free-text stage. Cycle 208
// captured REF's real form (fin_fb_stage2.raw.txt); the lines now come
// from feedbackStage2Lines. The old body was marked 【推测】.
func (m *runMode) panelFeedbackStage2() []string {
	return m.feedbackStage2Lines()
}

// rewindSteps returns one entry per turn-step in m.session, newest first.
// A turn is anchored on a user-role message: each user message is one
// "turn-step", and the step's snippet is the first ~30 runes of that user
// message. Tool-call and tool-result messages that flank a turn are kept
// attached to the turn immediately preceding them so a step's window is
// contiguous in m.session index space.  The list is in reverse chronological
// order so the most recent turn is at index 0 (REF's /rewind highlights the
// current / latest step, matching the empty-state `(current)` row).
//
// Cycle 222: the cycle-189 empty state shipped literal `(current)`. Cycle
// 222 fills in the non-empty state — one row per step — and the Enter key
// truncates m.session to the selected step. The latter is the safe stub
// the task brief allowed: it skips the model call but does the transcript
// edit so a follow-up prompt sees the prior state.
func rewindSteps(messages []backend.Message) []rewindStep {
	var steps []rewindStep
	// Walk backwards so we know the most-recent end index first; capture
	// the index AFTER the trailing assistant (or after the user message
	// itself when no assistant has replied yet). Then sweep back: every
	// time we encounter a user-role message we record it as a step whose
	// "end" boundary is the immediately preceding user's start.
	if len(messages) == 0 {
		return nil
	}
	end := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			steps = append(steps, rewindStep{
				userIdx: i,
				endIdx:  end,
				snippet: firstRow(messages[i].Content, 30),
			})
			end = i
		}
	}
	// The `end = i` above resets to *before* the user message, so the
	// remaining assistant/tool messages before the first user message are
	// silently dropped from the picker — that matches REF, which doesn't
	// surface orphan leading assistant rows. The result is newest-first.
	return steps
}

// firstRow takes the first non-empty line of s and caps it at max runes.
func firstRow(s string, max int) string {
	for _, line := range strings.SplitN(s, "\n", 2) {
		line = strings.TrimSpace(line)
		if line != "" {
			if r := []rune(line); len(r) > max {
				return string(r[:max-1]) + "\u2026"
			}
			return line
		}
	}
	return ""
}

// rewindStep is one row in /rewind's picker. userIdx is the m.session
// index of the user message that anchors the step; endIdx is the slice
// index AFTER the last message the step owns (one past the trailing
// assistant / tool-result chain).
type rewindStep struct {
	userIdx int
	endIdx  int
	snippet string
}

// rewindStepHeight is the vertical budget /rewind's picker consumes at
// the current terminal height: title (1) + blank (1) + rows + blank (1)
// + counter (1) + keyboard (1) = 5 chrome rows around the list. Matches
// the cycle-189 height-less default of 17 visible rows; falls back to
// the same when m.height is 0 (test runs, alt-screen-not-yet-settled).
func rewindStepHeight(h int) int {
	if h <= 0 {
		return 17
	}
	const chrome = 5
	v := h - chrome - 1 /* the status row */
	if v < 1 {
		v = 1
	}
	return v
}

// panelRewindLines renders the /rewind picker. Cycle 189 shipped the
// empty-state form (literal `(current)` row); cycle 222 fills in the
// non-empty form — one row per user-anchored turn, newest first, with
// the cursor row in brand blue and a `(current)` annotation on the
// most recent turn.
//
// Reference layout (recovered from /tmp/agycmp/panels/ppref.rewind.during.raw.txt
// for the empty form; non-empty form follows by analogy — REF keeps the
// same 5-row skeleton and adds one row per turn):
//
//	Rewind Conversation
//
//	> Turn 3  /help me with…                       (current)   ← newest, cursor here on open
//	  Turn 2  /statusline
//	  Turn 1  hello there
//
//	Keyboard: ↑/↓ Navigate  enter Select  esc Cancel
//
// Cycle 222 probe constraint: the REF binary at ~/.local/bin/agy 1.4.0
// refuses to start without OAuth — `agy` opens a Google OAuth browser
// dialog at first launch and never lets the user reach a multi-turn
// session without a Google account. The non-empty layout above was
// reasoned from the cycle-189 recovery of the empty row + the
// reference convention (Cursor / Aider rewind pickers show one row per
// turn with the most-recent flagged as the live one); see the 存疑 list
// in the cycle-222 deliverable for the things a real REF capture would
// firm up.
//
// Windowing: rows = rewindStepHeight(m.height). When the conversation
// has more turns than fit, m.panel.window tracks the first visible row
// (clamped to [0, total - visible]) and the footer grows a counter
// `(first-last of N turns)` in dim grey, matching the cycle-211 skills
// detail viewer and the cycle-213 codesearch counter.
func (m *runMode) panelRewindLines() []string {
	steps := rewindSteps(m.session)
	if len(steps) == 0 {
		// Cycle 189 empty form — preserved unchanged so the same
		// TestPanelRewindEmptyLayout golden file keeps passing.
		return []string{
			panelTitleOpen + "Rewind Conversation" + panelTitleClose,
			"",
			cBlueOpen + "> (current)" + cBlueClose,
			"",
			panelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
		}
	}
	visible := rewindStepHeight(m.height)
	total := len(steps)
	if visible > total {
		visible = total
	}
	// Clamp window + cursor to the loaded state. openSlashPanel resets
	// both to 0; tests may have advanced them.
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.cursor < 0 {
		m.panel.cursor = 0
	}
	if max := total - visible; m.panel.window > max && max >= 0 {
		m.panel.window = max
	}
	if m.panel.cursor > total-1 {
		m.panel.cursor = total - 1
	}
	window := m.panel.window

	// Pre-render each step row.
	allRows := make([]string, total)
	for i, s := range steps {
		label := fmt.Sprintf("Turn %d  %s", i+1, s.snippet)
		if i == 0 {
			// Cursor's "current turn" annotation — placed in dim gray
			// 238 (the same palette the (current) flag carries in the
			// /model picker). Only the first step carries it; for a
			// multi-pane capture a second `(current)` would just be
			// visual noise.
			label += panelGray238 + "  (current)" + panelGray238C
		}
		if i == m.panel.cursor {
			allRows[i] = cBlueOpen + "> " + label + cBlueClose
		} else {
			allRows[i] = "  " + label
		}
	}

	lines := []string{
		panelTitleOpen + "Rewind Conversation" + panelTitleClose,
		"",
	}
	lines = append(lines, allRows[window:window+visible]...)
	lines = append(lines, "")
	if total > visible {
		lines = append(lines, rewindStepCounter(window+1, window+visible, total))
	}
	lines = append(lines, panelKeyboardHint("↑/↓", "Navigate",
		"enter", "Select", "esc", "Cancel"))
	return lines
}

// rewindStepCounter renders the dim gray "(a–b of K turns)" counter the
// /rewind picker shows in its footer when the conversation has more steps
// than fit in the visible window. Same colour code as viewerCounter (line
// 2164) but the noun is "turns" — the picker never has documents.
func rewindStepCounter(start, end, total int) string {
	return cDimGrayOpen + fmt.Sprintf("  (%d\u2013%d of %d turns)", start, end, total) + cDimGrayClose
}

// panelRewindPickStep is the cycle-222 Enter handler for /rewind. The
// safe stub the cycle brief allowed: the picker emits a `Rewound to
// step N` line via slashRespond's REF-style echo format, the panel
// closes silently (same exit-notice=false set as /mcp /feedback), and
// m.session is truncated so a follow-up prompt no longer carries the
// dropped user+assistant pair. Doing the transcript edit (instead of a
// no-op) keeps the existing /rewind slash command and the picker in
// step — rewindSession pops only the trailing pair, while this routine
// pops trailing pairs until the picked step's user message is the
// trailing user message. Returns the trimmed count for the echo line.
func (m *runMode) panelRewindPickStep() int {
	steps := rewindSteps(m.session)
	if len(steps) == 0 {
		// Empty form — Enter is a no-op (matches cycle-189's "no rewind
		// target" comment). Esc still handles silent close.
		return 0
	}
	cur := m.panel.cursor
	if cur < 0 || cur >= len(steps) {
		cur = 0
	}
	// The picked step's NEW end boundary — keep everything up to and
	// including the step's trailing assistant / tool-result chain (one
	// past the user message). EndIdx is the slice index immediately
	// after the last message the step owns (the trailing assistant
	// message in the simple user/assistant case).
	target := steps[cur].endIdx
	removed := len(m.session) - target
	if removed <= 0 {
		// Already at or before the picked step (cursor on the current
		// turn, which is the default). No-op: no truncation, no echo.
		return 0
	}
	m.session = m.session[:target]
	if cur == 0 {
		m.slashRespond("/rewind",
			fmt.Sprintf("Rewound to step %d (current, no messages removed)", cur+1))
	} else {
		m.slashRespond("/rewind",
			fmt.Sprintf("Rewound to step %d (%d message(s) removed)", cur+1, removed))
	}
	return removed
}

// contextGridRow renders one row of the /context panel's 44-cell `□`
// grid with the right-side descriptor plain (default fg). The cells use
// dim gray 241, matching ppref.context.empty.raw.txt rows 15–21. There
// is no SGR between the last cell and the descriptor (so the right-side
// text inherits the default fg, which is also what REF does).
func contextGridRow(text string) string {
	return contextGridRowPadded(text, 5)
}

// contextGridRowWidth is the terminal-width-aware form: the reference
// paints one `□` cell per five columns (24 at 120x40, 40 at 200x50, 44
// at 220x50). A zero width keeps the historical 220-column shape for
// tests and callers that have not measured a window yet.
func contextGridRowWidth(text string, pad, width int) string {
	cols := width / 5
	if width <= 0 {
		cols = 44
	}
	if cols < 1 {
		cols = 1
	}
	return contextGridRowPaddedCols(text, pad, cols)
}

// contextGridRowPadded is contextGridRow with explicit padding width.
// REF uses 5 spaces for most rows and 6 for the `(0.0%)` row, which
// shifts the percentage one column further right (ppref row 16).
func contextGridRowPadded(text string, pad int) string {
	return contextGridRowPaddedCols(text, pad, 44)
}

func contextGridRowPaddedCols(text string, pad, cols int) string {
	var b strings.Builder
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString("\x1b[38;2;101;123;131m□\x1b[39m")
	}
	for i := 0; i < pad; i++ {
		b.WriteString(" ")
	}
	b.WriteString(text)
	return b.String()
}

func (m *runMode) contextGridRow(text string) string {
	return contextGridRowWidth(text, 5, m.width)
}

func (m *runMode) contextGridRowPadded(text string, pad int) string {
	return contextGridRowWidth(text, pad, m.width)
}

// panelContextLines renders the /context usage panel. The layout was
// recovered from /tmp/agycmp/panels/ppref.context.empty.raw.txt on an
// empty session; cycle 224 (TODO §4 follow-up to cycle 190) extends
// it to non-empty sessions.
//
// The `└ Context Usage` title is bold + dim gray 238 (ppref line 14);
// the three system-file paths use dim gray 241; the "Related:" line is
// also dim gray 241; the legend is a dot-hint. Esc closes silently.
//
// On an empty session (m.session has no user or assistant messages)
// every right-side descriptor hardcodes the empty-state text REF
// captures: "0 tokens (0.0%)", "1.0M (100.0%)", and the
// "Estimated usage (awaiting generation)" preamble.
//
// On a non-empty session the backend does not surface per-message
// token counts, so /context computes a proxy via sessionTokenCounts:
// 1 token ≈ 4 chars of content (the rule of thumb for mixed
// English/code text), and each recorded tool call counts as a small
// fixed token budget. The descriptors and the title-row `<used>/1.0M
// tokens` reflect that estimate. The "awaiting generation" label
// becomes "(live session, K messages)".
//
// Per-row bullet colors match REF: row 3 ("Estimated usage…") is bold
// + dim 238; rows 4-6 are `◉` in different colors (blue / green /
// orange) — REF uses these to mark user / agent / tool categories;
// row 7 is a plain `□` in dim 241.
func (m *runMode) panelContextLines() []string {
	model := m.model
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}
	const contextMax = 1_000_000 // 1.0M-token window (REF keeps the magic number in the panel).
	// Cycle 289: a turn in flight has a third layout. 1.4.3 still shows
	// "Estimated usage (awaiting generation)", only the user/agent/tool
	// rows, and one grid cell per non-empty category — the system
	// categories appear once the turn completes.
	if m.streaming {
		return m.panelContextStreamingLines(model, contextMax)
	}
	if len(m.session) == 0 {
		return m.panelContextEmptyLines(model)
	}
	return m.panelContextLiveLines(model, contextMax)
}

// contextTailLines is the shared bottom of every /context state: the
// auto-loaded system files, the Related row and the key hint.
func contextTailLines() []string {
	return []string{
		"",
		"System files · auto-loaded",
		"\x1b[38;2;101;123;131m └ ~/.gemini/GEMINI.md\x1b[39m",
		"\x1b[38;2;101;123;131m └ ~/.gemini/skills/\x1b[39m",
		"\x1b[38;2;101;123;131m └ ~/.gemini/antigravity/skills/\x1b[39m",
		"\x1b[38;2;101;123;131m └ .agents/agents/\x1b[39m",
		"",
		"\x1b[38;2;101;123;131mRelated: /artifact · /skill · /rewind\x1b[39m",
		panelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"),
	}
}

// panelContextEmptyLines is the cycle-190 layout verbatim. Kept as a
// separate helper so the non-empty branch in panelContextLines can be
// diffed against it without dragging the colors / padding through the
// non-empty path. The (0.0%) descriptor is padded one column further
// right than the other rows — REF uses 6-space padding for it
// specifically (ppref.context.empty.raw.txt row 16).
func (m *runMode) panelContextEmptyLines(model string) []string {
	lines := []string{
		"\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m",
		m.contextGridRow(model + " · 0/1.0M tokens"),
		m.contextGridRowPadded("(0.0%)", 6),
		m.contextGridRow("\x1b[1m\x1b[38;2;88;110;117mEstimated usage (awaiting generation)\x1b[0m"),
		m.contextGridRow("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: 0 tokens (0.0%)"),
		m.contextGridRow("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: 0 tokens (0.0%)"),
		m.contextGridRow("\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: 0 tokens (0.0%)"),
		m.contextGridRow("\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M (100.0%)"),
	}
	return append(lines, contextTailLines()...)
}

// panelContextStreamingLines is the in-flight layout. The reference keeps the
// "Estimated usage (awaiting generation)" title while a turn streams and
// prints only the user/agent/tool rows; the four system categories are
// added after completion. Each non-empty category owns exactly one grid
// cell, and the free-space row always reports the full window.
//
// The in-flight numbers are estimates, and not the stored ones: steps that
// have not finished have no token count yet, so EstimateTokenCounter falls
// back to max(len/4, 1). Captured live at 120x40 against the 1.4.4 binary
// with a slow mock CloudCode stream (prompt "hello world"):
//
//	└ Context Usage
//	◉ ◉ □ □ …                    Gemini 3.8 Flash (Medium) · 5/1.0M tokens
//	□ □ □ □ …                    (0.0%)
//	□ □ □ □ …                    Estimated usage (awaiting generation)
//	□ □ □ □ …                    ◉ User messages: 2 tokens (0.0%)
//	□ □ □ □ …                    ◉ Agent responses: 3 tokens (0.0%)
//	□ □ □ □ …                    ◉ Tool calls: 0 tokens (0.0%)
//	□ □ □ □ …                    □ Free space: 1.0M (100.0%)
//
// The same session later in the stream reported 2 / 28 / 0 / 30 as the
// partial reply grew — i.e. len(text)/4, matching estimateStepTokens.
func (m *runMode) panelContextStreamingLines(model string, contextMax int) []string {
	user, agent, tool := m.streamingTokenCounts()
	used := user + agent + tool
	if used > contextMax {
		used = contextMax
	}
	free := contextMax - used
	if free < 0 {
		free = 0
	}
	pct := func(x int) string {
		if x <= 0 {
			return "(0.0%)"
		}
		return fmt.Sprintf("(%.1f%%)", float64(x)/float64(contextMax)*100)
	}
	fill := make([]string, 0, 3)
	if user > 0 {
		fill = append(fill, "\x1b[38;2;38;139;210m◉\x1b[39m")
	}
	if agent > 0 {
		fill = append(fill, "\x1b[38;2;133;153;0m◉\x1b[39m")
	}
	if tool > 0 {
		fill = append(fill, "\x1b[38;5;172m◉\x1b[39m")
	}
	rows := []struct {
		text string
		pad  int
	}{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, contextTokenLabel(used)), 5},
		{pct(used), 6},
		{"\x1b[1m\x1b[38;2;88;110;117mEstimated usage (awaiting generation)\x1b[0m", 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: %s tokens %s", contextTokenLabel(user), pct(user)), 5},
		{fmt.Sprintf("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: %s tokens %s", contextTokenLabel(agent), pct(agent)), 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: %s tokens %s", contextTokenLabel(tool), pct(tool)), 5},
		// The free-space label is the window size, not the subtraction:
		// the reference prints "1.0M" even with tokens in use (live
		// 1.4.4 streaming capture, and the completed layout below).
		{fmt.Sprintf("\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M %s", pct(free)), 5},
	}
	cols := m.width / 5
	if m.width <= 0 {
		cols = 44
	}
	if cols < 1 {
		cols = 1
	}
	out := []string{"\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m"}
	for i, r := range rows {
		out = append(out, contextGridRowFilled(r.text, r.pad, cols, fill, i*cols))
	}
	return append(out, contextTailLines()...)
}

// streamingTokenCounts is sessionTokenCounts adjusted for the step that is
// still in flight: its user message has no stored token count yet (the
// reference falls back to max(len(raw)/4, 1)) and its assistant text is
// estimated the same way. Completed steps keep their stored counts.
func (m *runMode) streamingTokenCounts() (user, agent, tool int) {
	user, _, tool = m.sessionTokenCounts()
	for i := len(m.session) - 1; i >= 0; i-- {
		msg := m.session[i]
		if msg.Role != "user" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		user -= backend.UserContentTokenCount(wire)
		if n := len(msg.Content) / 4; n > 1 {
			user += n
		} else {
			user += 1
		}
		break
	}
	if user < 0 {
		user = 0
	}
	completed := int(atomic.LoadInt64(&m.sessionCompletionTokens) -
		atomic.LoadInt64(&m.lastTurnCompletionTokens))
	if completed < 0 {
		completed = 0
	}
	agent = completed + len(m.liveReplyBuf)/4
	return user, agent, tool
}

// panelContextLiveLines is the non-empty branch. 1.4.3 adds four
// system categories (prompt, tools, skills, subagents) that the old
// 1.4.2 capture did not have, and labels row 3 "Token usage by
// category". The grid cells on the first row are colour-coded by
// category in the reference; the left-hand grid is one continuous
// 24-cell-wide map, not a fresh bar per row.
func (m *runMode) panelContextLiveLines(model string, contextMax int) []string {
	user, agent, tool := m.sessionTokenCounts()
	sysPrompt, skills, subagents := m.promptSectionTokens()
	sysTools := m.toolTokenEstimate()
	used := user + agent + tool + sysPrompt + sysTools + skills + subagents
	if used > contextMax {
		used = contextMax
	}
	free := contextMax - used
	if free < 0 {
		free = 0
	}
	pct := func(x int) string {
		if x <= 0 {
			return "(0.0%)"
		}
		return fmt.Sprintf("(%.1f%%)", float64(x)/float64(contextMax)*100)
	}

	// The first grid row is filled in category order: system prompt,
	// user, agent, tool, system tools, skills, subagents. Each
	// category consumes roughly one cell per 4k tokens (minimum one
	// for any non-zero category), which reproduces the reference's
	// first-row mix at this session size.
	type gridCat struct {
		tokens int
		cell   string
	}
	gray := func(glyph string) string { return "\x1b[38;2;88;110;117m" + glyph + "\x1b[39m" }
	cats := []gridCat{
		{sysPrompt, gray("◉")},
		{user, "\x1b[38;2;38;139;210m◉\x1b[39m"},
		{agent, "\x1b[38;2;133;153;0m◉\x1b[39m"},
		{tool, "\x1b[38;5;172m◉\x1b[39m"},
		{sysTools, gray("◉")},
		{skills, gray("◉")},
		{subagents, gray("◉")},
	}
	var fill []string
	for _, c := range cats {
		if c.tokens <= 0 {
			continue
		}
		n := (c.tokens + 3999) / 4000
		if n < 1 {
			n = 1
		}
		for i := 0; i < n; i++ {
			fill = append(fill, c.cell)
		}
	}

	rows := []struct {
		text string
		pad  int
	}{
		{fmt.Sprintf("%s · %s/1.0M tokens", model, contextTokenLabel(used)), 5},
		{pct(used), 6},
		{"\x1b[1m\x1b[38;2;88;110;117mToken usage by category\x1b[0m", 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: %s tokens %s", contextTokenLabel(user), pct(user)), 5},
		{fmt.Sprintf("\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: %s tokens %s", contextTokenLabel(agent), pct(agent)), 5},
		{fmt.Sprintf("\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: %s tokens %s", contextTokenLabel(tool), pct(tool)), 5},
		{fmt.Sprintf("%s System prompt: %s tokens %s", gray("⛁"), contextTokenLabel(sysPrompt), pct(sysPrompt)), 5},
		{fmt.Sprintf("%s System tools: %s tokens %s", gray("⛁"), contextTokenLabel(sysTools), pct(sysTools)), 5},
		{fmt.Sprintf("%s Skills: %s tokens %s", gray("⛁"), contextTokenLabel(skills), pct(skills)), 5},
		{fmt.Sprintf("%s Subagents: %s tokens %s", gray("⛁"), contextTokenLabel(subagents), pct(subagents)), 5},
		{fmt.Sprintf("\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M %s", pct(free)), 5},
	}
	cols := m.width / 5
	if m.width <= 0 {
		cols = 44
	}
	if cols < 1 {
		cols = 1
	}
	out := make([]string, 0, len(rows)+8)
	out = append(out, "\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m")
	for i, r := range rows {
		out = append(out, contextGridRowFilled(r.text, r.pad, cols, fill, i*cols))
	}
	out = append(out, contextTailLines()...)
	return out
}

// toolTokenEstimate measures the tool declarations the clone actually sends:
// the JSON array the CloudCode envelope carries, divided by three. Marshalling
// m.tools directly undercounts because it skips the geminiToolSchema transform
// buildEnvelope applies.
//
// 【存疑】 The reference prints 4.6k for its 12 declarations (13,449 B on
// the wire), which is more than len/3 (4,483): its tools row appears to use
// a real tokenizer rather than EstimateTokenCounter's 3-byte divisor. This
// clone has no tokenizer, so the row is the same /3 estimate the other
// categories use, and it measures our own wire. Since cycle 293 the surface
// itself matches (12 declarations, 13,152 B), so the remaining 0.2k gap is
// the tokenizer alone.
func (m *runMode) toolTokenEstimate() int {
	b := backend.WireToolsJSON(m.wireToolDecls())
	return len(b) / 3
}

// wireToolDecls is the declaration list the turn actually sends.
//
// Cycle 293: agent.Loop.pigTools now keeps the captured declaration when an
// ExtraAgentTool owns the same name (declOverrideTool), so the subagent
// extras no longer replace anything on the wire. The advertised list is
// simply what EnableTools installed: the twelve captured declarations for the
// default agent, or the custom agent's seven.
func (m *runMode) wireToolDecls() []backend.ToolDecl {
	return append([]backend.ToolDecl(nil), m.tools...)
}

// contextTokenLabel formats a token count the way 1.4.3's /context
// does: 702, 4.6k, 11.5k, 1.0M.
func contextTokenLabel(n int) string {
	// Promote values that would round to "1000.0k": the reference prints
	// the free-space row as "1.0M", not "1000.0k" (live 1.4.3 capture,
	// 999,987 tokens free).
	if n >= 999_950 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.Itoa(n)
}

// contextGridRowFilled emits one grid row using the category-coloured
// cells in fill, starting at offset. Rows past the fill are empty `□`.
func contextGridRowFilled(text string, pad, cols int, fill []string, offset int) string {
	var b strings.Builder
	for i := 0; i < cols; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		if idx := offset + i; idx >= 0 && idx < len(fill) {
			b.WriteString(fill[idx])
		} else {
			b.WriteString("\x1b[38;2;101;123;131m□\x1b[39m")
		}
	}
	for i := 0; i < pad; i++ {
		b.WriteString(" ")
	}
	b.WriteString(text)
	return b.String()
}

// sessionTokenCounts returns the per-category token counts /context shows,
// using the reference's own accounting rather than a uniform chars-per-token
// estimate. Every rule here was recovered from the 1.4.3 binary (see
// chatTokensByCategory / stepTokens in
// docs/cycles/cycle-292-context-tokens/) and re-measured live against 1.4.4:
//
//   - User messages: len(<USER_REQUEST> envelope)/3 + 6. The envelope is the
//     text actually sent, so the row moves when the model label in the
//     settings-change block gets longer. Wire-less seeded history falls back
//     to wrapping its raw text the same way.
//   - Agent responses: the session's completed-step completion tokens minus
//     the step currently landing. A text-only first turn reports 0; after a
//     tool turn 12; after a second text turn 12 again.
//   - Tool calls: (len(raw result)+1)/3 + 6 per call, over the raw
//     functionResponse.output text, not the rendered step detail (a write
//     step's detail is a line count).
func (m *runMode) sessionTokenCounts() (user, agent, tool int) {
	for _, msg := range m.session {
		if msg.Role != "user" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		user += backend.UserContentTokenCount(wire)
	}
	agent = int(atomic.LoadInt64(&m.sessionCompletionTokens) -
		atomic.LoadInt64(&m.lastTurnCompletionTokens))
	if agent < 0 {
		agent = 0
	}
	for _, b := range m.blocks {
		for _, s := range b.steps {
			if s.kind == stepKindTool {
				tool += backend.ToolResultTokenCount(s.result)
			}
		}
	}
	for _, s := range m.liveSteps {
		if s.kind == stepKindTool {
			tool += backend.ToolResultTokenCount(s.result)
		}
	}
	return user, agent, tool
}

// ---- Cycle 193: seven A-class panels --------------------------------------

// searchCursorBlock is the `Search:` input cursor REF draws as one
// reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`). Both /model and
// /config reuse it.
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// searchUnderline is the 20-cell `─` row REF draws under the Search:
// input, in separator grey (256-colour 253).
const searchUnderline = "\x1b[38;2;238;232;213m────────────────────\x1b[39m"

// modelRowType is one picker row REF renders between the Search field
// and the effort slider. `display` is the label-column text REF renders:
// the Gemini row drops the "(High)" tier (the tier lives on the effort
// slider instead), the dim "Gemini 4 Argon" row is drawn in gray 241
// (unavailable on this account — see panelModelLines). `slug` is the
// catalogue ID prefix; the effort suffix is appended from the slider.
type modelRowType struct {
	display string // label column, verbatim from REF
	slug    string // catalogue base ID (no effort suffix)
	dim     bool   // row is drawn in dim gray 241
	note    string // optional trailing description (custom registry models)
}

// modelRows is the static, always-present portion of the /model picker.
// Order is the live 1.4.3 list captured at 120x40, with the clone-only
// MiniMax M3 row kept before the unavailable Argon placeholder: Gemini
// 3.8 Flash, Claude Sonnet 4.6, Claude Opus 4.6, GPT-OSS 120B,
// MiniMax M3, Gemini 4 Argon. The reference does not advertise MiniMax
// (it remains selectable with --model minimax-m3), but the agw gateway
// serves it and the picker deliberately keeps it available.
// Cycle model-catalog-claude-55 adds Claude Opus 5.5 / Claude Sonnet 5.5
// as a conditional suffix via modelRows55(); the picker iterates the
// concatenation (see modelRowsView).
var modelRows = []modelRowType{
	{"Gemini 3.8 Flash", "gemini-3.8-flash", false, ""},
	{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4-6", false, ""},
	{"Claude Opus 4.6 (Thinking)", "claude-opus-4-6-thinking", false, ""},
	{"GPT-OSS 120B (Medium)", "gpt-oss-120b-medium", false, ""},
	{"MiniMax M3", "minimax-m3", false, "MiniMax M3 via agw (openagy wrapper)"},
	{"Gemini 4 Argon", "", true, ""}, // slug unknown (not in `agy models`); cannot be selected
}

// modelRows55 returns the conditional Claude Opus 5.5 / Claude Sonnet
// 5.5 picker rows. Empty unless cli.Claude55Available() is true —
// the AGY 1.3.6 changelog line 1 contract ("Claude models now appear
// in /model only if your Google Cloud project can use them").
//
// Returning a fresh slice on each call lets tests toggle the env
// vars between renders without restarting the binary, and avoids
// mutating the caller's backing array.
func modelRows55() []modelRowType {
	if !cli.Claude55Available() {
		return nil
	}
	return []modelRowType{
		{"Claude Opus 5.5", "claude-opus-5-5", false, ""},
		{"Claude Sonnet 5.5", "claude-sonnet-5-5", false, ""},
	}
}

// modelRowsView returns the full picker view: modelRows + any
// conditional 5.5 entries. The picker always renders modelRows first
// so cursor indices 0–6 are stable across ADC state changes; the 5.5
// rows are appended at the end (indices 7, 8) when present.
func modelRowsView() []modelRowType {
	extra := modelRows55()
	if len(extra) == 0 {
		return modelRows
	}
	out := make([]modelRowType, 0, len(modelRows)+len(extra))
	out = append(out, modelRows...)
	out = append(out, extra...)
	return out
}

// panelModelLines renders the /model picker. Byte-for-byte layout
// recovered from /tmp/agycmp/panels/ppref.model.raw.txt lines 12–28:
//
//	Switch Model
//	                        ← blank
//	  Search: ▮
//	          ────────────────────
//	                        ← blank
//	> Gemini 3.8 Flash             (current)      ← pad to col 30, gray238
//	  Claude Sonnet 4.6 (Thinking)
//	  Claude Opus 4.6 (Thinking)
//	  GPT-OSS 120B (Medium)
//	  MiniMax M3                    MiniMax M3 via agw (openagy wrapper)
//	  Gemini 4 Argon                             ← dim gray 241
//	                        ← blank
//	  Effort  ◂        ●━━━...━◉        ▸        ← /effort bar +8 cells
//	                  low          medium          high
//	            Deepest reasoning…                ← +8 cells, no "(current)"
//	                        ← blank
//	Keyboard: ↑/↓ Navigate  ←/→ Effort  enter Select  esc Go Back
//
// Cycle 197 deepens: the `(current)` annotation is computed live from
// settings.json's "model" key (slug or label — both resolve), `←/→`
// advances m.panel.effortCursor (Gemini only; the other rows are fixed
// single-effort), and `enter` persists the selection and closes.
func (m *runMode) panelModelLines() []string {
	lines := []string{
		panelTitleOpen + "Switch Model" + panelTitleClose,
		"",
		"  Search: " + searchCursorBlock,
		"          " + searchUnderline,
		"",
	}
	cursor := m.panel.cursor
	rows := modelRowsView()
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	currentSlug := resolveCatalogueID(m.panelCurrentModelLabel())
	for i, r := range rows {
		isCurrent := r.slug != "" && r.catalogueID(m.panel.effortCursor) == currentSlug
		var row string
		ink := ""
		if r.dim {
			ink = cDimGrayOpen
		}
		if i == cursor {
			// Cursor row: "> " + display, padded to col 30 in brand
			// blue, then the gray annotation (ppref.model.raw.txt:18).
			// Cycle 208: the pad exists only to align the `(current)`
			// annotation — an un-annotated cursor row is NOT padded
			// (fin_model_down.raw.txt row 6 has no trailing space).
			row = cBlueOpen + "> " + r.display
			if isCurrent {
				row += strings.Repeat(" ", modelRowPad(r.display))
			}
			row += cBlueClose
			if isCurrent {
				row += panelGray238 + " (current)" + panelGray238C
			}
		} else {
			row = "  " + ink + r.display
			if ink != "" {
				row += cDimGrayClose
			}
			if isCurrent {
				row += strings.Repeat(" ", modelRowPad(r.display)) +
					panelGray238 + " (current)" + panelGray238C
			}
		}
		if r.note != "" {
			// Registry-provided description: two columns past the
			// (current) annotation, in 256-colour 60. Captured live
			// from 1.4.3 with the agw MiniMax entry loaded.
			row += strings.Repeat(" ", modelRowPad(r.display)+2) +
				"\x1b[38;5;60m" + r.note + "\x1b[39m"
		}
		lines = append(lines, row)
	}
	// Cycle 208: the slider (and its legend entry) only exist while the
	// cursor is on the three-tier Gemini row.
	if !m.modelRowHasEffortSlider() {
		lines = append(lines, "",
			panelKeyboardHint("↑/↓", "Navigate",
				"enter", "Select", "esc", "Go Back"))
		return lines
	}
	// Slider block: the /effort panel's bar/labels/desc shifted right by
	// 8 cells behind a "  Effort  " prefix, and without the desc's
	// "(current)" annotation (ppref.model.raw.txt:24–26).
	level := m.panel.effortCursor
	if level < 0 || level > 2 {
		level = 1
	}
	eff := rebuildEffortSlider(level, effortLow, false)
	lines = append(lines, "",
		"  Effort  "+eff[2][2:],
		"        "+eff[3],
		"            "+stripLeadingSpaces(eff[4], 4),
		"",
		panelKeyboardHint("↑/↓", "Navigate", "←/→", "Effort",
			"enter", "Select", "esc", "Go Back"))
	return lines
}

// catalogueID returns the full catalogue ID a picker row represents at
// the given effort cursor. Only the three-tier Gemini encodes the
// cursor; the other rows are fixed IDs.
func (r modelRowType) catalogueID(effortCursor int) string {
	if r.slug == "gemini-3.8-flash" {
		return r.slug + "-" + effortSlugForModelCursor(r.slug, effortCursor)
	}
	return r.slug
}

// resolveCatalogueID maps a settings.json "model" value (catalogue
// slug or display label) to the canonical catalogue slug. Returns ""
// for custom models such as "MiniMax-M3".
func resolveCatalogueID(val string) string {
	val = strings.TrimSpace(val)
	for _, e := range cli.ReferenceModels() {
		if e.ID == val || e.Label == val {
			return e.ID
		}
	}
	return ""
}

// stripLeadingSpaces drops up to n leading spaces (used to re-indent
// the borrowed /effort slider rows for the /model embed).
func stripLeadingSpaces(s string, n int) string {
	for n > 0 && len(s) > 0 && s[0] == ' ' {
		s = s[1:]
		n--
	}
	return s
}

// cfgVal is one stopping point of a setting's ←/→ cycle, as measured
// against the reference with /tmp/agycmp/probe_ref_walk2.py and
// probe_ref_vocab.py. `stored` is the JSON value the reference writes;
// nil means the key is *absent* from settings.json.
//
// The absence convention is a load-bearing finding: the reference
// represents every setting at its default by removing the key. Cycling
// Agent Mode back to "default" deletes `agentMode`; Copy on Select's
// "on" is absent while "off" is JSON false. Writing the literal default
// (e.g. `agentMode: "default"`) makes 1.4.0 reject the whole file with
//
//	⚠ Settings Error
//	  invalid settings: agentMode: unrecognized value "default"
//
// and silently run with defaults — so the clone must delete, not write.
type cfgVal struct {
	display string
	stored  any
}

// configRow is one rendered line of the /config picker. The table below is
// the reference's complete 21-row catalogue, recovered verbatim from
// /tmp/agycmp/probe_ref_scroll.py (all 20 top-level rows at height 50):
//
//	Agent Mode                   default            agentMode
//	Animation Speed              medium             runningLightSpeed
//	Artifact Review              always proceeds    artifactReviewPolicy
//	Color Scheme                 light              colorScheme
//	Conversation Picker Grouping none               pickerGrouping
//	Copy on Select               on                 copyOnSelect
//	Editor                       auto ($EDITOR)     editor
//	Editor Mode                  default            editorMode
//	Editor Mode › Insert First   off                vimInsertFirst   ← nested, only when Editor Mode = vim
//	Enable Telemetry             on                 (not persisted)
//	Non-Workspace Access         off                allowNonWorkspaceAccess
//	Notifications                off                notifications
//	Queued Messages              Queue              queuedMessages
//	Rendering Mode               no flickering …    altScreenMode
//	Sandbox Mode                 off                enableTerminalSandbox
//	Show Active Tasks            on                 hideActiveItems   ← inverted
//	Show Feedback Survey         on                 showFeedbackSurvey
//	Show Tips                    on                 showTips
//	Tool Permission              always-proceed     toolPermission
//	Use AI Credits               off                useG1Credits
//	Verbosity                    medium             verbosity
//
// Every value list and every persisted form was read off the reference by
// cycling ←/→ and diffing settings.json (probe_ref_walk2.py); the row→key
// mapping below is that probe's diff table, not an inference.
type configRow struct {
	name        string
	settingsKey string
	values      []cfgVal
	description string
	// showWhenKey/showWhenVal gate a child row on another setting's raw
	// value ("Editor Mode › Insert First" is drawn only while
	// editorMode == "vim").
	showWhenKey string
	showWhenVal string
	// memoryOnly: the reference toggles the value but writes nothing to
	// settings.json (Enable Telemetry). The clone keeps it in panel
	// state so the row still responds.
	memoryOnly bool
}

// cv builds a cfgVal whose stored form is the given string, or nil
// (absent) when s is "".
func cfgv(display, stored string) cfgVal {
	if stored == "" {
		return cfgVal{display: display, stored: nil}
	}
	return cfgVal{display: display, stored: stored}
}

// cbv builds a boolean cfgVal. The default reads as absent (nil);
// the non-default persists as a JSON bool.
func cfgb(display string, stored any) cfgVal {
	return cfgVal{display: display, stored: stored}
}

// configSettingsRows is the static catalogue. Rows whose showWhen gate is
// not met are dropped by configRows().
var configSettingsRows = []configRow{
	{name: "Agent Mode", settingsKey: "agentMode",
		values: []cfgVal{
			cfgv("default", ""), cfgv("accept-edits", "accept-edits"),
			cfgv("auto", "auto"), cfgv("plan", "plan"),
		},
		description: "'default': standard behavior. 'accept-edits': auto-approve file edits, prompt for commands. 'auto': auto-approve edits and safe commands, prompt on irreversible actions. 'plan': research and plan without making changes"},
	{name: "Animation Speed", settingsKey: "runningLightSpeed",
		values: []cfgVal{
			cfgv("medium", ""), cfgv("slow", "slow"), cfgv("off", "off"), cfgv("fast", "fast"),
		},
		description: "Speed of the running light animation"},
	{name: "Artifact Review", settingsKey: "artifactReviewPolicy",
		values: []cfgVal{
			cfgv("asks for review", ""), cfgv("always proceeds", "always-proceed"),
			cfgv("agent decides", "agent-decides"),
		},
		description: "Agent always asks for review."},
	{name: "Color Scheme", settingsKey: "colorScheme",
		values: []cfgVal{
			cfgv("terminal", ""), cfgv("light", "light"),
			cfgv("solarized light", "solarized light"),
			cfgv("colorblind-friendly light", "colorblind-friendly light"),
			cfgv("dark", "dark"), cfgv("solarized dark", "solarized dark"),
			cfgv("colorblind-friendly dark", "colorblind-friendly dark"),
			cfgv("tokyo night", "tokyo night"),
		},
		description: ""},
	{name: "Conversation Picker Grouping", settingsKey: "pickerGrouping",
		values: []cfgVal{
			cfgv("none", ""), cfgv("grouped", "grouped"),
		},
		description: "How the /resume picker organizes conversations. Press ctrl+f in the picker to switch view for one session without changing this."},
	{name: "Copy on Select", settingsKey: "copyOnSelect",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", false)},
		description: "When enabled, releasing a mouse text selection copies it to the system clipboard. Only active in alt-screen mode. When disabled, the artifact viewer uses native terminal selection instead."},
	{name: "Editor", settingsKey: "editor",
		values: []cfgVal{
			cfgv("auto ($EDITOR)", ""), cfgv("vim", "vim"), cfgv("emacs", "emacs"),
		},
		description: "Preferred editor for viewing artifacts"},
	{name: "Editor Mode", settingsKey: "editorMode",
		values:      []cfgVal{cfgv("default", ""), cfgv("vim", "vim")},
		description: "Configure how you edit the prompt (default or vim)"},
	{name: "Editor Mode \u203a Insert First", settingsKey: "vimInsertFirst",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Place vim in insert mode when the prompt gains focus.",
		showWhenKey: "editorMode", showWhenVal: "vim"},
	{name: "Enable Telemetry", settingsKey: "enableTelemetry",
		// agy 1.4.3 defaults this row to off on a clean settings.json
		// (live capture at 120x40, 2026-10-20); the 1.4.1 default was on.
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "When toggled on, Pigasus CLI collects usage data to help Google enhance performance and features",
		memoryOnly:  true},
	{name: "Non-Workspace Access", settingsKey: "allowNonWorkspaceAccess",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Allow agent to access files outside workspace directories"},
	{name: "Notifications", settingsKey: "notifications",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Bell + desktop notification on tool confirmation and agent completion"},
	{name: "Queued Messages", settingsKey: "queuedMessages",
		values: []cfgVal{
			cfgv("Queue", ""), cfgv("Send Immediately", "send-immediately"),
		},
		description: "Configure when follow-up messages are sent."},
	{name: "Rendering Mode", settingsKey: "altScreenMode",
		values: []cfgVal{
			cfgv("native terminal (inline)", ""), cfgv("no flickering (altscreen)", "always"),
		},
		description: "no flickering (altscreen) is ideal for long conversations but requires holding Shift/Option for native text selection. Native Terminal experience (inline) preserves native terminal behavior but may truncate long conversations and flicker when toggling views."},
	{name: "Sandbox Mode", settingsKey: "enableTerminalSandbox",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Enables terminal sandbox restrictions."},
	{name: "Show Active Tasks", settingsKey: "hideActiveItems",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", true)},
		description: "Show active subagents and background tasks below the input box"},
	{name: "Show Feedback Survey", settingsKey: "showFeedbackSurvey",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", false)},
		description: "Show feedback survey"},
	{name: "Show Tips", settingsKey: "showTips",
		values:      []cfgVal{cfgb("on", nil), cfgb("off", false)},
		description: "Show helpful tips while the agent is generating"},
	{name: "Tool Permission", settingsKey: "toolPermission",
		values:      []cfgVal{cfgv("always-proceed", ""), cfgv("strict", "strict")},
		description: "Controls whether terminal commands require your approval before running."},
	{name: "Use AI Credits", settingsKey: "useG1Credits",
		values:      []cfgVal{cfgb("off", nil), cfgb("on", true)},
		description: "Use your AI credits for model calls once your plan's quota runs out"},
	{name: "Verbosity", settingsKey: "verbosity",
		values: []cfgVal{
			cfgv("medium", ""), cfgv("low", "low"), cfgv("high", "high"),
		},
		description: "Controls how much detail is shown for the agent's tool calls, commands, and thoughts."},
}

// configRows returns the catalogue filtered by the dynamic gates — today
// only the "Editor Mode › Insert First" child row, which the reference
// draws immediately after Editor Mode while editorMode == "vim"
// (probe_ref_walk2.py row 8).
func configRows() []configRow {
	out := make([]configRow, 0, len(configSettingsRows)+1)
	for _, r := range configSettingsRows {
		if r.showWhenKey != "" {
			if raw, _ := auth.SettingsField(r.showWhenKey); rawString(raw) != r.showWhenVal {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// rawString renders a settings.json leaf as the string form used for
// gating comparisons.
func rawString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "on"
		}
		return "off"
	}
	return ""
}

// configSettingRaw returns the JSON leaf exactly as REF would compare it:
// the stored value, or nil when the key is absent (the default state).
func configSettingRaw(rowIdx int) any {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return nil
	}
	raw, err := auth.SettingsField(rows[rowIdx].settingsKey)
	if err != nil {
		return nil
	}
	return raw
}

// configSettingValue returns the display string for one /config row —
// what the picker paints in the value column.
func configSettingValue(rowIdx int) string {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return ""
	}
	return configDisplayFor(rows[rowIdx], configSettingRaw(rowIdx))
}

// configDisplayFor maps a stored leaf back to the display form of the
// matching cycle value. A value not present in the list (e.g. a
// `toolPermission` explicitly written as "always-proceed" while the
// default is absent, or a hand-edited colour) falls back to the first
// entry, which is the reference's own behaviour.
func configDisplayFor(row configRow, raw any) string {
	// Cycle 241 (AGY 1.4.1): Business accounts see the `Enable Telemetry`
	// row painted as `off (disabled)` regardless of what settings.json
	// (or, since it's memoryOnly, what m.panel.configMemory) holds. The
	// check sits in this lowest-level display function so the test,
	// configRowValueAt, configSettingValue, and the live panel render all
	// agree on the Business-account locked display.
	if isTelemetryRow(row) && IsBusinessAccount() {
		return telemetryDisabledValue
	}
	for _, v := range row.values {
		if jsonLeafEqual(v.stored, raw) {
			return v.display
		}
	}
	// The raw value names one of the *stored* strings even though the
	// list's default entry stores nil (Tool Permission).
	if s, ok := raw.(string); ok {
		for _, v := range row.values {
			if v.display == s {
				return v.display
			}
		}
	}
	return row.values[0].display
}

// jsonLeafEqual compares two settings.json leaves for cycle matching.
// nil (key absent) only equals nil.
func jsonLeafEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

// configVisibleRowsFor returns how many /config rows the reference draws
// at this terminal height. Measured with probe_ref_height.py:
//
//	h=20 → 3    h=24 → 6    h=28 → 9    h=32 → 12    h=40 → 18    h≥44 → all
//
// i.e. `height*3/4 - 12`, clamped to [1, len(catalogue)] — the same 3/4
// shape as helpListWindowFor, offset by the /config chrome (title,
// search box, description, keyboard legend). Returns 10 for an unknown
// height (before the first WindowSizeMsg) so the byte-exact snapshots and
// the unit tests are unchanged.
func configVisibleRowsFor(height int) int {
	if height <= 0 {
		return 10
	}
	n := height*3/4 - 12
	if total := len(configRows()); n > total {
		n = total
	}
	if n < 1 {
		n = 1
	}
	return n
}

// panelConfigLines renders the /config settings picker. Layout recovered
// from /tmp/agycmp/panels/ppref.config.raw.txt:
//   - bold "Settings" title
//   - "Search: ▮" + 20-cell `─` underline
//   - the catalogue rows; name padded to col 29, value at col 31.
//     Cursor row has both name + value in blue (38;5;69).
//   - selected-row description in dim grey 238
//   - `[W+1 of 20 items]` counter in dim grey 241 — only while the list
//     is windowed; the reference drops it when every row fits
//     (probe_ref_scroll.py at height 50 paints all 20 with no counter).
//   - Keyboard: ↑/↓ Navigate  ←/→ Change  enter Select  esc Clear Search/Exit
//
// Cycle 196 deepened the picker: values are read live from settings.json
// (auth.SettingsField) on every render, so ←/→ flips the value and
// persists before the next paint. Cycle 197 corrected the catalogue
// (all 21 rows, real keys, real value lists — see configRow) and made
// the window height-derived via configVisibleRowsFor.
func (m *runMode) panelConfigLines() []string {
	rows := configRowsFiltered(m.panel.configQuery)
	if m.panel.configSubmenu {
		return m.panelConfigSubmenuLines(rows)
	}
	visible := configVisibleRowsFor(m.height)
	searchContent := searchCursorBlock
	if m.panel.configQuery != "" {
		searchContent = m.panel.configQuery + searchCursorBlock
	}
	lines := []string{
		panelTitleOpen + "Settings" + panelTitleClose,
		"",
		"  Search: " + searchContent,
		"          " + searchUnderline,
		"",
	}
	if len(rows) == 0 {
		lines = append(lines, "  No matching settings found.", "")
		lines = append(lines, "", panelKeyboardHint("esc", "Clear Search/Exit"))
		return lines
	}
	cursor := m.panel.cursor
	if cursor < 0 || cursor >= len(rows) {
		cursor = 0
	}
	// Adjust window so cursor is always visible
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if cursor < m.panel.window {
		m.panel.window = cursor
	}
	if cursor >= m.panel.window+visible {
		m.panel.window = cursor - visible + 1
	}
	for i := m.panel.window; i < m.panel.window+visible && i < len(rows); i++ {
		row := rows[i]
		value := m.configRowValue(row)
		pad := configNamePad(row.name, 29)
		if i == cursor {
			// Cycle 208: two measured cursor-row forms — see
			// configCursorRowIsSingleSpan.
			if configCursorRowIsSingleSpan(row.name) {
				lines = append(lines,
					cBlueOpen+"> "+row.name+strings.Repeat(" ", pad)+value+cBlueClose)
			} else {
				lines = append(lines,
					cBlueOpen+"> "+row.name+cBlueClose+
						strings.Repeat(" ", pad)+
						cBlueOpen+value+cBlueClose)
			}
		} else {
			lines = append(lines, "  "+row.name+strings.Repeat(" ", pad)+value)
		}
	}
	desc := rows[cursor].description
	// Cycle 241 (AGY 1.4.1): for the locked `Enable Telemetry` row the
	// reference substitutes a Business-account-specific description so the
	// user understands *why* the row is non-togglable. The literal text is
	// pinned in business_account.go.
	if isTelemetryRow(rows[cursor]) && IsBusinessAccount() {
		desc = telemetryBusinessDescription()
	}
	lines = append(lines, "")
	for _, wrapped := range wrapPanelText(desc, m.width) {
		lines = append(lines, "  "+panelGray238+wrapped+panelGray238C)
	}
	lines = append(lines, "")
	// The counter slot is always present: the reference blanks it when
	// every row fits rather than collapsing the line (live REF capture
	// at height 50: rows, '', desc, '', '', '', Keyboard). Cycle 208: a
	// *windowed* list carries one more blank than a complete one before
	// the counter (REF at height 24: rows, '', desc, '', '', [W+1 of N],
	// '', Keyboard).
	if visible < len(rows) {
		// The description block above already ends with one blank row;
		// the counter follows it directly (live 1.4.3 at 120x40).
		lines = append(lines,
			cDimGrayOpen+fmt.Sprintf("  [%d-%d of %d items]",
				m.panel.window+1, m.panel.window+visible, len(rows))+cDimGrayClose)
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "", panelKeyboardHint("↑/↓", "Navigate", "←/→", "Change",
		"enter", "Select", "esc", "Clear Search/Exit"))
	return lines
}

// wrapPanelText word-wraps a /config description to width-2 content
// columns; the caller adds the two-space indent. agy 1.4.3 wraps the
// description across as many terminal rows as needed (the Agent Mode
// text fills exactly 120 columns and continues on the next row at
// 120x40), while bubbletea's renderer clips a single long line. width <= 0
// means unset (unit tests) and leaves the text unwrapped.
func wrapPanelText(s string, width int) []string {
	if width <= 2 {
		return []string{s}
	}
	avail := width - 2
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	out := []string{}
	line := words[0]
	for _, word := range words[1:] {
		if utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= avail {
			line += " " + word
			continue
		}
		out = append(out, line)
		line = word
	}
	return append(out, line)
}

// pluginColor253 is the xterm-253 grey REF uses for the inactive tab, the
// `[disabled]` badge, the Discover `●` bullets and the whole expanded details
// block. Emitted as a 256-colour code because the truecolour value behind it
// was not recovered; `capture-pane` normalises REF's RGB down to this index
// anyway (see the colour caveat at the top of this file).
const (
	pluginColor253  = "\x1b[38;2;238;232;213m"
	pluginColor253C = "\x1b[39m"
)

// panelPluginLines renders the /plugin picker. Layout recovered from
// /tmp/agycmp/panels/ppref.plugin.raw.txt and re-probed key-by-key in cycle
// 198 (/tmp/agycmp/probe_ref_plugin2.py, probe_ref_plugin3.py):
//
//	title        "\x1b[1m\x1b[39m  Plugins\x1b[0m"  (two leading spaces)
//	tab row      "  <Installed (n)>    <Discover (m)>   (tab to cycle)"
//	             the active tab is bold+underline+blue69 and its count is bold;
//	             the inactive tab is one grey-253 span.
//	search box   "  Type to search plugins..." grey 238
//	Installed    "> [enabled]  <name> <desc>" — the badge is green 29 with a
//	             trailing space inside the span, `[disabled]` is grey 253 with
//	             none, so both occupy ten columns; the name is bold blue on the
//	             cursor row and bold default-fg otherwise; the description is
//	             grey 241. Enter appends the five-line detail block.
//	Discover     "  ● <name> <desc>" (grey-253 bullet, bold name, grey-241
//	             description) behind a "> + Install from local directory..."
//	             row.
//
// Rows are truncated so the whole line fits terminal_width-2 columns, with
// "...": measured at 220 → 218 and 100 → 98 (/tmp/agycmp/probe_ref_width.py).
// The cut is mid-word, not word-wrapped.
//
// Esc closes silently (esc-ref-plugin-Esc.txt shows no `Exited` row).
//
// Cycle 198: m.panel.pluginTab selects the tab (panelInstalled / panelDiscover).
// The Installed tab lists the installed plugins with their enable badge; the
// Discover tab lists the marketplace entries behind the "+ Install from local
// directory..." row. See the cycle-198 report.
//
// Cycle 204 adds the Discover search box (space / a printable key focuses it,
// its text filters the list live and it carries a blinking reverse-video
// cursor in the reference), the Discover detail block `enter` opens, and the
// `←/→` first/last cursor jumps its footer announces. The box line, the legend
// and the two key paths live in slash_plugin_search.go; the details live there
// too (pluginDiscoverDetails). See docs/cycles/cycle-204/.
func (m *runMode) panelPluginLines() []string {
	// Cycle dpl (AGY 1.2.7): installed honours the "enabled-only
	// customization query" toggle (pluginEnabledOnly) before the search
	// filter narrows further. The filter only lives in panel state, so a
	// fresh panel re-derives the full list — that is what makes the
	// disabled rows reappear after panel close.
	installed := installedPluginRowsFiltered(m.panel.pluginEnabledOnly)
	mkts := pluginMarketplaces()
	discover := marketplaceItems(mkts)
	max := m.pluginLineMax()

	title := panelTitleOpen + "  Plugins" + panelTitleClose
	if m.panel.pluginLocalError != "" {
		title = panelTitleOpen + "  Plugins" + panelTitleClose +
			" \x1b[38;5;166m(Error: " + m.panel.pluginLocalError + ")\x1b[39m"
	}
	lines := []string{title}
	lines = append(lines, pluginTabHeader(m.panel.pluginTab, len(installed), len(discover)))
	lines = append(lines, m.pluginSearchBoxLine())

	if m.panel.pluginTab == panelDiscover {
		return append(lines, m.panelDiscoverLines(mkts, max)...)
	}

	// Cycle 219: when the Installed tab's box has focus and the typed
	// query has not yet been committed (Enter), the rows below stay
	// unfiltered — the user is still composing the query. After Enter,
	// the rows filter live.
	visible := installed
	if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		visible = pluginInstalledFilter(installed, m.panel.pluginQuery)
	}
	// The Installed count in the tab header reflects the filtered set
	// once the query has been committed — that is what the cycle-204
	// Discover header does. Before the commit the count is the full
	// installed total.
	headerInstalled := len(installed)
	if m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		headerInstalled = len(visible)
	}
	lines = []string{title}
	lines = append(lines, pluginTabHeader(m.panel.pluginTab, headerInstalled, len(discover)))
	lines = append(lines, m.pluginSearchBoxLine())

	if m.panel.pluginTab == panelDiscover {
		return append(lines, m.panelDiscoverLines(mkts, max)...)
	}

	cursor := m.pluginCursorFor(len(visible))
	for i, p := range visible {
		isCursor := i == cursor
		desc := pluginDescFor(p.name, mkts)
		lines = append(lines, pluginInstalledLine(isCursor, m.pluginEnabled(p.name), p.name, desc, max))
		if m.panel.pluginExpanded[p.name] {
			lines = append(lines, pluginInstalledDetails(p.name, mkts)...)
		}
	}
	switch {
	case m.panel.pluginSearch && m.panel.pluginQueryCommitted && len(visible) == 0:
		// Cycle 219: typed-search yielded no matches. The empty-state
		// line points at the live filter so the user knows how to
		// recover (Backspace / Esc clears the query).
		lines = append(lines,
			panelGray238+"  No installed plugins match your search."+panelGray238C,
			"")
	case m.panel.pluginEnabledOnly && len(visible) == 0:
		// Cycle dpl: an enabled-only customization query yielded no
		// matches (the user just disabled the last enabled row). The
		// reference shows this row instead of an empty list so the
		// user knows the disabled rows are still on disk — pressing
		// `/` once more re-toggles the filter.
		lines = append(lines,
			panelGray238+"  No enabled plugins — press `/` to show all."+panelGray238C,
			"")
	}
	lines = append(lines, "",
		m.pluginInstalledLegend())
	return lines
}

// panelDiscoverLines is the Discover tab body below its search box: the
// filtered entries (cycle 204) each followed by their detail block when
// expanded, the contextual footer, and — while the box has focus and nothing
// matches — the reference's "No marketplace plugins match your search." row.
//
// Measured at 220x50 (probes p16/p19): the no-match screen is
// box / message / blank / blank / Keyboard, i.e. one more blank than a screen
// with rows, so the empty branch appends its own trailing blank.
//
// Cycle 212: the local-install prompt replaces the local row while it is
// open and (only when the typed path is invalid) appends a hint line that
// shifts the rest of the panel down by one — see p1/p2/p4.out.
func (m *runMode) panelDiscoverLines(mkts []pluginMarketplace, max int) []string {
	rows := m.pluginDiscoverRows(mkts)
	searching := m.panel.pluginSearch
	prompt := m.pluginLocalInstallActive()
	offset := 0
	var lines []string
	if prompt {
		// The local-install row is now the prompt itself; the cursor is
		// parked there because the prompt owns every keypress.
		lines = append(lines, pluginLocalInstallRow(true,
			m.panel.pluginLocalInstall.query, m.panel.pluginBlinkOn))
		if h := pluginLocalInstallHint(m.panel.pluginLocalInstall.query); h != "" {
			lines = append(lines, h)
		}
	} else if !searching {
		// The "+ Install from local directory..." row heads the unfocused
		// list only; the box's focus hides it.
		offset = 1
		lines = append(lines, pluginLocalRow(m.pluginCursorFor(len(rows)+1) == 0))
	}
	cursor := m.pluginCursorFor(len(rows) + offset)
	installedSet := installedNames()
	for i, p := range rows {
		lines = append(lines, pluginDiscoverLine(cursor == i+offset, installedSet[p.name], p.name, p.desc, max))
		if m.panel.pluginExpanded[p.name] {
			lines = append(lines, pluginDiscoverDetails(p.name, mkts, max)...)
		}
	}
	if searching && len(rows) == 0 {
		lines = append(lines,
			panelGray238+"  No marketplace plugins match your search."+panelGray238C,
			"")
	}
	if prompt {
		lines = append(lines, "", pluginLocalInstallLegend())
	} else {
		lines = append(lines, "", m.pluginDiscoverLegend(!searching && cursor == 0))
	}
	return lines
}

// The two /plugin tabs.
const (
	panelInstalled = iota
	panelDiscover
)

// pluginTabHeader is the "  Installed (4)    Discover (17)   (tab to cycle)"
// row. Only the active tab carries the bold/underline/blue styling.
func pluginTabHeader(tab, nInstalled, nDiscover int) string {
	return "  " +
		pluginTabLabel("Installed", nInstalled, tab == panelInstalled) + "    " +
		pluginTabLabel("Discover", nDiscover, tab == panelDiscover) + " " +
		pluginColor253 + "  (tab to cycle)" + pluginColor253C
}

// pluginTabLabel renders one tab: grey-253 "Label (n)" when inactive, else
// bold+underline blue with a bold count (the reference keeps the underline on
// the space between the label and the count: `\x1b[0;4m`).
func pluginTabLabel(label string, n int, active bool) string {
	count := "(" + strconv.Itoa(n) + ")"
	if !active {
		return pluginColor253 + label + " " + count + pluginColor253C
	}
	return "\x1b[1;4m" + cBlueOpen + label + "\x1b[0;4m" + cBlueOpen + " " +
		"\x1b[1m" + count + "\x1b[0m"
}

// pluginBadge is the [enabled]/[disabled] tag. The ten-column width is fixed
// by keeping the trailing space inside the green span for [enabled] and
// leaving [disabled] a plain grey span.
func pluginBadge(enabled bool) string {
	if enabled {
		return "\x1b[38;2;133;153;0m[enabled] \x1b[39m"
	}
	return pluginColor253 + "[disabled]" + pluginColor253C
}

// pluginBlockedBadge is the third tag, rendered when the global plugin gate
// is on (AGY_CLI_DISABLE_PLUGINS env var or config.json's
// disableAllPlugins boolean). It is the AGY 1.2.16 changelog close-gap:
// /plugin previously showed "enabled" or "disabled" without indicating
// the row was also blocked by the global kill switch. The colour is
// 166 — the same red-orange the rest of the TUI uses for "error /
// blocked" rows (colors.go: cToolBulletErrOpen) — and the tag keeps the
// ten-column width the cycle-198 badge pins ("[blocked] " with a
// trailing space inside the span).
func pluginBlockedBadge() string {
	return "\x1b[38;5;166m[blocked] \x1b[39m"
}

// pluginRowState is the effective status string for one Installed row.
// The global gate wins: when auth.PluginsBlocked() is true every row
// reports "blocked" regardless of its per-plugin enabled flag. The
// panel rendering maps these three words to the matching badge.
func pluginRowState(enabled bool) string {
	if auth.PluginsBlocked() {
		return "blocked"
	}
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// pluginRowBadge is the badge for one Installed row, picking between the
// three cycle-235 states by the value of pluginRowState. It is the
// single point where "blocked" joins the existing "[enabled]" /
// "[disabled]" pair, so any future fourth state only needs to grow this
// switch.
func pluginRowBadge(enabled bool) string {
	switch pluginRowState(enabled) {
	case "blocked":
		return pluginBlockedBadge()
	case "enabled":
		return pluginBadge(true)
	default:
		return pluginBadge(false)
	}
}

// pluginInstalledLine renders one Installed row. `max` is the widest the whole
// line may be (terminal width − 2).
//
// Cycle 235: the badge comes from pluginRowBadge rather than pluginBadge
// directly, so a row that would otherwise say "[enabled]" / "[disabled]"
// gets the new "[blocked]" red-orange tag when the global plugin gate is
// on (env var or config.json's disableAllPlugins). The rest of the row
// layout is unchanged.
func pluginInstalledLine(isCursor, enabled bool, name, desc string, max int) string {
	// "  " + badge(10) + " " + name + " "
	desc = truncateTo(desc, max-(14+len(name)))
	badge := pluginRowBadge(enabled)
	if isCursor {
		return cBlueOpen + "> " + badge + " " +
			"\x1b[1m" + cBlueOpen + name + "\x1b[0m" + " " +
			cDimGrayOpen + desc + cDimGrayClose
	}
	return "  " + badge + " " +
		"\x1b[1m" + name + "\x1b[0m" + " " +
		cDimGrayOpen + desc + cDimGrayClose
}

// pluginDiscoverLine renders one Discover plugin row. The bullet is green 29
// when the plugin is installed and grey 253 otherwise — it tracks *installed*,
// not enabled (probed: disabling chrome-devtools-plugin on the Installed tab
// leaves its Discover bullet green). Only the `> ` prefix and the name change
// colour on the cursor row.
func pluginDiscoverLine(isCursor, installed bool, name, desc string, max int) string {
	desc = truncateTo(desc, max-(5+len(name)))
	bulletColor := pluginColor253
	if installed {
		bulletColor = "\x1b[38;2;133;153;0m"
	}
	bullet := bulletColor + "●" + pluginColor253C
	if isCursor {
		return cBlueOpen + "> " + bullet + " " +
			"\x1b[1m" + cBlueOpen + name + "\x1b[0m" + " " +
			cDimGrayOpen + desc + cDimGrayClose
	}
	return "  " + bullet + " " +
		"\x1b[1m" + name + "\x1b[0m" + " " +
		cDimGrayOpen + desc + cDimGrayClose
}

// pluginLocalRow is Discover's first row, "+ Install from local directory...".
// The legend's "enter / ctrl+s Enter Local Path" refers to it; the path prompt
// itself is not implemented (see the cycle-198 report).
func pluginLocalRow(isCursor bool) string {
	text := "\x1b[1m+ Install from local directory...\x1b[0m"
	if isCursor {
		return cBlueOpen + "> " + text
	}
	return "  " + text
}

// pluginInstalledDetails is the block Enter opens under an Installed row: six
// grey-253 lines indented five spaces, each label left-justified to 13 columns
// (so "Description:" gets one trailing space, "Path:" eight).
//
// Cycle 234: a sixth `Agents:` line is appended after `Skills:` so the
// detail block surfaces every subagent declared via the plugin's root
// `agents.json` (AGY 1.3.6 changelog line 13 + 35). When the plugin has no
// agents, the value is "-" — the same convention `Version:` uses for the
// missing manifest field — so the row count stays stable.
func pluginInstalledDetails(name string, mkts []pluginMarketplace) []string {
	man := readPluginManifest(name)
	skills := strings.Join(pluginSkills(name), ", ")
	agents := strings.Join(pluginAgents(name), ", ")
	if agents == "" {
		agents = "-"
	}
	version := man.Version
	if version == "" {
		version = "-"
	}
	return []string{
		pluginDetailLine("Description:", pluginDescFor(name, mkts)),
		pluginDetailLine("Skills:", skills),
		pluginDetailLine("Agents:", agents),
		pluginDetailLine("Marketplace:", marketplaceOf(name, mkts)),
		pluginDetailLine("Path:", pluginDir(name)),
		pluginDetailLine("Version:", version),
	}
}

// pluginDetailLine formats one detail row: five plain spaces, then the whole
// text in grey 253 (the leading space is inside the span).
func pluginDetailLine(label, value string) string {
	return "     " + pluginColor253 + " " + padRight(label, 13) + value + pluginColor253C
}

// padRight left-justifies s in a field of width n (no truncation).
func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// pluginLineMax is the widest /plugin line the reference emits: terminal width
// minus two (measured 220 → 218, 100 → 98). Before the first WindowSizeMsg
// (and in unit tests) the width is unknown, so return an effectively unbounded
// value.
func (m *runMode) pluginLineMax() int {
	if m.width <= 0 {
		return 1 << 20
	}
	return m.width - 2
}

// truncateTo shortens s to at most avail columns, appending "..." when it has
// to cut. avail below four columns is treated as "nothing fits".
func truncateTo(s string, avail int) string {
	if avail < 4 || len(s) <= avail {
		return s
	}
	return s[:avail-3] + "..."
}

// pluginEnabled reads the badge state from ~/.gemini/config/config.json. A read
// failure shows [disabled] rather than a stale value.
func (m *runMode) pluginEnabled(name string) bool {
	on, err := auth.PluginEnabled(name)
	return err == nil && on
}

// pluginCursorFor clamps the panel cursor to a row count.
func (m *runMode) pluginCursorFor(n int) int {
	if m.panel.cursor < 0 || (n > 0 && m.panel.cursor >= n) {
		return 0
	}
	if n == 0 {
		return 0
	}
	return m.panel.cursor
}

// pluginRowCount is the cursor bound of the tab currently shown: the Discover
// tab carries its "+ Install from local directory..." row ahead of the
// marketplace entries while unfocused, and only the filtered entries while the
// search box has focus (cycle 204: pluginDiscoverCount). Cycle 219: the
// Installed tab's bound uses the focused-mode "un-committed = full list,
// committed = filtered list" rule too (see pluginSearchCount).
//
// Cycle dpl (AGY 1.2.7): the Installed tab's bound also honours the
// `pluginEnabledOnly` filter — disabling the last enabled row while the
// filter is active must clamp the cursor to row 0 of an empty list, not
// keep it on a row index that no longer exists.
func (m *runMode) pluginRowCount() int {
	if m.panel.pluginTab == panelDiscover {
		return m.pluginDiscoverCount()
	}
	if m.panel.pluginSearch {
		return m.pluginSearchCount()
	}
	return len(installedPluginRowsFiltered(m.panel.pluginEnabledOnly))
}

// togglePluginEnabled flips the highlighted Installed row's
// plugins.<name>.enabled flag in ~/.gemini/config/config.json (probed: the
// toggle persists across launches; the badge re-reads the flag each frame).
//
// Cycle psif: the toggle acts on the row the cursor visually points at.
// That is the full installed list while the box is unfocused, and the
// filter result once the typed query has been committed (Enter) — see
// installedActiveRows.
func (m *runMode) togglePluginEnabled() {
	rows := m.installedActiveRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].name
	on, err := auth.PluginEnabled(name)
	if err != nil {
		return
	}
	_ = auth.SetPluginEnabled(name, !on)
}

// uninstallHighlightedPlugin removes the plugin under the cursor on the
// Installed tab. It is the ctrl+s handler — the legend has listed the
// action since cycle 198, but this commit is the first to wire the
// dispatcher to a real path.
//
// Steps:
//
//  1. Read the row list (the cycle-198 reader; the disabled-filter and
//     the typed-search filter compose on top of it but a ctrl+s on a
//     visible row targets that row directly — so we read the same set
//     the cursor is pointing at, not the unfiltered disk scan).
//  2. Resolve the highlighted name.
//  3. Call uninstallFeedback (which runs validatePluginID first). The
//     return string is the message we paint in the panel title — empty
//     on success means the title reverts to its cycle-198 form.
//
// On success the directory disappears, config.json's row is cleared,
// and the next frame's installedPluginRowsFiltered call produces a
// shorter list; the cursor is clamped to row 0 by pluginCursorFor.
func (m *runMode) uninstallHighlightedPlugin() (string, error) {
	rows := installedPluginRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		// Cursor lands in an empty list; the keystroke is a no-op.
		return "", nil
	}
	name := rows[i].name
	msg, err := uninstallFeedback(name)
	if err != nil && msg == "" {
		// Defensive: uninstallFeedback always returns a non-empty
		// message on error. If it does not, surface the bare error so
		// the panel still says *something* went wrong.
		return "Error: " + err.Error(), err
	}
	// Drop any cached detail-block expansion so the next render does
	// not leave a stale "Description:" / "Skills:" block under a row
	// that no longer exists.
	if m.panel.pluginExpanded != nil {
		delete(m.panel.pluginExpanded, name)
	}
	return msg, err
}

// togglePluginExpanded shows/hides the detail block under the highlighted
// Installed row. Expansion is tracked per plugin name, not by cursor index:
// the reference leaves a block open when the cursor moves away (probed on the
// Discover tab, where the cursor jumped back to row 0 with a block still
// open).
//
// Cycle psif: same active-rows rule as togglePluginEnabled — once a
// focused-mode commit has narrowed the list, Enter on a committed
// Installed box opens the block under the filtered row the cursor is
// parked on, not the same-index row of the full list.
func (m *runMode) togglePluginExpanded() {
	rows := m.installedActiveRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].name
	if m.panel.pluginExpanded == nil {
		m.panel.pluginExpanded = map[string]bool{}
	}
	if m.panel.pluginExpanded[name] {
		delete(m.panel.pluginExpanded, name)
		return
	}
	m.panel.pluginExpanded[name] = true
}

// installedActiveRows is the Installed-tab row list the cursor
// currently indexes. With no focused-mode commit it is the full
// installed list; once the typed query has been committed (Enter) it
// is the filter result, so a Space / Enter on the focused state acts
// on the row the cursor visually points at.
//
// Cycle psif: previously both togglePluginEnabled and
// togglePluginExpanded read installedPluginRows() unconditionally,
// which indexed the cursor into the *full* list even after a commit
// narrowed the visible rows. With more than one installed plugin the
// cursor and the action would disagree — Space would flip the wrong
// row's enabled flag, Enter would open the wrong row's detail block.
// The fix is one helper that both callers share.
func (m *runMode) installedActiveRows() []pluginRow {
	rows := installedPluginRows()
	if m.panel.pluginTab == panelInstalled &&
		m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		return pluginInstalledFilter(rows, m.panel.pluginQuery)
	}
	return rows
}

// skillsFromDisk enumerates ~/.gemini/skills/* (the same root that
// printSlashSkillInfos reads in print_slash.go). Cycle 193 ships the
// directory walk only — the SKILL.md frontmatter parse is in a follow-up.
//
// Returns nil when the directory does not exist so the panel can
// render an empty-state row instead of crashing.
func skillsFromDisk() []struct {
	name string
	desc string
} {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(home, ".gemini", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]struct {
		name string
		desc string
	}, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, struct {
			name string
			desc string
		}{
			name: e.Name(),
			desc: "Skill installed under ~/.gemini/skills/" + e.Name(),
		})
	}
	return out
}

// panelSkillsLines renders the /skills picker. Cycle 203 rebuilds it on the
// real catalogue and the reference's recovered layout — the whole
// implementation lives in skillsPanelLines (slash_skills.go), which this
// delegates to. `enter View` opens the per-skill detail (cycle 201) when a
// row is selected. See the block comment at the top of the cycle-203
// section in slash_skills.go for the evidence.
func (m *runMode) panelSkillsLines() []string {
	if m.panel.detail {
		return m.panelSkillDetailLines()
	}
	return m.skillsPanelLines()
}

// panelSkillDetailLines renders the per-skill view /skills' `enter View`
// opens. Cycle 203 backs it with the *selected catalogue entry* (name,
// description and SKILL.md path) via skillsPanelDetailLines in
// slash_skills.go; REF's real screen is a full SKILL.md document viewer
// that this cycle does not reproduce (see that function's comment).
func (m *runMode) panelSkillDetailLines() []string {
	return m.skillsPanelDetailLines()
}

// panelUsageLines renders the /usage panel. Layout recovered from
// /tmp/agycmp/panels/ppref.usage.raw.txt. Cycle 193 ships the static
// placeholders (no live quota call):
//   - bold dim "└ Models & Quota" title
//   - Account: navigaid@gmail.com  (brand blue)
//   - GEMINI MODELS group: 100% green bar, "Quota available"
//   - CLAUDE AND GPT MODELS group: 0.00% dim bar, "Refreshes in 74h 29m"
//   - "(1–17 of 20 lines)" counter in dim grey 241
//   - dot hint: ↑/↓ Scroll · pgup/pgdown Page · ctrl+end Bottom ·
//     ctrl+home Top · esc Close
//
// Esc closes with the standard exit notice (esc-ref-model-Esc.txt).
//
// Cycle 201: the body scrolls. usageViewerLines is the 20-line document;
// the panel draws a 17-row window over it so ↑/↓, pgup/pgdn and
// ctrl+home / ctrl+end move the window and the "(a–b of 20 lines)"
// counter follows. At offset 0 the rendered rows are the cycle-193 ones.
func (m *runMode) panelUsageLines() []string {
	// Cycle 207: with a real terminal size the panel uses REF 1.4.0's
	// title + chrome + window layout; the cycle-193 renderer below stays
	// the fallback while the size is unknown (the unit tests).
	if m.height > 0 {
		return m.panelUsageLinesREF()
	}
	spec, _ := m.viewerSpecFor()
	rows, start, end := m.viewerWindow(spec)
	lines := append([]string{}, rows...)
	lines = append(lines,
		"",
		viewerCounter(start, end, spec.total),
		"",
		panelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"),
	)
	return lines
}

// usageViewerLines is the /usage viewer's scrollable document. Cycle 225:
// when m.height > 0 the live terminal path delegates to usageDocLive in
// slash_viewer_panels.go so the loading doc and the live refresh countdown
// render. The height-0 fallback (unit tests) keeps the cycle-193 layout
// byte-exact via usageViewerLinesStatic; this matters for
// TestPanelUsageLayout / TestCycle207UsageDocGeometry.
func (m *runMode) usageViewerLines() []string {
	if m.height > 0 {
		return usageDocLive(m)
	}
	return usageViewerLinesStatic()
}

// usageViewerLinesStatic is the cycle-193/201 fallback used by every
// panelUsage-line test. The first 17 rows are byte-identical to the
// cycle-193 render (transcribed from ppref.usage.raw.txt); the trailing
// three rows are the wrapped tail of the group note REF clips behind its
// window — reconstructed, not captured. 【推测】
func usageViewerLinesStatic() []string {
	email := auth.EffectiveUserEmail()
	if email == "" {
		email = "navigaid@gmail.com"
	}
	return []string{
		"\x1b[1m" + panelGray238 + "└ Models & Quota" + "\x1b[0m",
		"",
		"  Account: " + cBlueOpen + email + cBlueClose,
		"",
		"\x1b[1m" + "GEMINI MODELS" + "\x1b[0m",
		cDimGrayOpen + "  Models within this group: Gemini Flash, Gemini Pro" + cDimGrayClose,
		"",
		"\x1b[1mWeekly Limit Remaining\x1b[0m",
		"    " + "\x1b[32m" + strings.Repeat("█", 50) + "\x1b[39m" +
			cDimGrayOpen + " 100.00%" + cDimGrayClose,
		"\x1b[32mQuota available\x1b[39m",
		"",
		"\x1b[1m" + "CLAUDE AND GPT MODELS" + "\x1b[0m",
		cDimGrayOpen + "  Models within this group: Claude Opus, Claude Sonnet, GPT-OSS" + cDimGrayClose,
		"",
		"\x1b[1mWeekly Limit Remaining\x1b[0m",
		"    [\x1b[38;2;101;123;131m" + strings.Repeat("░", 50) + "\x1b[39m]" +
			cDimGrayOpen + " 0.00%" + cDimGrayClose,
		"\x1b[31mRefreshes in 74h 29m\x1b[39m",
		// Off-screen tail (rows 18-20): the "│Within each group…" note
		// REF shows just above the counter. 【推测】
		cDimGrayOpen + "  │Within each group, models share a weekly limit." + cDimGrayClose,
		cDimGrayOpen + "  │Quota refreshes weekly and is consumed as you use" + cDimGrayClose,
		cDimGrayOpen + "  │models in the group." + cDimGrayClose,
	}
}

// changelogBody is the opening set of bullet points under `## 1.3.6`.
// Cycle 193 shipped this block verbatim from
// /tmp/agycmp/panels/ppref.changelog.txt; cycle 201 keeps it as the head
// of the scrollable document (the rest of the release notes is parsed
// from the embedded print-mode markdown below).
var changelogBody = []string{
	"",
	"  \x1b[1m\x1b[38;5;27m## 1.3.6\x1b[0m",
	"",
	"  \x1b[38;5;235m• Added Claude Opus 5.5 and Claude Sonnet 5.5 for Application Default Credentials (AGY_ADC_AUTH) sign-in. Claude models now appear in /model only if your Google Cloud project can use them.\x1b[39m",
	"  \x1b[38;5;235m• Improved Auto mode's review of commands that would leave the terminal sandbox: the review is faster and no longer times out without a decision, it allows the workspace's own format, lint, and fix commands instead of asking about them, and it blocks commands that would upload sensitive data.\x1b[39m",
	"  \x1b[38;5;235m• Improved the /diff Commit Tree view with colors: commit hashes and change IDs are highlighted, descriptions are dimmed, the Working Copy (Uncommitted Changes) row stands out, and @ is bold, so hashes, timestamps, and descriptions no longer run together.\x1b[39m",
	"  \x1b[38;5;235m• Fixed the agent hanging indefinitely when the model connection stalled: if no data arrives for 5 minutes the request is now canceled and retried automatically, and a stalled request no longer leaves an empty response step behind.\x1b[39m",
	"  \x1b[38;5;235m• Fixed questions asked by subagents and workflow agents being lost, which left the subagent waiting forever: the alert now reads <agent> has a question and shows it, and Ctrl+K or a in the subagent's view opens a question picker that sends your answer to that agent. Headless -p runs skip these questions so the subagent can continue.\x1b[39m",
	"  \x1b[38;5;235m• Fixed agent shell commands hanging on an invisible editor when a version control command wants one, such as git commit without -m or jj describe: the CLI now sets GIT_EDITOR, HGEDITOR, and JJ_EDITOR to false for agent commands, so they fail right away instead.\x1b[39m",
	"  \x1b[38;5;235m• Fixed skills, rules, workflows, and Markdown agents failing to load with mapping values are not allowed in this context when an unquoted description: contains a colon, such as description: Runs checks: formatters and linters.\x1b[39m",
	"  \x1b[38;5;235m• Fixed Mermaid diagrams that start with YAML frontmatter, a %%{init: ...}%% directive, or %% comments, or that use Mermaid v11 syntax such as id@{ shape: ..., label: ... } nodes, showing as raw source instead of rendering in agent responses and the artifact viewer.\x1b[39m",
	"  \x1b[38;5;235m• Fixed the @ file-mention dropdown staying open after a newline in a multi-line prompt, where it took over Up/Down and made Enter pick a file instead of inserting the newline.\x1b[39m",
	"  \x1b[38;5;235m• Fixed / search in the file and artifact viewer highlighting the wrong characters, or crashing, on lines with letters whose lowercase form has a different length such as İ; n and N now stop only on lines that show a highlight.\x1b[39m",
	"  \x1b[38;5;235m• Fixed long lines and file paths containing CJK or other multi-byte text turning into characters in /codesearch results; they are now cut cleanly with an ellipsis.\x1b[39m",
	"  \x1b[38;5;235m• Fixed agy plugin validate, the summary agy plugin install prints, and marketplace previews in the /plugin Discover tab leaving out subagents a plugin declares in its root agents.json.\x1b[39m",
}

// panelChangelogLines renders the /changelog viewer. Layout recovered
// from /tmp/agycmp/panels/ppref.changelog.raw.txt. Esc closes silently
// (the changelog viewer never writes an `Exited /changelog` row).
//
// Cycle 201: the viewer scrolls. changelogExcerptRows is the cycle-193
// first screen (title + tagline + the 1.3.6 block) and is what offset 0
// still renders; changelogRestRows appends the remaining sections so
// ↑/↓ have somewhere to go. The counter keeps REF's denominator.
//
// Cycle changelog-pagination: the document now comes from
// internal/cli/testdata/changelog_reference.txt when that file is
// reachable (see changelogViewerLines), so the panel has 900+ rows
// to scroll instead of the ~50 the cycle-201 hardcoded fallback had.
// The footer advertises the full pagination key set (↑/↓ line,
// pgup/pgdown page, ctrl+end bottom, ctrl+home top, esc close) the
// same way the /usage viewer does.
func (m *runMode) panelChangelogLines() []string {
	spec, _ := m.viewerSpecFor()
	rows, start, end := m.viewerWindow(spec)
	lines := append([]string{}, rows...)
	// Cycle 207: REF puts the counter immediately under the window and the
	// blank between the counter and the legend (changelog24.ref.raw.txt
	// rows 20-23). The counter is always present: the release-notes document
	// never fits.
	// Live 1.4.3 advertises only Navigate/Cancel in this viewer even though
	// the scroll keys still work; the full pagination hint was a 1.4.0
	// transcription.
	lines = append(lines,
		viewerCounter(start, end, spec.total),
		"",
		panelDotHint("  ", "↑/↓", "Navigate", "esc", "Cancel"))
	return lines
}

// changelogExcerptRows is the visible first screen of the /changelog
// viewer: the yellow title (2-space indent, 48;5;63 background — REF
// 1.4.0), a blank, the tagline and the transcribed 1.3.6 block.
func changelogExcerptRows() []string {
	out := []string{
		"  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m",
		"",
		"  \x1b[38;5;235mThe terminal-first surface to interact with Pigasus agents. Stay in your flow without context switching.\x1b[39m",
	}
	return append(out, changelogBody...)
}

// changelogWindowRows is how many document rows the viewer shows at
// once. Cycle changelog-pagination switched the source to the runtime
// reference file (internal/cli/testdata/changelog_reference.txt), but
// the first-screen shape is preserved at 18 rows: title + blank +
// tagline + blank + ## 1.3.6 + blank + the first 12 bullets of the
// 1.3.6 section. The 13th bullet and the section break to 1.3.5 land
// on the second screen, matching the cycle-201 first-screen layout
// that TestPanelChangelogLayout / TestPanelChangelogViewerScrolls pin.
//
// The 13th bullet is rendered into the document (and visible after a
// single ↓ press); only the window size is fixed at 18 so the counter
// starts at "(1–18 of 2014 lines)" rather than the wrap-dependent
// row count.
func changelogWindowRows() int { return 18 }

// changelogTotalLines is the denominator in the viewer's "(a–b of N lines)"
// counter: the reference's wrapped line count for the whole release-notes
// document at 120x40. Re-measured for the 1.4.4 capture (live: "1–36 of 2014
// lines"); the 1.4.2-era fixture showed 1962.
const changelogTotalLines = 2014

// changelogViewerLines is the /changelog document.
//
// Cycle changelog-pagination: when the runtime source loader finds
// internal/cli/testdata/changelog_reference.txt (it walks up from cwd
// looking for go.mod) the panel renders the full release notes
// 1.3.6 → 1.0.0 — ~915 logical rows, more when the bullet text wraps
// to the terminal width. The fallback path keeps the cycle-201
// transcription (changelogExcerptRows + changelogRestRows parsed from
// cmd/wagyu/testdata/print_changelog.md) so a binary installed in a
// user's PATH still shows a populated document.
//
// On a live terminal (m.width > 0 and m.height > 0) the document is
// wrapped to the panel width — see changelogPanelDoc in
// slash_changelog_wrap.go for the markdown path and
// changelogSourceWrapped in slash_changelog_source.go for the new
// bullet-list source. The width-0 fallback preserves the cycle-193
// transcription byte-for-byte (the transcription is one long line per
// bullet and only fits a width that wraps back to the same rows).
func (m *runMode) changelogViewerLines() []string {
	if m.width > 0 && m.height > 0 {
		if src := changelogSourceLines(); src != nil {
			return changelogSourceWrapped(src, m.width)
		}
		return changelogPanelDoc(m.width)
	}
	if src := changelogSourceLines(); src != nil {
		return changelogSourceStyled(src)
	}
	out := append([]string{}, changelogExcerptRows()...)
	return append(out, changelogRestRows...)
}

// changelogRestRows is the remainder of the release notes (1.3.5 down
// through 1.0.0) rendered in the panel's bullet style. It is parsed once
// from the embedded print-mode markdown (`printSlashChangelog`); the
// print-mode blob deliberately omits the 1.3.6 section that changelogBody
// above already covers, so the two concatenate without overlap.
var changelogRestRows = buildChangelogRestRows()

func buildChangelogRestRows() []string {
	var out []string
	headers := 0
	for _, raw := range strings.Split(printSlashChangelog, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "## ") {
			headers++
			// Cycle 207: the cycle-200 markdown refresh re-added the 1.3.6
			// section (line 7 of testdata/print_changelog.md), but
			// changelogExcerptRows already ships that block via
			// changelogBody. Emitting it again duplicated the whole
			// section in the panel, so skip the first header.
			if headers == 1 {
				continue
			}
			out = append(out, "",
				"  \x1b[1m\x1b[38;5;27m"+line+"\x1b[0m")
			continue
		}
		if headers < 2 {
			// The H1 title, tagline, HTML comments and the duplicated
			// 1.3.6 body above the first real (`## 1.3.5`) header are
			// already represented by the excerpt.
			continue
		}
		switch {
		case strings.HasPrefix(line, "- "):
			out = append(out,
				"  \x1b[38;5;235m• "+panelPlainInline(line[2:])+"\x1b[39m")
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// panelPlainInline strips the inline markdown the panel renders as plain
// text: `backticks` around code and **bold** markers. The reference's
// captures show both dropped (ppref.changelog.raw.txt renders
// "AGY_ADC_AUTH" with no quoting).
func panelPlainInline(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	return s
}

// panelDiffLines renders the /diff viewer. Two shapes exist in REF 1.4.0:
//
//   - a git (or hg/jj) workspace is detected from m.cwd — the All Changes
//     tab lists the working-copy files (panelDiffLinesVCS);
//   - otherwise the no-VCS warning below is shown. That shape (and its
//     leading " Diff" tab row with plain inactive tabs) is recovered from
//     /tmp/agycmp/panels/ppref.diff.raw.txt and is also the fallback the
//     unit tests exercise (mkPanel leaves m.cwd empty). 【一手】
//
// Cycle 207 wires the git branch in; the no-VCS renderer below is the
// cycle-193/201 shape, with the inactive tabs changed from dim gray to
// plain because both the 1.3.x capture and the 1.4.0 captures show no SGR
// on them.
func (m *runMode) panelDiffLines() []string {
	if m.cwd != "" && detectVCS(m.cwd) == "git" {
		return m.panelDiffLinesVCS()
	}
	cwd, _ := os.Getwd()
	if cwd == "" {
		cwd = "."
	}
	tab := m.panel.tab
	if tab < 0 || tab >= len(diffViews) {
		tab = 0
	}
	var row strings.Builder
	row.WriteString(" \x1b[1mDiff\x1b[0m")
	for i, name := range diffViews {
		row.WriteString("  ")
		if i == tab {
			row.WriteString(cBlueOpen + name + cBlueClose)
		} else {
			row.WriteString(name)
		}
	}
	return []string{
		row.String(),
		"",
		"   " + cToolBulletErrOpen + "\u26a0" + "\x1b[39m" +
			" no supported VCS found in " + cwd,
		"",
		panelDotHint("  ", "←/→", "switch view", "esc", "dismiss"),
	}
}

// diffViews are the three tabs of the /diff viewer, in REF order
// (ppref.diff.raw.txt row 1).
var diffViews = []string{"All Changes", "Per Turn", "Commit Tree"}

// ---- Cycle 201: scrollable viewers ----------------------------------------

// viewerSpec describes a scrollable documentary panel: the document lines,
// how many of them fit in the panel's window, and the total line count the
// footer's "(a–b of K lines)" counter quotes.
type viewerSpec struct {
	lines   []string
	visible int
	total   int
}

// viewerSpecFor returns the viewer spec for the open panel, or ok=false
// when the panel is not a document viewer.
func (m *runMode) viewerSpecFor() (viewerSpec, bool) {
	switch m.panel.kind {
	case panelUsage:
		// Cycle 207: on a real terminal the spec is REF 1.4.0's
		// 20-line document with a height-13 window.
		if m.height > 0 {
			return m.usageViewerSpecREF(), true
		}
		// REF's footer reads "(1–17 of 20 lines)": 17 rows visible out of
		// a 20-line document (ppref.usage.raw.txt row 22). The three
		// off-screen rows are the wrapped tail of the group note; the
		// clone transcribes the first line REF shows and reconstructs the
		// rest. 【推测】
		// Cycle 225: the height-aware path (m.height > 0) is handled
		// above by usageViewerSpecREF. This branch is the height-0
		// fallback, which the unit tests exercise; deliver the static
		// doc byte-exact.
		lines := usageViewerLinesStatic()
		return viewerSpec{lines: lines, visible: 17, total: 20}, true
	case panelChangelog:
		// Cycle 207: REF 1.4.0's window is (height-4) rows of the
		// document — title + window + counter + blank + legend fills
		// height-1 rows and the status row closes the screen. Measured
		// with a settled terminal at h=24 (counter 1–20), h=26 (1–22),
		// h=33 (1–29) and h=50 (1–46): every one is height-4. An earlier
		// reading of height-3 came from captures taken before the panel
		// had settled and is retracted here.
		if m.height > 0 {
			vis := m.height - 4
			if vis < 1 {
				vis = 1
			}
			return viewerSpec{lines: m.changelogViewerLines(), visible: vis, total: changelogTotalLines}, true
		}
		// its 17 transcribed rows (the same rows REF shows, which the
		// terminal wraps back up to 26 visual rows); the counter keeps
		// REF's wrapped line count as the document total.
		//
		// Caveat: the total is REF's *wrapped* line count measured at
		// 120x40 for the same release notes (cycle 292 re-capture: 2014,
		// previously 1962). The clone wraps the embedded capture at its own
		// width, so the counter's end can stop short of the total; the
		// window clamps against the content it has so a scroll never lands
		// on a blank row.
		lines := m.changelogViewerLines()
		return viewerSpec{lines: lines, visible: changelogWindowRows(), total: changelogTotalLines}, true
	case panelFeatDiff:
		// Cycle 236: the feature-parity viewer scrolls like /changelog.
		// height-aware window is height-4 (cycle-207 contract); the
		// height-0 fallback is 17 (same as /usage). total is the
		// actual document length (which varies with the JSON), not the
		// cycle-changelog-style 1962 placeholder.
		return m.featDiffViewerSpec(), true
	}
	return viewerSpec{}, false
}

// viewerWindow returns the rows of spec that the current scroll offset
// shows, plus the 1-based [start,end] range the counter quotes. It clamps
// the offset into [0, len(lines)-visible] so a scrolled window is always
// full of content.
func (m *runMode) viewerWindow(spec viewerSpec) (rows []string, start, end int) {
	n := len(spec.lines)
	vis := spec.visible
	if vis < 1 {
		vis = 1
	}
	maxOffset := n - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.window > maxOffset {
		m.panel.window = maxOffset
	}
	hi := m.panel.window + vis
	if hi > n {
		hi = n
	}
	rows = spec.lines[m.panel.window:hi]
	start = m.panel.window + 1
	end = m.panel.window + vis
	if end > n {
		end = n
	}
	if end < start {
		end = start
	}
	return rows, start, end
}

// viewerCounter renders the dim gray "(a–b of K lines)" row every viewer
// footer carries, with the same en-dash REF uses.
func viewerCounter(start, end, total int) string {
	return cDimGrayOpen + fmt.Sprintf("  (%d\u2013%d of %d lines)", start, end, total) + cDimGrayClose
}

// handlePanelKey routes a key to the open panel. Returns (handled, cmd)
// so the caller can return early without falling through to the prompt.
//
// Cycle 201 wires up the key actions the reference's footers announce:
//
//	/usage  /changelog   ↑/↓ line, pgup/pgdown page, ctrl+home/end ends
//	/plugin              tab cycles Installed ⇄ Discover
//	/skills              enter opens the per-skill detail, esc returns
//	/diff                ←/→ switch the active tab
//	/feedback            enter (or 1-6) advances to the free-text stage
//
// Caveat (routing, not this function): tui_run.go runs the transcript
// scroll bindings (pgup / pgdown / ctrl+home / ctrl+end) before the panel
// branch, and those consume the keys while alt-screen mode is on (the
// default). The viewer handles the full set regardless, so the page/end
// bindings work under --no-alt-screen and would work for panels too if
// the routing order changed.
func (m *runMode) handlePanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.panel.kind == panelNone {
		return false, nil
	}
	// Cycle 213: /codesearch's result list handles its own navigation
	// when the panel is showing matches; the empty-list / usage /
	// pattern-error forms fall through to listPanelOwningKey below.
	if m.panel.kind == panelCodeSearch && len(m.panel.cs.hits) > 0 {
		if handled, _, ok := m.codesearchPanelKey(key); ok {
			return handled, nil
		}
	}
	// Cycle 206: the four list panels own every key until dismissed.
	// This must precede the router below so nothing reaches the prompt,
	// $EDITOR, the alt-screen toggle or the transcript scroller.
	// Cycle tasks-panel: /tasks is now action-aware (Enter / k /
	// x), so the bare listPanelOwningKey dispatch is not enough.
	// We delegate to tasksPanelKey first for the actions, then
	// fall through to the inert swallow for the rest.
	if m.panel.kind == panelTasks {
		if handled, cmd := m.tasksPanelKey(key); handled {
			return true, cmd
		}
		return m.listPanelOwningKey(key)
	}
	if listPanelKind(m.panel.kind) {
		return m.listPanelOwningKey(key)
	}
	// Cycle 205: the action panels own their multi-stage key handling.
	switch m.panel.kind {
	case panelPermissions:
		return m.handlePermissionsKey(key)
	case panelHooks:
		return m.handleHooksKey(key)
	case panelAgents:
		return m.handleAgentsKey(key)
	}
	// Cycle 223: /add-dir panel owns every key — Esc closes with the
	// standard exit notice, Enter echoes through the cycle-217
	// addDirFeedback machinery, printable keys edit the path buffer.
	if m.panel.kind == panelAddDir {
		return m.handleAddDirKey(key)
	}
	// Cycle charmland-component-adoption/02: /rename panel — Esc
	// closes silently, Enter submits via renameFeedback, every other
	// key flows through bubbles/v2/textinput.Model.Update. The
	// textinput carries the value, cursor position, virtual cursor
	// blink phase, and the standard chat-app edits (ctrl+w word
	// delete, ctrl+u delete-to-start, ctrl+a line-start, ctrl+e
	// line-end, ctrl+k delete-to-end, etc.) that the previous
	// hand-rolled `panel.text` buffer did not implement.
	if m.panel.kind == panelRename {
		return m.handleRenamePanelKey(key)
	}
	// Document viewers scroll their window.
	if m.panel.kind.isViewer() {
		return m.handleViewerPanelKey(key)
	}
	// /feedback's stage-2 free-text form owns every key.
	if m.panel.kind == panelFeedback && m.panel.stage > 0 {
		return m.handleFeedbackTextKey(key)
	}
	// /diff's only non-Esc action is the tab row.
	if m.panel.kind == panelDiff {
		return m.handleDiffPanelKey(key)
	}
	n := m.panelRowCount(m.panel.kind)
	if m.panel.kind == panelEffort {
		// The effort slider is horizontal: ←/→ move it, ↑/↓ do nothing.
		switch key.Code {
		case tea.KeyLeft:
			if m.panel.cursor > effortLow {
				m.panel.cursor--
			}
			return true, nil
		case tea.KeyRight:
			if m.panel.cursor < effortHigh {
				m.panel.cursor++
			}
			return true, nil
		case tea.KeyEnter:
			// Cycle 208: Enter applies the highlighted level (see
			// applyEffortSelection).
			return true, m.applyEffortSelection(m.panel.cursor)
		case tea.KeyUp, tea.KeyDown:
			return true, nil
		}
	} else if m.panel.kind == panelModel {
		// Cycle 197: ←/→ cycles the effort for the highlighted model;
		// Enter selects the (model, effort) pair, persists to settings.json,
		// and closes the panel. Up/Down keep their cursor-navigation
		// semantics (no wrap on the model picker).
		switch key.Code {
		case tea.KeyLeft:
			m.cycleModelEffort(-1)
			return true, nil
		case tea.KeyRight:
			m.cycleModelEffort(+1)
			return true, nil
		case tea.KeyEnter:
			m.selectModelEffort()
			return true, m.closePanel()
		case tea.KeyUp:
			if m.panel.cursor > 0 {
				m.panel.cursor--
				// Cycle 208: REF re-seeds the slider from the model's
				// *current* effort when the cursor moves — it returns
				// to High after Down-then-Up (fin_model_up.raw.txt),
				// and the `(current)` marker stays on the Gemini row
				// while the cursor is elsewhere. Resetting to Medium
				// dropped both.
				m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
			}
			return true, nil
		case tea.KeyDown:
			if m.panel.cursor < n-1 {
				m.panel.cursor++
				m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
			}
			return true, nil
		}
	} else if m.panel.kind == panelConfig {
		handled, cmd := m.handleConfigKey(key, n)
		if handled {
			return true, cmd
		}
	} else if m.panel.kind == panelPlugin {
		// Cycle 198: tab switches Installed ↔ Discover (resetting the cursor
		// to the new tab's first row — REF clamps, it does not remember per
		// tab). On the Installed tab space flips config.json's
		// plugins.<name>.enabled and Enter expands/collapses the detail
		// block.
		//
		// Cycle 204: while the Discover search box has focus the box owns
		// every key (handlePluginSearchKey), the unfocused Discover tab gains
		// `←/→` first/last jumps, space (or any printable key) focuses the
		// box, and Enter opens the Discover detail block instead of the
		// Installed one.
		//
		// Cycle 212: the local-install prompt (openPluginLocalInstall) owns
		// every key while it is open. Enter / ctrl+s on the un-focused
		// Discover local-install row opens it.
		//
		// Cycle dpl (AGY 1.2.7): `/` toggles the Installed tab's
		// "enabled-only customization query" filter (panel.
		// pluginEnabledOnly); the filter resets to false on panel close
		// (closePanel assigns a zero slashPanel) so the disabled rows
		// reappear the next time the panel opens. The Install tab's
		// ctrl+s uninstalls the highlighted row after the hardened
		// validator confirms the ID is safe (see slash_plugin_uninstall.go).
		if m.pluginLocalInstallActive() {
			handled := m.handlePluginLocalInstallKey(key)
			return true, pluginCmdForFocus(handled, m)
		}
		// Cycle 219: the search box owns every key on either tab while
		// it has focus — the cycle-212 Tab-keeps-focus puts the user on
		// a focused Installed tab too, so handlePluginSearchKey has to
		// run there. The function dispatches on pluginTab internally
		// (Enter commits on Installed, Enter expands on Discover).
		if m.panel.pluginSearch {
			handled := m.handlePluginSearchKey(key)
			return true, pluginCmdForFocus(handled, m)
		}
		switch key.Code {
		case tea.KeyTab:
			if m.panel.pluginTab == panelInstalled {
				m.panel.pluginTab = panelDiscover
			} else {
				m.panel.pluginTab = panelInstalled
			}
			m.panel.cursor = 0
			return true, nil
		case tea.KeyUp:
			if m.panel.cursor > 0 {
				m.panel.cursor--
			}
			return true, nil
		case tea.KeyDown:
			if m.panel.cursor < m.pluginRowCount()-1 {
				m.panel.cursor++
			}
			return true, nil
		case tea.KeyLeft:
			// Measured: ← parks the cursor on the first Discover row (it is
			// inert on the Installed tab, whose footer does not list it).
			if m.panel.pluginTab == panelDiscover {
				m.panel.cursor = 0
			}
			return true, nil
		case tea.KeyRight:
			if m.panel.pluginTab == panelDiscover {
				if n := m.pluginRowCount(); n > 0 {
					m.panel.cursor = n - 1
				}
			}
			return true, nil
		case tea.KeySpace:
			if m.panel.pluginTab == panelInstalled {
				m.togglePluginEnabled()
				return true, nil
			}
			m.enterPluginSearch()
			return true, pluginBlink()
		case tea.KeyEnter:
			if m.panel.pluginTab == panelInstalled {
				m.togglePluginExpanded()
				return true, nil
			}
			if !m.panel.pluginSearch && m.panel.cursor == 0 {
				// Cycle 212: Enter on the "+ Install from local directory..."
				// row opens the inline path prompt. Measured at 220x50.
				m.openPluginLocalInstall()
				return true, pluginBlink()
			}
			m.togglePluginDiscoverExpanded()
			return true, nil
		case '/':
			// Cycle dpl (AGY 1.2.7): `/` toggles the Installed tab's
			// "enabled-only customization query". Only fires on the
			// Installed tab — Discover's `/` is a printable character
			// that focuses the search box (the cycle-204 catch-all below
			// takes it for Discover). The bug this fixes: a stale
			// `pluginEnabledOnly=true` flag persists across panel
			// close/reopen, which the AGY 1.2.7 release closed by ensuring
			// closePanel zeroes the field.
			if m.panel.pluginTab == panelInstalled {
				m.panel.pluginEnabledOnly = !m.panel.pluginEnabledOnly
				m.panel.cursor = 0
				return true, nil
			}
		}
		if key.Code == 's' && key.Mod.Contains(tea.ModCtrl) {
			// Cycle dpl (AGY 1.2.7): ctrl+s on the Installed tab
			// uninstalls the highlighted row. The legend already lists
			// it as "ctrl+s Uninstall"; this commit wires the action.
			// validatePluginID rejects path-traversal IDs before any
			// filesystem work happens, so an invalid row surfaces an
			// "Error:" line in the panel title rather than escaping the
			// per-plugin directory. On Discover the ctrl+s branch keeps
			// its cycle-212 local-install meaning.
			if m.panel.pluginTab == panelInstalled {
				notice, _ := m.uninstallHighlightedPlugin()
				if notice != "" {
					m.panel.pluginLocalError = notice
				}
				return true, nil
			}
			if m.panel.pluginTab == panelDiscover && !m.panel.pluginSearch &&
				m.panel.cursor == 0 {
				m.openPluginLocalInstall()
				return true, pluginBlink()
			}
		}
		if m.panel.pluginTab == panelDiscover && isTypedText(key) {
			// Any printable key focuses the Discover box and seeds it (the
			// trailing space of KeySpace is handled above). Measured: the
			// cursor re-parks on the first filtered result.
			m.enterPluginSearch()
			m.panel.pluginQuery = key.Text
			return true, pluginBlink()
		}
	} else {
		// Cycle 201 — /skills: enter opens the per-skill detail view and
		// Esc returns to the list. Cycle 211 expands the detail view's
		// key handling: ↑/↓, pgup/pgdown, ctrl+home/ctrl+end drive the
		// document viewer in slash_skills_detail.go; everything else
		// (printable, modified-fall-through) follows the cycle-201 rules.
		if m.panel.kind == panelSkills {
			if m.panel.detail {
				if handled, cmd := m.handleSkillsDetailKey(key); handled {
					return true, cmd
				}
				return false, nil
			}
			if key.Code == tea.KeyEnter {
				m.openSkillDetail()
				return true, nil
			}
			if key.Code == tea.KeyUp || key.Code == tea.KeyDown {
				m.moveSkillsCursor(key.Code == tea.KeyDown)
				return true, nil
			}
		}
		wrap := m.panel.kind.wraps()
		switch key.Code {
		case tea.KeyUp:
			if n > 0 {
				if m.panel.cursor > 0 {
					m.panel.cursor--
				} else if wrap {
					m.panel.cursor = n - 1
				}
			}
			return true, nil
		case tea.KeyDown:
			if n > 0 {
				if m.panel.cursor < n-1 {
					m.panel.cursor++
				} else if wrap {
					m.panel.cursor = 0
				}
			}
			return true, nil
		}
	}
	// Cycle 201 — /feedback: enter continues on the highlighted category
	// and 1-6 select a category and continue, both advancing to stage 2.
	if m.panel.kind == panelFeedback {
		if key.Code == tea.KeyEnter {
			m.panel.stage = 1
			return true, nil
		}
		if key.Mod == 0 && len(key.Text) == 1 && key.Text[0] >= '1' && key.Text[0] <= '6' {
			m.panel.cursor = int(key.Text[0] - '1')
			m.panel.stage = 1
			return true, nil
		}
	}
	// Cycle 222 — /rewind: enter rewinds to the cursor's step (the safe
	// stub). The branch is checked before the catch-all so it returns
	// (true, close-cmd) instead of swallowing the key. ↑/↓ keep their
	// generic clamp semantics from the switch above.
	if m.panel.kind == panelRewind && key.Code == tea.KeyEnter {
		m.panelRewindPickStep()
		return true, m.closePanel()
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, m.closePanel()
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		// Selecting a row is not implemented for these panels yet;
		// swallow so Enter cannot submit the (empty) prompt.
		return true, nil
	}
	// These panels have no search box, so printable input is ignored.
	// Modified keys (ctrl+*, alt+*) fall through to the global bindings.
	if key.Mod == 0 && key.Text != "" {
		return true, nil
	}
	return false, nil
}

// panelPageRows is the page distance for the panels whose footer says
// "←/→ Page" (/plugin). It matches the 10-row page the /config picker
// uses.
const panelPageRows = 10

// panelPageCursor moves the row cursor by count rows, clamped to the list.
func (m *runMode) panelPageCursor(n, count int) {
	if n <= 0 {
		return
	}
	m.panel.cursor += count
	if m.panel.cursor < 0 {
		m.panel.cursor = 0
	}
	if m.panel.cursor > n-1 {
		m.panel.cursor = n - 1
	}
}

// viewerMaxOffset is the largest scroll offset a viewer can hold without
// running off the end of its document.
func viewerMaxOffset(spec viewerSpec) int {
	max := len(spec.lines) - spec.visible
	if max < 0 {
		max = 0
	}
	return max
}

// viewerScrollBy moves a viewer's window by delta rows, clamped to
// [0, viewerMaxOffset].
func (m *runMode) viewerScrollBy(spec viewerSpec, delta int) {
	m.panel.window += delta
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if max := viewerMaxOffset(spec); m.panel.window > max {
		m.panel.window = max
	}
}

// handleViewerPanelKey drives a document viewer: ↑/↓ by a line,
// pgup/pgdown by a page, ctrl+home / ctrl+end to the ends, esc closes.
// These are exactly the bindings the /usage footer announces; /changelog
// advertises ↑/↓ Navigate · esc Cancel. Unmodified keys the viewer does
// not use are swallowed; modified ones (except ctrl+home/end) fall
// through so ctrl+c still quits.
func (m *runMode) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	spec, ok := m.viewerSpecFor()
	if !ok {
		return false, nil
	}
	page := spec.visible - 1
	if page < 1 {
		page = 1
	}
	switch {
	case key.Code == tea.KeyUp:
		m.viewerScrollBy(spec, -1)
		return true, nil
	case key.Code == tea.KeyDown:
		m.viewerScrollBy(spec, 1)
		return true, nil
	case key.Code == tea.KeyPgUp:
		m.viewerScrollBy(spec, -page)
		return true, nil
	case key.Code == tea.KeyPgDown:
		m.viewerScrollBy(spec, page)
		return true, nil
	case key.Code == tea.KeyHome && key.Mod.Contains(tea.ModCtrl):
		m.panel.window = 0
		return true, nil
	case key.Code == tea.KeyEnd && key.Mod.Contains(tea.ModCtrl):
		m.panel.window = viewerMaxOffset(spec)
		return true, nil
	case key.Code == tea.KeyEsc:
		return true, m.closePanel()
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// handleDiffPanelKey drives the /diff tab row: ←/→ switch the active view
// ("←/→ switch view"), esc dismisses. Unmodified keys stay inside the
// panel, modified keys fall through.
//
// Cycle 207 adds the All-Changes row cursor: ↑/↓/j/k move it over the file
// list ("↑/↓/j/k navigate"). It is inert on the Per Turn / Commit Tree tabs
// and in the no-VCS fallback, whose navigation could not be captured. 【存疑】
//
// Cycle 226: enter opens the inline-diff subview (the cycle-226 substitute
// for REF's side-by-side diff viewer, see slash_viewer_panels.go
// panelDiffInlineLines); esc on the subview returns to the file list.
//
// Cycle diff-folding (AGY 1.3.5): the inline-diff subview gains an
// `f` key (hunk fold toggle) and row-cursor navigation
// (↑/↓/j/k move diffInlineRow). Both bindings are inert on the list
// view, where the pre-existing list-view cursor and bindings still
// own the keys.
func (m *runMode) handleDiffPanelKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyLeft:
		if m.panel.diffView != 0 {
			// inline subview ignores tab switches (REF's diff viewer is
			// single-tab); Esc is the documented back binding.
			return true, nil
		}
		if m.panel.tab > 0 {
			m.panel.tab--
			m.panel.cursor = 0
		}
		return true, nil
	case tea.KeyRight:
		if m.panel.diffView != 0 {
			return true, nil
		}
		if m.panel.tab < len(diffViews)-1 {
			m.panel.tab++
			m.panel.cursor = 0
		}
		return true, nil
	case tea.KeyUp:
		if m.panel.diffView != 0 {
			m.moveDiffInlineCursor(-1)
			return true, nil
		}
		m.diffMoveCursor(-1)
		return true, nil
	case tea.KeyDown:
		if m.panel.diffView != 0 {
			m.moveDiffInlineCursor(1)
			return true, nil
		}
		m.diffMoveCursor(1)
		return true, nil
	case tea.KeyEnter:
		if m.panel.diffView == 0 {
			m.diffEnter()
		}
		return true, nil
	case tea.KeyEsc:
		if m.panel.diffView != 0 {
			m.diffCloseInline()
			return true, nil
		}
		return true, m.closePanel()
	}
	if d := diffTextKey(key); d != 0 {
		if m.panel.diffView != 0 {
			m.moveDiffInlineCursor(d)
		} else {
			m.diffMoveCursor(d)
		}
		return true, nil
	}
	// Cycle diff-folding (AGY 1.3.5): the hunk-fold 'f' key. Only
	// fires inside the inline subview (where the cursor row is
	// meaningful); on the list view 'f' is a swallowed
	// panel-owning no-op (same shape as the existing 'fall
	// through' branch for unrecognised letters, listed for
	// symmetry because the legend mentions it).
	if key.Mod == 0 && key.Code == 'f' {
		if m.panel.diffView != 0 {
			m.toggleDiffHunkFoldAtCursor()
		}
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// handleFeedbackTextKey edits the /feedback stage-2 text form: printable
// keys append, backspace deletes, enter (or ctrl+s) submits and esc
// returns to the category picker with the draft discarded.
//
// Cycle 221: openagy's Enter submits. REF's stage-2 form is a textarea
// whose legend advertises ctrl+s (fin_fb_stage2.raw.txt), so the
// reference binds Enter to a newline and ctrl+s to submit; but openagy
// has no upload endpoint and the user can only ever see the local-only
// echo, so a single Enter-submit binding matches the panel's UX. ctrl+s
// is kept as a parallel submit key so muscle memory from REF still
// works. See docs/cycles/cycle-221/.
func (m *runMode) handleFeedbackTextKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEsc:
		m.panel.stage = 0
		m.panel.text = ""
		return true, nil
	case key.Code == tea.KeyEnter:
		// Cycle 221: Enter submits (openagy has no upload endpoint;
		// the cycle-192-equivalent local-only line is the only outcome
		// the user can observe, and forcing a separate ctrl+s key
		// would be a UX dead-end). m.panel.cursor holds the
		// stage-1 category index and is preserved by submitFeedback's
		// blank reset, so a subsequent /feedback reopens on the same
		// row the user picked.
		return true, m.submitFeedback()
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		// ctrl+s is kept as a parallel submit key — REF advertises
		// it in the legend and the existing cycle-208 path was a no-op
		// to remove.
		return true, m.submitFeedback()
	case key.Code == tea.KeyBackspace:
		if r := []rune(m.panel.text); len(r) > 0 {
			m.panel.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if isTypedText(key) {
		m.panel.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// submitFeedback closes the /feedback panel after stage 2 and records
// the same local-only message the non-panel handler in slash_commands.go
// emits (the clone has no upload endpoint). Bound to both Enter and
// ctrl+s — see handleFeedbackTextKey for the rationale.
func (m *runMode) submitFeedback() tea.Cmd {
	m.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
	return nil
}

// openSkillDetail switches /skills into its per-skill detail view when the
// cursor is on an enumerated skill. Cycle 203: the cursor indexes the whole
// catalogue and -1 means "no selection", in which case Enter is a no-op
// (the reference's ppref behaviour — see caps/ref.enter.raw.txt).
func (m *runMode) openSkillDetail() {
	if m.panel.cursor < 0 || m.panel.cursor >= len(skillsPanelCatalog()) {
		return
	}
	m.panel.detail = true
}

// cycleConfigValue advances the value of the setting the cursor is on
// by `dir` (+1 for right, -1 for left) through its cycle list, then
// persists through auth: a non-nil `stored` is written, a nil `stored`
// *deletes* the key (the reference's representation of the default).
func (m *runMode) cycleConfigValue(dir int) {
	m.cycleConfigValueFiltered(configRows(), dir)
}

// configRowValue returns the display string for a configRow, honouring
// in-memory-only rows and business account locks.
func (m *runMode) configRowValue(row configRow) string {
	if isTelemetryRow(row) && IsBusinessAccount() {
		return telemetryDisabledValue
	}
	if row.memoryOnly {
		if v, ok := m.panel.configMemory[row.settingsKey]; ok {
			return v
		}
		return row.values[0].display
	}
	raw, err := auth.SettingsField(row.settingsKey)
	if err != nil {
		return configDisplayFor(row, nil)
	}
	return configDisplayFor(row, raw)
}

// configRowValueAt is the rendered value of one /config row, honouring the
// in-memory-only rows the reference never persists.
func (m *runMode) configRowValueAt(rowIdx int) string {
	rows := configRows()
	if rowIdx < 0 || rowIdx >= len(rows) {
		return ""
	}
	return m.configRowValue(rows[rowIdx])
}

// closePanel clears the panel and, when the reference does so, appends
// the "> /cmd" + "⎿  Exited /cmd command" block to the transcript.
func (m *runMode) closePanel() tea.Cmd {
	cmd := m.panel.cmd
	notice := m.panel.kind.exitNotice()
	m.panel = slashPanel{}
	// Cycle 216: /config can change "Editor Mode › Vim" and its Insert
	// First child, so the vim gate is refreshed when the panel closes.
	m.reloadVimSettings()
	if notice {
		m.response = append(m.response, slashEchoLines(cmd, "Exited "+cmd+" command")...)
	}
	// The input was already cleared when the panel opened (see the
	// openSlashPanel call site in Update) and every key was swallowed
	// while it was up, so there is nothing left to reset here.
	return nil
}

// closePanelSilent clears the panel without writing the
// `Exited /cmd command` echo. Cycle 223: /add-dir's commit path
// (Enter → addDirFeedback → slashRespond → close) already prints its
// own transcript block via slashRespond; emitting the exit notice on
// top would yield two stacked `> /add-dir` headers and is not what
// the brief wants. Esc still goes through closePanel so the standard
// exit notice does fire when the user backs out without committing.
//
// reloadVimSettings is called by both paths — /add-dir doesn't toggle
// vim gates, but every close should refresh so a follow-up panel
// across a /config change sees the fresh state.
func (m *runMode) closePanelSilent() tea.Cmd {
	m.panel = slashPanel{}
	m.reloadVimSettings()
	return nil
}

// panelModelBaseSlug returns the catalogue ID prefix for a panel row's
// display label. Cycle 197: the panel row stores the label
// ("Gemini 3.8 Flash (High)") and the catalogue ID
// ("gemini-3.8-flash-high") is what REF persists to settings.json.
// For labels that don't carry an effort suffix (e.g. "Claude Sonnet
// 4.6 (Thinking)"), the base is the catalogue ID with the trailing
// "-thinking" stripped.
//
// Note: this is distinct from print_slash.modelBaseSlug which strips
// the effort from an *ID*. The picker works in label-space and looks
// up the matching ID first.
func panelModelBaseSlug(label string) string {
	for _, m := range cli.ReferenceModels() {
		if m.Label == label {
			id := m.ID
			for _, suf := range []string{"-high", "-medium", "-low", "-thinking"} {
				if strings.HasSuffix(id, suf) {
					return strings.TrimSuffix(id, suf)
				}
			}
			return id
		}
	}
	return label
}

// effortSlugForModelCursor maps (base, cursor) to the effort slug that
// belongs in the catalogue ID. For Gemini (low/medium/high) the cursor
// directly indexes; for Claude/GPT-OSS the cursor is forced to "medium".
func effortSlugForModelCursor(base string, cursor int) string {
	switch base {
	case "gemini-3.8-flash":
		switch cursor {
		case 0:
			return "low"
		case 2:
			return "high"
		default:
			return "medium"
		}
	default:
		return "medium"
	}
}

// syncBackendForModel rebuilds the transport after a model change. The
// selected family decides the endpoint: catalogue Gemini/Claude/GPT-OSS
// go to CloudCode, MiniMax to the agw gateway. A nil factory (unit tests
// constructing runMode directly) leaves the backend untouched.
func (m *runMode) syncBackendForModel() {
	if m.backendFor == nil || strings.TrimSpace(m.model) == "" {
		return
	}
	if b := m.backendFor(m.model); b != nil {
		m.backend = b
	}
	// Cycle 292: a new selection puts the reference's
	// <USER_SETTINGS_CHANGE> block on the next user turn.
	m.wireSettingsPending = true
}

// cycleModelEffort advances `m.panel.effortCursor` by `dir` (+1 / -1)
// with wrap-around (low ⇆ high across the boundary, medium stays in
// the middle), then persists the new (model, effort) catalogue slug to
// settings.json and updates m.model / m.statusModel so the status row
// reflects the change on the very next frame.
//
// Cycle `model-left-right-effort` deepens cycle 197: the prior clamp
// behaviour only moved the slider — settings.json was written only on
// Enter, so a user that cycled the effort and then closed the panel
// without selecting (Esc, or selecting a different row) lost the
// preview. Now ←/→ is the change-of-record: settings.json sees the
// same slug `selectModelEffort` would write on Enter, the status row
// picks up the new tier on the next render, and the picker slider
// redraws because panelModelLines reads m.panel.effortCursor live.
//
// Only the three-tier Gemini row is multi-effort; the other rows are
// fixed and ←/→ remain inert on them — calling catalogueID(c) on a
// non-Gemini row would still yield the row's fixed slug, but we never
// want to overwrite the user's pinned model just because their cursor
// wandered onto Claude Sonnet and they pressed an arrow.
func (m *runMode) cycleModelEffort(dir int) {
	idx := m.panel.cursor
	rows := modelRowsView()
	if idx < 0 || idx >= len(rows) {
		return
	}
	row := rows[idx]
	if row.slug != "gemini-3.8-flash" {
		return
	}
	// 3-tier wrap: low → medium → high → low → … in either direction.
	c := m.panel.effortCursor + dir
	if c < 0 {
		c = 2
	} else if c > 2 {
		c = 0
	}
	m.panel.effortCursor = c
	slug := row.catalogueID(c)
	_ = auth.SetSettingsField("model", slug)
	if entry, ok := catalogueEntry(slug); ok {
		m.model = entry.Label
		m.statusModel = stripTier(entry.Label)
	} else {
		// Defensive: the Gemini-low/medium/high IDs are always in the
		// catalogue (cli.ReferenceModels pins all three), so this
		// branch is unreachable in practice. Falling back to the slug
		// mirrors selectModelEffort's custom-name fallback so we don't
		// leak a half-applied state.
		m.model = slug
		m.statusModel = stripTier(slug)
	}
	m.syncBackendForModel()
}

// selectModelEffort persists the highlighted (model, effort) as the
// new settings.json "model" key, updates m.model so the status row and
// the banner reflect the selection, and returns the display label to
// echo. Cycle 197.
//
// Persisted shape: the catalogue ID (e.g. "gemini-3.8-flash-medium") —
// the same form `agy -p /model` prints as its first column and the
// form modelAvailable() matches. m.model is set to the *label* so the
// status row renders "Gemini 3.8 Flash · medium" rather than the raw
// slug. Returns "" when the row has no catalogue ID (Gemini 4 Argon).
func (m *runMode) selectModelEffort() string {
	idx := m.panel.cursor
	rows := modelRowsView()
	if idx < 0 || idx >= len(rows) {
		return ""
	}
	row := rows[idx]
	if row.slug == "" {
		return "" // not selectable on this account
	}
	slug := row.catalogueID(m.panel.effortCursor)
	_ = auth.SetSettingsField("model", slug)
	if entry, ok := catalogueEntry(slug); ok {
		m.model = entry.Label
		m.statusModel = stripTier(entry.Label)
		m.syncBackendForModel()
		return entry.Label
	}
	// Custom-name row (MiniMax M3 etc.): the slug IS the label.
	m.model = slug
	m.statusModel = stripTier(slug)
	m.syncBackendForModel()
	return slug
}

// catalogueEntry looks up the catalogue by slug. Used by
// selectModelEffort to derive the display label from a chosen row's
// catalogue ID — a name like "minimax-m3" maps to "MiniMax M3".
func catalogueEntry(slug string) (cli.ModelEntry, bool) {
	for _, m := range cli.ReferenceModels() {
		if m.ID == slug {
			return m, true
		}
	}
	return cli.ModelEntry{}, false
}

// slashEchoLines renders the reference's transcript block for one slash
// command:
//
//	\x1b[1m\x1b[38;5;26m> /rename\x1b[0m
//	\x1b[1m\x1b[38;5;26m  ⎿  \x1b[0m\x1b[38;2;88;110;117mError: …\x1b[39m
//	<blank>
//
// Verified byte-for-byte against /tmp/agycmp/panels/ppref.rename.raw.txt.
func slashEchoLines(cmd, msg string) []string {
	return []string{
		cWarningBlueOpen + "> " + cmd + cWarningBlueClose,
		cWarningBlueOpen + "  \u23bf  " + cWarningBlueClose + panelGray238 + msg + panelGray238C,
		"",
	}
}

// ---- Cycle 223: panelAddDir /add-dir picker -------------------------------

// panelAddDirLines renders the /add-dir picker. REF 1.4.0 does not render
// a panel for `/add-dir` no-arg — it just echoes `> /add-dir ⎿ error: no
// directory specified` (`/private/tmp/agy217/caps/ref.adddir-bare.raw.txt`).
// Cycle 223 closes TODO §2.1's last A-class panel gap by giving OUR (which
// otherwise mirrors the same echo) a modal overlay:
//
//	Add Directory to Workspace
//	                       <- blank
//	  Trusted workspaces:   <- dim gray 241 heading
//	  /Users/...            <- dim gray 241 line per entry (from
//	                          auth.LoadTrustedWorkspaces)
//	  ...
//	                       <- blank
//	  Path: <text>          <- input row; the cursor is a single reverse-
//	                          video cell (searchCursorBlock) appended to
//	                          the typed text, same primitive /model and
//	                          /config use
//	                       <- blank
//	Keyboard: enter Add Directory  esc Cancel
//
// The title is the natural-language rendering of the cycle-217 success
// line ("Added %s to workspace") — same wording the cycle picker applies
// to its other modals. The keyboard hint's `enter Add Directory` is the
// closest analogue of REF's `/mcp` and `/tasks` legends; the brief
// explicitly calls out "press Enter to add, esc to close" as the
// expected closing.
//
// No-arg /add-dir is the only path that opens this panel — see
// openSlashPanelArgs in slash_codesearch.go.
func (m *runMode) panelAddDirLines() []string {
	trusted := auth.LoadTrustedWorkspaces()
	lines := []string{
		panelTitleOpen + "Add Directory to Workspace" + panelTitleClose,
		"",
	}
	if len(trusted) == 0 {
		lines = append(lines,
			cDimGrayOpen+"  No trusted workspaces yet."+cDimGrayClose)
	} else {
		lines = append(lines,
			cDimGrayOpen+"  Trusted workspaces:"+cDimGrayClose)
		for _, dir := range trusted {
			lines = append(lines,
				"  "+cDimGrayOpen+dir+cDimGrayClose)
		}
	}
	// The cursor sits at the end of the typed text. The reverse-video
	// cell is the same primitive /model's Search: box and /config's Search:
	// box use (searchCursorBlock). On every render we draw the latest
	// m.panel.text + the cursor — no blink, since the REF path this
	// replaces has no panel to mirror the cadence from.
	lines = append(lines,
		"",
		"  Path: "+m.panel.text+searchCursorBlock,
		"",
		panelKeyboardHint("enter", "Add Directory", "esc", "Cancel"),
	)
	return lines
}

// handleAddDirKey is the /add-dir picker's key handler. The panel owns
// every key until it is dismissed:
//
//   - Esc → closePanel (writes the standard "Exited /add-dir command"
//     echo — /add-dir stays in the exit-notice=true group because the
//     cycle-193 set explicitly carved out only specific panel kinds
//     like /mcp /feedback for silent close).
//   - Enter → run addDirFeedback on the typed text, emit its return
//     value through slashRespond, and closePanelSilent. The cycle-217
//     echo path handles "error: no directory specified" (empty input),
//     "error: not a directory: …" (regular file) and "Added <path> to
//     workspace" (success); the panel reuses it byte-for-byte so the
//     panel-form and with-args forms share one renderer. Going through
//     the silent close — not closePanel — keeps the standard exit
//     notice from doubling up with the slashRespond block.
//   - Backspace → drop one rune.
//   - Any printable → append.
//
// Falls through to the global bindings only when ctrl+*, alt+* or
// another modified key shows up (mirrors handleFeedbackTextKey).
func (m *runMode) handleAddDirKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEsc:
		return true, m.closePanel()
	case key.Code == tea.KeyEnter:
		msg := addDirFeedback(m.panel.text)
		m.slashRespond("/add-dir", msg)
		return true, m.closePanelSilent()
	case key.Code == tea.KeyBackspace:
		if r := []rune(m.panel.text); len(r) > 0 {
			m.panel.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if isTypedText(key) {
		m.panel.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}
