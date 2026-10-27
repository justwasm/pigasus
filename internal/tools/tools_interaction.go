package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/interaction"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/subagentquestions"
)

// ---- user interaction ------------------------------------------------------
//
// The three tools below need the user: ask_question asks one or more
// questions, ask_permission and ask_custom_permission ask for one
// action to be allowed.
//
// There are three ways to reach a user, and the tool takes the first
// one that exists:
//
//  1. The turn's interaction host (internal/interaction) — the Web UI's
//     interaction card puts one on the turn's context, and the tool
//     blocks on it until the user answers. The answer becomes the
//     tool's result.
//  2. The subagent-question store — the TUI's picker, and the print
//     runtime's own store.
//  3. Nobody — headless print mode. The reference's no-tool-call
//     reminder ends "Remember that the user cannot answer any
//     questions.", and the tool says the same thing.

// noUserText is returned by the interactive tools when nothing can be asked.
const noUserText = "The user cannot be asked in this mode, so no answer was provided."

// questionCancelledText is the tool's result when the user dismissed
// the prompt instead of answering it.
//
// 【推测】 the reference's own text for a dismissed questionnaire is not
// in the recovered evidence; the clone says what happened, so the model
// knows the question was not answered rather than answered empty.
const questionCancelledText = "The user dismissed the question without answering it."

// permissionGrantedText is the tool's result when the user allowed the
// action.
//
// 【推测】 as above: the reference's grant text is not in the recovered
// evidence. The clone names the action and the target, so the model
// knows what it may now do.
func permissionGrantedText(action, target string) string {
	if target == "" {
		return "The user allowed the action '" + action + "'."
	}
	return "The user allowed the action '" + action + "' on target '" + target + "'."
}

// AskQuestion — Source: tools.ask_question.
type AskQuestion struct {
	// SubAgentQuestions is the optional subagent question store
	// the parent TUI / print runtime wires in. nil keeps the
	// pre-picker behaviour.
	SubAgentQuestions *subagentquestions.Manager
	// SubAgentID identifies the subagent scope this tool call
	// is running in. Empty means the parent's own ask_question.
	SubAgentID string
}

func (a *AskQuestion) Name() string { return "ask_question" }
func (a *AskQuestion) Schema() json.RawMessage {
	return obj(`"questions":{"type":"array","description":"Questions to ask.","items":{"type":"object","properties":{"question":{"type":"string"},"options":{"type":"array","items":{"type":"string"}}},"required":["question"]}}`,
		"questions")
}

type questionItem struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

func (a *AskQuestion) askSubagentQuestions(questions []questionItem) string {
	var answers []string
	for _, q := range questions {
		queued := a.SubAgentQuestions.Enqueue(a.SubAgentID, q.Question, q.Options)
		if queued == nil {
			answers = append(answers, noUserText)
			continue
		}
		ans, _ := queued.Wait()
		answers = append(answers, ans)
	}
	if len(answers) == 1 {
		return answers[0]
	}
	return strings.Join(answers, "\n")
}

func (a *AskQuestion) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Questions []questionItem `json:"questions"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if len(p.Questions) == 0 {
		return nil, fmt.Errorf("ask_question: at least one question is required")
	}
	if host := interaction.HostFrom(ctx); host != nil {
		return a.askHost(ctx, host, p.Questions, string(args))
	}
	if a.SubAgentQuestions == nil {
		return noUserText, nil
	}
	return a.askSubagentQuestions(p.Questions), nil
}

// askHost asks the user through the turn's interaction host.
//
// The host is the transport that can reach the user — the Web UI's
// interaction card today. The tool blocks until the answer arrives,
// and the answer becomes the tool's result: the model reads it as the
// user's reply.
func (a *AskQuestion) askHost(ctx context.Context, host interaction.Host, questions []questionItem, argsJSON string) (any, error) {
	req := interaction.Request{Kind: interaction.KindQuestion, ToolName: a.Name(), ArgsJSON: argsJSON}
	for _, q := range questions {
		req.Questions = append(req.Questions, interaction.Question{
			Prompt:  q.Question,
			Options: q.Options,
		})
	}
	ans, err := host.Ask(ctx, req)
	if err != nil {
		return nil, err
	}
	if ans.Cancelled {
		return questionCancelledText, nil
	}
	if ans.Text == "" {
		return noUserText, nil
	}
	return ans.Text, nil
}

// AskPermission — Source: tools.ask_permission.
type AskPermission struct{}

func (a *AskPermission) Name() string { return "ask_permission" }
func (a *AskPermission) Schema() json.RawMessage {
	return obj(`"action":{"type":"string","description":"Action the agent wants to perform."},`+
		`"target":{"type":"string","description":"Resource the action applies to."}`,
		"action")
}
func (a *AskPermission) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Target string `json:"target"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Action == "" {
		return nil, fmt.Errorf("ask_permission: action is required")
	}
	if host := interaction.HostFrom(ctx); host != nil {
		return a.askHost(ctx, host, p.Action, p.Target, "", string(args))
	}
	return permissions.DeniedToolResult(p.Action, p.Target), nil
}

// askHost asks the user through the turn's interaction host.
//
// A refusal is the reference's own denial text — the same string the
// headless path returns — so the model's next turn is the same either
// way. A grant says what was allowed.
func (a *AskPermission) askHost(ctx context.Context, host interaction.Host, action, target, reason, argsJSON string) (any, error) {
	ans, err := host.Ask(ctx, interaction.Request{
		Kind:     interaction.KindPermission,
		ToolName: a.Name(),
		ArgsJSON: argsJSON,
		Action:   action,
		Target:   target,
		Reason:   reason,
	})
	if err != nil {
		return nil, err
	}
	if ans.Cancelled || !ans.Allowed {
		return permissions.DeniedToolResult(action, target), nil
	}
	return permissionGrantedText(action, target), nil
}

// AskCustomPermission — Source: tools.ask_custom_permission.
type AskCustomPermission struct{}

func (a *AskCustomPermission) Name() string { return "ask_custom_permission" }
func (a *AskCustomPermission) Schema() json.RawMessage {
	return obj(`"action":{"type":"string"},`+
		`"target":{"type":"string"},`+
		`"reason":{"type":"string","description":"Why the permission is needed."}`,
		"action")
}
func (a *AskCustomPermission) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
		Target string `json:"target"`
		Reason string `json:"reason"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Action == "" {
		return nil, fmt.Errorf("ask_custom_permission: action is required")
	}
	if host := interaction.HostFrom(ctx); host != nil {
		return a.askHost(ctx, host, p.Action, p.Target, p.Reason, string(args))
	}
	return permissions.DeniedToolResult(p.Action, p.Target), nil
}

// askHost asks the user through the turn's interaction host.
//
// Same contract as AskPermission's: a refusal is the reference's own
// denial text, a grant names the action.
func (a *AskCustomPermission) askHost(ctx context.Context, host interaction.Host, action, target, reason, argsJSON string) (any, error) {
	ans, err := host.Ask(ctx, interaction.Request{
		Kind:     interaction.KindPermission,
		ToolName: a.Name(),
		ArgsJSON: argsJSON,
		Action:   action,
		Target:   target,
		Reason:   reason,
	})
	if err != nil {
		return nil, err
	}
	if ans.Cancelled || !ans.Allowed {
		return permissions.DeniedToolResult(action, target), nil
	}
	return permissionGrantedText(action, target), nil
}

// NotifyUserTool — Source: tools.notify_user. Records a notification in the
// session inbox; there is no desktop notifier in print mode.
type NotifyUserTool struct{}

func (n *NotifyUserTool) Name() string { return "notify_user" }
func (n *NotifyUserTool) Schema() json.RawMessage {
	return obj(`"message":{"type":"string","description":"Notification text."}`, "message")
}
func (n *NotifyUserTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Message string `json:"message"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	if p.Message == "" {
		return nil, fmt.Errorf("notify_user: message is required")
	}
	inbox.add(p.Message)
	return "Notification recorded.", nil
}

// SendMessageTool — Source: tools.send_message. Delivers a message to a running
// background command.
type SendMessageTool struct{}

func (s *SendMessageTool) Name() string { return "send_message" }
func (s *SendMessageTool) Schema() json.RawMessage {
	return obj(`"id":{"type":"string","description":"Target command id."},`+
		`"message":{"type":"string","description":"Text delivered to the target."}`,
		"id", "message")
}
func (s *SendMessageTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	t, ok := backgroundTasks.get(p.ID)
	if !ok {
		return nil, fmt.Errorf("send_message: unknown task %q", p.ID)
	}
	if err := t.writeStdin(p.Message + "\n"); err != nil {
		return nil, err
	}
	return "Message delivered.", nil
}

// ManageInbox — Source: tools.manage_inbox. Lists or clears the notice inbox.
type ManageInbox struct{}

func (m *ManageInbox) Name() string { return "manage_inbox" }
func (m *ManageInbox) Schema() json.RawMessage {
	return obj(`"action":{"type":"string","description":"list or clear."}`, "action")
}
func (m *ManageInbox) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Action string `json:"action"`
	}
	if err := argsOf(args, &p); err != nil {
		return nil, err
	}
	switch p.Action {
	case "list":
		items := inbox.list()
		if len(items) == 0 {
			return "Inbox is empty.", nil
		}
		return strings.Join(items, "\n"), nil
	case "clear":
		n := inbox.clear()
		return fmt.Sprintf("Cleared %d inbox item(s).", n), nil
	}
	return nil, fmt.Errorf("manage_inbox: unknown action %q (want list or clear)", p.Action)
}

// ListPermissions — Source: tools.list_permissions. Reports the local rules.
type ListPermissions struct {
	Workspace string
	Allow     []string
	Deny      []string
}

func (l *ListPermissions) Name() string { return "list_permissions" }
func (l *ListPermissions) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (l *ListPermissions) Run(ctx context.Context, args json.RawMessage) (any, error) {
	var b strings.Builder
	if len(l.Deny) == 0 && len(l.Allow) == 0 {
		b.WriteString("No permission rules configured.\n")
	}
	for _, r := range l.Deny {
		fmt.Fprintf(&b, "deny  %s\n", r)
	}
	for _, r := range l.Allow {
		fmt.Fprintf(&b, "allow %s\n", r)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
