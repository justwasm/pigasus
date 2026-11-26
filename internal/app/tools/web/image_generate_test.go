package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTestPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return buf.Bytes()
}

func TestGenerateImageSavesArtifactAndPassesInputs(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	srcBytes := makeTestPNG(t)
	srcPath := filepath.Join(ws, "ref.png")
	if err := os.WriteFile(srcPath, srcBytes, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	outBytes := makeTestPNG(t)

	var gotReq GenerateImageRequest
	tool := &GenerateImage{
		Workspace:      ws,
		ConversationID: "conv-img-1",
		Home:           home,
		Provider: func(_ context.Context, req GenerateImageRequest) (GenerateImageResult, error) {
			gotReq = req
			return GenerateImageResult{MIMEType: "image/jpeg", Data: outBytes}, nil
		},
	}

	args, _ := json.Marshal(map[string]any{
		"Prompt":      "A stylized mountain sunrise",
		"ImageName":   "MountainSunrise",
		"ImagePaths":  []string{srcPath},
		"AspectRatio": "16:9",
	})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	body := stripStamp(t, out)
	if gotReq.Prompt != "A stylized mountain sunrise" {
		t.Errorf("Prompt = %q", gotReq.Prompt)
	}
	if gotReq.AspectRatio != "16:9" {
		t.Errorf("AspectRatio = %q, want 16:9", gotReq.AspectRatio)
	}
	if len(gotReq.InputImages) != 1 || gotReq.InputImages[0].MIMEType != "image/png" || !bytes.Equal(gotReq.InputImages[0].Data, srcBytes) {
		t.Fatalf("InputImages = %+v, want 1 PNG matching ref.png", gotReq.InputImages)
	}

	const prefix = "Generated image is saved at "
	if !strings.HasPrefix(body, prefix) || !strings.HasSuffix(body, ".") {
		t.Fatalf("unexpected body %q", body)
	}
	savedPath := strings.TrimSuffix(strings.TrimPrefix(body, prefix), ".")
	wantDir := filepath.Join(home, "brain", "conv-img-1")
	if filepath.Dir(savedPath) != wantDir {
		t.Errorf("saved dir = %q, want %q", filepath.Dir(savedPath), wantDir)
	}
	base := filepath.Base(savedPath)
	if !strings.HasPrefix(base, "mountain_sunrise_") || !strings.HasSuffix(base, ".jpg") {
		t.Errorf("saved filename = %q, want mountain_sunrise_<ts>.jpg", base)
	}
	diskBytes, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", savedPath, err)
	}
	if !bytes.Equal(diskBytes, outBytes) {
		t.Errorf("saved bytes mismatch")
	}
}

func TestGenerateImageValidationAndErrors(t *testing.T) {
	tool := &GenerateImage{
		Workspace:      t.TempDir(),
		ConversationID: "conv-err",
		Home:           t.TempDir(),
		Provider: func(context.Context, GenerateImageRequest) (GenerateImageResult, error) {
			return GenerateImageResult{}, errors.New("quota exhausted")
		},
	}

	if _, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"","ImageName":"x"}`)); err == nil {
		t.Fatal("expected error for empty prompt")
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"hi","ImageName":""}`)); err == nil {
		t.Fatal("expected error for empty image_name")
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"hi","ImageName":"x","AspectRatio":"5:4"}`)); err == nil {
		t.Fatal("expected error for unsupported aspect ratio")
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"hi","ImageName":"x","ImagePaths":["a","b","c","d"]}`)); err == nil {
		t.Fatal("expected error for >3 image paths")
	}

	out, err := tool.Run(context.Background(), json.RawMessage(`{"Prompt":"hi","ImageName":"x"}`))
	if err != nil {
		t.Fatalf("provider error should return tool failure text, got err: %v", err)
	}
	if body := stripStamp(t, out); body != "Encountered error in tool execution: quota exhausted" {
		t.Fatalf("body = %q", body)
	}
}
