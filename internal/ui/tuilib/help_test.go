package tuilib

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

func TestHelpListWindowFor(t *testing.T) {
	cases := []struct{ h, want int }{
		{13, 4}, {14, 5}, {15, 6}, {16, 7}, {17, 8}, {18, 9},
		{20, 11}, {22, 12}, {24, 14}, {26, 15}, {28, 17}, {30, 18},
		{32, 20}, {33, 20}, {34, 21}, {36, 23}, {38, 24}, {40, 26},
		{50, 33}, {60, 41},
	}
	for _, c := range cases {
		if got := HelpListWindowFor(c.h); got != c.want {
			t.Errorf("HelpListWindowFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

func TestHelpListWindowForUnknownHeight(t *testing.T) {
	for _, h := range []int{0, -1} {
		if got := HelpListWindowFor(h); got != 18 {
			t.Errorf("HelpListWindowFor(%d) = %d, want 18", h, got)
		}
	}
	if got := HelpListWindowFor(1); got < 1 {
		t.Errorf("HelpListWindowFor(1) = %d, want >= 1", got)
	}
}

func TestHelpListVisible(t *testing.T) {
	cases := []struct{ total, window, size, want int }{
		{99, 0, 18, 18},
		{99, 90, 18, 9},
		{99, 99, 18, 0},
		{27, 0, 18, 18},
		{27, 18, 18, 9},
		{0, 0, 18, 0},
		{5, 0, 18, 5},
		{99, 0, 0, 1},
	}
	for _, c := range cases {
		if got := HelpListVisible(c.total, c.window, c.size); got != c.want {
			t.Errorf("HelpListVisible(%d, %d, %d) = %d, want %d", c.total, c.window, c.size, got, c.want)
		}
	}
}

func TestBuildShortcutRow(t *testing.T) {
	row := HelpShortcuts[0]
	res := BuildShortcutRow(row, true)
	if !strings.HasPrefix(res, HelpBlueOpen+"> /") {
		t.Errorf("highlighted shortcut row prefix = %q", res)
	}
	resNorm := BuildShortcutRow(row, false)
	if !strings.HasPrefix(resNorm, "  "+HelpBlueOpen+"/") {
		t.Errorf("normal shortcut row prefix = %q", resNorm)
	}
}

func TestBuildCommandRow(t *testing.T) {
	cmd := slashcommands.Command{
		Name:        "/help",
		Aliases:     []string{"?"},
		Description: "Show help commands",
	}
	row := BuildCommandRow(cmd, true, 80)
	if !strings.Contains(row, "/help (?)") {
		t.Errorf("command row missing name and alias: %q", row)
	}
}

// Test_BuildCommandRowTruncation pins the cycle-182 width-relative
// description truncation. REF keeps `width - 52` description cells at
// the measured description column (51, 0-based) and replaces the
// overflow with `<cap-3>...`.
//
// Cycle 381: this test lived in internal/cliapp, where it exercised
// tuilib's subject through the host's aliases. It lives with its
// subject now.
// Test_BuildCommandRowTruncation pins the cycle-182 width-relative
// description truncation. REF keeps `width - 52` description cells at
// the measured description column (51, 0-based) and replaces the
// overflow with `<cap-3>...`.
func Test_BuildCommandRowTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	c := slashcommands.Command{Name: "/demo", Description: long}

	// Untruncated at 220 columns: cap = 168 -> 165 runes + "...".
	row := BuildCommandRow(c, false, 220)
	desc := row[strings.Index(row, HelpDescOpen)+len(HelpDescOpen):]
	desc = strings.TrimSuffix(desc, HelpDescClose)
	if got := len([]rune(desc)); got != 168 {
		t.Errorf("width=220 desc len = %d, want 168", got)
	}
	if !strings.HasSuffix(desc, "...") {
		t.Errorf("width=220 desc should end in ellipsis, got %q", desc[len(desc)-6:])
	}

	// Narrow terminal: cap = width - 52. At w=60 the cap is 8.
	row = BuildCommandRow(c, false, 60)
	desc = row[strings.Index(row, HelpDescOpen)+len(HelpDescOpen):]
	desc = strings.TrimSuffix(desc, HelpDescClose)
	if got := len([]rune(desc)); got != 8 {
		t.Errorf("width=60 desc len = %d, want 8", got)
	}

	// width <= 0 disables truncation entirely (the pre-182 behaviour
	// the byte-exact snapshots rely on).
	row = BuildCommandRow(c, false, 0)
	desc = row[strings.Index(row, HelpDescOpen)+len(HelpDescOpen):]
	desc = strings.TrimSuffix(desc, HelpDescClose)
	if got := len([]rune(desc)); got != 200 {
		t.Errorf("width=0 desc len = %d, want 200 (no truncation)", got)
	}

	// A short description is never touched, at any width.
	short := slashcommands.Command{Name: "/demo", Description: "short"}
	row = BuildCommandRow(short, false, 60)
	if !strings.Contains(row, HelpDescOpen+"short"+HelpDescClose) {
		t.Errorf("short description was mangled: %q", row)
	}
}

// Test_BuildCommandRowCursorStyling verifies the COMMANDS tab colours:
// the cursor row wraps the whole name column in brand blue, a normal
// row leaves it plain, and every row's description uses the darker
// description gray.
func Test_BuildCommandRowCursorStyling(t *testing.T) {
	c := slashcommands.Command{Name: "/demo", Description: "does a thing"}

	cursor := BuildCommandRow(c, true, 220)
	if !strings.HasPrefix(cursor, HelpBlueOpen+"> /demo") {
		t.Errorf("cursor row should open with blue \"> /demo\", got %q", cursor[:40])
	}
	if !strings.Contains(cursor, HelpBlueClose) {
		t.Error("cursor row should close the blue wrap")
	}
	if !strings.Contains(cursor, HelpDescOpen+"does a thing"+HelpDescClose) {
		t.Errorf("description missing description-gray wrap: %q", cursor)
	}

	normal := BuildCommandRow(c, false, 220)
	if strings.HasPrefix(normal, HelpBlueOpen) {
		t.Error("normal row should not open with blue")
	}
	if !strings.HasPrefix(normal, "  /demo") {
		t.Errorf("normal row should start with two-space marker, got %q", normal[:20])
	}
}

// Test_RenderHelpGeneralTierSuffix verifies cycle 182: the GENERAL
// panel's email row carries the plan tier in parentheses, exactly like
// the banner. The pre-182 comment claimed REF omitted it; the v1.4.0
// raw capture shows 'navigaid@gmail.com (Antigravity Starter Quota)'.
func Test_RenderHelpGeneralTierSuffix(t *testing.T) {
	var b strings.Builder
	RenderHelpGeneral(&b, "Gemini 3.8 Flash (High)", "Antigravity Starter Quota",
		"~/GitHub/openagy", "navigaid@gmail.com", "uuid")
	out := b.String()
	if !strings.Contains(out, HelpDimGrayOpen+"navigaid@gmail.com (Antigravity Starter Quota)"+HelpDimGrayClose) {
		t.Errorf("email row missing tier suffix: %q", out)
	}

	// An empty tier must not leave a dangling " ()".
	b.Reset()
	RenderHelpGeneral(&b, "Gemini 3.8 Flash", "", "~/x", "u@t", "uuid")
	out = b.String()
	if strings.Contains(out, "u@t (") {
		t.Errorf("empty tier should not render parentheses: %q", out)
	}
	if !strings.Contains(out, HelpDimGrayOpen+"u@t"+HelpDimGrayClose) {
		t.Errorf("bare email row missing: %q", out)
	}
}
