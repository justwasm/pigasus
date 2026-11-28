package filesystem

// Guard for the 1.5.18 "half-written image" fix.
//
// 【一手】 agy 1.5.18 changelog:
//
//	Fixed the agent attaching truncated or corrupt images to the conversation
//	when viewing files; it now rejects them with an error, so a half-written
//	image no longer makes later model requests fail and retry.
//
// The reference's 1.5.18 symbol table is what moved: `media.decodeImageMedia`
// is new and `media.ResolveMediaMetadata` is gone, i.e. the reference now
// fully decodes an image before it will attach it. The clone already decodes
// in imagery.Prepare, so the behaviour was already right — but nothing pinned
// it, and "the decoder happens to reject it" is exactly the kind of claim
// that rots. These tests pin the contract: a file that is not a complete,
// decodable image never reaches the conversation as an attachment.
//
// The distinction that matters is *attachment*, not "did the call fail":
// read_file returning the image's bytes as text is harmless (the model sees
// gibberish, not a broken media part), while a MediaOutput carrying a
// half-written image poisons every later request in the conversation with
// INVALID_MEDIA. So the assertion is on Images, not on the error alone.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// testImage builds a small, definitely-decodable image in the given format.
func testImage(t *testing.T, encode func(*bytes.Buffer, image.Image) error) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 5), uint8(y * 5), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := encode(&buf, img); err != nil {
		t.Fatalf("encode test image: %v", err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T) []byte {
	return testImage(t, func(b *bytes.Buffer, img image.Image) error { return png.Encode(b, img) })
}

func jpegBytes(t *testing.T) []byte {
	return testImage(t, func(b *bytes.Buffer, img image.Image) error {
		return jpeg.Encode(b, img, &jpeg.Options{Quality: 90})
	})
}

// viewImages reports how many media parts view_file attached.
func viewImages(t *testing.T, dir, name string) (int, error) {
	t.Helper()
	v := &ViewFile{Workspace: dir}
	out, err := v.Run(context.Background(), json.RawMessage(`{"path":"`+name+`"}`))
	if err != nil {
		return 0, err
	}
	if mo, ok := out.(tools.MediaOutput); ok {
		return len(mo.Images), nil
	}
	return 0, nil
}

func TestViewFileRejectsTruncatedImages(t *testing.T) {
	pngFull, jpegFull := pngBytes(t), jpegBytes(t)
	corruptPNG := append(append([]byte{}, pngFull[:8]...), bytes.Repeat([]byte{0x00}, 64)...)

	cases := []struct {
		name string
		data []byte
		why  string
	}{
		{"half.png", pngFull[:len(pngFull)/2], "half-written PNG"},
		{"header-only.png", pngFull[:8], "only the 8-byte signature"},
		{"no-iend.png", pngFull[:len(pngFull)-12], "IDAT complete, IEND missing"},
		{"no-eoi.jpg", jpegFull[:len(jpegFull)-2], "JPEG scan data complete, EOI missing"},
		{"half.jpg", jpegFull[:len(jpegFull)/2], "half-written JPEG"},
		{"corrupt.png", corruptPNG, "valid signature, invalid checksum"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, c.name), c.data, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			attached, err := viewImages(t, dir, c.name)
			if attached != 0 {
				t.Fatalf("view_file attached %d image part(s) for a %s; a half-written image poisons every later request", attached, c.why)
			}
			if err == nil {
				t.Fatalf("view_file(%s) succeeded; want an error naming the problem", c.name)
			}
		})
	}
}

// TestViewFileAttachesACompleteImage is the control: the rejection above must
// come from the image being broken, not from view_file refusing images.
func TestViewFileAttachesACompleteImage(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"ok.png", pngBytes(t)},
		{"ok.jpg", jpegBytes(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, c.name), c.data, 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			attached, err := viewImages(t, dir, c.name)
			if err != nil {
				t.Fatalf("view_file(%s): %v", c.name, err)
			}
			if attached != 1 {
				t.Fatalf("view_file(%s) attached %d image part(s), want 1", c.name, attached)
			}
		})
	}
}

// TestReadFileNeverAttachesAnImage pins the other half: read_file hands the
// bytes back as text (harmless) and never as a media part, so a broken image
// cannot reach the conversation through it either.
func TestReadFileNeverAttachesAnImage(t *testing.T) {
	pngFull := pngBytes(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "half.png"), pngFull[:len(pngFull)/2], 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	out, err := r.Run(context.Background(), json.RawMessage(`{"path":"half.png"}`))
	if err != nil {
		// An error is fine; the point is that no media part is produced.
		return
	}
	if _, ok := out.(tools.MediaOutput); ok {
		t.Fatal("read_file returned a MediaOutput for a half-written image; it must return text, never an attachment")
	}
}

// TestReadFileAcceptsACompleteImage is read_file's control.
func TestReadFileAcceptsACompleteImage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.png"), pngBytes(t), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	if _, err := r.Run(context.Background(), json.RawMessage(`{"path":"ok.png"}`)); err != nil {
		t.Fatalf("read_file(complete PNG): %v", err)
	}
}
