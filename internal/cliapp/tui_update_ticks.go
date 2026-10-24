package cliapp

import (
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

func (m *runMode) updateCompactDoneMsg(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.appendCompactResult(msg)
	if msg.CustomInstructions != "" && msg.Err == nil && !msg.Aborted {
		return m, m.streamPrompt(msg.CustomInstructions)
	}
	return m, nil
}

func (m *runMode) updateBashDoneMsg(msg bashDoneMsg) (tea.Model, tea.Cmd) {
	// A preempted command's completion arrives after its successor started;
	// its generation is stale, so it must not touch the live block.
	if msg.gen != m.bashGen {
		return m, nil
	}
	m.finishBashCommand(msg)
	if len(m.bashQueue) > 0 {
		next := m.bashQueue[0]
		m.bashQueue = m.bashQueue[1:]
		return m, tea.Batch(m.startBashCommand(next), tea.ClearScreen)
	}
	return m, tea.ClearScreen
}

func (m *runMode) updateSpinnerTickMsg(msg sp.TickMsg) (tea.Model, tea.Cmd) {
	if m.streaming {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.spinFrame++
		return m, cmd
	}
	return m, nil
}

func (m *runMode) updateUsageQuotaTickMsg() (tea.Model, tea.Cmd) {
	if m.panel.kind != panelUsage {
		return m, nil
	}
	m.quotaFrame++
	if m.panel.quota.state == quotaLoading && m.quotaFrame >= usageQuotaLoadingTicks {
		quotaStubRefills(m)
	}
	return m, usageQuotaTick()
}

func (m *runMode) updateCompactStartMsg(msg compactStartMsg) (tea.Model, tea.Cmd) {
	m.compactStart = msg.StartedAt
	m.compactEnd = time.Time{}
	m.compactSpinnerFrame = 0
	m.compactReason = msg.Reason
	m.compactTokensBefore = 0
	m.compactTokensAfter = 0
	return m, compactSpinnerTick()
}

func (m *runMode) updateCompactEndMsg(msg compactEndMsg) (tea.Model, tea.Cmd) {
	m.compactEnd = msg.EndedAt
	m.compactReason = msg.Reason
	m.compactTokensBefore = msg.TokensBefore
	m.compactTokensAfter = msg.EstimatedTokensAfter
	return m, nil
}

func (m *runMode) updateCompactSpinnerTickMsg() (tea.Model, tea.Cmd) {
	if m.compactStart.IsZero() || !m.compactEnd.IsZero() {
		return m, nil
	}
	m.compactSpinnerFrame++
	if m.compactSpinnerFrame >= 8 {
		m.compactSpinnerFrame = 0
	}
	return m, compactSpinnerTick()
}

func (m *runMode) updatePluginBlinkMsg() (tea.Model, tea.Cmd) {
	if m.panel.kind == panelPlugin && (m.panel.pluginSearch || m.pluginLocalInstallActive()) {
		m.panel.pluginBlinkOn = !m.panel.pluginBlinkOn
		return m, pluginBlink()
	}
	m.panel.pluginBlinkOn = false
	return m, nil
}

func (m *runMode) updateCSTickMsg() (tea.Model, tea.Cmd) {
	if m.panel.kind != panelCodeSearch || m.panel.cs.search == nil {
		return m, nil
	}
	drained, closed := m.drainCodesearchMatches()
	if closed {
		return m, nil
	}
	if drained {
		return m, csTick(m)
	}
	return m, csTick(m)
}

func (m *runMode) updateCompactEvents(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case compactDoneMsg:
		mod, cmd := m.updateCompactDoneMsg(msg)
		return mod, cmd, true
	case genericCompactDoneMsg:
		cmd := m.applyGenericCompactResult(msg, msg.CustomInstructions)
		return m, cmd, true
	case compactStartMsg:
		mod, cmd := m.updateCompactStartMsg(msg)
		return mod, cmd, true
	case compactEndMsg:
		mod, cmd := m.updateCompactEndMsg(msg)
		return mod, cmd, true
	case compactSpinnerTickMsg:
		mod, cmd := m.updateCompactSpinnerTickMsg()
		return mod, cmd, true
	}
	return m, nil, false
}

func (m *runMode) updateTicksAndCompactMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if mod, cmd, handled := m.updateCompactEvents(msg); handled {
		return mod, cmd, true
	}
	if mod, cmd, handled := m.updateTickMsg(msg); handled {
		return mod, cmd, true
	}
	return m.updatePanelTickMsg(msg)
}

// updateTickMsg handles the messages that only advance the transcript's
// own timers: the plan tier, the bash mode, the spinner and the task
// list.
func (m *runMode) updateTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case planTierMsg:
		// The tier belongs to the account it was read for. A session
		// that follows a switch or a sign-in gets the new account's
		// tier; until that answer lands the session shows the email
		// alone, never the previous account's tier.
		if msg.email != "" {
			m.tierAccount = msg.email
			m.tierFetched = msg.tier != ""
			if msg.tier != "" {
				m.tier = msg.tier
				// The tier was cached in the registry, and the /accounts
				// panel shows the cached tier, so its rows have to be
				// re-read.
				if m.panel.kind == panelAccounts {
					m.reloadAccountsRows()
				}
			} else {
				// The server named no tier for this account (a free
				// or an ineligible one): the session shows the email
				// alone, not the previous account's tier and not a
				// placeholder.
				m.tier = ""
			}
		}
		return m, nil, true
	case bashDoneMsg:
		mod, cmd := m.updateBashDoneMsg(msg)
		return mod, cmd, true
	case sp.TickMsg:
		mod, cmd := m.updateSpinnerTickMsg(msg)
		return mod, cmd, true
	case tasksTickMsg:
		if m.consumeTasksDirty() {
			return m, armTasksTick(), true
		}
		return m, nil, true
	case statusLineExternalTickMsg:
		m.ensureExternalState().MarkTick()
		go m.runExternalStatusLineTick()
		return m, armExternalStatusLineTick(), true
	}
	return m, nil, false
}

// updatePanelTickMsg handles the ticks that refresh a panel's own state:
// the usage panel's quota summary, the /provider panel's verification
// probe, the usage quota timer, the plugin blink and the codesearch
// spinner.
func (m *runMode) updatePanelTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case quotaSummaryMsg:
		if m.panel.kind == panelUsage {
			if msg.summary != nil {
				m.panel.quota.summary = msg.summary
				m.panel.quota.loadedAt = time.Now()
				m.panel.quota.state = quotaLoaded
				// The summary is cached for the session, so the
				// /accounts panel can show the active account's
				// quota without querying it a second time.
				m.cacheQuota(m.email, msg.summary)
			} else if msg.err != nil {
				m.panel.quota.fetchErr = msg.err.Error()
			}
		}
		return m, nil, true
	case accountsQuotaMsg:
		return m, m.updateAccountsQuotaMsg(msg), true
	case providerVerifyMsg:
		// The /provider panel's verification probe came back. The panel
		// may have been closed while the request was in flight.
		if m.panel.kind == panelProvider {
			result := msg.result
			m.panel.prov.verifying = false
			m.panel.prov.verify = &result
		}
		return m, nil, true
	case usageQuotaTickMsg:
		mod, cmd := m.updateUsageQuotaTickMsg()
		return mod, cmd, true
	case pluginBlinkMsg:
		mod, cmd := m.updatePluginBlinkMsg()
		return mod, cmd, true
	case csTickMsg:
		mod, cmd := m.updateCSTickMsg()
		return mod, cmd, true
	}
	return m, nil, false
}
