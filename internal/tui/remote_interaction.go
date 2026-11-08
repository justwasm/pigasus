// remote_interaction.go — the TUI host for a remote WAITING step.
//
// `pigasus --remote-agent` renders the same two interactions the local
// session does:
//
//   - a permission request raises the permission modal (y/a/n/d) and the
//     verdict travels back as `permission.allow`;
//   - an ask_question joins the subagent-question store, so the status
//     line shows the alert and Ctrl+K / `a` opens the picker, exactly as
//     for a local subagent question. The typed answer becomes
//     `askQuestion.responses`.
package tui

import (
	"context"
	"strings"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
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

// answerRemoteQuestions hands each question to the subagent-question store.
func (m *RootModel) answerRemoteQuestions(ctx context.Context, req agentturn.RemoteInteraction) (agentturn.RemoteInteractionAnswer, error) {
	if m.askQuestion.manager == nil {
		return agentturn.RemoteInteractionAnswer{Cancelled: true}, nil
	}
	answer := agentturn.RemoteInteractionAnswer{}
	for _, q := range req.Questions {
		options := make([]string, 0, len(q.Options))
		for _, opt := range q.Options {
			options = append(options, opt.Text)
		}
		queued := m.askQuestion.manager.Enqueue(subagentquestions.ParentKey, q.Prompt, options)
		if queued == nil {
			answer.Cancelled = true
			return answer, nil
		}
		m.requestAskQuestion(subagentquestions.ParentKey)
		m.repaint()

		replied := make(chan string, 1)
		go func() {
			text, _ := queued.Wait()
			replied <- text
		}()

		select {
		case text := <-replied:
			if text == subagentquestions.SkipAnswer {
				answer.Answers = append(answer.Answers, agentturn.RemoteQuestionAnswer{
					Prompt:  q.Prompt,
					Options: q.Options,
					Skipped: true,
				})
				continue
			}
			answer.Answers = append(answer.Answers, agentturn.AnswerFromText(q, text))
		case <-ctx.Done():
			_ = m.askQuestion.manager.Skip(subagentquestions.ParentKey, queued.ID)
			<-replied
			answer.Cancelled = true
			return answer, nil
		}
	}
	return answer, nil
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
