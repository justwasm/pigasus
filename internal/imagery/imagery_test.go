package imagery

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/exp/golden"
)

// readFile is a tiny helper that fails the test on read errors so the
// body of each test stays focused on the assertion.
func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", name, err)
	}
	return data
}

// makeSolidPNG returns a PNG-encoded image of the requested size where
// every pixel has the given color. Used to generate oversized PNG and
// JPEG fixtures without depending on testdata files for those formats.
func makeSolidPNG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode %dx%d PNG: %v", w, h, err)
	}
	return buf.Bytes()
}

// makeSolidJPEG returns a JPEG-encoded image of the requested size
// where every pixel has the given color.
func makeSolidJPEG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode %dx%d JPEG: %v", w, h, err)
	}
	return buf.Bytes()
}

// ─── 1. WebP detection via magic bytes ───────────────────────────────────────

// TestDetect_WebPMagicBytes exercises every branch of Detect for the
// WebP magic check. RIFF....WEBP is the only thing that triggers the
// WebP branch; RIFF with a different tag, plain WEBP without RIFF, and
// short buffers must not.
func TestDetect_WebPMagicBytes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want Format
	}{
		{
			name: "valid WebP: RIFF + size + WEBP",
			data: []byte{'R', 'I', 'F', 'F', 0x10, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '},
			want: FormatWebP,
		},
		{
			name: "WebP header with zero size still valid",
			data: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'},
			want: FormatWebP,
		},
		{
			name: "RIFF with non-WEBP form type (WAVE) is not WebP",
			data: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'},
			want: FormatUnknown,
		},
		{
			name: "WEBP without RIFF container is not WebP",
			data: []byte{'W', 'E', 'B', 'P', 0, 0, 0, 0},
			want: FormatUnknown,
		},
		{
			name: "empty buffer is not WebP",
			data: []byte{},
			want: FormatUnknown,
		},
		{
			name: "buffer shorter than 12 bytes is not WebP",
			data: []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B'},
			want: FormatUnknown,
		},
		{
			name: "PNG magic is detected as PNG, not WebP",
			data: []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
			want: FormatPNG,
		},
		{
			name: "JPEG magic is detected as JPEG, not WebP",
			data: []byte{0xFF, 0xD8, 0xFF, 0xE0},
			want: FormatJPEG,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.data); got != tc.want {
				t.Errorf("Detect(%x) = %s, want %s", tc.data, got, tc.want)
			}
		})
	}
}

// ─── 2. Oversized WebP gets scaled down ──────────────────────────────────────

// TestScale_WebP_OversizedScaledDown verifies that an oversized WebP
// (the exact bug the 1.2.4 fix is about) is detected as WebP, scaled
// down, and re-encoded. The result must be smaller in dimension than
// the input and must remain a valid raster image under the per-side
// limit.
func TestScale_WebP_OversizedScaledDown(t *testing.T) {
	data := readFile(t, "large.webp")
	if got := Detect(data); got != FormatWebP {
		t.Fatalf("Detect(large.webp) = %s, want FormatWebP", got)
	}
	w0, h0, err := Dimensions(data)
	if err != nil {
		t.Fatalf("Dimensions(large.webp): %v", err)
	}
	if w0 <= MaxSide && h0 <= MaxSide {
		t.Fatalf("large.webp fixture not actually oversized: %dx%d (MaxSide=%d)", w0, h0, MaxSide)
	}
	out, wasScaled, err := ScaleIfOversized(data)
	if err != nil {
		t.Fatalf("ScaleIfOversized: %v", err)
	}
	if !wasScaled {
		t.Fatalf("wasScaled = false; oversized WebP should have been scaled")
	}
	w1, h1, err := Dimensions(out)
	if err != nil {
		t.Fatalf("Dimensions(output): %v", err)
	}
	if w1 > MaxSide || h1 > MaxSide {
		t.Errorf("output %dx%d exceeds MaxSide=%d", w1, h1, MaxSide)
	}
	if w1 >= w0 || h1 >= h0 {
		t.Errorf("output %dx%d is not smaller than input %dx%d", w1, h1, w0, h0)
	}
	// Aspect ratio must be preserved (within 1 px of an integer scale).
	if got, want := w0*h1, h0*w1; abs(got-want) > h0 {
		t.Errorf("aspect ratio not preserved: w0*h1=%d h0*w1=%d diff=%d", got, want, abs(got-want))
	}
}

// ─── 3. Small WebP is returned unchanged ─────────────────────────────────────

// TestScale_WebP_SmallUnchanged verifies the fast path: a WebP that
// already fits MaxSide on both axes must be returned byte-for-byte
// with wasScaled=false. This is the same fast path PNG and JPEG used
// before the 1.2.4 fix; WebP used to bypass it because Detect said
// "unknown".
func TestScale_WebP_SmallUnchanged(t *testing.T) {
	data := readFile(t, "small.webp")
	if got := Detect(data); got != FormatWebP {
		t.Fatalf("Detect(small.webp) = %s, want FormatWebP", got)
	}
	w, h, err := Dimensions(data)
	if err != nil {
		t.Fatalf("Dimensions(small.webp): %v", err)
	}
	if w > MaxSide || h > MaxSide {
		t.Skipf("small.webp fixture is %dx%d, not actually small; regenerate", w, h)
	}
	out, wasScaled, err := ScaleIfOversized(data)
	if err != nil {
		t.Fatalf("ScaleIfOversized: %v", err)
	}
	if wasScaled {
		t.Errorf("wasScaled = true on a small WebP; expected false")
	}
	if !bytes.Equal(data, out) {
		t.Errorf("small WebP was modified: input=%d bytes, output=%d bytes", len(data), len(out))
	}
}

// ─── 4. PNG still works ──────────────────────────────────────────────────────

// TestScale_PNG_OversizedScaledDown makes sure the existing PNG path
// (which was already correct before the 1.2.4 fix) keeps working
// after the package is wired in. The fixture is generated in-test so
// it never goes stale and never depends on tooling outside the stdlib.
func TestScale_PNG_OversizedScaledDown(t *testing.T) {
	data := makeSolidPNG(t, 4000, 3000, color.RGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xFF})
	if got := Detect(data); got != FormatPNG {
		t.Fatalf("Detect(synth PNG) = %s, want FormatPNG", got)
	}
	out, wasScaled, err := ScaleIfOversized(data)
	if err != nil {
		t.Fatalf("ScaleIfOversized: %v", err)
	}
	if !wasScaled {
		t.Fatalf("wasScaled = false; oversized PNG should have been scaled")
	}
	if got := Detect(out); got != FormatPNG {
		t.Errorf("Detect(scaled PNG) = %s, want FormatPNG", got)
	}
	w1, h1, err := Dimensions(out)
	if err != nil {
		t.Fatalf("Dimensions(scaled PNG): %v", err)
	}
	if w1 > MaxSide || h1 > MaxSide {
		t.Errorf("scaled PNG %dx%d exceeds MaxSide=%d", w1, h1, MaxSide)
	}
	if w1 == 4000 || h1 == 3000 {
		t.Errorf("scaled PNG %dx%d was not actually scaled down from 4000x3000", w1, h1)
	}
}

// TestScale_PNG_SmallUnchanged is the PNG mirror of the WebP "small
// passes through" test.
func TestScale_PNG_SmallUnchanged(t *testing.T) {
	data := makeSolidPNG(t, 64, 48, color.RGBA{R: 0xFF, A: 0xFF})
	out, wasScaled, err := ScaleIfOversized(data)
	if err != nil {
		t.Fatalf("ScaleIfOversized: %v", err)
	}
	if wasScaled {
		t.Errorf("wasScaled = true on a 64x48 PNG")
	}
	golden.RequireEqual(t, out)
}

// ─── 5. JPEG still works ─────────────────────────────────────────────────────

// TestScale_JPEG_OversizedScaledDown mirrors the PNG test for JPEG.
// The JPEG path existed before the 1.2.4 fix; the fix didn't touch it
// but the regression coverage is cheap.
func TestScale_JPEG_OversizedScaledDown(t *testing.T) {
	data := makeSolidJPEG(t, 5000, 2000, color.RGBA{R: 0x40, G: 0x80, B: 0xC0, A: 0xFF})
	if got := Detect(data); got != FormatJPEG {
		t.Fatalf("Detect(synth JPEG) = %s, want FormatJPEG", got)
	}
	out, wasScaled, err := ScaleIfOversized(data)
	if err != nil {
		t.Fatalf("ScaleIfOversized: %v", err)
	}
	if !wasScaled {
		t.Fatalf("wasScaled = false; oversized JPEG should have been scaled")
	}
	if got := Detect(out); got != FormatJPEG {
		t.Errorf("Detect(scaled JPEG) = %s, want FormatJPEG", got)
	}
	w1, h1, err := Dimensions(out)
	if err != nil {
		t.Fatalf("Dimensions(scaled JPEG): %v", err)
	}
	if w1 > MaxSide || h1 > MaxSide {
		t.Errorf("scaled JPEG %dx%d exceeds MaxSide=%d", w1, h1, MaxSide)
	}
}

// TestScale_JPEG_SmallUnchanged is the JPEG mirror of the small-passthrough test.
func TestScale_JPEG_SmallUnchanged(t *testing.T) {
	data := makeSolidJPEG(t, 32, 32, color.RGBA{A: 0xFF})
	out, wasScaled, err := ScaleIfOversized(data)
	if err != nil {
		t.Fatalf("ScaleIfOversized: %v", err)
	}
	if wasScaled {
		t.Errorf("wasScaled = true on a 32x32 JPEG")
	}
	if !bytes.Equal(data, out) {
		t.Errorf("small JPEG was modified")
	}
}

// ─── supplementary: error sentinels and non-image passthrough ───────────────

// TestScale_NonImagePassesThrough checks that bytes that aren't a
// recognized image format (a text file, a binary blob, an empty buffer)
// come back unchanged with wasScaled=false and an error that wraps
// ErrTooShort for the empty case.
func TestScale_NonImagePassesThrough(t *testing.T) {
	t.Run("plain text", func(t *testing.T) {
		data := []byte("hello, world")
		out, wasScaled, err := ScaleIfOversized(data)
		if err != nil {
			t.Errorf("non-image error = %v, want nil", err)
		}
		if wasScaled {
			t.Errorf("wasScaled = true on plain text")
		}
		if !bytes.Equal(data, out) {
			t.Errorf("plain text was modified")
		}
	})
	t.Run("empty buffer", func(t *testing.T) {
		out, wasScaled, err := ScaleIfOversized(nil)
		if !errors.Is(err, ErrTooShort) {
			t.Errorf("empty buffer error = %v, want ErrTooShort", err)
		}
		if wasScaled {
			t.Errorf("wasScaled = true on empty buffer")
		}
		if out != nil {
			t.Errorf("empty buffer out = %v, want nil", out)
		}
	})
	t.Run("looks like WebP but isn't", func(t *testing.T) {
		// RIFF header + WEBP tag, but with the chunk size lying about
		// how many bytes follow. The decoder must reject, the caller
		// must get the bytes back unchanged and an ErrDecodeFailed
		// they can errors.Is for.
		bad := []byte{'R', 'I', 'F', 'F', 0xFF, 0xFF, 0xFF, 0x7F, 'W', 'E', 'B', 'P',
			'V', 'P', '8', ' ', 0xE0, 0x00, 0x00, 0x00,
		}
		out, wasScaled, err := ScaleIfOversized(bad)
		if !errors.Is(err, ErrDecodeFailed) {
			t.Errorf("bad WebP error = %v, want ErrDecodeFailed", err)
		}
		if wasScaled {
			t.Errorf("wasScaled = true on a bad WebP")
		}
		if !bytes.Equal(bad, out) {
			t.Errorf("bad WebP bytes were modified")
		}
	})
}

// ─── supplementary: Format / MIMEType coverage ───────────────────────────────

// TestFormatMIMEType locks the String() and MIMEType() mapping. The
// FormatUnknown branch is included because callers may log "unknown"
// and we want to know if that string ever drifts.
func TestFormatMIMEType(t *testing.T) {
	cases := []struct {
		f        Format
		name     string
		mimeType string
	}{
		{FormatPNG, "png", "image/png"},
		{FormatJPEG, "jpeg", "image/jpeg"},
		{FormatWebP, "webp", "image/webp"},
		{FormatUnknown, "unknown", ""},
	}
	for _, tc := range cases {
		if got := tc.f.String(); got != tc.name {
			t.Errorf("Format(%d).String() = %q, want %q", tc.f, got, tc.name)
		}
		if got := tc.f.MIMEType(); got != tc.mimeType {
			t.Errorf("Format(%d).MIMEType() = %q, want %q", tc.f, got, tc.mimeType)
		}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// abs is int-abs; the stdlib has math.Abs but that takes float64.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
