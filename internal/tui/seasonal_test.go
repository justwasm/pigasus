package tui

import (
	"strings"
	"testing"
	"time"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// pinSeason pins the seasonal clock for one test.
func pinSeason(t *testing.T, month time.Month) {
	t.Helper()
	old := seasonalNow
	seasonalNow = func() time.Time {
		return time.Date(2026, month, 15, 12, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() { seasonalNow = old })
}

// pinSeasonalPicks pins the banner's per-process picks — the scene and
// the sentence — so the assertions below do not move with the dice.
//
// The picks live in the seasonal package now; the host reaches them
// through the package's own pin.
func pinSeasonalPicks(t *testing.T, art, greeting int) {
	t.Helper()
	oldArt, oldGreeting := seasonal.SetBannerPicks(art, greeting)
	t.Cleanup(func() { seasonal.SetBannerPicks(oldArt, oldGreeting) })
}

// TestSeasonalBanner pins the themed banner the host renders: seven art
// rows, the information column at column 46, and the seasonal sentence.
//
// 【一手】 agy 1.4.7 (out/changelog-147/ref/config-seasonal-on.txt).
func TestSeasonalBanner(t *testing.T) {
	pinSeason(t, time.October)
	pinSeasonalPicks(t, 2, 0)
	// The banner's own bytes are pinned; the home directory is the
	// host's business, so the test pins it too.
	t.Setenv("HOME", "/home/pigasus")
	t.Setenv("PIGASUS_BRAND", "pigasus")
	t.Setenv("OPENAGY_BRAND", "")
	ev := seasonal.Current(seasonalNow())
	if ev == nil {
		t.Fatal("no event in October")
	}
	var b strings.Builder
	renderBannerWithTheme(&b, ev, "naivgaid@gmail.com", "Gemini 3.8 Flash (High)",
		"Google AI Pro", "/home/pigasus/Documents/GitHub/openagy")
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("the banner has %d lines, want 8 (7 art rows and the sentence)", len(lines))
	}
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = termcolor.StripSGR(l)
	}
	if !strings.HasPrefix(plain[0], "      ▀        ▄▄▀▀▀▀▀▀▄▄") {
		t.Errorf("the first art row is %q", plain[0])
	}
	// The art's cells are multi-byte, so the information column is
	// counted in runes, not bytes.
	col := func(i int) string { return string([]rune(plain[i])[46:]) }
	if got := col(0); !strings.HasPrefix(got, "Pigasus CLI ") {
		t.Errorf("the information column starts with %q", got)
	}
	if !strings.Contains(col(1), "naivgaid@gmail.com (Google AI Pro)") {
		t.Errorf("the account row is %q", col(1))
	}
	if !strings.Contains(col(3), "~/Documents/GitHub/openagy") {
		t.Errorf("the cwd row is %q", col(3))
	}
	if !strings.HasPrefix(plain[7], "  ") {
		t.Errorf("the sentence row is %q", plain[7])
	}
	if !strings.HasSuffix(plain[7], ", naivgaid.") {
		t.Errorf("the sentence row is %q", plain[7])
	}
}

// TestSeasonalPromptRule pins the themed prompt-box rules and that the
// plain rule stays in place when no event is in season.
//
// 【一手】 agy 1.4.7 (out/changelog-147/ref/seasonal-prompt.txt).
func TestSeasonalPromptRule(t *testing.T) {
	pinSeason(t, time.October)
	ev := seasonal.Current(seasonalNow())
	if ev == nil {
		t.Fatal("no event in October")
	}
	m := &runMode{width: 200, seasonal: ev}
	top := termcolor.StripSGR(m.promptRule(true))
	if !strings.Contains(top, "/^v^\\") {
		t.Errorf("the top rule has no motif: %q", top)
	}
	bottom := termcolor.StripSGR(m.promptRule(false))
	if !strings.Contains(bottom, "\\|//") {
		t.Errorf("the bottom rule has no motif: %q", bottom)
	}
	plain := &runMode{width: 200}
	if got := plain.promptRule(true); !strings.Contains(got, "─") || strings.Contains(termcolor.StripSGR(got), "/^v^\\") {
		t.Errorf("the unthemed rule is %q", got)
	}
}
