// slash_statusline_external.go — external status-line script path, token tracking,
// and tick lifecycle management. Delegates core script execution and pricing to
// internal/statusline.
package cliapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/statusline"
)

type statusLineExternalConfig = statusline.Config

func loadStatusLineConfig() (command string, ok bool) {
	cfg, found := statusline.LoadConfig()
	if !found || !cfg.Active() {
		return "", false
	}
	return cfg.Command, true
}

type statusLinePayload = statusline.Payload

type pricingRow = statusline.PricingRow

var pricingTable = statusline.PricingTable

func pricingForModel(model string, prompt, completion int) float64 {
	return statusline.PricingForModel(model, prompt, completion)
}

func (m *runMode) sessionTokenTotals() (prompt, completion int) {
	if m == nil {
		return 0, 0
	}
	return int(atomic.LoadInt64(&m.sessionPromptTokens)),
		int(atomic.LoadInt64(&m.sessionCompletionTokens))
}

func (m *runMode) recordTurnUsage(meta *backend.ChunkMeta) {
	if m == nil || meta == nil {
		return
	}
	atomic.AddInt64(&m.sessionPromptTokens, int64(meta.PromptTokens))
	atomic.AddInt64(&m.sessionCompletionTokens, int64(meta.CompletionTokens))
	atomic.StoreInt64(&m.lastTurnPromptTokens, int64(meta.PromptTokens))
	atomic.StoreInt64(&m.lastTurnCompletionTokens, int64(meta.CompletionTokens))
}

func splitStatusLineCommand(command string) (string, []string) {
	return statusline.SplitCommand(command)
}

type runStatusLineScriptResult = statusline.Result

func runStatusLineScript(command string, payload []byte) runStatusLineScriptResult {
	return statusline.RunScript(command, payload)
}

type statusLineExternalState = statusline.State

func (m *runMode) externalStateFor() *statusLineExternalState {
	if m == nil {
		return nil
	}
	return m.statusLineExternalState
}

func (m *runMode) ensureExternalState() *statusLineExternalState {
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
	statusCfg := m.statusLineConfig()
	titleCfg := m.titleConfig()
	if !statusCfg.Active() && !titleCfg.Active() {
		return
	}
	payload, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		if statusCfg.Active() {
			state := m.ensureExternalState()
			state.SetRendered("", fmt.Errorf("status-line payload marshal: %w", err))
			m.recordStatusLineFailure()
		}
		return
	}
	go func() {
		m.runExternalStatusLineRender(statusCfg, payload)
		m.runExternalTitleRender(titleCfg, payload)
	}()
}

// runExternalStatusLineRender runs the status-line script once. It reports
// whether a script was configured and enabled.
func (m *runMode) runExternalStatusLineRender(cfg statusline.Config, payload []byte) bool {
	if m == nil || !cfg.Active() {
		return false
	}
	state := m.ensureExternalState()
	if state.Disabled() {
		return true
	}
	res := runStatusLineScript(cfg.Command, payload)
	if res.Err != nil {
		state.SetRendered("", res.Err)
		m.recordStatusLineFailure()
		return true
	}
	state.SetRendered(res.Output, nil)
	state.RecordSuccess()
	return true
}

// recordStatusLineFailure counts one failed script run. The reference
// switches the status line off after 30 failures in a row.
func (m *runMode) recordStatusLineFailure() {
	state := m.ensureExternalState()
	state.RecordFailure()
}

// runExternalTitleRender runs the window-title script once and stores the
// sanitized title.
func (m *runMode) runExternalTitleRender(cfg statusline.Config, payload []byte) {
	if m == nil || !cfg.Active() {
		return
	}
	res := runStatusLineScript(cfg.Command, payload)
	if res.Err != nil {
		return
	}
	m.setWindowTitle(statusline.SanitizeTitle(res.Output))
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
	if m == nil || m.statusLineTickArmed {
		return nil
	}
	if !m.statusLineScriptActive() && !m.titleScriptActive() {
		return nil
	}
	m.statusLineTickArmed = true
	return armExternalStatusLineTick()
}

func (m *runMode) drainExternalStatusLine() (command string, ok bool) {
	if m == nil {
		return "", false
	}
	state := m.ensureExternalState()
	if state.SnapshotTick() == 0 {
		return "", false
	}
	state.ResetTick()
	return loadStatusLineConfig()
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
