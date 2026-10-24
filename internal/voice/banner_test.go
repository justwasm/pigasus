package voice

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/termcolor"
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
