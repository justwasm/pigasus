// Package tools — tools_interaction_test.go: the interactive tools
// when a transport can reach the user.
//
// The three tools that need the user — ask_question, ask_permission,
// ask_custom_permission — take the first transport that exists: the
// turn's interaction host (internal/interaction), the subagent-question
// store, or nobody (headless).
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/interaction"
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
	reg := Catalog(t.TempDir())

	got, err := reg.Dispatch(ctx, "ask_question", json.RawMessage(`{"questions":[{"question":"Which tea?","options":["Tea","Coffee"]}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != "Tea" {
		t.Errorf("ask_question = %q, want %q", got, "Tea")
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
	reg := Catalog(t.TempDir())

	got, err := reg.Dispatch(ctx, "ask_question", json.RawMessage(`{"questions":[{"question":"Which tea?"}]}`))
	if err != nil {
		t.Fatalf("ask_question: %v", err)
	}
	if got != questionCancelledText {
		t.Errorf("ask_question = %q, want %q", got, questionCancelledText)
	}
}

// TestAskPermission_AsksTheHost pins the permission path: a refusal
// is the reference's own denial text, a grant names the action.
func TestAskPermission_AsksTheHost(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, allow := range []bool{true, false} {
		host := &fakeInteractionHost{answer: interaction.Answer{Allowed: allow}}
		ctx := interaction.WithHost(context.Background(), host)

		got, err := reg.Dispatch(ctx, "ask_permission", json.RawMessage(`{"action":"run_command","target":"date"}`))
		if err != nil {
			t.Fatalf("ask_permission: %v", err)
		}
		if allow {
			if !strings.Contains(got, "run_command") || !strings.Contains(got, "date") {
				t.Errorf("ask_permission = %q, want the allowed action and target", got)
			}
		} else if !strings.Contains(got, "was denied.") {
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
	reg := Catalog(t.TempDir())

	if _, err := reg.Dispatch(ctx, "ask_custom_permission", json.RawMessage(`{"action":"a","target":"t","reason":"because"}`)); err != nil {
		t.Fatalf("ask_custom_permission: %v", err)
	}
	if host.request.Reason != "because" {
		t.Errorf("request reason = %q, want %q", host.request.Reason, "because")
	}
	if host.request.ToolName != "ask_custom_permission" {
		t.Errorf("request tool = %q, want ask_custom_permission", host.request.ToolName)
	}
}

func TestSendMessageTool_RejectsTaskInput(t *testing.T) {
	taskID := "task-123"
	backgroundTasks.register(&bgTask{id: taskID})
	defer backgroundTasks.remove(taskID)

	s := &SendMessageTool{}
	_, err := s.Run(context.Background(), json.RawMessage(`{"Recipient":"task-123","Message":"hello"}`))
	if err == nil {
		t.Fatalf("expected error when sending message to background task ID, got nil")
	}
	want := "send_message should not be used to provide input to tasks; use the manage_task tool with action 'send_input' instead"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}
