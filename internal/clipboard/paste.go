// Package clipboard bridges a clipboard paste event and the
// image-scaling pipeline used by the model adapter.
//
// The 1.2.13 changelog entry it closes:
//
//	"Fixed pasted WebP images from the clipboard being rejected even
//	 when the user has WebP image support enabled; the image now passes
//	 through the same scaling pipeline as PNG and JPEG."
//
// Before the fix, pasted WebP bytes were rejected with
// "unsupported image format" even when the user's terminal reported
// WebP support; the only paths through were PNG and JPEG. After the
// fix, WebP is gated only on the WebPEnabled setting (default true;
// the upstream CLI's default) and the bytes are handed to
// imagery.ScaleIfOversized for the same decode → resize → re-encode
// treatment PNG and JPEG have always received.
//
// This package is the byte-level glue. The terminal side (read pbpaste,
// parse OSC 52, react to kitty's image paste, etc.) is an ImageSource;
// the model side (build the attachment block, decide where the bytes
// land in the prompt) is the caller's job. AcceptPaste is exposed so
// the caller can plug in any byte source without going through
// ReadImage, and so unit tests don't need to mock the OS clipboard.
//
// Hard constraints from the gap brief:
//   - new Go files gofmt-clean (gofmt -l internal/clipboard/ empty)
//   - pre-existing not-clean files MUST NOT be reformatted wholesale
//   - internal/convstore/sqlite.go untouched
//   - at minimum 5 unit tests covering the brief's named cases:
//     WebP accepted, PNG still works, oversized WebP scaled, non-image
//     rejected, empty paste fails gracefully
package clipboard

import (
	"errors"
	"fmt"

	"github.com/justwasm/pigasus/imagery"
)

// Sentinel errors returned by ReadImage and AcceptPaste. All of them
// are designed to be matched with errors.Is — they are wrapped at the
// call site so the original error chain survives.
var (
	// ErrWebPDisabled is returned when the input is a WebP image and
	// Options.WebPEnabled is false. PNG and JPEG inputs are *not* gated
	// (only WebP), so this is the only setting that ever blocks a paste.
	// Wrapped at the call site, so callers can errors.Is for it.
	ErrWebPDisabled = errors.New("clipboard: WebP image support is disabled")

	// ErrEmptyPayload is returned when the caller passes a zero-length
	// buffer. This is the "no clipboard data fails gracefully" path:
	// the caller wires a paste command, the bytes are empty (nothing
	// on the clipboard), the helper reports ErrEmptyPayload rather
	// than crashing on a nil/empty slice.
	ErrEmptyPayload = errors.New("clipboard: empty payload")

	// ErrSourceUnavailable is returned by ReadImage when the source
	// cannot be read (nil source, OS clipboard not reachable, etc.).
	// Callers should fall back to text paste in this case.
	ErrSourceUnavailable = errors.New("clipboard: source not available")
)

// PasteImage is the result of inspecting a clipboard payload.
//
// Format is the detected raster format, or imagery.FormatUnknown when
// the input is not one of the supported rasters. Data is the (possibly
// scaled) image bytes; it is nil when Format is FormatUnknown. The
// Filename is a suggested name based on Format and an internal counter,
// suitable for showing in the prompt's "[Attached: <name>] <prompt
// text]" line.
//
// Scaled is true iff the bytes were resized by imagery.ScaleIfOversized
// (false means the image already fit imagery.MaxSide on both axes, or
// the decoder rejected the bytes and the originals were returned
// verbatim).
type PasteImage struct {
	Format   imagery.Format
	Data     []byte
	Filename string
	Scaled   bool
}

// IsEmpty reports whether the paste produced no image data.
// Callers that want to distinguish "not an image" (e.g. plain text
// pasted into a textarea) from "decode failed" (a malformed WebP) can
// match the returned error against imagery.ErrDecodeFailed.
func (p PasteImage) IsEmpty() bool {
	return p.Format == imagery.FormatUnknown
}

// Options configure ReadImage and AcceptPaste.
//
// WebPEnabled mirrors the upstream CLI's `imageSupport.webp` setting.
// The clone ships with the WebP path open (true) so that pasted WebP
// images go through the resize pipeline, matching the 1.2.13 default.
//
// Counter is incremented for each accepted image so the suggested
// Filename is unique within a session ("pasted-image-1.png",
// "pasted-image-2.webp", ...). The counter is independent of how many
// pastes were rejected — each *accepted* paste is what the user sees,
// so the counter keys off the accept path.
type Options struct {
	WebPEnabled bool
	Counter     int
}

// DefaultOptions returns the package defaults: WebP enabled, counter
// starting at 1. The settings manager is expected to override
// WebPEnabled when the user has explicitly turned off WebP image
// support in /config.
func DefaultOptions() Options {
	return Options{WebPEnabled: true, Counter: 1}
}

// ImageSource is the read side of a clipboard. Implementations are
// responsible for negotiating with the platform clipboard:
//
//   - macOS: shell out to pbpaste, possibly -Prefer for image MIME types
//   - Linux: wl-paste -t image/* or xclip -selection clipboard -t
//   - Windows: powershell Get-Clipboard -Format Image
//   - bubbletea OSC 52: read the last OSC 52 escape that arrived
//   - bubbletea kitty graphics protocol: assemble the payload from the
//     "T" command chunks
//
// The interface is exposed so tests can swap a stub source that returns
// a fixed byte slice without touching the system clipboard, and so
// the real source can be wired in by cmd/wagyu without the package
// taking an OS-specific dependency.
type ImageSource interface {
	Read() ([]byte, error)
}

// bytesSource is the trivial in-memory ImageSource.
type bytesSource struct{ data []byte }

func (b bytesSource) Read() ([]byte, error) { return b.data, nil }

// BytesSource returns an ImageSource that yields data verbatim. Used
// by tests and by callers that already have the payload in memory (a
// piped stdin, a paste handler that captured the bytes, the bubbletea
// OSC 52 escape payload).
func BytesSource(data []byte) ImageSource {
	return bytesSource{data: data}
}

// ReadImage inspects the bytes from src and runs them through the
// imagery pipeline. It returns PasteImage{} with Format==FormatUnknown
// when src yields a payload that is not a recognised raster — callers
// that need to tell "not an image" apart from "decode failed" should
// match the returned error against imagery.ErrDecodeFailed.
//
// Errors:
//   - ErrSourceUnavailable when src.Read fails or src is nil
//   - ErrEmptyPayload when src yields zero bytes
//   - ErrWebPDisabled when the payload is WebP and Options.WebPEnabled
//     is false (PNG and JPEG are not gated)
//   - imagery.ErrDecodeFailed when the payload is a recognised format
//     that the decoder rejected
//   - imagery.ErrTooShort when the input is shorter than the smallest
//     supported image header (8 bytes for PNG, 12 for WebP)
func ReadImage(src ImageSource, opts Options) (PasteImage, error) {
	if src == nil {
		return PasteImage{}, fmt.Errorf("%w: nil source", ErrSourceUnavailable)
	}
	raw, err := src.Read()
	if err != nil {
		return PasteImage{}, fmt.Errorf("%w: %v", ErrSourceUnavailable, err)
	}
	return AcceptPaste(raw, opts)
}

// AcceptPaste is the pure, source-free half of ReadImage. It runs the
// bytes through imagery.Detect and imagery.ScaleIfOversized, applying
// the Options.WebPEnabled gate only for WebP input. PNG and JPEG are
// always accepted; WebP is rejected with ErrWebPDisabled when
// Options.WebPEnabled is false.
//
// The return contract mirrors ReadImage; the function never panics on
// empty/garbage input.
func AcceptPaste(data []byte, opts Options) (PasteImage, error) {
	if len(data) == 0 {
		return PasteImage{}, fmt.Errorf("%w", ErrEmptyPayload)
	}
	format := imagery.Detect(data)
	if format == imagery.FormatUnknown {
		// The payload does not start with an image header. It may still
		// hold an image behind some leading bytes — a screenshot that
		// captured a warning on top of the PNG bytes, the shape
		// `adb exec-out screencap -p > screenshot.png` produces when the
		// device also prints something.
		//
		// 【一手】 agy 1.4.9, the vendor's own release notes: "such
		// images are now rejected with an error that says where the real
		// image header starts, so the agent can fix the command."
		if offset, f := imagery.HeaderOffset(data); f != imagery.FormatUnknown {
			return PasteImage{Format: f}, &imagery.HeaderOffsetError{
				Subject: "clipboard image",
				Offset:  offset,
				Format:  f,
			}
		}
		// Non-image bytes (a typed text payload, a short buffer, a
		// random binary blob) fall through with a populated sentinel
		// so callers can distinguish "not an image" from "decode
		// failed" via the returned error.
		return PasteImage{Format: imagery.FormatUnknown}, nil
	}
	if format == imagery.FormatWebP && !opts.WebPEnabled {
		return PasteImage{Format: format}, fmt.Errorf("%w", ErrWebPDisabled)
	}
	scaled, wasScaled, err := imagery.ScaleIfOversized(data)
	if err != nil {
		// imagery returns ErrDecodeFailed for "recognised format,
		// decoder rejected" and ErrTooShort for sub-header-sized
		// input. Both wrap the underlying error so the caller's
		// errors.Is chain survives.
		if errors.Is(err, imagery.ErrDecodeFailed) {
			return PasteImage{Format: format}, fmt.Errorf("%w: %v", imagery.ErrDecodeFailed, err)
		}
		if errors.Is(err, imagery.ErrTooShort) {
			return PasteImage{Format: format}, fmt.Errorf("%w: %v", imagery.ErrTooShort, err)
		}
		return PasteImage{Format: format}, err
	}
	return PasteImage{
		Format:   format,
		Data:     scaled,
		Filename: suggestedFilename(format, opts.Counter),
		Scaled:   wasScaled,
	}, nil
}

// suggestedFilename builds a human-readable name from the format and a
// 1-based counter ("pasted-image-1.png", "pasted-image-2.webp", ...).
// The counter is clamped to ≥1 so a misconfigured caller never sees
// "pasted-image-0.png".
func suggestedFilename(format imagery.Format, counter int) string {
	if counter < 1 {
		counter = 1
	}
	return fmt.Sprintf("pasted-image-%d.%s", counter, format)
}
