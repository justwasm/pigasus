package eligibility

import (
	"errors"
	"testing"
)

func TestFacadeEligibility(t *testing.T) {
	if HTTPStatus != 403 {
		t.Fatalf("expected HTTPStatus = 403, got %d", HTTPStatus)
	}
	err := &Error{Reason: "test"}
	if AsError(err) == nil {
		t.Fatal("expected non-nil AsError")
	}
	if AsError(errors.New("other")) != nil {
		t.Fatal("expected nil AsError for generic error")
	}
}
