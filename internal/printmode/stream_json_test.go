package printmode

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	gatewayapi "github.com/justwasm/pigasus/internal/infra/backend/gateway"
)

func TestParseStreamInputContent(t *testing.T) {
	// Standard string
	text, err := parseStreamInputContent("Hello world")
	if err != nil || text != "Hello world" {
		t.Errorf("parse string: got %q, err %v", text, err)
	}

	// List of text blocks
	blocks := []any{
		map[string]any{"type": "text", "text": "Part 1. "},
		map[string]any{"type": "text", "text": "Part 2."},
	}
	text, err = parseStreamInputContent(blocks)
	if err != nil || text != "Part 1. Part 2." {
		t.Errorf("parse text blocks: got %q, err %v", text, err)
	}

	// Unsupported block type
	badBlocks := []any{
		map[string]any{"type": "image", "url": "https://example.com/img.png"},
	}
	_, err = parseStreamInputContent(badBlocks)
	if err == nil || !strings.Contains(err.Error(), "text is the only supported block type") {
		t.Errorf("expected rejection of non-text block type, got err: %v", err)
	}

	// Empty string
	_, err = parseStreamInputContent("   ")
	if err == nil {
		t.Errorf("expected error for empty string prompt")
	}
}

func TestRunStreamJSON_MultiTurn(t *testing.T) {
	srv := scriptedGateway(t, "apple\n")
	defer srv.Close()

	origPicker := BackendPicker
	BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
		return gatewayapi.NewGatewayWithConfig(gatewayapi.GatewayConfig{
			URL:           srv.URL,
			ExternalModel: "MiniMax-M3",
			HTTPClient:    srv.Client(),
		})
	}
	defer func() { BackendPicker = origPicker }()

	inputNDJSON := strings.Join([]string{
		`{"event":"user","message":{"content":"Reply with exactly the word: apple. Nothing else."}}`,
		`{"event":"user","message":{"content":"What word did I ask you to reply with in my previous message?"}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	fs := newStandardFlagSetForTest(t, []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	})

	err := RunStreamJSON(&out, strings.NewReader(inputNDJSON), fs)
	if err != nil {
		t.Fatalf("RunStreamJSON: %v", err)
	}

	output := out.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	var initCount, resultCount int
	for _, l := range lines {
		if strings.Contains(l, `"event":"init"`) {
			initCount++
		}
		if strings.Contains(l, `"event":"result"`) {
			resultCount++
		}
	}

	if initCount != 1 {
		t.Errorf("expected exactly 1 init event, got %d. Output:\n%s", initCount, output)
	}
	if resultCount != 2 {
		t.Errorf("expected exactly 2 result events (one per turn), got %d. Output:\n%s", resultCount, output)
	}

	if !strings.Contains(output, `"num_turns":1`) {
		t.Errorf("expected turn 1 to have num_turns:1")
	}
	if !strings.Contains(output, `"num_turns":2`) {
		t.Errorf("expected turn 2 to have num_turns:2")
	}
}

func TestRunStreamJSON_UnsupportedEventWarning(t *testing.T) {
	inputNDJSON := `{"event":"future_thing","data":{}}` + "\n" +
		`{"event":"user","message":{"content":"hello"}}` + "\n"

	srv := scriptedGateway(t, "world\n")
	defer srv.Close()

	origPicker := BackendPicker
	BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
		return gatewayapi.NewGatewayWithConfig(gatewayapi.GatewayConfig{
			URL:           srv.URL,
			ExternalModel: "MiniMax-M3",
			HTTPClient:    srv.Client(),
		})
	}
	defer func() { BackendPicker = origPicker }()

	var out bytes.Buffer
	fs := newStandardFlagSetForTest(t, []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	})

	err := RunStreamJSON(&out, strings.NewReader(inputNDJSON), fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStreamJSON_CLIInternalSlashCommandError(t *testing.T) {
	inputNDJSON := `{"event":"user","message":{"content":"/model"}}` + "\n"

	var out bytes.Buffer
	fs := newStandardFlagSetForTest(t, []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	})

	err := RunStreamJSON(&out, strings.NewReader(inputNDJSON), fs)
	if err == nil {
		t.Fatal("expected error on /model in stream-json mode")
	}
	want := "/model is answered by the CLI itself and is unavailable with --input-format stream-json; run it as its own --print /model invocation"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if !strings.Contains(out.String(), want) {
		t.Errorf("stdout = %q, want error in result event envelope", out.String())
	}
}

func TestRunStreamJSON_UnsupportedBlockTypeRejection(t *testing.T) {
	inputNDJSON := `{"event":"user","message":{"content":[{"type":"image"}]}}` + "\n"

	var out bytes.Buffer
	fs := newStandardFlagSetForTest(t, []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
	})

	err := RunStreamJSON(&out, strings.NewReader(inputNDJSON), fs)
	if err == nil {
		t.Fatal("expected error on unsupported block type")
	}
	if !strings.Contains(err.Error(), "text is the only supported block type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunStreamJSON_FormatAlias(t *testing.T) {
	srv := scriptedGateway(t, "apple\n")
	defer srv.Close()

	origPicker := BackendPicker
	BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
		return gatewayapi.NewGatewayWithConfig(gatewayapi.GatewayConfig{
			URL:           srv.URL,
			ExternalModel: "MiniMax-M3",
			HTTPClient:    srv.Client(),
		})
	}
	defer func() { BackendPicker = origPicker }()

	inputNDJSON := `{"event":"user","message":{"content":"hello"}}` + "\n"

	var out bytes.Buffer
	fs := newStandardFlagSetForTest(t, []string{
		"--format", "stream-json",
	})

	err := RunStreamJSON(&out, strings.NewReader(inputNDJSON), fs)
	if err != nil {
		t.Fatalf("RunStreamJSON with --format: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, `"event":"init"`) {
		t.Errorf("expected init event in output: %s", output)
	}
	if !strings.Contains(output, `"event":"result"`) {
		t.Errorf("expected result event in output: %s", output)
	}
}

func TestRunStreamJSON_EndToEndEventLineFormatting(t *testing.T) {
	srv := scriptedGateway(t, "orange\n")
	defer srv.Close()

	origPicker := BackendPicker
	BackendPicker = func(fs *flag.FlagSet, modelName string, w io.Writer) backend.Client {
		return gatewayapi.NewGatewayWithConfig(gatewayapi.GatewayConfig{
			URL:           srv.URL,
			ExternalModel: "MiniMax-M3",
			HTTPClient:    srv.Client(),
		})
	}
	defer func() { BackendPicker = origPicker }()

	inputNDJSON := `{"event":"user","message":{"content":"say orange"}}` + "\n"

	var out bytes.Buffer
	fs := newStandardFlagSetForTest(t, []string{
		"--format", "stream-json",
	})

	err := RunStreamJSON(&out, strings.NewReader(inputNDJSON), fs)
	if err != nil {
		t.Fatalf("RunStreamJSON: %v", err)
	}

	output := out.String()
	if !strings.HasSuffix(output, "\n") {
		t.Fatalf("output does not end with newline: %q", output)
	}
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	if len(lines) != 4 {
		t.Fatalf("expected 4 event lines, got %d. Output:\n%s", len(lines), output)
	}

	// Verify line 1: init
	var line1 struct {
		Event          string `json:"event"`
		ConversationID string `json:"conversation_id"`
		Init           struct {
			CWD            string   `json:"cwd"`
			Tools          []string `json:"tools"`
			PermissionMode string   `json:"permission_mode"`
		} `json:"init"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &line1); err != nil {
		t.Fatalf("line 1 (init) invalid json: %v. Line: %s", err, lines[0])
	}
	if line1.Event != "init" {
		t.Errorf("line 1 event = %q, want init", line1.Event)
	}
	if line1.ConversationID == "" {
		t.Errorf("line 1 conversation_id is empty")
	}
	if line1.Init.CWD == "" {
		t.Errorf("line 1 init.cwd is empty")
	}
	if len(line1.Init.Tools) == 0 {
		t.Errorf("line 1 init.tools is empty")
	}

	// Verify line 2: step_update (user_input)
	var line2 struct {
		Event      string `json:"event"`
		StepUpdate struct {
			ConversationID string `json:"conversation_id"`
			StepIndex      int    `json:"step_index"`
			State          string `json:"state"`
			StepType       string `json:"step_type"`
		} `json:"step_update"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &line2); err != nil {
		t.Fatalf("line 2 (step_update user_input) invalid json: %v. Line: %s", err, lines[1])
	}
	if line2.Event != "step_update" {
		t.Errorf("line 2 event = %q, want step_update", line2.Event)
	}
	if line2.StepUpdate.StepType != "user_input" {
		t.Errorf("line 2 step_type = %q, want user_input", line2.StepUpdate.StepType)
	}
	if line2.StepUpdate.StepIndex != 0 {
		t.Errorf("line 2 step_index = %d, want 0", line2.StepUpdate.StepIndex)
	}

	// Verify line 3: step_update (agent_response)
	var line3 struct {
		Event      string `json:"event"`
		StepUpdate struct {
			ConversationID string `json:"conversation_id"`
			StepIndex      int    `json:"step_index"`
			State          string `json:"state"`
			StepType       string `json:"step_type"`
			TextDelta      string `json:"text_delta"`
		} `json:"step_update"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &line3); err != nil {
		t.Fatalf("line 3 (step_update agent_response) invalid json: %v. Line: %s", err, lines[2])
	}
	if line3.Event != "step_update" {
		t.Errorf("line 3 event = %q, want step_update", line3.Event)
	}
	if line3.StepUpdate.StepType != "agent_response" {
		t.Errorf("line 3 step_type = %q, want agent_response", line3.StepUpdate.StepType)
	}
	if line3.StepUpdate.StepIndex != 1 {
		t.Errorf("line 3 step_index = %d, want 1", line3.StepUpdate.StepIndex)
	}
	if line3.StepUpdate.TextDelta != "orange\n" {
		t.Errorf("line 3 text_delta = %q, want %q", line3.StepUpdate.TextDelta, "orange\n")
	}

	// Verify line 4: result
	var line4 struct {
		Event  string `json:"event"`
		Result struct {
			ConversationID  string    `json:"conversation_id"`
			Status          string    `json:"status"`
			Response        string    `json:"response"`
			DurationSeconds int       `json:"duration_seconds"`
			NumTurns        int       `json:"num_turns"`
			Usage           jsonUsage `json:"usage"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &line4); err != nil {
		t.Fatalf("line 4 (result) invalid json: %v. Line: %s", err, lines[3])
	}
	if line4.Event != "result" {
		t.Errorf("line 4 event = %q, want result", line4.Event)
	}
	if line4.Result.Status != "SUCCESS" {
		t.Errorf("line 4 result.status = %q, want SUCCESS", line4.Result.Status)
	}
	if line4.Result.Response != "orange\n" {
		t.Errorf("line 4 result.response = %q, want %q", line4.Result.Response, "orange\n")
	}
	if line4.Result.NumTurns != 1 {
		t.Errorf("line 4 result.num_turns = %d, want 1", line4.Result.NumTurns)
	}
}

func TestPrintMode_FormatStreamJSON_SlashCommandParity(t *testing.T) {
	var outOutputFormat, outFormat bytes.Buffer
	fs1 := newStandardFlagSetForTest(t, []string{"--output-format", "stream-json"})
	fs2 := newStandardFlagSetForTest(t, []string{"--format", "stream-json"})

	if err := Run(&outOutputFormat, "/help", fs1); err != nil {
		t.Fatalf("Run(--output-format stream-json): %v", err)
	}
	if err := Run(&outFormat, "/help", fs2); err != nil {
		t.Fatalf("Run(--format stream-json): %v", err)
	}

	if outOutputFormat.String() != outFormat.String() {
		t.Errorf("expected identical stream-json outputs for /help:\noutput-format:\n%s\nformat:\n%s",
			outOutputFormat.String(), outFormat.String())
	}

	// Verify NDJSON lines from stream-json output
	lines := strings.Split(strings.TrimSpace(outFormat.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 NDJSON lines, got %d", len(lines))
	}
	var ev1, ev2 jsonStreamEvent
	if err := json.Unmarshal([]byte(lines[0]), &ev1); err != nil {
		t.Fatalf("line 1 unmarshal: %v", err)
	}
	if ev1.Event != "command_result" {
		t.Errorf("line 1 event = %q, want command_result", ev1.Event)
	}
	if err := json.Unmarshal([]byte(lines[1]), &ev2); err != nil {
		t.Fatalf("line 2 unmarshal: %v", err)
	}
	if ev2.Event != "result" {
		t.Errorf("line 2 event = %q, want result", ev2.Event)
	}
	if ev2.Result == nil || ev2.Result.Status != "SUCCESS" {
		t.Errorf("ev2.Result = %#v, want status SUCCESS", ev2.Result)
	}

	// Compare --output-format json vs --format json
	var outJSON1, outJSON2 bytes.Buffer
	fs3 := newStandardFlagSetForTest(t, []string{"--output-format", "json"})
	fs4 := newStandardFlagSetForTest(t, []string{"--format", "json"})

	if err := Run(&outJSON1, "/help", fs3); err != nil {
		t.Fatalf("Run(--output-format json): %v", err)
	}
	if err := Run(&outJSON2, "/help", fs4); err != nil {
		t.Fatalf("Run(--format json): %v", err)
	}

	if outJSON1.String() != outJSON2.String() {
		t.Errorf("expected identical json outputs for /help:\noutput-format:\n%s\nformat:\n%s",
			outJSON1.String(), outJSON2.String())
	}
}
