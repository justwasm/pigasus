// Package permissions — grant.go: the reference's own grant
// grammar, `action(target)`.
//
// The reference's permission rules are *grant strings* — an action
// and the target it covers, in parentheses:
//
//	{"allow": ["command(git status)", "read_url(github.com)"]}
//
// The official documentation says it outright ("Each rule is an
// action(target) string, such as command(git status) or
// read_url(github.com), with no space before the opening
// parenthesis"), and the binary's own format regex is
//
//	^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$
//
// 【一手】 agy 1.4.11's own string table carries the format regex above
// and the panels' format hint "Format: action(target)"; the surviving
// pruned index of that table is out/local-macos-1.4.11/strings-index.txt
// (the full dump is retired).
//
// The clone's own vocabulary was "tool:pattern" ("run_command:go test*"),
// which the reference does not use anywhere: the reference's own parser
// (`shared.ParseGrantString`) splits the action from the target and then
// validates the *action* against a closed set.
//
// 【一手】 out/localharness/dec/100dc2c20_ParseGrantString.c: the action is
// validated by length and bytes — "mcp" (3), "custom" (6), "command" (7),
// "read_url" (8), "read_file" (9), "write_file" (10), "execute_url" /
// "unsandboxed" (11) and "escalate_admin" (14) — and a string that does not
// match the shape at all is refused with "invalid grant string: %q", an
// action outside the set with "unknown action %q in grant string: %q".
//
// The six live actions are the ones the CLI's own format regex names, and
// the ones the official documentation lists. Two of the actions the cortex
// parser knows are *not* part of this surface:
//
//   - "unsandboxed" — retired. The reference still names it in its format
//     regex and then refuses every rule that uses it: "unsandboxed is no
//     longer a valid rule, use command() instead" (【一手】 agy 1.4.11). The
//     sentence is the reference's own and is reproduced byte for byte.
//
//   - "custom" and "escalate_admin" — they belong to the plugin-grant and
//     the admin-escalation surfaces, not to the permission-settings one.
//     A "custom" grant has its own shape ("custom grant %q must be of the
//     form custom({"pluginID":...})(<grant>)"), so it does not round-trip
//     through the plain `action(target)` pair. 存疑: the reference's
//     permission-settings surface may accept them after all — the CLI's
//     format regex is narrower than the cortex parser's action set, and
//     which of the two the settings surface uses was not traced.
//
// The legacy "tool:pattern" form is still *read*: a permissions.json that
// was written before this grammar was corrected must not lose its rules,
// and the four dashboard methods (AddRemoteAgentPermissionGrants and
// friends) speak it.
//
// Stdlib-only. No third-party deps.
package permissions

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The reference's six live grant actions.
//
// 【一手】 agy 1.4.11's format regex —
// `^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$`
// — and the official documentation, which names the same six.
const (
	// ActionCommand runs a shell command.
	ActionCommand = "command"
	// ActionReadFile reads a file.
	ActionReadFile = "read_file"
	// ActionWriteFile writes a file.
	ActionWriteFile = "write_file"
	// ActionReadURL fetches a URL.
	ActionReadURL = "read_url"
	// ActionMCP calls an MCP tool.
	ActionMCP = "mcp"
	// ActionExecuteURL actuates on a web page.
	ActionExecuteURL = "execute_url"
)

// ActionUnsandboxed is the retired action.
//
// The reference still names it in its format regex and refuses every rule
// that uses it.
//
// 【一手】 agy 1.4.11: "unsandboxed is no longer a valid rule, use
// command() instead".
const ActionUnsandboxed = "unsandboxed"

// UnsandboxedRejectedMessage is the reference's own rejection sentence for
// an `unsandboxed` rule. Recovered verbatim.
const UnsandboxedRejectedMessage = "unsandboxed is no longer a valid rule, use command() instead"

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

// grantStringRE is the reference's grant shape: an action, an optional run
// of spaces, an opening parenthesis, the target and the closing
// parenthesis.
//
// The reference's own regex names the actions outright; the cortex parser
// takes any action-shaped token and validates the action separately — which
// is why an unknown action has its own error ("unknown action %q in grant
// string: %q") and not the "invalid grant string" one.
//
// 【一手】 out/localharness/dec/100dc2c20_ParseGrantString.c.
var grantStringRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*\((.*)\)$`)

// ErrGrantUnsandboxed is the reference's own refusal of the retired
// action, byte for byte.
var ErrGrantUnsandboxed = errors.New(UnsandboxedRejectedMessage)

// ErrInvalidGrantString renders the reference's own error for a string that
// is not a grant at all.
//
// 【一手】 agy 1.4.11: "invalid grant string: %q".
func ErrInvalidGrantString(s string) error {
	return &invalidGrantError{s: s}
}

// invalidGrantError is the reference's "invalid grant string" refusal.
//
// It is a type and not a bare error so the caller can tell a wrong *shape*
// from a wrong *action*: only the wrong shape earns the reference's format
// hint, because the hint is about the shape.
type invalidGrantError struct {
	s string
}

func (e *invalidGrantError) Error() string {
	return fmt.Sprintf("invalid grant string: %q", e.s)
}

// grantParseError renders a refused grant string for a wire envelope.
//
// The reference's own sentence comes first, byte for byte. A wrong *shape*
// — a string that is not a grant at all — is followed by the reference's own
// format hint, because the hint is what tells the caller the shape it
// wanted. A wrong *action* is not followed by anything: the reference's
// sentence already names the action it did not know.
func grantParseError(err error) string {
	var invalid *invalidGrantError
	if errors.As(err, &invalid) {
		return err.Error() + " (" + GrantFormatHint + ")"
	}
	return err.Error()
}

// ErrUnknownGrantAction renders the reference's own error for an action the
// reference does not know.
//
// 【一手】 agy 1.4.11: "unknown action %q in grant string: %q".
func ErrUnknownGrantAction(action, s string) error {
	return fmt.Errorf("unknown action %q in grant string: %q", action, s)
}

// ValidGrantAction reports whether action is one of the six live actions.
func ValidGrantAction(action string) bool {
	for _, a := range GrantActions {
		if a == action {
			return true
		}
	}
	return false
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

// ParseGrantKey parses one grant key into the clone's grant.
//
// Two forms are accepted:
//
//   - the reference's `action(target)` — the grant form;
//   - the clone's own legacy `tool:pattern` — kept for the permissions.json
//     files that carry it and for the four dashboard methods.
//
// A grant-shaped string the reference *refuses* never falls back to the
// legacy form: "unsandboxed(git)" is a rule the user wrote, not a tool name.
// A string that is not grant-shaped at all falls back, and a string that is
// neither form is the reference's "invalid grant string" error.
func ParseGrantKey(s string) (Grant, error) {
	action, target, err := ParseGrantString(s)
	if err == nil {
		return Grant{Action: action, Pattern: target}, nil
	}
	var invalid *invalidGrantError
	if !errors.As(err, &invalid) {
		// A grant-shaped string the reference refuses.
		return Grant{}, err
	}
	tool, pattern, ok := ParseToolPattern(s)
	if !ok {
		return Grant{}, ErrInvalidGrantString(strings.TrimSpace(s))
	}
	return Grant{ToolName: tool, Pattern: pattern}, nil
}

// grantKeyFor renders the dedup key of a grant.
//
// Two forms, one per vocabulary: the reference's `action(target)` and the
// clone's legacy `tool:pattern`.
func grantKeyFor(action, tool, pattern string) string {
	if action != "" {
		return action + "(" + pattern + ")"
	}
	return tool + ":" + pattern
}
