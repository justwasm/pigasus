package subagenttools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// The reference's launch acknowledgement.
//
// 【一手】 agy 1.4.4, live capture (docs/cycles/subagent-async-framing-capture/deliverable.md):
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
// The JSON is protojson's own indented form: two spaces after the colon, a
// two-space indent, and one array element per line. A plain
// json.MarshalIndent writes one space after the colon, so the block is
// rendered here rather than marshalled.
//
// The acknowledgement is the reference's `SubagentResult` message: its
// three fields are `conversation_id`, `log_absolute_uri` and
// `workspace_uris` (【descriptor】 exa.cortex_pb, see
// docs/cycles/async-subagent-framing/deliverable.md).
const (
	ackHeader = "Created the following subagent:"
	ackFooter = "The subagent will send you a message when it has completed its task or requires guidance. " +
		"There is no need to poll for its response."
)

// Acknowledgement is the launch acknowledgement the parent reads.
type Acknowledgement struct {
	// ConversationID is the child's own conversation id.
	ConversationID string
	// LogAbsoluteURI points at the child's transcript.
	LogAbsoluteURI string
	// WorkspaceURIs are the directories the child works in.
	WorkspaceURIs []string
	// At is the moment the acknowledgement was written.
	At time.Time
}

// Text renders the acknowledgement, header included.
//
// The header is the same stamp every tool result carries ("Created At:" /
// "Completed At:"), and the two stamps are the same instant: the
// acknowledgement is written the moment the child is launched.
func (a Acknowledgement) Text() string {
	at := a.At
	if at.IsZero() {
		at = time.Now()
	}
	var b strings.Builder
	b.WriteString("Created At: " + at.Format(time.RFC3339) + "\n")
	b.WriteString("Completed At: " + at.Format(time.RFC3339) + "\n")
	b.WriteString(ackHeader + "\n")
	b.WriteString(a.json())
	b.WriteString("\n")
	b.WriteString(ackFooter)
	return b.String()
}

// json renders the SubagentResult block in protojson's indented form.
func (a Acknowledgement) json() string {
	var b strings.Builder
	b.WriteString("{\n")
	b.WriteString("  \"conversationId\":  " + quoteProtoJSON(a.ConversationID) + ",\n")
	b.WriteString("  \"logAbsoluteUri\":  " + quoteProtoJSON(a.LogAbsoluteURI) + ",\n")
	b.WriteString("  \"workspaceUris\":  [\n")
	for i, uri := range a.WorkspaceURIs {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString("    " + quoteProtoJSON(uri))
	}
	b.WriteString("\n  ]\n")
	b.WriteString("}")
	return b.String()
}

// quoteProtoJSON renders one JSON string the way protojson does.
//
// protojson escapes a string the way encoding/json does, except that it
// keeps the non-ASCII bytes as they are. The paths in the acknowledgement
// are plain ASCII, so encoding/json's own escaping is the same bytes.
func quoteProtoJSON(s string) string {
	return fmt.Sprintf("%q", s)
}

// fileURI renders an absolute path as the reference's file URI.
//
// The reference's own URIs carry three slashes for an absolute path
// ("file:///Users/gear/…"), which is what "file://" plus the path gives.
func fileURI(path string) string {
	if path == "" {
		return ""
	}
	return "file://" + path
}

// newConversationID mints a conversation id for a child.
//
// The reference's conversation ids are RFC-4122 version-4 UUIDs (the
// captured acknowledgement carries
// "73ab3617-5a99-4e26-b1f1-c88e712efe39"), and the id is what the parent
// and the child use to address each other, so it has to look like one.
func newConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A failing random source is not worth failing a delegation
		// over: fall back to a timestamp-shaped id, which is still
		// unique inside one conversation.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
