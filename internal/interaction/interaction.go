// Package interaction is the seam between a tool that needs the user
// and whatever can reach the user.
//
// Deprecated: this package is a backward-compatible facade for
// internal/core/seam. New callers should import internal/core/seam directly.
package interaction

import (
	"context"

	"github.com/justwasm/pigasus/internal/core/seam"
)

// Kind is the kind of interaction a tool needs answered.
type Kind = seam.Kind

const (
	// KindQuestion is an ask_question call: the user answers one or
	// more questions, each with an optional list of options to pick
	// from and an optional write-in field.
	KindQuestion = seam.KindQuestion
	// KindPermission is an ask_permission / ask_custom_permission
	// call: the user allows or denies one action.
	KindPermission = seam.KindPermission
)

// Question is one entry of an ask_question call.
type Question = seam.Question

// Request is what a tool asks the user for.
type Request = seam.Request

// Answer is the user's reply.
type Answer = seam.Answer

// Host asks the user and waits for the answer.
type Host = seam.Host

// WithHost returns a context that carries host.
func WithHost(ctx context.Context, host Host) context.Context {
	return seam.WithHost(ctx, host)
}

// HostFrom returns the Host the context carries, or nil when the
// context carries none.
func HostFrom(ctx context.Context) Host {
	return seam.HostFrom(ctx)
}
