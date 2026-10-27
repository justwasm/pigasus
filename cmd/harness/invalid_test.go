// invalid_test.go — the reference's invalid tool call path in the harness.
//
// The expectations are the reference's own frames, captured from the
// `localharness` binary driven through the SDK's protocol against a local
// fake Gemini endpoint. See `out/localharness/captures/`.
package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// A call the reference refuses writes one frame.
//
// The frame comes from the *system*: the reference reports its own
// refusal, not the model's answer. It fails the step, targets the user,
// and quotes the reason in its text.
func TestInvalidStepWritesTheReferencesFrame(t *testing.T) {
	reason := "model output error: invalid tool call error (invalid_signature) " +
		"AbsolutePath is a required parameter. Follow the function call schema exactly."
	update := invalidStep("", reason)

	if update.GetState() != pb.StepUpdate_STATE_ERROR {
		t.Errorf("state = %v, want STATE_ERROR", update.GetState())
	}
	if update.GetTarget() != pb.StepUpdate_TARGET_USER {
		t.Errorf("target = %v, want TARGET_USER", update.GetTarget())
	}
	if update.GetSource() != pb.StepUpdate_SOURCE_SYSTEM {
		t.Errorf("source = %v, want SOURCE_SYSTEM", update.GetSource())
	}
	if got := update.GetErrorMessage(); got != "The model produced an invalid tool call." {
		t.Errorf("errorMessage = %q, want the reference's own sentence", got)
	}
	want := `The model produced an invalid tool call. ("` + reason + `")`
	if got := update.GetText(); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	if got := update.GetTextDelta(); got != want {
		t.Errorf("textDelta = %q, want %q", got, want)
	}
	if got := update.GetError().GetErrorMessage(); got != want {
		t.Errorf("error.errorMessage = %q, want %q", got, want)
	}
	if got := update.GetError().GetHttpCode(); got != 0 {
		t.Errorf("error.httpCode = %d, want 0", got)
	}
}

// The frame carries no action: the call never ran.
func TestInvalidStepCarriesNoAction(t *testing.T) {
	update := invalidStep("", "model output error: invalid tool call error (invalid_args) argument TargetFile not found")

	if update.GetRunCommand() != nil || update.GetEditFile() != nil || update.GetCustomTool() != nil {
		t.Errorf("the frame carries an action: %+v", update)
	}
}

// A refusal the reference words of its own carries that sentence.
//
// An edit of a file that is not there says so in the frame's errorMessage
// and in the frame's text.
func TestInvalidStepCarriesTheToolsOwnSentence(t *testing.T) {
	reason := "model output error: invalid tool call error (invalid_args) " +
		"/ws/nope.txt does not exist in the current location."
	update := invalidStep("Agent cannot edit files that do not exist.", reason)

	if got := update.GetErrorMessage(); got != "Agent cannot edit files that do not exist." {
		t.Errorf("errorMessage = %q, want the tool's own sentence", got)
	}
	want := `Agent cannot edit files that do not exist. ("` + reason + `")`
	if got := update.GetText(); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

// The refusal counter starts at four and falls by one per refused call.
//
// The reference tells the model how many retries are left.
func TestInvalidRetriesFallByOnePerRefusal(t *testing.T) {
	c := &conversation{}

	for _, want := range []int{4, 3, 2, 1} {
		if got := c.nextInvalidRetries("the model produced an invalid tool call"); got != want {
			t.Errorf("retries = %d, want %d", got, want)
		}
	}
}

// A call that worked starts the count over.
//
// The reference counts the refusals in a row.
func TestInvalidRetriesResetAfterASuccessfulCall(t *testing.T) {
	c := &conversation{}

	c.nextInvalidRetries("the model produced an invalid tool call")
	c.nextInvalidRetries("the model produced an invalid tool call")
	c.resetInvalidRetries()

	if got := c.nextInvalidRetries("the model produced an invalid tool call"); got != 4 {
		t.Errorf("retries = %d, want 4", got)
	}
}

// A call that ran and failed starts the count over as well.
//
// The reference counts the refusals in a row: a call that was not refused
// breaks the row, whether it worked or not.
func TestInvalidRetriesResetAfterAFailedCall(t *testing.T) {
	c := &conversation{}

	c.nextInvalidRetries("the model produced an invalid tool call")
	c.resetInvalidRetries()

	if got := c.nextInvalidRetries("the model produced an invalid tool call"); got != 4 {
		t.Errorf("retries = %d, want 4", got)
	}
}

// The retries the model has left.
//
// The first refusal tells the model that four retries are left, and the
// count falls by one per refusal.
func TestInvalidRetriesLeft(t *testing.T) {
	c := &conversation{}

	if got := c.invalidRetriesLeft(); got != 4 {
		t.Errorf("retries left = %d, want 4", got)
	}
	for _, want := range []int{4, 3, 2, 1} {
		c.nextInvalidRetries("the model produced an invalid tool call")
		if got := c.invalidRetriesLeft(); got != want {
			t.Errorf("retries left = %d, want %d", got, want)
		}
	}
}

// A failed run ends the connection.
//
// The reference closes the connection when the model uses up its retries:
// the client sees a normal close, and the process stays up. The clone
// marks the session as ended, and the read loop ends with it.
func TestAFailedRunEndsTheSession(t *testing.T) {
	c := &conversation{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if c.sessionEnded() {
		t.Fatal("the session ended before the run failed")
	}
	c.failSession()
	if !c.sessionEnded() {
		t.Fatal("the conversation does not know that the session ended")
	}
}

// The reference turns the function calling off when the model has one
// retry left.
//
// The last request carries `functionCallingConfig.mode: NONE`, so the
// model answers with text instead of another call.
func TestFunctionCallingTurnsOffOnTheLastRetry(t *testing.T) {
	c := &conversation{}
	client := &backend.GeminiClient{}
	inner := &countingProvider{}
	provider := c.withFunctionCallingMode(inner, client)
	wrapped, ok := provider.(*functionCallingProvider)
	if !ok {
		t.Fatalf("provider = %T, want *functionCallingProvider", provider)
	}
	if wrapped.client != client {
		t.Fatalf("the wrapper does not carry the client")
	}

	// A stream with four retries left keeps the function calling on.
	if _, err := wrapped.Stream(context.Background(), pi.TranscriptContext{}, pi.StreamOptions{}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got := client.FunctionCallingMode(); got != "AUTO" {
		t.Errorf("mode = %q, want AUTO", got)
	}

	// Four refusals leave one retry: the function calling goes off.
	for i := 0; i < 4; i++ {
		c.nextInvalidRetries("the model produced an invalid tool call")
	}
	if _, err := wrapped.Stream(context.Background(), pi.TranscriptContext{}, pi.StreamOptions{}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got := client.FunctionCallingMode(); got != "NONE" {
		t.Errorf("mode = %q, want NONE", got)
	}
}

// A transport that is not the Gemini one keeps its own request shape.
//
// The mode is a field of the Gemini request; the other dialects have no
// such field.
func TestFunctionCallingLeavesTheOtherTransportsAlone(t *testing.T) {
	c := &conversation{}
	inner := &countingProvider{}

	if got := c.withFunctionCallingMode(inner, backend.NewMock()); got != pi.Provider(inner) {
		t.Errorf("provider = %T, want the inner one", got)
	}
}

// countingProvider is a provider that counts its streams and returns
// nothing.
type countingProvider struct {
	streams int
}

func (p *countingProvider) ID() string { return "counting" }

func (p *countingProvider) Close() error { return nil }

func (p *countingProvider) Stream(context.Context, pi.TranscriptContext, pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	p.streams++
	return nil, nil
}
