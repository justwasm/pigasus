// Package tools — image_probe.go: the image-header probe behind the
// 1.4.9 "corrupt image" fix.
//
// Implements the AGY 1.4.9 release note:
//
//	"Fixed conversations failing every later model request with
//	 `INVALID_MEDIA` after the agent viewed a corrupt image or one with
//	 text in front of its header, such as a screenshot captured with
//	 `adb exec-out screencap -p > screenshot.png` that also captured a
//	 warning; such images are now rejected with an error that says where
//	 the real image header starts, so the agent can fix the command."
//
// A screenshot that captured a warning on top of the PNG bytes is still
// an image — the header sits a few bytes into the file. The reference
// rejects the file anyway (attaching it poisons every later request in
// the conversation) and names the offset of the real header, so the
// agent can fix the command that produced it.
//
// The reference's own wording, recovered from the 1.4.9 binary:
//
//	invalid %s: header found at byte offset %d after leading non-image bytes
//
// This is the same shape as the WebM probe in webm.go: the check runs at
// read time, so the agent sees the failure on the read_file / view_file
// call instead of a broken conversation three turns later.
package tools

import (
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/imagery"
)

// imageExtensions are the raster formats the tools attach to the model.
// The set matches the formats the imagery package can decode.
var imageExtensions = map[string]bool{
	".png":  true,
	".jpg":  true,
	".jpeg": true,
	".webp": true,
}

// IsImagePath reports whether path names an image file (case-insensitive
// extension). Anything else is left to the ordinary read path.
func IsImagePath(path string) bool {
	return imageExtensions[strings.ToLower(filepath.Ext(path))]
}

// probeImage rejects an image file the model must not see.
//
// The one case the 1.4.9 fix names is a header that is not at byte 0:
// the file was captured with something printed in front of the image
// bytes. It is rejected with the offset of the real header.
//
// A file with no image header at all is left alone: the reference's
// error for that case ("a corrupt image") does not name an offset, and
// the clone does not attach images to the model, so there is nothing to
// protect the conversation from.
//
// A file whose header is at byte 0 passes: the probe is a guard, not a
// decoder.
func probeImage(path string, data []byte) error {
	offset, format := imagery.HeaderOffset(data)
	if format == imagery.FormatUnknown || offset <= 0 {
		return nil
	}
	return &imagery.HeaderOffsetError{Subject: path, Offset: offset, Format: format}
}
