// subagent_question_host.go — the host's side of the subagent-question
// picker.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/subagentquestionpanel: it owns
// the scope, the open flag, the two cursors, the staged answer, the
// alert rows, the picker rows, the keys and the answer the Manager
// receives.
//
// The host keeps the Manager — the session owns it, and the tool seam
// (`tasks.AskQuestion`) enqueues into the same store — and supplies the
// three seams the panel cannot answer itself: where a row is echoed,
// what the composer holds, and how it is cleared.
//
// Unlike the slash panels this one is not rebuilt on every open: the
// staged answer outlives the picker's closing, because the answer is
// typed into the composer's prompt and submitted there.
//
// The alert rendering in the status row, the picker overlay and the
// Ctrl+K / `a` keybindings are the panel's; the headless `-p` path's
// wiring lives in print_pig.go, and the data model lives in
// internal/app/subagent/questions.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
)

// newSubAgentQuestionPanel builds the picker with the host's seams.
//
// The Manager is the host's: the session's tool seam enqueues into the
// same store, so the picker and the model see one list of pending
// questions.
func (m *runMode) newSubAgentQuestionPanel() *subagentquestionpanel.Panel {
	return subagentquestionpanel.New(subagentquestionpanel.Seams{
		Manager:    m.subAgentQuestions,
		Respond:    func(line string) { m.response = append(m.response, line) },
		InputValue: func() string { return m.input.Value() },
		ResetInput: func() { m.input.Reset() },
	})
}
