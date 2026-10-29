package clipboard

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

	"github.com/justwasm/pigasus/imagery"
)

// readFixture is a tiny helper that fails the test on read errors so
// the body of each test stays focused on the assertion. The WebP
// fixtures live in imagery/testdata because that package is
// the one that decodes them; clipboard borrows them via the
// directory at the package root. The path climbs two levels because
// imagery now lives at the top of the tree, beside internal/ — it
// used to be internal/imagery, one level down.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "imagery", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture ../../imagery/testdata/%s: %v", name, err)
	}
	return data
}

// makeSolidPNG returns a PNG-encoded image of the requested size
// where every pixel has the given color.
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

// failingSource is an ImageSource stub that returns the configured error.
type failingSource struct{ err error }

func (f failingSource) Read() ([]byte, error) { return nil, f.err }

// ─── brief case 1: WebP from clipboard is accepted ──────────────────────────

// TestAcceptPaste_WebPFromClipboardAccepted is the headline regression
// for the 1.2.13 gap.
func TestAcceptPaste_WebPFromClipboardAccepted(t *testing.T) {
	data := readFixture(t, "small.webp")
	if got := imagery.Detect(data); got != imagery.FormatWebP {
		t.Fatalf("fixture sanity: Detect(small.webp) = %s, want FormatWebP", got)
	}
	paste, err := AcceptPaste(data, DefaultOptions())
	if err != nil {
		t.Fatalf("AcceptPaste(WebP, WebPEnabled=true): unexpected error %v", err)
	}
	if paste.Format != imagery.FormatWebP {
		t.Errorf("Format = %s, want FormatWebP", paste.Format)
	}
	if len(paste.Data) == 0 {
		t.Errorf("Data is empty; expected the (possibly-scaled) WebP bytes")
	}
	if paste.Filename == "" {
		t.Errorf("Filename is empty; expected a suggested name like pasted-image-1.webp")
	}
	if errors.Is(err, ErrWebPDisabled) {
		t.Errorf("err chain contains ErrWebPDisabled even though WebP is enabled")
	}
	if paste.IsEmpty() {
		t.Errorf("IsEmpty() = true on a successful WebP paste")
	}
}

// ─── brief case 2: PNG still works ──────────────────────────────────────────

// TestAcceptPaste_PNGStillWorks verifies that the existing PNG path
// is unchanged after the 1.2.13 fix.
func TestAcceptPaste_PNGStillWorks(t *testing.T) {
	t.Run("small PNG passes through unchanged", func(t *testing.T) {
		data := makeSolidPNG(t, 64, 48, color.RGBA{R: 0xFF, A: 0xFF})
		paste, err := AcceptPaste(data, DefaultOptions())
		if err != nil {
			t.Fatalf("AcceptPaste(small PNG): %v", err)
		}
		if paste.Format != imagery.FormatPNG {
			t.Errorf("Format = %s, want FormatPNG", paste.Format)
		}
		if paste.Scaled {
			t.Errorf("Scaled = true on a small PNG")
		}
		if !bytes.Equal(data, paste.Data) {
			t.Errorf("small PNG was modified: input=%d bytes, output=%d bytes", len(data), len(paste.Data))
		}
		if paste.Filename == "" {
			t.Errorf("Filename is empty")
		}
	})
	t.Run("oversized PNG is scaled down", func(t *testing.T) {
		data := makeSolidPNG(t, 4000, 3000, color.RGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xFF})
		paste, err := AcceptPaste(data, DefaultOptions())
		if err != nil {
			t.Fatalf("AcceptPaste(oversized PNG): %v", err)
		}
		if !paste.Scaled {
			t.Errorf("Scaled = false on a 4000x3000 PNG")
		}
		w, h, err := imagery.Dimensions(paste.Data)
		if err != nil {
			t.Fatalf("Dimensions(scaled PNG): %v", err)
		}
		if w > imagery.MaxSide || h > imagery.MaxSide {
			t.Errorf("scaled PNG %dx%d exceeds MaxSide=%d", w, h, imagery.MaxSide)
		}
	})
	t.Run("JPEG still works (regression coverage)", func(t *testing.T) {
		data := makeSolidJPEG(t, 32, 32, color.RGBA{A: 0xFF})
		paste, err := AcceptPaste(data, DefaultOptions())
		if err != nil {
			t.Fatalf("AcceptPaste(small JPEG): %v", err)
		}
		if paste.Format != imagery.FormatJPEG {
			t.Errorf("Format = %s, want FormatJPEG", paste.Format)
		}
		if paste.Scaled {
			t.Errorf("Scaled = true on a small JPEG")
		}
	})
}

// ─── brief case 3: oversized WebP is scaled down ───────────────────────────

// TestAcceptPaste_OversizedWebPScaledDown verifies the WebP branch of
// imagery.Prepare is reachable from the clipboard path.
//
// The format the paste carries is the one the **bytes** are in, not the
// one the bytes arrived under: golang.org/x/image/webp only ships a
// decoder, so a scaled WebP leaves as a PNG — and a PNG labelled
// "image/webp" is the mismatched type the 1.4.10 note calls out.
func TestAcceptPaste_OversizedWebPScaledDown(t *testing.T) {
	data := readFixture(t, "large.webp")
	w0, h0, err := imagery.Dimensions(data)
	if err != nil {
		t.Fatalf("Dimensions(large.webp): %v", err)
	}
	if w0 <= imagery.MaxSide && h0 <= imagery.MaxSide {
		t.Fatalf("large.webp fixture not actually oversized: %dx%d (MaxSide=%d)", w0, h0, imagery.MaxSide)
	}
	paste, err := AcceptPaste(data, DefaultOptions())
	if err != nil {
		t.Fatalf("AcceptPaste(oversized WebP): %v", err)
	}
	if paste.Format != imagery.FormatPNG {
		t.Errorf("Format = %s, want FormatPNG — the media type follows the bytes", paste.Format)
	}
	if got := imagery.Detect(paste.Data); got != paste.Format {
		t.Errorf("Detect(paste.Data) = %s, want %s — the media type follows the bytes", got, paste.Format)
	}
	if !paste.Scaled {
		t.Errorf("Scaled = false on a %dx%d WebP", w0, h0)
	}
	w1, h1, err := imagery.Dimensions(paste.Data)
	if err != nil {
		t.Fatalf("Dimensions(scaled WebP): %v", err)
	}
	if w1 > imagery.MaxSide || h1 > imagery.MaxSide {
		t.Errorf("scaled WebP %dx%d exceeds MaxSide=%d", w1, h1, imagery.MaxSide)
	}
	if w1 >= w0 || h1 >= h0 {
		t.Errorf("scaled WebP %dx%d is not smaller than input %dx%d", w1, h1, w0, h0)
	}
}

// ─── brief case 4: non-image bytes are rejected, not crashed ────────────────

// TestAcceptPaste_NonImageRejected exercises the "not an image" path.
func TestAcceptPaste_NonImageRejected(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"plain text", []byte("hello, world")},
		{"short buffer (3 bytes)", []byte{0x00, 0x01, 0x02}},
		{"RIFF with non-WEBP form type", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'A', 'V', 'E'}},
		{"random binary blob", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paste, err := AcceptPaste(tc.data, DefaultOptions())
			if err != nil {
				t.Errorf("AcceptPaste(%s): unexpected error %v", tc.name, err)
			}
			if paste.Format != imagery.FormatUnknown {
				t.Errorf("Format = %s, want FormatUnknown", paste.Format)
			}
			if !paste.IsEmpty() {
				t.Errorf("IsEmpty() = false on non-image bytes")
			}
			if len(paste.Data) != 0 {
				t.Errorf("Data = %d bytes, want 0 (no image returned)", len(paste.Data))
			}
		})
	}
}

// ─── brief case 5: empty paste fails gracefully ─────────────────────────────

// TestAcceptPaste_EmptyPasteFailsGracefully exercises the "nothing on
// the clipboard" path.
func TestAcceptPaste_EmptyPasteFailsGracefully(t *testing.T) {
	t.Run("AcceptPaste(nil)", func(t *testing.T) {
		paste, err := AcceptPaste(nil, DefaultOptions())
		if !errors.Is(err, ErrEmptyPayload) {
			t.Errorf("err = %v, want ErrEmptyPayload (errors.Is)", err)
		}
		if paste.Format != imagery.FormatUnknown {
			t.Errorf("Format = %s, want FormatUnknown", paste.Format)
		}
		if len(paste.Data) != 0 {
			t.Errorf("Data = %d bytes, want 0", len(paste.Data))
		}
	})
	t.Run("AcceptPaste([]byte{})", func(t *testing.T) {
		paste, err := AcceptPaste([]byte{}, DefaultOptions())
		if !errors.Is(err, ErrEmptyPayload) {
			t.Errorf("err = %v, want ErrEmptyPayload (errors.Is)", err)
		}
		if !paste.IsEmpty() {
			t.Errorf("IsEmpty() = false on an empty payload")
		}
	})
	t.Run("ReadImage(BytesSource(nil))", func(t *testing.T) {
		paste, err := ReadImage(BytesSource(nil), DefaultOptions())
		if !errors.Is(err, ErrEmptyPayload) {
			t.Errorf("err = %v, want ErrEmptyPayload (errors.Is)", err)
		}
		if !paste.IsEmpty() {
			t.Errorf("IsEmpty() = false on an empty payload")
		}
	})
	t.Run("ReadImage(nil source)", func(t *testing.T) {
		paste, err := ReadImage(nil, DefaultOptions())
		if !errors.Is(err, ErrSourceUnavailable) {
			t.Errorf("err = %v, want ErrSourceUnavailable (errors.Is)", err)
		}
		if !paste.IsEmpty() {
			t.Errorf("IsEmpty() = false on a nil source")
		}
	})
	t.Run("ReadImage(failing source)", func(t *testing.T) {
		src := failingSource{err: errors.New("no clipboard utility found")}
		paste, err := ReadImage(src, DefaultOptions())
		if !errors.Is(err, ErrSourceUnavailable) {
			t.Errorf("err = %v, want ErrSourceUnavailable (errors.Is)", err)
		}
		if !paste.IsEmpty() {
			t.Errorf("IsEmpty() = false on a failing source")
		}
	})
}

// ─── supplementary: WebP gate, decoder rejection, filename counter ──────────

// TestAcceptPaste_WebPDisabledRespected verifies the one branch where
// WebP is rejected: when Options.WebPEnabled is false.
func TestAcceptPaste_WebPDisabledRespected(t *testing.T) {
	t.Run("WebP rejected when gate is closed", func(t *testing.T) {
		data := readFixture(t, "small.webp")
		opts := DefaultOptions()
		opts.WebPEnabled = false
		paste, err := AcceptPaste(data, opts)
		if !errors.Is(err, ErrWebPDisabled) {
			t.Errorf("err = %v, want ErrWebPDisabled (errors.Is)", err)
		}
		if paste.Format != imagery.FormatWebP {
			t.Errorf("Format = %s, want FormatWebP (so the caller can show the right error)", paste.Format)
		}
	})
	t.Run("PNG still accepted when WebP gate is closed", func(t *testing.T) {
		data := makeSolidPNG(t, 32, 32, color.RGBA{A: 0xFF})
		opts := DefaultOptions()
		opts.WebPEnabled = false
		paste, err := AcceptPaste(data, opts)
		if err != nil {
			t.Errorf("PNG paste errored when WebP gate is closed: %v", err)
		}
		if paste.Format != imagery.FormatPNG {
			t.Errorf("Format = %s, want FormatPNG", paste.Format)
		}
	})
	t.Run("JPEG still accepted when WebP gate is closed", func(t *testing.T) {
		data := makeSolidJPEG(t, 32, 32, color.RGBA{A: 0xFF})
		opts := DefaultOptions()
		opts.WebPEnabled = false
		paste, err := AcceptPaste(data, opts)
		if err != nil {
			t.Errorf("JPEG paste errored when WebP gate is closed: %v", err)
		}
		if paste.Format != imagery.FormatJPEG {
			t.Errorf("Format = %s, want FormatJPEG", paste.Format)
		}
	})
}

// TestAcceptPaste_WebPRejectedByDecoder verifies that a WebP payload
// whose header is valid but whose body is malformed bubbles up the
// imagery.ErrDecodeFailed sentinel.
func TestAcceptPaste_WebPRejectedByDecoder(t *testing.T) {
	data := append([]byte{'R', 'I', 'F', 'F', 0x10, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P'},
		[]byte("not a real vp8 chunk")...)
	paste, err := AcceptPaste(data, DefaultOptions())
	if !errors.Is(err, imagery.ErrDecodeFailed) {
		t.Errorf("err = %v, want imagery.ErrDecodeFailed (errors.Is)", err)
	}
	if paste.Format != imagery.FormatWebP {
		t.Errorf("Format = %s, want FormatWebP (so the caller can name it)", paste.Format)
	}
}

// TestAcceptPaste_FilenameSuggestion locks the suggested-name format.
func TestAcceptPaste_FilenameSuggestion(t *testing.T) {
	t.Run("counter increments by format", func(t *testing.T) {
		cases := []struct {
			format   imagery.Format
			counter  int
			expected string
		}{
			{imagery.FormatPNG, 1, "pasted-image-1.png"},
			{imagery.FormatJPEG, 2, "pasted-image-2.jpeg"},
			{imagery.FormatWebP, 3, "pasted-image-3.webp"},
		}
		for _, tc := range cases {
			if got := suggestedFilename(tc.format, tc.counter); got != tc.expected {
				t.Errorf("suggestedFilename(%s, %d) = %q, want %q", tc.format, tc.counter, got, tc.expected)
			}
		}
	})
	t.Run("counter clamps to 1 when zero or negative", func(t *testing.T) {
		if got := suggestedFilename(imagery.FormatPNG, 0); got != "pasted-image-1.png" {
			t.Errorf("suggestedFilename(PNG, 0) = %q, want clamped to 1", got)
		}
		if got := suggestedFilename(imagery.FormatWebP, -5); got != "pasted-image-1.webp" {
			t.Errorf("suggestedFilename(WebP, -5) = %q, want clamped to 1", got)
		}
	})
}

// TestReadImage_BytesSourceRoundTrip exercises the BytesSource
// constructor and ReadImage's "bytes in, paste out" happy path.
func TestReadImage_BytesSourceRoundTrip(t *testing.T) {
	t.Run("WebP through ReadImage", func(t *testing.T) {
		data := readFixture(t, "small.webp")
		paste, err := ReadImage(BytesSource(data), DefaultOptions())
		if err != nil {
			t.Fatalf("ReadImage(WebP): %v", err)
		}
		if paste.Format != imagery.FormatWebP {
			t.Errorf("Format = %s, want FormatWebP", paste.Format)
		}
	})
	t.Run("PNG through ReadImage", func(t *testing.T) {
		data := makeSolidPNG(t, 32, 32, color.RGBA{R: 0xAA, A: 0xFF})
		paste, err := ReadImage(BytesSource(data), DefaultOptions())
		if err != nil {
			t.Fatalf("ReadImage(PNG): %v", err)
		}
		if paste.Format != imagery.FormatPNG {
			t.Errorf("Format = %s, want FormatPNG", paste.Format)
		}
	})
}

// TestDefaultOptions sanity-checks the documented defaults.
func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if !opts.WebPEnabled {
		t.Errorf("DefaultOptions().WebPEnabled = false, want true (1.2.13 default)")
	}
	if opts.Counter != 1 {
		t.Errorf("DefaultOptions().Counter = %d, want 1", opts.Counter)
	}
}

// ─── brief-pattern aliases ──────────────────────────────────────────────────
//
// The brief's targeted-test pattern is
//
//	go test -run 'TestWebP|TestPaste|TestClipboard|TestImage' ./...
//
// so the names below deliberately match those prefixes. Each is a
// thin wrapper that runs the underlying assertion; the canonical
// tests above carry the structured assertion bodies. The aliases are
// here so a future regression run lands at least one match per
// pattern prefix without depending on the test author naming them.

// TestPasteWebPFromClipboardAccepted is the TestPaste + TestWebP
// alias of TestAcceptPaste_WebPFromClipboardAccepted.
func TestPasteWebPFromClipboardAccepted(t *testing.T) {
	TestAcceptPaste_WebPFromClipboardAccepted(t)
}

// TestPasteImagePNGStillWorks is the TestPaste + TestImage alias of
// TestAcceptPaste_PNGStillWorks.
func TestPasteImagePNGStillWorks(t *testing.T) {
	TestAcceptPaste_PNGStillWorks(t)
}

// TestClipboardEmptyPasteFailsGracefully is the TestClipboard alias of
// TestAcceptPaste_EmptyPasteFailsGracefully.
func TestClipboardEmptyPasteFailsGracefully(t *testing.T) {
	TestAcceptPaste_EmptyPasteFailsGracefully(t)
}
