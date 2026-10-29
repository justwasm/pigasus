package imagery

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// makeNoisePNG returns a PNG of the requested size whose pixels are
// deterministic noise.
//
// The noise is what makes the JPEG encoding *much* smaller than the PNG
// one: a flat picture compresses to almost nothing as a PNG and grows
// as a JPEG, so it exercises the other branch of the conversion rule.
func makeNoisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	seed := uint32(0x9E3779B9)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{
				R: uint8(seed >> 24),
				G: uint8(seed >> 16),
				B: uint8(seed >> 8),
				A: 0xFF,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode %dx%d noise PNG: %v", w, h, err)
	}
	return buf.Bytes()
}

// TestPrepare_LargePNGGoesOutAsAJPEG pins the 1.4.10 rule "large PNGs
// are sent as JPEGs when that is much smaller".
//
// The fixture is under the pixel cap and over LargePNGBytes, so the
// *only* reason it is re-encoded is its size — which is exactly the
// case the note is about: a conversation that collects many images is
// rejected for the request's size limit, not for the pixel count.
func TestPrepare_LargePNGGoesOutAsAJPEG(t *testing.T) {
	data := makeNoisePNG(t, 600, 600)
	if len(data) < LargePNGBytes {
		t.Fatalf("the fixture is %d bytes, under LargePNGBytes (%d)", len(data), LargePNGBytes)
	}

	out, format, changed, err := Prepare(data)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !changed {
		t.Fatal("changed = false; a large PNG should have been re-encoded")
	}
	if format != FormatJPEG {
		t.Errorf("the format = %s, want jpeg", format)
	}
	if got := Detect(out); got != FormatJPEG {
		t.Errorf("Detect(output) = %s, want jpeg — the reported format must follow the bytes", got)
	}
	if len(out)*JPEGMuchSmaller > len(data) {
		t.Errorf("the JPEG is %d bytes against a %d-byte PNG: not \"much smaller\"", len(out), len(data))
	}
}

// TestPrepare_SmallPNGIsLeftAlone pins the other side of the rule: a
// PNG that is not large gains nothing from a lossy round trip, so the
// bytes go out untouched.
func TestPrepare_SmallPNGIsLeftAlone(t *testing.T) {
	data := makeSolidPNG(t, 64, 48, color.RGBA{R: 0xFF, A: 0xFF})

	out, format, changed, err := Prepare(data)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if changed {
		t.Error("changed = true on a small PNG")
	}
	if format != FormatPNG {
		t.Errorf("the format = %s, want png", format)
	}
	if !bytes.Equal(data, out) {
		t.Error("a small PNG was modified")
	}
}

// TestPrepare_OversizedImageIsCapped pins the other half of the 1.4.10
// rule: "images are now capped at 2000 pixels per side".
func TestPrepare_OversizedImageIsCapped(t *testing.T) {
	data := makeSolidPNG(t, MaxSide+500, MaxSide+100, color.RGBA{R: 0x80, G: 0x40, B: 0x20, A: 0xFF})

	out, _, changed, err := Prepare(data)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !changed {
		t.Fatal("changed = false; an oversized image should have been re-encoded")
	}
	w, h, err := Dimensions(out)
	if err != nil {
		t.Fatalf("Dimensions(output): %v", err)
	}
	if w > MaxSide || h > MaxSide {
		t.Errorf("the output is %dx%d, over the %d-pixel cap", w, h, MaxSide)
	}
}

// TestPrepare_FormatFollowsTheBytes pins the third rule of the 1.4.10
// note: "an image saved as JPEG but labeled PNG … is no longer rejected
// for a mismatched type".
//
// The bytes are a JPEG; the name they arrived under is not part of the
// call, so the format the caller is told about is the one the bytes
// are in.
func TestPrepare_FormatFollowsTheBytes(t *testing.T) {
	data := makeSolidJPEG(t, 800, 600, color.RGBA{R: 0x20, G: 0x60, B: 0xA0, A: 0xFF})

	out, format, _, err := Prepare(data)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if format != FormatJPEG {
		t.Errorf("the format = %s, want jpeg", format)
	}
	if got := Detect(out); got != FormatJPEG {
		t.Errorf("Detect(output) = %s, want jpeg", got)
	}
}

// TestPrepare_TransparencyIsFlattenedOntoWhite pins the one hazard of
// the PNG→JPEG rule: JPEG has no alpha channel, so a see-through pixel
// must not turn into a black box.
//
// The fixture's left half is transparent and its right half is noise —
// the noise is what makes the PNG big enough for the conversion to
// happen at all.
func TestPrepare_TransparencyIsFlattenedOntoWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 600, 600))
	seed := uint32(0x9E3779B9)
	for y := 0; y < 600; y++ {
		for x := 0; x < 600; x++ {
			if x < 300 {
				img.Set(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
				continue
			}
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.NRGBA{
				R: uint8(seed >> 24),
				G: uint8(seed >> 16),
				B: uint8(seed >> 8),
				A: 0xFF,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	data := buf.Bytes()
	if len(data) < LargePNGBytes {
		t.Fatalf("the fixture is %d bytes, under LargePNGBytes (%d)", len(data), LargePNGBytes)
	}

	out, format, _, err := Prepare(data)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if format != FormatJPEG {
		t.Fatalf("the format = %s, want jpeg", format)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode the JPEG: %v", err)
	}
	r, g, b, _ := decoded.At(10, 10).RGBA()
	if r < 0xF000 || g < 0xF000 || b < 0xF000 {
		t.Errorf("the transparent half is (%d,%d,%d), want white", r>>8, g>>8, b>>8)
	}
}

// TestPrepare_NonImagePassesThrough pins the edges: bytes that are not
// an image come back unchanged, and the empty buffer still reports
// ErrTooShort.
func TestPrepare_NonImagePassesThrough(t *testing.T) {
	data := []byte("hello, world")
	out, format, changed, err := Prepare(data)
	if err != nil {
		t.Errorf("Prepare(text) error = %v, want nil", err)
	}
	if changed {
		t.Error("changed = true on plain text")
	}
	if format != FormatUnknown {
		t.Errorf("the format = %s, want unknown", format)
	}
	if !bytes.Equal(data, out) {
		t.Error("plain text was modified")
	}

	if _, _, _, err := Prepare(nil); err == nil {
		t.Error("Prepare(nil) error = nil, want ErrTooShort")
	}
}
