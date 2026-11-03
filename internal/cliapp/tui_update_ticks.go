package cliapp

import (
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
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
	if msg.gen != m.store.BashGen {
		return m, nil
	}
	m.finishBashCommand(msg)
	if len(m.store.BashQueue) > 0 {
		next := m.store.BashQueue[0]
		m.store.BashQueue = m.store.BashQueue[1:]
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
	if m.panel.kind != panelregistry.Usage {
		return m, nil
	}
	m.quotaFrame++
	if m.panel.quota.state == quotaLoading && m.quotaFrame >= usageQuotaLoadingTicks {
		quotaStubRefills(m)
	}
	return m, usagepanel.QuotaTick()
}

func (m *runMode) updateCompactStartMsg(msg compactStartMsg) (tea.Model, tea.Cmd) {
	m.store.CompactStart = msg.StartedAt
	m.store.CompactEnd = time.Time{}
	m.store.CompactSpinnerFrame = 0
	m.store.CompactReason = msg.Reason
	m.store.CompactTokensBefore = 0
	m.store.CompactTokensAfter = 0
	return m, compactSpinnerTick()
}

func (m *runMode) updateCompactEndMsg(msg compactEndMsg) (tea.Model, tea.Cmd) {
	m.store.CompactEnd = msg.EndedAt
	m.store.CompactReason = msg.Reason
	m.store.CompactTokensBefore = msg.TokensBefore
	m.store.CompactTokensAfter = msg.EstimatedTokensAfter
	return m, nil
}

func (m *runMode) updateCompactSpinnerTickMsg() (tea.Model, tea.Cmd) {
	if m.store.CompactStart.IsZero() || !m.store.CompactEnd.IsZero() {
		return m, nil
	}
	m.store.CompactSpinnerFrame++
	if m.store.CompactSpinnerFrame >= 8 {
		m.store.CompactSpinnerFrame = 0
	}
	return m, compactSpinnerTick()
}

func (m *runMode) updatePluginBlinkMsg() (tea.Model, tea.Cmd) {
	// Cycle 381: the blink phase is the panel's own state — the panel
	// lives in internal/ui/views/pluginsui — so the host only flips it
	// and re-arms the tick while a blinking cursor is on screen.
	p := m.panel.plg
	if p == nil {
		return m, nil
	}
	if m.panel.kind == panelregistry.Plugin && p.Blinking() {
		p.ToggleBlink()
		return m, pluginsui.Blink()
	}
	p.SetBlink(false)
	return m, nil
}

// updateCSTickMsg drains the matches the runner has produced and arms
// the next tick.
//
// Cycle 381: the drain, the spinner and the tick belong to the
// /codesearch panel (internal/ui/views/codesearchpanel); the host only
// carries the tick message.
func (m *runMode) updateCSTickMsg() (tea.Model, tea.Cmd) {
	if m.panel.kind != panelregistry.CodeSearch {
		return m, nil
	}
	p := m.codesearchPanel()
	if p == nil || p.Search() == nil {
		return m, nil
	}
	_, closed := p.Drain()
	if closed {
		return m, nil
	}
	return m, p.Tick()
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
				if m.panel.kind == panelregistry.Accounts {
					m.accountsPanel().Reload()
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
	case btwTickMsg:
		if m.IsBtwActive() {
			return m, btwTick(), true
		}
		return m, nil, true
	case btwDoneMsg:
		mod, cmd := m.updateBtwDoneMsg(msg)
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
		// The tick builds the payload on this goroutine — the session's
		// state is not safe to read from another one — and runs the
		// scripts in a goroutine of their own.
		m.runExternalStatusLineTick()
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
		if m.panel.kind == panelregistry.Usage {
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
		// The queue, the error line and the refreshed rows are the
		// panel's.
		return m, m.accountsPanel().QuotaDone(msg.email, msg.summary != nil, msg.err), true
	case providerpanel.VerifyMsg:
		// The /provider panel's verification probe came back. The panel
		// may have been closed while the request was in flight.
		if m.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyVerifyMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthResultMsg:
		if m.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyOAuthResultMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthTickMsg:
		if m.panel.kind == panelregistry.Provider && m.panel.prov != nil {
			return m, m.panel.prov.ApplyOAuthTick(), true
		}
		return m, nil, true
	case usageQuotaTickMsg:
		mod, cmd := m.updateUsageQuotaTickMsg()
		return mod, cmd, true
	case pluginsui.BlinkMsg:
		mod, cmd := m.updatePluginBlinkMsg()
		return mod, cmd, true
	case codesearchpanel.TickMsg:
		mod, cmd := m.updateCSTickMsg()
		return mod, cmd, true
	}
	return m, nil, false
}
