package clipboard

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/imagery"
)

// 【一手】 agy 1.4.9, the vendor's own release notes: "after the agent
// viewed a corrupt image or one with text in front of its header, such
// as a screenshot captured with `adb exec-out screencap -p >
// screenshot.png` that also captured a warning; such images are now
// rejected with an error that says where the real image header starts,
// so the agent can fix the command."

func TestAcceptPasteRejectsAnImageBehindLeadingBytes(t *testing.T) {
	warning := []byte("WARNING: device offline\n")
	png := makeSolidPNG(t, 8, 8, color.RGBA{R: 0xFF, A: 0xFF})
	payload := append(append([]byte{}, warning...), png...)

	paste, err := AcceptPaste(payload, DefaultOptions())
	if err == nil {
		t.Fatal("AcceptPaste must reject a payload whose header is not at byte 0")
	}
	var offsetErr *imagery.HeaderOffsetError
	if !errors.As(err, &offsetErr) {
		t.Fatalf("error = %v (%T), want *imagery.HeaderOffsetError", err, err)
	}
	if offsetErr.Offset != len(warning) {
		t.Fatalf("offset = %d, want %d", offsetErr.Offset, len(warning))
	}
	if !strings.Contains(err.Error(), "byte offset") {
		t.Fatalf("error = %q, want it to name the byte offset", err.Error())
	}
	if paste.Format != imagery.FormatPNG {
		t.Fatalf("format = %s, want png", paste.Format)
	}
}

func TestAcceptPasteStillAcceptsACleanImage(t *testing.T) {
	png := makeSolidPNG(t, 8, 8, color.RGBA{R: 0xFF, A: 0xFF})
	paste, err := AcceptPaste(png, DefaultOptions())
	if err != nil {
		t.Fatalf("AcceptPaste(clean PNG): %v", err)
	}
	if paste.Format != imagery.FormatPNG {
		t.Fatalf("format = %s, want png", paste.Format)
	}
}
