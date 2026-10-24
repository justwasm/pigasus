package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

func TestTurnErrorRow_Nil(t *testing.T) {
	if got := TurnErrorRow(nil); got != "" {
		t.Errorf("TurnErrorRow(nil) = %q, want empty", got)
	}
}

func TestTurnErrorRow_StandardError(t *testing.T) {
	err := errors.New("context deadline exceeded")
	got := TurnErrorRow(err)
	if got != "Error: context deadline exceeded" {
		t.Errorf("TurnErrorRow = %q, want 'Error: context deadline exceeded'", got)
	}
}

func TestTurnErrorRow_HTTPErrorUsesReferenceCopy(t *testing.T) {
	api := &backend.HTTPError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"Individual quota reached."}}`),
	}
	row := TurnErrorRow(api)
	if !strings.HasPrefix(row, "Error: ") {
		t.Errorf("row = %q, want 'Error: ' prefix", row)
	}
	if !strings.Contains(row, "Individual quota reached.") {
		t.Errorf("row = %q, want API message", row)
	}
}

func TestTurnErrorRow_AutoContNetworkFailed(t *testing.T) {
	last := errors.New("connection reset by peer")
	err := &ErrAutoContNetworkFailed{
		Attempts: 3,
		Last:     last,
	}
	row := TurnErrorRow(err)
	want := "Error: Network error after 3 retries: connection reset by peer"
	if row != want {
		t.Errorf("TurnErrorRow = %q, want %q", row, want)
	}
}

func TestTurnErrorRow_AutoContMaxDepth(t *testing.T) {
	last := errors.New("recursion limit reached")
	err := &ErrAutoContMaxDepth{
		Last: last,
	}
	row := TurnErrorRow(err)
	want := "Error: The turn stopped before it finished: recursion limit reached"
	if row != want {
		t.Errorf("TurnErrorRow = %q, want %q", row, want)
	}
}
