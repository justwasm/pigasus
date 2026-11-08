// Package remotecontrol — ls_interaction_test.go: the interactive
// tools' round trip through the Web UI.
//
// The contract under test is the one the IDE's own web bundle speaks:
//
//   - the tool publishes a WAITING step (CORTEX_STEP_STATUS_WAITING)
//     that carries the question,
//   - the answer arrives as HandleCascadeUserInteraction, naming the
//     trajectory and the step,
//   - the tool resumes with the user's answer as its result.
package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// lsInteractionFixture is one conversation with its dispatcher, its
// cascade and its trajectory.
type lsInteractionFixture struct {
	dispatcher *Dispatcher
	cascade    *cascades.Cascade
	traj       *lsTrajectory
}

// newLSInteractionFixture wires one conversation into a dispatcher.
func newLSInteractionFixture(t *testing.T) *lsInteractionFixture {
	t.Helper()
	registry := cascades.NewRegistry(4)
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess, Cascades: registry})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	c, err := cascades.New(cascades.Opts{ID: "cas-interaction", Model: "test-model"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := registry.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return &lsInteractionFixture{dispatcher: d, cascade: c, traj: d.trajectories.Get(c.ID)}
}

// TestLSInteraction_AskQuestionRoundTrip walks the whole round trip:
// the tool asks, the step appears, the answer RPC resolves it, and the
// step ends up done.
func TestLSInteraction_AskQuestionRoundTrip(t *testing.T) {
	f := newLSInteractionFixture(t)
	host := &lsInteractionHost{cascade: f.cascade, traj: f.traj}
	req := interaction.Request{
		Kind:     interaction.KindQuestion,
		ToolName: "ask_question",
		Questions: []interaction.Question{
			{Prompt: "Which tea?", Options: []string{"Tea", "Coffee"}},
		},
	}

	type result struct {
		ans interaction.Answer
		err error
	}
	done := make(chan result, 1)
	go func() {
		ans, err := host.Ask(context.Background(), req)
		done <- result{ans: ans, err: err}
	}()

	// The waiting step is on the trajectory, and it says everything
	// the Web UI's card reads.
	step := lsWaitForStep(t, f.traj)
	if got := step["type"]; got != lsStepTypeAskQuestion {
		t.Errorf("step type = %v, want %d", got, lsStepTypeAskQuestion)
	}
	if got := step["status"]; got != lsStepStatusWaiting {
		t.Errorf("step status = %v, want %d (WAITING)", got, lsStepStatusWaiting)
	}
	questions := lsStepQuestions(t, step)
	if len(questions) != 1 {
		t.Fatalf("questions = %d, want 1", len(questions))
	}
	if questions[0]["question"] != "Which tea?" {
		t.Errorf("question = %v, want %q", questions[0]["question"], "Which tea?")
	}
	options, _ := questions[0]["options"].([]any)
	if len(options) != 2 {
		t.Fatalf("options = %d, want 2", len(options))
	}
	first, _ := options[0].(map[string]any)
	if first["id"] != "1" || first["text"] != "Tea" {
		t.Errorf("option 0 = %v, want id 1 / Tea", first)
	}
	// The step carries its own position: the answer names the step by
	// it, and the Web UI groups the conversation's steps by it.
	info := lsStepInfo(t, step)
	if info["trajectoryId"] != f.traj.Snapshot().TrajectoryID {
		t.Errorf("step info trajectoryId = %v, want %v", info["trajectoryId"], f.traj.Snapshot().TrajectoryID)
	}
	if info["stepIndex"] != 0 {
		t.Errorf("step info stepIndex = %v, want 0", info["stepIndex"])
	}

	// The answer arrives the way the Web UI sends it: the trajectory
	// id, the step index, and the oneof member at the top level.
	params := map[string]any{
		"cascadeId": f.cascade.ID,
		"interaction": map[string]any{
			"trajectoryId": f.traj.Snapshot().TrajectoryID,
			"stepIndex":    0,
			"askQuestion": map[string]any{
				"responses": []any{
					map[string]any{
						"question":          "Which tea?",
						"options":           []any{map[string]any{"id": "1", "text": "Tea"}},
						"selectedOptionIds": []any{"1"},
						"writeInResponse":   "",
						"skipped":           false,
					},
				},
				"cancelled": false,
			},
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal params: %v", err)
	}
	if _, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw); rpcErr != nil {
		t.Fatalf("HandleCascadeUserInteraction: %v", rpcErr)
	}

	select {
	case res := <-done:
		if res.err != nil {
			t.Fatalf("Ask: %v", res.err)
		}
		if res.ans.Text != "Tea" {
			t.Errorf("answer text = %q, want %q", res.ans.Text, "Tea")
		}
		if res.ans.Cancelled {
			t.Errorf("answer cancelled = true, want false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the tool did not resume after the answer")
	}

	// The step is done, and it carries the exchange.
	finished := f.traj.Snapshot().Steps[0]
	if got := finished["status"]; got != lsStepStatusDone {
		t.Errorf("finished status = %v, want %d (DONE)", got, lsStepStatusDone)
	}
	// The exchange is read back through JSON: the Web UI decodes the
	// step with protobuf-es, so the wire shape is what matters.
	wire := map[string]any{}
	rawStep, err := json.Marshal(finished)
	if err != nil {
		t.Fatalf("Marshal step: %v", err)
	}
	if err := json.Unmarshal(rawStep, &wire); err != nil {
		t.Fatalf("Unmarshal step: %v", err)
	}
	completed, _ := wire["completedInteractions"].([]any)
	if len(completed) != 1 {
		t.Fatalf("completedInteractions = %d, want 1", len(completed))
	}
	entry, _ := completed[0].(map[string]any)
	response, _ := entry["response"].(map[string]any)
	if response == nil {
		t.Fatalf("completed interaction has no response: %v", entry)
	}
	if response["trajectoryId"] != f.traj.Snapshot().TrajectoryID {
		t.Errorf("completed response trajectoryId = %v, want %v", response["trajectoryId"], f.traj.Snapshot().TrajectoryID)
	}
	block, _ := response["askQuestion"].(map[string]any)
	if block == nil {
		t.Fatalf("completed response has no askQuestion: %v", response)
	}
	responses, _ := block["responses"].([]any)
	if len(responses) != 1 {
		t.Fatalf("completed responses = %d, want 1", len(responses))
	}
	answered, _ := responses[0].(map[string]any)
	ids, _ := answered["selectedOptionIds"].([]any)
	if len(ids) != 1 || ids[0] != "1" {
		t.Errorf("completed selectedOptionIds = %v, want [1]", ids)
	}
	// proto-JSON leaves the default fields out, and so does the
	// reference's own answer payload.
	for _, key := range []string{"cancelled", "writeInResponse", "skipped"} {
		if _, ok := answered[key]; ok {
			t.Errorf("the completed answer carries %q, want it left out", key)
		}
	}
	if _, ok := block["cancelled"]; ok {
		t.Errorf("the completed response carries \"cancelled\", want it left out")
	}
}

// TestLSInteraction_PermissionRoundTrip walks the permission card's
// round trip: the tool asks, the answer allows (or denies), and the
// tool resumes with the verdict.
func TestLSInteraction_PermissionRoundTrip(t *testing.T) {
	for _, allow := range []bool{true, false} {
		f := newLSInteractionFixture(t)
		host := &lsInteractionHost{cascade: f.cascade, traj: f.traj}
		req := interaction.Request{
			Kind:     interaction.KindPermission,
			ToolName: "ask_permission",
			Action:   "write",
			Target:   "/etc/hosts",
		}

		type result struct {
			ans interaction.Answer
			err error
		}
		done := make(chan result, 1)
		go func() {
			ans, err := host.Ask(context.Background(), req)
			done <- result{ans: ans, err: err}
		}()

		step := lsWaitForStep(t, f.traj)
		if got := step["status"]; got != lsStepStatusWaiting {
			t.Errorf("step status = %v, want %d (WAITING)", got, lsStepStatusWaiting)
		}
		requested, _ := step["requestedInteraction"].(map[string]any)
		permission, _ := requested["permission"].(map[string]any)
		if permission == nil {
			t.Fatalf("the permission step has no requestedInteraction.permission: %v", requested)
		}
		resource, _ := permission["resource"].(map[string]any)
		if resource["action"] != "write" || resource["target"] != "/etc/hosts" {
			t.Errorf("permission resource = %v, want write / /etc/hosts", resource)
		}

		params := map[string]any{
			"cascadeId": f.cascade.ID,
			"interaction": map[string]any{
				"trajectoryId": f.traj.Snapshot().TrajectoryID,
				"stepIndex":    0,
				"permission":   map[string]any{"allow": allow, "scope": 1},
			},
		}
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("Marshal params: %v", err)
		}
		if _, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw); rpcErr != nil {
			t.Fatalf("HandleCascadeUserInteraction: %v", rpcErr)
		}

		select {
		case res := <-done:
			if res.err != nil {
				t.Fatalf("Ask: %v", res.err)
			}
			if res.ans.Allowed != allow {
				t.Errorf("allowed = %v, want %v", res.ans.Allowed, allow)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the tool did not resume after the answer")
		}
	}
}

// TestLSInteraction_UnknownStepIsRejected pins the failure mode: an
// answer that names no waiting step is refused, not silently dropped.
func TestLSInteraction_UnknownStepIsRejected(t *testing.T) {
	f := newLSInteractionFixture(t)
	params := map[string]any{
		"cascadeId": f.cascade.ID,
		"interaction": map[string]any{
			"trajectoryId": f.traj.Snapshot().TrajectoryID,
			"stepIndex":    7,
			"askQuestion":  map[string]any{"responses": []any{}, "cancelled": false},
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Marshal params: %v", err)
	}
	_, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw)
	if rpcErr == nil {
		t.Fatal("the answer for an unknown step was accepted")
	}
	if rpcErr.Code != rpc.ErrCodeInteractionNotFound {
		t.Errorf("error code = %d, want %d", rpcErr.Code, rpc.ErrCodeInteractionNotFound)
	}
}

// TestLSResponseText pins the answer rendering: it is the bundle's own
// rendering, word for word.
func TestLSResponseText(t *testing.T) {
	question := interaction.Question{Prompt: "Which tea?", Options: []string{"Tea", "Coffee"}}
	cases := []struct {
		name  string
		entry lsAskQuestionResponse
		want  string
	}{
		{
			name:  "skipped",
			entry: lsAskQuestionResponse{Skipped: true},
			want:  "Skipped",
		},
		{
			name:  "nothing picked",
			entry: lsAskQuestionResponse{},
			want:  "No answer provided",
		},
		{
			name:  "one option",
			entry: lsAskQuestionResponse{SelectedOptionIDs: []string{"2"}},
			want:  "Coffee",
		},
		{
			name:  "write-in only",
			entry: lsAskQuestionResponse{WriteInResponse: "Latte"},
			want:  "Latte (write-in)",
		},
		{
			name:  "option and write-in",
			entry: lsAskQuestionResponse{SelectedOptionIDs: []string{"1"}, WriteInResponse: "Latte"},
			want:  "Tea, Latte (write-in)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lsResponseText(tc.entry, question); got != tc.want {
				t.Errorf("lsResponseText = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLSAskQuestionEntriesCarriesMultiSelect pins the Web UI contract:
// the card decides between radio buttons and checkboxes from the entry's
// isMultiSelect flag.
func TestLSAskQuestionEntriesCarriesMultiSelect(t *testing.T) {
	entries := lsAskQuestionEntries([]interaction.Question{
		{Prompt: "Pick", Options: []string{"A", "B"}, IsMultiSelect: true},
		{Prompt: "One", Options: []string{"C", "D"}},
	})
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	first, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entry = %T, want map", entries[0])
	}
	if got := first["isMultiSelect"]; got != true {
		t.Errorf("isMultiSelect = %v, want true", got)
	}
	second := entries[1].(map[string]any)
	if got := second["isMultiSelect"]; got != false {
		t.Errorf("second isMultiSelect = %v, want false", got)
	}
}

// TestLSAnswerBuildsPerQuestionAnswers pins that the Web UI's per-question
// responses reach the tool separately, so it can number them itself.
func TestLSAnswerBuildsPerQuestionAnswers(t *testing.T) {
	req := interaction.Request{Questions: []interaction.Question{
		{Prompt: "One?", Options: []string{"A", "B"}},
		{Prompt: "Two?", Options: []string{"C", "D"}},
	}}
	cue := lsCascadeUserInteraction{AskQuestion: &lsAskQuestionBlock{
		Responses: []lsAskQuestionResponse{
			{SelectedOptionIDs: []string{"1"}},
			{SelectedOptionIDs: []string{"2"}},
		},
	}}

	answer := cue.answer(req)
	if len(answer.Answers) != 2 {
		t.Fatalf("answers = %#v, want 2", answer.Answers)
	}
	if answer.Answers[0] != "A" || answer.Answers[1] != "D" {
		t.Errorf("answers = %#v, want [A D]", answer.Answers)
	}
	if answer.Text != "A\nD" {
		t.Errorf("text = %q, want %q", answer.Text, "A\nD")
	}
}

// lsWaitForStep waits for the trajectory's first step and returns it.
func lsWaitForStep(t *testing.T, traj *lsTrajectory) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := traj.Snapshot()
		if len(snap.Steps) > 0 {
			return snap.Steps[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no step appeared on the trajectory")
	return nil
}

// lsStepQuestions returns the questions the step carries.
func lsStepQuestions(t *testing.T, step map[string]any) []map[string]any {
	t.Helper()
	block, _ := step["askQuestion"].(map[string]any)
	if block == nil {
		t.Fatalf("the step has no askQuestion: %v", step)
	}
	raw, _ := block["questions"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		out = append(out, entry)
	}
	return out
}

// lsStepInfo returns the step's own position.
func lsStepInfo(t *testing.T, step map[string]any) map[string]any {
	t.Helper()
	meta, _ := step["metadata"].(map[string]any)
	if meta == nil {
		t.Fatalf("the step has no metadata: %v", step)
	}
	info, _ := meta["sourceTrajectoryStepInfo"].(map[string]any)
	if info == nil {
		t.Fatalf("the step has no sourceTrajectoryStepInfo: %v", meta)
	}
	return info
}

// TestLSInteraction_FilePermissionRoundTrip covers the file permission card dialect.
func TestLSInteraction_FilePermissionRoundTrip(t *testing.T) {
	for _, allow := range []bool{true, false} {
		f := newLSInteractionFixture(t)
		host := &lsInteractionHost{cascade: f.cascade, traj: f.traj}
		req := interaction.Request{
			Kind:     interaction.KindPermission,
			ToolName: "ask_permission",
			Action:   "write",
			Target:   "/tmp/test-file.txt",
		}

		done := make(chan interaction.Answer, 1)
		go func() {
			ans, _ := host.Ask(context.Background(), req)
			done <- ans
		}()

		step := lsWaitForStep(t, f.traj)
		if got := step["status"]; got != lsStepStatusWaiting {
			t.Errorf("step status = %v, want WAITING", got)
		}

		params := map[string]any{
			"cascadeId": f.cascade.ID,
			"interaction": map[string]any{
				"trajectoryId": f.traj.Snapshot().TrajectoryID,
				"stepIndex":    0,
				"filePermission": map[string]any{
					"allow":           allow,
					"scope":           1,
					"absolutePathUri": "file:///tmp/test-file.txt",
				},
			},
		}
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if _, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw); rpcErr != nil {
			t.Fatalf("HandleCascadeUserInteraction: %v", rpcErr)
		}

		select {
		case ans := <-done:
			if ans.Allowed != allow {
				t.Fatalf("allowed = %v, want %v", ans.Allowed, allow)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("tool did not resume")
		}
	}
}

// TestLSInteraction_ApprovalRoundTrip covers the generic approvalInteraction dialect.
func TestLSInteraction_ApprovalRoundTrip(t *testing.T) {
	for _, confirm := range []bool{true, false} {
		f := newLSInteractionFixture(t)
		host := &lsInteractionHost{cascade: f.cascade, traj: f.traj}
		req := interaction.Request{
			Kind:     interaction.KindPermission,
			ToolName: "ask_permission",
			Action:   "confirm_action",
		}

		done := make(chan interaction.Answer, 1)
		go func() {
			ans, _ := host.Ask(context.Background(), req)
			done <- ans
		}()

		_ = lsWaitForStep(t, f.traj)
		params := map[string]any{
			"cascadeId": f.cascade.ID,
			"interaction": map[string]any{
				"trajectoryId": f.traj.Snapshot().TrajectoryID,
				"stepIndex":    0,
				"approvalInteraction": map[string]any{
					"confirm": confirm,
				},
			},
		}
		raw, _ := json.Marshal(params)
		if _, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw); rpcErr != nil {
			t.Fatalf("HandleCascadeUserInteraction: %v", rpcErr)
		}

		select {
		case ans := <-done:
			if ans.Allowed != confirm {
				t.Fatalf("allowed = %v, want %v", ans.Allowed, confirm)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("tool did not resume")
		}
	}
}

// TestLSInteraction_RunCommandRoundTrip covers the runCommand dialect.
func TestLSInteraction_RunCommandRoundTrip(t *testing.T) {
	f := newLSInteractionFixture(t)
	host := &lsInteractionHost{cascade: f.cascade, traj: f.traj}
	req := interaction.Request{
		Kind:     interaction.KindPermission,
		ToolName: "run_command",
		Action:   "execute",
		Target:   "go build",
	}

	done := make(chan interaction.Answer, 1)
	go func() {
		ans, _ := host.Ask(context.Background(), req)
		done <- ans
	}()

	_ = lsWaitForStep(t, f.traj)
	params := map[string]any{
		"cascadeId": f.cascade.ID,
		"interaction": map[string]any{
			"trajectoryId": f.traj.Snapshot().TrajectoryID,
			"stepIndex":    0,
			"runCommand": map[string]any{
				"confirm":              true,
				"proposedCommandLine":  "go build",
				"submittedCommandLine": "go test ./...",
			},
		},
	}
	raw, _ := json.Marshal(params)
	if _, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw); rpcErr != nil {
		t.Fatalf("HandleCascadeUserInteraction: %v", rpcErr)
	}

	select {
	case ans := <-done:
		if !ans.Allowed {
			t.Fatalf("allowed = false, want true")
		}
		if ans.Text != "go test ./..." {
			t.Fatalf("text = %q, want %q", ans.Text, "go test ./...")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tool did not resume")
	}
}

// TestLSInteraction_ElicitationRoundTrip covers the elicitation card dialect.
func TestLSInteraction_ElicitationRoundTrip(t *testing.T) {
	f := newLSInteractionFixture(t)
	host := &lsInteractionHost{cascade: f.cascade, traj: f.traj}
	req := interaction.Request{
		Kind:     interaction.KindQuestion,
		ToolName: "elicitation",
	}

	done := make(chan interaction.Answer, 1)
	go func() {
		ans, _ := host.Ask(context.Background(), req)
		done <- ans
	}()

	_ = lsWaitForStep(t, f.traj)
	params := map[string]any{
		"cascadeId": f.cascade.ID,
		"interaction": map[string]any{
			"trajectoryId": f.traj.Snapshot().TrajectoryID,
			"stepIndex":    0,
			"elicitation": map[string]any{
				"action":      "submit",
				"contentJson": `{"api_key":"secret"}`,
			},
		},
	}
	raw, _ := json.Marshal(params)
	if _, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw); rpcErr != nil {
		t.Fatalf("HandleCascadeUserInteraction: %v", rpcErr)
	}

	select {
	case ans := <-done:
		if ans.Text != `{"api_key":"secret"}` {
			t.Fatalf("text = %q, want %q", ans.Text, `{"api_key":"secret"}`)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tool did not resume")
	}
}

// TestLSInteraction_EmptyCascadeID verifies that an empty cascadeId succeeds with {}.
func TestLSInteraction_EmptyCascadeID(t *testing.T) {
	f := newLSInteractionFixture(t)
	raw := []byte(`{"cascadeId":""}`)
	ans, rpcErr := f.dispatcher.routeMethod(context.Background(), "HandleCascadeUserInteraction", raw)
	if rpcErr != nil {
		t.Fatalf("HandleCascadeUserInteraction empty cascadeId: %v", rpcErr)
	}
	m, _ := ans.(map[string]any)
	if m == nil || len(m) != 0 {
		t.Fatalf("answer = %v, want {}", ans)
	}
}

// TestLSTrajectory_RehydrateFromConvstore verifies that historical conversations on disk
// are dynamically rehydrated by lsSnapshot and returned by GetCascadeTrajectory / GetAllCascadeTrajectories.
func TestLSTrajectory_RehydrateFromConvstore(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	convID := "conv-rehydrated-test"
	stored := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Text: "historical user request"},
		{Idx: 1, Type: convstore.StepModelResponse, Text: "historical model response"},
	}
	if err := convstore.AppendConversationSteps(convID, "/tmp/workspace", stored); err != nil {
		t.Fatalf("AppendConversationSteps: %v", err)
	}

	registry := cascades.NewRegistry(4)
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess, Cascades: registry})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// 1. GetCascadeTrajectory should rehydrate and succeed
	req, _ := json.Marshal(map[string]any{"cascadeId": convID})
	ans, rpcErr := d.routeMethod(context.Background(), "GetCascadeTrajectory", req)
	if rpcErr != nil {
		t.Fatalf("GetCascadeTrajectory rpcErr: %v", rpcErr)
	}
	ansMap, ok := ans.(map[string]any)
	if !ok {
		t.Fatalf("GetCascadeTrajectory answer is not a map: %v", ans)
	}
	traj, _ := ansMap["trajectory"].(map[string]any)
	if traj == nil {
		t.Fatalf("GetCascadeTrajectory missing trajectory: %v", ansMap)
	}
	steps, _ := traj["steps"].([]map[string]any)
	if len(steps) != 2 {
		t.Fatalf("GetCascadeTrajectory steps = %d, want 2", len(steps))
	}

	// 2. GetAllCascadeTrajectories should discover the conversation on disk and include it
	allAns, rpcErr := d.routeMethod(context.Background(), "GetAllCascadeTrajectories", []byte(`{}`))
	if rpcErr != nil {
		t.Fatalf("GetAllCascadeTrajectories rpcErr: %v", rpcErr)
	}
	allMap, _ := allAns.(map[string]any)
	summaries, _ := allMap["trajectorySummaries"].(map[string]any)
	if summaries == nil || summaries[convID] == nil {
		t.Fatalf("GetAllCascadeTrajectories missing summary for %s: %v", convID, allMap)
	}
}

// TestLSInteraction_TheSinkInstallsTheHost pins the seam the cascade
// domain uses to put the Web UI's interaction host on a turn's context.
//
// The domain owns the turn; the sink owns the Web UI's surface. So the
// domain asks the sink (cascades.InteractionHostInstaller) whether it
// can install a host — the dependency runs from the domain to the
// host, never the other way round.
//
// A sink that answers "no" leaves the turn headless: the interactive
// tools keep their headless behaviour and the Web UI's permission card
// never appears. Both of the sinks the Web UI's turn path installs must
// therefore answer "yes", and the context they hand back must carry
// the host.
func TestLSInteraction_TheSinkInstallsTheHost(t *testing.T) {
	f := newLSInteractionFixture(t)
	connSink := NewConnNotifierSink(f.dispatcher, "", f.cascade)
	trajSink := newLSTrajectorySink(f.traj, connSink)
	thoughtSink := newLSThoughtSink(trajSink)

	sinks := []struct {
		name string
		sink cascades.EventSink
	}{
		{"trajectory", trajSink},
		{"thought", thoughtSink},
	}
	for _, s := range sinks {
		installer, ok := s.sink.(cascades.InteractionHostInstaller)
		if !ok {
			t.Errorf("%s sink does not implement cascades.InteractionHostInstaller", s.name)
			continue
		}
		ctx := installer.InstallInteractionHost(context.Background(), f.cascade, s.sink)
		if interaction.HostFrom(ctx) == nil {
			t.Errorf("%s sink installed no interaction host", s.name)
		}
	}
}
