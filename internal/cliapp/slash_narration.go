// slash_narration.go — cycle 250: the /narration slash command.
// Delegates pure narration selection and feedback logic to internal/narration.
package cliapp

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/narration"
)

func narrationSessionStart(m *runMode) time.Time {
	if !m.narrationStart.IsZero() {
		return m.narrationStart
	}
	return time.Time{}
}

func narrationSelectText(args string, sessionStart time.Time) string {
	return narration.SelectText(args, sessionStart)
}

func narrationFeedback(m *runMode, args string) (text, echo string, mutedNow, already bool) {
	text, echo, mutedNow, already = narration.Feedback(args, narrationSessionStart(m), m.narrationOff)
	if mutedNow {
		m.narrationOff = true
	}
	return text, echo, mutedNow, already
}

const narrationDisabledMsg = narration.DisabledMsg
const narrationAlreadyDisabledMsg = narration.AlreadyDisabledMsg

var narrationSampleSeeds = narration.SampleSeeds

var _ = narrationSampleSeeds

func narrationEchoToResponse(m *runMode, echo string) {
	if echo == "" {
		return
	}
	m.response = append(m.response, panelGray238+echo+panelGray238C)
	m.response = append(m.response, "")
}

func (m *runMode) handleNarrationSlash(args string) (bool, tea.Cmd) {
	text, _, mutedNow, already := narrationFeedback(m, args)
	if mutedNow || already {
		return true, nil
	}
	if text != "" {
		for _, ln := range strings.Split(text, "\n") {
			m.response = append(m.response, panelGray238+ln+panelGray238C)
		}
		m.response = append(m.response, "")
	}
	return true, nil
}
