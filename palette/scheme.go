package palette

import "strconv"

// The reference's colour schemes.
//
// 【一手】 the reference's `types` package builds a map of eight named
// colour schemes when it initialises — `types.init` (0x102699fa0) fills
// the map with eight `runtime.mapassign` calls; each key is a scheme
// name and each value is a `types.SchemeColors` whose twenty colours
// are `lipgloss.Color` values (0x10267f660) — either a "#rrggbb"
// string or a decimal terminal index.  The `render` package turns one
// of them into its style set with `render.resolveStyles`
// (0x1029030c0).  The eight names are "light", "solarized light",
// "colorblind-friendly light", "dark", "solarized dark",
// "colorblind-friendly dark", "tokyo night" and "terminal" (their
// string headers sit at 0x1082ab820 + 0x10·k).
//
// 【反编译】 `types.SchemeColors` is 320 bytes — twenty colour slots,
// sixteen bytes each, in the order the struct declares them:
//
//	Bg +0x0    Fg +0x10   Accent +0x20  Border +0x30
//	UserFg +0x40   AgentFg +0x50  AddFg +0x60   AddBg +0x70
//	AddWordBg +0x80   RemoveFg +0x90  RemoveBg +0xa0
//	RemoveWordBg +0xb0   LineNumFg +0xc0  ToolFg +0xd0
//	TaskFg +0xe0   ErrorFg +0xf0  WarningFg +0x100 LinkFg +0x110
//	MutedFg +0x120  DimFg +0x130
//
// Seven of the schemes carry true colours; the "terminal" scheme
// carries terminal indices instead and leaves the background slots
// empty.

// Color is one colour of a scheme, the way the reference stores it.
//
// 【一手】 the reference's schemes hold `lipgloss.Color` values
// (0x10267f660): either a "#rrggbb" string or a decimal terminal
// index.  Seven of the eight schemes carry true colours; the
// "terminal" scheme carries indices.
type Color struct {
	value string
	ansi  bool
}

// Hex returns a true-colour value.
func Hex(value string) Color {
	return Color{value: value}
}

// ANSI returns a terminal-index value.
func ANSI(index int) Color {
	return Color{value: strconv.Itoa(index), ansi: true}
}

// Unset is the colour of a slot a scheme leaves empty.
var Unset = Color{}

// IsSet reports whether the slot carries a colour.
func (c Color) IsSet() bool { return c.value != "" }

// Value returns the colour as the reference stores it — "#268bd2" for a
// true colour, "12" for a terminal index.
func (c Color) Value() string { return c.value }

// Open returns the SGR sequence that switches the colour on, or the
// empty string when the slot carries none.
//
// The clone's own palette writes the same two forms: the 24-bit escape
// for a true colour and the 256-colour escape for an index.
func (c Color) Open() string {
	switch {
	case c.value == "":
		return ""
	case c.ansi:
		return "\x1b[38;5;" + c.value + "m"
	default:
		return hexOpen(c.value)
	}
}

// hexOpen turns "#rrggbb" into the 24-bit escape the reference emits.
func hexOpen(value string) string {
	if len(value) != 7 || value[0] != '#' {
		return ""
	}
	r := hexByte(value[1:3])
	g := hexByte(value[3:5])
	b := hexByte(value[5:7])
	return "\x1b[38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)) + "m"
}

// hexByte reads one hex pair. A malformed pair reads as zero.
func hexByte(pair string) byte {
	var out byte
	for i := 0; i < len(pair); i++ {
		out <<= 4
		c := pair[i]
		switch {
		case c >= '0' && c <= '9':
			out |= byte(c - '0')
		case c >= 'a' && c <= 'f':
			out |= byte(c-'a') + 10
		case c >= 'A' && c <= 'F':
			out |= byte(c-'A') + 10
		}
	}
	return out
}

// SchemeColors is the reference's `types.SchemeColors` — the twenty
// colour slots a scheme carries, in the order the struct declares
// them.
type SchemeColors struct {
	Bg           Color
	Fg           Color
	Accent       Color
	Border       Color
	UserFg       Color
	AgentFg      Color
	AddFg        Color
	AddBg        Color
	AddWordBg    Color
	RemoveFg     Color
	RemoveBg     Color
	RemoveWordBg Color
	LineNumFg    Color
	ToolFg       Color
	TaskFg       Color
	ErrorFg      Color
	WarningFg    Color
	LinkFg       Color
	MutedFg      Color
	DimFg        Color
}

// slot returns the colour of a named slot, and Unset for a name the
// scheme does not carry.
func (s SchemeColors) slot(name string) Color {
	switch name {
	case "Bg":
		return s.Bg
	case "Fg":
		return s.Fg
	case "Accent":
		return s.Accent
	case "Border":
		return s.Border
	case "UserFg":
		return s.UserFg
	case "AgentFg":
		return s.AgentFg
	case "AddFg":
		return s.AddFg
	case "AddBg":
		return s.AddBg
	case "AddWordBg":
		return s.AddWordBg
	case "RemoveFg":
		return s.RemoveFg
	case "RemoveBg":
		return s.RemoveBg
	case "RemoveWordBg":
		return s.RemoveWordBg
	case "LineNumFg":
		return s.LineNumFg
	case "ToolFg":
		return s.ToolFg
	case "TaskFg":
		return s.TaskFg
	case "ErrorFg":
		return s.ErrorFg
	case "WarningFg":
		return s.WarningFg
	case "LinkFg":
		return s.LinkFg
	case "MutedFg":
		return s.MutedFg
	case "DimFg":
		return s.DimFg
	}
	return Unset
}

// Light is the reference's 'light' scheme.
var Light = SchemeColors{
	Bg:           Unset,
	Fg:           Hex("#202124"),
	Accent:       Hex("#4285f4"),
	Border:       Hex("#dadce0"),
	UserFg:       Hex("#1967d2"),
	AgentFg:      Hex("#188038"),
	AddFg:        Hex("#188038"),
	AddBg:        Hex("#e6f4ea"),
	AddWordBg:    Hex("#a8dab5"),
	RemoveFg:     Hex("#c5221f"),
	RemoveBg:     Hex("#fce8e6"),
	RemoveWordBg: Hex("#f6aea9"),
	LineNumFg:    Hex("#80868b"),
	ToolFg:       Hex("#e37400"),
	TaskFg:       Hex("#9334e6"),
	ErrorFg:      Hex("#d93025"),
	WarningFg:    Hex("#e37400"),
	LinkFg:       Hex("#1a73e8"),
	MutedFg:      Hex("#3c4043"),
	DimFg:        Hex("#5f6368"),
}

// SolarizedLight is the reference's 'solarized light' scheme.
var SolarizedLight = SchemeColors{
	Bg:           Hex("#586e75"),
	Fg:           Hex("#073642"),
	Accent:       Hex("#268bd2"),
	Border:       Hex("#eee8d5"),
	UserFg:       Hex("#268bd2"),
	AgentFg:      Hex("#859900"),
	AddFg:        Hex("#859900"),
	AddBg:        Hex("#eee8d5"),
	AddWordBg:    Hex("#cde0b4"),
	RemoveFg:     Hex("#dc322f"),
	RemoveBg:     Hex("#eee8d5"),
	RemoveWordBg: Hex("#f2c6be"),
	LineNumFg:    Hex("#839496"),
	ToolFg:       Hex("#b58900"),
	TaskFg:       Hex("#d33682"),
	ErrorFg:      Hex("#dc322f"),
	WarningFg:    Hex("#cb4b16"),
	LinkFg:       Hex("#268bd2"),
	MutedFg:      Hex("#586e75"),
	DimFg:        Hex("#657b83"),
}

// ColorblindFriendlyLight is the reference's 'colorblind-friendly light' scheme.
var ColorblindFriendlyLight = SchemeColors{
	Bg:           Hex("#3c4043"),
	Fg:           Hex("#202124"),
	Accent:       Hex("#4285f4"),
	Border:       Hex("#dadce0"),
	UserFg:       Hex("#1967d2"),
	AgentFg:      Hex("#1967d2"),
	AddFg:        Hex("#1967d2"),
	AddBg:        Hex("#e8f0fe"),
	AddWordBg:    Hex("#aecbfa"),
	RemoveFg:     Hex("#c5221f"),
	RemoveBg:     Hex("#fce8e6"),
	RemoveWordBg: Hex("#f6aea9"),
	LineNumFg:    Hex("#80868b"),
	ToolFg:       Hex("#e8710a"),
	TaskFg:       Hex("#1967d2"),
	ErrorFg:      Hex("#d93025"),
	WarningFg:    Hex("#e8710a"),
	LinkFg:       Hex("#1a73e8"),
	MutedFg:      Hex("#3c4043"),
	DimFg:        Hex("#5f6368"),
}

// Dark is the reference's 'dark' scheme.
var Dark = SchemeColors{
	Bg:           Hex("#bdc1c6"),
	Fg:           Hex("#e8eaed"),
	Accent:       Hex("#8ab4f8"),
	Border:       Hex("#3c4043"),
	UserFg:       Hex("#8ab4f8"),
	AgentFg:      Hex("#81c995"),
	AddFg:        Hex("#81c995"),
	AddBg:        Hex("#0d2e16"),
	AddWordBg:    Hex("#1e5c2e"),
	RemoveFg:     Hex("#f28b82"),
	RemoveBg:     Hex("#3c0000"),
	RemoveWordBg: Hex("#731414"),
	LineNumFg:    Hex("#80868b"),
	ToolFg:       Hex("#fdd663"),
	TaskFg:       Hex("#d7aefb"),
	ErrorFg:      Hex("#f28b82"),
	WarningFg:    Hex("#f9ab00"),
	LinkFg:       Hex("#8ab4f8"),
	MutedFg:      Hex("#bdc1c6"),
	DimFg:        Hex("#9aa0a6"),
}

// SolarizedDark is the reference's 'solarized dark' scheme.
var SolarizedDark = SchemeColors{
	Bg:           Hex("#93a1a1"),
	Fg:           Hex("#eee8d5"),
	Accent:       Hex("#268bd2"),
	Border:       Hex("#073642"),
	UserFg:       Hex("#268bd2"),
	AgentFg:      Hex("#859900"),
	AddFg:        Hex("#859900"),
	AddBg:        Hex("#073642"),
	AddWordBg:    Hex("#1d4e2f"),
	RemoveFg:     Hex("#dc322f"),
	RemoveBg:     Hex("#073642"),
	RemoveWordBg: Hex("#4c1e2a"),
	LineNumFg:    Hex("#657b83"),
	ToolFg:       Hex("#b58900"),
	TaskFg:       Hex("#d33682"),
	ErrorFg:      Hex("#dc322f"),
	WarningFg:    Hex("#cb4b16"),
	LinkFg:       Hex("#268bd2"),
	MutedFg:      Hex("#93a1a1"),
	DimFg:        Hex("#839496"),
}

// ColorblindFriendlyDark is the reference's 'colorblind-friendly dark' scheme.
var ColorblindFriendlyDark = SchemeColors{
	Bg:           Hex("#bdc1c6"),
	Fg:           Hex("#e8eaed"),
	Accent:       Hex("#8ab4f8"),
	Border:       Hex("#3c4043"),
	UserFg:       Hex("#8ab4f8"),
	AgentFg:      Hex("#8ab4f8"),
	AddFg:        Hex("#8ab4f8"),
	AddBg:        Hex("#041e49"),
	AddWordBg:    Hex("#0c3d8a"),
	RemoveFg:     Hex("#f28b82"),
	RemoveBg:     Hex("#3c0000"),
	RemoveWordBg: Hex("#731414"),
	LineNumFg:    Hex("#80868b"),
	ToolFg:       Hex("#f9ab00"),
	TaskFg:       Hex("#8ab4f8"),
	ErrorFg:      Hex("#f28b82"),
	WarningFg:    Hex("#f9ab00"),
	LinkFg:       Hex("#8ab4f8"),
	MutedFg:      Hex("#bdc1c6"),
	DimFg:        Hex("#9aa0a6"),
}

// TokyoNight is the reference's 'tokyo night' scheme.
var TokyoNight = SchemeColors{
	Bg:           Hex("#a9b1d6"),
	Fg:           Hex("#c0caf5"),
	Accent:       Hex("#7aa2f7"),
	Border:       Hex("#414868"),
	UserFg:       Hex("#7aa2f7"),
	AgentFg:      Hex("#9ece6a"),
	AddFg:        Hex("#9ece6a"),
	AddBg:        Hex("#1c2d26"),
	AddWordBg:    Hex("#2b5340"),
	RemoveFg:     Hex("#f7768e"),
	RemoveBg:     Hex("#3b2229"),
	RemoveWordBg: Hex("#732e40"),
	LineNumFg:    Hex("#787c99"),
	ToolFg:       Hex("#e0af68"),
	TaskFg:       Hex("#bb9af7"),
	ErrorFg:      Hex("#f7768e"),
	WarningFg:    Hex("#ff9e64"),
	LinkFg:       Hex("#7aa2f7"),
	MutedFg:      Hex("#a9b1d6"),
	DimFg:        Hex("#9aa5ce"),
}

// Terminal is the reference's 'terminal' scheme.
var Terminal = SchemeColors{
	Bg:           Unset,
	Fg:           Unset,
	Accent:       ANSI(12),
	Border:       ANSI(8),
	UserFg:       ANSI(4),
	AgentFg:      ANSI(2),
	AddFg:        ANSI(2),
	AddBg:        Unset,
	AddWordBg:    Unset,
	RemoveFg:     ANSI(1),
	RemoveBg:     Unset,
	RemoveWordBg: Unset,
	LineNumFg:    ANSI(8),
	ToolFg:       ANSI(3),
	TaskFg:       ANSI(5),
	ErrorFg:      ANSI(1),
	WarningFg:    ANSI(3),
	LinkFg:       ANSI(4),
	MutedFg:      ANSI(7),
	DimFg:        ANSI(8),
}

// Schemes maps the reference's scheme names to their colours.
var Schemes = map[string]SchemeColors{
	"light":                     Light,
	"solarized light":           SolarizedLight,
	"colorblind-friendly light": ColorblindFriendlyLight,
	"dark":                      Dark,
	"solarized dark":            SolarizedDark,
	"colorblind-friendly dark":  ColorblindFriendlyDark,
	"tokyo night":               TokyoNight,
	"terminal":                  Terminal,
}

// SchemeByName returns the named scheme.
//
// 【一手】 the reference looks the name up in the map `types.init` builds;
// a name the map does not hold is not a scheme at all.
func SchemeByName(name string) (SchemeColors, bool) {
	scheme, ok := Schemes[name]
	return scheme, ok
}

// The four styles the reference's task row paints its marker and its
// status word with.
//
// 【反编译】 `render.StatusMarker` (0x1028c8040) and
// `render.RenderTaskItem` (0x1028c8f30) both read the same four
// styles out of the style set, in the same order:
//
//	running   — the record's byte at +0x18 has its low bit set
//	cancelled — the record's byte at +0x41 has its low bit set and the
//	            exit code is 6
//	failed    — the record's byte at +0x30 has its low bit set
//	done      — everything else
//
// The two functions read the styles at the same offsets inside
// `render.Styles` (+0x8148, +0x83d0, +0x8658 and +0x88e0), so the
// marker and the status word always carry the same colour.
const (
	StyleToolDone     = "ToolDone"
	StyleToolRunning  = "ToolRunning"
	StyleToolCanceled = "ToolCanceled"
	StyleToolError    = "ToolError"
)

// styleSlots maps a style of the reference's style set to the scheme
// slot that colours it on the reference's *scheme path* — the branch
// `render.resolveStyles` takes when the scheme name is in the map.
//
// 【反编译】 the scheme path reads the scheme out of the map, copies it
// into the frame at +0x588 and reads a slot out of it for every style
// it builds; a slot's index is (offset − 0x588) / 0x10.
var styleSlots = map[string]string{
	"AgentActive":           "MutedFg",
	"AgentCursor":           "MutedFg",
	"AgentDesc":             "ErrorFg",
	"AgentKey":              "Accent",
	"AgentName":             "DimFg",
	"AgentTitle":            "ToolFg",
	"Box":                   "Accent",
	"BtnApprove":            "Bg",
	"BtnOpen":               "MutedFg",
	"BtnReject":             "TaskFg",
	"BtwHint":               "DimFg",
	"BtwLabel":              "DimFg",
	"Chevron":               "MutedFg",
	"CloudSession":          "WarningFg",
	"Comment":               "DimFg",
	"CommentLineNum":        "Accent",
	"CompactionMarker":      "Border",
	"CursorLineNum":         "LineNumFg",
	"CursorMarker":          "Accent",
	"DiffAdd":               "DimFg",
	"DiffAddLineNum":        "AddFg",
	"DiffHunk":              "LineNumFg",
	"DiffLineNum":           "DimFg",
	"DiffRemove":            "ToolFg",
	"DiffRemoveLineNum":     "RemoveFg",
	"Dim":                   "LineNumFg",
	"EffortHigh":            "AddFg",
	"EffortLow":             "MutedFg",
	"EffortMax":             "ErrorFg",
	"EffortMaxGlint":        "ErrorFg",
	"EffortMedium":          "MutedFg",
	"EffortXHigh":           "WarningFg",
	"Error":                 "ToolFg",
	"ExitHint":              "WarningFg",
	"HelpDesc":              "Accent",
	"HelpKey":               "Accent",
	"HelpLabel":             "DimFg",
	"HelpTitle":             "DimFg",
	"HighlightHint":         "Border",
	"HighlightedText":       "ToolFg",
	"ModeAcceptEdits":       "Bg",
	"ModeAuto":              "DimFg",
	"ModeBestOfN":           "AgentFg",
	"ModePlan":              "DimFg",
	"PendingHint":           "ToolFg",
	"PendingInput":          "ErrorFg",
	"ScrollThumb":           "DimFg",
	"ScrollThumbActive":     "DimFg",
	"ScrollTrack":           "MutedFg",
	"SelCursor":             "UserFg",
	"SelDesc":               "Accent",
	"SelHint":               "Accent",
	"SelTitle":              "MutedFg",
	"Selection":             "Accent",
	"Separator":             "DimFg",
	"SkillCount":            "Accent",
	"SkillDesc":             "Accent",
	"SkillLabel":            "MutedFg",
	"SkillName":             "Accent",
	"SkillTitle":            "Accent",
	"SystemCommandArgsText": "Bg",
	"SystemCommandText":     "Bg",
	"TaskHeader":            "Fg",
	"TaskStatus":            "TaskFg",
	"ToolArgs":              "ToolFg",
	"ToolCanceled":          "LinkFg",
	"ToolDone":              "AgentFg",
	"ToolError":             "AgentFg",
	"ToolName":              "UserFg",
	"ToolNoun":              "MutedFg",
	"ToolRunning":           "ErrorFg",
	"VimInsertIndicator":    "UserFg",
	"VimVisualIndicator":    "UserFg",
	"VimVisualSelection":    "Accent",
	"Warning":               "ErrorFg",
	"Working":               "MutedFg",
}

// styleLiterals maps a style to the literal terminal index that
// colours it on the scheme path — the few styles the scheme path does
// not take a colour from the scheme.
var styleLiterals = map[string]int{
	"URL": 15,
}

// styleBackgroundSlots maps a style to the scheme slot that paints its
// background on the scheme path.
var styleBackgroundSlots = map[string]string{
	"BtnApprove":            "Accent",
	"CursorLineNum":         "RemoveBg",
	"DiffAddLineNum":        "AddBg",
	"DiffRemoveLineNum":     "RemoveBg",
	"Dim":                   "AddBg",
	"ModeAcceptEdits":       "Accent",
	"SystemCommandArgsText": "ErrorFg",
	"SystemCommandText":     "WarningFg",
}

// styleBackgroundLiterals maps a style to the literal terminal index
// that paints its background on the scheme path.
var styleBackgroundLiterals = map[string]int{
	"URL": 4,
}

// styleBold lists the styles the scheme path sets bold.
var styleBold = map[string]bool{
	"AgentPath":             true,
	"BtnReject":             true,
	"Chevron":               true,
	"CommentLineNum":        true,
	"DiffRemove":            true,
	"EffortMaxGlint":        true,
	"HelpCmd":               true,
	"HighlightedText":       true,
	"ModeAuto":              true,
	"ModePlan":              true,
	"PendingHint":           true,
	"PendingInput":          true,
	"SelCursor":             true,
	"SelItem":               true,
	"SkillName":             true,
	"SkillPath":             true,
	"SystemCommandArgsText": true,
	"TaskStatus":            true,
	"ToolArgs":              true,
	"ToolCanceled":          true,
	"ToolDone":              true,
	"ToolName":              true,
	"ToolRunning":           true,
	"VimVisualSelection":    true,
}

// fallbackSlots maps a style to the terminal index that colours it on
// the reference's *fallback path* — the branch it takes when the
// scheme name is not in the map.
//
// 【反编译】 the fallback builds the same ninety-one styles from
// literal terminal indices instead of scheme colours, and the two
// paths do not agree field for field: the fallback paints a failed
// task's marker with index 1 (red) where the scheme path paints it
// with the scheme's agent colour.
var fallbackSlots = map[string]int{
	"AgentActive":           12,
	"AgentCursor":           12,
	"AgentDesc":             7,
	"AgentKey":              12,
	"AgentPath":             7,
	"BtnApprove":            2,
	"BtnOpen":               4,
	"BtnReject":             1,
	"BtwHint":               8,
	"BtwLabel":              3,
	"Chevron":               12,
	"CloudSession":          12,
	"Comment":               4,
	"CommentLineNum":        4,
	"CompactionMarker":      7,
	"CursorLineNum":         12,
	"CursorMarker":          12,
	"DiffAdd":               2,
	"DiffAddLineNum":        8,
	"DiffHunk":              6,
	"DiffLineNum":           8,
	"DiffRemove":            1,
	"DiffRemoveLineNum":     8,
	"Dim":                   8,
	"EffortHigh":            1,
	"EffortLow":             2,
	"EffortMax":             1,
	"EffortMedium":          3,
	"EffortXHigh":           1,
	"Error":                 1,
	"ExitHint":              8,
	"HelpCmd":               12,
	"HelpDesc":              8,
	"HelpKey":               12,
	"HighlightedText":       4,
	"Hint":                  8,
	"ModeAcceptEdits":       2,
	"ModeAuto":              3,
	"ModeBestOfN":           5,
	"ModePlan":              12,
	"PendingHint":           8,
	"ScrollThumb":           8,
	"ScrollThumbActive":     7,
	"ScrollTrack":           0,
	"SelCursor":             12,
	"SelDesc":               7,
	"SelHint":               7,
	"SelSelected":           12,
	"Selection":             15,
	"Separator":             8,
	"SkillCount":            8,
	"SkillDesc":             7,
	"SkillLabel":            8,
	"SkillPath":             8,
	"SystemCommandArgsText": 13,
	"SystemCommandText":     5,
	"TaskHeader":            5,
	"TaskStatus":            7,
	"ToolArgs":              7,
	"ToolCanceled":          8,
	"ToolDone":              2,
	"ToolError":             1,
	"ToolName":              3,
	"ToolNoun":              15,
	"ToolRunning":           3,
	"UserMsg":               4,
	"VimInsertIndicator":    8,
	"VimVisualIndicator":    8,
	"VimVisualSelection":    0,
	"Warning":               3,
	"Working":               3,
}

// fallbackBackgroundSlots maps a style to the terminal index that
// paints its background on the fallback path.
var fallbackBackgroundSlots = map[string]int{
	"Selection":          4,
	"VimVisualSelection": 4,
}

// fallbackBold lists the styles the fallback path sets bold.
var fallbackBold = map[string]bool{
	"AgentActive":        true,
	"AgentTitle":         true,
	"BtnApprove":         true,
	"BtnOpen":            true,
	"BtnReject":          true,
	"BtwLabel":           true,
	"CursorLineNum":      true,
	"CursorMarker":       true,
	"EffortMax":          true,
	"EffortMaxGlint":     true,
	"EffortXHigh":        true,
	"HelpLabel":          true,
	"HelpTitle":          true,
	"HighlightHint":      true,
	"PendingInput":       true,
	"SelTitle":           true,
	"SkillTitle":         true,
	"SystemCommandText":  true,
	"TaskHeader":         true,
	"ToolName":           true,
	"UserMsg":            true,
	"VimInsertIndicator": true,
	"VimVisualIndicator": true,
}

// StyleColor returns the colour a style carries under a scheme.
//
// 【反编译】 the reference resolves the style set once and hands it to
// every renderer, so a style's colour is a property of the scheme the
// set was resolved with.
func StyleColor(scheme SchemeColors, style string) (Color, bool) {
	if slot, ok := styleSlots[style]; ok {
		return scheme.slot(slot), true
	}
	if index, ok := styleLiterals[style]; ok {
		return ANSI(index), true
	}
	return Unset, false
}

// StyleBackground returns the background colour a style carries under a
// scheme.
func StyleBackground(scheme SchemeColors, style string) (Color, bool) {
	if slot, ok := styleBackgroundSlots[style]; ok {
		return scheme.slot(slot), true
	}
	if index, ok := styleBackgroundLiterals[style]; ok {
		return ANSI(index), true
	}
	return Unset, false
}

// StyleIsBold reports whether the scheme path sets the style bold.
func StyleIsBold(style string) bool {
	return styleBold[style]
}

// FallbackColor returns the colour a style carries on the reference's
// terminal fallback palette.
func FallbackColor(style string) (Color, bool) {
	index, ok := fallbackSlots[style]
	if !ok {
		return Unset, false
	}
	return ANSI(index), true
}

// FallbackBackground returns the background colour a style carries on
// the reference's terminal fallback palette.
func FallbackBackground(style string) (Color, bool) {
	index, ok := fallbackBackgroundSlots[style]
	if !ok {
		return Unset, false
	}
	return ANSI(index), true
}

// FallbackIsBold reports whether the fallback path sets the style bold.
func FallbackIsBold(style string) bool {
	return fallbackBold[style]
}
