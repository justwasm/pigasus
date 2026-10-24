package usagepanel

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
)

// TestDocGeometry pins the /usage document shape: two groups of six lines
// separated by two blanks, then the four-line wrapped note. At a normal
// width the document is 20 lines; a narrow terminal wraps the note further
// and the document grows (the counter's total follows the document length,
// not a constant).
func TestDocGeometry(t *testing.T) {
	doc := Doc(220)
	if len(doc) != 20 {
		t.Fatalf("Doc(220) = %d lines, want 20", len(doc))
	}
	if doc[0] != "\x1b[1mGEMINI MODELS\x1b[0m" {
		t.Errorf("doc[0] = %q", doc[0])
	}
	if doc[3] != "  \x1b[1mWeekly Limit Remaining\x1b[0m" {
		t.Errorf("doc[3] = %q, want a 2-space-indented header", doc[3])
	}
	// Group separation: two blanks between the groups and before the note.
	if doc[6] != "" || doc[7] != "" {
		t.Errorf("doc[6:8] = %q, want two blanks", doc[6:8])
	}
	if doc[14] != "" || doc[15] != "" {
		t.Errorf("doc[14:16] = %q, want two blanks before the note", doc[14:16])
	}
	for i, want := range []string{
		"Within each group, models share a weekly limit. Quota is consumed",
		"proportionally to the cost of the tokens. Thus, limits will last longer with",
		"shorter tasks or using more cost-effective models. Your weekly limit is tied",
		"directly to your individual tier.",
	} {
		line := doc[16+i]
		if !strings.HasPrefix(line, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m") {
			t.Fatalf("note line %d prefix = %q", i, line)
		}
		if !strings.Contains(line, want) {
			t.Errorf("note line %d = %q, want %q", i, line, want)
		}
		if !strings.HasSuffix(line, "\x1b[39m") {
			t.Errorf("note line %d should end with a reset: %q", i, line)
		}
	}
	if n := len(Doc(60)); n != 21 {
		t.Errorf("Doc(60) = %d lines, want 21", n)
	}
}

// TestBar pins the "[green fill|gray remainder] NN.NN%" bar. 66.65% of 50
// cells rounds to 33 — exactly ref_usage_220x50.raw.txt row 21. A 0% bar
// emits no green SGR at all (row 29) and a 100% bar no gray remainder.
func TestBar(t *testing.T) {
	got := Bar(66.65, 50)
	want := "    [\x1b[32m" + strings.Repeat("█", 33) + "\x1b[38;2;101;123;131m" + strings.Repeat("░", 17) +
		"\x1b[39m]\x1b[38;2;101;123;131m 66.65%\x1b[39m"
	if got != want {
		t.Errorf("Bar(66.65,50):\n got %q\nwant %q", got, want)
	}
	got0 := Bar(0, 50)
	want0 := "    [\x1b[38;2;101;123;131m" + strings.Repeat("░", 50) + "\x1b[39m]\x1b[38;2;101;123;131m 0.00%\x1b[39m"
	if got0 != want0 {
		t.Errorf("Bar(0,50):\n got %q\nwant %q", got0, want0)
	}
	got100 := Bar(100, 50)
	want100 := "    [\x1b[32m" + strings.Repeat("█", 50) + "\x1b[39m]\x1b[38;2;101;123;131m 100.00%\x1b[39m"
	if got100 != want100 {
		t.Errorf("Bar(100,50):\n got %q\nwant %q", got100, want100)
	}
}

// TestPage pins the height-13 window: 11 rows at h=24, 15 at h=28, 20 at
// h=33, 27 at h=40 and 37 at h=50.
func TestPage(t *testing.T) {
	for _, tc := range []struct{ h, want int }{{24, 11}, {28, 15}, {33, 20}, {40, 27}, {50, 37}} {
		if got := Page(tc.h); got != tc.want {
			t.Errorf("Page(%d) = %d, want %d", tc.h, got, tc.want)
		}
	}
	if got := Page(0); got != 1 {
		t.Errorf("Page(0) = %d, want 1 (clamped)", got)
	}
}

// TestWrapWidth pins the note's wrap budget: width-4, capped at NoteWrap.
func TestWrapWidth(t *testing.T) {
	for _, tc := range []struct{ width, want int }{
		{220, NoteWrap},
		{100, NoteWrap},
		{60, 56},
		{80, 76},
		{4, NoteWrap},
	} {
		if got := WrapWidth(tc.width); got != tc.want {
			t.Errorf("WrapWidth(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

// TestWrapWords pins the greedy word-wrap: it fills to exactly the
// requested width, breaks on single spaces and always returns at least one
// element.
func TestWrapWords(t *testing.T) {
	got := wrapWords(NoteText, 76)
	want := []string{
		"Within each group, models share a weekly limit. Quota is consumed",
		"proportionally to the cost of the tokens. Thus, limits will last longer with",
		"shorter tasks or using more cost-effective models. Your weekly limit is tied",
		"directly to your individual tier.",
	}
	if len(got) != len(want) {
		t.Fatalf("wrapWords(.,76) = %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if got := wrapWords("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("wrapWords(\"\", 10) = %q, want [\"\"]", got)
	}
}

// TestTitleAndChrome pins the fixed chrome rows above the document.
func TestTitleAndChrome(t *testing.T) {
	if got := Title(); got != "\x1b[1m\x1b[38;2;88;110;117m└ Models & Quota\x1b[0m" {
		t.Errorf("Title() = %q", got)
	}
	chrome := Chrome("u@example.com")
	if len(chrome) != 3 {
		t.Fatalf("Chrome() = %d rows, want 3", len(chrome))
	}
	if chrome[0] != "" || chrome[2] != "" {
		t.Errorf("Chrome() = %q, want blank first and last rows", chrome)
	}
	if chrome[1] != "  Account: \x1b[38;2;38;139;210mu@example.com\x1b[39m" {
		t.Errorf("Chrome() account row = %q", chrome[1])
	}
}

// TestSpinnerFrame pins the 8-frame Braille spinner and its wrap-around.
func TestSpinnerFrame(t *testing.T) {
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	for i, want := range frames {
		if got := SpinnerFrame(i); got != want {
			t.Errorf("SpinnerFrame(%d) = %q, want %q", i, got, want)
		}
	}
	if got := SpinnerFrame(8); got != frames[0] {
		t.Errorf("SpinnerFrame(8) = %q, want %q (wrap)", got, frames[0])
	}
	if got := SpinnerFrame(-1); got != frames[0] {
		t.Errorf("SpinnerFrame(-1) = %q, want %q (clamped)", got, frames[0])
	}
}

// TestLoadingDoc pins the one-row loading document.
func TestLoadingDoc(t *testing.T) {
	doc := LoadingDoc(0)
	if len(doc) != 1 {
		t.Fatalf("LoadingDoc() = %d rows, want 1", len(doc))
	}
	want := "  ⣾  \x1b[38;2;101;123;131mLoading quota summary...\x1b[39m"
	if doc[0] != want {
		t.Errorf("LoadingDoc(0) = %q, want %q", doc[0], want)
	}
}

// TestDocSummary pins the live 1.4.3 layout: every group carries its
// buckets, two blank rows separate the groups, and the note carries the
// summary's own description when it has one.
func TestDocSummary(t *testing.T) {
	reset := time.Now().Add(90 * time.Minute)
	s := &auth.QuotaSummary{
		Description: "your note",
		Groups: []auth.QuotaGroup{
			{
				Name:        "gemini",
				Description: "Gemini models",
				Buckets: []auth.QuotaBucket{
					{DisplayName: "Weekly", RemainingFraction: 0.5, ResetTime: reset},
					{DisplayName: "5-hour", RemainingFraction: 0, ResetTime: reset},
				},
			},
		},
	}
	doc := DocSummary(s, 220)
	assertSummaryHeader(t, doc)
	assertSummaryBuckets(t, doc)
	assertSummaryTail(t, doc)
}

// assertSummaryHeader pins the group's upper-cased name and its description.
func assertSummaryHeader(t *testing.T, doc []string) {
	t.Helper()
	if doc[0] != "\x1b[1mGEMINI\x1b[0m" {
		t.Errorf("doc[0] = %q, want the upper-cased group name", doc[0])
	}
	if doc[1] != "  \x1b[38;2;101;123;131mGemini models\x1b[39m" {
		t.Errorf("doc[1] = %q", doc[1])
	}
}

// assertSummaryBuckets pins the two buckets: the first is half spent and
// green, the second is spent and red.
func assertSummaryBuckets(t *testing.T, doc []string) {
	t.Helper()
	if doc[2] != "" || doc[3] != "  \x1b[1mWeekly\x1b[0m" {
		t.Errorf("doc[2:4] = %q, want a blank then the bucket header", doc[2:4])
	}
	if !strings.Contains(doc[4], "50.00%") {
		t.Errorf("doc[4] = %q, want the 50%% bar", doc[4])
	}
	if !strings.Contains(doc[5], "Refreshes in 1h 29m") {
		t.Errorf("doc[5] = %q, want the refresh countdown", doc[5])
	}
	if doc[6] != "" || doc[7] != "  \x1b[1m5-hour\x1b[0m" {
		t.Errorf("doc[6:8] = %q, want a blank then the second bucket header", doc[6:8])
	}
	if !strings.Contains(doc[8], "0.00%") {
		t.Errorf("doc[8] = %q, want the empty bar", doc[8])
	}
	if !strings.Contains(doc[9], "\x1b[31mRefreshes in") {
		t.Errorf("doc[9] = %q, want a red refresh line", doc[9])
	}
}

// assertSummaryTail pins the two blank rows after the group and the note.
func assertSummaryTail(t *testing.T, doc []string) {
	t.Helper()
	if doc[10] != "" || doc[11] != "" {
		t.Errorf("doc[10:12] = %q, want two blanks after the group", doc[10:12])
	}
	if !strings.HasSuffix(doc[len(doc)-1], "\x1b[39m") {
		t.Errorf("last note row = %q, want a reset", doc[len(doc)-1])
	}
	if !strings.Contains(doc[len(doc)-1], "your note") {
		t.Errorf("last note row = %q, want the summary description", doc[len(doc)-1])
	}
}

// TestViewerLinesStatic pins the cycle-193/201 fallback document: the
// four chrome rows, then the two group blocks and the clipped note tail.
func TestViewerLinesStatic(t *testing.T) {
	lines := ViewerLinesStatic()
	if len(lines) != 20 {
		t.Fatalf("ViewerLinesStatic() = %d rows, want 20", len(lines))
	}
	if lines[0] != "\x1b[1m\x1b[38;2;88;110;117m└ Models & Quota\x1b[0m" {
		t.Errorf("lines[0] = %q", lines[0])
	}
	if !strings.Contains(lines[2], "Account: ") {
		t.Errorf("lines[2] = %q, want the account row", lines[2])
	}
	if lines[1] != "" || lines[3] != "" {
		t.Errorf("lines = %q, want blank rows around the account row", lines)
	}
}

// TestCountdown pins the three duration forms and the negative clamp.
// The reference truncates to whole seconds, so the expectations leave a
// second of slack below.
func TestCountdown(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    time.Duration
		want string
	}{
		{"two days", 49*time.Hour + 23*time.Minute + 30*time.Second, "49h 23m"},
		{"two hours", 2*time.Hour + 5*time.Minute + 30*time.Second, "2h 5m"},
		{"seconds", 9*time.Second + 500*time.Millisecond, "9s"},
		{"negative", -time.Hour, "0s"},
	} {
		got := Countdown(time.Now().Add(tc.d))
		if got != tc.want {
			t.Errorf("%s: Countdown(%v) = %q, want %q", tc.name, tc.d, got, tc.want)
		}
	}
}
