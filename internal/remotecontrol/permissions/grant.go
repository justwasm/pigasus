// Package permissions — grant.go: the reference's own grant
// grammar, `action(target)`.
package permissions

import (
	corepermissions "github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// The reference's six live grant actions.
const (
	ActionCommand              = corepermissions.ActionCommand
	ActionReadFile             = corepermissions.ActionReadFile
	ActionWriteFile            = corepermissions.ActionWriteFile
	ActionReadURL              = corepermissions.ActionReadURL
	ActionMCP                  = corepermissions.ActionMCP
	ActionExecuteURL           = corepermissions.ActionExecuteURL
	ActionUnsandboxed          = corepermissions.ActionUnsandboxed
	UnsandboxedRejectedMessage = corepermissions.UnsandboxedRejectedMessage
	GrantFormatHint            = corepermissions.GrantFormatHint
)

var (
	GrantActions        = corepermissions.GrantActions
	ErrGrantUnsandboxed = corepermissions.ErrGrantUnsandboxed
)

type invalidGrantError = corepermissions.InvalidGrantError

func ErrInvalidGrantString(s string) error {
	return corepermissions.ErrInvalidGrantString(s)
}

func ErrUnknownGrantAction(action, s string) error {
	return corepermissions.ErrUnknownGrantAction(action, s)
}

func ValidGrantAction(action string) bool {
	return corepermissions.ValidGrantAction(action)
}

func ParseGrantString(s string) (action, target string, err error) {
	return corepermissions.ParseGrantString(s)
}

// grantParseError renders a refused grant string for a wire envelope.
func grantParseError(err error) string {
	return corepermissions.GrantParseError(err)
}

// ParseGrantKey parses one grant key into a Grant.
func ParseGrantKey(s string) (Grant, error) {
	return corepermissions.ParseGrantKey(s)
}

// grantKeyFor renders the dedup key of a grant.
func grantKeyFor(action, tool, pattern string) string {
	return corepermissions.GrantKeyFor(action, tool, pattern)
}
