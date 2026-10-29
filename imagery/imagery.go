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
// # The 1.4.10 pipeline
//
// 【一手】 agy 1.4.10, the vendor's own release notes: "Fixed
// conversations with many or large images, such as screenshots or pasted
// images, being rejected for exceeding the model's request size limit:
// images are now capped at 2000 pixels per side and large PNGs are sent
// as JPEGs when that is much smaller, and an image saved as JPEG but
// labeled PNG, such as a pasted image or browser screenshot, is no
// longer rejected for a mismatched type."
//
// Three rules follow from that note, and Prepare implements all three:
//
//   - the pixel cap is 2000 per side (MaxSide);
//   - a large PNG leaves as a JPEG when the JPEG is much smaller
//     (LargePNGBytes, JPEGMuchSmaller);
//   - the media type follows the **bytes**, never the name the bytes
//     arrived under — Prepare reports the format it actually produced,
//     so a JPEG saved as ".png" is sent as "image/jpeg".
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
	"image/color"
	"image/jpeg"
	"image/png"

	// Register the WebP decoder with image.Decode and image.DecodeConfig
	// via the package's init(). Without this blank import, image.Decode
	// would return image.ErrFormat for valid WebP bytes.
	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

// MaxSide is the maximum number of pixels allowed on either dimension of
// an image handed to the model.
//
// 【一手】 agy 1.4.10, the vendor's own release notes: "Fixed
// conversations with many or large images, such as screenshots or pasted
// images, being rejected for exceeding the model's request size limit:
// images are now capped at 2000 pixels per side …".
//
// The ceiling was 2576 before that — the value the 1.3.2 notes put in
// place ("images the agent views or you attach are now scaled down to at
// most 2576 pixels per side (previously 4096)") — so the cap moves
// *down* here.
const MaxSide = 2000

// LargePNGBytes is the size above which a PNG is a candidate for the
// JPEG conversion in Prepare.
//
// 【推测】 The upstream note says "large PNGs are sent as JPEGs when that
// is much smaller" without naming a size; the reference's own threshold
// is not recoverable from the release notes. 64 KiB is the boundary the
// clone uses: below it a PNG is left alone (a small icon gains nothing
// from a lossy round-trip), above it the JPEG encoding is offered and
// wins when it is *much* smaller — see JPEGMuchSmaller.
const LargePNGBytes = 64 << 10

// JPEGMuchSmaller is the ratio that decides whether a JPEG encoding
// replaces a PNG one: the JPEG is used when it is at most half the
// size of the PNG.
//
// 【推测】 The upstream note says "large PNGs are sent as JPEGs when
// that is much smaller" — "much" is the ratio this constant pins.
const JPEGMuchSmaller = 2

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

// targetScaledDimensions returns the size an image of w×h is scaled to
// so that its larger side is MaxSide, preserving the aspect ratio.
func targetScaledDimensions(w, h int) (dw, dh int) {
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
	return dw, dh
}

// Prepare returns the bytes to hand to the model, and the format those
// bytes are **actually** in.
//
// It is the full pipeline an image goes through on its way into a
// request:
//
//   - an image wider or taller than MaxSide is scaled down to fit;
//   - a PNG is written as a JPEG when the JPEG encoding is *much*
//     smaller (see JPEGMuchSmaller) — the 1.4.10 note's "large PNGs are
//     sent as JPEGs when that is much smaller";
//   - everything else is handed over untouched.
//
// The format is reported because it follows the **bytes**, not the
// name the bytes arrived under: a JPEG saved as ".png" is sent as
// "image/jpeg". The 1.4.10 note calls that out — "an image saved as
// JPEG but labeled PNG, such as a pasted image or browser screenshot,
// is no longer rejected for a mismatched type".
//
// changed is true iff the bytes were re-encoded (resized, converted, or
// both).
func Prepare(data []byte) (out []byte, format Format, changed bool, err error) {
	return prepare(data, true)
}

// prepare is Prepare's body, with the JPEG conversion behind a flag:
// ScaleIfOversized scales and nothing else.
func prepare(data []byte, convert bool) ([]byte, Format, bool, error) {
	if len(data) < 12 {
		return nil, FormatUnknown, false, fmt.Errorf("%w: %d bytes", ErrTooShort, len(data))
	}
	format := Detect(data)
	if format == FormatUnknown {
		return data, FormatUnknown, false, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, format, false, fmt.Errorf("%w: %v", ErrDecodeFailed, err)
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	oversized := w > MaxSide || h > MaxSide
	// A PNG that fits the pixel cap is still a candidate when it is
	// large enough for the JPEG encoding to pay for itself: the
	// request-size limit the 1.4.10 note names is a *size* limit, and
	// a 1900-pixel screenshot is small in pixels and huge in bytes.
	candidate := oversized || (convert && format == FormatPNG && len(data) >= LargePNGBytes)
	if !candidate {
		return data, format, false, nil
	}
	var dst image.Image = img
	if oversized {
		dw, dh := targetScaledDimensions(w, h)
		scaled := image.NewNRGBA(image.Rect(0, 0, dw, dh))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, bounds, draw.Src, nil)
		dst = scaled
	}
	encoded, outFormat, err := encodeForModel(dst, format, convert)
	if err != nil {
		return data, format, false, err
	}
	return encoded, outFormat, true, nil
}

// encodeForModel writes img in the encoding the model should receive.
//
// A PNG is offered to both encoders and the JPEG wins only when it is
// *much* smaller — a PNG of a flat drawing is smaller as a PNG, and
// re-encoding it as a JPEG would only lose quality.
func encodeForModel(img image.Image, format Format, convert bool) ([]byte, Format, error) {
	switch format {
	case FormatPNG:
		pngBytes, err := encodePNG(img)
		if err != nil {
			return nil, FormatUnknown, err
		}
		if !convert {
			return pngBytes, FormatPNG, nil
		}
		jpegBytes, jerr := encodeJPEG(img)
		if jerr == nil && len(jpegBytes)*JPEGMuchSmaller <= len(pngBytes) {
			return jpegBytes, FormatJPEG, nil
		}
		return pngBytes, FormatPNG, nil
	case FormatJPEG:
		encoded, err := encodeJPEG(img)
		if err != nil {
			return nil, FormatUnknown, err
		}
		return encoded, FormatJPEG, nil
	default:
		// WebP: golang.org/x/image/webp only ships a decoder, so the
		// bytes leave as a PNG — still a valid raster, which is what
		// the model's limit cares about.
		encoded, err := encodePNG(img)
		if err != nil {
			return nil, FormatUnknown, err
		}
		return encoded, FormatPNG, nil
	}
}

// encodePNG writes img as a PNG.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("imagery: png re-encode: %w", err)
	}
	return buf.Bytes(), nil
}

// encodeJPEG writes img as a JPEG.
//
// The picture is flattened onto a white background first: JPEG has no
// alpha channel, and a transparent pixel would otherwise be written as
// whatever colour the decoder happened to produce — a black box where
// the picture is see-through.
func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenOnWhite(img), &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("imagery: jpeg re-encode: %w", err)
	}
	return buf.Bytes(), nil
}

// flattenOnWhite draws img over an opaque white background.
func flattenOnWhite(img image.Image) *image.NRGBA {
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}

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
//
// Prepare is the fuller pipeline: it scales *and* writes a large PNG
// as a JPEG when that is much smaller. This function scales only, so
// the output format always matches the input's.
func ScaleIfOversized(data []byte) (out []byte, wasScaled bool, err error) {
	out, _, wasScaled, err = prepare(data, false)
	return out, wasScaled, err
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
