//go:build darwin

package clipboard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/imagery"
)

// TestDarwinScriptsAreTheReferences pins the two AppleScript sources to
// the reference's own wording, recovered verbatim from the 1.4.9
// binary's string table.
func TestDarwinScriptsAreTheReferences(t *testing.T) {
	wantFileURL := "try\n" +
		"\tset fileURL to the clipboard as «class furl»\n" +
		"\treturn POSIX path of fileURL\n" +
		"on error\n" +
		"\treturn \"\"\n" +
		"end try"
	if darwinFileURLScript != wantFileURL {
		t.Errorf("file-URL script:\n got %q\nwant %q", darwinFileURLScript, wantFileURL)
	}
	wantImage := "set tempFile to %q\n" +
		"try\n" +
		"\tset imageData to the clipboard as «class %s»\n" +
		"\tset f to open for access POSIX file tempFile with write permission\n" +
		"\tset eof f to 0\n" +
		"\twrite imageData to f\n" +
		"\tclose access f\n" +
		"\treturn tempFile\n" +
		"on error\n" +
		"\treturn \"\"\n" +
		"end try"
	if darwinImageScript != wantImage {
		t.Errorf("image script:\n got %q\nwant %q", darwinImageScript, wantImage)
	}
}

// TestDarwinClassesAreTheReferences pins the AppleScript class codes the
// reference tries, in the order its string table carries them.
func TestDarwinClassesAreTheReferences(t *testing.T) {
	want := []string{"PNGf", "JPEG", "GIFf", "TIFF"}
	if len(darwinImageClasses) != len(want) {
		t.Fatalf("classes = %v, want %v", darwinImageClasses, want)
	}
	for i := range want {
		if darwinImageClasses[i] != want[i] {
			t.Errorf("class %d = %q, want %q", i, darwinImageClasses[i], want[i])
		}
	}
}

// fakeOsascript installs an execRunner that answers the two scripts the
// way a real clipboard would.
func fakeOsascript(t *testing.T, fileURL string, image []byte) {
	t.Helper()
	origExec := execRunner
	t.Cleanup(func() { execRunner = origExec })
	execRunner = func(name string, args ...string) ([]byte, error) {
		if name != "osascript" {
			return nil, errors.New("unexpected command: " + name)
		}
		script := args[len(args)-1]
		if strings.Contains(script, "class furl") {
			return []byte(fileURL), nil
		}
		const marker = "set tempFile to "
		idx := strings.Index(script, marker)
		if idx < 0 {
			return nil, errors.New("unexpected script: " + script)
		}
		rest := script[idx+len(marker):]
		end := strings.Index(rest, "\n")
		path := strings.Trim(rest[:end], "\"")
		if err := os.WriteFile(path, image, 0o600); err != nil {
			return nil, err
		}
		return []byte(path), nil
	}
}

// TestReadMediaDarwin_ReadsTheClipboardImage covers the macOS reader:
// the clipboard holds image bytes, the script writes them into a
// temporary file, and the file becomes the attachment.
func TestReadMediaDarwin_ReadsTheClipboardImage(t *testing.T) {
	fakeOsascript(t, "", tinyPNG(t))
	media, err := ReadMedia(DefaultOptions())
	if err != nil {
		t.Fatalf("ReadMedia: %v", err)
	}
	if len(media.Data) == 0 {
		t.Fatalf("media has no data")
	}
	if media.Format != imagery.FormatPNG {
		t.Errorf("format = %v, want png", media.Format)
	}
}

// TestReadMediaDarwin_ReadsAFileOnTheClipboard covers the other half: the
// clipboard holds a *file* (a dragged or copied image), and the
// reference's file-URL script resolves it.
func TestReadMediaDarwin_ReadsAFileOnTheClipboard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, tinyPNG(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	fakeOsascript(t, path, tinyPNG(t))
	media, err := ReadMedia(DefaultOptions())
	if err != nil {
		t.Fatalf("ReadMedia: %v", err)
	}
	if media.Path != path {
		t.Errorf("path = %q, want %q", media.Path, path)
	}
	if media.Filename != "shot.png" {
		t.Errorf("filename = %q, want %q", media.Filename, "shot.png")
	}
}

// TestReadMediaDarwin_EmptyClipboard pins the reference's sentence for a
// clipboard with nothing to attach.
func TestReadMediaDarwin_EmptyClipboard(t *testing.T) {
	fakeOsascript(t, "", nil)
	_, err := ReadMedia(DefaultOptions())
	if err == nil {
		t.Fatalf("ReadMedia on an empty clipboard = nil error")
	}
	if err.Error() != "No image or video found in the clipboard" {
		t.Errorf("error = %q, want the reference's sentence", err.Error())
	}
}
