package voice

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/palette"
)

// Phase represents the current stage of voice dictation.
type Phase int

const (
	PhaseNone       Phase = 0
	PhaseConnecting Phase = 1
	PhaseRecording  Phase = 2
	PhaseFinishing  Phase = 3
	PhaseLive       Phase = 4
)

const (
	gray238   = "\x1b[38;5;238m"
	sgrOff    = "\x1b[0m"
	redDot    = "\x1b[38;5;196m●\x1b[0m"
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

// KeepTail truncates text from the left to keep the most recent tail visible,
// matching upstream Jetski/AGY live.keepTail.
// If the text starts with "You: ", the prefix is preserved and the remainder
// is truncated with an ellipsis "…".
func KeepTail(text string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	w := ansi.StringWidth(text)
	if w <= maxCols {
		return text
	}
	prefix := ""
	rem := text
	if strings.HasPrefix(text, "You: ") {
		prefix = "You: "
		rem = text[5:]
		maxCols -= 5
		if maxCols <= 1 {
			return prefix + "…"
		}
	}
	remW := ansi.StringWidth(rem)
	if remW <= maxCols {
		return prefix + rem
	}
	toRemove := remW - maxCols + 1
	tail := ansi.TruncateLeft(rem, toRemove, "")
	return prefix + "…" + tail
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
	case PhaseLive:
		dot := redDot
		if int(elapsed.Seconds())%2 == 1 {
			dot = hollowDot
		}
		left = "  " + dot + " Live " + FormatTimer(elapsed) + " "
		right = "m to mute " + gray238 + "· esc to hang up" + sgrOff
		return left, right, true
	default:
		return "", "", false
	}
}

// FormatBannerLive renders the status line and bottom rule for PhaseLive matching upstream agy.
func FormatBannerLive(elapsed time.Duration, width int, muted bool, speaking bool, thinking bool, preview string, promptEmpty bool) string {
	timeStr := FormatTimer(elapsed)

	dotColor := "\x1b[38;5;221m" // yellow
	state := "listening"
	muteAction := "mute"
	if muted {
		dotColor = "\x1b[38;5;109m" // slate
		state = "muted"
		muteAction = "unmute"
	} else if speaking {
		dotColor = "\x1b[38;5;221m" // yellow
		state = "speaking"
	} else if thinking {
		dotColor = "\x1b[38;5;111m" // cyan
		state = "thinking"
	}

	left := fmt.Sprintf("  %s●\x1b[39m Live \x1b[38;5;109m%s · \x1b[39m%s",
		dotColor, timeStr, state)
	stopKey := "f7"
	if promptEmpty {
		stopKey = "esc"
	}
	hints := fmt.Sprintf("\x1b[38;5;111mf6\x1b[38;5;109m %s\x1b[38;5;109m · \x1b[38;5;111m%s\x1b[38;5;109m hang up\x1b[39m",
		muteAction, stopKey)
	sep := "\x1b[38;5;109m · \x1b[39m"

	line := left + sep + hints
	preview = strings.TrimSpace(strings.ReplaceAll(preview, "\n", " "))

	if preview != "" && width > 0 {
		if !strings.HasPrefix(preview, "You: ") {
			preview = "You: " + preview
		}
		leftW := BannerVisibleLen(left)
		hintsW := BannerVisibleLen(hints)
		sepW := 3 // " · "
		avail := width - leftW - hintsW - (2 * sepW)
		if avail >= 8 {
			truncatedPreview := KeepTail(preview, avail)
			line = left + sep + "\x1b[38;5;109m" + truncatedPreview + "\x1b[39m" + sep + hints
		}
	}

	if width > 0 && BannerVisibleLen(line) > width {
		line = ansi.Truncate(line, width, "…")
	}

	rule := ""
	if width > 0 {
		rule = palette.Rule(width) + "\n"
	}
	return line + "\n" + rule
}

// FormatBanner renders the full overlay banner for voice dictation matching AGY 1.4.3.
func FormatBanner(phase Phase, elapsed time.Duration, width int) string {
	return FormatBannerMuted(phase, elapsed, width, false)
}

// FormatBannerMuted renders the voice banner with optional mute indicator.
func FormatBannerMuted(phase Phase, elapsed time.Duration, width int, muted bool) string {
	if phase == PhaseLive {
		return FormatBannerLive(elapsed, width, muted, false, false, "", true)
	}
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
