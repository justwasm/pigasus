//go:build darwin

// Package clipboard media_darwin.go — the macOS clipboard's image.
//
// The two AppleScript sources below are the reference's own, recovered
// verbatim from the 1.4.9 binary's string table:
//
//	"try
//		set fileURL to the clipboard as «class furl»
//		return POSIX path of fileURL
//	on error
//		return ""
//	end try"
//
//	"set tempFile to %q
//	try
//		set imageData to the clipboard as «class %s»
//		set f to open for access POSIX file tempFile with write permission
//		set eof f to 0
//		write imageData to f
//		close access f
//		return tempFile
//	on error
//		return ""
//	end try"
//
// The first script reads a *file* off the clipboard — that is what a
// dragged or copied file is — and the second reads the image bytes into
// a temporary file, whose path the caller chooses (the %q). The class
// code (%s) is the AppleScript type of the image; the reference's own
// string table carries "PNGf", "JPEG", "GIFf" and "TIFF" next to "sips",
// the macOS image converter it uses for the formats it cannot read
// directly.
package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// darwinFileURLScript reads a file URL off the clipboard.
const darwinFileURLScript = "try\n" +
	"\tset fileURL to the clipboard as «class furl»\n" +
	"\treturn POSIX path of fileURL\n" +
	"on error\n" +
	"\treturn \"\"\n" +
	"end try"

// darwinImageScript writes the clipboard's image into a temporary file
// and returns that file's path.
const darwinImageScript = "set tempFile to %q\n" +
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

// darwinImageClasses are the AppleScript clipboard classes the reference
// tries, in the order its own string table carries them.
var darwinImageClasses = []string{"PNGf", "JPEG", "GIFf", "TIFF"}

// darwinClassExt is the file extension each class is written with, so
// that a reader (and sips) recognises the payload.
var darwinClassExt = map[string]string{
	"PNGf":  ".png",
	"JPEG":  ".jpg",
	"GIFf":  ".gif",
	"TIFF":  ".tif",
	"PNG":   ".png",
	"JPEGf": ".jpg",
}

// readMedia reads the macOS clipboard.
func readMedia(opts Options) (Media, error) {
	// A file on the clipboard comes first: that is a dragged or copied
	// file, and the reference attaches it.
	path, err := darwinClipboardFileURL()
	if err == nil && path != "" {
		media, ok := mediaFromFile(path)
		if ok {
			return media, nil
		}
		return Media{}, fmt.Errorf("Clipboard file URL read error for %s: %v", path, ErrNoMedia)
	}
	// Otherwise the clipboard holds image bytes.
	for _, class := range darwinImageClasses {
		media, ok := darwinClipboardImage(class)
		if ok {
			media.Filename = pastedName(media.Format, opts.Counter)
			return media, nil
		}
	}
	return Media{}, ErrNoMedia
}

// darwinClipboardFileURL runs the reference's file-URL script and
// returns the POSIX path it printed (empty when the clipboard holds no
// file).
func darwinClipboardFileURL() (string, error) {
	out, err := execRunner("osascript", "-e", darwinFileURLScript)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// darwinClipboardImage runs the reference's image script for one class
// and reads the file it wrote.
func darwinClipboardImage(class string) (Media, bool) {
	ext := darwinClassExt[class]
	if ext == "" {
		ext = ".png"
	}
	tmp, err := os.CreateTemp("", "agy-clipboard-*"+ext)
	if err != nil {
		return Media{}, false
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)

	script := fmt.Sprintf(darwinImageScript, path, class)
	out, err := execRunner("osascript", "-e", script)
	if err != nil {
		return Media{}, false
	}
	written := strings.TrimSpace(string(out))
	if written == "" {
		return Media{}, false
	}
	if media, ok := mediaFromFile(written); ok {
		return media, true
	}
	// A format the pipeline cannot decode (TIFF, GIF) is converted with
	// sips, the macOS image tool the reference names beside the class
	// codes.
	return darwinConvertWithSips(written, filepath.Base(path))
}

// darwinConvertWithSips converts an image the pipeline cannot decode
// into a PNG with sips.
func darwinConvertWithSips(in, name string) (Media, bool) {
	if _, err := lookPath("sips"); err != nil {
		return Media{}, false
	}
	out := in + ".png"
	defer os.Remove(out)
	if _, err := execRunner("sips", "-s", "format", "png", in, "--out", out); err != nil {
		return Media{}, false
	}
	media, ok := mediaFromFile(out)
	if !ok {
		return Media{}, false
	}
	media.Filename = name
	return media, true
}
