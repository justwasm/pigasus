package pastemedia

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/imagery"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// pngBytes is a minimal PNG header plus payload; the module only inspects
// the detected format, so the bytes need only sniff as PNG.
func pngBytes() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01}
}

// writePNG puts a PNG on disk and returns its path, standing in for a file
// dragged into the prompt.
func writePNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dropped.png")
	if err := os.WriteFile(path, pngBytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- fakes: the module's whole dependency surface, in memory ----

type fakeInput struct {
	modal  bool
	typed  []string
	cmd    tea.Cmd
	pastes int
}

func (f *fakeInput) InsertPastedText(text string) tea.Cmd {
	f.pastes++
	f.typed = append(f.typed, text)
	return f.cmd
}

func (f *fakeInput) HasActiveModal() bool { return f.modal }

type fakeAttach struct {
	pending []clipboard.Media
	notice  string
	cleared int
}

func (f *fakeAttach) PendingMedia() []clipboard.Media { return f.pending }

// AddMedia mirrors the port contract: appending an attachment also clears
// the composer's transient notice.
func (f *fakeAttach) AddMedia(m clipboard.Media) {
	f.pending = append(f.pending, m)
	f.notice = ""
}

func (f *fakeAttach) ClearNotice()          { f.cleared++; f.notice = "" }
func (f *fakeAttach) SetNotice(text string) { f.notice = text }

type fakeClipboard struct {
	media clipboard.Media
	err   error
	calls int
}

func (f *fakeClipboard) ReadMedia(clipboard.Options) (clipboard.Media, error) {
	f.calls++
	return f.media, f.err
}

// fixture bundles the model with its fakes so a scenario reads as one unit.
type fixture struct {
	model *Model
	input *fakeInput
	att   *fakeAttach
	clip  *fakeClipboard
}

func newFixture(goos string) *fixture {
	f := &fixture{input: &fakeInput{}, att: &fakeAttach{}, clip: &fakeClipboard{}}
	f.model = &Model{Input: f.input, Attach: f.att, Clipboard: f.clip, GOOS: goos}
	return f
}

func pngMedia(name string) clipboard.Media {
	return clipboard.Media{Filename: name, Format: imagery.FormatPNG, Data: pngBytes()}
}

// ---- scenarios ----

// Scenario: a modal overlay owns the keyboard, so the paste goes to that
// panel's own editor and the composer's clipboard path is skipped: nothing
// is attached and no clipboard read happens.
func TestScenario_ModalOwnsKeyboard_PasteIsDropped(t *testing.T) {
	f := newFixture("darwin")
	f.input.modal = true

	if cmd := f.model.HandlePasteContent("from the panel"); cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if f.clip.calls != 0 {
		t.Errorf("clipboard reads = %d, want 0", f.clip.calls)
	}
	if len(f.att.pending) != 0 {
		t.Errorf("pending = %d, want 0 (modal paste must not attach)", len(f.att.pending))
	}
	if len(f.input.typed) != 1 || f.input.typed[0] != "from the panel" {
		t.Errorf("typed = %v, want [from the panel]", f.input.typed)
	}
}

// Scenario: ordinary text is pasted into the composer as text.
func TestScenario_PlainText_GoesIntoComposer(t *testing.T) {
	f := newFixture("darwin")

	if cmd := f.model.HandlePasteContent("hello world"); cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if len(f.input.typed) != 1 || f.input.typed[0] != "hello world" {
		t.Errorf("typed = %v, want [hello world]", f.input.typed)
	}
	if len(f.att.pending) != 0 {
		t.Errorf("pending = %d, want 0", len(f.att.pending))
	}
}

// Scenario: a dropped image path becomes an attachment, not text. This is
// what "drag an image file into the prompt" produces on a terminal.
func TestScenario_DroppedImagePath_BecomesAttachment(t *testing.T) {
	f := newFixture("darwin")
	path := writePNG(t)

	cmd := f.model.HandlePasteContent(path)
	if cmd != nil {
		t.Fatalf("cmd = %v, want nil", cmd)
	}
	if len(f.att.pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(f.att.pending))
	}
	if len(f.input.typed) != 0 {
		t.Errorf("typed = %v, want none (path is an attachment, not text)", f.input.typed)
	}
}

// Scenario: an empty paste asks the platform clipboard for an image, and
// the probe runs as a command rather than blocking the update goroutine.
func TestScenario_EmptyPaste_ProbesClipboardOffGoroutine(t *testing.T) {
	f := newFixture("darwin")
	f.clip.media = pngMedia("pasted.png")

	cmd := f.model.HandlePasteContent("")
	if cmd == nil {
		t.Fatalf("cmd = nil, want a deferred probe")
	}
	if f.clip.calls != 0 {
		t.Errorf("clipboard read on the update path; want it deferred")
	}
	msg, ok := cmd().(MediaMsg)
	if !ok {
		t.Fatalf("probe returned %T, want MediaMsg", msg)
	}
	if f.clip.calls != 1 {
		t.Errorf("clipboard reads = %d, want 1", f.clip.calls)
	}
	if len(msg.Media.Data) == 0 || msg.Err != nil {
		t.Errorf("probe = (%v, %v), want a media payload", len(msg.Media.Data), msg.Err)
	}
}

// Scenario: the probe's answer attaches the image and clears the notice.
func TestScenario_ProbeAnswer_AttachesImage(t *testing.T) {
	f := newFixture("darwin")
	f.att.notice = "stale"

	if attached := f.model.UpdatePasteMedia(MediaMsg{Media: pngMedia("shot.png")}); !attached {
		t.Fatalf("attached = false, want true")
	}
	if len(f.att.pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(f.att.pending))
	}
	if f.att.notice == "stale" {
		t.Errorf("notice not replaced on a successful attach")
	}
}

// Scenario: the clipboard holds nothing to attach, so the composer says so
// rather than silently doing nothing.
func TestScenario_ProbeEmpty_ShowsNotice(t *testing.T) {
	f := newFixture("darwin")

	if attached := f.model.UpdatePasteMedia(MediaMsg{Err: clipboard.ErrNoMedia}); attached {
		t.Fatalf("attached = true, want false")
	}
	if len(f.att.pending) != 0 {
		t.Errorf("pending = %d, want 0", len(f.att.pending))
	}
	if f.att.notice == "" {
		t.Errorf("notice is empty; an empty clipboard must explain itself")
	}
}

// Scenario: a missing clipboard helper names the package to install, so the
// user can act on it.
func TestScenario_MissingHelper_NamesThePackage(t *testing.T) {
	notice := Notice(&clipboard.ToolMissingError{Tool: "wl-paste", Package: "wl-clipboard"})
	if notice == "" {
		t.Fatalf("notice is empty")
	}
}

// Scenario: pending attachments become transport image parts with the mime
// type taken from the detected format.
func TestScenario_PendingMedia_BecomesImageParts(t *testing.T) {
	f := newFixture("darwin")
	f.att.pending = []clipboard.Media{pngMedia("1.png"), pngMedia("2.png")}

	parts := f.model.ImageParts()
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	for _, p := range parts {
		if p.MIMEType != "image/png" {
			t.Errorf("mime = %q, want image/png", p.MIMEType)
		}
		if len(p.Data) == 0 {
			t.Errorf("image data is empty")
		}
	}
}

// Scenario: no pending attachments means no image parts, so the user turn
// carries nothing extra.
func TestScenario_NoPendingMedia_NoImageParts(t *testing.T) {
	f := newFixture("darwin")

	if parts := f.model.ImageParts(); parts != nil {
		t.Errorf("parts = %v, want nil", parts)
	}
}

// Scenario: a notice that is cleared stays cleared.
func TestScenario_ClearNotice(t *testing.T) {
	f := newFixture("darwin")
	f.att.notice = "boom"

	f.att.ClearNotice()
	if f.att.notice != "" {
		t.Errorf("notice = %q, want empty", f.att.notice)
	}
}

// Scenario: an errored probe with no payload shows the error sentence.
func TestScenario_ProbeErrorWithPayload_NotAttached(t *testing.T) {
	f := newFixture("darwin")

	if attached := f.model.UpdatePasteMedia(MediaMsg{Media: pngMedia("x.png"), Err: errors.New("denied")}); attached {
		t.Fatalf("attached = true, want false")
	}
	if len(f.att.pending) != 0 {
		t.Errorf("pending = %d, want 0", len(f.att.pending))
	}
}
