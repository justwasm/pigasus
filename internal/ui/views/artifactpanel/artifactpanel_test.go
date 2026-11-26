// artifactpanel_test.go — the /artifact panel's frames.
//
// The empty frame's layout is the reference's own: the title, the empty
// state, a blank and the legend. The populated frame lists the
// conversation's artifacts; its row shape is ours (the reference's
// populated /artifact frame was never captured — see cycle 206's 存疑),
// but it may never claim "No artifacts" when there are some.

package artifactpanel

import (
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/palette"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripSGR removes the SGR spans from a row.
func stripSGR(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

func TestLinesLayout(t *testing.T) {
	lines := Lines()
	want := []string{
		"Artifacts",
		"  No artifacts",
		"",
		"Keyboard: ↑/↓ Navigate  p preview  enter open  ctrl+g open in editor  esc Dismiss",
	}
	if len(lines) != len(want) {
		t.Fatalf("line count: got %d, want %d\n got: %q\nwant: %q", len(lines), len(want), lines, want)
	}
	for i := range want {
		if got := stripSGR(lines[i]); got != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got, want[i])
		}
	}
}

// The empty state is painted in the dim grey, and the title is the
// reference's own bold span.
func TestLinesColours(t *testing.T) {
	lines := Lines()
	if !strings.HasPrefix(lines[0], panelTitleOpen+"Artifacts"+panelTitleClose) {
		t.Errorf("title = %q", lines[0])
	}
	if !strings.Contains(lines[1], palette.DimGrayOpen) {
		t.Errorf("empty state should be the dim gray: %q", lines[1])
	}
	if !strings.HasSuffix(lines[1], palette.DimGrayClose) {
		t.Errorf("empty state must end with the dim gray close: %q", lines[1])
	}
}

// TestLinesForEmptyFallsBackToTheEmptyFrame keeps an empty conversation on
// the exact frame the reference draws.
func TestLinesForEmptyFallsBackToTheEmptyFrame(t *testing.T) {
	got := LinesFor(nil)
	want := Lines()
	if len(got) != len(want) {
		t.Fatalf("line count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

// TestLinesForListsTheArtifacts is the regression test for the panel that
// answered "No artifacts" for every conversation. The title stays the
// reference's, each artifact gets a row, and the legend stays last.
func TestLinesForListsTheArtifacts(t *testing.T) {
	refs := []compaction.ArtifactRef{
		{Name: "artifact-alignment-plan"},
		{Name: "config-key-audit"},
	}
	got := LinesFor(refs)
	wantNames := []string{"artifact-alignment-plan", "config-key-audit"}

	if p := stripSGR(got[0]); p != "Artifacts" {
		t.Errorf("title = %q, want Artifacts", p)
	}
	if len(got) != len(wantNames)+3 {
		t.Fatalf("line count = %d, want %d: %q", len(got), len(wantNames)+3, got)
	}
	for i, name := range wantNames {
		if p := stripSGR(got[i+1]); p != "  "+name {
			t.Errorf("row %d = %q, want %q", i+1, p, "  "+name)
		}
	}
	if p := stripSGR(got[len(got)-2]); p != "" {
		t.Errorf("penultimate row = %q, want blank", p)
	}
	if p := stripSGR(got[len(got)-1]); !strings.HasPrefix(p, "Keyboard: ") {
		t.Errorf("last row = %q, want the legend", p)
	}
}

// TestLinesForNeverSaysNoArtifactsWhenThereAreSome is the blunt form of the
// defect: "No artifacts" must not appear once the conversation has any.
func TestLinesForNeverSaysNoArtifactsWhenThereAreSome(t *testing.T) {
	for _, line := range LinesFor([]compaction.ArtifactRef{{Name: "plan"}}) {
		if strings.Contains(stripSGR(line), "No artifacts") {
			t.Errorf("the populated frame still says %q", stripSGR(line))
		}
	}
}

// TestLinesForRowsAreDimmed keeps the row styling consistent with the other
// list panels: an unselected row is the dim grey.
func TestLinesForRowsAreDimmed(t *testing.T) {
	got := LinesFor([]compaction.ArtifactRef{{Name: "plan"}})
	if !strings.HasPrefix(got[1], "  "+palette.DimGrayOpen) {
		t.Errorf("row = %q, want the dim grey lead", got[1])
	}
	if !strings.HasSuffix(got[1], palette.DimGrayClose) {
		t.Errorf("row = %q, want the dim grey close", got[1])
	}
}
