package permissions

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// GrantFormatHint is the reference's own hint for the surfaces that take a
// grant string.
//
// 【一手】 agy 1.4.11: "Format: action(target)".
const GrantFormatHint = "Format: action(target)"

// GrantActions is the accepted action set, in the order the reference's own
// format regex names them.
var GrantActions = []string{
	ActionCommand,
	ActionReadFile,
	ActionWriteFile,
	ActionReadURL,
	ActionMCP,
	ActionExecuteURL,
}

var grantStringRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)$`)

// ErrGrantUnsandboxed is the reference's own refusal of the retired
// action, byte for byte.
var ErrGrantUnsandboxed = errors.New(UnsandboxedRejectedMessage)

// InvalidGrantError is the reference's "invalid grant string" refusal.
//
// It is a type and not a bare error so the caller can tell a wrong *shape*
// from a wrong *action*: only the wrong shape earns the reference's format
// hint, because the hint is about the shape.
type InvalidGrantError struct {
	S string
}

func (e *InvalidGrantError) Error() string {
	return fmt.Sprintf("invalid grant string: %q", e.S)
}

// ErrInvalidGrantString renders the reference's own error for a string that
// is not a grant at all.
//
// 【一手】 agy 1.4.11: "invalid grant string: %q".
func ErrInvalidGrantString(s string) error {
	return &InvalidGrantError{S: s}
}

// ErrUnknownGrantAction renders the reference's own error for an action the
// reference does not know.
//
// 【一手】 agy 1.4.11: "unknown action %q in grant string: %q".
func ErrUnknownGrantAction(action, s string) error {
	return fmt.Errorf("unknown action %q in grant string: %q", action, s)
}

// ParseGrantString parses the reference's grant string — `action(target)`.
//
// The action is one of the six live actions; the target is the text between
// the parentheses, verbatim.
//
// The error is the reference's own:
//
//   - a string that is not a grant at all → "invalid grant string: %q";
//   - the retired `unsandboxed` action → "unsandboxed is no longer a valid
//     rule, use command() instead";
//   - an action the reference does not know → "unknown action %q in grant
//     string: %q".
func ParseGrantString(s string) (action, target string, err error) {
	m := grantStringRE.FindStringSubmatch(s)
	if m == nil {
		return "", "", ErrInvalidGrantString(s)
	}
	action = strings.TrimSpace(m[1])
	target = strings.TrimSpace(m[2])
	switch {
	case action == ActionUnsandboxed:
		return "", "", ErrGrantUnsandboxed
	case !ValidGrantAction(action):
		return "", "", ErrUnknownGrantAction(action, s)
	}
	return action, target, nil
}
