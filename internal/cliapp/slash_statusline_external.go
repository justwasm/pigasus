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
	return statusline.LoadConfig()
}

func persistStatusLineVisibility(visible bool) {
	statusline.PersistVisibility(visible)
}

type statusLinePayload = statusline.Payload

func (m *runMode) buildStatusLinePayload() statusLinePayload {
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	prompt, completion := m.sessionTokenTotals()
	cost := pricingForModel(model, prompt, completion)
	return statusLinePayload{
		Model:             model,
		Cost:              cost,
		ConversationTitle: m.conversationTitle(),
		CurrentDir:        m.cwd,
	}
}

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

func (m *runMode) runExternalStatusLineTick() {
	if m == nil {
		return
	}
	command, ok := m.drainExternalStatusLine()
	if !ok {
		return
	}
	m.runExternalStatusLineRender(command)
}

func (m *runMode) runExternalStatusLineRender(command string) {
	if m == nil {
		return
	}
	state := m.ensureExternalState()
	payload, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		state.SetRendered("", fmt.Errorf("status-line payload marshal: %w", err))
		return
	}
	res := runStatusLineScript(command, payload)
	if res.Err != nil {
		state.SetRendered("", res.Err)
		return
	}
	state.SetRendered(res.Output, nil)
}

type statusLineExternalTickMsg struct{}

func armExternalStatusLineTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return statusLineExternalTickMsg{} })
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
	cmd, ok := loadStatusLineConfig()
	if !ok {
		return "", false
	}
	return cmd, true
}

func (m *runMode) externalRenderOK() bool {
	if m == nil {
		return false
	}
	if !m.statusLineExternal || m.statusLine == "off" {
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
