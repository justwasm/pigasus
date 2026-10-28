package imagery

import "testing"

// 【一手】 agy 1.4.9, the vendor's own release notes: "such images are
// now rejected with an error that says where the real image header
// starts, so the agent can fix the command".

func TestHeaderOffsetOnACleanImage(t *testing.T) {
	png := append([]byte{}, pngMagic...)
	off, format := HeaderOffset(png)
	if off != 0 || format != FormatPNG {
		t.Fatalf("offset/format = %d/%v, want 0/png", off, format)
	}
}

func TestHeaderOffsetFindsTheRealHeader(t *testing.T) {
	// The shape `adb exec-out screencap -p > screenshot.png` produces
	// when the device also printed a warning: the warning comes first.
	warning := []byte("WARNING: linker: out of memory\n")
	payload := append(append([]byte{}, warning...), pngMagic...)
	payload = append(payload, 0x00, 0x01, 0x02, 0x03)

	off, format := HeaderOffset(payload)
	if off != len(warning) {
		t.Fatalf("offset = %d, want %d", off, len(warning))
	}
	if format != FormatPNG {
		t.Fatalf("format = %v, want png", format)
	}
}

func TestHeaderOffsetOnAJPEG(t *testing.T) {
	payload := append([]byte("junk"), jpegMagic...)
	off, format := HeaderOffset(payload)
	if off != 4 || format != FormatJPEG {
		t.Fatalf("offset/format = %d/%v, want 4/jpeg", off, format)
	}
}

func TestHeaderOffsetOnAWebP(t *testing.T) {
	payload := append([]byte{0x00, 0x01}, 'R', 'I', 'F', 'F')
	payload = append(payload, 0x10, 0x00, 0x00, 0x00)
	payload = append(payload, 'W', 'E', 'B', 'P')
	off, format := HeaderOffset(payload)
	if off != 2 || format != FormatWebP {
		t.Fatalf("offset/format = %d/%v, want 2/webp", off, format)
	}
}

func TestHeaderOffsetWithoutAnyHeader(t *testing.T) {
	off, format := HeaderOffset([]byte("just some text"))
	if off != -1 || format != FormatUnknown {
		t.Fatalf("offset/format = %d/%v, want -1/unknown", off, format)
	}
}

func TestHeaderOffsetErrorMessage(t *testing.T) {
	err := &HeaderOffsetError{Subject: "screenshot.png", Offset: 42, Format: FormatPNG}
	want := "invalid screenshot.png: header found at byte offset 42 after leading non-image bytes"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
