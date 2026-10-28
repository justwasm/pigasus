//go:build linux

// Package clipboard media_linux.go — the Linux clipboard's image.
//
// The upstream 1.4.9 note says the paste key "names the clipboard tool
// to install, such as `wl-clipboard` on Wayland, when one is missing",
// so this reader picks the tool the session's display server needs:
//
//   - Wayland (WAYLAND_DISPLAY): wl-paste, from the wl-clipboard
//     package,
//   - X11 (DISPLAY): xclip.
//
// Without either variable there is no display to talk to. Over SSH that
// is the case the reference names in its own words: "Can't read your
// clipboard over SSH: connect with ssh -X (and install xclip on this
// host) to paste images".
package clipboard

import (
	"os"
	"strings"
)

// readMedia reads the Linux clipboard.
func readMedia(opts Options) (Media, error) {
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return readMediaWayland(opts)
	case os.Getenv("DISPLAY") != "":
		return readMediaX11(opts)
	case onSSH():
		return Media{}, ErrSSHClipboard
	default:
		return Media{}, ErrNoMedia
	}
}

// readMediaWayland reads the clipboard through wl-paste.
func readMediaWayland(opts Options) (Media, error) {
	if _, err := lookPath("wl-paste"); err != nil {
		return Media{}, &ToolMissingError{Tool: "wl-paste", Package: "wl-clipboard"}
	}
	listing, err := execRunner("wl-paste", "--list-types")
	if err != nil {
		return Media{}, ErrNoMedia
	}
	mime := preferredImageType(string(listing))
	if mime == "" {
		return Media{}, ErrNoMedia
	}
	data, err := execRunner("wl-paste", "--type", mime)
	if err != nil || len(data) == 0 {
		return Media{}, ErrNoMedia
	}
	media, ok := mediaFromBytes(data, "")
	if !ok {
		return Media{}, ErrNoMedia
	}
	media.Filename = pastedName(media.Format, opts.Counter)
	return media, nil
}

// readMediaX11 reads the clipboard through xclip.
func readMediaX11(opts Options) (Media, error) {
	if _, err := lookPath("xclip"); err != nil {
		return Media{}, &ToolMissingError{Tool: "xclip", Package: "xclip"}
	}
	// TARGETS lists what the selection holds; an image type in it is
	// the one to ask for.
	mime := ""
	if targets, err := execRunner("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o"); err == nil {
		mime = firstImageType(string(targets))
	}
	if mime == "" {
		mime = "image/png"
	}
	data, err := execRunner("xclip", "-selection", "clipboard", "-t", mime, "-o")
	if err != nil || len(data) == 0 {
		return Media{}, ErrNoMedia
	}
	media, ok := mediaFromBytes(data, "")
	if !ok {
		return Media{}, ErrNoMedia
	}
	media.Filename = pastedName(media.Format, opts.Counter)
	return media, nil
}

// waylandTypeNames is the set of image types the Wayland reader prefers,
// best first. wl-paste can hand back any of them.
var waylandTypeNames = []string{"image/png", "image/jpeg", "image/webp"}

// preferredImageType returns the first type of the listing that is
// image/*, preferring the formats the pipeline decodes natively.
func preferredImageType(listing string) string {
	available := map[string]bool{}
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			available[line] = true
		}
	}
	for _, want := range waylandTypeNames {
		if available[want] {
			return want
		}
	}
	return firstImageType(listing)
}
