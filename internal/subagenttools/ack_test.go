package subagenttools

import (
	"strings"
	"testing"
	"time"
)

// TestAcknowledgementTextIsTheCapturedBytes pins the launch
// acknowledgement to the reference's own bytes.
//
// 【一手】 agy 1.4.4, live capture
// (docs/cycles/subagent-async-framing-capture/deliverable.md):
//
//	Created At: 2026-10-22T11:24:33+08:00
//	Completed At: 2026-10-22T11:24:33+08:00
//	Created the following subagent:
//	{
//	  "conversationId":  "73ab3617-5a99-4e26-b1f1-c88e712efe39",
//	  "logAbsoluteUri":  "file:///Users/gear/.gemini/antigravity-cli/brain/73ab3617-5a99-4e26-b1f1-c88e712efe39/.system_generated/logs/transcript.jsonl",
//	  "workspaceUris":  [
//	    "file:///private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode"
//	  ]
//	}
//	The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.
//
// The JSON is protojson's indented form — two spaces after the colon, a
// two-space indent, one URI per line — so a plain json.MarshalIndent (one
// space after the colon) would not be the same bytes.
func TestAcknowledgementTextIsTheCapturedBytes(t *testing.T) {
	ack := Acknowledgement{
		ConversationID: "73ab3617-5a99-4e26-b1f1-c88e712efe39",
		LogAbsoluteURI: "file:///Users/gear/.gemini/antigravity-cli/brain/73ab3617-5a99-4e26-b1f1-c88e712efe39/.system_generated/logs/transcript.jsonl",
		WorkspaceURIs:  []string{"file:///private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode"},
		At:             time.Date(2026, 10, 22, 11, 24, 33, 0, time.FixedZone("", 8*60*60)),
	}
	want := strings.Join([]string{
		"Created At: 2026-10-22T11:24:33+08:00",
		"Completed At: 2026-10-22T11:24:33+08:00",
		"Created the following subagent:",
		"{",
		"  \"conversationId\":  \"73ab3617-5a99-4e26-b1f1-c88e712efe39\",",
		"  \"logAbsoluteUri\":  \"file:///Users/gear/.gemini/antigravity-cli/brain/73ab3617-5a99-4e26-b1f1-c88e712efe39/.system_generated/logs/transcript.jsonl\",",
		"  \"workspaceUris\":  [",
		"    \"file:///private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode\"",
		"  ]",
		"}",
		"The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.",
	}, "\n")
	if got := ack.Text(); got != want {
		t.Fatalf("acknowledgement:\n got %q\nwant %q", got, want)
	}
}

// TestNewConversationIDIsAVersion4UUID pins the shape of the id the
// parent and the child address each other by.
//
// The captured acknowledgement carries
// "73ab3617-5a99-4e26-b1f1-c88e712efe39" — an RFC-4122 version-4 UUID.
func TestNewConversationIDIsAVersion4UUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 16; i++ {
		id := newConversationID()
		if len(id) != 36 {
			t.Fatalf("newConversationID() = %q, want 36 characters", id)
		}
		for _, at := range []int{8, 13, 18, 23} {
			if id[at] != '-' {
				t.Fatalf("newConversationID() = %q, want a dash at %d", id, at)
			}
		}
		if id[14] != '4' {
			t.Fatalf("newConversationID() = %q, want version 4 at index 14", id)
		}
		switch id[19] {
		case '8', '9', 'a', 'b':
		default:
			t.Fatalf("newConversationID() = %q, want the RFC-4122 variant at index 19", id)
		}
		if seen[id] {
			t.Fatalf("newConversationID() returned %q twice", id)
		}
		seen[id] = true
	}
}

// TestFileURIIsTheReferencesShape pins the file URI the acknowledgement
// carries: "file://" plus the absolute path, which is three slashes for a
// path under the root.
func TestFileURIIsTheReferencesShape(t *testing.T) {
	if got := fileURI("/Users/gear/x.jsonl"); got != "file:///Users/gear/x.jsonl" {
		t.Fatalf("fileURI = %q, want %q", got, "file:///Users/gear/x.jsonl")
	}
	if got := fileURI(""); got != "" {
		t.Fatalf("fileURI(\"\") = %q, want the empty string", got)
	}
}
