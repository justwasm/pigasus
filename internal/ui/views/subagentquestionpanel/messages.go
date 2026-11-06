package subagentquestionpanel

// MsgDismissed signals that the subagent question picker was closed without staging.
type MsgDismissed struct{}

// MsgQuestionStaged signals that a question was selected/staged for answering.
type MsgQuestionStaged struct {
	QuestionID string
	Scope      string
	Prompt     string
}
