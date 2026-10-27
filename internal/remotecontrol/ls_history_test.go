package remotecontrol

import (
	"encoding/json"
	"testing"
)

// TestLSFillSummaryTitle covers the rule the Web UI's header depends
// on: a summary message without a title takes the row's title.
//
// 【一手】 2026-10-26: the reference's own daemon reconciles the
// summaries table when it starts and rewrites the rows it can read in
// the reference's own shape — with an empty title when it cannot
// generate one, because the conversation's steps are the clone's shape
// and not the reference's protobuf. The blob it wrote carries no title
// either, and the Web UI then falls back to "Untitled Conversation".
func TestLSFillSummaryTitle(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		title string
		want  string
	}{
		{
			name:  "a message without a title takes the row's",
			raw:   `{"stepCount":2,"trajectoryId":"abc"}`,
			title: "named-conversation",
			want:  "named-conversation",
		},
		{
			name:  "a message with an empty title takes the row's",
			raw:   `{"summary":"","stepCount":2}`,
			title: "named-conversation",
			want:  "named-conversation",
		},
		{
			name:  "a message with a title of its own keeps it",
			raw:   `{"summary":"reference-name"}`,
			title: "clone-name",
			want:  "reference-name",
		},
		{
			name:  "no title anywhere leaves the message alone",
			raw:   `{"stepCount":2}`,
			title: "",
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := lsFillSummaryTitle(json.RawMessage(tc.raw), tc.title)
			var obj struct {
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(out, &obj); err != nil {
				t.Fatalf("unmarshal %s: %v", out, err)
			}
			if obj.Summary != tc.want {
				t.Errorf("summary = %q, want %q", obj.Summary, tc.want)
			}
		})
	}
}

// TestLSFillSummaryTitleKeepsTheOtherFields covers that the patch
// leaves the rest of the message alone: the Web UI reads the step
// count and the status from the same message.
func TestLSFillSummaryTitleKeepsTheOtherFields(t *testing.T) {
	raw := json.RawMessage(`{"stepCount":7,"status":"CASCADE_RUN_STATUS_IDLE","trajectoryId":"abc"}`)
	out := lsFillSummaryTitle(raw, "named-conversation")
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatalf("unmarshal %s: %v", out, err)
	}
	if obj["summary"] != "named-conversation" {
		t.Errorf("summary = %v, want %q", obj["summary"], "named-conversation")
	}
	if obj["status"] != "CASCADE_RUN_STATUS_IDLE" {
		t.Errorf("status = %v, want the message's own", obj["status"])
	}
	if obj["trajectoryId"] != "abc" {
		t.Errorf("trajectoryId = %v, want the message's own", obj["trajectoryId"])
	}
	if got, ok := obj["stepCount"].(float64); !ok || got != 7 {
		t.Errorf("stepCount = %v, want 7", obj["stepCount"])
	}
}
