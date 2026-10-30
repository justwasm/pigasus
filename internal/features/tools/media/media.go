// Package mediagen provides media generation with mTLS Imagen/Lyria/Veo
// backends and ffmpeg fallbacks.
//
// Recreated from cycle 23 source (cycle-23/media-gen/media.go, 112 lines)
// as a proper Go package usable by internal/tools.
package media

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// GenerateImage tries the mTLS Imagen backend when MTLS_TOKEN is set, and
// otherwise falls back to an ffmpeg-generated gradient PNG.
func GenerateImage(prompt string) ([]byte, error) {
	if token := os.Getenv("MTLS_TOKEN"); token != "" {
		return imagenCall(prompt, token)
	}
	return ffmpegGradient(prompt)
}

// GenerateAudio tries the mTLS Lyria backend when MTLS_TOKEN is set, and
// otherwise falls back to an ffmpeg-generated sine-wave MP3.
func GenerateAudio(prompt string) ([]byte, error) {
	if token := os.Getenv("MTLS_TOKEN"); token != "" {
		return lyriaCall(prompt, token)
	}
	return ffmpegSine(prompt, 440.0, 3*time.Second)
}

// GenerateVideo tries the mTLS Veo backend when MTLS_TOKEN is set, and
// otherwise falls back to an ffmpeg-generated test animation MP4.
func GenerateVideo(prompt string) ([]byte, error) {
	if token := os.Getenv("MTLS_TOKEN"); token != "" {
		return veoCall(prompt, token)
	}
	return ffmpegAnimation(prompt, 5*time.Second)
}

// imagenCall would POST to the cloudaicompanion mTLS endpoint at
// /v1internal:generateChat model=imagen-3.0-generate-002. Until a cert is
// available this is a stub that always errors.
func imagenCall(prompt, token string) ([]byte, error) {
	_ = prompt
	_ = token
	return nil, fmt.Errorf("imagen mTLS not yet implemented (need cert)")
}

// lyriaCall would invoke the Lyria music backend over mTLS.
func lyriaCall(prompt, token string) ([]byte, error) {
	_ = prompt
	_ = token
	return nil, fmt.Errorf("lyria mTLS not yet implemented (need cert)")
}

// veoCall would invoke the Veo video backend over mTLS.
func veoCall(prompt, token string) ([]byte, error) {
	_ = prompt
	_ = token
	return nil, fmt.Errorf("veo mTLS not yet implemented (need cert)")
}

// ffmpegGradient renders a 512x512 PNG via ffmpeg's gradients filter,
// keyed off a stable hash of the prompt so identical prompts reuse the
// same filename.
func ffmpegGradient(prompt string) ([]byte, error) {
	out := filepath.Join(os.TempDir(), "imagen-"+hashStr(prompt)+".png")
	c := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i",
		"gradients=size=512x512:duration=1", "-frames:v", "1", out)
	if err := c.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(out)
	return os.ReadFile(out)
}

// ffmpegSine renders a sine-wave MP3 at the supplied frequency/duration.
func ffmpegSine(prompt string, freq float64, dur time.Duration) ([]byte, error) {
	out := filepath.Join(os.TempDir(), "lyria-"+hashStr(prompt)+".mp3")
	c := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i",
		fmt.Sprintf("sine=frequency=%f:duration=%f", freq, dur.Seconds()),
		out)
	if err := c.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(out)
	return os.ReadFile(out)
}

// ffmpegAnimation renders a test-source MP4 with libx264 of the supplied
// duration.
func ffmpegAnimation(prompt string, dur time.Duration) ([]byte, error) {
	out := filepath.Join(os.TempDir(), "veo-"+hashStr(prompt)+".mp4")
	c := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i",
		fmt.Sprintf("testsrc=size=320x240:duration=%f", dur.Seconds()),
		"-c:v", "libx264", out)
	if err := c.Run(); err != nil {
		return nil, err
	}
	defer os.Remove(out)
	return os.ReadFile(out)
}

// hashStr is a tiny deterministic string->uint32 hash used only to
// disambiguate output filenames for ffmpeg fallbacks. It is not
// cryptographic.
func hashStr(s string) string {
	h := uint32(0)
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", h)
}
