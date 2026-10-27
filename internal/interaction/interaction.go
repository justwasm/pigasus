// Package interaction is the seam between a tool that needs the user
// and whatever can reach the user.
//
// The interactive tools — ask_question, ask_permission,
// ask_custom_permission — cannot answer themselves. The reference's
// agent blocks the tool call until the user replies, and the reply
// becomes the tool's result: the model reads it as the user's answer.
//
// The clone has three ways to reach a user:
//
//   - the TUI's subagent-question picker (internal/subagentquestions),
//   - the Web UI's interaction card (internal/remotecontrol),
//   - nothing at all — headless print mode, where the reference's own
//     no-tool-call reminder ends "Remember that the user cannot answer
//     any questions."
//
// The first two are transports; this package is the contract they
// share. A tool asks through the Host its context carries, and the
// transport that put the Host there answers.
//
// The package is deliberately dependency-free: internal/tools imports
// it and internal/remotecontrol imports it, and the two cannot import
// each other (remotecontrol → piagentbridge → tools).
package interaction

import "context"

// Kind is the kind of interaction a tool needs answered.
type Kind string

const (
	// KindQuestion is an ask_question call: the user answers one or
	// more questions, each with an optional list of options to pick
	// from and an optional write-in field.
	KindQuestion Kind = "askQuestion"
	// KindPermission is an ask_permission / ask_custom_permission
	// call: the user allows or denies one action.
	KindPermission Kind = "permission"
)

// Question is one entry of an ask_question call.
type Question struct {
	// Prompt is the question's text.
	Prompt string
	// Options are the choices the user may pick. An empty list means
	// the question is open-ended and the user writes the answer.
	Options []string
}

// Request is what a tool asks the user for.
type Request struct {
	// Kind is the interaction's kind.
	Kind Kind
	// ToolName is the tool that asked — ask_question, ask_permission,
	// ask_custom_permission. The Web UI's permission card reads it to
	// decide how to render the prompt.
	ToolName string
	// ArgsJSON is the tool call's raw arguments. The Web UI shows them
	// on the step the interaction published.
	ArgsJSON string
	// Questions is the ask_question list (Kind == KindQuestion).
	Questions []Question
	// Action is the action a permission request covers.
	Action string
	// Target is the resource the action applies to.
	Target string
	// Reason is why the permission is needed (ask_custom_permission).
	Reason string
}

// Answer is the user's reply.
type Answer struct {
	// Text is the answer text: the chosen options and the write-in
	// response, joined the way the reference's own card renders them
	// ("Tea, Coffee" / "Something else (write-in)").
	Text string
	// Allowed is the permission verdict.
	Allowed bool
	// Cancelled is true when the user dismissed the prompt instead of
	// answering it.
	Cancelled bool
}

// Host asks the user and waits for the answer.
//
// Ask blocks until the user answered, the interaction expired, or ctx
// is done. A dismissed prompt is not an error: the answer carries
// Cancelled.
type Host interface {
	Ask(ctx context.Context, req Request) (Answer, error)
}

// hostKey is the context key the Host rides on.
type hostKey struct{}

// WithHost returns a context that carries host.
//
// A nil host returns ctx unchanged, so a caller can hand the result to
// a code path that checks for the host itself.
func WithHost(ctx context.Context, host Host) context.Context {
	if ctx == nil || host == nil {
		return ctx
	}
	return context.WithValue(ctx, hostKey{}, host)
}

// HostFrom returns the Host the context carries, or nil when the
// context carries none — the headless case, where the tool answers for
// itself.
func HostFrom(ctx context.Context) Host {
	if ctx == nil {
		return nil
	}
	host, _ := ctx.Value(hostKey{}).(Host)
	return host
}
