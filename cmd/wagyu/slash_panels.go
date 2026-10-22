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
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
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

// ---- Cycle 193: seven A-class panels --------------------------------------

// searchCursorBlock is the `Search:` input cursor REF draws as one
// reverse-video cell (`\x1b[7m\x1b[37m \x1b[0m`). Both /model and
// /config reuse it.
const searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"

// searchUnderline is the 20-cell `─` row REF draws under the Search:
// input, in separator grey (256-colour 253).
const searchUnderline = "\x1b[38;2;238;232;213m────────────────────\x1b[39m"




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

