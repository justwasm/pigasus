// schedule_parity_test.go — cycle 302: the background-task launch text,
// compared byte for byte against the reference's own capture.
//
// internal/resultparity replays the captured result transcript against the
// clone's executors, but it *skips* the schedule section: the captured result
// carries a runtime task id, a brain-directory path and a timestamp, so a
// literal comparison would fail on values that cannot be reproduced.
//
// The three run-dependent values are the only difference, so they can be
// normalised away and the rest compared byte for byte. This test does that —
// it turns the skipped section into a real assertion on the reference's
// wording, and it is the fence for the task subsystem's launch result.
//
// The capture (internal/tools/testdata/result_texts_1.4.7.txt, 【一手】) reads:
//
//	Created At: 2026-10-25T03:43:15+08:00
//	Tool is running as a background task with task id: 0a9d1d33-…-ca5a1b3e636c/task-4
//	Task logs are available at: file:///Users/gear/.gemini/antigravity-cli/brain/0a9d1d33-…-ca5a1b3e636c/.system_generated/tasks/task-4.log
//	YOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS: A) either proceed to other relevant work (if any) or, B) simply update the user with a short message (that you have launched the command and will wait for it to finish) and end the turn.
//	 DO NOTHING ELSE.
//
// Note the leading space on the last line: it is in the capture, and it is
// part of the reference's bytes.
package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/resultparity"
)

// normaliseTaskText replaces the three run-dependent values of a background
// task's launch result with placeholders:
//
//   - the "Created At" timestamp,
//   - the conversation id in the task id ("<conv>/task-<n>"),
//   - the home directory in the task log path.
//
// Everything else - the wording, the line order, the blank lines, the
// leading space on " DO NOTHING ELSE." - is compared literally.
func normaliseTaskText(s string) string {
	s = regexp.MustCompile(`Created At: \S+`).ReplaceAllString(s, "Created At: <TS>")

	// The log path carries two run-dependent values - the antigravity home
	// and the conversation id - so the whole line is canonicalised. It is
	// done before the task id, because the path also ends in "task-<n>.log"
	// and must not be caught by that rule.
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if !strings.HasPrefix(ln, "Task logs are available at: file://") {
			continue
		}
		rest := strings.TrimPrefix(ln, "Task logs are available at: file://")
		idx := strings.Index(rest, "/brain/")
		if idx < 0 {
			continue
		}
		tail := rest[idx+len("/brain/"):]
		tail = regexp.MustCompile(`[^/\s]+`).ReplaceAllString(tail, "<CONV>")
		tail = regexp.MustCompile(`task-\d+`).ReplaceAllString(tail, "task-N")
		lines[i] = "Task logs are available at: file://<HOME>/brain/" + tail
	}

	// "<conv>/task-<n>" - the conversation id is a uuid in the capture and
	// anything else in the test.
	s = regexp.MustCompile(`[^/\s]+/task-\d+`).ReplaceAllString(strings.Join(lines, "\n"), "<CONV>/task-N")
	return s
}

// capturedSection returns the body of the named section of the 1.4.7
// capture, or fails the test.
func capturedSection(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", "result_texts_1.4.7.txt")
	sections, err := resultparity.Load(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	for _, s := range sections {
		if s.Name == name {
			return s.Body
		}
	}
	t.Fatalf("%s has no %q section", path, name)
	return ""
}

// TestScheduleLaunchResult_MatchesReferenceCapture compares the clone's
// background-task launch result with the reference's captured one, byte for
// byte, after normalising the three values that cannot be reproduced.
func TestScheduleLaunchResult_MatchesReferenceCapture(t *testing.T) {
	want := normaliseTaskText(capturedSection(t, "schedule"))

	home := t.TempDir()
	tool := &ScheduleTool{ConversationID: "conv-schedule-parity", Home: home}
	got, err := tool.Run(context.Background(), []byte(`{"Prompt":"ping","DurationSeconds":3600}`))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	text, ok := got.(string)
	if !ok {
		t.Fatalf("schedule returned %T, want string", got)
	}

	if normaliseTaskText(text) != want {
		t.Errorf("background-task launch text differs from the reference capture\n--- want ---\n%s\n--- got ---\n%s", want, normaliseTaskText(text))
	}
}

// TestScheduleLaunchResult_WritesTaskLog checks the other half of the
// reference's shape: the task log file the launch result points at must
// exist, at the path the result names, under the antigravity home.
func TestScheduleLaunchResult_WritesTaskLog(t *testing.T) {
	home := t.TempDir()
	tool := &ScheduleTool{ConversationID: "conv-schedule-log", Home: home}
	got, err := tool.Run(context.Background(), []byte(`{"Prompt":"ping","DurationSeconds":3600}`))
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	text, ok := got.(string)
	if !ok {
		t.Fatalf("schedule returned %T, want string", got)
	}

	const marker = "Task logs are available at: file://"
	var path string
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, marker) {
			path = strings.TrimPrefix(ln, marker)
			break
		}
	}
	if path == "" {
		t.Fatalf("launch result has no %q line:\n%s", marker, text)
	}
	if !strings.HasPrefix(path, home) {
		t.Errorf("task log path %q is not under the antigravity home %q", path, home)
	}
	wantPath := filepath.Join(home, "brain", "conv-schedule-log", ".system_generated", "tasks", "task-1.log")
	if path != wantPath {
		t.Errorf("task log path = %q, want %q", path, wantPath)
	}
	b, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read task log: %v", err)
	}
	if !strings.Contains(string(b), "prompt: ping") {
		t.Errorf("task log does not carry the prompt:\n%s", b)
	}
}
