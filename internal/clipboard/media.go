// Package clipboard media.go — the platform clipboard's image payload.
//
// This is the read half of agy 1.4.9's image pasting:
//
//	"Added image pasting through your terminal's own paste shortcut, such
//	 as Cmd+V or Ctrl+Shift+V with only an image copied, and by dragging
//	 image files into the prompt, which attaches them instead of
//	 inserting their paths; image paste now also works over SSH with X11
//	 or Wayland forwarding, and the CLI's paste key now says
//	 `No image or video found in the clipboard` when there is nothing to
//	 attach and names the clipboard tool to install, such as
//	 `wl-clipboard` on Wayland, when one is missing."
//	(internal/cli/testdata/changelog_upstream_1.4.9.md)
//
// The byte-level half already existed in this package: AcceptPaste takes
// the bytes of a clipboard payload and runs them through the imagery
// pipeline. What was missing was the *source* — nothing ever asked the
// platform for the image — and the *rule* that decides what a paste
// means:
//
//   - an image on the clipboard becomes an attachment,
//   - a path to an image file becomes an attachment too (that is what
//     "dragging image files into the prompt" produces: a terminal cannot
//     deliver a file, so it inserts the path),
//   - anything else stays text.
//
// The messages are the reference's own wording, recovered verbatim from
// the 1.4.9 binary's string table:
//
//	"No image or video found in the clipboard"
//	"Can't read your clipboard over SSH: connect with ssh -X (and install
//	 xclip on this host) to paste images"
//	"Clipboard file URL read error for %s: %v"
//
// The capitalisation is deliberate: these are user-facing sentences, not
// Go error strings, and the reference prints them verbatim.
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/imagery"
)

// Media is one image read off the platform clipboard (or off a file the
// user dropped into the prompt).
type Media struct {
	// Data is the image bytes, already through the imagery scaling
	// pipeline.
	Data []byte
	// Format is the detected raster format.
	Format imagery.Format
	// Filename is the name to show for the attachment.
	Filename string
	// Path is the file the image came from, when it came from a file.
	// It is empty for a clipboard payload.
	Path string
}

// ErrNoMedia is the reference's own wording for a clipboard that holds
// no image and no video.
var ErrNoMedia = errors.New("No image or video found in the clipboard")

// ErrSSHClipboard is the reference's own wording for an SSH session
// whose clipboard cannot be reached — no X11 forwarding, so neither
// xclip nor wl-paste has anything to talk to.
var ErrSSHClipboard = errors.New("Can't read your clipboard over SSH: " +
	"connect with ssh -X (and install xclip on this host) to paste images")

// ToolMissingError reports a clipboard helper that is not installed.
//
// The upstream 1.4.9 note says the paste key "names the clipboard tool
// to install, such as `wl-clipboard` on Wayland, when one is missing", so
// the error carries the tool it ran and the package that provides it.
type ToolMissingError struct {
	// Tool is the binary that was not found ("wl-paste").
	Tool string
	// Package is the package that ships it ("wl-clipboard").
	Package string
}

// Error renders the missing-tool sentence.
func (e *ToolMissingError) Error() string {
	return fmt.Sprintf("%s is not installed; install %s to paste images from the clipboard",
		e.Tool, e.Package)
}

// execRunner runs a command and returns its stdout. It is the seam the
// platform readers go through so the tests can answer with a fixture
// instead of touching the real clipboard.
var execRunner = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// lookPath is the seam for the "is the tool installed?" question.
var lookPath = exec.LookPath

// ReadMedia reads the platform clipboard's image payload.
//
// It returns ErrNoMedia when the clipboard holds no image, ErrSSHClipboard
// when the session is over SSH without X11 forwarding, and a
// *ToolMissingError when the clipboard helper is not installed.
func ReadMedia(opts Options) (Media, error) {
	return readMedia(opts)
}

// MediaFromPath reads an image file the user dropped into the prompt.
//
// A terminal cannot hand a file to a program: dragging an image into the
// prompt inserts its *path* as text (quoted and backslash-escaped, the
// way Terminal.app and iTerm2 do it). The upstream 1.4.9 note says such
// a paste "attaches them instead of inserting their paths", so the clone
// recognises the path and attaches the image.
//
// ok is false when the text is not a path to a readable image — the
// caller then treats the paste as ordinary text.
func MediaFromPath(text string) (Media, bool) {
	raw := strings.TrimSpace(text)
	if raw == "" || strings.ContainsAny(raw, "\n\r") {
		return Media{}, false
	}
	for _, candidate := range droppedPathCandidates(raw) {
		if media, ok := mediaFromFile(candidate); ok {
			return media, true
		}
	}
	return Media{}, false
}

// droppedPathCandidates lists the spellings of a dropped path worth
// trying: the text as it arrived, the text with the terminal's
// backslash escapes removed, and the path of a `file://` URL.
func droppedPathCandidates(raw string) []string {
	out := []string{strings.Trim(raw, `"'`)}
	if unescaped := unescapeDroppedPath(raw); unescaped != raw {
		out = append(out, strings.Trim(unescaped, `"'`))
	}
	if strings.HasPrefix(raw, "file://") {
		out = append(out, strings.TrimPrefix(raw, "file://"))
	}
	return out
}

// unescapeDroppedPath removes the backslash escapes a terminal puts in
// front of the special characters of a dropped path ("my\ file.png" for
// "my file.png").
func unescapeDroppedPath(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// mediaFromFile reads an image file and returns it as a Media.
func mediaFromFile(path string) (Media, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return Media{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Media{}, false
	}
	media, ok := mediaFromBytes(data, filepath.Base(path))
	if !ok {
		return Media{}, false
	}
	media.Path = path
	return media, true
}

// mediaFromBytes turns raw bytes into a Media, scaling them through the
// imagery pipeline on the way. ok is false when the bytes are not an
// image the pipeline understands.
func mediaFromBytes(data []byte, name string) (Media, bool) {
	if len(data) == 0 {
		return Media{}, false
	}
	format := imagery.Detect(data)
	if format == imagery.FormatUnknown {
		return Media{}, false
	}
	scaled, _, err := imagery.ScaleIfOversized(data)
	if err != nil {
		// A header the pipeline recognises but cannot decode is still an
		// image as far as the attachment is concerned; the bytes go out
		// unchanged rather than being dropped.
		scaled = data
	}
	if name == "" {
		name = "pasted-image.png"
	}
	return Media{Data: scaled, Format: format, Filename: name}, true
}

// pastedName is the name a pasted image carries: "pasted-image-1.png".
// It is the name the package's own AcceptPaste suggests, so both halves
// of the paste pipeline name an image the same way.
func pastedName(format imagery.Format, counter int) string {
	if counter < 1 {
		counter = 1
	}
	return fmt.Sprintf("pasted-image-%d.%s", counter, format.String())
}

// firstImageType picks the first image/* MIME type out of a clipboard
// type listing. The listing is the newline-separated output of
// `wl-paste --list-types` or of an X11 TARGETS read.
func firstImageType(listing string) string {
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "image/") {
			return line
		}
	}
	return ""
}

// onSSH reports whether the session runs over SSH. The reference's
// clipboard advice ("connect with ssh -X") only makes sense there.
func onSSH() bool {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}
