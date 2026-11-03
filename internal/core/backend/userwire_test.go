package backend

import (
	"testing"
	"time"
)

// The want strings below are verbatim user contents from agy 1.4.4 request
// bodies captured through the cycle-274 wrapper (bodies 1792538006259632000
// and 1792535837583188000 in /tmp/mock_bodies). Byte equality is the point:
// the /context user row is computed from this length.
func TestWrapUserContent_MatchesCapture(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 21, 7, 13, 26, 0, loc)

	got := WrapUserContent("read file", "Gemini 3.8 Flash (Medium)", true, now)
	want := "<USER_REQUEST>\nread file\n</USER_REQUEST>\n" +
		"<ADDITIONAL_METADATA>\n" +
		"The current local time is: 2026-10-21T07:13:26+08:00.\n" +
		"</ADDITIONAL_METADATA>\n" +
		"<USER_SETTINGS_CHANGE>\n" +
		"The user changed setting `Model Selection` from None to Gemini 3.8 Flash (Medium). " +
		"No need to comment on this change if the user doesn't ask about it. " +
		"If reporting what model you are, please use a human readable name instead of the exact string.\n" +
		"</USER_SETTINGS_CHANGE>"
	if got != want {
		t.Errorf("WrapUserContent mismatch:\n got %q\nwant %q", got, want)
	}
	if len(got) != 432 {
		t.Errorf("wrapped length = %d, want 432 (the captured body's length)", len(got))
	}

	now = time.Date(2026, 10, 21, 6, 37, 17, 0, loc)
	got = WrapUserContent("a", "", false, now)
	want = "<USER_REQUEST>\na\n</USER_REQUEST>\n" +
		"<ADDITIONAL_METADATA>\n" +
		"The current local time is: 2026-10-21T06:37:17+08:00.\n" +
		"</ADDITIONAL_METADATA>"
	if got != want {
		t.Errorf("settings-less wrap mismatch:\n got %q\nwant %q", got, want)
	}
	if len(got) != 131 {
		t.Errorf("wrapped length = %d, want 131", len(got))
	}
}

// TestContextTokenFormulas pins the three reference counts recovered from the
// 1.4.3 decompilation (chatTokensByCategory) and re-measured live against the
// 1.4.4 panel.
func TestContextTokenFormulas(t *testing.T) {
	userCases := []struct {
		wire string
		want int
	}{
		{"", 0},
		{string(make([]byte, 131)), 49},
		{string(make([]byte, 232)), 83},
		{string(make([]byte, 433)), 150},
		{string(make([]byte, 437)), 151},
	}
	for _, tc := range userCases {
		if got := UserContentTokenCount(tc.wire); got != tc.want {
			t.Errorf("UserContentTokenCount(len=%d) = %d, want %d", len(tc.wire), got, tc.want)
		}
	}

	toolCases := []struct {
		n    int
		want int
	}{
		{0, 0},
		{109, 42},
		{158, 59},
		{211, 76},
	}
	for _, tc := range toolCases {
		if got := ToolResultTokenCount(string(make([]byte, tc.n))); got != tc.want {
			t.Errorf("ToolResultTokenCount(len=%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}
