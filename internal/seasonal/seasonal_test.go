package seasonal

import (
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// strip removes the SGR sequences from a rendered string.
func strip(s string) string { return ansiRe.ReplaceAllString(s, "") }

// refTopRule and refBottomRule are the reference's own themed rules,
// captured live from agy 1.4.7 with the Halloween event on
// (out/changelog-147/ref/seasonal-prompt.txt).
const (
	refTopRule = "──────────────────/^v^\\───────────────────────────────────\\\\|//──────────────────────────────────/\\oo/\\───────────────────────────────────/^v^\\───────────────────────────────────\\\\|//─────────────────"
	refBotRule = "────────────────────────────\\\\|//──────────────────────────────────/\\oo/\\───────────────────────────────────/^v^\\───────────────────────────────────\\\\|//──────────────────────────────────/\\oo/\\───────"
)

// TestCurrentIsHalloweenInOctober pins the event window: the reference
// shows the /config row only while the event is in season, and the event
// id carries the year.
func TestCurrentIsHalloweenInOctober(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("Current() = nil in October, want the halloween event")
	}
	if ev.ID != "halloween-2026" {
		t.Errorf("event id = %q, want %q", ev.ID, "halloween-2026")
	}
	if Current(time.Date(2026, time.September, 30, 12, 0, 0, 0, time.Local)) != nil {
		t.Error("Current() in September = an event, want nil")
	}
	if Current(time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local)) != nil {
		t.Error("Current() in November = an event, want nil")
	}
}

// TestEnabledNeedsTheEventID pins the opt-in: the settings.json leaf
// must carry the event's own id.
func TestEnabledNeedsTheEventID(t *testing.T) {
	now := time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local)
	if Enabled("", now) {
		t.Error("Enabled(\"\") = true, want false (an absent key is off)")
	}
	if Enabled("on", now) {
		t.Error("Enabled(\"on\") = true, want false (the token is the event id)")
	}
	if !Enabled("halloween-2026", now) {
		t.Error("Enabled(\"halloween-2026\") = false, want true")
	}
}

// TestRuleMatchesTheReference pins the themed rule byte for byte against
// the reference's own capture.
func TestRuleMatchesTheReference(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	top := strip(ev.Border.Rule(200, true))
	if top != refTopRule {
		t.Errorf("top rule mismatch:\n got %q\nwant %q", top, refTopRule)
	}
	bottom := strip(ev.Border.Rule(200, false))
	if bottom != refBotRule {
		t.Errorf("bottom rule mismatch:\n got %q\nwant %q", bottom, refBotRule)
	}
}

// TestRuleIsWidthIndependent pins the phases: the reference's rules keep
// the same motif placement at 120 and at 200 columns.
func TestRuleIsWidthIndependent(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	short := strip(ev.Border.Rule(120, true))
	if !strings.HasPrefix(short, "──────────────────/^v^\\") {
		t.Errorf("120-column top rule starts with %q", short[:24])
	}
	if !strings.HasSuffix(short, "/\\oo/\\─────────────────") {
		t.Errorf("120-column top rule ends with %q", short[len(short)-24:])
	}
}

// TestRuleColoursFollowTheGradient pins the per-segment colours: the
// reference's rule cycles five colours, one per segment, and paints each
// motif in its own colour.
func TestRuleColoursFollowTheGradient(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	rule := ev.Border.Rule(200, true)
	want := []string{
		"\x1b[38;2;194;65;12m",
		"\x1b[38;2;78;46;150m",
		"\x1b[38;2;173;59;63m",
		"\x1b[38;2;138;90;11m",
		"\x1b[38;2;152;53;115m",
		"\x1b[38;2;38;112;58m",
		"\x1b[38;2;130;46;166m",
		"\x1b[38;2;109;40;217m",
	}
	for _, c := range want {
		if !strings.Contains(rule, c) {
			t.Errorf("rule is missing the colour %q", c)
		}
	}
}

// TestArtBottomHalfUsesTheForeground pins the half-block semantics: a
// cell whose *lower* sub-pixel alone carries a colour is painted "fg ▄
// 39", because a half block fills its own half with the foreground
// colour.
//
// 【一手】 agy 1.4.7's own capture reads
// "\x1b[38;2;195;166;255m▄▄" for such a cell
// (out/changelog-147/ref/config-seasonal-on.txt) — the colour is in the
// foreground, not the background.
func TestArtBottomHalfUsesTheForeground(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	lower, upper := 0, 0
	for _, line := range ev.Art(0).Lines() {
		for _, c := range parseCells(t, line) {
			switch c.glyph {
			case "▄":
				lower++
				if c.bg == "" && c.fg == "" {
					t.Errorf("a lower half carries no colour:\n%q", line)
				}
				if c.bg != "" && c.fg == "" {
					t.Errorf("a lower half is painted with the background:\n%q", line)
				}
			case "▀":
				upper++
				if c.fg == "" {
					t.Errorf("an upper half is painted with the background:\n%q", line)
				}
			}
		}
	}
	if lower == 0 || upper == 0 {
		t.Errorf("the art holds %d lower and %d upper halves, want both", lower, upper)
	}
}

// cell is one rendered terminal cell of the banner art.
type cell struct {
	glyph string
	fg    string
	bg    string
}

// parseCells walks a rendered art line and returns its cells with the
// colours that were in effect when the glyph was written.
func parseCells(t *testing.T, line string) []cell {
	t.Helper()
	var cells []cell
	fg, bg := "", ""
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			j := i + 1
			for j < len(line) && line[j] != 'm' {
				j++
			}
			if j >= len(line) {
				t.Fatalf("unterminated escape in %q", line)
			}
			switch p := line[i+2 : j]; {
			case p == "39":
				fg = ""
			case p == "49":
				bg = ""
			case p == "" || p == "0":
				fg, bg = "", ""
			case strings.HasPrefix(p, "38;2;"):
				fg = p[len("38;2;"):]
			case strings.HasPrefix(p, "48;2;"):
				bg = p[len("48;2;"):]
			}
			i = j + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		switch r {
		case '▀', '▄':
			cells = append(cells, cell{glyph: string(r), fg: fg, bg: bg})
			fg, bg = "", ""
		case ' ':
			cells = append(cells, cell{glyph: " "})
		default:
			t.Fatalf("unexpected glyph %q in %q", r, line)
		}
		i += size
	}
	return cells
}

// TestSpinnerFrames pins the moon-phase spinner: the four frames in the
// binary's own order, each carrying a trailing space.
//
// 【一手】 agy 1.4.7: the streaming row reads "◐  Generating…" — glyph,
// space, space, text — and the string table holds the four frames as
// four four-byte literals.
func TestSpinnerFrames(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	frames := ev.SpinnerFrames()
	want := []string{"◐ ", "◓ ", "◑ ", "◒ "}
	if len(frames) != len(want) {
		t.Fatalf("spinner has %d frames, want %d", len(frames), len(want))
	}
	for i, f := range frames {
		if f != want[i] {
			t.Errorf("frame %d = %q, want %q", i, f, want[i])
		}
	}
}

// TestGreetingVariants pins the banner's four sentences and that the
// greeting picks one of them.
//
// 【一手】 agy 1.4.7: forty launches on 2026-10-27 showed all four
// sentences, each of them next to any of the five banner scenes.
func TestGreetingVariants(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	want := []string{
		"Spooky season, steady builds",
		"No bugs tonight, only boos",
		"Happy haunting",
		"Something is brewing in the build",
	}
	if len(ev.Taglines) != len(want) {
		t.Fatalf("the event has %d sentences, want %d", len(ev.Taglines), len(want))
	}
	for i, w := range want {
		if got := ev.Greeting(i); got != w {
			t.Errorf("sentence %d is %q, want %q", i, got, w)
		}
	}
	if ev.Greeting(len(want)) != want[0] {
		t.Error("the greeting does not wrap around the sentences")
	}
	var nilEvent *Event
	if got := nilEvent.Greeting(0); got != "" {
		t.Errorf("the greeting of no event is %q", got)
	}
}

// TestBannerScenesMatchTheReference pins the event's five banner scenes
// against the reference's own truecolor output, cell for cell.
//
// 【一手】 agy 1.4.7, driven live on 2026-10-27: twenty-two launches in
// a pty (COLORTERM=truecolor) produced exactly five distinct settled
// banners — a pumpkin, a moon, a ghost, a witch's hat and a bat, each
// standing on the same spider, web and ground — and every scene was
// drawn the same way every time. The lines below are the reference's
// own cells, in the settled (signed-in) banner's coordinates.
func TestBannerScenesMatchTheReference(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	if len(ev.Arts) != 5 {
		t.Fatalf("the event holds %d scenes, want 5", len(ev.Arts))
	}
	want := [][]string{
		{
			"      ▀       ▄▄▄▄ ▀▀ ▄▄▄▄     ▀▄▄▄▀    ▄",
			"      ▀    ▄▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▄  ▄    ▄ ▀▄▀",
			"    ▀▄▀▄▀ ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀   ▄▄▀▀▀▄▄",
			"    ▀▀▀▀▀ ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀    ▀▀▀▀▀",
			"   ▀ ▀ ▀  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀ ▄▄▄▀▀▀▀▀▄▄▄",
			"  ▀▀▀       ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀  ▀  ▀▀▀▀▀  ▀",
			"  ▀▀▀▄▀▄▄▄▀▄▄▀▀▄▄▀▄▄▀▄▄▀▄▀▀▄▀▄▄▄▀▄▄▄▀▄▄▀▄▀",
		},
		{
			"      ▀     ▄     ▄ ▄▀▀▀▀▀▀▄   ▀▄▄▄▀    ▄",
			"      ▀    ▄▀▀▄▄▄▀▀▀▀▀▀▀▀▀▀▀▀  ▄      ▀▄▀",
			"    ▀▄▀▄▀  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀    ▄▄▄▄▄",
			"    ▀▀▀▀▀  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀   ▀▀▀▀▀▀▀",
			"   ▀ ▀ ▀   ▄▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀     ▀▀▀▀▀▀▀",
			"  ▀▀▀      ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀      ▀▀▀▀▀▀▀",
			"  ▀▀▀▄▀▄▄▄▀▄▄▀▀▄▄▀▄▄▀▄▄▀▄▀▀▄▀▄▄▀▄▀▀▀▀▀▀▀▄▀",
		},
		{
			"      ▀        ▄▄▀▀▀▀▀▀▄▄      ▀▄▄▄▀    ▄",
			"      ▀      ▄▀▀▀▀▀▀▀▀▀▀▀▀▄    ▄      ▀▄▀",
			"    ▀▄▀▄▀    ▀▀▀▀▀▀▀▀▀▀▀▀▀▀       ▄▄▄▄▄",
			"    ▀▀▀▀▀  ▄▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▄    ▀▀▀▀▀▀▀",
			"   ▀ ▀ ▀   ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀    ▀▀▀▀▀▀▀",
			"  ▀▀▀        ▀▀▀▀▀▀▀▀▀▀▀▀▀▀      ▀▀▀▀▀▀▀",
			"  ▀▀▀▄▀▄▄▄▀▄▄▀▀▄▄▀▄▄▀▄▄▀▄▀▀▄▀▄▄▀▄▀▀▀▀▀▀▀▄▀",
		},
		{
			"      ▀               ▄▄▀▀     ▀▄▄▄▀    ▄",
			"      ▀             ▄▀▀▀▀      ▄    ▄ ▀▄▀",
			"    ▀▄▀▄▀         ▄▀▀▀▀▀▀▄       ▄▄▀▀▀▄▄",
			"    ▀▀▀▀▀       ▄▀▀▀▀▀▀▀▀▀▄       ▀▀▀▀▀",
			"   ▀ ▀ ▀    ▄▄▄▄▀▀▀▀▀▀▀▀▀▀▀▄▄  ▄▄▄▀▀▀▀▀▄▄▄",
			"  ▀▀▀      ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀ ▀  ▀▀▀▀▀  ▀",
			"  ▀▀▀▄▀▄▄▄▀▄▄▀▀▄▄▀▄▄▀▄▄▀▄▀▀▄▀▄▄▄▀▄▄▄▀▄▄▀▄▀",
		},
		{
			"      ▀           ▄  ▄         ▀▄▄▄▀    ▄",
			"      ▀    ▄▀▀▄  ▄▀▀▀▀▄  ▄▀▀▄  ▄    ▄ ▀▄▀",
			"    ▀▄▀▄▀ ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀   ▄▄▀▀▀▄▄",
			"    ▀▀▀▀▀ ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀    ▀▀▀▀▀",
			"   ▀ ▀ ▀  ▀▀▀ ▀▀▀▀▀▀▀▀▀▀▀▀ ▀▀▀ ▄▄▄▀▀▀▀▀▄▄▄",
			"  ▀▀▀              ▀▀          ▀  ▀▀▀▀▀  ▀",
			"  ▀▀▀▄▀▄▄▄▀▄▄▀▀▄▄▀▄▄▀▄▄▀▄▀▀▄▀▄▄▄▀▄▄▄▀▄▄▀▄▀",
		},
	}
	for s, w := range want {
		lines := ev.Art(s).Lines()
		if len(lines) != 7 {
			t.Fatalf("scene %d has %d lines, want 7", s, len(lines))
		}
		for i, wl := range w {
			got := strings.TrimRight(strip(lines[i]), " ")
			if got != wl {
				t.Errorf("scene %d line %d:\n got %q\nwant %q", s, i, got, wl)
			}
		}
	}
}

// TestBannerScenesAreDistinct pins that the event's scenes are five
// different pictures with five different colour sets, and that the pick
// cycles through them.
//
// 【一手】 agy 1.4.7: the themed banner is not one picture — every launch
// draws one of five, and each scene brings its own colours.
func TestBannerScenesAreDistinct(t *testing.T) {
	ev := Current(time.Date(2026, time.October, 27, 12, 0, 0, 0, time.Local))
	if ev == nil {
		t.Fatal("no event in October")
	}
	shapes := map[string]bool{}
	palettes := map[string]bool{}
	for s := range ev.Arts {
		art := ev.Art(s)
		if art.Width != 46 {
			t.Errorf("scene %d is %d cells wide, want 46", s, art.Width)
		}
		if len(art.Rows) != 14 {
			t.Errorf("scene %d has %d sub-rows, want 14", s, len(art.Rows))
		}
		var shape strings.Builder
		for _, line := range art.Lines() {
			shape.WriteString(strip(line))
			shape.WriteString("\n")
		}
		shapes[shape.String()] = true
		var palette strings.Builder
		for _, c := range art.Palette {
			palette.WriteString(string(rune(c[0])))
			palette.WriteString(string(rune(c[1])))
			palette.WriteString(string(rune(c[2])))
			palette.WriteByte(';')
		}
		palettes[palette.String()] = true
	}
	if len(shapes) != len(ev.Arts) {
		t.Errorf("the event holds %d distinct pictures, want %d", len(shapes), len(ev.Arts))
	}
	if len(palettes) != len(ev.Arts) {
		t.Errorf("the event holds %d distinct colour sets, want %d", len(palettes), len(ev.Arts))
	}
	if ev.Art(len(ev.Arts)).Width != ev.Art(0).Width {
		t.Error("the pick does not wrap around the scenes")
	}
	var nilEvent *Event
	if got := nilEvent.Art(0); got.Width != 0 {
		t.Errorf("the art of no event is %d cells wide, want 0", got.Width)
	}
}
