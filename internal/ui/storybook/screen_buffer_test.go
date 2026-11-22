package storybook

import (
	"strings"
	"testing"
)

func TestScreenBuffer_CaptureAndRoundtrip(t *testing.T) {
	// Sample ANSI view with color, bold, reverse, and plain text
	view := "\x1b[1;34mHeader Title\x1b[0m\n" +
		"\x1b[7mHighlighted Action\x1b[0m plain text\n" +
		"\x1b[4mUnderlined Path: /Users/developer/projects/openagy\x1b[0m (0.01s)\n"

	sb := CaptureScreen(view, 60, 5)

	if sb.Width != 60 || sb.Height != 5 {
		t.Fatalf("expected dimensions 60x5, got %dx%d", sb.Width, sb.Height)
	}

	// 1. Text Matrix Assertions
	row0 := sb.RowText(0)
	if !strings.HasPrefix(row0, "Header Title") {
		t.Errorf("Row 0 expected prefix 'Header Title', got %q", row0)
	}

	row1 := sb.RowText(1)
	if !strings.HasPrefix(row1, "Highlighted Action plain text") {
		t.Errorf("Row 1 expected prefix 'Highlighted Action plain text', got %q", row1)
	}

	row2 := sb.RowText(2)
	// Sanitizer should have sanitized /Users/gear/GitHub/openagy and (0.01s)
	if !strings.Contains(row2, "/workspace") {
		t.Errorf("Row 2 expected sanitized '/workspace', got %q", row2)
	}
	if !strings.Contains(row2, "(XX.Xs)") {
		t.Errorf("Row 2 expected sanitized '(XX.Xs)', got %q", row2)
	}

	// 2. Attribute Matrix Assertions
	// "Header Title" at row 0 should be bold and blue (ansi:4)
	attr0 := sb.AttrAt(0, 0)
	if !attr0.Bold {
		t.Errorf("Row 0, col 0 expected bold, got %+v", attr0)
	}
	if attr0.Fg != "ansi:4" && !strings.Contains(attr0.Fg, "4") {
		t.Errorf("Row 0, col 0 expected fg=ansi:4, got %+v", attr0)
	}

	// "Highlighted Action" at row 1 should be reverse
	attr1 := sb.AttrAt(1, 0)
	if !attr1.Reverse {
		t.Errorf("Row 1, col 0 expected reverse, got %+v", attr1)
	}

	// Plain text at row 1 (e.g. col 25) should have zero attributes
	attr1Plain := sb.AttrAt(1, 25)
	if !attr1Plain.IsZero() {
		t.Errorf("Row 1, col 25 expected zero attributes, got %+v", attr1Plain)
	}

	// "Underlined Path" at row 2 should be underline
	attr2 := sb.AttrAt(2, 0)
	if !attr2.Underline {
		t.Errorf("Row 2, col 0 expected underline, got %+v", attr2)
	}

	// 3. Encode & Decode Roundtrip Test
	encoded := sb.Encode()
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Width != sb.Width || decoded.Height != sb.Height {
		t.Fatalf("Decoded dimensions %dx%d != original %dx%d",
			decoded.Width, decoded.Height, sb.Width, sb.Height)
	}

	diffReport := Diff(sb, decoded)
	if diffReport != "" {
		t.Fatalf("Roundtrip mismatch between original and decoded:\n%s", diffReport)
	}
}

func TestSnapshotDiff_CharacterLevelMismatch(t *testing.T) {
	sb1 := NewScreenBuffer(40, 3)
	copy(sb1.Text[0], []rune("Hello World"))
	sb1.Attrs[0][0] = CellAttribute{Bold: true, Fg: "ansi:2"}
	sb1.Attrs[0][1] = CellAttribute{Bold: true, Fg: "ansi:2"}

	sb2 := NewScreenBuffer(40, 3)
	copy(sb2.Text[0], []rune("Hello Earth"))
	// Intentionally mismatch character at col 6 ('W' vs 'E')
	// and attribute at col 0 (fg ansi:2 vs ansi:1)
	sb2.Attrs[0][0] = CellAttribute{Bold: true, Fg: "ansi:1"}
	sb2.Attrs[0][1] = CellAttribute{Bold: true, Fg: "ansi:2"}

	diffReport := Diff(sb1, sb2)
	if diffReport == "" {
		t.Fatalf("expected Diff to report differences, but got empty string")
	}

	// Verify diff report contains ruler and exact character details
	if !strings.Contains(diffReport, "Row 00 Mismatch") {
		t.Errorf("expected 'Row 00 Mismatch' in diff report, got:\n%s", diffReport)
	}
	if !strings.Contains(diffReport, "rune mismatch") {
		t.Errorf("expected 'rune mismatch' in diff report, got:\n%s", diffReport)
	}
	if !strings.Contains(diffReport, "attr mismatch") {
		t.Errorf("expected 'attr mismatch' in diff report, got:\n%s", diffReport)
	}
	if !strings.Contains(diffReport, "^") {
		t.Errorf("expected visual ruler '^' in diff report, got:\n%s", diffReport)
	}
}
