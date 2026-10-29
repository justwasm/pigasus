package remotecontrol

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// TestMessageImages_TheMediaTypeFollowsTheBytes pins the 1.4.10 rule
// "an image saved as JPEG but labeled PNG, such as a pasted image or
// browser screenshot, is no longer rejected for a mismatched type".
//
// The Web UI hands the picture over with the mime type it read off the
// file; the bytes are what the model is sent. When the two disagree,
// the bytes win.
func TestMessageImages_TheMediaTypeFollowsTheBytes(t *testing.T) {
	// A real JPEG, labelled as a PNG — the shape a pasted image or a
	// browser screenshot arrives in.
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 0x40, A: 0xFF})
		}
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	jpegBytes := buf.Bytes()

	images := messageImagesFromMedia([]lsMediaItem{{
		MIMEType:   "image/png",
		InlineData: base64.StdEncoding.EncodeToString(jpegBytes),
	}})
	if len(images) != 1 {
		t.Fatalf("read %d images, want 1", len(images))
	}
	if images[0].MIMEType != "image/jpeg" {
		t.Errorf("the media type = %q, want image/jpeg — it follows the bytes", images[0].MIMEType)
	}
	if !bytes.Equal(images[0].Data, jpegBytes) {
		t.Error("the bytes were modified")
	}
}

// TestMessageImages_ANonImageKeepsItsOwnType is the other side of the
// rule: a picture the pipeline does not know (a PDF, say) keeps the
// media type the frontend stated.
func TestMessageImages_ANonImageKeepsItsOwnType(t *testing.T) {
	images := messageImagesFromMedia([]lsMediaItem{{
		MIMEType:   "application/pdf",
		InlineData: base64.StdEncoding.EncodeToString([]byte("%PDF-1.7\n")),
	}})
	if len(images) != 1 {
		t.Fatalf("read %d images, want 1", len(images))
	}
	if images[0].MIMEType != "application/pdf" {
		t.Errorf("the media type = %q, want application/pdf", images[0].MIMEType)
	}
}

// TestMessageImages_ALargePNGIsCapped pins the size half of the 1.4.10
// note: the picture that goes into the request fits the model's caps.
func TestMessageImages_ALargePNGIsCapped(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2400, 1200))
	for y := 0; y < 1200; y++ {
		for x := 0; x < 2400; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x20, A: 0xFF})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}

	images := messageImagesFromMedia([]lsMediaItem{{
		MIMEType:   "image/png",
		InlineData: base64.StdEncoding.EncodeToString(buf.Bytes()),
	}})
	if len(images) != 1 {
		t.Fatalf("read %d images, want 1", len(images))
	}
	decoded, _, err := image.Decode(bytes.NewReader(images[0].Data))
	if err != nil {
		t.Fatalf("decode the image that goes into the request: %v", err)
	}
	b := decoded.Bounds()
	if b.Dx() > 2000 || b.Dy() > 2000 {
		t.Errorf("the picture is %dx%d, over the 2000-pixel cap", b.Dx(), b.Dy())
	}
}
