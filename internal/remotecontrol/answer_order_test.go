package remotecontrol

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

// TestTheDispatchedAnswerKeepsTheSchemaOrder pins the byte order of a
// unary answer.
//
// The reference's answers are protojson, and protojson writes a
// message's fields in the order the descriptor declares them — not
// alphabetically. The clone's dispatcher answers with Go maps, and
// encoding/json writes a map's keys alphabetically, so the clone's
// answers carried the same data in a different order.
//
// 【一手】 the reference's own captured GetMendelFlags answer carries
// its experiment entries as keyString, disabled, string, json, csv,
// source — the declaration order of
// exa.codeium_common_pb.ExperimentWithVariant (key 1, key_string 5,
// disabled 6, string 2, json 3, csv 4, source 7).
func TestTheDispatchedAnswerKeepsTheSchemaOrder(t *testing.T) {
	installMendelFetcher(t, []auth.MendelFlag{
		{Name: "some-api-flag", Disabled: true},
	})

	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	resText, err := d.DispatchDirect(context.Background(), "GetMendelFlags", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetMendelFlags: %v", err)
	}

	// The head of the answer: the experiment config, then the mesh
	// flag.
	if !strings.HasPrefix(resText, `{"experimentConfig":{"experiments":[`) {
		t.Fatalf("the answer does not start with the experiment config: %s", resText)
	}
	// The first experiment: the language server's own version flag.
	want := `{"keyString":"remote-control-version","string":"v2","source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"}`
	if !strings.Contains(resText, want) {
		t.Fatalf("the first experiment is not in the schema's order: %s", resText)
	}
	// A flag the backend serves: the disabled flag first, then the
	// source.
	want = `{"keyString":"some-api-flag","disabled":true,"source":"EXPERIMENT_SOURCE_LANGUAGE_SERVER"}`
	if !strings.Contains(resText, want) {
		t.Fatalf("the backend's flag is not in the schema's order: %s", resText)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(resText), &decoded); err != nil {
		t.Fatalf("the answer does not parse: %v", err)
	}
}

// TestTheStateFrameKeepsTheSchemaOrder pins the same rule for the
// state stream's frame: the app state, then the user config, and the
// app state's own fields in the descriptor's declaration order.
func TestTheStateFrameKeepsTheSchemaOrder(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	frame, err := marshalAnswer("JetboxSubscribeToState", d.state.snapshot())
	if err != nil {
		t.Fatalf("marshalAnswer: %v", err)
	}
	if !strings.HasPrefix(string(frame), `{"appState":`) {
		t.Fatalf("the frame does not start with the app state: %s", frame)
	}
}

// TestTheAgentStateFrameKeepsTheSchemaOrder pins the pushed frame's
// own order.
//
// The frame is exa.jetski_cortex_pb.StreamAgentStateUpdatesResponse —
// the `update` member — and the update's fields go out in the
// descriptor's declaration order: conversationId, trajectoryId,
// status, executableStatus, executorLoopStatus, mainTrajectoryUpdate,
// fullyIdle, pendingAgentMessagesUpdate.
//
// 【反编译】 exa.jetski_cortex_pb.AgentStateUpdate: conversation_id
// 1, trajectory_id 2, status 3, executable_status 4,
// executor_loop_status 5, executor_metadata 6, main_trajectory_update
// 7, fully_idle 18, pending_agent_messages_update 20.
func TestTheAgentStateFrameKeepsTheSchemaOrder(t *testing.T) {
	frame := map[string]any{
		"update": map[string]any{
			"conversationId":     "c1",
			"trajectoryId":       "t1",
			"status":             "CASCADE_RUN_STATUS_IDLE",
			"executableStatus":   "CASCADE_RUN_STATUS_IDLE",
			"executorLoopStatus": "CASCADE_RUN_STATUS_IDLE",
			"fullyIdle":          true,
			"mainTrajectoryUpdate": map[string]any{
				"status": "CASCADE_RUN_STATUS_IDLE",
			},
		},
	}
	raw, err := marshalLSMessage("exa.jetski_cortex_pb.StreamAgentStateUpdatesResponse", frame)
	if err != nil {
		t.Fatalf("marshalLSMessage: %v", err)
	}
	want := `{"update":{"conversationId":"c1","trajectoryId":"t1","status":"CASCADE_RUN_STATUS_IDLE",` +
		`"executableStatus":"CASCADE_RUN_STATUS_IDLE","executorLoopStatus":"CASCADE_RUN_STATUS_IDLE",` +
		`"mainTrajectoryUpdate":{"status":"CASCADE_RUN_STATUS_IDLE"},"fullyIdle":true}}`
	if got := string(raw); got != want {
		t.Fatalf("frame:\n got %s\nwant %s", got, want)
	}
}
