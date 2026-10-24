// Package usagepanel holds the pure rendering rules of the /usage panel.
//
// The panel the reference shows is three fixed rows of chrome (a blank, the
// account line, a blank) under the bold title, then a scrollable document.
// The document is 20 logical lines at a normal width: two model groups of six
// lines each plus two blanks, followed by a four-line note whose text is
// wrapped to min(width-4, 78). The window height is height-13 (see the
// derivation in Page), the counter quotes the *unwrapped* line
// numbers, and when the whole document fits the counter is omitted
// entirely. Recovered from usage24/28/40/44/50 and usage60/100 captures.
//
// Everything here is a pure function of its arguments: no terminal state, no
// session state, no clock. The parts that are not pure — the runMode-coupled
// window/scroll plumbing — stay in cmd/wagyu.
package usagepanel

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/pluginsui"
	"github.com/justwasm/pigasus/internal/tuiviewer"
)

// NoteText is the group note. REF wraps it; the clone stores it
// unwrapped and wraps on render. Transcribed from
// ref_usage_220x50.raw.txt rows 33-36 / usage24.ref.raw.txt row 16.
const NoteText = "Within each group, models share a weekly limit. Quota is consumed proportionally to the cost of the tokens. Thus, limits will last longer with shorter tasks or using more cost-effective models. Your weekly limit is tied directly to your individual tier."

// NoteWrap caps the note's wrap width. The measured captures pin it
// to the range [76,79]: at w=60 the note wraps at width-4 == 56; at w=100
// and w=220 the same lines appear, so the width-4 value (96, 216) is capped.
// The live 1.4.3 note (which adds the 5-hour clause) pins it to 77: line 4
// ends at "capacity" and "across" starts line 5. 【一手】
const NoteWrap = 77

// PanelGray238 is the dim-gray the reference uses for the title's trailing
// glyph run and the panel's descriptive rows.
const PanelGray238 = "\x1b[38;2;88;110;117m"

// Title is the fixed bold title row.
func Title() string {
	return "\x1b[1m" + PanelGray238 + "└ Models & Quota" + "\x1b[0m"
}

// Chrome is the three fixed rows between the title and the scrolling
// document: a blank, the account line (brand blue email), a blank.
func Chrome(email string) []string {
	if email == "" {
		email = auth.EffectiveUserEmail()
	}
	if email == "" {
		email = "navigaid@gmail.com"
	}
	return []string{
		"",
		"  Account: " + palette.BlueOpen + email + palette.BlueClose,
		"",
	}
}

// WrapWidth is the note's wrap width: width-4, capped at NoteWrap.
func WrapWidth(width int) int {
	w := width - 4
	if w <= 0 || w > NoteWrap {
		w = NoteWrap
	}
	return w
}

// The greedy word-wrap primitive used below lives in internal/pluginsui
// (cycle 204's `wrapWords`). Cycles 204 and 207 developed one independently
// each; they were merged into the single helper in that file. Its contract is
// the union of both: it breaks on single spaces, fills to exactly `width`
// columns, emits an over-long word whole, and always returns at least one
// element (so `WrapWords("", n)` is `[""]`) — which the /plugin Discover
// detail block indexes unconditionally. For single-spaced input — both call
// sites here — the two implementations produced identical output.

// Bar renders one quota bar: an opening bracket, the green filled
// portion, the dim-gray remainder, a reset and the closing bracket, then
// the percentage in dim gray. REF omits the green SGR when the fill is
// empty and omits the gray SGR when the bar is full.
// (ref_usage_220x50.raw.txt rows 21 and 29.)
func Bar(pct float64, cells int) string {
	fill := int(math.Round(pct / 100.0 * float64(cells)))
	if fill < 0 {
		fill = 0
	}
	if fill > cells {
		fill = cells
	}
	var b strings.Builder
	b.WriteString("    [")
	if fill > 0 {
		b.WriteString("\x1b[32m")
		b.WriteString(strings.Repeat("█", fill))
	}
	if cells-fill > 0 {
		b.WriteString("\x1b[38;2;101;123;131m")
		b.WriteString(strings.Repeat("░", cells-fill))
	}
	b.WriteString("\x1b[39m]")
	b.WriteString("\x1b[38;2;101;123;131m")
	fmt.Fprintf(&b, " %.2f%%", pct)
	b.WriteString("\x1b[39m")
	return b.String()
}

// Group renders one model group: a bold header, the dim-gray member
// list, a blank, the (indented) "Weekly Limit Remaining" header, the bar
// and the coloured "Refreshes in ..." line.
//
// The percentages are the clone's frozen mock values (100.00% / 0.00%);
// REF's are live and move every run, so the *values* cannot be matched —
// only the structure. The refresh durations are REF's captured values
// (158h 18m / 64h 47m) frozen for the same reason. 【推测】(values only)
func Group(name, members string, pct, fillCells int, refreshColor, refresh string) []string {
	return []string{
		"\x1b[1m" + name + "\x1b[0m",
		"  " + palette.DimGrayOpen + "Models within this group: " + members + palette.DimGrayClose,
		"",
		"  \x1b[1mWeekly Limit Remaining\x1b[0m",
		Bar(float64(pct), fillCells),
		"    " + refreshColor + refresh + "\x1b[39m",
	}
}

// Doc is the /usage scrollable document. It is 20 lines at a normal
// width (the note wrapping to four lines) and grows when a narrow terminal
// wraps the note further.
func Doc(width int) []string {
	doc := Group("GEMINI MODELS", "Gemini Flash, Gemini Pro", 100, 50,
		"\x1b[32m", "Refreshes in 158h 18m")
	doc = append(doc, "", "")
	doc = append(doc, Group("CLAUDE AND GPT MODELS", "Claude Opus, Claude Sonnet, GPT-OSS", 0, 50,
		"\x1b[31m", "Refreshes in 64h 47m")...)
	doc = append(doc, "", "")
	for _, l := range wrapWords(NoteText, WrapWidth(width)) {
		doc = append(doc, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m"+l+"\x1b[39m")
	}
	return doc
}

// DocSummary renders a live retrieveUserQuotaSummary response in the
// 1.4.3 layout: each group carries both its weekly and 5-hour buckets,
// with two blank rows before the next group and before the note.
func DocSummary(s *auth.QuotaSummary, width int) []string {
	var doc []string
	for _, g := range s.Groups {
		doc = append(doc, "\x1b[1m"+strings.ToUpper(g.Name)+"\x1b[0m")
		if g.Description != "" {
			doc = append(doc, "  "+palette.DimGrayOpen+g.Description+palette.DimGrayClose)
		}
		for _, b := range g.Buckets {
			doc = append(doc, "", "  \x1b[1m"+b.DisplayName+"\x1b[0m")
			doc = append(doc, Bar(b.RemainingFraction*100, 50))
			color := "\x1b[32m"
			if b.RemainingFraction <= 0 {
				color = "\x1b[31m"
			}
			if !b.ResetTime.IsZero() {
				doc = append(doc, "    "+color+"Refreshes in "+tuiviewer.QuotaCountdown(b.ResetTime)+"\x1b[39m")
			}
		}
		doc = append(doc, "", "")
	}
	note := s.Description
	if note == "" {
		note = NoteText
	}
	for _, l := range wrapWords(note, WrapWidth(width)) {
		doc = append(doc, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m"+l+"\x1b[39m")
	}
	return doc
}

// Page is the number of document lines REF shows at once. REF lays the
// panel out as title + 3 chrome rows + window + (counter) + blank + legend
// and starts the panel ten rows above the bottom of the screen, which works
// out to height-13: 11 rows at h=24, 15 at h=28, 20 at h=33 (the last height
// that still shows a counter). Recovered from the h=24/28/40/44/50 captures.
func Page(height int) int {
	page := height - 13
	if page < 1 {
		page = 1
	}
	return page
}

// SpinnerFrame picks the nth frame from sp.Dot, the same 8-frame
// Braille set the streaming spinner uses (sp.Dot: ⣾⣽⣻⢿⡿⣟⣯⣷). It is
// pure — no cap on n — so callers increment freely. Returning the glyph
// bare (two trailing spaces follow it in the row builder) keeps the row
// builder in one place for the height-0 test path that never sets
// quotaFrame.
func SpinnerFrame(n int) string {
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	if n < 0 {
		n = 0
	}
	return frames[n%len(frames)]
}

// LoadingDoc is the document the height-aware panel shows while the quota
// call is in flight. It is one row tall (Braille glyph + two
// spaces + dim gray "Loading quota summary…") and the spinner frame
// cycles through sp.Dot on every tick — see SpinnerFrame.
//
// Byte shape (captured from REF 1.4.0 against a live quota call that did
// not resolve during the 50 s probe window):
//
//	⣾  \x1b[38;2;101;123;131mLoading quota summary...\x1b[39m
//
// REF does not render a counter during the loading phase; the offline
// guard in the panel renderer renders a 1-row window against a 1-row
// doc, so no counter row appears.
func LoadingDoc(frame int) []string {
	return []string{
		"  " + SpinnerFrame(frame) + "  " +
			palette.DimGrayOpen + "Loading quota summary..." + palette.DimGrayClose,
	}
}

// Countdown formats time.Until(t) as the "Nd Nh" / "Nh Nm" /
// "Nm Ns" string REF shows in the two refresh rows. Negative durations
// clamp to zero so a bucket that already rolled over renders "0m 0s"
// rather than going backwards.
//
// The three forms are:
//
//	>= 24h: "Nd Nh"  (e.g. 6d 23h)
//	1h..24h: "Nh Nm"
//	<  1h: "Nm Ns"   (so the second-by-second tick is visible to the user)
func Countdown(t time.Time) string {
	return tuiviewer.QuotaCountdown(t)
}

// GeminiGroupRows is the cycle-193/201 first group block used by every
// panelUsage-line test. 【推测】
func GeminiGroupRows() []string {
	return []string{
		"\x1b[1m" + "GEMINI MODELS" + "\x1b[0m",
		palette.DimGrayOpen + "  Models within this group: Gemini Flash, Gemini Pro" + palette.DimGrayClose,
		"",
		"\x1b[1mWeekly Limit Remaining\x1b[0m",
		"    " + "\x1b[32m" + strings.Repeat("█", 50) + "\x1b[39m" +
			palette.DimGrayOpen + " 100.00%" + palette.DimGrayClose,
		"\x1b[32mQuota available\x1b[39m",
	}
}

// ClaudeGroupRows is the cycle-193/201 second group block plus the three
// clipped note rows. 【推测】
func ClaudeGroupRows() []string {
	return []string{
		"\x1b[1m" + "CLAUDE AND GPT MODELS" + "\x1b[0m",
		palette.DimGrayOpen + "  Models within this group: Claude Opus, Claude Sonnet, GPT-OSS" + palette.DimGrayClose,
		"",
		"\x1b[1mWeekly Limit Remaining\x1b[0m",
		"    [\x1b[38;2;101;123;131m" + strings.Repeat("░", 50) + "\x1b[39m]" +
			palette.DimGrayOpen + " 0.00%" + palette.DimGrayClose,
		"\x1b[31mRefreshes in 74h 29m\x1b[39m",
		palette.DimGrayOpen + "  │Within each group, models share a weekly limit." + palette.DimGrayClose,
		palette.DimGrayOpen + "  │Quota refreshes weekly and is consumed as you use" + palette.DimGrayClose,
		palette.DimGrayOpen + "  │models in the group." + palette.DimGrayClose,
	}
}

// ViewerLinesStatic is the cycle-193/201 fallback used by every
// panelUsage-line test. The first 17 rows are byte-identical to the
// cycle-193 render (transcribed from ppref.usage.raw.txt); the trailing
// three rows are the wrapped tail of the group note REF clips behind its
// window — reconstructed, not captured. 【推测】
func ViewerLinesStatic() []string {
	email := auth.EffectiveUserEmail()
	if email == "" {
		email = "navigaid@gmail.com"
	}
	out := []string{
		"\x1b[1m" + PanelGray238 + "└ Models & Quota" + "\x1b[0m",
		"",
		"  Account: " + palette.BlueOpen + email + palette.BlueClose,
		"",
	}
	out = append(out, GeminiGroupRows()...)
	out = append(out, "")
	out = append(out, ClaudeGroupRows()...)
	return out
}

// wrapWords is the greedy word-wrap on single spaces. It is the single
// helper internal/pluginsui owns — cycles 204 and 207 each developed one
// independently and they were merged into that one, so this package calls it
// rather than growing a third copy.
func wrapWords(s string, width int) []string {
	return pluginsui.WrapWords(s, width)
}
