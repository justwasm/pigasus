// fileviewer_test.go — the /codesearch panel's file viewer.
//
// Cycle 383 (A-18): Enter on a result opens the file in the panel's own
// file viewer, at the matching line, and Ctrl+G opens it in the external
// editor (official-docs/cli/commands/codesearch.md:36-37).
//
// The panel owns the view and the decisions; the host answers the two
// seams the viewer needs (invariant 163): the file's lines and the
// editor. The tests below answer the seams themselves, so the panel is
// the subject and the host's wiring is tested in internal/cliapp.
//
// The viewer's comment flow is cycle 383's A-19 and is tested in
// comment_test.go.

package codesearchpanel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// viewerPanel builds a panel over root with the two seams the viewer
// needs.
//
// The ReadFile seam answers with the file's lines, exactly as the host
// does; the OpenEditor seam records the path and the line it was given.
func viewerPanel(t *testing.T, root string, height int) (*Panel, *string, *int) {
	t.Helper()
	editorPath := ""
	editorLine := 0
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, height },
		Streaming: func() bool { return false },
		ReadFile: func(path string) ([]string, string, bool) {
			data, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				return nil, "File not found: " + path, false
			}
			text := strings.TrimSuffix(string(data), "\n")
			if text == "" {
				return nil, "", true
			}
			return strings.Split(text, "\n"), "", true
		},
		OpenEditor: func(path string, line int) tea.Cmd {
			editorPath, editorLine = path, line
			return nil
		},
	})
	return p, &editorPath, &editorLine
}

// TestViewerEnterOpensTheFile: Enter on a result opens the file, at the
// matching line, and the result list gives way to the file's rows.
func TestViewerEnterOpensTheFile(t *testing.T) {
	root := panelTree(t)
	p, _, _ := viewerPanel(t, root, 24)
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	hit := p.Hits()[0]
	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || !ok {
		t.Fatalf("Enter must be handled by the panel: handled=%v ok=%v", handled, ok)
	}
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, hit.File) {
		t.Errorf("the viewer must show the file's header %q; got %q", hit.File, joined)
	}
	if !strings.Contains(joined, fmt.Sprintf("L%d: ", hit.Line)) {
		t.Errorf("the viewer must show the matching line %d; got %q", hit.Line, joined)
	}
	if strings.Contains(joined, "Found ") {
		t.Errorf("the result list must not be shown while a file is open; got %q", joined)
	}
}

// TestViewerCtrlGOpensTheEditor: Ctrl+G on the result list opens the
// highlighted hit in the external editor, at the hit's line.
func TestViewerCtrlGOpensTheEditor(t *testing.T) {
	root := panelTree(t)
	p, editorPath, editorLine := viewerPanel(t, root, 24)
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	hit := p.Hits()[0]
	handled, _, ok := p.HandleKey(tea.Key{Code: 'g', Mod: tea.ModCtrl})
	if !handled || !ok {
		t.Fatalf("Ctrl+G must be handled by the panel: handled=%v ok=%v", handled, ok)
	}
	if *editorPath != hit.File {
		t.Errorf("editor path = %q, want %q", *editorPath, hit.File)
	}
	if *editorLine != hit.Line {
		t.Errorf("editor line = %d, want %d", *editorLine, hit.Line)
	}
}

// TestViewerCtrlGInsideTheViewer: Ctrl+G inside the open file opens
// that file in the editor, at the matching line.
func TestViewerCtrlGInsideTheViewer(t *testing.T) {
	root := panelTree(t)
	p, editorPath, editorLine := viewerPanel(t, root, 24)
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	hit := p.Hits()[0]
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	handled, _, ok := p.HandleKey(tea.Key{Code: 'g', Mod: tea.ModCtrl})
	if !handled || !ok {
		t.Fatalf("Ctrl+G must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if *editorPath != hit.File {
		t.Errorf("editor path = %q, want %q", *editorPath, hit.File)
	}
	if *editorLine != hit.Line {
		t.Errorf("editor line = %d, want %d", *editorLine, hit.Line)
	}
}

// TestViewerScrollsAndReturns: the viewer owns the scroll keys, and Esc
// closes it — the result list is back.
func TestViewerScrollsAndReturns(t *testing.T) {
	root := t.TempDir()
	body := ""
	for i := 1; i <= 40; i++ {
		body += fmt.Sprintf("line %d\n", i)
		if i == 20 {
			body += "needle\n"
		}
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _, _ := viewerPanel(t, root, 12)
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "L20: ") {
		t.Errorf("the viewer must open on the matching line; got %q", joined)
	}
	if !strings.Contains(joined, "of 41 lines)") {
		t.Errorf("a file longer than the window must show the counter row; got %q", joined)
	}
	// Down scrolls the window.
	before := p.viewer.offset
	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyDown})
	if !handled || !ok {
		t.Fatalf("Down must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if p.viewer.offset != before+1 {
		t.Errorf("offset = %d, want %d", p.viewer.offset, before+1)
	}
	// Esc closes the viewer: the result list is back.
	handled, _, ok = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || !ok {
		t.Fatalf("Esc must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if p.viewer != nil {
		t.Error("Esc must close the viewer")
	}
	joined = strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "Found ") {
		t.Errorf("the result list must be back after Esc; got %q", joined)
	}
}

// TestViewerUnreadableFile: a file the host cannot read shows the
// host's message and no lines.
func TestViewerUnreadableFile(t *testing.T) {
	root := panelTree(t)
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 24 },
		Streaming: func() bool { return false },
		ReadFile: func(path string) ([]string, string, bool) {
			return nil, "Cannot view a pipe, socket, or device; only regular files can be viewed.", false
		},
	})
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "Cannot view a pipe, socket, or device; only regular files can be viewed.") {
		t.Errorf("the viewer must show the host's message; got %q", joined)
	}
}

// TestViewerUnownedKey: a key the viewer does not own is not handled and
// not claimed — the host's global handlers still see it.
func TestViewerUnownedKey(t *testing.T) {
	root := panelTree(t)
	p, _, _ := viewerPanel(t, root, 24)
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	handled, cmd, ok := p.HandleKey(tea.Key{Code: 'x', Text: "x"})
	if handled || cmd != nil || ok {
		t.Errorf("a key the viewer does not own must fall through: handled=%v cmd=%v ok=%v", handled, cmd, ok)
	}
}

// TestViewerFooter: the viewer's footer is the reference's own legend,
// byte for byte (official-docs/cli/commands/codesearch.md:74-78).
func TestViewerFooter(t *testing.T) {
	root := panelTree(t)
	p, _, _ := viewerPanel(t, root, 24)
	p.Start("needle")
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	joined := strings.Join(plainLines(p.Lines()), "\n")
	want := "  ↑/↓ scroll · pgup/pgdown page · shift+g bottom · g top · c comment · ctrl+g editor · / search"
	if !strings.Contains(joined, want) {
		t.Errorf("the footer must be %q; got %q", want, joined)
	}
}
