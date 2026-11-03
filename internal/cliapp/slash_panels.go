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

package cliapp

import (
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/features/auth"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
	"github.com/justwasm/pigasus/palette"
)

// Palette. Indices are the xterm-256 codes REF emits (post-tmux
// normalisation). cBlueOpen / cDimGrayOpen / cWarningBlueOpen are the
// repo's recovered true-colour constants and are reused rather than
// duplicated.
var (
	panelGray238  = palette.SubtleOpen // dim descriptions ("No MCP servers configured.")
	panelGray238C = "\x1b[39m"
)

const (
	// panelGray166 is the orange-red foreground the reference uses for a
	// warning line (recovered verbatim from
	// /tmp/agycmp/panels/ppref.schedule.raw.txt and ppref.browser.raw.txt
	// — both lines were captured at fg 166; cycle 217). The clone uses it
	// for the warning row a failing /browser run prints.
	panelGray166  = "\x1b[38;5;166m"
	panelGray166C = "\x1b[39m"
)

// slashPanel is the live panel state on the runMode.
type slashPanel struct {
	kind   panelregistry.Kind
	cmd    string // "/mcp" — the Esc notice quotes it
	cursor int    // selected index into the panel's row list
	window int    // index of the first visible row in scrollable pickers
	// Cycle 381: the /agents panel. The panel itself lives in
	// internal/ui/views/agentspanel: it owns the markdown-agent list,
	// the running-agent section, the cursor, the keys and the prepared
	// switch. The reference keeps the panel open after Enter, switches
	// on Esc, and forks the conversation at that point.
	ag *agentspanel.Panel

	// Cycle 381: the /feedback panel. The panel itself lives in
	// internal/ui/views/feedbackpanel: it owns the two stages, the six
	// categories, the cursor, the free-text buffer and the keys.
	fb *feedbackpanel.Panel

	// Cycle 381: the /credits panel. The panel itself lives in
	// internal/ui/views/creditspanel: it owns the two actions, the
	// cursor, the frame and the keys.
	cr *creditspanel.Panel
	// Cycle 381: the /plugin picker. The panel itself lives in
	// internal/ui/views/pluginsui: it owns the two tabs, the search box,
	// the Discover shelves, the inline local-install prompt, the cursor,
	// the keys and the writes. The rows are re-read from the install tree
	// and the marketplace feed on every draw, so the panel never shows a
	// stale copy of either.
	plg *pluginsui.Panel

	// Cycle 381: the /rename panel. The panel itself lives in
	// internal/ui/views/renamepanel: it owns the single-line input,
	// the prompt, the placeholder, the rows and the keys.
	rn *renamepanel.Panel

	// Cycle 381: the /add-dir panel. The panel itself lives in
	// internal/ui/views/adddirpanel: it owns the path buffer, the
	// trusted-workspace list, the rows and the keys.
	ad *adddirpanel.Panel

	// Cycle 381: the viewport-backed viewer panels (/usage, /changelog
	// and /feature-diff). The panel itself lives in
	// internal/ui/views/viewerpanel: it owns the viewport, the pager
	// keybindings and the scroll arithmetic. The scroll offset itself
	// stays the host's — panel.window is shared with the /codesearch
	// list and the /tasks detail views — so the panel takes the offset
	// as an argument and hands the clamped one back.
	vw *viewerpanel.Panel

	// Cycle 381: the /diff panel. The panel itself lives in
	// internal/ui/views/diffpanel: it owns the three tabs (All
	// Changes, Per Turn, Commit Tree), the cursor, the visible window,
	// the inline diff subview, its row cursor, the per-hunk fold
	// state, the keys and the rows. The panel is rebuilt on every
	// open, so a stale cursor, a stale tab or a stale fold state is
	// never shown.
	df *diffpanel.Panel

	// Cycle 381: the /codesearch panel. The panel itself lives in
	// internal/ui/views/codesearchpanel: it owns the argument tail, the
	// query, the hit set, the file set, the spinner, the cursor and the
	// scroll window, the keys and the rows. The search and the runner
	// it fills live in internal/features/tools/codesearch.
	csp *codesearchpanel.Panel

	// Cycle 225: /usage quota fetch state. The zero value's state is
	// quotaLoading, which the height-aware renderer reads to pick the
	// "Loading quota summary..." spinner row; the height-0 renderer (unit
	// tests, fallback) bypasses this and emits the cycle-193 placeholder
	// doc byte-exact.
	quota quotaState

	// Cycle provider-model-editor: the /provider panel. The panel itself
	// lives in internal/ui/views/providerpanel: it owns the rows, the
	// edit form, the keys and the writes. The rows are re-read from the
	// configuration file on every open and after every write, so the
	// panel never shows a stale copy of a file the CLI and the wrapper
	// share.
	prov *providerpanel.Panel

	// Cycle multi-account-switch: the /accounts panel. The panel itself
	// lives in internal/ui/views/accountspanel: it owns the account
	// list, the pool policy, the tab bar, the keys and the writes. The
	// rows are re-read from the account registry on every open and
	// after every switch, so the panel never shows a stale account
	// list.
	acct *accountspanel.Panel

	// Cycle 381: the /config settings panel. The panel itself lives in
	// internal/ui/views/configpanel: it owns the search box, the row
	// list, the inline option submenu, the keys and the writes. The
	// rows are re-read from settings.json on every open, so the panel
	// never shows a stale copy of the file.
	cfg *configpanel.Panel

	// Cycle 381: the /model picker. The panel itself lives in
	// internal/ui/views/modelpicker: it owns the search box, the row
	// list, the effort slider, the keys and the writes. The rows are
	// re-read from the catalogue on every open, so the panel never shows
	// a stale copy of the model list.
	mdl *modelpicker.Panel

	// Cycle 381: the /skills picker. The panel itself lives in
	// internal/ui/views/skillsui: it owns the grouped list, the cursor,
	// the per-skill detail view and its scroll window. The rows are
	// re-read from the catalogue on every open, so the panel never shows
	// a stale copy of the skill list.
	sk *skillsui.Panel

	// Cycle 381: the /permissions editor. The panel itself lives in
	// internal/ui/views/permissionspanel: it owns the scope picker, the
	// per-scope permission lists, the add/edit rule form, the cursor,
	// the keys and the writes. The rules are re-read from the settings
	// store and the project file on every draw, so the panel never shows
	// a stale list.
	perm *permissionspanel.Panel

	// Cycle 381: the /hooks editor. The panel itself lives in
	// internal/ui/views/hookspanel: it owns the hook-type list, the
	// per-type matcher list, the add-matcher and add-hook forms, the
	// cursor and the keys. The panel is rebuilt on every open, so a
	// stale stage or a stale form text is never shown.
	hk *hookspanel.Panel

	// Cycle 381: the /tasks panel. The panel itself lives in
	// internal/ui/views/taskspanel: it owns the task list, the cursor,
	// the two detail views (the task detail and the subagent detail)
	// and their scroll window, the keys and the rows. The panel is
	// rebuilt on every open, so a stale cursor or a stale detail view
	// is never shown.
	tp *taskspanel.Panel

	// Cycle 381: the /effort panel. The panel itself lives in
	// internal/ui/views/effortpanel: it owns the slider's marker, the
	// frame and the keys. The panel is rebuilt on every open, so a stale
	// marker is never shown.
	ef *effortpanel.Panel

	// Cycle 381: the /rewind picker. The panel itself lives in
	// internal/ui/views/rewindpanel: it owns the cursor, the visible
	// window, the rows and the keys. The panel is rebuilt on every
	// open, so a stale cursor or a stale window is never shown.
	rw *rewindpanel.Panel
}

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

// panelExitNotice reports whether Escaping the panel leaves the
// "⎿  Exited /<cmd> command" line in the transcript.
//
// Cycle 351 re-derived the whole set from agy 1.4.8 itself. The
// reference's binary carries the ten literal messages "Exited /diff
// command", "Exited /help command", "Exited /hooks command", "Exited
// /model command", "Exited /tasks command", "Exited /agents command",
// "Exited /effort command", "Exited /skills command", "Exited /credits
// command" and "Exited /artifact command" — plus one generic format
// string, "Exited %-*s %s", which is how /config's own message is
// built. Every other panel closes silently.
//
// A live 1.4.8 probe (one command, one Esc, one capture per panel)
// agrees with that reading: /config, /diff, /skills, /agents, /effort,
// /credits, /artifact, /model, /hooks, /tasks and /help echo the
// notice, while /usage, /permissions, /context, /codesearch,
// /changelog, /plugin, /mcp, /feedback and /rewind close silently.
// /add-dir is not a panel at all in the reference — it answers "error:
// no directory specified" inline — so it does not echo either.
//
// Evidence 【一手】: agy 1.4.8 (tmux, one probe per panel).
// Evidence 【strings】: the 1.4.8 string table.
func (m *runMode) initOpenedSlashPanel(kind panelregistry.Kind) {
	m.initOpenedSlashPanelData(kind)
}

// initOpenedSlashPanelData loads the data the panel shows.
//
//   - /agents discovers the agent definitions in the workspace.
//   - Cycle 225: /usage opens in the loading state on a real terminal.
//     The first usageQuotaTickMsg flips it to quotaLoaded and stamps the
//     two stub bucket refill times; subsequent ticks tick the countdown.
//     The height-0 fallback (unit tests) ignores this and renders the
//     cycle-193 placeholder doc byte-exact (see panelUsageLines).
//   - The viewer panels get their viewport so key handling delegates to
//     bubbles/viewport.Model.
//   - Cycle provider-model-editor: /provider reads the configuration
//     file when it opens, so the panel shows what is on disk and not what
//     was on disk when the panel was last used.
//   - Cycle multi-account-switch: /accounts reads the account registry
//     when it opens, so the panel shows the accounts the clone knows
//     about now — and the account the shared credential belongs to.
func (m *runMode) initOpenedSlashPanelData(kind panelregistry.Kind) {
	if kind == panelregistry.Usage && m.height > 0 {
		// Drop any summary from a previous open; the async fetch below
		// fills it in.
		m.panel.quota = quotaState{state: quotaLoading}
	}
	if kind.IsViewer() {
		m.initViewerViewport()
	}
	if kind == panelregistry.Provider {
		m.panel.prov = m.newProviderPanel()
		m.panel.prov.Reload()
		m.panel.prov.Notice("")
	}
	if kind == panelregistry.Config {
		// Cycle 381: the /config panel is rebuilt on every open, so the
		// search box, the cursor and the memory-only values never
		// survive a close.
		m.panel.cfg = m.newConfigPanel()
	}
	if kind == panelregistry.Model {
		// Cycle 381: the /model panel is rebuilt on every open, so the
		// search box, the cursor and the effort slider's position never
		// survive a close.
		m.panel.mdl = m.newModelPanel()
	}
	if kind == panelregistry.Plugin {
		// Cycle 381: the /plugin panel is rebuilt on every open, so the
		// search box, the cursor, the "enabled only" filter and the
		// local-install prompt never survive a close.
		m.panel.plg = m.newPluginPanel()
	}
	if kind == panelregistry.Skills {
		// Cycle 381: the /skills panel is rebuilt on every open, so the
		// cursor, the detail view and its scroll window never survive a
		// close.
		m.panel.sk = m.newSkillsPanel()
	}
	if kind == panelregistry.Permissions {
		// Cycle 381: the /permissions panel is rebuilt on every open, so
		// the cursor, the scope, the tab and the form's text never
		// survive a close.
		m.panel.perm = m.newPermissionsPanel()
	}
	if kind == panelregistry.Hooks {
		// Cycle 381: the /hooks panel is rebuilt on every open, so the
		// cursor, the stage and the form's text never survive a close.
		m.panel.hk = m.newHooksPanel()
	}
	if kind == panelregistry.Agents {
		// Cycle 381: the /agents panel is rebuilt on every open, so the
		// cursor and the prepared switch never survive a close. The
		// markdown agents are discovered now: the discovery reads the
		// workspace's and the user's markdown trees.
		m.panel.ag = m.newAgentsPanel()
	}
	if kind == panelregistry.Feedback {
		// Cycle 381: the /feedback panel is rebuilt on every open, so
		// the cursor, the stage and the draft never survive a close.
		m.panel.fb = m.newFeedbackPanel()
	}
	if kind == panelregistry.Credits {
		// Cycle 381: the /credits panel is rebuilt on every open, so
		// the cursor never survives a close.
		m.panel.cr = m.newCreditsPanel()
	}
	if kind == panelregistry.Tasks {
		// Cycle 381: the /tasks panel is rebuilt on every open, so the
		// cursor, the detail view and its scroll window never survive a
		// close.
		m.panel.tp = m.newTasksPanel()
	}
	if kind == panelregistry.Effort {
		// Cycle 381: the /effort panel is rebuilt on every open, so the
		// slider's marker never survives a close. The panel seeds itself
		// on the tier the session runs at — the reference opens the
		// slider on the current tier (REF shows the ◉ on "high" and
		// "  (current)" beside the blurb).
		m.panel.ef = m.newEffortPanel()
	}
	if kind == panelregistry.CodeSearch {
		// Cycle 381: the /codesearch panel is rebuilt on every open, so
		// the query, the hit set, the spinner and the cursor never
		// survive a close.
		m.panel.csp = m.newCodesearchPanel()
	}
	if kind == panelregistry.Rewind {
		// Cycle 381: the /rewind picker is rebuilt on every open, so the
		// cursor and the visible window never survive a close.
		m.panel.rw = m.newRewindPanel()
	}
	if kind == panelregistry.Diff {
		// Cycle 381: the /diff panel is rebuilt on every open, so the
		// active tab, the cursor, the visible window and the inline
		// subview's fold state never survive a close.
		m.panel.df = m.newDiffPanel()
	}
	if kind == panelregistry.Rename {
		// Cycle 381: the /rename panel is rebuilt on every open, so
		// the seed, the typed name and the cursor never survive a
		// close. The panel is also armed with the input's blink
		// command — the bare `/rename` opens the panel through
		// openSlashPanel, so this is the only place that path can
		// build it.
		m.panel.rn = m.newRenamePanel()
		m.armRenameFocus()
	}
	if kind == panelregistry.AddDir {
		// Cycle 381: the /add-dir panel is rebuilt on every open, so
		// the typed path never survives a close.
		m.panel.ad = m.newAddDirPanel()
	}
	if kind == panelregistry.Accounts {
		// Cycle 381: the /accounts panel is rebuilt on every open, so
		// the account list, the cursor and the policy never survive a
		// close.
		m.panel.acct = m.newAccountsPanel()
		m.panel.acct.Reload()
		m.panel.acct.ClearNotice()
		// /pool opens the same panel on its Pool tab: the policy that
		// decides which account serves a turn.
		if m.panel.cmd == "/pool" {
			m.panel.acct.SetTab(accountspanel.TabPool)
		}
		m.panel.acct.ReloadPolicy()
	}
}

// openSlashPanel opens the modal panel for cmd if the reference renders
// one. Returns false for every other command so the caller can fall
// through to the normal handler.
func (m *runMode) openSlashPanel(cmd string) bool {
	// Cycle 206: the reference resolves the catalog's aliases first, so
	// `/cs` and `/search` open the Code Search panel like `/codesearch`
	// (and `/settings` / `/quota` / `/undo` open their panels).
	//
	// Cycle 381, tranche 88: the alias rule and the command → panel
	// answer live in internal/ui/slashcommands, the one owner of the
	// slash-command vocabulary.
	kind, ok := slashcommands.PanelFor(cmd)
	if !ok {
		return false
	}
	if cmdInfo, found := slashcommands.Find(cmd); found {
		sctx := m.slashScopeContext()
		if !slashcommands.IsAvailable(cmdInfo, sctx) {
			return false
		}
	}
	// Cycle 383 (A-31): the panel carries the *canonical* name, so every
	// alias of a command closes with the same bytes. The reference's own
	// notice names the command, not the spelling the user typed
	// ("Exited /config command"), and the binary carries one literal per
	// command rather than one per alias.
	canonical := cmd
	if c, ok := slashcommands.Canonical(cmd); ok {
		canonical = c
	}
	m.panel = slashPanel{kind: kind, cmd: "/" + canonical}
	m.initOpenedSlashPanel(kind)
	return true
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
	lbl, _ := modelcatalog.ResolveEffectiveModel("")
	return lbl
}
