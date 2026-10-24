package voice

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/justwasm/pigasus/internal/termcolor"
)

// Phase represents the current stage of voice dictation.
type Phase int

const (
	PhaseNone       Phase = 0
	PhaseConnecting Phase = 1
	PhaseRecording  Phase = 2
	PhaseFinishing  Phase = 3
)

const (
	gray238 = "\x1b[38;5;238m"
	sgrOff  = "\x1b[0m"
	redDot  = "\x1b[38;5;196m●\x1b[0m"
	hollowDot = "\x1b[38;5;196m○\x1b[0m"
)

// FormatTimer formats a duration as mm:ss (e.g. "02:02").
func FormatTimer(d time.Duration) string {
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

// BannerVisibleLen returns the visible display column count of an ANSI string.
func BannerVisibleLen(s string) int {
	return runewidth.StringWidth(termcolor.StripSGR(s))
}

// BannerTexts returns the left and right banner labels for a given phase and elapsed time.
func BannerTexts(phase Phase, elapsed time.Duration) (left, right string, ok bool) {
	switch phase {
	case PhaseConnecting:
		return "  Connecting... ", gray238 + "esc to cancel" + sgrOff, true
	case PhaseRecording:
		dot := redDot
		if int(elapsed.Seconds())%2 == 1 {
			dot = hollowDot
		}
		left = "  " + dot + " Recording " + FormatTimer(elapsed) + " "
		right = "enter or f5 to insert " + gray238 + "· esc to discard" + sgrOff
		return left, right, true
	case PhaseFinishing:
		left = "  Finishing up...  "
		right = "enter or f5 to insert what you have " + gray238 + "· esc to discard" + sgrOff
		return left, right, true
	default:
		return "", "", false
	}
}

// FormatBanner renders the full overlay banner for voice dictation matching AGY 1.4.3.
func FormatBanner(phase Phase, elapsed time.Duration, width int) string {
	if width <= 0 {
		width = 80
	}
	left, right, ok := BannerTexts(phase, elapsed)
	if !ok {
		return ""
	}
	pad := width - BannerVisibleLen(left) - BannerVisibleLen(right)
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right + "\n"
}
