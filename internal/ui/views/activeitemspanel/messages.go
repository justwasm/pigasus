package activeitemspanel

// MsgOpenDetail requests opening the details for an active item in /tasks.
type MsgOpenDetail struct {
	ID         string
	IsSubagent bool
}

// MsgDefocused signals that the active items block lost focus.
type MsgDefocused struct{}
