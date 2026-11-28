// sse_error_test.go — the status mapping for a mid-stream error frame.
//
// The frame's status decides whether streampipe.Pipeline.Run retries the
// attempt (see isMidStreamRetryable): a 5xx or 429 is retried, anything else
// ends the turn. Getting the mapping wrong therefore silently converts a
// permanent client error into an endless retry — or, before this file, a
// client error into `gateway: upstream 500: Unknown error`, which is a lie
// about what the provider said.

package gateway

import (
	"net/http"
	"testing"
)

func TestClassifySSEErrorStatus(t *testing.T) {
	cases := []struct {
		name    string
		errType string
		errCode string
		errMsg  string
		want    int
	}{
		{"openai invalid_request_error", "invalid_request_error", "", "bad", http.StatusBadRequest},
		{"numeric string code 400", "", "400", "", http.StatusBadRequest},
		{"bare numeric status wins over default", "", "422", "", http.StatusUnprocessableEntity},
		{"not_found_error", "not_found_error", "", "", http.StatusNotFound},
		{"context length exceeded", "invalid_request_error", "", "maximum context length is 8192 tokens", http.StatusBadRequest},
		{"rate limit", "rate_limit_exceeded", "", "", http.StatusTooManyRequests},
		{"resource exhausted", "", "429", "", http.StatusTooManyRequests},
		{"overloaded", "overloaded_error", "", "", 529},
		{"service unavailable", "", "503", "", http.StatusServiceUnavailable},
		{"server error", "server_error", "", "", http.StatusInternalServerError},
		{"numeric 500", "", "500", "", http.StatusInternalServerError},
		{"unknown shape still server-side", "weird_error", "", "Unknown error", http.StatusInternalServerError},
		{"auth", "authentication_error", "", "", http.StatusUnauthorized},
		{"permission", "permission_error", "", "", http.StatusForbidden},
		{"prepaid", "invalid_request_error", "", "your prepaid balance is empty", http.StatusPaymentRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySSEErrorStatus(tc.errType, tc.errCode, tc.errMsg); got != tc.want {
				t.Fatalf("classifySSEErrorStatus(%q, %q, %q) = %d, want %d",
					tc.errType, tc.errCode, tc.errMsg, got, tc.want)
			}
		})
	}
}

// TestSSEErrorFrameToHTTPErrorMessage pins the string the transcript shows. The
// user reported `gateway: upstream 500: Unknown error`; the same frame must
// still render that way for a genuine 500, because the message comes from the
// provider and http_error.go only rewrites the cases it knows.
func TestSSEErrorFrameToHTTPErrorMessage(t *testing.T) {
	he := sseErrorToHTTPError("server_error", "500", "Unknown error", nil, "https://gw.example/v1/chat/completions")
	if he.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", he.StatusCode)
	}
	if got, want := he.Error(), "gateway: upstream 500: Unknown error"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
