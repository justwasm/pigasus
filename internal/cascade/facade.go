// Package cascade is a backward-compatible facade over
// internal/core/lifecycle.
package cascade

import (
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/core/lifecycle"
)

type ApprovalKind = lifecycle.ApprovalKind

const (
	ApprovalToolConfirm = lifecycle.ApprovalToolConfirm
	ApprovalUserInput   = lifecycle.ApprovalUserInput
)

type PendingApproval = lifecycle.PendingApproval
type CascadeEventType = lifecycle.CascadeEventType
type CascadeEvent = lifecycle.CascadeEvent
type CascaderState = lifecycle.CascaderState

const (
	CascaderInit          = lifecycle.CascaderInit
	CascaderReady         = lifecycle.CascaderReady
	CascaderExecute       = lifecycle.CascaderExecute
	CascaderStream        = lifecycle.CascaderStream
	CascaderSave          = lifecycle.CascaderSave
	CascaderDone          = lifecycle.CascaderDone
	CascaderAwaitApproval = lifecycle.CascaderAwaitApproval
	CascaderError         = lifecycle.CascaderError
)

type State = lifecycle.State
type Transition = lifecycle.Transition

var NewState = lifecycle.NewState

type RealLifecycle = lifecycle.RealLifecycle

func NewRealLifecycle(be backend.Backend) *RealLifecycle {
	return lifecycle.NewRealLifecycle(be)
}

func WithState(be backend.Backend, st *State) *RealLifecycle {
	return lifecycle.WithState(be, st)
}
