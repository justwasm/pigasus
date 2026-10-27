package palette_test

import (
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// The reference's scheme names, in the order `types.init` puts them in
// the map it builds.
//
// 【一手】 the eight string headers sit at 0x1082ab820 + 0x10·k.
func TestSchemeNames(t *testing.T) {
	want := []string{
		"light",
		"solarized light",
		"colorblind-friendly light",
		"dark",
		"solarized dark",
		"colorblind-friendly dark",
		"tokyo night",
		"terminal",
	}
	if len(palette.Schemes) != len(want) {
		t.Fatalf("Schemes holds %d schemes, want %d", len(palette.Schemes), len(want))
	}
	for _, name := range want {
		if _, ok := palette.SchemeByName(name); !ok {
			t.Errorf("SchemeByName(%q) found no scheme", name)
		}
	}
	if _, ok := palette.SchemeByName("solarized"); ok {
		t.Error(`SchemeByName("solarized") found a scheme; the reference's names carry the variant`)
	}
}

// The "terminal" scheme is the only one that carries terminal indices
// instead of true colours, and it leaves the background slots empty.
//
// 【一手】 the reference's `terminal` scheme holds ANSI(12), ANSI(8),
// ANSI(4), ANSI(2), ANSI(1), ANSI(3), ANSI(5) and ANSI(7) — the
// sixteen-colour palette.
func TestTerminalScheme(t *testing.T) {
	scheme, ok := palette.SchemeByName("terminal")
	if !ok {
		t.Fatal("no terminal scheme")
	}
	slots := map[string]*palette.Color{
		"Accent":    &scheme.Accent,
		"Border":    &scheme.Border,
		"UserFg":    &scheme.UserFg,
		"AgentFg":   &scheme.AgentFg,
		"AddFg":     &scheme.AddFg,
		"RemoveFg":  &scheme.RemoveFg,
		"LineNumFg": &scheme.LineNumFg,
		"ToolFg":    &scheme.ToolFg,
		"TaskFg":    &scheme.TaskFg,
		"ErrorFg":   &scheme.ErrorFg,
		"WarningFg": &scheme.WarningFg,
		"LinkFg":    &scheme.LinkFg,
		"MutedFg":   &scheme.MutedFg,
		"DimFg":     &scheme.DimFg,
	}
	want := map[string]string{
		"Accent":    "12",
		"Border":    "8",
		"UserFg":    "4",
		"AgentFg":   "2",
		"AddFg":     "2",
		"RemoveFg":  "1",
		"LineNumFg": "8",
		"ToolFg":    "3",
		"TaskFg":    "5",
		"ErrorFg":   "1",
		"WarningFg": "3",
		"LinkFg":    "4",
		"MutedFg":   "7",
		"DimFg":     "8",
	}
	for name, slot := range slots {
		if got := slot.Value(); got != want[name] {
			t.Errorf("terminal %s = %q, want %q", name, got, want[name])
		}
	}
	empty := map[string]*palette.Color{
		"Bg":           &scheme.Bg,
		"Fg":           &scheme.Fg,
		"AddBg":        &scheme.AddBg,
		"AddWordBg":    &scheme.AddWordBg,
		"RemoveBg":     &scheme.RemoveBg,
		"RemoveWordBg": &scheme.RemoveWordBg,
	}
	for name, slot := range empty {
		if slot.IsSet() {
			t.Errorf("terminal %s = %q, want the slot left empty", name, slot.Value())
		}
	}
}

// The four styles the task row paints its marker and its status word
// with, and the slot each one takes its colour from.
//
// 【反编译】 `render.StatusMarker` (0x1028c8040) and
// `render.RenderTaskItem` (0x1028c8f30) read the same four styles out
// of `render.Styles` at +0x8148, +0x83d0, +0x8658 and +0x88e0.
func TestTaskRowStyles(t *testing.T) {
	scheme := palette.SolarizedLight
	cases := []struct {
		style string
		want  palette.Color
	}{
		{palette.StyleToolDone, scheme.AgentFg},
		{palette.StyleToolRunning, scheme.ErrorFg},
		{palette.StyleToolCanceled, scheme.LinkFg},
		{palette.StyleToolError, scheme.AgentFg},
	}
	for _, c := range cases {
		got, ok := palette.StyleColor(scheme, c.style)
		if !ok {
			t.Errorf("StyleColor(%q) found no colour", c.style)
			continue
		}
		if got != c.want {
			t.Errorf("StyleColor(%q) = %q, want %q", c.style, got.Value(), c.want.Value())
		}
	}
	// The scheme path sets the first three bold and leaves the error
	// style plain.
	for _, style := range []string{palette.StyleToolDone, palette.StyleToolRunning, palette.StyleToolCanceled} {
		if !palette.StyleIsBold(style) {
			t.Errorf("StyleIsBold(%q) = false, want true", style)
		}
	}
	if palette.StyleIsBold(palette.StyleToolError) {
		t.Error("StyleIsBold(ToolError) = true, want false")
	}
}

// The reference's fallback path — the branch it takes when the scheme
// name is not in the map — builds the same styles from literal
// terminal indices, and does not agree with the scheme path.
//
// 【反编译】 the fallback paints a failed task with index 1 (red)
// where the scheme path paints it with the scheme's agent colour.
func TestFallbackStyles(t *testing.T) {
	cases := []struct {
		style string
		want  string
	}{
		{palette.StyleToolDone, "2"},
		{palette.StyleToolRunning, "3"},
		{palette.StyleToolCanceled, "8"},
		{palette.StyleToolError, "1"},
	}
	for _, c := range cases {
		got, ok := palette.FallbackColor(c.style)
		if !ok {
			t.Errorf("FallbackColor(%q) found no colour", c.style)
			continue
		}
		if got.Value() != c.want {
			t.Errorf("FallbackColor(%q) = %q, want %q", c.style, got.Value(), c.want)
		}
	}
	// The two paths disagree about the failed style: red on the
	// fallback, the agent colour on the scheme path.
	fallback, _ := palette.FallbackColor(palette.StyleToolError)
	scheme, _ := palette.StyleColor(palette.SolarizedLight, palette.StyleToolError)
	if fallback == scheme {
		t.Error("the fallback and the scheme path agree on the failed style; they do not")
	}
}

// The clone's captured palette is the reference's "solarized light"
// scheme.
//
// 【一手】 the truecolour pty capture of agy 1.4.3 that
// `palette/palette.go` carries emits these escapes; nine of
// them are scheme slots, and five of those nine differ between the
// solarized light and the solarized dark scheme — so the capture
// picks one of the two.
func TestCaptureIsSolarizedLight(t *testing.T) {
	scheme := palette.SolarizedLight
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Fg", scheme.Fg.Open(), palette.BodyOpen},
		{"UserFg", scheme.UserFg.Open(), palette.BlueOpen},
		{"DimFg", scheme.DimFg.Open(), palette.DimGrayOpen},
		{"WarningFg", scheme.WarningFg.Open(), palette.WarningOpen},
		{"Border", scheme.Border.Open(), palette.SeparatorOpen},
		{"MutedFg", scheme.MutedFg.Open(), palette.SubtleOpen},
		{"AddFg", scheme.AddFg.Open(), palette.ToolAddOpen},
		{"RemoveFg", scheme.RemoveFg.Open(), palette.ToolRemoveOpen},
		{"ToolFg", scheme.ToolFg.Open(), palette.ToolBulletRunningOpen},
		{"LineNumFg", scheme.LineNumFg.Open(), palette.DiffNumOpen},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("solarized light %s = %q, want the captured %q", c.name, c.got, c.want)
		}
	}
	// The dark scheme's foreground and border are the other way round,
	// so it is not the scheme the capture was taken under.
	dark := palette.SolarizedDark
	if dark.Fg.Open() == palette.BodyOpen {
		t.Error("solarized dark carries the captured foreground; the capture is not solarized dark")
	}
	if dark.Border.Open() == palette.SeparatorOpen {
		t.Error("solarized dark carries the captured border; the capture is not solarized dark")
	}
}

// Every scheme paints an added line and an agent the same colour, so
// a capture cannot tell the two slots apart.
func TestAgentColourMatchesAddColour(t *testing.T) {
	for name := range palette.Schemes {
		scheme, _ := palette.SchemeByName(name)
		if scheme.AgentFg != scheme.AddFg {
			t.Errorf("scheme %q: agent colour %q differs from the add colour %q",
				name, scheme.AgentFg.Value(), scheme.AddFg.Value())
		}
	}
}

// A colour opens as the 24-bit escape the reference emits for a true
// colour and as the 256-colour escape for a terminal index.
func TestColorOpen(t *testing.T) {
	cases := []struct {
		color palette.Color
		want  string
	}{
		{palette.Hex("#268bd2"), "\x1b[38;2;38;139;210m"},
		{palette.Hex("#859900"), "\x1b[38;2;133;153;0m"},
		{palette.ANSI(2), "\x1b[38;5;2m"},
		{palette.Unset, ""},
	}
	for _, c := range cases {
		if got := c.color.Open(); got != c.want {
			t.Errorf("Open() = %q, want %q", got, c.want)
		}
	}
	if palette.Unset.IsSet() {
		t.Error("Unset reports a colour")
	}
	if !palette.Hex("#268bd2").IsSet() {
		t.Error("a hex colour reports no colour")
	}
}
