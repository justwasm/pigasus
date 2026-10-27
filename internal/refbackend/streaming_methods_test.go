package refbackend

import (
	"testing"

	"github.com/justwasm/pigasus/lsproto"
)

// TestStreamingMethodsMatchDescriptor pins the reference's
// server-streaming method list to the service descriptor.
//
// The list decides how the reference's answers are read: a method on
// the list is a Connect stream (length-prefixed envelopes), a method
// off it is a unary JSON answer. A method that belongs on the list but
// is missing from it is proxied as a unary call, and the reference's
// stream answer is then read as a unary body — the UI never sees the
// frames.
//
// The descriptor closure is the schema the frontend bundle itself
// embeds, so it is the authority; the hand-written list is checked
// against it here.
//
// Evidence grade: 一手 (the descriptor is extracted from the IDE's own
// bundle, see lsproto/descriptor/README.md).
func TestStreamingMethodsMatchDescriptor(t *testing.T) {
	codec, err := lsproto.New()
	if err != nil {
		t.Fatalf("lsproto.New: %v", err)
	}
	want := codec.ServerStreamingMethods()
	if len(want) == 0 {
		t.Fatal("the descriptor knows no server-streaming method; the check would be vacuous")
	}
	have := make(map[string]bool, len(streamingMethods))
	for name := range streamingMethods {
		have[name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("streamingMethods is missing %q; the reference streams it", name)
		}
	}
	for name := range streamingMethods {
		if !codec.IsServerStreaming(name) {
			t.Errorf("streamingMethods lists %q, which the descriptor calls unary", name)
		}
	}
}

// TestUnaryMethodIsNotStream guards the other direction: a method the
// descriptor calls unary must not be proxied as a stream.
func TestUnaryMethodIsNotStream(t *testing.T) {
	for _, name := range []string{"GetAllSkills", "GetAllWorkflows", "GetUserStatus"} {
		if isStreamingMethod(name) {
			t.Errorf("isStreamingMethod(%q) = true, want false", name)
		}
	}
}
