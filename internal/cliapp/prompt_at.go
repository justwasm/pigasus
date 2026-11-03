// prompt_at.go — the "@" prefix: the subagent dispatch.
//
// The composer's "@" opens two different things:
//
//   - "@<name> <message>" addresses a named subagent — the message is
//     delivered to it and the turn continues without the model;
//   - "@<path>" opens the file-mention dropdown, whose rows the
//     composer completes with Tab.
//
// The second one is a panel — internal/ui/views/mentionpicker — and
// the host only routes its keys. This file owns the first one, which
// is the host's own business: it delivers a message to a subagent and
// echoes the reference's answer.
package cliapp

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	subagentrouter "github.com/justwasm/pigasus/internal/app/subagent/router"
)

// defaultRouter is the process-wide subagent router the "@<name>"
// dispatch resolves against.
var defaultRouter = subagentrouter.NewManager()

// atDispatchTimeout bounds the delivery of a message to a named
// subagent.
const atDispatchTimeout = 30 * 1000 * 1000 * 1000 // 30 seconds

// handlePromptAtPrefix dispatches a prompt that starts with "@" to a
// named subagent.
func (m *runMode) handlePromptAtPrefix(buf string) (bool, tea.Cmd) {
	if len(buf) == 0 || buf[0] != '@' {
		return false, nil
	}
	name, message, ok := subagentrouter.ParsePrompt(buf)
	if !ok {
		return false, nil
	}
	m.resetPromptForAt(buf)
	if message == "" {
		m.response = append(m.response, "", "> "+buf, "", subagentrouter.NamedSubagentEmptyMessage(name))
		m.saveSession()
		return true, nil
	}
	m.deliverAtSubagent(name, message, buf)
	return true, nil
}

// resetPromptForAt clears the composer after an "@" dispatch.
func (m *runMode) resetPromptForAt(buf string) {
	m.pushHistory(buf)
	m.input.Reset()
	m.streaming = false
	m.promptHistory.Reset()
	m.picker.Reset()
}

// deliverAtSubagent sends the message to the named subagent.
func (m *runMode) deliverAtSubagent(name, message, buf string) {
	if m.atRouter == nil {
		m.atRouter = defaultRouter
	}
	entry, ok := m.atRouter.Lookup(name)
	if !ok {
		m.response = append(m.response, "", "> "+buf, "", subagentrouter.NamedSubagentUnknownMessage(name))
		m.saveSession()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), atDispatchTimeout)
	defer cancel()
	if err := entry.Sender.Send(ctx, message); err != nil {
		m.response = append(m.response,
			"",
			"> "+buf,
			"",
			strings.TrimSpace("at-subagent: failed to deliver: "+err.Error()),
		)
		m.saveSession()
		return
	}
	m.response = append(m.response,
		"",
		cWarningBlueOpen+"> @"+entry.Name+" "+message+cWarningBlueClose,
		"",
		panelGray238+"delivered to \""+entry.Name+"\""+panelGray238C,
		"",
	)
	m.saveSession()
}
