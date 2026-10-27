package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/seasonal"
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

// seasonalRowIndex returns the index of the "Seasonal Themes" row in
// rows, or -1.
func seasonalRowIndex(rows []configRow) int {
	for i, r := range rows {
		if r.Name == seasonal.RowName {
			return i
		}
	}
	return -1
}

// TestSeasonalConfigRowOnlyInSeason pins the row's gate: the reference
// draws it only while the theme is in season (the 1.4.7 changelog says
// "a row shown only while the theme is in season").
func TestSeasonalConfigRowOnlyInSeason(t *testing.T) {
	pinSeason(t, time.October)
	if seasonalRowIndex(configRows()) < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	pinSeason(t, time.January)
	if seasonalRowIndex(configRows()) >= 0 {
		t.Error("the Seasonal Themes row is drawn outside the season")
	}
}

// TestSeasonalConfigRowPosition pins where the reference draws the row:
// between "Sandbox Mode" and "Show Active Tasks".
func TestSeasonalConfigRowPosition(t *testing.T) {
	pinSeason(t, time.October)
	rows := configRows()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	if idx == 0 || rows[idx-1].Name != "Sandbox Mode" {
		t.Errorf("the row follows %q, want %q", rows[idx-1].Name, "Sandbox Mode")
	}
	if idx+1 >= len(rows) || rows[idx+1].Name != "Show Active Tasks" {
		t.Errorf("the row precedes %q, want %q", rows[idx+1].Name, "Show Active Tasks")
	}
}

// TestSeasonalConfigRowDescription pins the row's help line, which is
// the reference's own sentence.
//
// 【一手】 agy 1.4.7 /config.
func TestSeasonalConfigRowDescription(t *testing.T) {
	pinSeason(t, time.October)
	rows := configRows()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	if rows[idx].Description != seasonal.RowDescription {
		t.Errorf("description = %q, want %q", rows[idx].Description, seasonal.RowDescription)
	}
	if rows[idx].SettingsKey != seasonal.OptInKey {
		t.Errorf("settings key = %q, want %q", rows[idx].SettingsKey, seasonal.OptInKey)
	}
}

// TestSeasonalConfigRowValue pins the two stops and the way the
// reference stores them: the event's own id when on, the key absent
// when off.
//
// 【一手】 agy 1.4.7 settings.json: switching the row on writes
// "seasonalThemesOptIn": "halloween-2026"; switching it off removes the
// key again.
func TestSeasonalConfigRowValue(t *testing.T) {
	pinSeason(t, time.October)
	rows := configRows()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	row := rows[idx]

	if got := configDisplayFor(row, nil); got != "off" {
		t.Errorf("an absent key reads as %q, want %q", got, "off")
	}
	if got := configDisplayFor(row, "halloween-2026"); got != "on" {
		t.Errorf("the event id reads as %q, want %q", got, "on")
	}
	if got := configDisplayFor(row, "halloween-2025"); got != "off" {
		t.Errorf("a stale event id reads as %q, want %q", got, "off")
	}
}

// ensureSettings makes sure the scratch home carries a settings.json.
//
// The clone's writer refuses to create the file — it edits the
// reference's own settings.json in place — so a test that writes a
// setting has to give it one first.
func ensureSettings(t *testing.T) {
	t.Helper()
	path := filepath.Join(auth.AntigravityHome(), "settings.json")
	old, err := os.ReadFile(path)
	if err == nil {
		t.Cleanup(func() { _ = os.WriteFile(path, old, 0o600) })
		return
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("writing settings.json: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
}

// TestSeasonalConfigRowPersists pins what the row writes: the event id
// for "on", and the *deletion* of the key for "off" — the reference's
// own "a setting at its default is absent" convention.
func TestSeasonalConfigRowPersists(t *testing.T) {
	pinSeason(t, time.October)
	ensureSettings(t)
	rows := configRows()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	row := rows[idx]
	m := &runMode{}

	m.applyConfigValue(row, row.Values[1])
	raw, err := auth.SettingsField(seasonal.OptInKey)
	if err != nil {
		t.Fatalf("reading the opt-in: %v", err)
	}
	if raw != "halloween-2026" {
		t.Errorf("the opt-in = %v, want %q", raw, "halloween-2026")
	}

	m.applyConfigValue(row, row.Values[0])
	raw, err = auth.SettingsField(seasonal.OptInKey)
	if err != nil {
		t.Fatalf("reading the opt-in: %v", err)
	}
	if raw != nil {
		t.Errorf("the opt-in is still %v, want the key to be gone", raw)
	}
}

// pinSeasonalPicks pins the banner's per-process picks — the scene and
// the sentence — so the assertions below do not move with the dice.
func pinSeasonalPicks(t *testing.T, art, greeting int) {
	t.Helper()
	oldArt, oldGreeting := seasonalArtIndex, seasonalGreetingIndex
	seasonalArtIndex, seasonalGreetingIndex = art, greeting
	t.Cleanup(func() { seasonalArtIndex, seasonalGreetingIndex = oldArt, oldGreeting })
}

// TestSeasonalBanner pins the themed banner's layout: seven art rows,
// the information column at column 46, and the seasonal sentence.
//
// 【一手】 agy 1.4.7 (out/changelog-147/ref/config-seasonal-on.txt).
func TestSeasonalBanner(t *testing.T) {
	pinSeason(t, time.October)
	pinSeasonalPicks(t, 2, 0)
	ev := seasonal.Current(seasonalNow())
	if ev == nil {
		t.Fatal("no event in October")
	}
	var b strings.Builder
	renderSeasonalBanner(&b, ev, "naivgaid@gmail.com", "Gemini 3.8 Flash (High)",
		"Google AI Pro", "/Users/gear/Documents/GitHub/openagy")
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("the banner has %d lines, want 8 (7 art rows and the sentence)", len(lines))
	}
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
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

// TestSeasonalBannerDrawsOneOfTheEventScenes pins that the themed
// banner draws one of the event's scenes, and that the scenes are not
// the same picture.
//
// 【一手】 agy 1.4.7: the themed banner is not one picture — every launch
// draws one of five scenes, each with its own colours.
func TestSeasonalBannerDrawsOneOfTheEventScenes(t *testing.T) {
	pinSeason(t, time.October)
	ev := seasonal.Current(seasonalNow())
	if ev == nil {
		t.Fatal("no event in October")
	}
	first := map[string]bool{}
	for s := range ev.Arts {
		pinSeasonalPicks(t, s, 0)
		var b strings.Builder
		renderSeasonalBanner(&b, ev, "naivgaid@gmail.com", "Gemini 3.8 Flash (High)",
			"Google AI Pro", "/Users/gear/Documents/GitHub/openagy")
		lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
		if len(lines) != 8 {
			t.Fatalf("scene %d: the banner has %d lines, want 8", s, len(lines))
		}
		row := strings.TrimRight(string([]rune(stripSGR(lines[0]))[:46]), " ")
		want := strings.TrimRight(stripSGR(ev.Art(s).Lines()[0]), " ")
		if row != want {
			t.Errorf("scene %d: the banner's first art row is %q, want %q", s, row, want)
		}
		first[row] = true
	}
	if len(first) != len(ev.Arts) {
		t.Errorf("the banner drew %d distinct pictures, want %d", len(first), len(ev.Arts))
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
	top := stripSGR(m.promptRule(true))
	if !strings.Contains(top, "/^v^\\") {
		t.Errorf("the top rule has no motif: %q", top)
	}
	bottom := stripSGR(m.promptRule(false))
	if !strings.Contains(bottom, "\\|//") {
		t.Errorf("the bottom rule has no motif: %q", bottom)
	}
	plain := &runMode{width: 200}
	if got := plain.promptRule(true); !strings.Contains(got, "─") || strings.Contains(stripSGR(got), "/^v^\\") {
		t.Errorf("the unthemed rule is %q", got)
	}
}
