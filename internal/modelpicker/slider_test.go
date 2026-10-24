package modelpicker

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/palette"
)

func TestEffortBarSegmentsLevels(t *testing.T) {
	for _, level := range []int{EffortLow, EffortMedium, EffortHigh} {
		lit, dim := EffortBarSegments(level)
		if !strings.Contains(lit, "◉") {
			t.Errorf("level %d: lit missing active marker ◉: %q", level, lit)
		}
		if strings.Contains(dim, "◉") {
			t.Errorf("level %d: dim should not contain active marker ◉: %q", level, dim)
		}
		dots := strings.Count(lit, "●")
		if dots != level {
			t.Errorf("level %d: lit has %d ● markers, want %d", level, dots, level)
		}
	}
}

func TestEffortBarSegmentsBounds(t *testing.T) {
	litHigh, dimHigh := EffortBarSegments(EffortHigh)
	for _, bad := range []int{-5, -1, 3, 10} {
		lit, dim := EffortBarSegments(bad)
		if lit != litHigh || dim != dimHigh {
			t.Errorf("level %d did not default to EffortHigh", bad)
		}
	}
}

func TestEffortSliderBar(t *testing.T) {
	colours := [3]string{PanelEffLowOpen, PanelEffMedOpen, PanelEffHighOpen}
	lowBar := EffortSliderBar(EffortLow, colours)
	if !strings.Contains(lowBar, PanelGray238+"◂"+PanelGray238C) {
		t.Errorf("lowBar should have dim left arrow: %q", lowBar)
	}
	if !strings.Contains(lowBar, palette.BlueOpen+"▸"+palette.BlueClose) {
		t.Errorf("lowBar should have lit right arrow: %q", lowBar)
	}

	highBar := EffortSliderBar(EffortHigh, colours)
	if !strings.Contains(highBar, palette.BlueOpen+"◂"+palette.BlueClose) {
		t.Errorf("highBar should have lit left arrow: %q", highBar)
	}
	if !strings.Contains(highBar, PanelGray238+"▸"+PanelGray238C) {
		t.Errorf("highBar should have dim right arrow: %q", highBar)
	}
}

func TestEffortSliderLabels(t *testing.T) {
	colours := [3]string{PanelEffLowOpen, PanelEffMedOpen, PanelEffHighOpen}
	labels := EffortSliderLabels(EffortMedium, colours)
	if !strings.Contains(labels, colours[EffortMedium]) {
		t.Errorf("labels missing medium colour: %q", labels)
	}
	if !strings.Contains(labels, "medium") {
		t.Errorf("labels missing medium text: %q", labels)
	}
	if !strings.Contains(labels, PanelGray238) {
		t.Errorf("labels missing dim colour for inactive tiers: %q", labels)
	}
}

func TestRebuildEffortSliderStructure(t *testing.T) {
	lines := RebuildEffortSlider(EffortMedium, EffortLow, false)
	if len(lines) != 7 {
		t.Fatalf("RebuildEffortSlider returned %d lines, want 7", len(lines))
	}
	if !strings.Contains(lines[0], "Set Effort") {
		t.Errorf("title line mismatch: %q", lines[0])
	}
	if lines[1] != "" || lines[5] != "" {
		t.Errorf("blank lines mismatch: %q, %q", lines[1], lines[5])
	}
	if !strings.Contains(lines[6], "Keyboard:") {
		t.Errorf("legend line missing Keyboard hint: %q", lines[6])
	}
}

func TestRebuildEffortSliderAnnotation(t *testing.T) {
	annotated := RebuildEffortSlider(EffortHigh, EffortHigh, true)
	if !strings.Contains(annotated[4], "(current)") {
		t.Errorf("expected (current) annotation in description: %q", annotated[4])
	}

	notAnnotated := RebuildEffortSlider(EffortHigh, EffortHigh, false)
	if strings.Contains(notAnnotated[4], "(current)") {
		t.Errorf("unexpected (current) annotation when annotate is false: %q", notAnnotated[4])
	}

	mismatched := RebuildEffortSlider(EffortHigh, EffortLow, true)
	if strings.Contains(mismatched[4], "(current)") {
		t.Errorf("unexpected (current) annotation when levels differ: %q", mismatched[4])
	}
}

func TestRenderModelPickerRowCursor(t *testing.T) {
	row := Row{Display: "Gemini 3.8 Flash", Slug: "gemini-3.8-flash"}
	cursorLine := RenderModelPickerRow(row, 0, 0, false)
	if !strings.HasPrefix(cursorLine, palette.BlueOpen+"> ") {
		t.Errorf("cursor line missing blue pointer prefix: %q", cursorLine)
	}
	if !strings.Contains(cursorLine, row.Display) {
		t.Errorf("cursor line missing display label: %q", cursorLine)
	}

	nonCursorLine := RenderModelPickerRow(row, 1, 0, false)
	if !strings.HasPrefix(nonCursorLine, "  "+row.Display) {
		t.Errorf("non-cursor line missing indent prefix: %q", nonCursorLine)
	}
}

func TestRenderModelPickerRowCurrentAndDim(t *testing.T) {
	row := Row{Display: "Gemini 4 Argon", Dim: true}
	dimLine := RenderModelPickerRow(row, 1, 0, false)
	if !strings.Contains(dimLine, palette.DimGrayOpen) {
		t.Errorf("dim line missing dim grey styling: %q", dimLine)
	}

	currRow := Row{Display: "Claude Sonnet 4.6 (Thinking)", Slug: "claude-sonnet-4-6"}
	currLine := RenderModelPickerRow(currRow, 0, 0, true)
	if !strings.Contains(currLine, "(current)") {
		t.Errorf("current row missing (current) annotation: %q", currLine)
	}
}

func TestRenderModelPickerRowNote(t *testing.T) {
	row := Row{Display: "MiniMax M3", Slug: "minimax-m3", Note: "via gateway"}
	line := RenderModelPickerRow(row, 0, 0, false)
	if !strings.Contains(line, "via gateway") {
		t.Errorf("row with note missing note text: %q", line)
	}
	if !strings.Contains(line, "\x1b[38;5;60m") {
		t.Errorf("note missing xterm-60 colour: %q", line)
	}
}

func TestPickerRenderHelpers(t *testing.T) {
	if got := ResolveCatalogueID("  gemini-3.8-flash-high  "); got != "gemini-3.8-flash-high" {
		t.Errorf("ResolveCatalogueID = %q, want gemini-3.8-flash-high", got)
	}
	if got := StripLeadingSpaces("    padded", 4); got != "padded" {
		t.Errorf("StripLeadingSpaces = %q, want padded", got)
	}
	if got := PanelModelBaseSlug("Gemini 3.8 Flash (High)"); got != "gemini-3.8-flash" {
		t.Errorf("PanelModelBaseSlug = %q, want gemini-3.8-flash", got)
	}
	if entry, ok := CatalogueEntry("gemini-3.8-flash-high"); !ok || entry.ID != "gemini-3.8-flash-high" {
		t.Errorf("CatalogueEntry(gemini-3.8-flash-high) failed: %+v, ok=%v", entry, ok)
	}
	if _, ok := CatalogueEntry("non-existent-model"); ok {
		t.Errorf("CatalogueEntry for unknown model succeeded unexpectedly")
	}
}
