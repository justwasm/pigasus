package palette_test

import (
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// The palette's colours follow the selected colour scheme.
//
// 【一手】 agy 1.4.8's /config → Color Scheme panel repaints the whole
// interface in the hovered scheme's colours — the reference resolves
// every slot from the scheme table, so the clone does the same.
func TestSetSchemeRecomputesTheColours(t *testing.T) {
	restore := palette.PushScheme("dark")
	defer restore()

	dark, ok := palette.SchemeByName("dark")
	if !ok {
		t.Fatal("the dark scheme is missing")
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"BlueOpen", palette.BlueOpen, dark.Accent.Seq(false)},
		{"BlueBoldOpen", palette.BlueBoldOpen, palette.SeqWith(dark.Accent, "1")},
		{"DimGrayOpen", palette.DimGrayOpen, dark.DimFg.Seq(false)},
		{"SeparatorOpen", palette.SeparatorOpen, dark.Border.Seq(false)},
		{"SubtleOpen", palette.SubtleOpen, dark.MutedFg.Seq(false)},
		{"UserFgOpen", palette.UserFgOpen, dark.UserFg.Seq(false)},
		{"UserFgBoldOpen", palette.UserFgBoldOpen, palette.SeqWith(dark.UserFg, "1")},
		{"AgentFgOpen", palette.AgentFgOpen, dark.AgentFg.Seq(false)},
		{"TaskFgOpen", palette.TaskFgOpen, dark.TaskFg.Seq(false)},
		{"ToolAddOpen", palette.ToolAddOpen, dark.AddFg.Seq(false)},
		{"ToolRemoveOpen", palette.ToolRemoveOpen, dark.RemoveFg.Seq(false)},
		{"BodyOpen", palette.BodyOpen, dark.Fg.Seq(false)},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s under dark = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// The terminal scheme carries terminal indices instead of true colours,
// so the palette's escapes follow it too.
func TestSetSchemeToTerminalUsesIndices(t *testing.T) {
	restore := palette.PushScheme("terminal")
	defer restore()

	if got, want := palette.BlueOpen, "\x1b[94m"; got != want {
		t.Errorf("BlueOpen under terminal = %q, want %q", got, want)
	}
	if got, want := palette.BlueBoldOpen, "\x1b[94;1m"; got != want {
		t.Errorf("BlueBoldOpen under terminal = %q, want %q", got, want)
	}
}

// A scheme change notifies the packages that alias the palette's
// colours, so their copies do not keep the previous scheme's escape.
func TestOnSchemeChangeFires(t *testing.T) {
	fired := 0
	palette.OnSchemeChange(func() { fired++ })

	restore := palette.PushScheme("dark")
	if fired != 1 {
		t.Errorf("the listener fired %d times on the switch, want 1", fired)
	}
	restore()
	if fired != 2 {
		t.Errorf("the listener fired %d times after the restore, want 2", fired)
	}
}

// SeqWith writes the colour and the attribute in one escape — the form
// the reference emits — and leaves the escape alone when there is no
// attribute: the reference never writes a trailing `;`.
func TestSeqWith(t *testing.T) {
	c := palette.Hex("#268bd2")
	if got, want := palette.SeqWith(c, ""), "\x1b[38;2;38;139;210m"; got != want {
		t.Errorf("SeqWith(c, \"\") = %q, want %q", got, want)
	}
	if got, want := palette.SeqWith(c, "1"), "\x1b[38;2;38;139;210;1m"; got != want {
		t.Errorf("SeqWith(c, \"1\") = %q, want %q", got, want)
	}
	if got, want := palette.SeqWith(palette.ANSI(12), "1"), "\x1b[94;1m"; got != want {
		t.Errorf("SeqWith(ANSI(12), \"1\") = %q, want %q", got, want)
	}
}

// The code-highlighting palette of each scheme, in true colour.
//
// 【一手】 agy 1.4.8's preview card, read off the reference's own output
// bytes in a true-colour terminal: the card writes 24-bit escapes for
// the code roles under every scheme but the terminal one, whose roles
// are the basic sixteen-colour codes.
func TestSyntaxColorsFor(t *testing.T) {
	cases := []struct {
		scheme   string
		keyword  string
		str      string
		function string
		typ      string
		variable string
		attr     string
	}{
		{"terminal", "12", "3", "4", "5", "", "1"},
		{"light", "#770088", "#008800", "#0000ff", "#008855", "#0055aa", ""},
		{"solarized light", "#cb4b16", "#859900", "#2aa198", "#6c71c4", "#657b83", "22"},
		{"colorblind-friendly light", "#e8710a", "#7b1fa2", "#1967d2", "#00838f", "#3c4043", ""},
		{"dark", "#d8884b", "#97c67b", "#75b3db", "#eede7b", "#b5bcc5", ""},
		{"solarized dark", "#cb4b16", "#859900", "#2aa198", "#6c71c4", "#839496", "22"},
		{"colorblind-friendly dark", "#fb8c00", "#b388ff", "#ffe57f", "#00e5ff", "#e0e0e0", ""},
		{"tokyo night", "#bb9af7", "#9ece6a", "#7aa2f7", "#2ac3de", "#c0caf5", ""},
	}
	for _, c := range cases {
		syn := palette.SyntaxColorsFor(c.scheme)
		got := []string{
			syn.Keyword.Value(), syn.String.Value(), syn.Function.Value(),
			syn.Type.Value(), syn.Variable.Value(), syn.KeywordAttr,
		}
		want := []string{c.keyword, c.str, c.function, c.typ, c.variable, c.attr}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s role %d = %q, want %q", c.scheme, i, got[i], want[i])
			}
		}
	}
}
