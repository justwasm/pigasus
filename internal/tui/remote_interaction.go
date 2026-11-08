// remote_interaction.go — the TUI host for a remote WAITING step.
//
// `pigasus --remote-agent` renders the same two interactions the local
// session does:
//
//   - a permission request raises the permission modal (y/a/n/d) and the
//     verdict travels back as `permission.allow`;
//   - an ask_question opens the same interactive form the local tool uses
//     (multi-select, write-in and per-question navigation included), and
//     the answers travel back as `askQuestion.responses` with the picked
//     option ids and write-in text.
package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/ui/views/askquestion"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

// wireRemoteInteractionHandler connects the remote engine to the host.
func (m *RootModel) wireRemoteInteractionHandler() {
	if m == nil {
		return
	}
	aware, ok := m.turn.engine.(agentturn.RemoteInteractionAware)
	if !ok {
		return
	}
	aware.SetRemoteInteractionHandler(m.handleRemoteInteraction)
}

// handleRemoteInteraction renders one WAITING step and blocks for the answer.
func (m *RootModel) handleRemoteInteraction(ctx context.Context, req agentturn.RemoteInteraction) (agentturn.RemoteInteractionAnswer, error) {
	if req.Kind == agentturn.RemoteInteractionPermission {
		return m.answerRemotePermission(ctx, req)
	}
	return m.answerRemoteQuestions(ctx, req)
}

// answerRemotePermission raises the permission modal.
func (m *RootModel) answerRemotePermission(ctx context.Context, req agentturn.RemoteInteraction) (agentturn.RemoteInteractionAnswer, error) {
	command := strings.TrimSpace(strings.TrimSpace(req.Action) + " " + strings.TrimSpace(req.Target))
	prompt := DefaultPermissionPrompts.Raise(permissionprompt.Request{
		Tool:        req.ToolName,
		Kind:        permissionprompt.KindDefault,
		Command:     command,
		Description: req.Reason,
	})
	m.repaint()
	res, err := prompt.Wait(ctx)
	if err != nil {
		return agentturn.RemoteInteractionAnswer{Cancelled: true}, nil
	}
	return agentturn.RemoteInteractionAnswer{Allowed: res.IsApproved()}, nil
}

// answerRemoteQuestions opens the same interactive form the local
// ask_question tool uses, so a remote turn keeps multi-select and the
// per-question flow instead of falling back to the single-select
// subagent-question picker.
func (m *RootModel) answerRemoteQuestions(ctx context.Context, req agentturn.RemoteInteraction) (agentturn.RemoteInteractionAnswer, error) {
	if m == nil || m.turn.prog == nil {
		return agentturn.RemoteInteractionAnswer{Cancelled: true}, nil
	}
	return m.answerRemoteQuestionsVia(ctx, req, m.turn.prog.Send)
}

// answerRemoteQuestionsVia is the seam tests use: send posts a message to
// the running program.
func (m *RootModel) answerRemoteQuestionsVia(ctx context.Context, req agentturn.RemoteInteraction, send func(tea.Msg)) (agentturn.RemoteInteractionAnswer, error) {
	if len(req.Questions) == 0 {
		return agentturn.RemoteInteractionAnswer{Cancelled: true}, nil
	}

	questions := make([]askquestion.Question, len(req.Questions))
	for i, q := range req.Questions {
		options := make([]string, len(q.Options))
		for j, opt := range q.Options {
			options[j] = opt.Text
		}
		questions[i] = askquestion.Question{
			Question:      q.Prompt,
			Options:       options,
			IsMultiSelect: q.IsMultiSelect,
		}
	}

	resps, err := askThroughProgram(ctx, send, questions)
	if err != nil {
		return agentturn.RemoteInteractionAnswer{Cancelled: true}, nil
	}

	answer := agentturn.RemoteInteractionAnswer{
		Answers: make([]agentturn.RemoteQuestionAnswer, 0, len(req.Questions)),
	}
	for i, q := range req.Questions {
		var resp askquestion.Response
		if i < len(resps) {
			resp = resps[i]
		}
		answer.Answers = append(answer.Answers, remoteQuestionAnswer(q, resp))
	}
	return answer, nil
}

// remoteQuestionAnswer maps the form's response back to the remote wire
// answer, which names the picked options by their stable ids.
func remoteQuestionAnswer(q agentturn.RemoteQuestion, resp askquestion.Response) agentturn.RemoteQuestionAnswer {
	out := agentturn.RemoteQuestionAnswer{Prompt: q.Prompt, Options: q.Options}
	if resp.Skipped {
		out.Skipped = true
		return out
	}
	for _, idx := range resp.SelectedOptionIndices {
		if idx >= 0 && idx < len(q.Options) {
			out.SelectedOptionIDs = append(out.SelectedOptionIDs, q.Options[idx].ID)
		}
	}
	// A response built without indices falls back to matching the texts.
	if len(out.SelectedOptionIDs) == 0 && len(resp.SelectedOptions) > 0 {
		for _, text := range resp.SelectedOptions {
			for _, opt := range q.Options {
				if opt.Text == text {
					out.SelectedOptionIDs = append(out.SelectedOptionIDs, opt.ID)
					break
				}
			}
		}
	}
	out.WriteInResponse = resp.WriteInText
	if len(out.SelectedOptionIDs) == 0 && out.WriteInResponse == "" {
		out.Skipped = true
	}
	return out
}

// repaint asks Bubble Tea to render immediately. A WAITING prompt is raised
// from the turn's goroutine, which the program does not otherwise see.
func (m *RootModel) repaint() {
	if m == nil || m.turn.prog == nil {
		return
	}
	m.turn.prog.Send(remoteInteractionRaisedMsg{})
}

// remoteInteractionRaisedMsg is a repaint cue.
type remoteInteractionRaisedMsg struct{}
