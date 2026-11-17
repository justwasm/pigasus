package voice

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed previews/*.mp3
var voicePreviewFS embed.FS

const (
	// VoicePreviewDir is the embedded directory holding the recorded clips.
	VoicePreviewDir = "previews"
	// VoicePreviewVariants is the number of recorded samples per persona.
	VoicePreviewVariants = 3
	// VoicePreviewTimeout caps a single sample playback.
	VoicePreviewTimeout = 20 * time.Second
)

// ErrNoVoicePreview reports that no recorded sample ships for a voice.
var ErrNoVoicePreview = errors.New("voice: no recorded preview for this voice")

// ErrNoAudioPlayer reports that no supported audio player was found on PATH.
var ErrNoAudioPlayer = errors.New("voice: no audio player found on PATH")

// VoicePreviewSample is one recorded clip shipped with the binary.
type VoicePreviewSample struct {
	// Persona is the Antigravity persona the clip was recorded for, e.g. "Tari".
	Persona string
	// Key is the embedded file stem, e.g. "tari_2".
	Key string
	// Variant is the 1-based sample index within the persona.
	Variant int
	// Bytes is the size of the embedded clip.
	Bytes int
	// GeminiVoice is the prebuilt Gemini Live voice the persona maps to.
	GeminiVoice string
}

// String renders the sample the way the CLIs cite it: "Tari (tari_2.mp3, sample 2/3)".
func (s VoicePreviewSample) String() string {
	return fmt.Sprintf("%s (%s.mp3, sample %d/%d)", s.Persona, s.Key, s.Variant, VoicePreviewVariants)
}

// previewPersonas is the recorded persona order, which is also the picker order.
var previewPersonas = []string{"fola", "arlo", "cleo", "finn", "gero", "lora", "neno", "olin", "tari"}

// geminiPreviewAliases maps a prebuilt Gemini Live voice to the recorded
// persona that speaks with it, so "/voice preview Puck" still auditions a clip.
var geminiPreviewAliases = map[string]string{
	"aoede":  "fola",
	"puck":   "arlo",
	"kore":   "cleo",
	"fenrir": "finn",
	"charon": "neno",
}

var (
	previewOnce    sync.Once
	previewCatalog []VoicePreviewSample
)

// VoicePreviewCatalog returns the recorded samples, ordered by persona then
// variant. The slice is a copy: callers may not mutate the catalog.
func VoicePreviewCatalog() []VoicePreviewSample {
	previewOnce.Do(func() {
		for _, stem := range previewPersonas {
			persona := strings.ToUpper(stem[:1]) + stem[1:]
			for variant := 1; variant <= VoicePreviewVariants; variant++ {
				key := fmt.Sprintf("%s_%d", stem, variant)
				data, err := voicePreviewFS.ReadFile(VoicePreviewDir + "/" + key + ".mp3")
				if err != nil {
					continue
				}
				previewCatalog = append(previewCatalog, VoicePreviewSample{
					Persona:     persona,
					Key:         key,
					Variant:     variant,
					Bytes:       len(data),
					GeminiVoice: MapVoiceName(persona),
				})
			}
		}
	})
	out := make([]VoicePreviewSample, len(previewCatalog))
	copy(out, previewCatalog)
	return out
}

// VoicePreviewPersonas lists the personas that ship recorded samples, in the
// same order their clips were recorded.
func VoicePreviewPersonas() []string {
	var out []string
	for _, stem := range previewPersonas {
		out = append(out, strings.ToUpper(stem[:1])+stem[1:])
	}
	return out
}

// HasVoicePreview reports whether a persona or Gemini voice name has a clip.
func HasVoicePreview(name string) bool {
	stem, ok := previewStem(name)
	return ok && stem != ""
}

// previewStem resolves a persona or Gemini voice name to its recorded stem.
func previewStem(name string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return "", false
	}
	if alias, ok := geminiPreviewAliases[lower]; ok {
		return alias, true
	}
	for _, stem := range previewPersonas {
		if stem == lower {
			return stem, true
		}
	}
	if p, ok := FindVoicePersona(lower); ok {
		candidate := strings.ToLower(p.Name)
		for _, stem := range previewPersonas {
			if stem == candidate {
				return stem, true
			}
		}
	}
	return "", false
}

// ResolveVoicePreviewSample finds the clip for a voice name and a 1-based
// variant. A variant of 0 selects the first sample; anything outside
// 1..VoicePreviewVariants is rejected.
func ResolveVoicePreviewSample(name string, variant int) (VoicePreviewSample, error) {
	stem, ok := previewStem(name)
	if !ok {
		return VoicePreviewSample{}, fmt.Errorf("%w: %q (recorded: %s)",
			ErrNoVoicePreview, name, strings.Join(VoicePreviewPersonas(), ", "))
	}
	if variant == 0 {
		variant = 1
	}
	if variant < 1 || variant > VoicePreviewVariants {
		return VoicePreviewSample{}, fmt.Errorf("voice: sample %d out of range (1-%d)", variant, VoicePreviewVariants)
	}
	key := fmt.Sprintf("%s_%d", stem, variant)
	data, err := voicePreviewFS.ReadFile(VoicePreviewDir + "/" + key + ".mp3")
	if err != nil {
		return VoicePreviewSample{}, fmt.Errorf("%w: %q sample %d", ErrNoVoicePreview, name, variant)
	}
	persona := strings.ToUpper(stem[:1]) + stem[1:]
	return VoicePreviewSample{
		Persona:     persona,
		Key:         key,
		Variant:     variant,
		Bytes:       len(data),
		GeminiVoice: MapVoiceName(persona),
	}, nil
}

// VoicePreviewBytes returns the embedded mp3 payload for a sample.
func VoicePreviewBytes(sample VoicePreviewSample) ([]byte, error) {
	return voicePreviewFS.ReadFile(VoicePreviewDir + "/" + sample.Key + ".mp3")
}

// PlayVoicePreview resolves, writes to a temp file and plays a recorded sample.
// It returns the sample it played so callers can cite it in their own log line.
func PlayVoicePreview(ctx context.Context, name string, variant int) (VoicePreviewSample, error) {
	sample, err := ResolveVoicePreviewSample(name, variant)
	if err != nil {
		return sample, err
	}
	data, err := VoicePreviewBytes(sample)
	if err != nil {
		return sample, err
	}

	dir, err := os.MkdirTemp("", "pigasus-voice-preview-")
	if err != nil {
		return sample, fmt.Errorf("voice preview: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, sample.Key+".mp3")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return sample, fmt.Errorf("voice preview: write clip: %w", err)
	}
	if err := voicePreviewPlayerFn(ctx, path); err != nil {
		return sample, err
	}
	return sample, nil
}

// voicePreviewPlayerFn is the playback seam: tests replace it to observe the
// call without touching the sound card.
var voicePreviewPlayerFn = playAudioFile

// SetVoicePreviewPlayerForTest swaps the playback seam and returns a restore func.
func SetVoicePreviewPlayerForTest(fn func(ctx context.Context, path string) error) func() {
	prev := voicePreviewPlayerFn
	voicePreviewPlayerFn = fn
	return func() { voicePreviewPlayerFn = prev }
}

// PlayVoicePreviewFile plays a single audio file with the platform player.
func PlayVoicePreviewFile(ctx context.Context, path string) error {
	return playAudioFile(ctx, path)
}

func playAudioFile(ctx context.Context, path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("voice preview: empty audio path")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("voice preview: %w", err)
	}

	var avail []string
	var failures []string
	for _, candidate := range audioPlayerCandidates(path) {
		bin, err := exec.LookPath(candidate[0])
		if err != nil {
			continue
		}
		avail = append(avail, candidate[0])
		cmd := exec.CommandContext(ctx, bin, candidate[1:]...)
		attachPreviewProcessGroup(cmd)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate[0], err))
			continue
		}
		return nil
	}
	if len(avail) == 0 {
		return fmt.Errorf("%w (tried %s)", ErrNoAudioPlayer, previewPlayerHint())
	}
	return fmt.Errorf("voice preview: every player failed (%s)", strings.Join(failures, "; "))
}

// audioPlayerCandidates lists the player commands to try, in preference order.
func audioPlayerCandidates(path string) [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{
			{"afplay", path},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
		}
	case "windows":
		quoted := strings.ReplaceAll(path, "'", "''")
		return [][]string{
			{"powershell", "-NoProfile", "-Command",
				fmt.Sprintf("(New-Object Media.SoundPlayer '%s').PlaySync()", quoted)},
		}
	default:
		return [][]string{
			{"paplay", path},
			{"pw-play", path},
			{"ffplay", "-nodisp", "-autoexit", "-loglevel", "quiet", path},
			{"mpv", "--no-video", "--really-quiet", path},
			{"play", "-q", path},
			{"aplay", "-q", path},
		}
	}
}

func previewPlayerHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "afplay, ffplay"
	case "windows":
		return "powershell"
	default:
		return "paplay, pw-play, ffplay, mpv, play, aplay"
	}
}

// ParseVoicePreviewVariant accepts the CLI spelling of a sample index
// ("", "1".."3", "all") and returns the variant to play. "all" returns 0 with
// playAll set, which callers handle by iterating the variants.
func ParseVoicePreviewVariant(arg string) (variant int, playAll bool, err error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return 1, false, nil
	}
	if strings.EqualFold(arg, "all") {
		return 0, true, nil
	}
	n, convErr := strconv.Atoi(arg)
	if convErr != nil {
		return 0, false, fmt.Errorf("voice: sample %q is not a number (1-%d, or all)", arg, VoicePreviewVariants)
	}
	if n < 1 || n > VoicePreviewVariants {
		return 0, false, fmt.Errorf("voice: sample %d out of range (1-%d)", n, VoicePreviewVariants)
	}
	return n, false, nil
}
