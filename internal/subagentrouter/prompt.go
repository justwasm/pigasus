package subagentrouter

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// AtNameMaxLen caps the parser's name slot.
const AtNameMaxLen = 64

// ErrEmptyBody is returned when the buffer is @<name> with no message body.
var ErrEmptyBody = errors.New("at-subagent: empty message body")

// ErrDispatchFailed wraps an underlying dispatch error.
type ErrDispatchFailed struct {
	Cause error
}

func (e ErrDispatchFailed) Error() string {
	return "at-subagent: dispatch failed: " + e.Cause.Error()
}

func (e ErrDispatchFailed) Unwrap() error {
	return e.Cause
}

// ParsePrompt recognises `@<name><whitespace><message>`.
func ParsePrompt(buf string) (name, message string, ok bool) {
	if len(buf) < 2 || buf[0] != '@' {
		return "", "", false
	}
	end := -1
	for i := 1; i < len(buf); i++ {
		c := buf[i]
		if c == ' ' || c == '\t' || c == '\n' {
			end = i
			break
		}
		if !IsAtNameRune(c) {
			return "", "", false
		}
	}
	if end < 0 {
		return "", "", false
	}
	name = buf[1:end]
	if name == "" || len(name) > AtNameMaxLen {
		return "", "", false
	}
	rest := buf[end+1:]
	trimmed := strings.TrimSpace(rest)
	return name, trimmed, true
}

// IsAtNameRune reports whether c may appear in the name slot.
func IsAtNameRune(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == '.':
		return true
	}
	return false
}

// NamedSubagentEmptyMessage returns user-visible error text when no body was provided.
func NamedSubagentEmptyMessage(name string) string {
	return fmt.Sprintf("@%s has no message body. Type a message after the name.", name)
}

// NamedSubagentUnknownMessage returns user-visible error text when the name is unknown.
func NamedSubagentUnknownMessage(name string) string {
	return fmt.Sprintf("No subagent named %q is running. Start one or check the name.", name)
}

// Route parses buf and dispatches via router.
func Route(ctx context.Context, router *Manager, buf string) (*Entry, bool, error) {
	name, message, ok := ParsePrompt(buf)
	if !ok {
		return nil, false, nil
	}
	if message == "" {
		return nil, true, ErrEmptyBody
	}
	if router == nil {
		return nil, true, ErrUnknownName
	}
	entry, lookupOK := router.Lookup(name)
	if !lookupOK {
		return nil, true, ErrUnknownName
	}
	if err := entry.Sender.Send(ctx, message); err != nil {
		return nil, true, ErrDispatchFailed{Cause: err}
	}
	return entry, true, nil
}
