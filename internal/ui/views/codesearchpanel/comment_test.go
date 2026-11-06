// comment_test.go — the /codesearch panel's comment flow.
//
// Cycle 383 (A-19): the file viewer carries the reference's comment
// flow — c opens the inline comment editor, d drops the comment the
// cursor's line carries, a saved comment is marked with a 💬 in the
// gutter, and leaving the file with comments that were not sent yet
// opens the "Unsent Comments" screen (official-docs/cli/commands/codesearch.md:80-100).
//
// The panel owns the flow; the host answers the seams — the file's
// lines, the editor, what happens when the comments are sent and how
// the panel is left. The tests below answer the seams themselves, so
// the panel is the subject and the host's wiring is tested in
// internal/cliapp.

package codesearchpanel

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
)

// commentPanel builds a panel over root with the seams the comment flow
// needs.
//
// The ReadFile seam answers with the file's lines; the SendComments
// seam records the text the panel hands over and the Close seam records
// that the panel was left.
func commentPanel(t *testing.T, root string) (p *Panel, sent *string, closed *bool) {
	t.Helper()
	var sentText string
	var wasClosed bool
	p = New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 24 },
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
		SendComments: func(text string) tea.Cmd {
			sentText = text
			return nil
		},
		Close: func() tea.Cmd {
			wasClosed = true
			return nil
		},
	})
	return p, &sentText, &wasClosed
}

// openFirstFile opens the first hit's file in the panel's viewer.
func openFirstFile(t *testing.T, p *Panel) {
	t.Helper()
	if len(p.Hits()) == 0 {
		t.Fatal("the search found no hits")
	}
	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || !ok {
		t.Fatalf("Enter must be handled by the panel: handled=%v ok=%v", handled, ok)
	}
	if p.viewer == nil {
		t.Fatal("Enter must open the file viewer")
	}
}

// typeComment types a comment into the open editor and saves it.
func typeComment(t *testing.T, p *Panel, body string) {
	t.Helper()
	for _, r := range body {
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
	}
	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || !ok {
		t.Fatalf("Enter must save the comment: handled=%v ok=%v", handled, ok)
	}
}

// TestViewerCommentMarksTheLine: c opens the inline comment editor, the
// saved comment is marked with the reference's 💬 in the gutter, and the
// comment is anchored to the cursor's line.
func TestViewerCommentMarksTheLine(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	line := p.viewer.cursor

	handled, _, ok := p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	if !handled || !ok {
		t.Fatalf("c must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if !p.viewer.commenting {
		t.Fatal("c must open the comment editor")
	}
	typeComment(t, p, "look here")

	if p.viewer.commenting {
		t.Error("Enter must close the comment editor")
	}
	c, found := p.viewer.comments.At(line)
	if !found {
		t.Fatalf("the comment must be stored against line %d", line)
	}
	if c.Body != "look here" {
		t.Errorf("the comment body = %q, want %q", c.Body, "look here")
	}
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, linecomments.Marker) {
		t.Errorf("the line must carry the %s marker; got %q", linecomments.Marker, joined)
	}
}

// TestViewerDeleteComment: d drops the comment the cursor's line
// carries, and the marker goes with it.
func TestViewerDeleteComment(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")

	handled, _, ok := p.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if !handled || !ok {
		t.Fatalf("d must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if _, found := p.viewer.comments.At(p.viewer.cursor); found {
		t.Error("d must drop the comment the cursor's line carries")
	}
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if strings.Contains(joined, linecomments.Marker) {
		t.Errorf("the marker must be gone; got %q", joined)
	}
}

// TestViewerCtrlDIsNotDeleteComment: Ctrl+D is the pager's page-down and
// must not drop a comment.
func TestViewerCtrlDIsNotDeleteComment(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")

	p.HandleKey(tea.Key{Code: 'd', Mod: tea.ModCtrl})
	if _, found := p.viewer.comments.At(p.viewer.cursor); !found {
		t.Error("Ctrl+D must not drop the comment")
	}
}

// TestViewerEscWithCommentsOpensTheUnsentScreen: leaving the file with
// comments that were not sent yet opens the reference's screen.
func TestViewerEscWithCommentsOpensTheUnsentScreen(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")

	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || !ok {
		t.Fatalf("Esc must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if p.unsent == nil {
		t.Fatal("Esc must open the Unsent Comments screen when a comment is pending")
	}
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, linecomments.Header) {
		t.Errorf("the screen must carry the %q header; got %q", linecomments.Header, joined)
	}
}

// TestViewerEscWithoutCommentsClosesTheFile: without a pending comment
// Esc closes the file and the result list is back.
func TestViewerEscWithoutCommentsClosesTheFile(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.viewer != nil {
		t.Error("Esc must close the file when no comment is pending")
	}
	if p.unsent != nil {
		t.Error("the Unsent Comments screen must not open without a pending comment")
	}
}

// TestUnsentSmallLettersDoNotAnswer: the screen's answers are the
// capital letters — a lower-case y is not a yes.
func TestUnsentSmallLettersDoNotAnswer(t *testing.T) {
	root := panelTree(t)
	p, sent, closed := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.unsent == nil {
		t.Fatal("the Unsent Comments screen must be up")
	}

	for _, r := range []rune{'y', 'n'} {
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
		if p.unsent == nil {
			t.Fatalf("the lower-case %q must not answer the screen", string(r))
		}
		if *sent != "" {
			t.Errorf("the lower-case %q must not send the comments; sent %q", string(r), *sent)
		}
		if *closed {
			t.Errorf("the lower-case %q must not close the panel", string(r))
		}
	}
}

// TestUnsentYSendsTheComments: Y hands the comments to the host in the
// reference's wire format.
func TestUnsentYSendsTheComments(t *testing.T) {
	root := panelTree(t)
	p, sent, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.unsent == nil {
		t.Fatal("the Unsent Comments screen must be up")
	}
	line := p.viewer.cursor
	file := p.viewer.path

	handled, _, ok := p.HandleKey(tea.Key{Code: 'Y', Text: "Y"})
	if !handled || !ok {
		t.Fatalf("Y must be handled by the screen: handled=%v ok=%v", handled, ok)
	}
	want := "<file>" + file + ":" + strconv.Itoa(line) + ":look here\n"
	if *sent != want {
		t.Errorf("the sent text = %q, want %q", *sent, want)
	}
	if p.unsent != nil {
		t.Error("the screen must be gone after Y")
	}
}

// TestUnsentNDiscardsTheComments: N leaves the panel without sending.
func TestUnsentNDiscardsTheComments(t *testing.T) {
	root := panelTree(t)
	p, sent, closed := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.unsent == nil {
		t.Fatal("the Unsent Comments screen must be up")
	}

	handled, _, ok := p.HandleKey(tea.Key{Code: 'N', Text: "N"})
	if !handled || !ok {
		t.Fatalf("N must be handled by the screen: handled=%v ok=%v", handled, ok)
	}
	if *sent != "" {
		t.Errorf("N must not send the comments; sent %q", *sent)
	}
	if !*closed {
		t.Error("N must leave the panel")
	}
	if p.unsent != nil {
		t.Error("the screen must be gone after N")
	}
}

// TestUnsentEscReturnsToTheViewer: Esc cancels the screen and the
// viewer is back.
func TestUnsentEscReturnsToTheViewer(t *testing.T) {
	root := panelTree(t)
	p, sent, closed := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.unsent == nil {
		t.Fatal("the Unsent Comments screen must be up")
	}

	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || !ok {
		t.Fatalf("Esc must be handled by the screen: handled=%v ok=%v", handled, ok)
	}
	if p.unsent != nil {
		t.Error("Esc must close the screen")
	}
	if p.viewer == nil {
		t.Error("Esc must bring the viewer back")
	}
	if *sent != "" {
		t.Errorf("Esc must not send the comments; sent %q", *sent)
	}
	if *closed {
		t.Error("Esc must not close the panel")
	}
	if _, found := p.viewer.comments.At(p.viewer.cursor); !found {
		t.Error("the comment must survive the cancelled screen")
	}
}

// TestViewerCommentSurvivesTheScreen: a comment is anchored to a line
// of a file, so it is still there when the screen is cancelled.
func TestViewerCommentSurvivesTheScreen(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	line := p.viewer.cursor

	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.unsent == nil {
		t.Fatal("the Unsent Comments screen must be up")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	if p.viewer == nil {
		t.Fatal("the viewer must be back")
	}
	if _, found := p.viewer.comments.At(line); !found {
		t.Error("the comment must survive the cancelled screen")
	}
}

// TestViewerCommentsArePerFile: a comment is anchored to a line of a
// file, so the two files' comments never mix.
func TestViewerCommentsArePerFile(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	p.HandleKey(tea.Key{Code: tea.KeyEsc})

	file := p.viewer.path
	other := ""
	for _, h := range p.Hits() {
		if h.File != file {
			other = h.File
			break
		}
	}
	if other == "" {
		t.Fatal("the tree must hold a second file with a hit")
	}
	if got := p.storeFor(file).Len(); got != 1 {
		t.Errorf("the comments of %q = %d, want 1", file, got)
	}
	if got := p.storeFor(other).Len(); got != 0 {
		t.Errorf("the comments of %q = %d, want 0", other, got)
	}
}

// TestViewerCommentEditorClaimsEveryKey: the inline editor is modal —
// it claims the keys under it, and the ones it does not own are text.
func TestViewerCommentEditorClaimsEveryKey(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})

	handled, cmd, ok := p.HandleKey(tea.Key{Code: 'x', Text: "x"})
	if !handled || cmd != nil || !ok {
		t.Errorf("the editor must claim the key: handled=%v cmd=%v ok=%v", handled, cmd, ok)
	}
	if !p.viewer.commenting {
		t.Error("the editor must stay up")
	}
	if got := strings.TrimSpace(p.viewer.comment.Value()); got != "x" {
		t.Errorf("the draft = %q, want %q", got, "x")
	}
}

// TestUnsentScreenClaimsEveryKey: the Unsent Comments screen is modal —
// it claims every key under it, and a key it does not answer leaves the
// screen up.
func TestUnsentScreenClaimsEveryKey(t *testing.T) {
	root := panelTree(t)
	p, sent, closed := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.unsent == nil {
		t.Fatal("the Unsent Comments screen must be up")
	}

	handled, cmd, ok := p.HandleKey(tea.Key{Code: 'x', Text: "x"})
	if !handled || cmd != nil || !ok {
		t.Errorf("the screen must claim the key: handled=%v cmd=%v ok=%v", handled, cmd, ok)
	}
	if p.unsent == nil {
		t.Error("a key the screen does not answer must leave it up")
	}
	if *sent != "" || *closed {
		t.Errorf("the key must neither send nor close: sent=%q closed=%v", *sent, *closed)
	}
}

// TestViewerCommentBindingBeatsThePager: the comment bindings are
// checked before the pager's own keys, so d drops a comment and does not
// page down.
func TestViewerCommentBindingBeatsThePager(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "look here")

	before := p.viewer.cursor
	p.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if p.viewer.cursor != before {
		t.Errorf("the cursor = %d, want %d: d must not page down", p.viewer.cursor, before)
	}
}

// TestViewerSearchFindsTheNextLine: the search prompt moves the cursor
// to the next line that carries the query.
func TestViewerSearchFindsTheNextLine(t *testing.T) {
	root := panelTree(t)
	p, _, _ := commentPanel(t, root)
	p.Start("needle")
	openFirstFile(t, p)

	handled, _, ok := p.HandleKey(tea.Key{Code: '/', Text: "/"})
	if !handled || !ok {
		t.Fatalf("/ must be handled by the viewer: handled=%v ok=%v", handled, ok)
	}
	if !p.viewer.searching {
		t.Fatal("/ must open the search prompt")
	}
	for _, r := range "line4" {
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p.viewer.searching {
		t.Error("Enter must close the search prompt")
	}
	line := p.viewer.cursor
	if line < 1 || line > len(p.viewer.lines) {
		t.Fatalf("the cursor = %d, out of the file's %d lines", line, len(p.viewer.lines))
	}
	if got := p.viewer.lines[line-1]; got != "line4" {
		t.Errorf("the cursor's line = %q, want %q", got, "line4")
	}
}

func Test_CodesearchPureMessagePassing(t *testing.T) {
	root := panelTree(t)
	// Build panel with zero action seams (no OpenEditor, no SendComments, no Close)
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 24 },
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
	})
	p.Start("needle")

	// Enter opens the file in viewer and returns OpenFileCmd
	handled, cmd, ok := p.Update(tea.Key{Code: tea.KeyEnter})
	if !handled || !ok || cmd == nil {
		t.Fatal("expected Enter to return handled with tea.Cmd")
	}
	fileMsg, isFileMsg := cmd().(FileOpenedMsg)
	if !isFileMsg || fileMsg.Path == "" {
		t.Fatalf("expected FileOpenedMsg, got %#v", cmd())
	}

	// Add a comment
	p.Update(tea.Key{Code: 'c', Text: "c"})
	typeComment(t, p, "pure message comment")
	p.Update(tea.Key{Code: tea.KeyEsc}) // leaves comment editor, opens unsent screen

	if p.unsent == nil {
		t.Fatal("expected unsent comments screen to be active")
	}

	// In unsent screen, 'Y' sends comments. With zero seams, it emits SendCommentsMsg!
	handled, cmd, ok = p.Update(tea.Key{Code: 'Y', Text: "Y"})
	if !handled || !ok || cmd == nil {
		t.Fatal("expected Enter in unsent screen to return handled with tea.Cmd")
	}
	sendMsg, isSendMsg := cmd().(SendCommentsMsg)
	if !isSendMsg || !strings.Contains(sendMsg.Text, "pure message comment") {
		t.Fatalf("expected SendCommentsMsg containing comment, got %#v", cmd())
	}
}

