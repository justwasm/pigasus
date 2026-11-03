package modelpicker

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// stripSGR removes the SGR escape sequences from a rendered line.
func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
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
