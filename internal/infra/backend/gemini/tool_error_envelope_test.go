// tool_error_envelope_test.go — the Gemini dialect's tool-result
// envelope.
//
// The Gemini API-key dialect is the reference's own path, so it takes
// the same envelope as the CloudCode one: a failed call travels under
// `error`, a successful one under `output`.
//
// 【一手】 agy 1.4.8, captured live — see
// internal/tools/testdata/tool_errors_1.4.8.txt.
package gemini

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// The bodies below are the reference's own, copied from the 1.4.8 capture
// (internal/tools/testdata/tool_errors_1.4.8.txt).
const (
	capturedFailure = "Created At: 2026-10-27T23:34:41+08:00\n" +
		"Completed At: 2026-10-27T23:34:41+08:00\n" +
		"Encountered error in tool execution: file does not exist"
	capturedSuccess = "Created At: 2026-10-27T23:40:16+08:00\n" +
		"Completed At: 2026-10-27T23:40:16+08:00\n" +
		"Showing lines 1-2 of 2\n1: hello 1.4.8\n2: second line"
)

func TestGemini_ToolCallEnvelope(t *testing.T) {
	failed := toolResponseFor(backend.Message{Content: capturedFailure, IsError: true})
	if _, ok := failed["error"]; !ok {
		t.Fatalf("failed call = %v, want an `error` member", failed)
	}
	if _, ok := failed["output"]; ok {
		t.Fatalf("failed call = %v, want no `output` member", failed)
	}
	ok := toolResponseFor(backend.Message{Content: capturedSuccess})
	if _, ok := ok["output"]; !ok {
		t.Fatalf("successful call = %v, want an `output` member", ok)
	}
	if _, ok := ok["error"]; ok {
		t.Fatalf("successful call = %v, want no `error` member", ok)
	}
}
