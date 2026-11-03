// slash_statusline_external.go — the host's side of the external
// status-line script: the tick that runs it, the token accounting it
// reports and the cached output the status area draws.
//
// The script itself, the window-title script and the stores they write
// into are internal/ui/views/statusline's — that package owns the runner,
// the payload document and the title store. The host keeps the facts the
// runner cannot know: which blocks are configured and enabled, the
// session's token counters and the model that is running.
package cliapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)


func (m *runMode) sessionTokenTotals() (prompt, completion int) {
	if m == nil {
		return 0, 0
	}
	return int(atomic.LoadInt64(&m.store.SessionPromptTokens)),
		int(atomic.LoadInt64(&m.store.SessionCompletionTokens))
}

func (m *runMode) recordTurnUsage(meta *backend.ChunkMeta) {
	if m == nil || meta == nil {
		return
	}
	atomic.AddInt64(&m.store.SessionPromptTokens, int64(meta.PromptTokens))
	atomic.AddInt64(&m.store.SessionCompletionTokens, int64(meta.CompletionTokens))
	atomic.StoreInt64(&m.store.LastTurnPromptTokens, int64(meta.PromptTokens))
	atomic.StoreInt64(&m.store.LastTurnCompletionTokens, int64(meta.CompletionTokens))
}

// externalStateFor returns the cached rendered output, or nil when no
// script has run yet.
func (m *runMode) externalStateFor() *statusline.State {
	if m == nil {
		return nil
	}
	return m.statusLineExternalState
}

// ensureExternalState returns the cached rendered output, creating it when
// the model was built without one.
func (m *runMode) ensureExternalState() *statusline.State {
	if m == nil {
		return nil
	}
	if m.statusLineExternalState == nil {
		m.statusLineExternalState = &statusline.State{}
	}
	return m.statusLineExternalState
}

// runExternalStatusLineTick renders the configured status-line script and,
// when one is configured, the window-title script.
//
// It runs on the model's own goroutine: it reads the session's state (the
// quota cache, the task registry, the token counters) and builds the
// payload there, so no other goroutine ever touches that state. The
// scripts themselves run in a goroutine, because one of them can take up
// to five seconds.
//
// The reference runs the scripts at most once every 300 ms and gives up on
// a run that takes longer than five seconds. It also stops running a
// status-line script after 30 failed runs in a row.
func (m *runMode) runExternalStatusLineTick() {
	if m == nil {
		return
	}
	statusCfg := m.statusLineCfg
	titleCfg := m.titleCfg
	if !statusCfg.Active() && !titleCfg.Active() {
		return
	}
	payload, err := json.Marshal(m.buildStatusLinePayloadDoc())
	if err != nil {
		if statusCfg.Active() {
			state := m.ensureExternalState()
			state.SetRendered("", fmt.Errorf("status-line payload marshal: %w", err))
			state.RecordFailure()
		}
		return
	}
	go func() {
		statusline.RenderStatusLine(m.ensureExternalState(), statusCfg, payload)
		statusline.RenderWindowTitle(m.titleStoreFor(), titleCfg, payload)
	}()
}

type statusLineExternalTickMsg struct{}

// armExternalStatusLineTick re-arms the status-line tick.
//
// The reference runs the script at most once every 300 ms; the clone's
// tick fires at the same cadence.
func armExternalStatusLineTick() tea.Cmd {
	return tea.Tick(statusline.ScriptInterval, func(time.Time) tea.Msg { return statusLineExternalTickMsg{} })
}

// maybeArmStatusLineTick starts the status-line tick when a script is
// configured and the tick is not running yet.
//
// The tick re-arms itself, so it is started once: at startup when the
// settings already hold a script, and by /statusline or /title when the
// user points the CLI at one.
func (m *runMode) maybeArmStatusLineTick() tea.Cmd {
	if m == nil || m.store.StatusLineTickArmed {
		return nil
	}
	if !m.statusLineScriptActive() && !m.titleCfg.Active() {
		return nil
	}
	m.store.StatusLineTickArmed = true
	return armExternalStatusLineTick()
}

func (m *runMode) externalRenderOK() bool {
	if m == nil {
		return false
	}
	if m.statusLine == "off" {
		return false
	}
	if !m.statusLineScriptActive() {
		return false
	}
	state := m.externalStateFor()
	if state == nil {
		return false
	}
	out, _ := state.Current()
	return strings.TrimSpace(out) != ""
}

func (m *runMode) externalRenderedForRow() (string, error) {
	if !m.externalRenderOK() {
		return "", nil
	}
	state := m.externalStateFor()
	if state == nil {
		return "", nil
	}
	out, lastErr := state.Current()
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	return out, lastErr
}
