package main

import (
	"strings"
	"testing"
)

// TestHelpExitsWithTwo checks the reference's exit code for the help.
//
// The reference exits 2 where Go's `flag` package exits 0, and the
// IDE's spawn recipe reads the exit code.
//
// 【一手】 observed against the reference binary.
func TestHelpExitsWithTwo(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runWith([]string{"language_server", "--help"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.HasPrefix(stderr.String(), "Usage of language_server:\n") {
		t.Errorf("the help does not start with the usage:\n%s", stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("the help wrote to stdout: %q", stdout.String())
	}
}

// TestUnknownFlagExitsWithTwo checks the reference's complaint about a
// flag that does not exist and its exit code.
func TestUnknownFlagExitsWithTwo(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runWith([]string{"language_server", "--bogus1", "--bogus2"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	want := "flags provided but not defined: -bogus1 -bogus2\n"
	if !strings.HasPrefix(stderr.String(), want) {
		t.Errorf("complaint = %q, want %q", stderr.String(), want)
	}
	if !strings.HasSuffix(stderr.String(), "error during flag parsing, exiting cleanly with status 2...\n") {
		t.Errorf("the complaint is not closed by the reference's line:\n%s", stderr.String())
	}
}

// TestStampGoesToStdout checks the `-stamp` answer and its exit code.
//
// The IDE reads the stamp from stdout and parses `Built at CL: (\d+)`
// out of it.
func TestStampGoesToStdout(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runWith([]string{"language_server", "--stamp"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Built at CL: ") {
		t.Errorf("the stamp does not carry the changelist:\n%s", stdout.String())
	}
	if stderr.String() != "" {
		t.Errorf("the stamp wrote to stderr: %q", stderr.String())
	}
}

// TestHiddenFlagIsAccepted checks that the flag the IDE passes and the
// reference does not list is accepted.
func TestHiddenFlagIsAccepted(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runWith([]string{"language_server", "--enable_sidecars", "--stamp"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Built at CL: ") {
		t.Errorf("the stamp did not reach stdout:\n%s", stdout.String())
	}
}
