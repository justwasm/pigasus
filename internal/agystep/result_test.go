package agystep

import "testing"

// TestParseRunCommandResult covers the two shapes the reference prints.
//
// The first is the CLI's own formatter — what agy 1.4.4 sends to the model
// (the live capture in docs/cycles/command-output-truncation/captures.md)
// and what agy 1.4.7 stores (the conversation records under
// ~/.gemini/antigravity-cli/conversations).
//
// The second is the step string, which the reference produced at 1.4.2 and
// 1.4.3 and which eleven of its own transcript records from 2026-10-19/20
// still carry.
func TestParseRunCommandResult(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantCode int
		wantOut  string
		wantHas  bool
	}{
		{
			name: "current shape",
			in: "Created At: 2026-10-22T11:14:02+08:00\n" +
				"Completed At: 2026-10-22T11:14:03+08:00\n" +
				"Command exited with code 0.\n" +
				"\n" +
				"Command output:\n" +
				"a\nb\n",
			wantCode: 0,
			wantOut:  "a\nb\n",
			wantHas:  true,
		},
		{
			name: "current shape, no output",
			in: "Created At: 2026-10-22T11:14:02+08:00\n" +
				"Completed At: 2026-10-22T11:14:03+08:00\n" +
				"Command exited with code 0.\n" +
				"\n" +
				"No output from the command.",
			wantCode: 0,
			wantOut:  "",
			wantHas:  false,
		},
		{
			name: "current shape, non-zero exit",
			in: "Created At: 2026-10-22T11:14:02+08:00\n" +
				"Completed At: 2026-10-22T11:14:03+08:00\n" +
				"Command exited with code 3.\n" +
				"\n" +
				"Command output:\n" +
				"out\nerr\n",
			wantCode: 3,
			wantOut:  "out\nerr\n",
			wantHas:  true,
		},
		{
			name: "step-string shape",
			in: "Created At: 2026-10-20T02:27:51+08:00\n" +
				"Completed At: 2026-10-20T02:27:51+08:00\n" +
				"\n" +
				"The command exited with code 0.\n" +
				"Output:\n" +
				"PROBE-DONE\r\n\n",
			wantCode: 0,
			wantOut:  "PROBE-DONE\r\n\n",
			wantHas:  true,
		},
		{
			name: "step-string shape, no output",
			in: "Created At: 2026-10-20T02:27:51+08:00\n" +
				"Completed At: 2026-10-20T02:27:51+08:00\n" +
				"\n" +
				"The command exited with code 0.\n" +
				"No output\n",
			wantCode: 0,
			wantOut:  "",
			wantHas:  false,
		},
		{
			// The reference's stored payloads carry CRLF: the command's
			// output arrives from a pty.
			name: "CRLF step-string shape",
			in: "Created At: x\r\n" +
				"Completed At: y\r\n" +
				"\r\n" +
				"The command exited with code 0.\r\n" +
				"Output:\r\n" +
				"hello\r\nworld\r\n",
			wantCode: 0,
			wantOut:  "hello\r\nworld\r\n",
			wantHas:  true,
		},
		{
			name: "CRLF current shape",
			in: "Created At: x\r\n" +
				"Completed At: y\r\n" +
				"Command exited with code 2.\r\n" +
				"\r\n" +
				"Command output:\r\n" +
				"boom\r\n",
			wantCode: 2,
			wantOut:  "boom\r\n",
			wantHas:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, ok := ParseRunCommandResult(tc.in)
			if !ok {
				t.Fatalf("the result did not parse")
			}
			if res.ExitCode != tc.wantCode {
				t.Errorf("exit code %d, want %d", res.ExitCode, tc.wantCode)
			}
			if res.Output != tc.wantOut {
				t.Errorf("output %q, want %q", res.Output, tc.wantOut)
			}
			if res.HasOutput != tc.wantHas {
				t.Errorf("has output %v, want %v", res.HasOutput, tc.wantHas)
			}
		})
	}
}

// TestParseRunCommandResultRejectsOtherResults checks that a result of
// some other tool is not mistaken for a command's.
func TestParseRunCommandResultRejectsOtherResults(t *testing.T) {
	for _, in := range []string{
		"",
		"Showing lines 1-3 of 3\n1: line one\n",
		"File is empty",
		"Created file.",
	} {
		if _, ok := ParseRunCommandResult(in); ok {
			t.Errorf("%q parsed as a command result", in)
		}
	}
}

// TestParseRunCommandResultReadsTheTruncatedPreview covers the shape the
// clone produces for a long output: the truncation note comes first, then
// the exit line, then the preview.
func TestParseRunCommandResultReadsTheTruncatedPreview(t *testing.T) {
	in := "Created At: 2026-10-22T11:14:02+08:00\n" +
		"Completed At: 2026-10-22T11:14:03+08:00\n" +
		"The tool's output was truncated due to being too long. You can find the full output in a file at file:///tmp/output.txt.\n" +
		"\n" +
		"Here is a preview with the middle of the output truncated:\n" +
		"Command exited with code 0.\n" +
		"\n" +
		"Command output:\n" +
		"head\n<truncated 119072 of 128964 bytes from the middle of the output>\ntail\n"
	res, ok := ParseRunCommandResult(in)
	if !ok {
		t.Fatal("the truncated result did not parse")
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code %d, want 0", res.ExitCode)
	}
	if !res.HasOutput {
		t.Error("the truncated preview has no output")
	}
}
