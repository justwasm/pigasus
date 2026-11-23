// Package slashpicker is the slash-command completion popup: the
// overlay the composer raises above the prompt block when the buffer
// starts with "/".
//
// The panel owns the overlay's state — the filtered rows, the
// highlight, the scroll window, the on-screen rectangle — and the rows
// it writes. The host that owns the composer keeps the facts the
// panel cannot know (the terminal's width, whether the alternate
// screen is up, which screen row is the composer, what the composer
// holds) and hands them to the panel as seams.
//
// 【一手】 agy 1.4.6, live tmux capture: the popup renders five
// visible rows, the "↑ N more" / "↓ N more" tails, a blank separator
// and the "↑/↓ Navigate · enter Select · tab Complete" hint row.
//
// Lifecycle:
//
//	idle         the buffer does not start with "/"
//	picker-empty the buffer starts with "/" but the filter returns no
//	             row (the overlay stays visible and renders the
//	             "   No matches" row — cycle 93)
//	picker-shown the buffer starts with "/" and at least one row
//	             matches
//
// The behaviour matches the v1.3.5 binary's captures:
//
//	"/a"       -> popup open, scroll offset 0, /add-dir highlighted
//	"/add"     -> popup open, /add-dir highlighted (filtered subset)
//	"/xyz"     -> popup open but empty; renders "   No matches"
//	Up arrow   -> the previous row of the filtered subset
//	Down arrow -> the next row of the filtered subset
//	Tab        -> replaces the buffer with the selected command's name
//	Enter      -> submits the buffer (as text; the "/" is preserved)
//	Esc        -> closes the popup
//
// The popup state is held outside the buffer (no "/command \x00"
// sentinel in the composer): it is a derived view of the buffer and
// the highlight.
package slashpicker

import (
	"strings"

	shlex "github.com/carapace-sh/carapace-shlex"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// ModelRow is one row of the "/model <query>" completion popup: the
// display label the reference shows ("Gemini 3.8 Flash (High)") and
// the catalogue id ("gemini-3.8-flash-high").
type ModelRow struct {
	Label string
	ID    string
}

// Seams are what the panel needs from the host that owns it.
//
// A nil seam reads as its zero value: the panel falls back to a
// hidden popup, a zero width, a composer that holds nothing. The
// panel never reaches into the host.
type Seams struct {
	// AltScreen reports whether the session runs in the alternate
	// screen. The mouse handlers answer false outside it.
	AltScreen func() bool
	// Width returns the terminal's width in columns.
	Width func() int
	// IsInputY reports whether a screen row belongs to the composer.
	IsInputY func(y int) bool
	// Buffer returns the composer's current text.
	Buffer func() string
	// SetBuffer replaces the composer's text.
	SetBuffer func(s string)
	// CustomCommands returns additional dynamic slash commands (e.g. from the active Agent backend).
	CustomCommands func() []slashcommands.Command
	// ScopeContext returns the active engine scope and capabilities for filtering commands.
	ScopeContext func() slashcommands.ScopeContext
}

// Panel is the slash-command completion popup.
//
// Every method is safe on a nil panel: a host that never built the
// popup answers "not visible" and writes nothing.
type Panel struct {
	seams Seams

	// visible is true while the overlay should be rendered. True when
	// the buffer starts with "/", regardless of whether the filter
	// returned 0 or more rows (cycle 93).
	visible bool
	// empty is true when visible but the row set is empty.
	empty bool
	// all is the filtered row set (recomputed when the buffer changes).
	all []slashcommands.Command
	// idx is the highlighted row. -1 when the row set is empty.
	idx int
	// hidden is a session-level set of slash command names to exclude
	// from the popup whatever the buffer holds. Cycle 264: agy 1.4.1
	// hides "/remote-control" when AGY_ADC_AUTH is in effect.
	hidden map[string]bool
	// modelMode says the popup holds the model list rather than the
	// command list — the buffer is "/model <query>".
	modelMode bool
	// voiceMode says the popup holds voice subcommands or subarguments.
	voiceMode bool
	// voiceSubcmd is the current subcommand being completed ("" for root subcommands).
	voiceSubcmd string
	// terminalMode says the popup holds /terminal subcommands or session IDs.
	terminalMode bool
	// terminalSubcmd is the current /terminal subcommand being completed ("" for root subcommands).
	terminalSubcmd string
	// resumeMode says the popup holds saved session IDs for /resume.
	resumeMode bool
	// modelSource returns the catalogue the model mode filters. It is
	// a function so the catalogue can change under the popup (the
	// user edited the provider configuration) without the caller
	// re-arming the popup on every keystroke.
	modelSource func() []ModelRow
	// resumeSource returns available saved sessions for /resume autocompletion.
	resumeSource func() []slashcommands.Command
	// terminalSource returns active terminal sessions for /terminal autocompletion.
	terminalSource func() []slashcommands.Command
	// commandSource overrides default command catalogue filtering if set.
	commandSource func(prefix string) []slashcommands.Command
	// query is the text after "/model " in the model mode.
	query string

	// customCommandsSource returns the dynamic slash commands (e.g. from an active agent backend).
	customCommandsSource func() []slashcommands.Command

	// offset is the index of the first row drawn — the scroll window.
	offset int
	// layout is the popup's internal structure, filled in by Rows().
	layout layout
	// geom is the popup's on-screen rectangle.
	geom geom
	// bodyStart is the screen row the overlay starts on.
	bodyStart int
}

// New builds the popup with the host's seams.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// Reset clears the popup's state. The host calls it whenever the
// buffer's slash prefix is removed or replaced with a non-slash
// character.
func (p *Panel) Reset() {
	if p == nil {
		return
	}
	p.visible = false
	p.empty = false
	p.all = nil
	p.idx = -1
	p.modelMode = false
	p.voiceMode = false
	p.voiceSubcmd = ""
	p.terminalMode = false
	p.terminalSubcmd = ""
	p.resumeMode = false
	p.query = ""
}

// SetModelCatalogueSource installs the function that returns the rows
// the "/model <query>" popup offers. It is called on every
// re-derivation, so a catalogue that changes (the user edited the
// provider configuration) shows up on the next keystroke. A nil source
// leaves the model mode empty.
func (p *Panel) SetModelCatalogueSource(fn func() []ModelRow) {
	if p == nil {
		return
	}
	p.modelSource = fn
}

// SetCustomCommandsSource installs the function that returns dynamic slash commands
// SetCustomCommandsSource installs the function that returns dynamic slash commands
// offered by the active agent backend.
func (p *Panel) SetCustomCommandsSource(fn func() []slashcommands.Command) {
	if p == nil {
		return
	}
	p.customCommandsSource = fn
}

// SetResumeSource installs the function returning session items for the "/resume <query>" popup.
func (p *Panel) SetResumeSource(fn func() []slashcommands.Command) {
	if p == nil {
		return
	}
	p.resumeSource = fn
}

// SetTerminalSource installs the function returning active terminal sessions for "/terminal <subcmd> <query>".
func (p *Panel) SetTerminalSource(fn func() []slashcommands.Command) {
	if p == nil {
		return
	}
	p.terminalSource = fn
}

// IsTerminalMode reports whether the popup holds /terminal subcommands or session IDs.
func (p *Panel) IsTerminalMode() bool {
	return p != nil && p.terminalMode
}

// SetCommandSource overrides default command catalogue filtering if set.
func (p *Panel) SetCommandSource(fn func(prefix string) []slashcommands.Command) {
	if p == nil {
		return
	}
	p.commandSource = fn
}

// IsModelMode reports whether the popup holds the model list rather
// than the command list.
func (p *Panel) IsModelMode() bool {
	return p != nil && p.modelMode
}

// Query returns the text after "/model " in the model mode. Empty in
// the command mode.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// SetHiddenCommands replaces the popup's hidden-name set. Empty or nil
// argument list clears the set. Names are normalised to start with
// "/" so callers can pass either "/remote-control" or
// "remote-control" interchangeably.
//
// Cycle 264: agy 1.4.1 hides the /remote-control command when ADC
// is in effect. The host calls this once at start-up; the popup
// consults the set during Update, so callers do not have to call it
// again on every keystroke.
func (p *Panel) SetHiddenCommands(names ...string) {
	if p == nil {
		return
	}
	if len(names) == 0 {
		p.hidden = nil
		return
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if !strings.HasPrefix(n, "/") {
			n = "/" + n
		}
		set[n] = true
	}
	p.hidden = set
}

// Update re-derives the popup's state from the current buffer. It
// reports whether the state changed (the caller should redraw).
//
// The rules, matching the v1.3.5 binary:
//
//   - the buffer does not start with "/" -> hidden
//   - the buffer starts with "/" but matches no row -> visible and
//     empty (cycle 93: the reference shows the "   No matches"
//     overlay for /xyz and friends)
//   - the buffer starts with "/" and matches -> visible and populated
//   - when the filtered count drops below idx+1, the highlight clamps
//     to the last valid row
//
// The "/model <query>" line is the one exception: it is not a command
// prefix but an *argument* being typed, and the reference swaps the
// command rows for the model rows (【一手】 agy 1.4.6, live).
func (p *Panel) Update(buffer []rune) bool {
	if p == nil {
		return false
	}
	prevVisible := p.visible
	prevLen := len(p.all)
	if query, ok := modelQuery(buffer); ok {
		p.modelMode = true
		p.voiceMode = false
		p.voiceSubcmd = ""
		p.terminalMode = false
		p.terminalSubcmd = ""
		p.query = query
		p.all = p.filterModels(query)
		p.visible = true
		p.clampSelection()
		return true
	}
	p.modelMode = false

	if subcmd, query, ok := voiceQuery(buffer); ok {
		p.voiceMode = true
		p.voiceSubcmd = subcmd
		p.terminalMode = false
		p.terminalSubcmd = ""
		p.resumeMode = false
		p.query = query
		p.all = p.filterVoice(subcmd, query)
		p.visible = true
		p.clampSelection()
		return true
	}
	p.voiceMode = false
	p.voiceSubcmd = ""

	if subcmd, query, ok := terminalQuery(buffer); ok {
		p.terminalMode = true
		p.terminalSubcmd = subcmd
		p.resumeMode = false
		p.query = query
		p.all = p.filterTerminal(subcmd, query)
		p.visible = true
		p.clampSelection()
		return true
	}
	p.terminalMode = false
	p.terminalSubcmd = ""

	if query, ok := resumeQuery(buffer); ok {
		p.resumeMode = true
		p.query = query
		p.all = p.filterResume(query)
		p.visible = true
		p.clampSelection()
		return true
	}
	p.resumeMode = false
	p.query = ""
	if len(buffer) == 0 || buffer[0] != '/' {
		p.Reset()
		return p.visible != prevVisible || len(p.all) != prevLen
	}
	tok := readToken(buffer)
	if len([]rune(tok)) < len(buffer) {
		p.Reset()
		return p.visible != prevVisible || len(p.all) != prevLen
	}
	p.all = p.filterVisibleCommands(tok)
	p.visible = true
	p.clampSelection()
	return true
}

// clampSelection keeps the highlight inside the filtered row set.
func (p *Panel) clampSelection() {
	if len(p.all) == 0 {
		p.empty = true
		p.idx = -1
		return
	}
	p.empty = false
	if p.idx < 0 {
		p.idx = 0
	} else if p.idx >= len(p.all) {
		p.idx = len(p.all) - 1
	}
}

// filterVisibleCommands returns the rows the buffer's token matches,
// minus the hidden names, merging in dynamic agent commands when available.
func (p *Panel) filterVisibleCommands(prefix string) []slashcommands.Command {
	if p.commandSource != nil {
		return p.commandSource(prefix)
	}
	var sctx slashcommands.ScopeContext
	if p.seams.ScopeContext != nil {
		sctx = p.seams.ScopeContext()
	}
	if p.customCommandsSource != nil {
		sctx.Dynamic = p.customCommandsSource()
	} else if p.seams.CustomCommands != nil {
		sctx.Dynamic = p.seams.CustomCommands()
	}
	cmds := slashcommands.FilterScoped(prefix, sctx)
	if len(p.hidden) == 0 || len(cmds) == 0 {
		return cmds
	}
	filtered := cmds[:0:0]
	for _, c := range cmds {
		if !p.hidden[c.Name] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// filterModels returns the catalogue rows whose label or id contains
// the query, case-insensitively.
//
// The reference matches a *substring* of the display label or of the
// catalogue id — not a subsequence. 【一手】 agy 1.4.6, live: "onnet"
// matches "Claude Sonnet 4.6 (Thinking)" (both the label and the id
// contain it), "4.6" matches both Claude rows (the label), and "son"
// matches only the Sonnet row: "Claude Opus 4.6 (Thinking)" has no
// "son" substring, even though a subsequence matcher would accept it.
func (p *Panel) filterModels(query string) []slashcommands.Command {
	if p.modelSource == nil {
		return nil
	}
	rows := p.modelSource()
	q := strings.ToLower(query)
	out := make([]slashcommands.Command, 0, len(rows))
	for _, r := range rows {
		if q != "" &&
			!strings.Contains(strings.ToLower(r.Label), q) &&
			!strings.Contains(strings.ToLower(r.ID), q) {
			continue
		}
		out = append(out, slashcommands.Command{Name: r.Label, Description: r.ID})
	}
	return out
}

// Selected returns the highlighted row. nil when the popup is not
// visible.
func (p *Panel) Selected() *slashcommands.Command {
	if p == nil || !p.visible || p.idx < 0 || p.idx >= len(p.all) {
		return nil
	}
	return &p.all[p.idx]
}

// Visible reports whether the popup should be rendered.
func (p *Panel) Visible() bool {
	return p != nil && p.visible
}

// Empty reports whether the popup is visible but holds no filtered
// row (cycle 93). The renderer emits the "   No matches" row and the
// hint row instead of the popup's rows and the "↓ N more" tail.
// Always false when the popup is not visible.
func (p *Panel) Empty() bool {
	return p != nil && p.visible && len(p.all) == 0
}

// Len returns the number of filtered rows.
func (p *Panel) Len() int {
	if p == nil {
		return 0
	}
	return len(p.all)
}

// SelectedAt returns a pointer to the i-th row of the filtered list,
// or nil when i is out of range. Cycle 60 added this so the renderer
// can enumerate the visible rows without copying the slice.
func (p *Panel) SelectedAt(i int) *slashcommands.Command {
	if p == nil || i < 0 || i >= len(p.all) {
		return nil
	}
	return &p.all[i]
}

// Idx returns the highlighted row's index. -1 when the popup is
// hidden.
func (p *Panel) Idx() int {
	if p == nil {
		return -1
	}
	return p.idx
}

// MoveUp moves the highlight up by 1, clamping at 0. It reports
// whether the highlight actually changed.
func (p *Panel) MoveUp() bool {
	if p == nil || !p.visible || len(p.all) == 0 {
		return false
	}
	if p.idx <= 0 {
		return false
	}
	p.idx--
	return true
}

// MoveDown moves the highlight down by 1, clamping at len(all)-1.
func (p *Panel) MoveDown() bool {
	if p == nil || !p.visible || len(p.all) == 0 {
		return false
	}
	if p.idx >= len(p.all)-1 {
		return false
	}
	p.idx++
	return true
}

// SetIdx moves the highlight to the i-th row, clamping to the valid
// range. It reports whether the highlight actually changed. False when
// the popup is hidden or holds no row.
//
// Cycle 232 (the popup's mouse click and hover) drives SetIdx from
// the mouse handler: a click on row 3 or a hover over row 0 mirrors
// what an Up/Down arrow would do, but lands on a specific row
// instead of stepping.
func (p *Panel) SetIdx(i int) bool {
	if p == nil || !p.visible || len(p.all) == 0 {
		return false
	}
	if i < 0 {
		i = 0
	}
	if i >= len(p.all) {
		i = len(p.all) - 1
	}
	if p.idx == i {
		return false
	}
	p.idx = i
	return true
}

// readToken returns the leading slash-prefix token from buffer (up to
// the first whitespace, or the end of the buffer).
func readToken(buffer []rune) string {
	end := len(buffer)
	for i, r := range buffer {
		if r == ' ' || r == '\t' || r == '\n' {
			end = i
			break
		}
	}
	return string(buffer[:end])
}

// modelQuery reports whether the buffer is a "/model <query>" line and
// returns the query — the text after the command and its space.
//
// The reference enters the model mode as soon as the command is
// followed by a space, even with an empty query: "/model " lists every
// model (【一手】 agy 1.4.6, live). A bare "/model" is still a command
// prefix and shows the command rows.
func modelQuery(buffer []rune) (string, bool) {
	s := string(buffer)
	const prefix = "/model "
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return s[len(prefix):], true
}

// voiceQuery reports whether the buffer is a "/voice <query>" or "/speaker <query>" line and returns
// the subcommand and the query after it.
func voiceQuery(buffer []rune) (subcmd string, query string, ok bool) {
	s := string(buffer)
	const prefixVoice = "/voice "
	const prefixSpeaker = "/speaker "
	var after string
	var forceSpeaker bool
	if strings.HasPrefix(s, prefixVoice) {
		after = s[len(prefixVoice):]
	} else if strings.HasPrefix(s, prefixSpeaker) {
		after = s[len(prefixSpeaker):]
		forceSpeaker = true
	} else {
		return "", "", false
	}
	tokens, err := shlex.Split(after)
	var words []string
	if err == nil {
		for _, tok := range tokens {
			if tok.Value != "" {
				words = append(words, tok.Value)
			}
		}
	} else {
		words = strings.Fields(after)
	}
	if forceSpeaker {
		if len(words) == 0 {
			return "speaker", "", true
		}
		return "speaker", words[0], true
	}
	if len(words) == 0 {
		return "", "", true
	}
	if len(words) == 1 && !strings.HasSuffix(after, " ") {
		return "", words[0], true
	}
	sub := strings.ToLower(words[0])
	switch sub {
	case "speaker", "duplex", "volume", "speed", "preview":
		if len(words) == 1 {
			return sub, "", true
		}
		return sub, words[1], true
	default:
		return sub, "", false
	}
}

// resumeQuery reports whether the buffer is a "/resume <query>" or "/sessions <query>" line.
func resumeQuery(buffer []rune) (query string, ok bool) {
	s := string(buffer)
	const prefixResume = "/resume "
	const prefixSessions = "/sessions "
	if strings.HasPrefix(s, prefixResume) {
		return strings.TrimSpace(s[len(prefixResume):]), true
	}
	if strings.HasPrefix(s, prefixSessions) {
		return strings.TrimSpace(s[len(prefixSessions):]), true
	}
	return "", false
}

func (p *Panel) filterResume(query string) []slashcommands.Command {
	if p.resumeSource == nil {
		return nil
	}
	pool := p.resumeSource()
	if query == "" {
		out := make([]slashcommands.Command, len(pool))
		copy(out, pool)
		return out
	}
	q := strings.ToLower(query)
	var out []slashcommands.Command
	for _, c := range pool {
		if strings.HasPrefix(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}

var terminalSubcommands = []slashcommands.Command{
	{Name: "new", Description: "Create and open a new interactive terminal session"},
	{Name: "list", Description: "List active terminal sessions"},
	{Name: "switch", Description: "Switch to an active terminal session (<id>)"},
	{Name: "attach", Description: "Attach to an active terminal session (<id>)"},
	{Name: "rename", Description: "Rename the active terminal session (<title>)"},
	{Name: "close", Description: "Close a terminal session (<id>)"},
	{Name: "kill", Description: "Kill a terminal session (<id>)"},
	{Name: "run", Description: "Run a shell command in a terminal (<command>)"},
}

// terminalQuery reports whether the buffer is a "/terminal <query>" line and returns
// the subcommand and the query after it.
func terminalQuery(buffer []rune) (subcmd string, query string, ok bool) {
	s := string(buffer)
	const prefix = "/terminal "
	if !strings.HasPrefix(s, prefix) {
		return "", "", false
	}
	after := s[len(prefix):]
	tokens, err := shlex.Split(after)
	var words []string
	if err == nil {
		for _, tok := range tokens {
			if tok.Value != "" {
				words = append(words, tok.Value)
			}
		}
	} else {
		words = strings.Fields(after)
	}
	if len(words) == 0 {
		return "", "", true
	}
	if len(words) == 1 && !strings.HasSuffix(after, " ") {
		return "", words[0], true
	}
	sub := strings.ToLower(words[0])
	switch sub {
	case "switch", "attach", "open", "close", "kill":
		if len(words) == 1 {
			return sub, "", true
		}
		if len(words) == 2 && !strings.HasSuffix(after, " ") {
			return sub, words[1], true
		}
		return sub, "", false
	default:
		return sub, "", false
	}
}

func (p *Panel) filterTerminal(subcmd, query string) []slashcommands.Command {
	var pool []slashcommands.Command
	switch subcmd {
	case "switch", "attach", "open", "close", "kill":
		if p.terminalSource != nil {
			pool = p.terminalSource()
		}
	default:
		pool = terminalSubcommands
	}
	if query == "" {
		out := make([]slashcommands.Command, len(pool))
		copy(out, pool)
		return out
	}
	q := strings.ToLower(query)
	var out []slashcommands.Command
	for _, c := range pool {
		if strings.HasPrefix(strings.ToLower(c.Name), q) ||
			(subcmd != "" && (strings.Contains(strings.ToLower(c.Name), q) || strings.Contains(strings.ToLower(c.Description), q))) {
			out = append(out, c)
		}
	}
	return out
}

var voiceSubcommands = []slashcommands.Command{
	{Name: "speaker", Description: "Set or view text-to-speech speaker voice"},
	{Name: "preview", Description: "Audition a recorded voice sample (1-3, or all)"},
	{Name: "live", Description: "Start a live voice call"},
	{Name: "dictate", Description: "Dictate a prompt using your microphone"},
	{Name: "stop", Description: "End the active voice call"},
	{Name: "mute", Description: "Toggle microphone mute state"},
	{Name: "duplex", Description: "Toggle full-duplex barge-in audio"},
	{Name: "volume", Description: "Set playback volume (0-100)"},
	{Name: "speed", Description: "Set playback speed (0.5-3.0)"},
}

var voiceSpeakers = []slashcommands.Command{
	// Antigravity Personas
	{Name: "fola", Description: "Speaker voice: Fola (Antigravity default)"},
	{Name: "arlo", Description: "Speaker voice: Arlo (Antigravity)"},
	{Name: "cleo", Description: "Speaker voice: Cleo (Antigravity)"},
	{Name: "finn", Description: "Speaker voice: Finn (Antigravity)"},
	{Name: "gero", Description: "Speaker voice: Gero (Antigravity)"},
	{Name: "lora", Description: "Speaker voice: Lora (Antigravity)"},
	{Name: "neno", Description: "Speaker voice: Neno (Antigravity)"},
	{Name: "olin", Description: "Speaker voice: Olin (Antigravity)"},
	{Name: "tari", Description: "Speaker voice: Tari (Antigravity)"},
	// Gemini Prebuilt Voices
	{Name: "aoede", Description: "Speaker voice: Aoede (Gemini)"},
	{Name: "puck", Description: "Speaker voice: Puck (Gemini)"},
	{Name: "kore", Description: "Speaker voice: Kore (Gemini)"},
	{Name: "fenrir", Description: "Speaker voice: Fenrir (Gemini)"},
	{Name: "charon", Description: "Speaker voice: Charon (Gemini)"},
	{Name: "zephyr", Description: "Speaker voice: Zephyr (Gemini)"},
	{Name: "leda", Description: "Speaker voice: Leda (Gemini)"},
	{Name: "orus", Description: "Speaker voice: Orus (Gemini)"},
}

// voicePreviewSpeakers lists the personas that ship a recorded sample, so
// "/voice preview <tab>" completes only names that can actually be auditioned.
// keep in sync with internal/voice/previews/.
var voicePreviewSpeakers = []slashcommands.Command{
	{Name: "fola", Description: "Preview Fola (Antigravity default, 3 samples)"},
	{Name: "arlo", Description: "Preview Arlo (Antigravity, 3 samples)"},
	{Name: "cleo", Description: "Preview Cleo (Antigravity, 3 samples)"},
	{Name: "finn", Description: "Preview Finn (Antigravity, 3 samples)"},
	{Name: "gero", Description: "Preview Gero (Antigravity, 3 samples)"},
	{Name: "lora", Description: "Preview Lora (Antigravity, 3 samples)"},
	{Name: "neno", Description: "Preview Neno (Antigravity, 3 samples)"},
	{Name: "olin", Description: "Preview Olin (Antigravity, 3 samples)"},
	{Name: "tari", Description: "Preview Tari (Antigravity, 3 samples)"},
}

var voiceDuplexOptions = []slashcommands.Command{
	{Name: "on", Description: "Enable full-duplex voice mode"},
	{Name: "off", Description: "Disable full-duplex voice mode"},
}

var voiceVolumeOptions = []slashcommands.Command{
	{Name: "100", Description: "Full volume (100%)"},
	{Name: "75", Description: "High volume (75%)"},
	{Name: "50", Description: "Medium volume (50%)"},
	{Name: "25", Description: "Low volume (25%)"},
	{Name: "0", Description: "Mute volume (0%)"},
}

var voiceSpeedOptions = []slashcommands.Command{
	{Name: "1.0", Description: "Normal speed (1.0x)"},
	{Name: "1.2", Description: "Fast speed (1.2x)"},
	{Name: "1.5", Description: "Faster speed (1.5x)"},
	{Name: "2.0", Description: "Double speed (2.0x)"},
	{Name: "0.8", Description: "Slower speed (0.8x)"},
	{Name: "0.5", Description: "Half speed (0.5x)"},
}

func (p *Panel) filterVoice(subcmd, query string) []slashcommands.Command {
	var pool []slashcommands.Command
	switch subcmd {
	case "speaker":
		pool = voiceSpeakers
	case "preview":
		pool = voicePreviewSpeakers
	case "duplex":
		pool = voiceDuplexOptions
	case "volume":
		pool = voiceVolumeOptions
	case "speed":
		pool = voiceSpeedOptions
	default:
		pool = make([]slashcommands.Command, 0, len(voiceSubcommands)+len(voiceSpeakers))
		pool = append(pool, voiceSubcommands...)
		pool = append(pool, voiceSpeakers...)
	}
	if query == "" {
		out := make([]slashcommands.Command, len(pool))
		copy(out, pool)
		return out
	}
	q := strings.ToLower(query)
	var out []slashcommands.Command
	for _, c := range pool {
		if strings.HasPrefix(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}
