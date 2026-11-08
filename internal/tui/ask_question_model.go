package tui

// ask_question_model.go — the ask_question model and subagent-question sub-model.
//
// AskQuestionModel owns both the turn-level interactive questions (which replace
// the composer prompt area 1:1 with jetski's AskQuestionModel) and the delegated
// subagent question queue & picker.
//
// Upstream: google3/third_party/jetski/cli/model/model.AskQuestionModel.

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/core/interaction"
	"github.com/justwasm/pigasus/internal/ui/views/askquestion"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
)

// AskQuestionModel owns both the turn-level interactive questions (replacing composer)
// and the delegated subagent questions (queue + picker).
type AskQuestionModel struct {
	manager *subagentquestions.Manager
	panel   *subagentquestionpanel.Panel
	model   *askquestion.Model
	replyCh chan []askquestion.Response
}

// askQuestionPendingMsg asks the host to bring up the subagent question picker.
type askQuestionPendingMsg struct {
	scope string
}

type askQuestionOpenMsg struct {
	questions []askquestion.Question
	replyCh   chan []askquestion.Response
}

type askQuestionCancelMsg struct{}

// Active reports whether the interactive ask_question prompt is active in the input area.
func (a *AskQuestionModel) Active() bool {
	return a != nil && a.model != nil && a.model.Active()
}

// View returns the rendered ask_question view for the input area.
func (a *AskQuestionModel) View() string {
	if a == nil || a.model == nil {
		return ""
	}
	return a.model.Render()
}

// Update routes messages to the interactive question model.
func (a *AskQuestionModel) Update(msg tea.Msg) tea.Cmd {
	if a == nil || a.model == nil {
		return nil
	}
	_, cmd := a.model.Update(msg)
	return cmd
}

// SetWidth updates the width of the active question model.
func (a *AskQuestionModel) SetWidth(w int) {
	if a != nil && a.model != nil {
		a.model.SetWidth(w)
	}
}

// OpenQuestions activates the interactive ask_question prompt in the input area.
//
// A second request while a question is already up would orphan the first
// caller: the host would overwrite the reply channel and the first tool
// would block until its context dies. Dismiss the open form first so the
// first caller gets the answers it already has instead of hanging.
func (a *AskQuestionModel) OpenQuestions(questions []askquestion.Question, width int, replyCh chan []askquestion.Response) {
	if a == nil {
		return
	}
	if a.replyCh != nil {
		a.Cancel()
	}
	a.replyCh = replyCh
	a.model = askquestion.New(questions, width)
	a.model.SetOnComplete(func(responses []askquestion.Response) {
		if a.replyCh != nil {
			a.replyCh <- responses
			a.replyCh = nil
		}
	})
}

// Cancel dismisses the active question prompt. Answers the user already
// gave are kept; the questions that were still open are marked skipped.
func (a *AskQuestionModel) Cancel() {
	if a == nil {
		return
	}
	if a.replyCh != nil {
		var responses []askquestion.Response
		if a.model != nil {
			responses = a.model.CancelResponses()
		}
		a.replyCh <- responses
		a.replyCh = nil
	}
	a.model = nil
}

// OpenParent opens the subagent picker when a subagent question is pending.
func (a *AskQuestionModel) OpenParent() bool {
	if a == nil || a.panel == nil {
		return false
	}
	a.panel.SetScope(subagentquestions.ParentKey)
	return a.panel.Open()
}

// turnInteractionHost bridges interaction.Host requests (like ask_question)
// from tool goroutines to the interactive TUI prompt.
type turnInteractionHost struct {
	prog func() *tea.Program
}

// errNoInteractiveHost is returned when no running program can show the
// interactive form. Callers translate it to a cancelled answer.
var errNoInteractiveHost = errors.New("ask_question: no interactive host")

// askThroughProgram opens the interactive form through send and blocks
// until the user finishes it or ctx is cancelled. It returns the
// per-question responses, which keep both the selected option texts and
// their indices so a transport can map them back to its own option ids.
func askThroughProgram(ctx context.Context, send func(tea.Msg), questions []askquestion.Question) ([]askquestion.Response, error) {
	if len(questions) == 0 {
		return nil, nil
	}
	if send == nil {
		return nil, errNoInteractiveHost
	}
	replyCh := make(chan []askquestion.Response, 1)
	send(askQuestionOpenMsg{
		questions: questions,
		replyCh:   replyCh,
	})

	select {
	case <-ctx.Done():
		send(askQuestionCancelMsg{})
		return nil, ctx.Err()
	case resps := <-replyCh:
		return resps, nil
	}
}

func (h *turnInteractionHost) Ask(ctx context.Context, req interaction.Request) (interaction.Answer, error) {
	if req.Kind != interaction.KindQuestion {
		return interaction.Answer{}, nil
	}
	if len(req.Questions) == 0 {
		return interaction.Answer{Cancelled: true}, nil
	}

	prog := h.prog()
	if prog == nil {
		return interaction.Answer{Cancelled: true}, nil
	}

	qs := make([]askquestion.Question, len(req.Questions))
	for i, q := range req.Questions {
		qs[i] = askquestion.Question{
			Question:      q.Prompt,
			Options:       q.Options,
			IsMultiSelect: q.IsMultiSelect,
		}
	}

	resps, err := askThroughProgram(ctx, prog.Send, qs)
	if err != nil {
		return interaction.Answer{Cancelled: true}, err
	}
	var answers []string
	allSkipped := true
	for _, r := range resps {
		if !r.Skipped {
			allSkipped = false
		}
		answers = append(answers, r.FormattedAnswer())
	}
	return interaction.Answer{
		Answers:   answers,
		Cancelled: allSkipped,
	}, nil
}

func (m *RootModel) turnInteractionHost() interaction.Host {
	return &turnInteractionHost{
		prog: func() *tea.Program {
			if m == nil || m.turn.prog == nil {
				return nil
			}
			return m.turn.prog
		},
	}
}
