// Package agy implements quarantined compatibility behaviors, quirks, and prompt templates
// specific to the Antigravity (agy 1.4.12) reference implementation.
//
// Following the 5-tier architecture rules, this package is located in Tier 2 (internal/features/compat/agy),
// importing only Tier 1 (core) and Tier 2 (features), ensuring complete isolation from generic
// agent execution backends like ACP, Codex, and Claude.
package agy

import (
	"strings"
)

// Policy defines whether agy-specific quirks should be active.
type Policy struct {
	// Enabled indicates whether the agy compatibility layer is loaded.
	Enabled bool
	// ReferenceVersion is the targeted agy version.
	ReferenceVersion string
}

// DefaultPolicy returns the default policy for official agy parity mode.
func DefaultPolicy() Policy {
	return Policy{
		Enabled:          true,
		ReferenceVersion: "1.5.5",
	}
}

// IsAgyBackend reports whether the specified backend identifier requires Antigravity quirks.
func IsAgyBackend(backendName string) bool {
	switch strings.ToLower(strings.TrimSpace(backendName)) {
	case "", "local", "pigasus", "agy", "antigravity":
		return true
	default:
		return false
	}
}

// FormatUnknownCommand returns the byte-for-byte recovered reference string for unknown slash commands.
// Recovered from live 1.3.6 TUI capture of "/zzznotacommand".
func FormatUnknownCommand(cmd string) string {
	return "Unknown command: /" + strings.TrimPrefix(cmd, "/")
}

// UserRequestEnvelope wraps a user message into the reference's XML-style block if needed.
func UserRequestEnvelope(prompt string) string {
	return "<USER_REQUEST>\n" + prompt + "\n</USER_REQUEST>"
}
