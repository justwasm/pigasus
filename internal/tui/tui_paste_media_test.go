package tui

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// pastePNG returns the bytes of a one-pixel PNG.
func pastePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 9, G: 8, B: 7, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// pasteRunMode builds a run mode for the paste tests.
func pasteRunMode(t *testing.T) *runMode {
	t.Helper()
	m := newRunMode(&fakeClient{}, "u@t", "Gemni 3.8 Flash (Medium)", "", t.TempDir(), false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// TestPasteOfAnImagePathAttachesTheImage covers agy 1.4.9's "dragging
// image files into the prompt, which attaches them instead of inserting
// their paths": the drag arrives as the file's path, and the path is not
// inserted into the composer.
func TestPasteOfAnImagePathAttachesTheImage(t *testing.T) {
	m := pasteRunMode(t)
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, pastePNG(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	m.handlePasteContent(path)
	if len(m.composer.pendingMedia) != 1 {
		t.Fatalf("pendingMedia = %d entries, want 1", len(m.composer.pendingMedia))
	}
	if m.composer.pendingMedia[0].Filename != "shot.png" {
		t.Errorf("filename = %q, want %q", m.composer.pendingMedia[0].Filename, "shot.png")
	}
	if m.composer.input.Value() != "" {
		t.Errorf("composer = %q, want empty (the path is not inserted)", m.composer.input.Value())
	}
}

// TestPasteOfTextStaysText pins that ordinary text is still pasted.
func TestPasteOfTextStaysText(t *testing.T) {
	m := pasteRunMode(t)
	m.handlePasteContent("hello")
	if len(m.composer.pendingMedia) != 0 {
		t.Fatalf("pendingMedia = %d entries, want 0", len(m.composer.pendingMedia))
	}
	if m.composer.input.Value() != "hello" {
		t.Errorf("composer = %q, want %q", m.composer.input.Value(), "hello")
	}
}

// TestPasteWithNothingDeliveredProbesTheClipboard pins that a paste that
// delivered nothing asks the platform for an image.
func TestPasteWithNothingDeliveredProbesTheClipboard(t *testing.T) {
	m := pasteRunMode(t)
	cmd := m.handlePasteContent("")
	if cmd == nil {
		t.Fatalf("handlePasteContent(\"\") = nil command, want the probe")
	}
}

// TestPasteMediaMsgAttachesTheImage covers the probe's answer.
func TestPasteMediaMsgAttachesTheImage(t *testing.T) {
	m := pasteRunMode(t)
	mod, _ := m.updatePasteMedia(pasteMediaMsg{media: clipboard.Media{
		Data:     pastePNG(t),
		Format:   1,
		Filename: "pasted-image-1.png",
	}})
	rm, ok := mod.(*runMode)
	if !ok {
		t.Fatalf("model is %T, want *runMode", mod)
	}
	if len(rm.composer.pendingMedia) != 1 {
		t.Fatalf("pendingMedia = %d entries, want 1", len(rm.composer.pendingMedia))
	}
	if rm.viewport.inputNotice != "" {
		t.Errorf("notice = %q, want empty", rm.viewport.inputNotice)
	}
}

// TestPasteMediaMsgSaysTheReferencesSentence covers the upstream 1.4.9
// note: "the CLI's paste key now says `No image or video found in the
// clipboard` when there is nothing to attach".
func TestPasteMediaMsgSaysTheReferencesSentence(t *testing.T) {
	m := pasteRunMode(t)
	mod, _ := m.updatePasteMedia(pasteMediaMsg{err: clipboard.ErrNoMedia})
	rm := mod.(*runMode)
	want := "No image or video found in the clipboard"
	if rm.viewport.inputNotice != want {
		t.Errorf("notice = %q, want %q", rm.viewport.inputNotice, want)
	}
}

// TestPasteMediaMsgNamesTheMissingTool covers the other half of the
// sentence: a missing clipboard tool is named.
func TestPasteMediaMsgNamesTheMissingTool(t *testing.T) {
	m := pasteRunMode(t)
	mod, _ := m.updatePasteMedia(pasteMediaMsg{err: &clipboard.ToolMissingError{
		Tool:    "wl-paste",
		Package: "wl-clipboard",
	}})
	rm := mod.(*runMode)
	if rm.viewport.inputNotice == "" {
		t.Fatalf("notice is empty")
	}
	if !bytes.Contains([]byte(rm.viewport.inputNotice), []byte("wl-clipboard")) {
		t.Errorf("notice %q does not name the package", rm.viewport.inputNotice)
	}
}

// TestAttachedImageRidesWithTheUserTurn pins that the attachment reaches
// the model as an image part of the user message.
func TestAttachedImageRidesWithTheUserTurn(t *testing.T) {
	m := pasteRunMode(t)
	m.attachMedia(clipboard.Media{Data: pastePNG(t), Format: 1, Filename: "pasted-image-1.png"})
	m.appendUserTurn("what is this?")
	if len(m.engine.session) != 1 {
		t.Fatalf("session = %d messages, want 1", len(m.engine.session))
	}
	msg := m.engine.session[0]
	if len(msg.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(msg.Images))
	}
	if msg.Images[0].MIMEType != "image/png" {
		t.Errorf("mime = %q, want %q", msg.Images[0].MIMEType, "image/png")
	}
	if len(msg.Images[0].Data) == 0 {
		t.Errorf("image data is empty")
	}
	if len(m.composer.pendingMedia) != 0 {
		t.Errorf("pendingMedia = %d entries after the turn, want 0", len(m.composer.pendingMedia))
	}
}

// TestClipboardReadMediaSeamIsReplaceable keeps the probe testable: the
// seam is what the TUI calls, so a test can answer with a fixture.
func TestClipboardReadMediaSeamIsReplaceable(t *testing.T) {
	orig := clipboardReadMedia
	t.Cleanup(func() { clipboardReadMedia = orig })
	called := false
	clipboardReadMedia = func(opts clipboard.Options) (clipboard.Media, error) {
		called = true
		return clipboard.Media{}, errors.New("boom")
	}
	m := pasteRunMode(t)
	cmd := m.probeClipboardMedia()
	if cmd == nil {
		t.Fatalf("probeClipboardMedia = nil")
	}
	msg := cmd()
	_, ok := msg.(pasteMediaMsg)
	if !ok {
		t.Fatalf("message is %T, want pasteMediaMsg", msg)
	}
	if !called {
		t.Errorf("the seam was not called")
	}
}

// TestPasteMultipleImagesViaDragAndDrop covers dragging multiple files simultaneously.
func TestPasteMultipleImagesViaDragAndDrop(t *testing.T) {
	m := pasteRunMode(t)
	dir := t.TempDir()
	p1 := filepath.Join(dir, "one.png")
	p2 := filepath.Join(dir, "two.png")
	if err := os.WriteFile(p1, pastePNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, pastePNG(t), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := m.handlePasteContent(`"` + p1 + `" "` + p2 + `"`)
	if cmd != nil {
		t.Errorf("handlePasteContent returned cmd %v, want nil", cmd)
	}
	if len(m.composer.pendingMedia) != 2 {
		t.Fatalf("pendingMedia = %d entries, want 2", len(m.composer.pendingMedia))
	}
	if m.composer.pendingMedia[0].Filename != "one.png" || m.composer.pendingMedia[1].Filename != "two.png" {
		t.Errorf("attached filenames = %q, %q", m.composer.pendingMedia[0].Filename, m.composer.pendingMedia[1].Filename)
	}
	if m.composer.input.Value() != "" {
		t.Errorf("composer input = %q, want empty", m.composer.input.Value())
	}
}

// TestCtrlOTogglesMediaExpanded verifies ctrl+o key toggling.
func TestCtrlOTogglesMediaExpanded(t *testing.T) {
	m := pasteRunMode(t)
	m.attachMedia(clipboard.Media{Filename: "shot.png", Format: 1, Data: pastePNG(t)})

	if m.composer.mediaExpanded {
		t.Fatalf("initially expanded = true, want false")
	}

	// Send ctrl+o
	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !m.composer.mediaExpanded {
		t.Errorf("after ctrl+o: mediaExpanded = false, want true")
	}

	// Send ctrl+o again
	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if m.composer.mediaExpanded {
		t.Errorf("after second ctrl+o: mediaExpanded = true, want false")
	}
}

// TestBackspaceRemovesLastPendingMediaWhenInputEmpty covers backspace deleting attachments.
func TestBackspaceRemovesLastPendingMediaWhenInputEmpty(t *testing.T) {
	m := pasteRunMode(t)
	m.attachMedia(clipboard.Media{Filename: "1.png", Format: 1, Data: pastePNG(t)})
	m.attachMedia(clipboard.Media{Filename: "2.png", Format: 1, Data: pastePNG(t)})

	if len(m.composer.pendingMedia) != 2 {
		t.Fatalf("pendingMedia = %d, want 2", len(m.composer.pendingMedia))
	}

	// Backspace when input is empty
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m.composer.pendingMedia) != 1 || m.composer.pendingMedia[0].Filename != "1.png" {
		t.Errorf("after backspace: pendingMedia = %v, want [1.png]", m.composer.pendingMedia)
	}

	// Backspace again -> removes remaining
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if len(m.composer.pendingMedia) != 0 {
		t.Errorf("after second backspace: pendingMedia = %v, want empty", m.composer.pendingMedia)
	}
}
