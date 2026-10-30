package eligibility

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/render"
)

func TestError_Formatting(t *testing.T) {
	var nilErr *Error
	if got := nilErr.Error(); got != "eligibility: unknown reason" {
		t.Errorf("nil error = %q, want 'eligibility: unknown reason'", got)
	}

	errNoURL := &Error{Reason: "plan exhausted"}
	if got := errNoURL.Error(); got != "eligibility: plan exhausted" {
		t.Errorf("no URL error = %q, want 'eligibility: plan exhausted'", got)
	}

	errWithURL := &Error{Reason: "verify phone", VerificationURL: "https://example.com/verify"}
	want := "eligibility: verify phone (verify at https://example.com/verify)"
	if got := errWithURL.Error(); got != want {
		t.Errorf("with URL error = %q, want %q", got, want)
	}
}

func TestError_UnwrapAndAs(t *testing.T) {
	cause := errors.New("underlying http 403")
	ee := &Error{Reason: "tier ineligible", Cause: cause}
	wrapped := fmt.Errorf("outer: %w", ee)

	found := AsError(wrapped)
	if found == nil || found.Reason != "tier ineligible" {
		t.Fatalf("AsError failed: got %v", found)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatalf("errors.Is failed to find cause through Unwrap")
	}
}

func TestClassifyError(t *testing.T) {
	apiErr, ok := ClassifyError(nil)
	if ok || apiErr != nil {
		t.Errorf("ClassifyError(nil) = (%v, %v), want (nil, false)", apiErr, ok)
	}

	ee := &Error{Reason: "plan expired"}
	apiErr, ok = ClassifyError(ee)
	if !ok || apiErr == nil {
		t.Fatalf("ClassifyError(ee) = (%v, %v), want (non-nil, true)", apiErr, ok)
	}
	if apiErr.Status != "ELIGIBILITY_DENIED" {
		t.Errorf("status = %q, want ELIGIBILITY_DENIED", apiErr.Status)
	}
	if apiErr.Retryable {
		t.Errorf("expected Retryable = false")
	}
}

func TestRenderNoticeRow(t *testing.T) {
	if got := RenderNoticeRow("", "", render.HyperlinkCaps{}); got != "" {
		t.Errorf("empty reason should yield empty string, got %q", got)
	}

	row := RenderNoticeRow("phone verification required", "https://example.com/verify", render.HyperlinkCaps{OSC8: true})
	if !strings.Contains(row, "phone verification required") {
		t.Errorf("row missing reason: %q", row)
	}
	if !strings.Contains(row, "Verify here") {
		t.Errorf("row missing hyperlink label: %q", row)
	}
	if !strings.HasSuffix(row, "\n") {
		t.Errorf("row should end with newline: %q", row)
	}
}
