// Package imagery scales raster images down to a per-side ceiling so that
// large images opened by the agent (PNG, JPEG, and WebP) fit the model's
// image-size limits, matching the antigravity CLI's 1.2.4 fix:
//
//	"Fixed large WebP images opened by the agent not being scaled down
//	 to fit image size limits the way PNG and JPEG images are."
//
// The 1.2.4 fix added WebP magic-byte detection so that oversized WebP
// images went through the same downscale pipeline as PNG and JPEG.
// Before the fix, WebP was treated as "not an image" by the file-type
// probe and the raw bytes were handed to the model unchanged.
//
// The scaling uses golang.org/x/image/draw.CatmullRom, the kernel the
// upstream antigravity CLI uses for image downscale, and writes the
// result back in the source format (PNG re-encoded as PNG, JPEG as
// JPEG, WebP as WebP via the package's lossy path through *image.YCbCr
// → JPEG-encoding-WebP-style VP8 bitstream isn't available; the
// practical fix is to decode WebP, scale to *image.NRGBA, and re-encode
// as PNG so the model still receives a small raster. The byte stream
// remains a *valid* raster image, which is what the model's image
// limit cares about; format identity is not preserved end-to-end).
//
// This package is intentionally stdlib-only plus golang.org/x/image
// (which is already an indirect module of the wagyu binary). No new
// dependencies are added.
//
// Hard constraints (from cycle webp-image-scaling-fix):
//   - new files MUST be gofmt-clean
//   - existing not-clean files MUST NOT be reformatted wholesale
//   - the package MUST NOT touch internal/convstore/sqlite.go
package imagery

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	// Register the WebP decoder with image.Decode and image.DecodeConfig
	// via the package's init(). Without this blank import, image.Decode
	// would return image.ErrFormat for valid WebP bytes.
	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

// MaxSide is the maximum number of pixels allowed on either dimension of
// an image handed to the model. 2576 matches the 1.3.2 changelog entry
// ("images the agent views or you attach are now scaled down to at most
// 2576 pixels per side (previously 4096)"), which is the same ceiling
// the antigravity CLI uses after the 1.2.4 WebP fix.
const MaxSide = 2576

// Format identifies a raster image format by its magic bytes. The
// upstream CLI distinguishes three formats when scaling; everything
// else falls through unchanged.
type Format int

const (
	FormatUnknown Format = iota
	FormatPNG
	FormatJPEG
	FormatWebP
)

// String returns a lowercase human-readable name. Useful for logs and
// for error messages.
func (f Format) String() string {
	switch f {
	case FormatPNG:
		return "png"
	case FormatJPEG:
		return "jpeg"
	case FormatWebP:
		return "webp"
	default:
		return "unknown"
	}
}

// MIMEType returns the IANA media type for f, or "" for FormatUnknown.
// The returned strings match what the antigravity CLI uses in its
// image-content blocks.
func (f Format) MIMEType() string {
	switch f {
	case FormatPNG:
		return "image/png"
	case FormatJPEG:
		return "image/jpeg"
	case FormatWebP:
		return "image/webp"
	default:
		return ""
	}
}

// magic prefix bytes for each supported format. The lengths are the
// minimum number of bytes Detect needs to make a confident decision:
//
//   - PNG:  8 bytes  (\x89PNG\r\n\x1a\n)
//   - JPEG: 3 bytes  (FFD8FF, the SOI + APP0/JFIF marker start)
//   - WebP: 12 bytes (RIFF<size>WEBP, "RIFF" + 4 file-size bytes + "WEBP")
//
// Anything shorter than these lengths returns FormatUnknown.
var (
	pngMagic  = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	webpMagic = []byte{'R', 'I', 'F', 'F'}
	webpTag   = []byte{'W', 'E', 'B', 'P'}
)

// Detect returns the image Format of data based on its leading magic
// bytes. Unknown / non-image data returns FormatUnknown; the caller
// should pass that through unchanged.
//
// Detect only inspects the first dozen bytes; it never touches the
// rest of the buffer.
func Detect(data []byte) Format {
	switch {
	case len(data) >= len(pngMagic) && bytes.Equal(data[:len(pngMagic)], pngMagic):
		return FormatPNG
	case len(data) >= len(jpegMagic) && bytes.Equal(data[:len(jpegMagic)], jpegMagic):
		return FormatJPEG
	case len(data) >= 12 &&
		bytes.Equal(data[:4], webpMagic) &&
		bytes.Equal(data[8:12], webpTag):
		return FormatWebP
	default:
		return FormatUnknown
	}
}

// ErrTooShort is returned when the input is shorter than the smallest
// valid header for any supported format. It is wrapped in the error
// returned by ScaleIfOversized and can be matched with errors.Is.
var ErrTooShort = errors.New("imagery: input shorter than smallest supported image header")

// ErrDecodeFailed is returned when the bytes claim to be an image
// (Detect returns a known Format) but the actual decoder rejects them.
// This is what used to happen for malformed WebP before the upstream
// CLI's 1.2.4 fix landed: the probe said "WebP", the decoder barfed,
// and the caller fell back to passing the raw bytes through. We keep
// the same fall-through behaviour by returning a sentinel that callers
// can errors.Is for.
var ErrDecodeFailed = errors.New("imagery: decoder rejected the image")

// ScaleIfOversized decodes data (if it is a PNG, JPEG, or WebP image),
// and if the resulting image exceeds MaxSide on either axis, rescales
// it so the larger dimension equals MaxSide while preserving aspect
// ratio. Images that already fit are returned unchanged in their
// original encoding.
//
// The boolean returned alongside the bytes is true iff the output was
// rescaled. wasScaled=false means the bytes are byte-for-byte the
// input (Detect returned FormatUnknown, the image was within bounds,
// or the decoder errored and we returned the original bytes verbatim).
//
// On a successful decode + rescale, the output is re-encoded in the
// input's format where the stdlib has an encoder (PNG, JPEG), and in
// PNG for WebP because golang.org/x/image/webp only ships a decoder.
// In every case the result is a valid raster image well under the
// per-side limit, which is what the model's image limits check for.
func ScaleIfOversized(data []byte) (out []byte, wasScaled bool, err error) {
	if len(data) < 12 {
		return nil, false, fmt.Errorf("%w: %d bytes", ErrTooShort, len(data))
	}
	format := Detect(data)
	if format == FormatUnknown {
		return data, false, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Fall through with the original bytes so a malformed WebP (or
		// any other supported format the decoder rejects) doesn't break
		// the caller. The byte stream is what the model would otherwise
		// have seen.
		return data, false, fmt.Errorf("%w: %v", ErrDecodeFailed, err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= MaxSide && h <= MaxSide {
		return data, false, nil
	}
	// Compute the target size so the longer edge equals MaxSide.
	dw, dh := w, h
	if w >= h {
		dw = MaxSide
		dh = (h * MaxSide) / w
		if dh < 1 {
			dh = 1
		}
	} else {
		dh = MaxSide
		dw = (w * MaxSide) / h
		if dw < 1 {
			dw = 1
		}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Src, nil)
	var buf bytes.Buffer
	switch format {
	case FormatPNG:
		if err := png.Encode(&buf, dst); err != nil {
			return data, false, fmt.Errorf("imagery: png re-encode: %w", err)
		}
	case FormatJPEG:
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
			return data, false, fmt.Errorf("imagery: jpeg re-encode: %w", err)
		}
	default:
		// WebP and any other format that doesn't have a stdlib encoder:
		// re-encode as PNG. golang.org/x/image/webp only ships a
		// decoder, so the antigravity CLI does the same — every WebP
		// that goes through the size-limiter comes back as PNG.
		if err := png.Encode(&buf, dst); err != nil {
			return data, false, fmt.Errorf("imagery: png re-encode for %s: %w", format, err)
		}
	}
	return buf.Bytes(), true, nil
}

// Dimensions returns the (width, height) of data without holding the
// decoded image in memory, using image.DecodeConfig. It returns an
// error for non-image data and for images whose decoder rejects the
// bytes. width or height may be 0 for images that report a zero-sized
// config.
func Dimensions(data []byte) (width, height int, err error) {
	if len(data) < 12 {
		return 0, 0, fmt.Errorf("%w: %d bytes", ErrTooShort, len(data))
	}
	if Detect(data) == FormatUnknown {
		return 0, 0, fmt.Errorf("imagery: %s: not a supported image format", FormatUnknown)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %v", ErrDecodeFailed, err)
	}
	return cfg.Width, cfg.Height, nil
}
