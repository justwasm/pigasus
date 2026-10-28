package imagery

import (
	"bytes"
	"fmt"
)

// HeaderOffset returns the byte offset of the image header in data and
// the format of the image that header starts.
//
// 【一手】 agy 1.4.9, the vendor's own release notes: "Fixed
// conversations failing every later model request with `INVALID_MEDIA`
// after the agent viewed a corrupt image or one with text in front of
// its header, such as a screenshot captured with `adb exec-out
// screencap -p > screenshot.png` that also captured a warning; such
// images are now rejected with an error that says where the real image
// header starts, so the agent can fix the command."
//
// The reference's own error text, recovered from the 1.4.9 binary
// (the string sits next to the function name
// `media.findLeadingImageMagicOffset`):
//
//	invalid %s: header found at byte offset %d after leading non-image bytes
//
// A payload whose header is not at byte 0 still holds an image — the
// leading bytes are whatever the capturing command printed first — so
// the probe reports where the real header starts instead of guessing.
//
// The offset is -1 when the payload holds no image header at all.
func HeaderOffset(data []byte) (int, Format) {
	if f := Detect(data); f != FormatUnknown {
		return 0, f
	}
	best, format := -1, FormatUnknown
	if i := bytes.Index(data, pngMagic); i >= 0 {
		best, format = i, FormatPNG
	}
	if i := bytes.Index(data, jpegMagic); i >= 0 && (best < 0 || i < best) {
		best, format = i, FormatJPEG
	}
	if i := indexWebP(data); i >= 0 && (best < 0 || i < best) {
		best, format = i, FormatWebP
	}
	return best, format
}

// indexWebP returns the offset of the first RIFF/WEBP header in data,
// or -1 when there is none. The WebP header is "RIFF", four size bytes
// and "WEBP", so the magic cannot be matched as one run of bytes.
func indexWebP(data []byte) int {
	for off := 0; off+12 <= len(data); {
		i := bytes.Index(data[off:], webpMagic)
		if i < 0 {
			return -1
		}
		i += off
		if i+12 <= len(data) && bytes.Equal(data[i+8:i+12], webpTag) {
			return i
		}
		off = i + 1
	}
	return -1
}

// HeaderOffsetError reports that an image payload does not start with
// its header: the header was found further in, after leading non-image
// bytes.
//
// The message is the reference's own wording — "invalid %s: header
// found at byte offset %d after leading non-image bytes" — so the agent
// can see where the real header starts and fix the command that
// produced the file.
type HeaderOffsetError struct {
	// Subject names what was invalid: the file path, or a short name
	// for a payload that has no path.
	Subject string
	// Offset is the byte offset of the real image header.
	Offset int
	// Format is the format of the image the header starts.
	Format Format
}

func (e *HeaderOffsetError) Error() string {
	return fmt.Sprintf("invalid %s: header found at byte offset %d after leading non-image bytes", e.Subject, e.Offset)
}
