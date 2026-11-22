package historypanel

// Entry is one prompt entry displayed in the history panel.
type Entry struct {
	Text      string
	Workspace string
	Timestamp int64
}
