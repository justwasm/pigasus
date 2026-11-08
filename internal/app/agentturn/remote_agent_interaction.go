// remote_agent_interaction.go — the client half of the remote user seam.
//
// When the remote agent needs the user, the trajectory publishes a step in
// CORTEX_STEP_STATUS_WAITING (9):
//
//   - ask_question: type 138, the questions in `askQuestion` and
//     `requestedInteraction.askQuestion`;
//   - permission: a generic step with `requestedInteraction.permission`.
//
// The client renders that step, then answers with
// `HandleCascadeUserInteraction`, naming the trajectory and the step index
// and carrying `askQuestion.responses` or `permission.allow` — the exact
// proto-JSON the reference sends.
package agentturn

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// Remote interaction kinds.
const (
	RemoteInteractionQuestion   = "askQuestion"
	RemoteInteractionPermission = "permission"
)

// RemoteOption is one choice of a remote question.
type RemoteOption struct {
	ID   string
	Text string
}

// RemoteQuestion is one entry of a remote ask_question.
type RemoteQuestion struct {
	Prompt  string
	Options []RemoteOption
}

// RemoteInteraction is a WAITING step, decoded.
type RemoteInteraction struct {
	// CascadeID is the conversation the step belongs to.
	CascadeID string
	// TrajectoryID and StepIndex name the step in the answer.
	TrajectoryID string
	StepIndex    int
	// Kind is RemoteInteractionQuestion or RemoteInteractionPermission.
	Kind string
	// ToolName and ArgsJSON are the tool call that asked.
	ToolName string
	ArgsJSON string
	// Questions is set when Kind is RemoteInteractionQuestion.
	Questions []RemoteQuestion
	// Action, Target and Reason are set when Kind is
	// RemoteInteractionPermission.
	Action string
	Target string
	Reason string
}

// RemoteQuestionAnswer is the answer to one question.
type RemoteQuestionAnswer struct {
	Prompt            string
	Options           []RemoteOption
	SelectedOptionIDs []string
	WriteInResponse   string
	Skipped           bool
}

// RemoteInteractionAnswer is the user's reply to a WAITING step.
type RemoteInteractionAnswer struct {
	// Answers is the per-question answer for an ask_question.
	Answers []RemoteQuestionAnswer
	// Allowed is the verdict on a permission step.
	Allowed bool
	// Cancelled answers a dismissed question.
	Cancelled bool
}

// RemoteInteractionHandler renders a WAITING step and blocks for the answer.
type RemoteInteractionHandler func(ctx context.Context, req RemoteInteraction) (RemoteInteractionAnswer, error)

// RemoteInteractionAware is implemented by engines that can surface a
// remote interaction to the host.
type RemoteInteractionAware interface {
	SetRemoteInteractionHandler(RemoteInteractionHandler)
}

// SetRemoteInteractionHandler installs the host's interaction handler.
func (e *RemoteAgentEngine) SetRemoteInteractionHandler(h RemoteInteractionHandler) {
	if e == nil {
		return
	}
	e.interactionMu.Lock()
	e.interaction = h
	e.interactionMu.Unlock()
}

// answerInteraction asks the host and posts the answer back to the proxy.
func (e *RemoteAgentEngine) answerInteraction(ctx context.Context, req RemoteInteraction, out chan<- agentlink.Frame) {
	answer := RemoteInteractionAnswer{Cancelled: true}

	e.interactionMu.Lock()
	handler := e.interaction
	e.interactionMu.Unlock()

	if handler != nil {
		got, err := handler(ctx, req)
		if err == nil {
			answer = got
		} else {
			answer.Cancelled = true
		}
	}

	payload := interactionPayload(req, answer)
	// The answer is a unary RPC; do not let a dead proxy park the turn.
	postCtx, cancel := remoteagentclient.WithUnaryTimeout(context.Background())
	defer cancel()
	if err := e.client.HandleCascadeUserInteraction(postCtx, req.CascadeID, payload); err != nil {
		select {
		case out <- agentlink.Frame{Kind: agentlink.KindNotice, Notice: &agentlink.Notice{
			Kind: agentlink.NoticeError,
			Text: "remote interaction: " + err.Error(),
		}}:
		default:
		}
	}
}

// interactionPayload builds the CascadeUserInteraction body the reference
// sends: the step is named by trajectory id and step index, and the answer
// is the oneof member at the top level.
func interactionPayload(req RemoteInteraction, answer RemoteInteractionAnswer) map[string]any {
	interaction := map[string]any{
		"trajectoryId": req.TrajectoryID,
		"stepIndex":    req.StepIndex,
	}
	switch req.Kind {
	case RemoteInteractionPermission:
		interaction["permission"] = map[string]any{"allow": answer.Allowed}
	default:
		if answer.Cancelled {
			interaction["askQuestion"] = map[string]any{"cancelled": true}
			break
		}
		responses := make([]any, 0, len(answer.Answers))
		for _, a := range answer.Answers {
			entry := map[string]any{
				"question": a.Prompt,
			}
			if len(a.Options) > 0 {
				options := make([]any, 0, len(a.Options))
				for _, o := range a.Options {
					options = append(options, map[string]any{"id": o.ID, "text": o.Text})
				}
				entry["options"] = options
			}
			if len(a.SelectedOptionIDs) > 0 {
				entry["selectedOptionIds"] = a.SelectedOptionIDs
			}
			if a.WriteInResponse != "" {
				entry["writeInResponse"] = a.WriteInResponse
			}
			if a.Skipped {
				entry["skipped"] = true
			}
			responses = append(responses, entry)
		}
		interaction["askQuestion"] = map[string]any{"responses": responses}
	}
	return interaction
}

// waitingInteraction decodes a WAITING step. fallbackIndex is the step's
// position in the frame, used when the step carries no
// metadata.sourceTrajectoryStepInfo.stepIndex.
func waitingInteraction(step map[string]any, cascade string, fallbackIndex int) (RemoteInteraction, bool) {
	if stepStatus(step["status"]) != 9 {
		return RemoteInteraction{}, false
	}
	requested, _ := step["requestedInteraction"].(map[string]any)
	if requested == nil {
		return RemoteInteraction{}, false
	}

	req := RemoteInteraction{CascadeID: cascade, StepIndex: fallbackIndex}

	meta, _ := step["metadata"].(map[string]any)
	if info, _ := meta["sourceTrajectoryStepInfo"].(map[string]any); info != nil {
		if id := stringField(info, "trajectoryId"); id != "" {
			req.TrajectoryID = id
		}
		if _, ok := info["stepIndex"]; ok {
			req.StepIndex = intField(info, "stepIndex")
		}
	}
	if call, _ := meta["toolCall"].(map[string]any); call != nil {
		req.ToolName = stringField(call, "name")
		req.ArgsJSON = stringField(call, "argumentsJson")
	}

	if permission, ok := requested["permission"].(map[string]any); ok {
		req.Kind = RemoteInteractionPermission
		if resource, _ := permission["resource"].(map[string]any); resource != nil {
			req.Action = stringField(resource, "action")
			req.Target = stringField(resource, "target")
		}
		req.Reason = stringField(permission, "reason")
		if req.Reason == "" {
			req.Reason = stringField(permission, "actionDescription")
		}
		if req.ToolName == "" {
			req.ToolName = "ask_permission"
		}
		return req, true
	}

	ask, _ := requested["askQuestion"].(map[string]any)
	if ask == nil {
		if top, ok := step["askQuestion"].(map[string]any); ok {
			ask = top
		}
	}
	if ask == nil {
		return RemoteInteraction{}, false
	}
	req.Kind = RemoteInteractionQuestion
	req.Questions = decodeQuestions(ask["questions"])
	if req.ToolName == "" {
		req.ToolName = "ask_question"
	}
	if req.ArgsJSON == "" {
		if encoded, err := json.Marshal(map[string]any{"questions": ask["questions"]}); err == nil {
			req.ArgsJSON = string(encoded)
		}
	}
	return req, true
}

// decodeQuestions accepts the proto-JSON question list. Options may be
// {id,text} objects or bare strings (older shapes).
func decodeQuestions(raw any) []RemoteQuestion {
	list, _ := raw.([]any)
	out := make([]RemoteQuestion, 0, len(list))
	for _, item := range list {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		q := RemoteQuestion{Prompt: stringField(entry, "question")}
		if options, ok := entry["options"].([]any); ok {
			for i, optRaw := range options {
				switch opt := optRaw.(type) {
				case map[string]any:
					id := stringField(opt, "id")
					if id == "" {
						id = itoa(i + 1)
					}
					q.Options = append(q.Options, RemoteOption{ID: id, Text: stringField(opt, "text")})
				case string:
					q.Options = append(q.Options, RemoteOption{ID: itoa(i + 1), Text: opt})
				}
			}
		}
		out = append(out, q)
	}
	return out
}

// itoa avoids importing strconv just for option ids.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// AnswerFromText builds an answer for a question whose host returned free
// text: a text that equals an option selects it, anything else is a write-in.
func AnswerFromText(q RemoteQuestion, text string) RemoteQuestionAnswer {
	text = strings.TrimSpace(text)
	out := RemoteQuestionAnswer{Prompt: q.Prompt, Options: q.Options}
	if text == "" {
		out.Skipped = true
		return out
	}
	for _, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Text), text) || opt.ID == text {
			out.SelectedOptionIDs = []string{opt.ID}
			return out
		}
	}
	out.WriteInResponse = text
	return out
}

// compile-time check.
var _ RemoteInteractionAware = (*RemoteAgentEngine)(nil)
