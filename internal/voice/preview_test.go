package voice

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestVoicePreviewCatalog(t *testing.T) {
	catalog := VoicePreviewCatalog()
	if len(catalog) != len(previewPersonas)*VoicePreviewVariants {
		t.Fatalf("catalog has %d samples, want %d", len(catalog), len(previewPersonas)*VoicePreviewVariants)
	}

	personas := VoicePreviewPersonas()
	if len(personas) != 9 {
		t.Fatalf("personas = %v, want 9", personas)
	}
	for _, want := range []string{"Fola", "Arlo", "Cleo", "Finn", "Gero", "Lora", "Neno", "Olin", "Tari"} {
		found := false
		for _, p := range personas {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("persona %q missing from %v", want, personas)
		}
	}

	seen := map[string]bool{}
	for _, s := range catalog {
		if seen[s.Key] {
			t.Errorf("duplicate sample key %q", s.Key)
		}
		seen[s.Key] = true
		if s.Bytes < 4096 {
			t.Errorf("%s: only %d bytes, expected a real clip", s.Key, s.Bytes)
		}
		if s.Variant < 1 || s.Variant > VoicePreviewVariants {
			t.Errorf("%s: variant %d out of range", s.Key, s.Variant)
		}
		if want := strings.ToLower(s.Persona) + "_" + itoa(s.Variant); s.Key != want {
			t.Errorf("%s: key should be %q", s.Key, want)
		}
		if s.GeminiVoice == "" {
			t.Errorf("%s: no Gemini voice mapping", s.Key)
		}
		data, err := VoicePreviewBytes(s)
		if err != nil {
			t.Fatalf("%s: read: %v", s.Key, err)
		}
		if !looksLikeMP3(data) {
			t.Errorf("%s: payload is not an mp3 (first bytes % x)", s.Key, data[:min(4, len(data))])
		}
	}
}

func TestResolveVoicePreviewSample(t *testing.T) {
	tests := []struct {
		name        string
		variant     int
		wantKey     string
		wantPersona string
		wantGemini  string
	}{
		{name: "Tari", variant: 1, wantKey: "tari_1", wantPersona: "Tari", wantGemini: "Kore"},
		{name: "tari", variant: 2, wantKey: "tari_2", wantPersona: "Tari", wantGemini: "Kore"},
		{name: "  Arlo  ", variant: 3, wantKey: "arlo_3", wantPersona: "Arlo", wantGemini: "Puck"},
		{name: "Fola", variant: 0, wantKey: "fola_1", wantPersona: "Fola", wantGemini: "Aoede"},
		{name: "Puck", variant: 2, wantKey: "arlo_2", wantPersona: "Arlo", wantGemini: "Puck"},
		{name: "Kore", variant: 1, wantKey: "cleo_1", wantPersona: "Cleo", wantGemini: "Kore"},
		{name: "charon", variant: 3, wantKey: "neno_3", wantPersona: "Neno", wantGemini: "Charon"},
	}
	for _, tc := range tests {
		sample, err := ResolveVoicePreviewSample(tc.name, tc.variant)
		if err != nil {
			t.Fatalf("ResolveVoicePreviewSample(%q, %d): %v", tc.name, tc.variant, err)
		}
		if sample.Key != tc.wantKey || sample.Persona != tc.wantPersona || sample.GeminiVoice != tc.wantGemini {
			t.Errorf("ResolveVoicePreviewSample(%q, %d) = %+v, want key=%s persona=%s gemini=%s",
				tc.name, tc.variant, sample, tc.wantKey, tc.wantPersona, tc.wantGemini)
		}
		if sample.Bytes <= 0 {
			t.Errorf("%s: empty sample", sample.Key)
		}
	}
}

func TestResolveVoicePreviewSampleErrors(t *testing.T) {
	if _, err := ResolveVoicePreviewSample("Zephyr", 1); !errors.Is(err, ErrNoVoicePreview) {
		t.Errorf("Zephyr (no recording) error = %v, want ErrNoVoicePreview", err)
	}
	if _, err := ResolveVoicePreviewSample("", 1); !errors.Is(err, ErrNoVoicePreview) {
		t.Errorf("empty name error = %v, want ErrNoVoicePreview", err)
	}
	if _, err := ResolveVoicePreviewSample("Tari", 4); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("variant 4 error = %v, want out of range", err)
	}
	if _, err := ResolveVoicePreviewSample("Tari", -1); err == nil {
		t.Error("negative variant should be rejected")
	}
}

func TestHasVoicePreview(t *testing.T) {
	for _, name := range append(VoicePreviewPersonas(), "aoede", "PUCK", "kore", "fenrir", "charon") {
		if !HasVoicePreview(name) {
			t.Errorf("HasVoicePreview(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"zephyr", "leda", "orus", "unknown", ""} {
		if HasVoicePreview(name) {
			t.Errorf("HasVoicePreview(%q) = true, want false", name)
		}
	}
}

func TestPlayVoicePreviewWritesFileAndPlays(t *testing.T) {
	var gotPath string
	var gotBytes int
	restore := SetVoicePreviewPlayerForTest(func(ctx context.Context, path string) error {
		gotPath = path
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("player received a path that does not exist: %v", err)
		}
		gotBytes = int(info.Size())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read clip: %v", err)
		}
		if !looksLikeMP3(data) {
			t.Errorf("%s is not an mp3", path)
		}
		return nil
	})
	defer restore()

	sample, err := PlayVoicePreview(context.Background(), "Neno", 2)
	if err != nil {
		t.Fatalf("PlayVoicePreview: %v", err)
	}
	if sample.Key != "neno_2" {
		t.Errorf("played %s, want neno_2", sample.Key)
	}
	if !strings.HasSuffix(gotPath, "neno_2.mp3") {
		t.Errorf("player path = %q, want .../neno_2.mp3", gotPath)
	}
	if gotBytes != sample.Bytes {
		t.Errorf("on-disk clip is %d bytes, want %d", gotBytes, sample.Bytes)
	}
	if _, err := os.Stat(gotPath); !os.IsNotExist(err) {
		t.Errorf("temp clip %q should be removed after playback (stat err = %v)", gotPath, err)
	}
}

func TestPlayVoicePreviewPropagatesPlayerError(t *testing.T) {
	wantErr := errors.New("no sound card")
	restore := SetVoicePreviewPlayerForTest(func(ctx context.Context, path string) error {
		return wantErr
	})
	defer restore()

	if _, err := PlayVoicePreview(context.Background(), "Fola", 1); !errors.Is(err, wantErr) {
		t.Fatalf("PlayVoicePreview error = %v, want %v", err, wantErr)
	}
}

func TestPlayAudioFileRejectsMissingPath(t *testing.T) {
	if err := PlayVoicePreviewFile(context.Background(), ""); err == nil {
		t.Fatal("empty path should be rejected")
	}
	if err := PlayVoicePreviewFile(context.Background(), "/nonexistent/voice.mp3"); err == nil {
		t.Fatal("missing file should be rejected")
	}
}

func TestAudioPlayerCandidatesNameRealPlayers(t *testing.T) {
	// The candidate list must never be empty: a platform without a listed
	// player would silently swallow every preview.
	if got := audioPlayerCandidates("/tmp/a.mp3"); len(got) == 0 {
		t.Fatal("no audio player candidates on this platform")
	}
	if hint := previewPlayerHint(); hint == "" {
		t.Fatal("empty player hint")
	}
}

func TestParseVoicePreviewVariant(t *testing.T) {
	tests := []struct {
		arg       string
		want      int
		wantAll   bool
		wantError bool
	}{
		{arg: "", want: 1},
		{arg: "2", want: 2},
		{arg: " 3 ", want: 3},
		{arg: "all", wantAll: true},
		{arg: "ALL", wantAll: true},
		{arg: "0", wantError: true},
		{arg: "4", wantError: true},
		{arg: "two", wantError: true},
	}
	for _, tc := range tests {
		got, all, err := ParseVoicePreviewVariant(tc.arg)
		if tc.wantError {
			if err == nil {
				t.Errorf("ParseVoicePreviewVariant(%q) should fail", tc.arg)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVoicePreviewVariant(%q): %v", tc.arg, err)
			continue
		}
		if got != tc.want || all != tc.wantAll {
			t.Errorf("ParseVoicePreviewVariant(%q) = (%d, %v), want (%d, %v)", tc.arg, got, all, tc.want, tc.wantAll)
		}
	}
}

func TestVoicePreviewTimeoutIsBounded(t *testing.T) {
	if VoicePreviewTimeout <= 0 || VoicePreviewTimeout > 2*time.Minute {
		t.Fatalf("VoicePreviewTimeout = %v, want a bounded duration", VoicePreviewTimeout)
	}
}

func looksLikeMP3(data []byte) bool {
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return true
	}
	return len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
