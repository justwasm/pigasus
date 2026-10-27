package lsserver

import (
	"errors"
	"testing"
)

// TestFallbackAnswerIsTheDefaultInstance covers the fallback's
// contract: a method the dispatcher does not implement is answered with
// the default instance of its response message, not with "Method not
// found".
//
// 【一手】 2026-10-27: the reference answers every method of the
// service — 111 of its 153 answers are exactly `{}` — and the
// workbench logs an error for every call that comes back as -32601
// (out/langserver/coverage-351.json).
func TestFallbackAnswerIsTheDefaultInstance(t *testing.T) {
	cases := []struct {
		method string
		want   string
	}{
		{method: "GetStatus", want: "{}"},
		{method: "GetAllWorkflows", want: "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			got, ok := fallbackAnswer(tc.method)
			if !ok {
				t.Fatalf("fallbackAnswer(%q): no answer", tc.method)
			}
			if got != tc.want {
				t.Errorf("fallbackAnswer(%q) = %s, want %s", tc.method, got, tc.want)
			}
		})
	}
}

// TestFallbackAnswerKeepsAnUnknownMethod covers that a method the
// descriptor set does not know has no default: the caller's own
// failure is kept.
func TestFallbackAnswerKeepsAnUnknownMethod(t *testing.T) {
	if got, ok := fallbackAnswer("NoSuchMethodAtAll"); ok {
		t.Errorf("fallbackAnswer = %q, want no answer", got)
	}
}

// TestIsMethodNotFound covers the failure the fallback replaces: the
// dispatcher's "method not found" and nothing else.
func TestIsMethodNotFound(t *testing.T) {
	if !isMethodNotFound(codedError{code: notFoundCode}) {
		t.Error("the -32601 failure was not recognised")
	}
	if isMethodNotFound(codedError{code: -32602}) {
		t.Error("a -32602 failure was taken for a missing method")
	}
	if isMethodNotFound(errors.New("boom")) {
		t.Error("a plain failure was taken for a missing method")
	}
}

// codedError is a failure that carries a JSON-RPC code.
type codedError struct {
	code int
}

func (e codedError) Error() string { return "coded failure" }

// JSONRPCCode implements the interface the transport learns the code
// through.
func (e codedError) JSONRPCCode() int { return e.code }
