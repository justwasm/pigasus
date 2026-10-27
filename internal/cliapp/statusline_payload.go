// statusline_payload.go — the JSON document the CLI pipes to a custom
// status-line script's stdin (and to a custom window-title script's stdin:
// the reference sends the very same document to both).
//
// The shape is the reference's `types.StatusLineData`. Every member, its
// JSON name and its omit rule are part of the wire contract — the official
// example scripts published at
// github.com/google-antigravity/antigravity-cli/examples read the members
// by name with jq.
//
// Evidence:
//   - 【一手】 agy 1.4.7 was run with a capture script bound to
//     `statusLine.command`; the payloads it produced are the fixture in
//     internal/statusline/payload_test.go.
//   - 【反编译】 the JSON tags were read out of the binary's string table
//     (`types.StatusLineData` and its member structs).
package cliapp

import (
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/modelctx"
	"github.com/justwasm/pigasus/internal/statusline"
)

// statusLineProduct is the `product` member of the payload. The reference
// writes the application name.
const statusLineProduct = "antigravity"

// exceeds200kTokens is the context size above which the reference reports
// `exceeds_200k_tokens`.
const exceeds200kTokens = 200000

// buildStatusLinePayload renders the document the scripts read.
func (m *runMode) buildStatusLinePayload() statusLinePayload {
	payload := statusLinePayload{
		Cwd:            m.cwd,
		SessionID:      m.conversationID,
		ConversationID: m.conversationID,
		TranscriptPath: m.statusLineTranscriptPath(),
		Version:        cli.ReferenceVersion,
		Product:        statusLineProduct,
		Workspace: statusline.Workspace{
			CurrentDir: m.cwd,
			ProjectDir: m.cwd,
		},
		ContextWindow: m.statusLineContextWindow(),
		AgentState:    m.statusLineAgentState(),
		CycleMode:     m.mode,
		PlanTier:      m.tier,
		Email:         m.email,
		TerminalWidth: m.width,
	}
	if title := m.conversationTitle(); title != "" {
		payload.ConversationTitle = title
	}
	if m.model != "" {
		payload.Model = &statusline.ModelInfo{
			ID:          m.model,
			DisplayName: m.model,
			Effort:      m.currentEffortName(),
		}
	}
	if m.agentName != "" {
		payload.Agent = &statusline.AgentInfo{Name: m.agentName}
	}
	if vcs := statusline.DetectVCS(m.cwd); vcs != nil {
		payload.VCS = vcs
	}
	payload.Sandbox = &statusline.SandboxInfo{Enabled: m.toolSandbox.Enabled}
	if quota := m.statusLineQuota(); len(quota) > 0 {
		payload.Quota = quota
	}
	if n := m.statusLineTaskCount(); n > 0 {
		payload.TaskCount = n
	}
	if cost := m.statusLineCost(); cost > 0 {
		payload.Cost = &statusline.CostInfo{TotalUSD: cost, Estimated: true}
	}
	payload.Exceeds200kTokens = m.statusLineExceeds200k()
	return payload
}

// statusLineTranscriptPath is where the reference keeps the conversation's
// transcript. It is the brain directory's `.system_generated/logs`
// subdirectory.
//
// Evidence 【一手】: agy 1.4.7's payload carried
// `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`,
// and the id-less form `<appDataDir>/brain/.system_generated/logs/transcript.jsonl`
// before the conversation started.
func (m *runMode) statusLineTranscriptPath() string {
	return filepath.Join(auth.AntigravityConfigHome(), "brain", m.conversationID,
		".system_generated", "logs", "transcript.jsonl")
}

// statusLineContextWindow renders the token accounting.
//
// The reference's `used_percentage` is the share of the context window the
// *prompt* tokens take: 19136 prompt tokens in a 1048576-token window is
// 1.824951171875%, which is what the live payload carried.
func (m *runMode) statusLineContextWindow() statusline.ContextWindow {
	prompt, completion := m.sessionTokenTotals()
	size := m.statusLineContextSize()
	used := 0.0
	remaining := 0.0
	if size > 0 {
		used = float64(prompt) / float64(size) * 100
		remaining = 100 - used
	}
	cw := statusline.ContextWindow{
		TotalInputTokens:    prompt,
		TotalOutputTokens:   completion,
		ContextWindowSize:   size,
		UsedPercentage:      used,
		RemainingPercentage: remaining,
	}
	if last, ok := m.statusLineLastUsage(); ok {
		cw.CurrentUsage = &last
	}
	return cw
}

// statusLineContextSize is the model's context window.
func (m *runMode) statusLineContextSize() int {
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	if model == "" {
		return 0
	}
	return modelctx.ContextWindowFor(model)
}

// statusLineLastUsage returns the last turn's token usage. The reference
// reports `current_usage` as null until the first model response.
func (m *runMode) statusLineLastUsage() (statusline.CurrentUsage, bool) {
	prompt := int(atomic.LoadInt64(&m.lastTurnPromptTokens))
	completion := int(atomic.LoadInt64(&m.lastTurnCompletionTokens))
	if prompt == 0 && completion == 0 {
		return statusline.CurrentUsage{}, false
	}
	return statusline.CurrentUsage{
		InputTokens:  prompt,
		OutputTokens: completion,
		// The clone's transports do not surface the provider's cache
		// counters, so the two cache members stay zero.
	}, true
}

// statusLineExceeds200k reports whether the conversation crossed the 200k
// context threshold. The reference reports null until the first response.
func (m *runMode) statusLineExceeds200k() *bool {
	if m.lastTurnPromptTokens == 0 && m.lastTurnCompletionTokens == 0 {
		return nil
	}
	exceeds := m.sessionPromptTokens > exceeds200kTokens
	return &exceeds
}

// statusLineAgentState is the reference's `agent_state` member.
//
// The reference reports "initializing" while it starts up, "authenticating"
// while it signs in, "idle" between turns and "working" while the agent
// runs. The clone has no separate authentication phase, so it reports the
// two it can tell apart.
func (m *runMode) statusLineAgentState() string {
	if m.streaming {
		return "working"
	}
	return "idle"
}

// statusLineQuota renders the account's quota buckets.
//
// The reference keys the map by the bucket id ("gemini-5h", "3p-weekly",
// …) and reports the remaining fraction, the reset time and the seconds
// until the reset.
func (m *runMode) statusLineQuota() map[string]statusline.QuotaBucket {
	summary := m.cachedQuota(m.email)
	if summary == nil {
		return nil
	}
	out := map[string]statusline.QuotaBucket{}
	now := time.Now()
	for _, group := range summary.Groups {
		for _, bucket := range group.Buckets {
			entry := statusline.QuotaBucket{RemainingFraction: bucket.RemainingFraction}
			if !bucket.ResetTime.IsZero() {
				reset := bucket.ResetTime
				entry.ResetTime = &reset
				entry.ResetInSeconds = int(reset.Sub(now).Seconds())
			}
			out[bucket.ID] = entry
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// statusLineTaskCount is the number of running background tasks.
func (m *runMode) statusLineTaskCount() int {
	if m.taskReg == nil {
		return 0
	}
	return m.taskReg.CountRunning()
}

// statusLineCost is the session's estimated spend.
func (m *runMode) statusLineCost() float64 {
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	prompt, completion := m.sessionTokenTotals()
	return pricingForModel(model, prompt, completion)
}

// statusLineConfig returns the cached `statusLine` block.
//
// The TUI reads settings.json once at startup and refreshes the cache
// whenever /statusline changes it: the status area is re-rendered on every
// frame, so it must not hit the disk.
func (m *runMode) statusLineConfig() statusline.Config {
	return m.statusLineCfg
}

// titleConfig returns the cached `title` block.
func (m *runMode) titleConfig() statusline.Config {
	return m.titleCfg
}

// reloadStatusLineConfig re-reads the `statusLine` and `title` blocks from
// settings.json and refreshes the caches.
func (m *runMode) reloadStatusLineConfig() {
	if cfg, ok := statusline.LoadConfig(); ok {
		m.statusLineCfg = cfg
		m.statusLineCommand = cfg.Command
		m.statusLineExternal = cfg.Active()
	} else {
		m.statusLineCfg = statusline.Config{}
		m.statusLineCommand = ""
		m.statusLineExternal = false
	}
	if cfg, ok := statusline.LoadTitleConfig(); ok {
		m.titleCfg = cfg
		m.titleCommand = cfg.Command
		if cfg.Active() {
			m.title = "on"
		}
	} else {
		m.titleCfg = statusline.Config{}
		m.titleCommand = ""
	}
}

// statusLineScriptActive reports whether a custom status-line script is
// configured and enabled.
func (m *runMode) statusLineScriptActive() bool {
	if m.statusLineCfg.Active() {
		return true
	}
	return m.statusLineExternal
}

// titleScriptActive reports whether a custom window-title script is
// configured and enabled.
func (m *runMode) titleScriptActive() bool {
	return m.titleCfg.Active()
}
