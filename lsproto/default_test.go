package lsproto

import "testing"

// TestDefaultAnswer covers the fallback's contract: the answer for a
// method the clone does not implement is the *default instance* of its
// response message — the answer a method with nothing to report gives.
//
// 【一手】 2026-10-27: 111 of the reference's own 153 answers are
// exactly that (out/langserver/coverage-351.json).
func TestDefaultAnswer(t *testing.T) {
	codec, err := Shared()
	if err != nil {
		t.Fatalf("Shared: %v", err)
	}
	cases := []struct {
		method string
		want   string
	}{
		// A response whose fields are all unset: proto-JSON omits
		// them, so the answer is the empty object.
		{method: "GetStatus", want: "{}"},
		{method: "GetAllWorkflows", want: "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			out, ok := codec.DefaultAnswer(tc.method)
			if !ok {
				t.Fatalf("DefaultAnswer(%q): no default", tc.method)
			}
			if string(out) != tc.want {
				t.Errorf("DefaultAnswer(%q) = %s, want %s", tc.method, out, tc.want)
			}
		})
	}
}

// TestDefaultAnswerUnknownMethod covers that a method the descriptor
// set does not know has no default: the caller keeps its own answer
// (and its own failure) for it.
func TestDefaultAnswerUnknownMethod(t *testing.T) {
	codec, err := Shared()
	if err != nil {
		t.Fatalf("Shared: %v", err)
	}
	if _, ok := codec.DefaultAnswer("NoSuchMethodAtAll"); ok {
		t.Error("DefaultAnswer: an unknown method has a default")
	}
}

// TestDefaultAnswerIsValidJSON covers the shape the transport needs:
// the default instance is proto-JSON, so it parses as an object.
func TestDefaultAnswerIsValidJSON(t *testing.T) {
	codec, err := Shared()
	if err != nil {
		t.Fatalf("Shared: %v", err)
	}
	out, ok := codec.DefaultAnswer("GetCascadeTrajectory")
	if !ok {
		t.Fatal("DefaultAnswer: no default")
	}
	if len(out) == 0 || out[0] != '{' {
		t.Errorf("DefaultAnswer = %s, want a JSON object", out)
	}
}
