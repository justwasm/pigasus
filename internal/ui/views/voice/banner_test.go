package voice

import (
	"strings"
	"testing"
	"time"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

func TestFormatTimer(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{5 * time.Second, "00:05"},
		{65 * time.Second, "01:05"},
		{122 * time.Second, "02:02"},
	}
	for _, tc := range cases {
		got := FormatTimer(tc.d)
		if got != tc.want {
			t.Errorf("FormatTimer(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatBanner_Phases(t *testing.T) {
	// PhaseNone
	if got := FormatBanner(PhaseNone, 0, 80); got != "" {
		t.Errorf("PhaseNone banner = %q, want empty", got)
	}

	// PhaseConnecting
	conn := FormatBanner(PhaseConnecting, 0, 80)
	plainConn := termcolor.StripSGR(conn)
	if !strings.Contains(plainConn, "Connecting...") {
		t.Errorf("connecting banner missing 'Connecting...': %q", plainConn)
	}
	if !strings.Contains(plainConn, "esc to cancel") {
		t.Errorf("connecting banner missing 'esc to cancel': %q", plainConn)
	}

	// PhaseRecording even second (solid dot)
	rec0 := FormatBanner(PhaseRecording, 0, 80)
	if !strings.Contains(rec0, "●") {
		t.Errorf("even recording banner missing solid dot: %q", rec0)
	}
	if !strings.Contains(rec0, "00:00") {
		t.Errorf("recording banner missing '00:00': %q", rec0)
	}

	// PhaseRecording odd second (hollow dot)
	rec1 := FormatBanner(PhaseRecording, 3*time.Second, 80)
	if !strings.Contains(rec1, "○") {
		t.Errorf("odd recording banner missing hollow dot: %q", rec1)
	}
	if !strings.Contains(rec1, "00:03") {
		t.Errorf("recording banner missing '00:03': %q", rec1)
	}

	// PhaseFinishing
	fin := FormatBanner(PhaseFinishing, 210*time.Second, 80)
	plainFin := termcolor.StripSGR(fin)
	if !strings.Contains(plainFin, "Finishing up...") {
		t.Errorf("finishing banner missing 'Finishing up...': %q", plainFin)
	}
}

func TestFormatBanner_Padding(t *testing.T) {
	// Narrow width should still yield at least 1 space padding
	narrow := FormatBanner(PhaseConnecting, 0, 10)
	if !strings.HasSuffix(narrow, "\n") {
		t.Errorf("banner should end in newline, got %q", narrow)
	}
	if BannerVisibleLen(narrow) <= 0 {
		t.Errorf("visible len should be > 0, got %d", BannerVisibleLen(narrow))
	}
}

func TestKeepTail(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		maxCols int
		want    string
	}{
		{
			name:    "fits",
			text:    "You: hello",
			maxCols: 20,
			want:    "You: hello",
		},
		{
			name:    "truncates with prefix",
			text:    "You: hello world this is a test",
			maxCols: 18,
			want:    "You: …is is a test",
		},
		{
			name:    "no prefix",
			text:    "hello world this is a test",
			maxCols: 15,
			want:    "…this is a test",
		},
		{
			name:    "zero cols",
			text:    "You: hello",
			maxCols: 0,
			want:    "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := KeepTail(tc.text, tc.maxCols)
			if got != tc.want {
				t.Errorf("KeepTail(%q, %d) = %q, want %q", tc.text, tc.maxCols, got, tc.want)
			}
		})
	}
}

func TestFormatBannerLive(t *testing.T) {
	// 1. Listening (unmuted, empty prompt -> esc hang up)
	banner := FormatBannerLive(12*time.Minute+32*time.Second, 80, false, false, false, "", true)
	plain := termcolor.StripSGR(banner)
	if !strings.Contains(plain, "Live 12:32 · listening · f6 mute · esc hang up") {
		t.Errorf("expected canonical listening banner with esc hang up, got %q", plain)
	}

	// 2. Non-empty prompt -> f7 hang up
	bannerNonEmpty := FormatBannerLive(12*time.Minute+32*time.Second, 80, false, false, false, "", false)
	plainNonEmpty := termcolor.StripSGR(bannerNonEmpty)
	if !strings.Contains(plainNonEmpty, "Live 12:32 · listening · f6 mute · f7 hang up") {
		t.Errorf("expected canonical banner with f7 hang up, got %q", plainNonEmpty)
	}

	// 3. Muted (empty prompt -> esc hang up)
	bannerMuted := FormatBannerLive(12*time.Minute+32*time.Second, 80, true, false, false, "", true)
	plainMuted := termcolor.StripSGR(bannerMuted)
	if !strings.Contains(plainMuted, "Live 12:32 · muted · f6 unmute · esc hang up") {
		t.Errorf("expected canonical muted banner with esc hang up, got %q", plainMuted)
	}

	// 4. Muted (non-empty prompt -> f7 hang up)
	bannerMutedNonEmpty := FormatBannerLive(12*time.Minute+32*time.Second, 80, true, false, false, "", false)
	plainMutedNonEmpty := termcolor.StripSGR(bannerMutedNonEmpty)
	if !strings.Contains(plainMutedNonEmpty, "Live 12:32 · muted · f6 unmute · f7 hang up") {
		t.Errorf("expected canonical muted banner with f7 hang up, got %q", plainMutedNonEmpty)
	}

	// 5. Speaking
	bannerSpeaking := FormatBannerLive(12*time.Minute+32*time.Second, 80, false, true, false, "", true)
	plainSpeaking := termcolor.StripSGR(bannerSpeaking)
	if !strings.Contains(plainSpeaking, "Live 12:32 · speaking · f6 mute · esc hang up") {
		t.Errorf("expected canonical speaking banner, got %q", plainSpeaking)
	}

	// 6. Thinking
	bannerThinking := FormatBannerLive(12*time.Minute+32*time.Second, 80, false, false, true, "", true)
	plainThinking := termcolor.StripSGR(bannerThinking)
	if !strings.Contains(plainThinking, "Live 12:32 · thinking · f6 mute · esc hang up") {
		t.Errorf("expected canonical thinking banner, got %q", plainThinking)
	}

	// 7. Speech preview included
	bannerPreview := FormatBannerLive(12*time.Minute+32*time.Second, 100, false, false, false, "what is this repo?", true)
	plainPreview := termcolor.StripSGR(bannerPreview)
	if !strings.Contains(plainPreview, "listening · You: what is this repo? · f6 mute · esc hang up") {
		t.Errorf("expected preview in banner, got %q", plainPreview)
	}

	// 8. User specific scenario: 15:36 listening · You: I'll go ahead and · f6 mute · esc hang up
	bannerUserEx := FormatBannerLive(15*time.Minute+36*time.Second, 120, false, false, false, "I'll go ahead and", true)
	plainUserEx := termcolor.StripSGR(bannerUserEx)
	expectedRow := "  ● Live 15:36 · listening · You: I'll go ahead and · f6 mute · esc hang up"
	if !strings.Contains(plainUserEx, expectedRow) {
		t.Errorf("expected exact user example %q, got %q", expectedRow, plainUserEx)
	}
}
