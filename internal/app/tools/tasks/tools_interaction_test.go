// tools_interaction_test.go: the interactive tools when a transport can
// reach the user.
//
// The three tools that need the user — ask_question, ask_permission,
// ask_custom_permission — take the first transport that exists: the
// turn's interaction host (internal/core/interaction), the subagent-question
// store, or nobody (headless).
package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/interaction"
)

// fakeInteractionHost answers with a canned answer and keeps the
// request it was asked.
type fakeInteractionHost struct {
	request interaction.Request
	answer  interaction.Answer
	err     error
}

// Ask records the request and returns the canned answer.
func (f *fakeInteractionHost) Ask(_ context.Context, req interaction.Request) (interaction.Answer, error) {
	f.request = req
	return f.answer, f.err
}

// TestAskQuestion_AsksTheHost pins the question path: the tool asks
// through the host and the host's answer becomes the tool's result.
func TestAskQuestion_AsksTheHost(t *testing.T) {
	host := &fakeInteractionHost{answer: interaction.Answer{Text: "Tea"}}
	ctx := interaction.WithHost(context.Background(), host)

	got, err := (&AskQuestion{}).Run(ctx, json.RawMessage(`{"questions":[{"question":"Which tea?","options":["Tea","Coffee"]}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != "A1: Tea" {
		t.Errorf("ask_question = %q, want %q", got, "A1: Tea")
	}
	if host.request.Kind != interaction.KindQuestion {
		t.Errorf("request kind = %q, want %q", host.request.Kind, interaction.KindQuestion)
	}
	if host.request.ToolName != "ask_question" {
		t.Errorf("request tool = %q, want ask_question", host.request.ToolName)
	}
	if len(host.request.Questions) != 1 || host.request.Questions[0].Prompt != "Which tea?" {
		t.Errorf("request questions = %+v", host.request.Questions)
	}
	if len(host.request.Questions[0].Options) != 2 {
		t.Errorf("request options = %v, want 2", host.request.Questions[0].Options)
	}
	if !strings.Contains(host.request.ArgsJSON, "Which tea?") {
		t.Errorf("request args = %q, want the tool's own arguments", host.request.ArgsJSON)
	}
}

// TestAskQuestion_Dismissed pins the dismissed path: the tool says the
// question was not answered, so the model does not read an empty
// answer as an answer.
func TestAskQuestion_Dismissed(t *testing.T) {
	host := &fakeInteractionHost{answer: interaction.Answer{Cancelled: true}}
	ctx := interaction.WithHost(context.Background(), host)

	got, err := (&AskQuestion{}).Run(ctx, json.RawMessage(`{"questions":[{"question":"Which tea?"}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != "A1: User Skipped" {
		t.Errorf("ask_question = %q, want %q", got, "A1: User Skipped")
	}
}

// TestAskQuestion_OnAskFiresBeforeWait pins the auto-open seam: the
// tool notifies the host as soon as it enqueues, before it blocks, so
// the picker can come up while the turn waits.
func TestAskQuestion_OnAskFiresBeforeWait(t *testing.T) {
	mgr := subagentquestions.NewManager(false)
	asked := make(chan string, 1)
	tool := &AskQuestion{
		SubAgentQuestions: mgr,
		SubAgentID:        subagentquestions.ParentKey,
		OnAsk:             func(scope string) { asked <- scope },
	}

	resCh := make(chan any, 1)
	go func() {
		out, err := tool.Run(context.Background(), json.RawMessage(`{"questions":[{"question":"Which tea?","options":["Tea"]}]}`))
		if err != nil {
			t.Errorf("ask_question: %v", err)
		}
		resCh <- out
	}()

	select {
	case scope := <-asked:
		if scope != subagentquestions.ParentKey {
			t.Errorf("OnAsk scope = %q, want the parent scope", scope)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnAsk did not fire before Wait")
	}

	pending := mgr.Pending(subagentquestions.ParentKey)
	if len(pending) != 1 {
		t.Fatalf("pending questions = %d, want 1", len(pending))
	}
	if err := mgr.Answer(subagentquestions.ParentKey, pending[0].ID, "Tea"); err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if got := <-resCh; got != "A1: Tea" {
		t.Errorf("ask_question = %q, want %q", got, "A1: Tea")
	}
}

// TestAskQuestion_NumbersEveryAnswer pins the multi-question result:
// each answer is numbered, so the model can tell which question it
// belongs to.
func TestAskQuestion_NumbersEveryAnswer(t *testing.T) {
	mgr := subagentquestions.NewManager(false)
	tool := &AskQuestion{
		SubAgentQuestions: mgr,
		SubAgentID:        subagentquestions.ParentKey,
	}
	resCh := make(chan any, 1)
	go func() {
		out, _ := tool.Run(context.Background(), json.RawMessage(`{"questions":[{"question":"One?","options":["a"]},{"question":"Two?","options":["b"]}]}`))
		resCh <- out
	}()

	for _, answer := range []string{"a", "b"} {
		deadline := time.After(5 * time.Second)
		for {
			pending := mgr.Pending(subagentquestions.ParentKey)
			if len(pending) > 0 {
				if err := mgr.Answer(subagentquestions.ParentKey, pending[0].ID, answer); err != nil {
					t.Fatalf("Answer: %v", err)
				}
				break
			}
			select {
			case <-deadline:
				t.Fatalf("question %q never enqueued", answer)
			case <-time.After(time.Millisecond):
			}
		}
	}
	if got := <-resCh; got != "A1: a\nA2: b" {
		t.Errorf("ask_question = %q, want %q", got, "A1: a\nA2: b")
	}
}

// TestAskPermission_AsksTheHost pins the permission path: a refusal
// is the reference's own denial text, a grant names the action.
func TestAskPermission_AsksTheHost(t *testing.T) {
	for _, allow := range []bool{true, false} {
		host := &fakeInteractionHost{answer: interaction.Answer{Allowed: allow}}
		ctx := interaction.WithHost(context.Background(), host)

		got, err := (&AskPermission{}).Run(ctx, json.RawMessage(`{"action":"run_command","target":"date"}`))
		if err != nil {
			t.Fatalf("ask_permission: %v", err)
		}
		if allow {
			if !strings.Contains(got.(string), "run_command") || !strings.Contains(got.(string), "date") {
				t.Errorf("ask_permission = %q, want the allowed action and target", got)
			}
		} else if !strings.Contains(got.(string), "was denied.") {
			t.Errorf("ask_permission = %q, want the reference's denial text", got)
		}
		if host.request.Kind != interaction.KindPermission {
			t.Errorf("request kind = %q, want %q", host.request.Kind, interaction.KindPermission)
		}
		if host.request.Action != "run_command" || host.request.Target != "date" {
			t.Errorf("request = %+v, want run_command / date", host.request)
		}
	}
}

// TestAskCustomPermission_AsksTheHost pins the custom-permission path:
// the reason rides along with the request.
func TestAskCustomPermission_AsksTheHost(t *testing.T) {
	host := &fakeInteractionHost{answer: interaction.Answer{Allowed: true}}
	ctx := interaction.WithHost(context.Background(), host)

	if _, err := (&AskCustomPermission{}).Run(ctx, json.RawMessage(`{"action":"a","target":"t","reason":"because"}`)); err != nil {
		t.Fatalf("ask_custom_permission: %v", err)
	}
	if host.request.Reason != "because" {
		t.Errorf("request reason = %q, want %q", host.request.Reason, "because")
	}
	if host.request.ToolName != "ask_custom_permission" {
		t.Errorf("request tool = %q, want ask_custom_permission", host.request.ToolName)
	}
}

// TestSendMessageTool_RejectsTaskInput pins the guard: a message addressed
// at a background task is refused, because the reference's own wording
// points the model at manage_task's send_input action instead.
//
// The task is a real background command, started through the shell
// capability's own door, so the table holds the id the tool looks up.
func TestSendMessageTool_RejectsTaskInput(t *testing.T) {
	id := launchBackgroundTask(t, "conv-send-message", "cat")

	_, err := (&SendMessageTool{}).Run(context.Background(), json.RawMessage(`{"Recipient":"`+id+`","Message":"hello"}`))
	if err == nil {
		t.Fatalf("expected error when sending message to background task ID, got nil")
	}
	want := "send_message should not be used to provide input to tasks; use the manage_task tool with action 'send_input' instead"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// The shell capability owns the background-task table; the task capability
// reaches it only through the exported doors. This test pins that the door
// the message guard uses answers for a live task.
func TestSendMessageTool_AcceptsANonTaskRecipient(t *testing.T) {
	out, err := (&SendMessageTool{}).Run(context.Background(), json.RawMessage(`{"Recipient":"conv-other","Message":"hello"}`))
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "conv-other") {
		t.Errorf("send_message = %q, want the recipient named", out)
	}
}

// The table's own door is what the task tools use; this test keeps the
// contract explicit.
func TestBackgroundTaskDoorAnswersForALiveTask(t *testing.T) {
	id := launchBackgroundTask(t, "conv-door", "cat")
	if _, ok := shell.FindBackgroundTask(id); !ok {
		t.Errorf("the table does not carry the task %q", id)
	}
}

// TestAskQuestion_SchemaMatchesTheModelDeclaration pins the registry
// schema against the reference's model-facing declaration: at least one
// question, both question and options required, at least two options and
// a non-empty question.
func TestAskQuestion_SchemaMatchesTheModelDeclaration(t *testing.T) {
	var schema struct {
		Properties struct {
			Questions struct {
				MinItems int `json:"minItems"`
				Items    struct {
					Required   []string `json:"required"`
					Properties struct {
						Question struct {
							MinLength int `json:"minLength"`
						} `json:"question"`
						Options struct {
							MinItems int `json:"minItems"`
						} `json:"options"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"questions"`
		} `json:"properties"`
	}
	if err := json.Unmarshal((&AskQuestion{}).Schema(), &schema); err != nil {
		t.Fatalf("ask_question schema: %v", err)
	}
	questions := schema.Properties.Questions
	if questions.MinItems != 1 {
		t.Errorf("questions.minItems = %d, want 1", questions.MinItems)
	}
	required := strings.Join(questions.Items.Required, ",")
	if required != "question,options" {
		t.Errorf("required = %q, want question,options", required)
	}
	if got := questions.Items.Properties.Options.MinItems; got != 2 {
		t.Errorf("options.minItems = %d, want 2", got)
	}
	if got := questions.Items.Properties.Question.MinLength; got != 1 {
		t.Errorf("question.minLength = %d, want 1", got)
	}
}

// TestAskQuestion_NumbersPerQuestionAnswers pins the host path: a host
// that carries the answers separately gets one numbered line per
// question.
func TestAskQuestion_NumbersPerQuestionAnswers(t *testing.T) {
	host := &fakeInteractionHost{answer: interaction.Answer{Answers: []string{"a", "b"}}}
	ctx := interaction.WithHost(context.Background(), host)
	got, err := (&AskQuestion{}).Run(ctx, json.RawMessage(`{"questions":[{"question":"One?","options":["a"]},{"question":"Two?","options":["b"]}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != "A1: a\nA2: b" {
		t.Errorf("ask_question = %q, want %q", got, "A1: a\nA2: b")
	}
}

// TestAskQuestion_KeepsPreformattedLegacyAnswer pins the legacy
// dashboard's already-numbered multi-question string.
func TestAskQuestion_KeepsPreformattedLegacyAnswer(t *testing.T) {
	host := &fakeInteractionHost{answer: interaction.Answer{Text: "A1: a\nA2: b"}}
	ctx := interaction.WithHost(context.Background(), host)
	got, err := (&AskQuestion{}).Run(ctx, json.RawMessage(`{"questions":[{"question":"One?","options":["a"]},{"question":"Two?","options":["b"]}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != "A1: a\nA2: b" {
		t.Errorf("ask_question = %q, want the legacy answer unchanged", got)
	}
}

// TestAskQuestion_SingleAnswerStartingWithA1IsNumbered pins that the
// legacy detection only applies to multi-question answers: a one-question
// write-in that happens to start with "A1: " is still an answer.
func TestAskQuestion_SingleAnswerStartingWithA1IsNumbered(t *testing.T) {
	host := &fakeInteractionHost{answer: interaction.Answer{Text: "A1: hello"}}
	ctx := interaction.WithHost(context.Background(), host)
	got, err := (&AskQuestion{}).Run(ctx, json.RawMessage(`{"questions":[{"question":"One?","options":["a"]}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != "A1: A1: hello" {
		t.Errorf("ask_question = %q, want %q", got, "A1: A1: hello")
	}
}
