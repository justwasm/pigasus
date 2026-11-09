// statusline_payload_test.go — pins the bytes of the document the host's
// facts produce.
//
// The document itself belongs to internal/ui/views/statusline (its golden
// lives in that package's payload_build_test.go). This test pins the seam:
// the facts the host collects — the workspace, the conversation, the model,
// the account, the token counters, the quota — and the document they make.
//
// The fixture is the clone's own: a session with a prompt token count, a
// quota bucket, an agent and a running cost.
//
// Evidence 【一手】: the payloads agy 1.4.7 produced — see
// internal/ui/views/statusline/payload_test.go for the reference's own
// capture.
package tui

import (
	"encoding/json"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	"github.com/justwasm/pigasus/internal/core/version"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// payloadGoldenModel is the fixture's model label.
//
// The label carries its effort tier, so the payload's `model.effort` member
// is the tier the reference reports.
const (
	payloadGoldenModel   = "gpt-oss-120b (High)"
	payloadGoldenStatus  = "gpt-oss-120b"
	payloadGoldenConvID  = "eab2aff5-dfbc-436e-b19d-8c113441f508"
	payloadGoldenCwd     = "/agyprobe/repo"
	payloadGoldenHome    = "/agyprobe/home"
	payloadGoldenTitle   = "Probe"
	payloadGoldenAccount = "user4@example.com"
	payloadGoldenTier    = "Google AI Pro"
)

// payloadGolden is the document the fixture's facts produce.
//
// The version member is interpolated: the reference's version moves with
// every release, the document's shape does not.
var payloadGolden = `{"cwd":"` + payloadGoldenCwd + `",` +
	`"session_id":"` + payloadGoldenConvID + `","conversation_id":"` + payloadGoldenConvID + `",` +
	`"conversation_title":"` + payloadGoldenTitle + `",` +
	`"transcript_path":"` + payloadGoldenHome + `/brain/` + payloadGoldenConvID + `/.system_generated/logs/transcript.jsonl",` +
	`"model":{"id":"` + payloadGoldenModel + `","display_name":"` + payloadGoldenModel + `","effort":"high"},` +
	`"workspace":{"current_dir":"` + payloadGoldenCwd + `","project_dir":"` + payloadGoldenCwd + `"},` +
	`"version":"` + version.Reference + `",` +
	`"context_window":{"total_input_tokens":19136,"total_output_tokens":117,"context_window_size":131072,` +
	`"used_percentage":14.599609375,"remaining_percentage":85.400390625,` +
	`"current_usage":{"input_tokens":13327,"output_tokens":117,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},` +
	`"exceeds_200k_tokens":false,"agent":{"name":"probe"},"product":"antigravity",` +
	`"quota":{"3p-5h":{"remaining_fraction":1}},"agent_state":"idle","cycle_mode":"auto",` +
	`"sandbox":{"enabled":false},"plan_tier":"` + payloadGoldenTier + `","email":"` + payloadGoldenAccount + `",` +
	`"terminal_width":120,"cost":{"total_usd":0.0097435,"subagent_usd":0,"estimated":true}}`

// payloadGoldenSession builds the fixture session the golden is rendered
// from.
//
// The account is registered in an isolated registry, because the quota
// snapshot the payload carries is the registry's: the session's own
// cache is the registry, so the fixture writes the snapshot there.
func payloadGoldenSession(t *testing.T) *RootModel {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", payloadGoldenHome)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(t.TempDir(), "accounts.json"))
	if _, _, err := accounts.Register(accountsFakeToken(t, payloadGoldenAccount, "at-probe")); err != nil {
		t.Fatalf("register the account: %v", err)
	}
	m := &RootModel{
		session: sessionState{
			statusModel:      payloadGoldenStatus,
			model:            payloadGoldenModel,
			cwd:              payloadGoldenCwd,
			conversationID:   payloadGoldenConvID,
			conversationName: payloadGoldenTitle,
			email:            payloadGoldenAccount,
			tier:             payloadGoldenTier,
			mode:             "auto",
			agentName:        "probe",
			store: tuistore.Store{Tokens: tuistore.Tokens{
				SessionPromptTokens:      19136,
				SessionCompletionTokens:  117,
				LastTurnPromptTokens:     13327,
				LastTurnCompletionTokens: 117,
			}},
		},
		viewport: uiState{width: 120},
	}
	m.cacheQuota(payloadGoldenAccount, &auth.QuotaSummary{
		Groups: []auth.QuotaGroup{{
			Name: "gpt",
			Buckets: []auth.QuotaBucket{
				{ID: "3p-5h", RemainingFraction: 1},
			},
		}},
	})
	return m
}

// TestBuildStatusLinePayload_GoldenBytes pins the document the host's
// facts produce.
func TestBuildStatusLinePayload_GoldenBytes(t *testing.T) {
	m := payloadGoldenSession(t)
	raw, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != payloadGolden {
		t.Errorf("payload bytes changed:\n got %s\nwant %s", raw, payloadGolden)
	}
}

// TestBuildStatusLinePayload_WorkingState pins the `agent_state` member:
// the reference reports "working" while the agent runs.
func TestBuildStatusLinePayload_WorkingState(t *testing.T) {
	m := payloadGoldenSession(t)
	m.conversation.streaming = true
	raw, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["agent_state"] != "working" {
		t.Errorf("agent_state = %v, want working", decoded["agent_state"])
	}
}

// TestBuildStatusLinePayload_NoUsageReportsNull pins that the reference's
// `current_usage` and `exceeds_200k_tokens` are null before the first
// model response.
func TestBuildStatusLinePayload_NoUsageReportsNull(t *testing.T) {
	m := payloadGoldenSession(t)
	m.session.store.LastTurnPromptTokens = 0
	m.session.store.LastTurnCompletionTokens = 0
	raw, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["exceeds_200k_tokens"] != nil {
		t.Errorf("exceeds_200k_tokens = %v, want null before the first response",
			decoded["exceeds_200k_tokens"])
	}
	cw, ok := decoded["context_window"].(map[string]any)
	if !ok {
		t.Fatalf("context_window is not an object: %s", raw)
	}
	if cw["current_usage"] != nil {
		t.Errorf("current_usage = %v, want null before the first response",
			cw["current_usage"])
	}
}

// TestBuildStatusLinePayload_VimMember pins the `vim` member.
//
// The member is present only when vim editor mode is enabled, so the
// fixture's own session (no vim) carries none.
func TestBuildStatusLinePayload_VimMember(t *testing.T) {
	m := payloadGoldenSession(t)
	raw, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeHostPayload(t, raw)
	if _, ok := decoded["vim"]; ok {
		t.Errorf("vim must be absent while vim editor mode is off: %s", raw)
	}

	m.prompt.vimOn = true
	raw, err = json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	decoded = decodeHostPayload(t, raw)
	vim, ok := decoded["vim"].(map[string]any)
	if !ok {
		t.Fatalf("vim is not an object: %s", raw)
	}
	if vim["mode"] != "NORMAL" {
		t.Errorf("vim.mode = %v, want NORMAL", vim["mode"])
	}
}

// TestBuildStatusLinePayload_PendingInputCount pins the
// `pending_input_count` member: the queued user messages.
//
// The queue mode's own prompt counts as one — it is the next prompt the
// session runs — and the bash queue does not: those are shell commands.
func TestBuildStatusLinePayload_PendingInputCount(t *testing.T) {
	m := payloadGoldenSession(t)
	m.session.store.StreamQueue = []string{"first", "second"}
	m.session.store.QueuedPrompt = "third"
	raw, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeHostPayload(t, raw)
	if decoded["pending_input_count"] != float64(3) {
		t.Errorf("pending_input_count = %v, want 3", decoded["pending_input_count"])
	}
}

// TestBuildStatusLinePayload_Subagents pins the `subagents` member: the
// conversation's live subagents.
func TestBuildStatusLinePayload_Subagents(t *testing.T) {
	m := payloadGoldenSession(t)
	registry := subagenttools.NewRegistry(payloadGoldenConvID)
	child := subagenttools.NewChild("child-1", "research", "deep dive", "/tmp", nil)
	child.Model = "Gemini 3.8 Flash (High)"
	registry.Add(child)
	m.agents.registry = registry
	raw, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeHostPayload(t, raw)
	list, ok := decoded["subagents"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("subagents is not a one-element array: %s", raw)
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("subagents[0] is not an object: %s", raw)
	}
	if entry["name"] != "research" || entry["role"] != "deep dive" || entry["status"] != "running" {
		t.Errorf("subagents[0] = %v, want research/deep dive/running", entry)
	}
	if entry["model"] != "Gemini 3.8 Flash (High)" {
		t.Errorf("subagents[0].model = %v", entry["model"])
	}
}

// TestAgentState_HostFacts pins the host's side of the `agent_state`
// member: the four states the clone can tell apart.
//
// The idle state carries no cell in the status row — the row's default
// needs no label — and the payload still reports it.
func TestAgentState_HostFacts(t *testing.T) {
	m := payloadGoldenSession(t)
	if got := m.agentState(); got != "" {
		t.Errorf("agentState = %q, want no cell while the session is idle", got)
	}

	m.conversation.streaming = true
	if got := m.agentState(); got != "working" {
		t.Errorf("agentState = %q, want working", got)
	}

	m.conversation.activeTool = "⠿ read_file"
	if got := m.agentState(); got != "tool_use" {
		t.Errorf("agentState = %q, want tool_use", got)
	}

	m.conversation.streaming = false
	m.conversation.activeTool = ""
	m.display.lastTurnOutcome = tea.ProgramStateError
	if got := m.agentState(); got != "error" {
		t.Errorf("agentState = %q, want error", got)
	}
}

// decodeHostPayload decodes a rendered document into a fresh map.
//
// The map is fresh every time: unmarshalling into the same map twice
// keeps the keys the second document does not carry.
func decodeHostPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
