package creditspanel

// MsgClose signals that the credits panel should close.
type MsgClose struct{}

// MsgOpenURL requests opening an external subscription/activity URL.
type MsgOpenURL struct {
	URL string
}
