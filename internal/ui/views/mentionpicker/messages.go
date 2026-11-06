package mentionpicker

// MsgCompleted signals that a mention candidate was selected and completed.
type MsgCompleted struct {
	NewBuffer string
	RelPath   string
}

// MsgDismissed signals that the mention dropdown was closed.
type MsgDismissed struct{}
