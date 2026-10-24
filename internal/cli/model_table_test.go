// Tests for the model catalogue's rendering.
//
// 【一手】 agy 1.4.5 renders `agy models` two ways:
//
//	stdout is a terminal  ->  space aligned columns (tabwriter, padding 2)
//	stdout is a pipe      ->  tab separated rows
//
// The aligned form was measured off a pty capture of the reference: the
// widest catalogue id is 24 characters ("claude-opus-4-6-thinking"), so
// every label starts at column 26.
package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestModelTable_TabSeparatedWhenNotATerminal(t *testing.T) {
	var buf bytes.Buffer
	entries := []ModelEntry{
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
	}
	if err := modelTable(&buf, entries, false); err != nil {
		t.Fatalf("modelTable: %v", err)
	}
	want := "gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n" +
		"claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)\n"
	if buf.String() != want {
		t.Errorf("tab separated table mismatch\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestModelTable_AlignedWhenTerminal(t *testing.T) {
	var buf bytes.Buffer
	entries := []ModelEntry{
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
	}
	if err := modelTable(&buf, entries, true); err != nil {
		t.Fatalf("modelTable: %v", err)
	}
	// Column 26 for every label: the widest id (24) plus the padding of 2.
	want := "gemini-3.8-flash-medium   Gemini 3.8 Flash (Medium)\n" +
		"claude-opus-4-6-thinking  Claude Opus 4.6 (Thinking)\n"
	if buf.String() != want {
		t.Errorf("aligned table mismatch\n got: %q\nwant: %q", buf.String(), want)
	}
	// The last cell of a row is never padded, so no row ends in a space.
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("row ends in a space: %q", line)
		}
	}
}

func TestModelTable_ReferenceColumnWidth(t *testing.T) {
	// The reference's own catalogue: the label column starts at 26.
	var buf bytes.Buffer
	if err := modelTable(&buf, ReferenceModels(), true); err != nil {
		t.Fatalf("modelTable: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		idx := strings.Index(line, "  ")
		if idx < 0 {
			continue
		}
		label := strings.TrimLeft(line[idx:], " ")
		if label == "" {
			continue
		}
		col := len(line) - len(label)
		if col != 26 {
			t.Errorf("label column = %d, want 26 (line %q)", col, line)
		}
	}
}

func TestIsTerminalWriter(t *testing.T) {
	if isTerminalWriter(&bytes.Buffer{}) {
		t.Error("a bytes.Buffer must not be reported as a terminal")
	}
	// os.Stdout under `go test` is a pipe or a file, never a terminal, so the
	// only assertion available here is the negative one.
	if isTerminalWriter(os.Stdout) {
		t.Skip("stdout is a terminal in this environment")
	}
}
