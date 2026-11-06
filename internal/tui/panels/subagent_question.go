package panels

import (
	"github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
)

// SubAgentQuestionHost defines host seams for subagent question picker.
type SubAgentQuestionHost interface {
	SubAgentQuestionsManager() *questions.Manager
	AppendSubAgentResponse(line string)
	InputValue() string
	ResetInput()
}

// NewSubAgentQuestionPanel builds the subagent question picker with the host's seams.
func NewSubAgentQuestionPanel(h SubAgentQuestionHost) *subagentquestionpanel.Panel {
	return subagentquestionpanel.New(subagentquestionpanel.Seams{
		Manager:    h.SubAgentQuestionsManager(),
		Respond:    h.AppendSubAgentResponse,
		InputValue: h.InputValue,
		ResetInput: h.ResetInput,
	})
}
