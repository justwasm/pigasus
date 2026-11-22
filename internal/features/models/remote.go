package models

import "strings"

// IsRemoteAgentSupported reports whether the model (by slug, catalogue ID, or display name)
// is supported by remote agents.
func IsRemoteAgentSupported(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "gemini-3.8-flash"), strings.Contains(m, "gemini 3.8 flash"):
		return true
	case strings.Contains(m, "gemini-3.8-pro"), strings.Contains(m, "gemini 3.8 pro"):
		return true
	case strings.Contains(m, "claude-sonnet-4-6"), strings.Contains(m, "claude sonnet 4.6"):
		return true
	case strings.Contains(m, "claude-opus-4-6"), strings.Contains(m, "claude opus 4.6"):
		return true
	case strings.Contains(m, "claude-opus-5-5"), strings.Contains(m, "claude opus 5.5"):
		return true
	case strings.Contains(m, "claude-sonnet-5-5"), strings.Contains(m, "claude sonnet 5.5"):
		return true
	default:
		return false
	}
}

// RemoteAgentUnsupportedMessage returns the standard 1.5.14 error message when an unsupported
// model is requested in a remote agent session.
func RemoteAgentUnsupportedMessage(model string) string {
	return model + " isn't available for remote conversations yet. Choose another model and try again."
}
