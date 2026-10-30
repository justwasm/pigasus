// Package subagentrouter is a backward-compatible facade over internal/app/subagent/router.
package subagentrouter

import (
	"context"

	"github.com/justwasm/pigasus/internal/app/subagent/router"
)

type State = router.State
type Sender = router.Sender
type SenderFunc = router.SenderFunc
type Entry = router.Entry

var (
	ErrEmptyName             = router.ErrEmptyName
	ErrAlreadyRegistered     = router.ErrAlreadyRegistered
	ErrUnknownName           = router.ErrUnknownName
	ErrEmptyBody             = router.ErrEmptyBody
)

type Manager = router.Manager
var NewManager = router.NewManager

const AtNameMaxLen = router.AtNameMaxLen

type ErrDispatchFailed = router.ErrDispatchFailed

var (
	ParsePrompt                = router.ParsePrompt
	IsAtNameRune               = router.IsAtNameRune
	NamedSubagentEmptyMessage   = router.NamedSubagentEmptyMessage
	NamedSubagentUnknownMessage = router.NamedSubagentUnknownMessage
	Route                      = router.Route
)

func _() {
	var _ func(ctx context.Context, router *Manager, buf string) (*Entry, bool, error) = Route
}
