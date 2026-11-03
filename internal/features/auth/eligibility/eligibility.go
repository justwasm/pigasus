// Package eligibility models eligibility verification errors.
//
// The package owns the eligibility *facts* — the typed error, the
// Reason, the VerificationURL, the upstream HTTP status and the
// print-mode envelope. The inline notice the TUI paints above the
// prompt is a UI row, and a UI row does not belong in a features
// package: it lives in internal/ui/views/announce (cycle 381,
// tranche 53), which renders it from the notice's own data.
package eligibility

import (
	"errors"
	"fmt"

	"github.com/justwasm/pigasus/internal/core/apierror"
)

// HTTPStatus is the recovered HTTP status code the upstream returns for the
// eligibility case (IneligibleTier surfaced as a 403 PERMISSION_DENIED in the
// v1.3.4 binary's strings table).
const HTTPStatus = 403

// Error is the typed error the upstream returns when an account is not
// eligible to make a request (account-not-verified, plan-quota-exceeded,
// region-locked, etc.).
type Error struct {
	Reason          string
	VerificationURL string
	Cause           error
}

// Error renders the canonical one-line shape that surfaces in diagnostic logs.
func (e *Error) Error() string {
	if e == nil {
		return "eligibility: unknown reason"
	}
	if e.VerificationURL != "" {
		return fmt.Sprintf("eligibility: %s (verify at %s)", e.Reason, e.VerificationURL)
	}
	return "eligibility: " + e.Reason
}

// Unwrap exposes Cause for errors.Is / errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AsError walks err's chain looking for an *Error.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var ee *Error
	if errors.As(err, &ee) {
		return ee
	}
	return nil
}

// ClassifyError produces the AGY_ERROR JSON envelope the print path emits on
// stderr when a turn terminates on an *Error. Returns nil, false when err is
// not an eligibility error so the caller can fall through to the ordinary
// classifyAPIError path.
//
// The envelope shape is identical to the existing apiError
// (short_error / status / error_code / code_kind / retryable /
// error_id) — cycle 262's spec "prints the error to stderr in
// the same AGY_ERROR JSON shape" is the print-mode contract.
// Status is "ELIGIBILITY_DENIED" so an automation script
// branching on AGY_ERROR.status can route eligibility failures
// to a "show user the verification URL" handler instead of a
// generic "retry the API call" path. error_code stays at 0
// (no canonical HTTP status code) so the field does not lie
// about the cause — the cycle's design uses status as the
// distinguishing field.
//
// retryable stays false: a "verify your account" failure does
// not get better with a retry. The reference's quota case
// (the only other cycle-262-adjacent structured error)
// returns retryable=true; cycle 262 deliberately diverges
// here.
func ClassifyError(err error) (*apierror.APIError, bool) {
	ee := AsError(err)
	if ee == nil {
		return nil, false
	}
	msg := ee.Reason
	if msg == "" {
		msg = "your account is not eligible for this request"
	}
	return &apierror.APIError{
		Message:    msg,
		ShortError: fmt.Sprintf("ELIGIBILITY_DENIED: %s", msg),
		Status:     "ELIGIBILITY_DENIED",
		ErrorCode:  0,
		CodeKind:   "eligibility",
		Retryable:  false,
		ErrorID:    apierror.NewErrorID(),
	}, true
}
