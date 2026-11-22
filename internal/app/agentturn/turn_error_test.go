package agentturn

import (
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/apierror"
)

func TestTurnErrorMessage_Nil(t *testing.T) {
	message, ok := TurnErrorMessage(nil)
	if ok {
		t.Errorf("TurnErrorMessage(nil) reported a message: %q", message)
	}
	if message != "" {
		t.Errorf("TurnErrorMessage(nil) = %q, want empty", message)
	}
}

func TestTurnErrorMessage_StandardError(t *testing.T) {
	err := errors.New("context deadline exceeded")
	got, ok := TurnErrorMessage(err)
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	if got != "context deadline exceeded" {
		t.Errorf("TurnErrorMessage = %q, want 'context deadline exceeded'", got)
	}
}

// TestTurnErrorMessage_EmptyMessageFallsBack pins that an error whose
// text is empty still classifies into a message the row can render.
func TestTurnErrorMessage_EmptyMessageFallsBack(t *testing.T) {
	got, ok := TurnErrorMessage(emptyMessageError{})
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	if got != "the turn failed" {
		t.Errorf("TurnErrorMessage = %q, want 'the turn failed'", got)
	}
}

// TestTurnErrorMessage_HTTPErrorUsesReferenceCopy pins that the
// classifier answers with the reference's own copy for a model-API
// failure.
func TestTurnErrorMessage_HTTPErrorUsesReferenceCopy(t *testing.T) {
	api := &apierror.HTTPError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"Individual quota reached."}}`),
	}
	got, ok := TurnErrorMessage(api)
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	if !strings.Contains(got, "Individual quota reached.") {
		t.Errorf("TurnErrorMessage = %q, want the API message", got)
	}
}

func TestTurnErrorMessage_AutoContNetworkFailed(t *testing.T) {
	last := errors.New("connection reset by peer")
	err := &ErrAutoContNetworkFailed{
		Attempts: 3,
		Last:     last,
	}
	got, ok := TurnErrorMessage(err)
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	want := "Network error after 3 retries: connection reset by peer"
	if got != want {
		t.Errorf("TurnErrorMessage = %q, want %q", got, want)
	}
}

func TestTurnErrorMessage_AutoContMaxDepth(t *testing.T) {
	last := errors.New("recursion limit reached")
	err := &ErrAutoContMaxDepth{
		Last: last,
	}
	got, ok := TurnErrorMessage(err)
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	want := "The turn stopped before it finished: recursion limit reached"
	if got != want {
		t.Errorf("TurnErrorMessage = %q, want %q", got, want)
	}
}

// TestTurnErrorMessage_AutoContWithoutCause pins that a give-up error
// with no cause keeps its own message: the wrapper's sentence is all
// there is, and the row renders it as it is.
func TestTurnErrorMessage_AutoContWithoutCause(t *testing.T) {
	got, ok := TurnErrorMessage(&ErrAutoContNetworkFailed{Attempts: 2})
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	if got != "agent: network error after 2 retries (last error: <nil>)" {
		t.Errorf("TurnErrorMessage = %q, want the wrapper's own message", got)
	}
}

// emptyMessageError is an error whose text is empty.
type emptyMessageError struct{}

func (emptyMessageError) Error() string { return "" }

func TestTurnErrorMessage_AutoContWrappedHTTPError(t *testing.T) {
	httpErr := &apierror.HTTPError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"Resource has been exhausted (e.g. check quota)."}}`),
	}

	maxDepth := &ErrAutoContMaxDepth{
		Depth: 3,
		Last:  httpErr,
	}
	got, ok := TurnErrorMessage(maxDepth)
	if !ok {
		t.Fatal("expected TurnErrorMessage to report message")
	}
	if !strings.Contains(got, "Resource has been exhausted") {
		t.Errorf("TurnErrorMessage(maxDepth) = %q, want it to contain API error message", got)
	}

	netErr := &ErrAutoContNetworkFailed{
		Attempts: 5,
		Last:     httpErr,
	}
	gotNet, ok := TurnErrorMessage(netErr)
	if !ok {
		t.Fatal("expected TurnErrorMessage to report message")
	}
	if !strings.Contains(gotNet, "Resource has been exhausted") {
		t.Errorf("TurnErrorMessage(netErr) = %q, want it to contain API error message", gotNet)
	}
}
