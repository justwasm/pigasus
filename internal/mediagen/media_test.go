package mediagen

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestHashStr verifies hashStr is deterministic and that distinct inputs
// usually yield distinct hashes (the algorithm is a tiny non-cryptographic
// hash so we only assert determinism + the empty-string edge case + that
// two different inputs produce different output).
func TestHashStr(t *testing.T) {
	// Empty string: h=0, format "00000000".
	if got := hashStr(""); got != "00000000" {
		t.Errorf("hashStr(\"\") = %q; want %q", got, "00000000")
	}
	// Determinism: every call returns the same value for the same input.
	inputs := []string{"a", "hello", "mediagen prompt", "foo", "bar"}
	for _, s := range inputs {
		first := hashStr(s)
		for i := 0; i < 5; i++ {
			if got := hashStr(s); got != first {
				t.Errorf("hashStr(%q) not deterministic: %q vs %q", s, first, got)
			}
		}
	}
	// Different inputs should mostly yield different hashes.
	if hashStr("foo") == hashStr("bar") {
		t.Errorf("hashStr(\"foo\") == hashStr(\"bar\"); expected distinct")
	}
	// Output is always an 8-character hex string.
	for _, s := range append(inputs, "") {
		got := hashStr(s)
		if len(got) != 8 {
			t.Errorf("hashStr(%q) length = %d; want 8", s, len(got))
		}
		for _, r := range got {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
				t.Errorf("hashStr(%q) = %q; not lowercase hex", s, got)
			}
		}
	}
}

// TestGenerateImageFfmpeg exercises the ffmpeg fallback path. If ffmpeg is
// missing, the test is skipped so CI without the binary still passes.
func TestGenerateImageFfmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available; skipping gradient fallback test")
	}
	data, err := GenerateImage("hello-world")
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("GenerateImage returned 0 bytes")
	}
	// PNG magic: 89 50 4e 47 0d 0a 1a 0a
	if !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("GenerateImage output is not a PNG (got %d bytes)", len(data))
	}
}

// TestGenerateAudioFfmpegSine exercises the audio fallback.
func TestGenerateAudioFfmpegSine(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available; skipping sine fallback test")
	}
	data, err := GenerateAudio("ping")
	if err != nil {
		t.Fatalf("GenerateAudio returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("GenerateAudio returned 0 bytes")
	}
}

// TestGenerateVideoFfmpegAnimation exercises the video fallback.
func TestGenerateVideoFfmpegAnimation(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available; skipping video fallback test")
	}
	data, err := GenerateVideo("loop")
	if err != nil {
		t.Fatalf("GenerateVideo returned error: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("GenerateVideo returned 0 bytes")
	}
}

// TestImagenCallNotImpl verifies the mTLS Imagen path returns an
// "not implemented" error rather than silently falling back or succeeding.
func TestImagenCallNotImpl(t *testing.T) {
	_, err := imagenCall("any prompt", "fake-token")
	if err == nil {
		t.Fatalf("imagenCall returned nil error; expected not-implemented")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("imagenCall error %q does not mention 'not yet implemented'", err.Error())
	}
}

// TestLyriaCallNotImpl verifies the mTLS Lyria path returns an error.
func TestLyriaCallNotImpl(t *testing.T) {
	_, err := lyriaCall("any prompt", "fake-token")
	if err == nil {
		t.Fatalf("lyriaCall returned nil error; expected not-implemented")
	}
}

// TestVeoCallNotImpl verifies the mTLS Veo path returns an error.
func TestVeoCallNotImpl(t *testing.T) {
	_, err := veoCall("any prompt", "fake-token")
	if err == nil {
		t.Fatalf("veoCall returned nil error; expected not-implemented")
	}
}
