// Package narration provides narration text selection and feedback state
// for introductory context in multi-step tasks.
package narration

import (
	"strings"
	"time"
)

const (
	// DisabledMsg is the wording when narration is flipped off.
	DisabledMsg = "Narration disabled."
	// AlreadyDisabledMsg is the wording when narration is already off.
	AlreadyDisabledMsg = "Narration already disabled."
)

// SelectText resolves narration args into a catalogue paragraph or verbatim text.
func SelectText(args string, sessionStart time.Time) string {
	switch {
	case args == "":
		return SelectNarration("", "default", "", sessionStart)
	case args == "auto":
		return SelectNarration("", "auto", "", sessionStart)
	case strings.HasPrefix(args, "auto "):
		task := strings.TrimSpace(args[len("auto "):])
		return SelectNarration("", "auto", task, sessionStart)
	default:
		return SelectNarration(args, "", "", sessionStart)
	}
}

// Feedback resolves "/narration <args>" given current muted state and session start time.
func Feedback(args string, sessionStart time.Time, currentlyMuted bool) (text, echo string, mutedNow, already bool) {
	args = strings.TrimSpace(args)

	switch args {
	case "off", "OFF", "Off":
		if currentlyMuted {
			return "", AlreadyDisabledMsg, false, true
		}
		return "", DisabledMsg, true, false
	}

	if currentlyMuted {
		return "", "", false, true
	}

	text = SelectText(args, sessionStart)
	echo = text
	return text, echo, false, false
}
