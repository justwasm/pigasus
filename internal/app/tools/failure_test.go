package tools

import "testing"

// The bodies below are the reference's own, copied from the 1.4.8 capture
// (internal/tools/testdata/tool_errors_1.4.8.txt).
const (
	failureExecution = "Created At: 2026-10-27T23:34:41+08:00\n" +
		"Completed At: 2026-10-27T23:34:41+08:00\n" +
		"Encountered error in tool execution: file does not exist"
	failureValidation = "Created At: 2026-10-27T23:33:40+08:00\n" +
		"Completed At: 2026-10-27T23:33:40+08:00\n" +
		"Encountered error in tool validation: unknown tool: \"view_file\" — check spelling"
	successOutput = "Created At: 2026-10-27T23:40:16+08:00\n" +
		"Completed At: 2026-10-27T23:40:16+08:00\n" +
		"Showing lines 1-2 of 2\n1: hello 1.4.8\n2: second line"
)

func TestIsFailureText(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"the reference's execution failure", failureExecution, true},
		{"the reference's validation failure", failureValidation, true},
		{"a successful result", successOutput, false},
		{"a marker without the stamps", "Encountered error in tool execution: nope", true},
		{"the marker inside the body is not a failure",
			"Created At: 2026-10-27T23:40:16+08:00\n" +
				"Completed At: 2026-10-27T23:40:16+08:00\n" +
				"1: Encountered error in tool execution: file does not exist", false},
		{"an empty result", "", false},
		{"the marker alone", FailureMarker, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFailureText(tc.text); got != tc.want {
				t.Errorf("IsFailureText(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}
