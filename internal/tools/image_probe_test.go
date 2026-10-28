package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/imagery"
)

// 【一手】 agy 1.4.9, the vendor's own release notes: "such images are
// now rejected with an error that says where the real image header
// starts, so the agent can fix the command."

// pngMagic is the PNG signature; a real image file starts with it.
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

func TestProbeImageRejectsAHeaderBehindLeadingBytes(t *testing.T) {
	warning := []byte("WARNING: linker: out of memory\n")
	data := append(append([]byte{}, warning...), pngMagic...)
	err := probeImage("screenshot.png", data)
	if err == nil {
		t.Fatal("probeImage must reject an image whose header is not at byte 0")
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
}

func TestProbeImageAcceptsACleanImage(t *testing.T) {
	if err := probeImage("shot.png", pngMagic); err != nil {
		t.Fatalf("probeImage(clean PNG): %v", err)
	}
}

func TestProbeImageLeavesAFileWithoutAHeaderAlone(t *testing.T) {
	// The reference's error for a fully corrupt image does not name an
	// offset, and the clone does not attach images to the model, so the
	// probe stays quiet here.
	if err := probeImage("notes.png", []byte("not an image at all")); err != nil {
		t.Fatalf("probeImage(no header): %v", err)
	}
}

func TestIsImagePath(t *testing.T) {
	for _, path := range []string{"a.png", "a.PNG", "b.jpg", "c.jpeg", "d.webp"} {
		if !IsImagePath(path) {
			t.Errorf("IsImagePath(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"a.txt", "a.webm", "a", "a.png.txt"} {
		if IsImagePath(path) {
			t.Errorf("IsImagePath(%q) = true, want false", path)
		}
	}
}

func TestReadFileRejectsAnImageWithLeadingBytes(t *testing.T) {
	dir := t.TempDir()
	warning := []byte("adb: warning: device offline\n")
	data := append(append([]byte{}, warning...), pngMagic...)
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	_, err := r.Run(context.Background(), json.RawMessage(`{"path":"shot.png"}`))
	if err == nil {
		t.Fatal("read_file must reject an image whose header is not at byte 0")
	}
	if !strings.Contains(err.Error(), "byte offset") {
		t.Fatalf("error = %q, want it to name the byte offset", err.Error())
	}
}

func TestViewFileRejectsAnImageWithLeadingBytes(t *testing.T) {
	dir := t.TempDir()
	warning := []byte("adb: warning: device offline\n")
	data := append(append([]byte{}, warning...), pngMagic...)
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	v := &ViewFile{Workspace: dir}
	_, err := v.Run(context.Background(), json.RawMessage(`{"path":"shot.png"}`))
	if err == nil {
		t.Fatal("view_file must reject an image whose header is not at byte 0")
	}
	if !strings.Contains(err.Error(), "byte offset") {
		t.Fatalf("error = %q, want it to name the byte offset", err.Error())
	}
}

func TestReadFileAcceptsACleanImage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), pngMagic, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	r := &ReadFile{Workspace: dir}
	if _, err := r.Run(context.Background(), json.RawMessage(`{"path":"shot.png"}`)); err != nil {
		t.Fatalf("read_file(clean PNG): %v", err)
	}
}
