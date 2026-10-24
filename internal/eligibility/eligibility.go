// Package eligibility models eligibility verification errors and inline notice formatting.
package eligibility

import (
	"errors"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/apierror"
	"github.com/justwasm/pigasus/internal/palette"
	"github.com/justwasm/pigasus/internal/render"
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
// stderr when a turn terminates on an *Error.
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

// RenderNoticeRow renders the single styled line placed between the top separator
// and the prompt input:
//
//	ⓘ <Reason> <render.Hyperlink("Verify here", URL)>
func RenderNoticeRow(reason, verificationURL string, caps render.HyperlinkCaps) string {
	if reason == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(palette.DimGrayOpen)
	b.WriteString("  ⓘ ")
	b.WriteString(palette.WarningBodyOpen)
	b.WriteString(reason)
	if verificationURL != "" {
		b.WriteString(" ")
		b.WriteString(render.Hyperlink(verificationURL, "Verify here", 0, caps))
	}
	b.WriteString("\x1b[0m\n")
	return b.String()
}
