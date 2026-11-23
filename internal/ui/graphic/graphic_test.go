package graphic

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func mapGetenv(env map[string]string) func(string) string {
	return func(k string) string {
		return env[k]
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantProto Protocol
		wantMux   bool
		wantProbe bool
	}{
		{
			name:      "empty env falls back to none",
			env:       map[string]string{},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "kitty via KITTY_WINDOW_ID",
			env:       map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-256color"},
			wantProto: ProtocolKitty,
			wantMux:   false,
			wantProbe: true,
		},
		{
			name:      "kitty via TERM=xterm-kitty",
			env:       map[string]string{"TERM": "xterm-kitty"},
			wantProto: ProtocolKitty,
			wantMux:   false,
			wantProbe: true,
		},
		{
			name:      "ghostty via GHOSTTY_RESOURCES_DIR",
			env:       map[string]string{"GHOSTTY_RESOURCES_DIR": "/Applications/Ghostty.app/Contents/Resources/ghostty"},
			wantProto: ProtocolKitty,
			wantMux:   false,
			wantProbe: true,
		},
		{
			name:      "ghostty via TERM=xterm-ghostty",
			env:       map[string]string{"TERM": "xterm-ghostty"},
			wantProto: ProtocolKitty,
			wantMux:   false,
			wantProbe: true,
		},
		{
			name:      "ghostty via TERM_PROGRAM=ghostty",
			env:       map[string]string{"TERM_PROGRAM": "ghostty"},
			wantProto: ProtocolKitty,
			wantMux:   false,
			wantProbe: true,
		},
		{
			name:      "iterm2 via TERM_PROGRAM=iTerm.app",
			env:       map[string]string{"TERM_PROGRAM": "iTerm.app"},
			wantProto: ProtocolITerm2,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "vscode suppressed even with leaked KITTY_WINDOW_ID",
			env:       map[string]string{"TERM_PROGRAM": "vscode", "KITTY_WINDOW_ID": "1"},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "wezterm suppressed even with xterm-kitty TERM",
			env:       map[string]string{"TERM_PROGRAM": "WezTerm", "TERM": "xterm-kitty"},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "apple terminal suppressed",
			env:       map[string]string{"TERM_PROGRAM": "Apple_Terminal"},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "tmux suppresses kitty",
			env:       map[string]string{"TMUX": "/tmp/tmux-1000/default,123,0", "KITTY_WINDOW_ID": "1", "TERM": "tmux-256color"},
			wantProto: ProtocolNone,
			wantMux:   true,
			wantProbe: false,
		},
		{
			name:      "screen STY suppresses ghostty",
			env:       map[string]string{"STY": "12345.pts-0.host", "GHOSTTY_RESOURCES_DIR": "/usr/share/ghostty", "TERM": "screen-256color"},
			wantProto: ProtocolNone,
			wantMux:   true,
			wantProbe: false,
		},
		{
			name:      "zellij suppresses kitty and iterm2",
			env:       map[string]string{"ZELLIJ": "0", "TERM_PROGRAM": "iTerm.app", "KITTY_WINDOW_ID": "2"},
			wantProto: ProtocolNone,
			wantMux:   true,
			wantProbe: false,
		},
		{
			name:      "TERM=screen prefix suppresses kitty",
			env:       map[string]string{"TERM": "screen", "KITTY_WINDOW_ID": "1"},
			wantProto: ProtocolNone,
			wantMux:   true,
			wantProbe: false,
		},
		{
			name:      "TERM=tmux prefix suppresses ghostty",
			env:       map[string]string{"TERM": "tmux-256color", "GHOSTTY_RESOURCES_DIR": "/tmp/ghostty"},
			wantProto: ProtocolNone,
			wantMux:   true,
			wantProbe: false,
		},
		{
			name:      "CLI_GRAPHICS=kitty overrides tmux",
			env:       map[string]string{"CLI_GRAPHICS": "kitty", "TMUX": "/tmp/tmux,1,0", "TERM": "tmux-256color"},
			wantProto: ProtocolKitty,
			wantMux:   true,
			wantProbe: true,
		},
		{
			name:      "CLI_GRAPHICS=kitty overrides vscode",
			env:       map[string]string{"CLI_GRAPHICS": "kitty", "TERM_PROGRAM": "vscode"},
			wantProto: ProtocolKitty,
			wantMux:   false,
			wantProbe: true,
		},
		{
			name:      "CLI_GRAPHICS=iterm2 overrides tmux",
			env:       map[string]string{"CLI_GRAPHICS": "iterm2", "TMUX": "/tmp/tmux,1,0"},
			wantProto: ProtocolITerm2,
			wantMux:   true,
			wantProbe: false,
		},
		{
			name:      "CLI_GRAPHICS=none disables kitty",
			env:       map[string]string{"CLI_GRAPHICS": "none", "KITTY_WINDOW_ID": "1"},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "CLI_GRAPHICS=off disables ghostty",
			env:       map[string]string{"CLI_GRAPHICS": "off", "GHOSTTY_RESOURCES_DIR": "/tmp/ghostty"},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
		{
			name:      "CLI_GRAPHICS=ascii disables iterm2",
			env:       map[string]string{"CLI_GRAPHICS": "ascii", "TERM_PROGRAM": "iTerm.app"},
			wantProto: ProtocolNone,
			wantMux:   false,
			wantProbe: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getenv := mapGetenv(tc.env)
			caps := Detect(getenv)
			if caps.Protocol != tc.wantProto {
				t.Fatalf("Detect().Protocol = %v, want %v", caps.Protocol, tc.wantProto)
			}
			if caps.InMultiplexer != tc.wantMux {
				t.Fatalf("Detect().InMultiplexer = %v, want %v", caps.InMultiplexer, tc.wantMux)
			}
			if gotProbe := ShouldProbe(getenv); gotProbe != tc.wantProbe {
				t.Fatalf("ShouldProbe() = %v, want %v", gotProbe, tc.wantProbe)
			}
			seq := KittyProbeSequenceWithEnv(31, getenv)
			if tc.wantProbe {
				wantSeq := "\x1b_Ga=q,i=31,s=1,v=1,f=32,t=d;AAAA\x1b\\"
				if seq != wantSeq {
					t.Fatalf("KittyProbeSequenceWithEnv(31) = %q, want %q", seq, wantSeq)
				}
			} else if seq != "" {
				t.Fatalf("KittyProbeSequenceWithEnv(31) = %q, want empty string", seq)
			}
		})
	}
}

func TestKittyProbeSequenceEnv(t *testing.T) {
	t.Setenv("CLI_GRAPHICS", "")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	t.Setenv("ZELLIJ", "")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "7")

	if got, want := KittyProbeSequence(42), "\x1b_Ga=q,i=42,s=1,v=1,f=32,t=d;AAAA\x1b\\"; got != want {
		t.Fatalf("KittyProbeSequence(42) = %q, want %q", got, want)
	}

	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	if got := KittyProbeSequence(42); got != "" {
		t.Fatalf("KittyProbeSequence(42) inside tmux = %q, want empty", got)
	}
}

func TestEncodeKittyPNGChunking(t *testing.T) {
	// 10,000 bytes -> ~13,336 base64 chars -> 4 chunks (4096, 4096, 4096, 1048).
	raw := make([]byte, 10000)
	for i := range raw {
		raw[i] = byte(i % 251)
	}

	out := EncodeKittyPNG(raw, ImageOptions{
		ID:          17,
		PlacementID: 3,
		Cols:        20,
		Rows:        10,
		Quiet:       true,
		ZIndex:      -1,
	})

	// Split into APC frames ending with ST (\x1b\\).
	frames := strings.Split(out, "\x1b\\")
	if frames[len(frames)-1] != "" {
		t.Fatalf("expected trailing ST terminator, got leftover %q", frames[len(frames)-1])
	}
	frames = frames[:len(frames)-1]

	if len(frames) != 4 {
		t.Fatalf("expected 4 chunks for 10000-byte payload, got %d", len(frames))
	}

	var reassembled strings.Builder
	for idx, frame := range frames {
		if !strings.HasPrefix(frame, "\x1b_G") {
			t.Fatalf("chunk %d missing APC prefix: %q", idx, frame[:min(16, len(frame))])
		}
		body := strings.TrimPrefix(frame, "\x1b_G")
		ctrl, payload, ok := strings.Cut(body, ";")
		if !ok {
			t.Fatalf("chunk %d missing ';' separator", idx)
		}
		if len(payload) > kittyMaxChunkSize {
			t.Fatalf("chunk %d payload length %d exceeds %d", idx, len(payload), kittyMaxChunkSize)
		}
		if idx == 0 {
			for _, wantParam := range []string{"a=T", "f=100", "i=17", "p=3", "c=20", "r=10", "q=2", "z=-1", "m=1"} {
				if !strings.Contains(ctrl, wantParam) {
					t.Fatalf("first chunk control %q missing %q", ctrl, wantParam)
				}
			}
		} else if idx < len(frames)-1 {
			if ctrl != "m=1" {
				t.Fatalf("intermediate chunk %d control = %q, want m=1", idx, ctrl)
			}
		} else {
			if ctrl != "m=0" {
				t.Fatalf("final chunk %d control = %q, want m=0", idx, ctrl)
			}
		}
		reassembled.WriteString(payload)
	}

	decoded, err := base64.StdEncoding.DecodeString(reassembled.String())
	if err != nil {
		t.Fatalf("base64 decode reassembled payload: %v", err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatalf("reassembled payload mismatch: got %d bytes, want %d bytes", len(decoded), len(raw))
	}
}

func TestEncodeKittyPNGSingleChunkAndDelete(t *testing.T) {
	raw := []byte("tiny-png-payload")
	out := EncodeKittyPNG(raw, ImageOptions{ID: 5, Quiet: true})
	wantPrefix := "\x1b_Ga=T,f=100,i=5,q=2,m=0;"
	if !strings.HasPrefix(out, wantPrefix) {
		t.Fatalf("single chunk = %q, want prefix %q", out, wantPrefix)
	}
	if !strings.HasSuffix(out, "\x1b\\") {
		t.Fatalf("single chunk missing ST suffix: %q", out)
	}

	if got, want := DeleteKittyImage(5), "\x1b_Ga=d,d=I,i=5,q=2\x1b\\"; got != want {
		t.Fatalf("DeleteKittyImage(5) = %q, want %q", got, want)
	}
}

func TestEncodeITerm2(t *testing.T) {
	raw := []byte("fake-png-bytes")
	out := EncodeITerm2(raw, ImageOptions{Cols: 24, Rows: 12})
	want := "\x1b]1337;File=inline=1;size=14;width=24;height=12:" +
		base64.StdEncoding.EncodeToString(raw) + "\x07"
	if out != want {
		t.Fatalf("EncodeITerm2() = %q, want %q", out, want)
	}
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderHalfBlockAndQueenBee(t *testing.T) {
	bee := QueenBeeImage()
	if bee.Bounds().Dx() != 16 || bee.Bounds().Dy() != 12 {
		t.Fatalf("QueenBeeImage bounds = %v, want 16x12", bee.Bounds())
	}

	out := RenderHalfBlock(bee, 16, 0)
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Fatalf("expected 24-bit ANSI foreground sequence in half-block output")
	}
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Fatalf("expected 24-bit ANSI background sequence in half-block output")
	}
	if !strings.Contains(out, "▀") {
		t.Fatalf("expected upper half-block character '▀' in output")
	}

	lines := strings.Split(out, "\n")
	if len(lines) != 6 {
		t.Fatalf("16x12 half-block line count = %d, want 6", len(lines))
	}
	for i, line := range lines {
		plain := ansiEscapeRe.ReplaceAllString(line, "")
		if got := utf8.RuneCountInString(plain); got != 16 {
			t.Fatalf("line %d visible width = %d (%q), want 16", i, got, plain)
		}
	}

	// Downscaling to 8 cols should produce 3 rows of width 8.
	scaled := RenderHalfBlock(bee, 8, 0)
	scaledLines := strings.Split(scaled, "\n")
	if len(scaledLines) != 3 {
		t.Fatalf("scaled 8-col half-block line count = %d, want 3", len(scaledLines))
	}
	for i, line := range scaledLines {
		plain := ansiEscapeRe.ReplaceAllString(line, "")
		if got := utf8.RuneCountInString(plain); got != 8 {
			t.Fatalf("scaled line %d visible width = %d, want 8", i, got)
		}
	}
}

func TestRenderQueenBeePixelArtAndDispatch(t *testing.T) {
	t.Setenv("CLI_GRAPHICS", "none")
	asciiBee := RenderQueenBeePixelArt(16)
	if !strings.Contains(asciiBee, "▀") || !strings.Contains(asciiBee, "\x1b[38;2;") {
		t.Fatalf("RenderQueenBeePixelArt with CLI_GRAPHICS=none did not emit half-block ANSI")
	}

	t.Setenv("CLI_GRAPHICS", "kitty")
	kittyBee := RenderQueenBeePixelArt(16)
	if !strings.HasPrefix(kittyBee, "\x1b_Ga=T,f=100") {
		t.Fatalf("RenderQueenBeePixelArt with CLI_GRAPHICS=kitty = %q, want Kitty APC", kittyBee[:min(32, len(kittyBee))])
	}

	// Verify RenderImage and RenderPNG dispatch across protocols.
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 200, B: 0, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	gotKitty, err := RenderImage(img, ImageOptions{ID: 9, Quiet: true}, Capabilities{Protocol: ProtocolKitty})
	if err != nil || !strings.HasPrefix(gotKitty, "\x1b_Ga=T,f=100,i=9,q=2") {
		t.Fatalf("RenderImage(Kitty) = (%q, %v)", gotKitty, err)
	}

	gotITerm, err := RenderPNG(pngBuf.Bytes(), ImageOptions{Cols: 4, Rows: 2}, Capabilities{Protocol: ProtocolITerm2})
	if err != nil || !strings.HasPrefix(gotITerm, "\x1b]1337;File=inline=1;") {
		t.Fatalf("RenderPNG(iTerm2) = (%q, %v)", gotITerm, err)
	}

	gotForcedASCII, err := RenderPNG(pngBuf.Bytes(), ImageOptions{ForceASCII: true}, Capabilities{Protocol: ProtocolKitty})
	if err != nil || !strings.Contains(gotForcedASCII, "▀") {
		t.Fatalf("RenderPNG(ForceASCII) = (%q, %v)", gotForcedASCII, err)
	}
}
