// Package configpanel holds the /config settings picker's catalogue and
// its pure layout rules: the 21-row settings table recovered from the
// reference, the display-mapping rules, the search filter, the window
// sizing and the column padding.
//
// The panel itself stays in cmd/wagyu because it reads and writes
// settings.json through internal/auth; everything here is a pure
// function of its arguments.
//
// The catalogue was recovered verbatim from
// /tmp/agycmp/probe_ref_scroll.py (all 20 top-level rows at height 50)
// and /tmp/agycmp/probe_ref_walk2.py (the row→settings.json key mapping
// and every value list, read off the reference by cycling ←/→ and
// diffing settings.json).
package configpanel

import (
	"strings"
	"unicode/utf8"
)

// Val is one stopping point of a setting's ←/→ cycle, as measured
// against the reference with /tmp/agycmp/probe_ref_walk2.py and
// probe_ref_vocab.py. `Stored` is the JSON value the reference writes;
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
type Val struct {
	Display string
	Stored  any
}

// Row is one rendered line of the /config picker. The table below is
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
type Row struct {
	Name        string
	SettingsKey string
	Values      []Val
	Description string
	// ShowWhenKey/ShowWhenVal gate a child row on another setting's raw
	// value ("Editor Mode › Insert First" is drawn only while
	// editorMode == "vim").
	ShowWhenKey string
	ShowWhenVal string
	// MemoryOnly: the reference toggles the value but writes nothing to
	// settings.json (Enable Telemetry). The clone keeps it in panel
	// state so the row still responds.
	MemoryOnly bool
}

// V builds a Val whose stored form is the given string, or nil
// (absent) when s is "".
func V(display, stored string) Val {
	if stored == "" {
		return Val{Display: display, Stored: nil}
	}
	return Val{Display: display, Stored: stored}
}

// B builds a boolean Val. The default reads as absent (nil);
// the non-default persists as a JSON bool.
func B(display string, stored any) Val {
	return Val{Display: display, Stored: stored}
}

// TelemetryRowName is the name of the "Enable Telemetry" row, which the
// reference locks for Business accounts.
const TelemetryRowName = "Enable Telemetry"

// SettingsRows is the static catalogue. Rows whose ShowWhen gate is
// not met are dropped by Rows().
var SettingsRows = []Row{
	{Name: "Agent Mode", SettingsKey: "agentMode",
		Values: []Val{
			V("default", ""), V("accept-edits", "accept-edits"),
			V("auto", "auto"), V("plan", "plan"),
		},
		Description: "'default': standard behavior. 'accept-edits': auto-approve file edits, prompt for commands. 'auto': auto-approve edits and safe commands, prompt on irreversible actions. 'plan': research and plan without making changes"},
	{Name: "Animation Speed", SettingsKey: "runningLightSpeed",
		Values: []Val{
			V("medium", ""), V("slow", "slow"), V("off", "off"), V("fast", "fast"),
		},
		Description: "Speed of the running light animation"},
	{Name: "Artifact Review", SettingsKey: "artifactReviewPolicy",
		Values: []Val{
			V("asks for review", ""), V("always proceeds", "always-proceed"),
			V("agent decides", "agent-decides"),
		},
		Description: "Agent always asks for review."},
	{Name: "Color Scheme", SettingsKey: "colorScheme",
		Values: []Val{
			V("terminal", ""), V("light", "light"),
			V("solarized light", "solarized light"),
			V("colorblind-friendly light", "colorblind-friendly light"),
			V("dark", "dark"), V("solarized dark", "solarized dark"),
			V("colorblind-friendly dark", "colorblind-friendly dark"),
			V("tokyo night", "tokyo night"),
		},
		Description: ""},
	{Name: "Conversation Picker Grouping", SettingsKey: "pickerGrouping",
		Values: []Val{
			V("none", ""), V("grouped", "grouped"),
		},
		Description: "How the /resume picker organizes conversations. Press ctrl+f in the picker to switch view for one session without changing this."},
	{Name: "Copy on Select", SettingsKey: "copyOnSelect",
		Values:      []Val{B("on", nil), B("off", false)},
		Description: "When enabled, releasing a mouse text selection copies it to the system clipboard. Only active in alt-screen mode. When disabled, the artifact viewer uses native terminal selection instead."},
	{Name: "Editor", SettingsKey: "editor",
		Values: []Val{
			V("auto ($EDITOR)", ""), V("vim", "vim"), V("emacs", "emacs"),
		},
		Description: "Preferred editor for viewing artifacts"},
	{Name: "Editor Mode", SettingsKey: "editorMode",
		Values:      []Val{V("default", ""), V("vim", "vim")},
		Description: "Configure how you edit the prompt (default or vim)"},
	{Name: "Editor Mode › Insert First", SettingsKey: "vimInsertFirst",
		Values:      []Val{B("off", nil), B("on", true)},
		Description: "Place vim in insert mode when the prompt gains focus.",
		ShowWhenKey: "editorMode", ShowWhenVal: "vim"},
	{Name: "Enable Telemetry", SettingsKey: "enableTelemetry",
		// agy 1.4.3 defaults this row to off on a clean settings.json
		// (live capture at 120x40, 2026-10-20); the 1.4.1 default was on.
		Values:      []Val{B("off", nil), B("on", true)},
		Description: "When toggled on, Pigasus CLI collects usage data to help Google enhance performance and features",
		MemoryOnly:  true},
	{Name: "Non-Workspace Access", SettingsKey: "allowNonWorkspaceAccess",
		Values:      []Val{B("off", nil), B("on", true)},
		Description: "Allow agent to access files outside workspace directories"},
	{Name: "Notifications", SettingsKey: "notifications",
		Values:      []Val{B("off", nil), B("on", true)},
		Description: "Bell + desktop notification on tool confirmation and agent completion"},
	{Name: "Queued Messages", SettingsKey: "queuedMessages",
		Values: []Val{
			V("Queue", ""), V("Send Immediately", "send-immediately"),
		},
		Description: "Configure when follow-up messages are sent."},
	{Name: "Rendering Mode", SettingsKey: "altScreenMode",
		// Cycle 294 (AGY 1.4.7, measured 2026-10-25): the row's two stops
		// are the alternate screen ("always") and the inline view
		// ("never"). The reference's own panel writes an *empty* value for
		// the inline stop, which its `json:"altScreenMode,omitempty"`
		// writer then drops from settings.json — so the reference's
		// inline option does not survive a restart, because an absent key
		// is the reference's *default* and its default is the alternate
		// screen (raw pty capture: agy 1.4.7 emits ?1049h for an absent
		// key and for "always", and stays inline for "never"; every other
		// value — "off", "inline", "default", "auto", "none", "false" — is
		// rejected by the reference's settings validation and falls back
		// to the default).
		//
		// The clone therefore writes the reference's *own* inline value,
		// "never", which agy 1.4.7 accepts and honours, and keeps the
		// alternate screen first so that the absent key (the default) is
		// the row's first entry: the catalogue's convention that the
		// first entry is the default state, and the only way the inline
		// stop is reachable from the default state.
		Values: []Val{
			V("no flickering (altscreen)", "always"), V("native terminal (inline)", "never"),
		},
		Description: "no flickering (altscreen) is ideal for long conversations but requires holding Shift/Option for native text selection. Native Terminal experience (inline) preserves native terminal behavior but may truncate long conversations and flicker when toggling views."},
	{Name: "Sandbox Mode", SettingsKey: "enableTerminalSandbox",
		Values:      []Val{B("off", nil), B("on", true)},
		Description: "Enables terminal sandbox restrictions."},
	{Name: "Show Active Tasks", SettingsKey: "hideActiveItems",
		Values:      []Val{B("on", nil), B("off", true)},
		Description: "Show active subagents and background tasks below the input box"},
	{Name: "Show Feedback Survey", SettingsKey: "showFeedbackSurvey",
		Values:      []Val{B("on", nil), B("off", false)},
		Description: "Show feedback survey"},
	{Name: "Show Tips", SettingsKey: "showTips",
		Values:      []Val{B("on", nil), B("off", false)},
		Description: "Show helpful tips while the agent is generating"},
	{Name: "Tool Permission", SettingsKey: "toolPermission",
		Values:      []Val{V("always-proceed", ""), V("strict", "strict")},
		Description: "Controls whether terminal commands require your approval before running."},
	{Name: "Use AI Credits", SettingsKey: "useG1Credits",
		Values:      []Val{B("off", nil), B("on", true)},
		Description: "Use your AI credits for model calls once your plan's quota runs out"},
	{Name: "Verbosity", SettingsKey: "verbosity",
		Values: []Val{
			V("medium", ""), V("low", "low"), V("high", "high"),
		},
		Description: "Controls how much detail is shown for the agent's tool calls, commands, and thoughts."},
}

// RowsFiltered returns the configuration rows matching the search query.
// When query is empty, all active rows are returned.
// Matching is case-insensitive substring search over the row name, description,
// and option display values.
func RowsFiltered(rows []Row, query string) []Row {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return rows
	}
	var filtered []Row
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Name), q) ||
			strings.Contains(strings.ToLower(r.Description), q) {
			filtered = append(filtered, r)
			continue
		}
		for _, v := range r.Values {
			if strings.Contains(strings.ToLower(v.Display), q) {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

// RawString renders a settings.json leaf as the string form used for
// gating comparisons.
func RawString(v any) string {
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

// JSONLeafEqual compares two settings.json leaves for cycle matching.
// nil (key absent) only equals nil.
func JSONLeafEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a == b
}

// DisplayFor maps a stored leaf back to the display form of the
// matching cycle value. A value not present in the list (e.g. a
// `toolPermission` explicitly written as "always-proceed" while the
// default is absent, or a hand-edited colour) falls back to the first
// entry, which is the reference's own behaviour.
//
// telemetryLocked, when set, forces the "off (disabled)" display — the
// Business-account lock the reference applies to the Enable Telemetry
// row (cycle 241, AGY 1.4.1).
func DisplayFor(row Row, raw any, telemetryLocked string) string {
	if telemetryLocked != "" && row.Name == TelemetryRowName {
		return telemetryLocked
	}
	for _, v := range row.Values {
		if JSONLeafEqual(v.Stored, raw) {
			return v.Display
		}
	}
	// The raw value names one of the *stored* strings even though the
	// list's default entry stores nil (Tool Permission).
	if s, ok := raw.(string); ok {
		for _, v := range row.Values {
			if v.Display == s {
				return v.Display
			}
		}
	}
	return row.Values[0].Display
}

// IsTelemetryRow reports whether the row is the Enable Telemetry row.
func IsTelemetryRow(row Row) bool {
	return row.Name == TelemetryRowName
}

// VisibleRowsFor returns how many /config rows the reference draws
// at this terminal height. Measured with probe_ref_height.py:
//
//	h=20 → 3    h=24 → 6    h=28 → 9    h=32 → 12    h=40 → 18    h≥44 → all
//
// i.e. `height*3/4 - 12`, clamped to [1, len(catalogue)] — the same 3/4
// shape as helpListWindowFor, offset by the /config chrome (title,
// search box, description, keyboard legend). Returns 10 for an unknown
// height (before the first WindowSizeMsg) so the byte-exact snapshots and
// the unit tests are unchanged.
func VisibleRowsFor(height, total int) int {
	if height <= 0 {
		return 10
	}
	n := height*3/4 - 12
	if n > total {
		n = total
	}
	if n < 1 {
		n = 1
	}
	return n
}

// NamePad is the padding between a /config row's name and its value.
// The reference aligns the value column at 29 characters counted in
// *runes*; the nested row `Editor Mode › Insert First` is 26 runes but 28
// bytes, so a byte count leaves it two cells short (fin config capture,
// row 13).
func NamePad(name string, col int) int {
	pad := col - utf8.RuneCountInString(name)
	if pad < 1 {
		pad = 1
	}
	return pad
}

// CursorFieldCols is the name-field width at which the /config cursor
// row switches from "blue name span + uncoloured pad + blue value span"
// to one blue span covering the whole row. Measured across all 21 rows
// in two independent runs (harness/cfg_cursor.py): every name of 16 or
// more runes renders as a single span, every name of 15 or fewer keeps
// the pad uncoloured. The mechanism behind the 16-cell boundary was not
// recovered — see the cycle-208 report's 存疑 section.
const CursorFieldCols = 16

// CursorRowIsSingleSpan reports which of the two measured cursor-row
// forms the reference uses for this row.
func CursorRowIsSingleSpan(name string) bool {
	return utf8.RuneCountInString(name) >= CursorFieldCols
}

// WrapText word-wraps a /config description to width-2 content
// columns; the caller adds the two-space indent. agy 1.4.3 wraps the
// description across as many terminal rows as needed (the Agent Mode
// text fills exactly 120 columns and continues on the next row at
// 120x40), while bubbletea's renderer clips a single long line. width <= 0
// means unset (unit tests) and leaves the text unwrapped.
func WrapText(s string, width int) []string {
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
