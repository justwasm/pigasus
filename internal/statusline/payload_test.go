// payload_test.go — pins the wire form of the document the CLI pipes to a
// custom status-line / window-title script.
//
// The fixture is a payload agy 1.4.7 actually produced: the capture script
// was bound to `statusLine.command` and every line it wrote is the
// reference's own JSON.
//
// Evidence 【一手】: agy 1.4.7, capture script bound to `statusLine.command`.
package statusline_test

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/statusline"
)

// referencePayload is one payload agy 1.4.7 produced, verbatim.
//
//	{"cwd":"/tmp/agyprobe/repo","session_id":"eab2aff5-dfbc-436e-b19d-8c113441f508",
//	 "conversation_id":"eab2aff5-dfbc-436e-b19d-8c113441f508",
//	 "transcript_path":"…/brain/eab2aff5-…/.system_generated/logs/transcript.jsonl",
//	 "model":{"id":"Gemini 3.8 Flash (High)","display_name":"Gemini 3.8 Flash (High)","effort":"high"},
//	 "workspace":{"current_dir":"/tmp/agyprobe/repo","project_dir":"/tmp/agyprobe/repo"},
//	 "version":"1.4.7","context_window":{…},"exceeds_200k_tokens":false,
//	 "product":"antigravity","quota":{…},"agent_state":"idle","vcs":{"type":"git"},
//	 "cycle_mode":"auto","sandbox":{"enabled":false},"plan_tier":"Google AI Pro",
//	 "email":"…","terminal_width":120}
const referencePayload = `{"cwd":"/tmp/agyprobe/repo","session_id":"eab2aff5-dfbc-436e-b19d-8c113441f508","conversation_id":"eab2aff5-dfbc-436e-b19d-8c113441f508","transcript_path":"/Users/gear/.gemini/antigravity/brain/eab2aff5-dfbc-436e-b19d-8c113441f508/.system_generated/logs/transcript.jsonl","model":{"id":"Gemini 3.8 Flash (High)","display_name":"Gemini 3.8 Flash (High)","effort":"high"},"workspace":{"current_dir":"/tmp/agyprobe/repo","project_dir":"/tmp/agyprobe/repo"},"version":"1.4.7","context_window":{"total_input_tokens":19136,"total_output_tokens":117,"context_window_size":1048576,"used_percentage":1.824951171875,"remaining_percentage":98.175048828125,"current_usage":{"input_tokens":13327,"output_tokens":117,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},"exceeds_200k_tokens":false,"product":"antigravity","quota":{"3p-5h":{"remaining_fraction":1,"reset_time":"2026-10-25T18:00:20Z","reset_in_seconds":17948}},"agent_state":"idle","vcs":{"type":"git"},"cycle_mode":"auto","sandbox":{"enabled":false},"plan_tier":"Google AI Pro","email":"stakingminer@gmail.com","terminal_width":120}`

// referenceKeyOrder is the member order of the reference's payload.
var referenceKeyOrder = []string{
	"cwd",
	"session_id",
	"conversation_id",
	"conversation_title",
	"transcript_path",
	"model",
	"workspace",
	"version",
	"context_window",
	"exceeds_200k_tokens",
	"agent",
	"product",
	"quota",
	"agent_state",
	"vcs",
	"cycle_mode",
	"sandbox",
	"subagents",
	"artifact_count",
	"plan_tier",
	"email",
	"pending_input_count",
	"tool_confirmation_pending",
	"task_count",
	"terminal_width",
	"vim",
	"cost",
}

// jsonKeys returns the member names of a JSON object in wire order.
func jsonKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		t.Fatalf("payload is not an object: %v", tok)
	}
	var keys []string
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			t.Fatalf("token: %v", err)
		}
		name, ok := key.(string)
		if !ok {
			t.Fatalf("member name is not a string: %v", key)
		}
		keys = append(keys, name)
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			t.Fatalf("decode %q: %v", name, err)
		}
	}
	return keys
}

// TestPayload_ReferenceKeyOrder pins the member order of the payload.
//
// The order is part of the contract: the reference's `types.StatusLineData`
// is a Go struct, and `encoding/json` writes the members in declaration
// order. A script that reads the document with a streaming parser sees the
// order.
//
// Every member is populated here, so the comparison covers the members the
// reference omits when they are empty as well.
//
// Evidence 【一手】: the captured agy 1.4.7 payload above.
func TestPayload_ReferenceKeyOrder(t *testing.T) {
	reset := time.Date(2026, 10, 25, 18, 0, 20, 0, time.UTC)
	exceeds := false
	payload := statusline.Payload{
		Cwd:               "/tmp/agyprobe/repo",
		SessionID:         "eab2aff5-dfbc-436e-b19d-8c113441f508",
		ConversationID:    "eab2aff5-dfbc-436e-b19d-8c113441f508",
		ConversationTitle: "Probe",
		TranscriptPath:    "/Users/gear/.gemini/antigravity/brain/eab2aff5-dfbc-436e-b19d-8c113441f508/.system_generated/logs/transcript.jsonl",
		Model: &statusline.ModelInfo{
			ID:          "Gemini 3.8 Flash (High)",
			DisplayName: "Gemini 3.8 Flash (High)",
			Effort:      "high",
		},
		Workspace: statusline.Workspace{
			CurrentDir: "/tmp/agyprobe/repo",
			ProjectDir: "/tmp/agyprobe/repo",
		},
		Version: "1.4.7",
		ContextWindow: statusline.ContextWindow{
			TotalInputTokens:    19136,
			TotalOutputTokens:   117,
			ContextWindowSize:   1048576,
			UsedPercentage:      1.824951171875,
			RemainingPercentage: 98.175048828125,
			CurrentUsage: &statusline.CurrentUsage{
				InputTokens:  13327,
				OutputTokens: 117,
			},
		},
		Exceeds200kTokens: &exceeds,
		Agent:             &statusline.AgentInfo{Name: "default"},
		Product:           "antigravity",
		Quota: map[string]statusline.QuotaBucket{
			"3p-5h": {
				RemainingFraction: 1,
				ResetTime:         &reset,
				ResetInSeconds:    17948,
			},
		},
		AgentState:              "idle",
		VCS:                     &statusline.VCSInfo{Type: "git", Branch: "main", Dirty: true},
		CycleMode:               "auto",
		Sandbox:                 &statusline.SandboxInfo{Enabled: false},
		Subagents:               []statusline.SubagentInfo{{Name: "probe", Role: "worker", Status: "running"}},
		ArtifactCount:           2,
		PlanTier:                "Google AI Pro",
		Email:                   "stakingminer@gmail.com",
		PendingInputCount:       1,
		ToolConfirmationPending: true,
		TaskCount:               3,
		TerminalWidth:           120,
		Vim:                     &statusline.VimInfo{Mode: "NORMAL"},
		Cost:                    &statusline.CostInfo{TotalUSD: 1.5, SubagentUSD: 0.5, Estimated: true},
	}

	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got := jsonKeys(t, out)
	want := referenceKeyOrder
	if len(got) != len(want) {
		t.Fatalf("member count = %d, want %d\n%s", len(got), len(want), out)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("member %d = %q, want %q\n%s", i, got[i], want[i], out)
		}
	}
}

// TestPayload_MatchesReferenceBytes pins the bytes, not just the order: the
// clone's document must be byte-identical to the reference's for the same
// values.
func TestPayload_MatchesReferenceBytes(t *testing.T) {
	reset := time.Date(2026, 10, 25, 18, 0, 20, 0, time.UTC)
	exceeds := false
	payload := statusline.Payload{
		Cwd:            "/tmp/agyprobe/repo",
		SessionID:      "eab2aff5-dfbc-436e-b19d-8c113441f508",
		ConversationID: "eab2aff5-dfbc-436e-b19d-8c113441f508",
		TranscriptPath: "/Users/gear/.gemini/antigravity/brain/eab2aff5-dfbc-436e-b19d-8c113441f508/.system_generated/logs/transcript.jsonl",
		Model: &statusline.ModelInfo{
			ID:          "Gemini 3.8 Flash (High)",
			DisplayName: "Gemini 3.8 Flash (High)",
			Effort:      "high",
		},
		Workspace: statusline.Workspace{
			CurrentDir: "/tmp/agyprobe/repo",
			ProjectDir: "/tmp/agyprobe/repo",
		},
		Version: "1.4.7",
		ContextWindow: statusline.ContextWindow{
			TotalInputTokens:    19136,
			TotalOutputTokens:   117,
			ContextWindowSize:   1048576,
			UsedPercentage:      1.824951171875,
			RemainingPercentage: 98.175048828125,
			CurrentUsage: &statusline.CurrentUsage{
				InputTokens:  13327,
				OutputTokens: 117,
			},
		},
		Exceeds200kTokens: &exceeds,
		Product:           "antigravity",
		Quota: map[string]statusline.QuotaBucket{
			"3p-5h": {
				RemainingFraction: 1,
				ResetTime:         &reset,
				ResetInSeconds:    17948,
			},
		},
		AgentState:    "idle",
		VCS:           &statusline.VCSInfo{Type: "git"},
		CycleMode:     "auto",
		Sandbox:       &statusline.SandboxInfo{Enabled: false},
		PlanTier:      "Google AI Pro",
		Email:         "stakingminer@gmail.com",
		TerminalWidth: 120,
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != referencePayload {
		t.Errorf("payload bytes differ from the reference's:\n got %s\nwant %s", out, referencePayload)
	}
}

// TestPayload_OmitRules pins the members the reference omits.
//
// Evidence 【一手】: agy 1.4.7's payloads —
//   - `model` is written as null while the model is being resolved;
//   - `product` is absent while the CLI is still starting up;
//   - `conversation_title` is absent before the conversation has a name;
//   - `quota`, `vcs`, `agent`, `subagents`, `artifact_count`, `plan_tier`,
//     `email`, `pending_input_count`, `tool_confirmation_pending`,
//     `task_count`, `terminal_width`, `vim` and `cost` are absent when
//     empty.
func TestPayload_OmitRules(t *testing.T) {
	payload := statusline.Payload{}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	want := `{"cwd":"","session_id":"","conversation_id":"","model":null,"workspace":{"current_dir":"","project_dir":""},"version":"","context_window":{"total_input_tokens":0,"total_output_tokens":0,"context_window_size":0,"used_percentage":0,"remaining_percentage":0,"current_usage":null},"exceeds_200k_tokens":null}`
	if got != want {
		t.Errorf("empty payload =\n %s\nwant\n %s", got, want)
	}
}

// TestPayload_NullModelIsWritten pins that an unresolved model is written
// as JSON null rather than dropped.
//
// Evidence 【一手】: agy 1.4.7 wrote `"model":null` while the model was
// still being resolved.
func TestPayload_NullModelIsWritten(t *testing.T) {
	out, err := json.Marshal(statusline.Payload{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	value, ok := decoded["model"]
	if !ok {
		t.Fatalf("model member absent: %s", out)
	}
	if value != nil {
		t.Errorf("model = %v, want null", value)
	}
}
