package tui

import (
	"sync/atomic"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/logspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// ---- tui_update_ticks.go ----

func (m *RootModel) updateCompactDoneMsg(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.appendCompactResult(msg)
	if msg.CustomInstructions != "" && msg.Err == nil && !msg.Aborted {
		return m, m.streamPrompt(msg.CustomInstructions)
	}
	return m, nil
}

func (m *RootModel) updateBashDoneMsg(msg bashDoneMsg) (tea.Model, tea.Cmd) {
	// A preempted command's completion arrives after its successor started;
	// its generation is stale, so it must not touch the live block.
	if msg.gen != m.session.store.BashGen {
		return m, nil
	}
	m.finishBashCommand(msg)
	if len(m.session.store.BashQueue) > 0 {
		next := m.session.store.BashQueue[0]
		m.session.store.BashQueue = m.session.store.BashQueue[1:]
		return m, tea.Batch(m.startBashCommand(next), tea.ClearScreen)
	}
	return m, tea.ClearScreen
}

func (m *RootModel) updateSpinnerTickMsg(msg sp.TickMsg) (tea.Model, tea.Cmd) {
	if m.conversation.streaming || m.audio.thinking {
		var cmd tea.Cmd
		m.conversation.spinner, cmd = m.conversation.spinner.Update(msg)
		if cmd != nil {
			m.conversation.spinFrame++
		}
		if m.conversation.shouldRefreshQuotaWhileStreaming(time.Now()) {
			if refreshCmd := m.refreshQuotaWhileStreamingCmd(); refreshCmd != nil {
				m.conversation.quotaRefreshInFlight = true
				m.conversation.lastQuotaRefreshAt = time.Now()
				return m, tea.Batch(cmd, refreshCmd)
			}
		}
		return m, cmd
	}
	return m, nil
}

func (m *RootModel) updateUsageQuotaTickMsg() (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind != panelregistry.Usage {
		return m, nil
	}
	m.display.quotaFrame++
	if m.overlays.panel.quota.state == quotaLoading && m.display.quotaFrame >= usageQuotaLoadingTicks {
		quotaStubRefills(m)
	}
	return m, usagepanel.QuotaTick()
}

func (m *RootModel) updateLogsTickMsg() (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind != panelregistry.Logs {
		return m, nil
	}
	m.invalidateViewCache()
	return m, logspanel.Tick()
}

func (m *RootModel) updateCompactStartMsg(msg compactStartMsg) (tea.Model, tea.Cmd) {
	m.session.store.CompactStart = msg.StartedAt
	m.session.store.CompactEnd = time.Time{}
	m.session.store.CompactSpinnerFrame = 0
	m.session.store.CompactReason = msg.Reason
	m.session.store.CompactTokensBefore = 0
	m.session.store.CompactTokensAfter = 0
	return m, compactSpinnerTick()
}

func (m *RootModel) updateCompactEndMsg(msg compactEndMsg) (tea.Model, tea.Cmd) {
	m.session.store.CompactEnd = msg.EndedAt
	m.session.store.CompactReason = msg.Reason
	m.session.store.CompactTokensBefore = msg.TokensBefore
	m.session.store.CompactTokensAfter = msg.EstimatedTokensAfter
	m.session.store.CompactStepsSummarized = msg.Steps
	m.session.store.CompactJustHappened = true
	m.conversation.compactHappenedThisTurn = true
	m.conversation.compactBlockIdx = len(m.conversation.blocks)
	m.conversation.compactStepIdx = len(m.conversation.liveSteps)

	if msg.Summary != "" || msg.Resume != "" {
		requests := compaction.UserRequests(m.SessionMessages(), compaction.MaxUserRequests)
		currentSteps := convstore.BuildSteps(m.trajectoryFacts())
		includedEnd := msg.Steps
		if includedEnd <= 0 {
			includedEnd = len(currentSteps)
		}
		if includedEnd <= 0 {
			includedEnd = 1
		}
		m.session.queueCheckpoint(convstore.Step{
			Type:            convstore.StepCheckpoint,
			Status:          3,
			Summary:         msg.Summary,
			UserRequests:    requests,
			Strategy:        msg.Strategy,
			CheckpointModel: msg.SummaryModel,
			TokensBefore:    msg.TokensBefore,
			TokensAfter:     msg.EstimatedTokensAfter,
			IncludedStart:   1,
			IncludedEnd:     includedEnd,
		}, m.checkpointInsertionPoint(msg.KeepRecent))
		resume := msg.Resume
		if resume == "" {
			resume = compaction.ResumingBlock(requests, msg.Summary)
		}
		carriedTokens := compaction.EstimateTokens(compaction.CarriedBlock(requests))
		m.setSessionMessages([]backend.Message{{
			Role:    "user",
			Content: resume,
		}})
		if sup := m.session.sup(); sup != nil {
			sup.SetSeededConversation(currentSteps, m.session.messageCount())
		}
		m.conversation.liveReplyIdx = -1

		overhead := compaction.ContextOverheadTokens(m.systemPrompt(), modelDeclarationsJSON())
		beforeTokens, afterTokens, summaryTokens := compaction.ComputeCompactTokens(
			msg.TokensBefore,
			0,
			msg.EstimatedTokensAfter,
			0,
			overhead,
			msg.Summary,
			carriedTokens,
		)

		took := msg.EndedAt.Sub(m.session.store.CompactStart)
		if took < 0 {
			took = 0
		}

		m.conversation.response = append(m.conversation.response,
			compaction.DividerRow(m.viewport.width),
			compaction.StatsRow(
				m.viewport.width,
				msg.Steps,
				beforeTokens,
				afterTokens,
				summaryTokens,
				took,
			),
			"",
		)
		m.viewport.rowsCacheDirty = true
	}

	return m, compactExpireCmd()
}

func (m *RootModel) updateCompactSpinnerTickMsg() (tea.Model, tea.Cmd) {
	if m.session.store.CompactStart.IsZero() || !m.session.store.CompactEnd.IsZero() {
		return m, nil
	}
	m.session.store.CompactSpinnerFrame++
	if m.session.store.CompactSpinnerFrame >= 8 {
		m.session.store.CompactSpinnerFrame = 0
	}
	return m, compactSpinnerTick()
}

func (m *RootModel) updatePluginBlinkMsg() (tea.Model, tea.Cmd) {
	// Cycle 381: the blink phase is the panel's own state — the panel
	// lives in internal/ui/views/pluginsui — so the host only flips it
	// and re-arms the tick while a blinking cursor is on screen.
	p := m.overlays.panel.plg
	if p == nil {
		return m, nil
	}
	if m.overlays.panel.kind == panelregistry.Plugin && p.Blinking() {
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
func (m *RootModel) updateCSTickMsg() (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind != panelregistry.CodeSearch {
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

func (m *RootModel) updateCompactEvents(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
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
	case compactExpiredMsg:
		m.session.store.ClearCompactStatusNotice()
		m.viewport.rowsCacheDirty = true
		return m, nil, true
	}
	return m, nil, false
}

func (m *RootModel) updateTicksAndCompactMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if mod, cmd, handled := m.updateBattleMsg(msg); handled {
		return mod, cmd, true
	}
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
func (m *RootModel) updateTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case planTierMsg:
		// The tier belongs to the account it was read for. A session
		// that follows a switch or a sign-in gets the new account's
		// tier; until that answer lands the session shows the email
		// alone, never the previous account's tier.
		if msg.email != "" {
			m.session.tierAccount = msg.email
			m.session.tierFetched = msg.tier != ""
			if msg.tier != "" {
				m.session.tier = msg.tier
				// The tier was cached in the registry, and the /accounts
				// panel shows the cached tier, so its rows have to be
				// re-read.
				if m.overlays.panel.kind == panelregistry.Accounts {
					m.accountsPanel().Reload()
				}
			} else {
				// The server named no tier for this account (a free
				// or an ineligible one): the session shows the email
				// alone, not the previous account's tier and not a
				// placeholder.
				m.session.tier = ""
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
	case tasksChangedMsg:
		// Clear before consuming dirty: a concurrent change can enqueue the
		// next wakeup, rather than being lost behind this one.
		atomic.StoreInt32(&m.tasks.wakePending, 0)
		if m.consumeTasksDirty() {
			m.invalidateFrameLayout()
		}
		if m.shouldTickTasks() {
			return m, m.ensureTasksTick(), true
		}
		return m, nil, true
	case tasksTickMsg:
		if !m.tasks.tickArmed || msg.generation != m.tasks.tickGeneration {
			return m, nil, true
		}
		m.tasks.tickArmed = false
		dirty := m.consumeTasksDirty()
		running := m.shouldTickTasks()
		if dirty || running {
			// A task's duration/output may change without a registry event.
			m.invalidateFrameLayout()
		}
		if running {
			return m, m.ensureTasksTick(), true
		}
		return m, nil, true
	case credentialReadMsg:
		// A re-read of the shared credential, performed off this
		// goroutine because on macOS it is a keychain syscall.
		return m, m.applyCredentialRead(msg), true
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
func (m *RootModel) updatePanelTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case quotaSummaryMsg:
		m.conversation.quotaRefreshInFlight = false
		if msg.summary != nil {
			// Always cache the refreshed quota summary so /usage, /quota, and
			// custom statusLine commands stay current even when the Usage panel
			// is not open (reference behaviour since 1.5.16, unchanged in 1.5.17).
			m.cacheQuota(m.session.email, msg.summary)
		}
		if m.overlays.panel.kind == panelregistry.Usage {
			if msg.summary != nil {
				m.overlays.panel.quota.summary = msg.summary
				m.overlays.panel.quota.loadedAt = time.Now()
				m.overlays.panel.quota.state = quotaLoaded
			} else if msg.err != nil {
				m.overlays.panel.quota.fetchErr = msg.err.Error()
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
		if m.overlays.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyVerifyMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthResultMsg:
		if m.overlays.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyOAuthResultMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthTickMsg:
		if m.overlays.panel.kind == panelregistry.Provider && m.overlays.panel.prov != nil {
			return m, m.overlays.panel.prov.ApplyOAuthTick(), true
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
	case logspanel.TickMsg:
		mod, cmd := m.updateLogsTickMsg()
		return mod, cmd, true
	}
	return m, nil, false
}

// ---- tui_update_login.go ----

func (m *RootModel) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.auth.login == nil {
		return m, nil, false
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.viewport.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if !isPlausibleWindowSize(msg) {
			return m, nil, true
		}
		m.viewport.width, m.viewport.height = msg.Width, msg.Height
		m.auth.login.Width, m.auth.login.Height = msg.Width, msg.Height
		return m, nil, true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.auth.login = nil
			return m, tea.Quit, true
		}
		if msg.String() == "esc" && m.auth.login.State == loginui.StateSelectMethod {
			m.auth.login = nil
			m.slashRespond("/login", "Sign in cancelled.")
			return m, nil, true
		}
	case *auth.AntigravityToken:
		return m, m.finishLogin(msg), true
	}
	newLM, cmd := m.auth.login.Update(msg)
	if lm, ok := newLM.(*loginui.Model); ok {
		m.auth.login = lm
		if lm.Authenticated {
			// Cycle multi-account-switch: the OAuth sign-in becomes a
			// known account (the *auth.AntigravityToken case above).
			// The ADC path carries no token, so there is nothing to
			// register here.
			return m, m.finishLogin(nil), true
		}
	}
	return m, cmd, true
}

// finishLogin closes the login panel and applies the signed-in identity.
//
// The token is the OAuth path's credential; it is nil on the ADC path,
// which carries no token to register.
//
// It returns the command that reads the new account's tier: the tier
// belongs to an account, so the session that just signed in as another
// account has to ask about *that* account — otherwise the previous
// account's tier would be shown against the new one.
func (m *RootModel) finishLogin(token *auth.AntigravityToken) tea.Cmd {
	m.auth.login = nil
	// The cached credential read predates the sign-in, so it is the
	// credential the user just replaced.
	m.forgetCredential()
	m.session.email = auth.EffectiveUserEmail()
	if m.turn.backendFor != nil {
		m.turn.backend = m.turn.backendFor(m.session.model)
	}
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	// Cycle multi-account-switch: the sign-in becomes a known
	// account, so it can be switched back to later.
	if token != nil {
		acc, _, err := accounts.Register(token)
		if err == nil && acc.Email != "" {
			m.session.email = acc.Email
		}
	}
	// The sign-in completed, so the shared credential changed: the
	// panel's rows have to be refreshed even when no panel is open.
	m.accountsPanel().Reload()
	m.slashRespond("/login", "Authentication successful!")
	return m.fetchPlanTierCmd()
}

// ---- tui_alt_esc.go ----

// tui_alt_esc.go — Option/Alt key translation and Esc chord release.

// altKeyFire records one synthesised alt+letter dispatch.
type altKeyFire struct {
	Letter rune   // lowercase letter that was synthesised
	Action string // action name from keybindings.json
}

// altDispatchAction is the production action table.
func (m *RootModel) altDispatchAction(letter rune, action string) {
	m.prompt.altKeyFires = append(m.prompt.altKeyFires, altKeyFire{Letter: letter, Action: action})
	m.conversation.response = append(m.conversation.response,
		uirows.NoticeDim("alt+"+string(letter)+" → "+action))
}

// releaseBufferedEsc replays a buffered bare Esc that was not the first
// half of an alt+letter sequence.
//
// In vim mode the editor owns Esc — a bare Esc leaves Insert mode and
// returns to Normal — so the release goes to the vim engine. Everywhere
// else it is the host's Esc.
func (m *RootModel) releaseBufferedEsc() {
	if m.vimEnabled() {
		if _, handled := m.handleVimKey(tea.Key{Code: tea.KeyEsc}); handled {
			return
		}
	}
	_, _ = m.handleEscKey()
}

// handleEscKey runs the bare-Esc branch: delegates to handleEscKeyMsg
// to ensure unified priority across direct Esc and EscReleaseWindow tick.
func (m *RootModel) handleEscKey() (tea.Cmd, bool) {
	_, cmd := m.handleEscKeyMsg()
	return cmd, true
}

// armEscReleaseTick returns a tea.Cmd that fires after keybind.EscReleaseWindow.
func (m *RootModel) armEscReleaseTick() tea.Cmd {
	return tea.Tick(keybind.EscReleaseWindow, func(time.Time) tea.Msg {
		return keybind.EscTimeoutMsg{}
	})
}

func (m *RootModel) canConsumeAltEsc() bool {
	if m.overlays.panel.kind != panelregistry.None || m.help.Panel.Active() || (m.swarm.Board != nil && m.swarm.Board.Active()) ||
		(m.agents.battle.Panel != nil && m.agents.battle.Panel.Active()) ||
		m.prompt.picker.Visible() || m.prompt.atFilePicker.Visible() ||
		m.overlays.resumePicker.Active() || (m.audio.Surface != nil && m.audio.Surface.Active() && m.audio.Surface.Phase() != voice.PhaseLive) ||
		m.prompt.mediaExpanded || m.isBtwCardVisible() ||
		m.conversation.streaming || m.session.store.BashRunning() ||
		// Bash mode's Esc leaves the mode: holding it for an alt pair
		// would make the key look dead in a composer whose own editor
		// owns Esc.
		m.session.store.BashMode ||
		m.prompt.HasInputSelection() {
		return false
	}
	return true
}

// consumeAltEsc is the alt-keytranslation entry point.
func (m *RootModel) consumeAltEsc(msg tea.KeyMsg) (newMsg tea.KeyMsg, consumed bool, cmd tea.Cmd) {
	key := msg.Key()
	if !keybind.TermNeedsOptionMeta() {
		return msg, false, nil
	}

	if m.prompt.keybindings == nil || len(m.prompt.keybindings.RawBindings()) == 0 {
		return msg, false, nil
	}

	if !m.canConsumeAltEsc() {
		return msg, false, nil
	}

	if m.prompt.altEscBuf.Pending {
		consumedKey := m.prompt.altEscBuf.Feed(
			key,
			func(r rune) bool { return m.prompt.keybindings != nil && m.prompt.keybindings.Has(r) },
			func(r rune) {
				key = tea.Key{Code: r, Mod: tea.ModAlt, Text: string(r)}
			},
			m.releaseBufferedEsc,
			time.Now(),
		)
		if consumedKey {
			return tea.KeyPressMsg(key), false, nil
		}
		return msg, false, nil
	}

	if key.Code == tea.KeyEsc && key.Mod == 0 {
		if m.prompt.altEscBuf.Start(time.Now()) {
			if m.vimEnabled() {
				// Vim owns the bare Esc: deliver it now so Normal mode
				// starts on the key press itself, and keep the buffer
				// only so a following bound letter still folds into one
				// alt+<key> event (Terminal.app sends Esc then the
				// letter). Releasing the buffer must not dispatch a
				// second Esc — Delivered says so.
				m.prompt.altEscBuf.Delivered = true
				return msg, false, m.armEscReleaseTick()
			}
			return msg, true, m.armEscReleaseTick()
		}
	}

	return msg, false, nil
}
