// payload_build_test.go — pins the bytes of the document the package
// renders from a state.
//
// The document is the reference's `types.StatusLineData`: the member names,
// their order and their omit rules are a wire contract, and the fixture in
// payload_test.go is the reference's own capture. The golden below is the
// clone's document for a fixture state — the same shape, filled with the
// clone's values.
//
// The version member is interpolated: the reference's version moves with
// every release, the document's shape does not.
//
// Evidence:
//   - 【一手】 the payloads agy 1.4.7 produced (payload_test.go).
//   - 【反编译】 the JSON tags were read out of the binary's string table
//     (`types.StatusLineData` and its member structs).
package statusline_test

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/core/version"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// goldenState is the fixture state the golden document is rendered from.
//
// The values mirror the clone's own capture: a session with a prompt
// token count, a quota bucket, an agent and a running cost.
func goldenState() statusline.PayloadState {
	return statusline.PayloadState{
		Cwd:               "/agyprobe/repo",
		ConversationID:    "eab2aff5-dfbc-436e-b19d-8c113441f508",
		ConversationTitle: "Probe",
		Model:             "gpt-oss-120b (High)",
		Effort:            "high",
		AgentName:         "probe",
		CycleMode:         "auto",
		PlanTier:          "Google AI Pro",
		Email:             "user4@example.com",
		TerminalWidth:     120,
		SandboxEnabled:    false,
		Quota: map[string]statusline.QuotaBucket{
			"3p-5h": {RemainingFraction: 1},
		},
		SessionPromptTokens:      19136,
		SessionCompletionTokens:  117,
		LastTurnPromptTokens:     13327,
		LastTurnCompletionTokens: 117,
		ContextWindowSize:        131072,
		PricingModel:             "gpt-oss-120b",
	}
}

// decodePayload decodes a rendered document into a fresh map.
//
// The map is fresh every time: unmarshalling into the same map twice
// keeps the keys the second document does not carry.
func decodePayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// goldenPayload is the clone's document for goldenState.
var goldenPayload = `{"cwd":"/agyprobe/repo","session_id":"eab2aff5-dfbc-436e-b19d-8c113441f508",` +
	`"conversation_id":"eab2aff5-dfbc-436e-b19d-8c113441f508","conversation_title":"Probe",` +
	`"transcript_path":"/agyprobe/home/brain/eab2aff5-dfbc-436e-b19d-8c113441f508/.system_generated/logs/transcript.jsonl",` +
	`"model":{"id":"gpt-oss-120b (High)","display_name":"gpt-oss-120b (High)","effort":"high"},` +
	`"workspace":{"current_dir":"/agyprobe/repo","project_dir":"/agyprobe/repo"},` +
	`"version":"` + version.Reference + `",` +
	`"context_window":{"total_input_tokens":19136,"total_output_tokens":117,"context_window_size":131072,` +
	`"used_percentage":14.599609375,"remaining_percentage":85.400390625,` +
	`"current_usage":{"input_tokens":13327,"output_tokens":117,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},` +
	`"exceeds_200k_tokens":false,"agent":{"name":"probe"},"product":"antigravity",` +
	`"quota":{"3p-5h":{"remaining_fraction":1}},"agent_state":"idle","cycle_mode":"auto",` +
	`"sandbox":{"enabled":false},"plan_tier":"Google AI Pro","email":"user4@example.com",` +
	`"terminal_width":120,"cost":{"total_usd":0.0097435,"subagent_usd":0,"estimated":true}}`

// TestPayloadFrom_GoldenBytes pins the document's bytes.
func TestPayloadFrom_GoldenBytes(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	raw, err := json.Marshal(statusline.PayloadFrom(goldenState()))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != goldenPayload {
		t.Errorf("payload bytes changed:\n got %s\nwant %s", raw, goldenPayload)
	}
}

// TestPayloadFrom_WorkingState pins the `agent_state` member: the
// reference reports "working" while the agent runs and "idle" between
// turns.
func TestPayloadFrom_WorkingState(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	state := goldenState()
	state.Streaming = true
	raw, err := json.Marshal(statusline.PayloadFrom(state))
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

// TestPayloadFrom_EmptyState pins the omit rules of an empty session.
//
// The members the reference omits when they are empty are omitted here
// too; `model`, `current_usage` and `exceeds_200k_tokens` are written as
// JSON null, because the reference writes them that way.
func TestPayloadFrom_EmptyState(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	raw, err := json.Marshal(statusline.PayloadFrom(statusline.PayloadState{}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"cwd":"","session_id":"","conversation_id":"",` +
		`"transcript_path":"/agyprobe/home/brain/.system_generated/logs/transcript.jsonl",` +
		`"model":null,"workspace":{"current_dir":"","project_dir":""},"version":"` + version.Reference + `",` +
		`"context_window":{"total_input_tokens":0,"total_output_tokens":0,"context_window_size":0,` +
		`"used_percentage":0,"remaining_percentage":0,"current_usage":null},` +
		`"exceeds_200k_tokens":null,"product":"antigravity","agent_state":"idle","sandbox":{"enabled":false}}`
	if string(raw) != want {
		t.Errorf("empty payload changed:\n got %s\nwant %s", raw, want)
	}
}

// TestPayloadFrom_NoUsageReportsNull pins that the reference's
// `current_usage` and `exceeds_200k_tokens` are null before the first
// model response.
func TestPayloadFrom_NoUsageReportsNull(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	state := goldenState()
	state.LastTurnPromptTokens = 0
	state.LastTurnCompletionTokens = 0
	raw, err := json.Marshal(statusline.PayloadFrom(state))
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

// TestPayloadFrom_Exceeds200k pins the threshold: the reference reports
// `exceeds_200k_tokens` once the session's prompt tokens cross 200,000.
func TestPayloadFrom_Exceeds200k(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	state := goldenState()
	state.SessionPromptTokens = 200001
	raw, err := json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["exceeds_200k_tokens"] != true {
		t.Errorf("exceeds_200k_tokens = %v, want true", decoded["exceeds_200k_tokens"])
	}
}

// TestAgentState pins the `agent_state` vocabulary.
//
// The reference names seven states (official-docs/cli/statusline.md:67);
// the clone can tell four of them apart. The precedence is the
// reference's: the tool call inside a running turn is the more precise
// state, and a failed turn is the state the session rests in.
func TestAgentState(t *testing.T) {
	cases := []struct {
		name         string
		streaming    bool
		toolInFlight bool
		turnFailed   bool
		want         string
	}{
		{"idle", false, false, false, statusline.StateIdle},
		{"working", true, false, false, statusline.StateWorking},
		{"tool_use", true, true, false, statusline.StateToolUse},
		{"error", false, false, true, statusline.StateError},
		{"a running turn beats a failed turn", true, false, true, statusline.StateWorking},
		{"a tool call beats a failed turn", true, true, true, statusline.StateToolUse},
	}
	for _, c := range cases {
		got := statusline.AgentState(c.streaming, c.toolInFlight, c.turnFailed)
		if got != c.want {
			t.Errorf("%s: agent_state = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestPayloadFrom_VimMember pins the `vim` member.
//
// The member is present only when vim editor mode is enabled
// (official-docs/cli/statusline.md:79), so an empty mode omits it.
func TestPayloadFrom_VimMember(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	state := goldenState()
	state.VimMode = "INSERT"
	raw, err := json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodePayload(t, raw)
	vim, ok := decoded["vim"].(map[string]any)
	if !ok {
		t.Fatalf("vim is not an object: %s", raw)
	}
	if vim["mode"] != "INSERT" {
		t.Errorf("vim.mode = %v, want INSERT", vim["mode"])
	}

	// Vim editor mode off: the member is gone.
	state.VimMode = ""
	raw, err = json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	decoded = decodePayload(t, raw)
	if _, ok := decoded["vim"]; ok {
		t.Errorf("vim must be omitted when vim editor mode is off: %s", raw)
	}
}

// TestPayloadFrom_PendingInputCount pins the `pending_input_count`
// member: the number of queued user messages, omitted when there are none.
func TestPayloadFrom_PendingInputCount(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	state := goldenState()
	state.PendingInputCount = 2
	raw, err := json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodePayload(t, raw)
	if decoded["pending_input_count"] != float64(2) {
		t.Errorf("pending_input_count = %v, want 2", decoded["pending_input_count"])
	}

	state.PendingInputCount = 0
	raw, err = json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	decoded = decodePayload(t, raw)
	if _, ok := decoded["pending_input_count"]; ok {
		t.Errorf("pending_input_count must be omitted when it is 0: %s", raw)
	}
}

// TestPayloadFrom_Subagents pins the `subagents` member: the array of
// the conversation's subagents, omitted when there are none.
func TestPayloadFrom_Subagents(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/agyprobe/home")
	state := goldenState()
	state.Subagents = []statusline.SubagentInfo{
		{Name: "research", Role: "research", Status: "running"},
	}
	raw, err := json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodePayload(t, raw)
	list, ok := decoded["subagents"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("subagents is not a one-element array: %s", raw)
	}
	entry, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("subagents[0] is not an object: %s", raw)
	}
	if entry["name"] != "research" || entry["role"] != "research" || entry["status"] != "running" {
		t.Errorf("subagents[0] = %v, want name/role research and status running", entry)
	}

	state.Subagents = nil
	raw, err = json.Marshal(statusline.PayloadFrom(state))
	if err != nil {
		t.Fatal(err)
	}
	decoded = decodePayload(t, raw)
	if _, ok := decoded["subagents"]; ok {
		t.Errorf("subagents must be omitted when there are none: %s", raw)
	}
}
