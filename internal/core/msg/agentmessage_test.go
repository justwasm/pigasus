package msg

import (
	"strings"
	"testing"
	"time"
)

// TestRenderIsTheCapturedEnvelope pins the envelope to the reference's own
// bytes.
//
// 【一手】 agy 1.4.4, live capture
// (docs/cycles/subagent-async-framing-capture/deliverable.md):
//
//	The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.
//
//	<SYSTEM_MESSAGE>
//	[Message] timestamp=2026-10-22T03:24:39Z sender=73ab3617-5a99-4e26-b1f1-c88e712efe39 priority=MESSAGE_PRIORITY_HIGH content=PONG
//	</SYSTEM_MESSAGE>
//
// Two details the capture pins: the blank line between the lead-in and the
// tag, and the timestamp in UTC (every other time in the reference's
// request is +08:00).
func TestRenderIsTheCapturedEnvelope(t *testing.T) {
	at := time.Date(2026, 10, 22, 11, 24, 39, 0, time.FixedZone("", 8*60*60))
	got := Render("73ab3617-5a99-4e26-b1f1-c88e712efe39", "PONG", at, PriorityHigh)
	want := strings.Join([]string{
		"The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.",
		"",
		"<SYSTEM_MESSAGE>",
		"[Message] timestamp=2026-10-22T03:24:39Z sender=73ab3617-5a99-4e26-b1f1-c88e712efe39 priority=MESSAGE_PRIORITY_HIGH content=PONG",
		"</SYSTEM_MESSAGE>",
	}, "\n")
	if got != want {
		t.Fatalf("envelope:\n got %q\nwant %q", got, want)
	}
}

// TestRenderKeepsAMultiLineContentWhole pins the reference's own habit:
// the content is not quoted, so it continues to the end of the block.
func TestRenderKeepsAMultiLineContentWhole(t *testing.T) {
	at := time.Date(2026, 10, 24, 19, 15, 22, 0, time.UTC)
	got := Render("task-56", "Command exited with code 0.\n\nCommand output:\nhello", at, "")
	if !strings.Contains(got, "content=Command exited with code 0.\n\nCommand output:\nhello\n</SYSTEM_MESSAGE>") {
		t.Fatalf("envelope %q does not carry the multi-line content", got)
	}
	// An empty priority means the high one: a delivered message is
	// information the model has to pay attention to.
	if !strings.Contains(got, "priority=MESSAGE_PRIORITY_HIGH") {
		t.Fatalf("envelope %q does not carry the default priority", got)
	}
}
