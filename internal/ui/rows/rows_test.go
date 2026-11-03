// rows_test.go — the notice rows' and the queued rows' own bytes.
//
// The rows are pinned by their exact byte sequence, because the
// difference between the three notice shapes is two spaces of indent
// and the body's colour — a difference no rendered comparison shows.
//
// The golden constants below are the palette's default scheme values.
// The first test pins the palette to them, so the golden rows are
// anchored to the palette rather than to a copy of it.

package rows

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/palette"
)

// The palette's default scheme values, as literals.
const (
	goldenWarningBlueOpen  = "\x1b[38;2;38;139;210;1m"
	goldenWarningBlueClose = "\x1b[0m"
	goldenSubtleOpen       = "\x1b[38;2;88;110;117m"
	goldenDimGrayOpen      = "\x1b[38;2;101;123;131m"
	goldenClose39          = "\x1b[39m"
)

// TestRows_PaletteDefaults pins the palette's default scheme values to
// the literals the golden rows below are built from. Without it the
// goldens would be the palette's own values restated.
func TestRows_PaletteDefaults(t *testing.T) {
	if palette.WarningBlueOpen != goldenWarningBlueOpen {
		t.Errorf("palette.WarningBlueOpen = %q, want %q", palette.WarningBlueOpen, goldenWarningBlueOpen)
	}
	if palette.WarningBlueClose != goldenWarningBlueClose {
		t.Errorf("palette.WarningBlueClose = %q, want %q", palette.WarningBlueClose, goldenWarningBlueClose)
	}
	if palette.SubtleOpen != goldenSubtleOpen {
		t.Errorf("palette.SubtleOpen = %q, want %q", palette.SubtleOpen, goldenSubtleOpen)
	}
	if palette.DimGrayOpen != goldenDimGrayOpen {
		t.Errorf("palette.DimGrayOpen = %q, want %q", palette.DimGrayOpen, goldenDimGrayOpen)
	}
	if palette.DimGrayClose != goldenClose39 {
		t.Errorf("palette.DimGrayClose = %q, want %q", palette.DimGrayClose, goldenClose39)
	}
}

// noticeCase is one fixture: a name and the text the row carries.
type noticeCase struct {
	name string
	text string
}

// noticeFixtures are the fixtures every notice shape is exercised with.
//
// The set covers the empty body, a body with a newline in it, a body
// with a wide (CJK) rune in it, a very long body, the compaction
// package's own cancellation literal, and the reference's own failure
// prefix.
var noticeFixtures = []noticeCase{
	{name: "empty", text: ""},
	{name: "plain", text: "Already compacting."},
	{name: "ellipsis", text: "Compacting context\u2026"},
	{name: "cancellation literal", text: compaction.CompactionCancelledLiteral},
	{name: "failure prefix", text: "Compact failed: exit status 1"},
	{name: "newline", text: "first line\nsecond line"},
	{name: "wide rune", text: "\u538b\u7f29\u5bf9\u8bdd\u2026"},
	{name: "very long", text: strings.Repeat("x", 4096)},
	{name: "percent signs", text: "%s %d %v"},
	{name: "quotes", text: `he said "hi"`},
	{name: "queue-mode label", text: "alt+m \u2192 Queued Messages: Queue"},
	{name: "queue-mode label, queueing", text: "alt+m \u2192 Queued Messages: Queued Messages"},
}

// TestRows_NoticeCompact pins the compact notice row: the bold-blue
// "⎿  " bullet at the left margin, with the muted body.
func TestRows_NoticeCompact(t *testing.T) {
	for _, tc := range noticeFixtures {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenWarningBlueOpen + "\u23bf  " + goldenWarningBlueClose +
				goldenSubtleOpen + tc.text + goldenClose39
			got := NoticeCompact(tc.text)
			if got != want {
				t.Errorf("NoticeCompact(%q) = %q, want %q", tc.text, got, want)
			}
		})
	}
}

// TestRows_NoticeIndented pins the indented notice row: the bold-blue
// "  ⎿  " bullet, with the muted body.
func TestRows_NoticeIndented(t *testing.T) {
	for _, tc := range noticeFixtures {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenWarningBlueOpen + "  \u23bf  " + goldenWarningBlueClose +
				goldenSubtleOpen + tc.text + goldenClose39
			got := NoticeIndented(tc.text)
			if got != want {
				t.Errorf("NoticeIndented(%q) = %q, want %q", tc.text, got, want)
			}
		})
	}
}

// TestRows_NoticeDim pins the indented notice row with the dim-gray
// body.
func TestRows_NoticeDim(t *testing.T) {
	for _, tc := range noticeFixtures {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenWarningBlueOpen + "  \u23bf  " + goldenWarningBlueClose +
				goldenDimGrayOpen + tc.text + goldenClose39
			got := NoticeDim(tc.text)
			if got != want {
				t.Errorf("NoticeDim(%q) = %q, want %q", tc.text, got, want)
			}
		})
	}
}

// TestRows_NoticeShapesDiffer pins that the three notice shapes are
// three shapes and not three spellings of one. The compact row differs
// from the indented one by two spaces, and the indented row differs
// from the dim one by the body's colour.
func TestRows_NoticeShapesDiffer(t *testing.T) {
	const text = "x"
	compact := NoticeCompact(text)
	indented := NoticeIndented(text)
	dim := NoticeDim(text)

	if compact == indented {
		t.Error("the compact and the indented notice rows are the same bytes")
	}
	if indented == dim {
		t.Error("the indented and the dim notice rows are the same bytes")
	}
	if !strings.HasPrefix(indented, goldenWarningBlueOpen+"  ") {
		t.Errorf("the indented row does not start with two spaces: %q", indented)
	}
	if strings.HasPrefix(compact, goldenWarningBlueOpen+"  ") {
		t.Errorf("the compact row starts with two spaces: %q", compact)
	}
}

// queuedFixtures are the fixtures the queued rows are exercised with.
//
// The set covers the empty body, a body with a newline in it, a body
// with a wide (CJK) rune in it, a very long body, a queued shell
// command and the empty command.
var queuedFixtures = []noticeCase{
	{name: "empty", text: ""},
	{name: "plain", text: "SECOND message while running"},
	{name: "newline", text: "first line\nsecond line"},
	{name: "wide rune", text: "\u961f\u5217\u6d88\u606f"},
	{name: "very long", text: strings.Repeat("y", 4096)},
	{name: "shell command", text: "git status --porcelain"},
	{name: "empty command", text: ""},
	{name: "percent signs", text: "%s %d %v"},
	{name: "quotes", text: `he said "hi"`},
}

// TestRows_QueuedPromptRow pins the queued prompt's row: the dim-gray
// "▸ " head, the prompt, and the gesture suffix inside the same span.
func TestRows_QueuedPromptRow(t *testing.T) {
	for _, tc := range queuedFixtures {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenDimGrayOpen + "\u25b8 " + tc.text +
				" (alt+m to send immediately)" + goldenClose39
			got := QueuedPromptRow(tc.text)
			if got != want {
				t.Errorf("QueuedPromptRow(%q) = %q, want %q", tc.text, got, want)
			}
		})
	}
}

// TestRows_QueuedCommandRow pins the queued command's row: the dim-gray
// "▸ " head and the command, with no gesture suffix.
func TestRows_QueuedCommandRow(t *testing.T) {
	for _, tc := range queuedFixtures {
		t.Run(tc.name, func(t *testing.T) {
			want := goldenDimGrayOpen + "\u25b8 " + tc.text + goldenClose39
			got := QueuedCommandRow(tc.text)
			if got != want {
				t.Errorf("QueuedCommandRow(%q) = %q, want %q", tc.text, got, want)
			}
		})
	}
}

// TestRows_QueuedRowsDiffer pins that the queued prompt's row and the
// queued command's row are two shapes: only the prompt's carries the
// gesture suffix.
func TestRows_QueuedRowsDiffer(t *testing.T) {
	const text = "x"
	prompt := QueuedPromptRow(text)
	command := QueuedCommandRow(text)
	if prompt == command {
		t.Error("the queued prompt and the queued command rows are the same bytes")
	}
	if !strings.HasSuffix(prompt, " (alt+m to send immediately)"+goldenClose39) {
		t.Errorf("the queued prompt row does not carry the gesture suffix: %q", prompt)
	}
	if strings.Contains(command, "alt+m") {
		t.Errorf("the queued command row carries the gesture suffix: %q", command)
	}
}
