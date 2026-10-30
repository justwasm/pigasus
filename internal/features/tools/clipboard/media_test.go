package clipboard

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/imagery"
)

// tinyPNG writes a one-pixel PNG and returns its bytes.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// TestMediaFromPath_ReadsAnImageFile covers the "dragging image files
// into the prompt" half of agy 1.4.9's image pasting: a terminal cannot
// hand a file to a program, so the drag arrives as the file's path — and
// the reference attaches the image instead of inserting the path.
func TestMediaFromPath_ReadsAnImageFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, tinyPNG(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	media, ok := MediaFromPath(path)
	if !ok {
		t.Fatalf("MediaFromPath(%q) = not ok, want ok", path)
	}
	if media.Filename != "shot.png" {
		t.Errorf("filename = %q, want %q", media.Filename, "shot.png")
	}
	if media.Path != path {
		t.Errorf("path = %q, want %q", media.Path, path)
	}
	if media.Format != imagery.FormatPNG {
		t.Errorf("format = %v, want png", media.Format)
	}
	if len(media.Data) == 0 {
		t.Errorf("data is empty")
	}
}

// TestMediaFromPath_UnescapesTheTerminalsEscapes covers the spelling a
// terminal produces when the dragged path contains a space.
func TestMediaFromPath_UnescapesTheTerminalsEscapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "my shot.png")
	if err := os.WriteFile(path, tinyPNG(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	escaped := strings.ReplaceAll(path, " ", `\ `)
	media, ok := MediaFromPath(escaped)
	if !ok {
		t.Fatalf("MediaFromPath(%q) = not ok, want ok", escaped)
	}
	if media.Path != path {
		t.Errorf("path = %q, want %q", media.Path, path)
	}
}

// TestMediaFromPath_QuotedPath covers a terminal that quotes the path.
func TestMediaFromPath_QuotedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, tinyPNG(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	media, ok := MediaFromPath(`"` + path + `"`)
	if !ok {
		t.Fatalf("MediaFromPath(quoted) = not ok, want ok")
	}
	if media.Path != path {
		t.Errorf("path = %q, want %q", media.Path, path)
	}
}

// TestMediaFromPath_RejectsWhatIsNotAnImage pins the rule that only an
// image path becomes an attachment; everything else stays text.
func TestMediaFromPath_RejectsWhatIsNotAnImage(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(text, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	cases := []string{
		"",
		"hello world",
		text,
		filepath.Join(dir, "missing.png"),
		"line one\nline two",
	}
	for _, c := range cases {
		if _, ok := MediaFromPath(c); ok {
			t.Errorf("MediaFromPath(%q) = ok, want not ok", c)
		}
	}
}

// TestFirstImageType pins the pick out of a clipboard type listing.
func TestFirstImageType(t *testing.T) {
	listing := "text/plain;charset=utf-8\nimage/png\nimage/jpeg\n"
	if got := firstImageType(listing); got != "image/png" {
		t.Errorf("firstImageType = %q, want %q", got, "image/png")
	}
	if got := firstImageType("text/plain\n"); got != "" {
		t.Errorf("firstImageType(text only) = %q, want empty", got)
	}
}

// TestToolMissingErrorNamesTheTool pins the sentence the upstream 1.4.9
// note describes: the paste key "names the clipboard tool to install,
// such as wl-clipboard on Wayland, when one is missing".
func TestToolMissingErrorNamesTheTool(t *testing.T) {
	err := &ToolMissingError{Tool: "wl-paste", Package: "wl-clipboard"}
	msg := err.Error()
	if !strings.Contains(msg, "wl-paste") {
		t.Errorf("message %q does not name the tool", msg)
	}
	if !strings.Contains(msg, "wl-clipboard") {
		t.Errorf("message %q does not name the package", msg)
	}
}

// TestNoMediaMessageIsTheReferences pins the reference's own sentence.
func TestNoMediaMessageIsTheReferences(t *testing.T) {
	want := "No image or video found in the clipboard"
	if ErrNoMedia.Error() != want {
		t.Errorf("ErrNoMedia = %q, want %q", ErrNoMedia.Error(), want)
	}
	wantSSH := "Can't read your clipboard over SSH: connect with ssh -X " +
		"(and install xclip on this host) to paste images"
	if ErrSSHClipboard.Error() != wantSSH {
		t.Errorf("ErrSSHClipboard = %q, want %q", ErrSSHClipboard.Error(), wantSSH)
	}
}

// TestMediaFromBytesRejectsNonImages pins that the reader only accepts
// what the imagery pipeline understands.
func TestMediaFromBytesRejectsNonImages(t *testing.T) {
	if _, ok := mediaFromBytes([]byte("not an image"), ""); ok {
		t.Errorf("mediaFromBytes(text) = ok, want not ok")
	}
	if _, ok := mediaFromBytes(nil, ""); ok {
		t.Errorf("mediaFromBytes(nil) = ok, want not ok")
	}
	media, ok := mediaFromBytes(tinyPNG(t), "")
	if !ok {
		t.Fatalf("mediaFromBytes(png) = not ok, want ok")
	}
	if media.Filename == "" {
		t.Errorf("filename is empty")
	}
}

// TestOnSSHChecksTheSession pins the SSH detection the reader uses to
// answer with the reference's ssh advice.
func TestOnSSHChecksTheSession(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	if onSSH() {
		t.Errorf("onSSH() = true, want false")
	}
	t.Setenv("SSH_TTY", "/dev/ttys001")
	if !onSSH() {
		t.Errorf("onSSH() = false, want true")
	}
}

// TestErrorsIsUnwrappable keeps the sentinel errors usable with
// errors.Is, the way the rest of the package's callers expect.
func TestErrorsIsUnwrappable(t *testing.T) {
	if !errors.Is(ErrNoMedia, ErrNoMedia) {
		t.Errorf("ErrNoMedia does not match itself")
	}
	var toolErr *ToolMissingError
	if !errors.As(error(&ToolMissingError{Tool: "xclip", Package: "xclip"}), &toolErr) {
		t.Errorf("errors.As failed")
	}
}
