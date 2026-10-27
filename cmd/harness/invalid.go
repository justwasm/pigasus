// invalid.go — the reference's invalid tool call path.
//
// The reference checks a call's arguments before it dispatches the tool
// and refuses an invalid one as an *invalid tool call*. Such a call:
//
//   - is never announced — the reference does not tell the client about a
//     call it is going to refuse;
//   - never runs — the tool is not dispatched at all;
//   - is reported in one frame, after the token count of the response
//     that made it: `SOURCE_SYSTEM`, `TARGET_USER`, `STATE_ERROR`, and
//     the sentence "The model produced an invalid tool call." with the
//     reason quoted;
//   - is reported to the model as a paragraph, the step's invalid branch,
//     with the number of retries that are left.
//
// The rules themselves are in `internal/harnesstools` — they are the
// reference's runtime schema, which is not the schema it advertises.
package main

import (
	"context"
	"encoding/json"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/harnesstools"
)

// knownToolNames returns the tool names the session advertises.
//
// The reference refuses a call to a tool that is not among them, and the
// client may add tools of its own.
func (c *conversation) knownToolNames() map[string]bool {
	out := map[string]bool{}
	for _, decl := range harnessDeclarations(c.sideToolSubset(), c.settings.FinishSchema) {
		out[decl.Name] = true
	}
	for _, tool := range c.clientTools() {
		out[tool.Name()] = true
	}
	return out
}

// invalidVerdict builds the verdict for a call the reference refuses.
//
// The model reads the reference's paragraph, the frame quotes the reason,
// and the call never runs.
func (c *conversation) invalidVerdict(call harnesstools.InvalidCall) callVerdict {
	reason := call.Reason()
	return callVerdict{
		Blocked:        true,
		Invalid:        true,
		Reason:         reason,
		InvalidMessage: call.Message,
		Result:         harnesstools.Invalid(reason, c.nextInvalidRetries(reason), time.Now()),
	}
}

// nextInvalidRetries counts the refusal the model is being given.
//
// The counter starts at four and falls by one per refused call; a call
// that succeeded resets it. When it reaches zero the reference gives up:
// the run fails with the reason, and the trajectory carries it.
func (c *conversation) nextInvalidRetries(reason string) int {
	c.invalidMu.Lock()
	defer c.invalidMu.Unlock()
	c.invalidCalls++
	retries := harnesstools.InvalidRetries - (c.invalidCalls - 1)
	if retries < 0 {
		retries = 0
	}
	if retries == 0 {
		c.invalidFailure = reason
	}
	return retries
}

// invalidFailureReason reports why the run failed, if it did.
//
// The reference gives up when the model uses up its retries: the run
// fails with the reason, the trajectory says so, and the harness stops.
func (c *conversation) invalidFailureReason() string {
	c.invalidMu.Lock()
	defer c.invalidMu.Unlock()
	return c.invalidFailure
}

// resetInvalidRetries forgets the refused calls.
//
// The counter counts the refusals in a row: a call that ran starts it over.
func (c *conversation) resetInvalidRetries() {
	c.invalidMu.Lock()
	defer c.invalidMu.Unlock()
	c.invalidCalls = 0
}

// invalidRetriesLeft reports how many retries the model has left.
//
// The first refusal tells the model that four retries are left, and the
// count falls by one per refusal.
func (c *conversation) invalidRetriesLeft() int {
	c.invalidMu.Lock()
	defer c.invalidMu.Unlock()
	if c.invalidCalls == 0 {
		return harnesstools.InvalidRetries
	}
	left := harnesstools.InvalidRetries - c.invalidCalls + 1
	if left < 0 {
		left = 0
	}
	return left
}

// functionCallingProvider turns the function calling off for the request
// that comes after the model's last retry.
//
// The reference does that: when the model has one retry left, the next
// request carries `functionCallingConfig.mode: NONE`, so the model answers
// with text instead of another call. The declarations stay on the wire —
// only the mode changes.
type functionCallingProvider struct {
	inner  pi.Provider
	client *backend.GeminiClient
	// retriesLeft reports how many retries the model has left.
	retriesLeft func() int
}

func (p *functionCallingProvider) ID() string { return p.inner.ID() }

func (p *functionCallingProvider) Close() error { return p.inner.Close() }

func (p *functionCallingProvider) Stream(ctx context.Context, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	if p.client != nil {
		mode := "AUTO"
		if p.retriesLeft != nil && p.retriesLeft() <= 1 {
			mode = "NONE"
		}
		p.client.SetFunctionCallingMode(mode)
	}
	return p.inner.Stream(ctx, transcript, opts)
}

// withFunctionCallingMode wraps the provider so the request can turn the
// function calling off.
//
// The transport is only the Gemini one: the mode is a field of the Gemini
// request, and the other dialects have no such field.
func (c *conversation) withFunctionCallingMode(inner pi.Provider, be backend.Client) pi.Provider {
	client, ok := be.(*backend.GeminiClient)
	if !ok {
		return inner
	}
	return &functionCallingProvider{inner: inner, client: client, retriesLeft: c.invalidRetriesLeft}
}

// checkCall reports why the reference refuses a call, if it does.
//
// The check runs before the client's own guards: the reference never
// announces a call it is going to refuse, and the client is asked about
// a call only when it is announced.
func (c *conversation) checkCall(toolName string, args json.RawMessage) (callVerdict, bool) {
	call, ok := harnesstools.CheckCall(toolName, args, c.knownToolNames())
	if !ok {
		return callVerdict{}, false
	}
	return c.invalidVerdict(call), true
}
