package agyupdate

// Tests for the `agy update` status-line terminator.
//
// The 1.5.17 changelog entry says:
//
//	Fixed `agy update` printing status lines with staircase indentation when
//	run in a terminal with raw output mode enabled, while keeping
//	newline-only output when piped or redirected to a file.
//
// The staircase happens because a terminal in raw output mode has no
// OPOST/ONLCR: a bare "\n" moves the cursor down but leaves it in the column
// it was in, so each status line starts where the previous one ended. The fix
// is to return the cursor to column 0 explicitly ("\r\n") for a terminal, and
// to keep the plain "\n" when the output is piped or redirected.
//
// The production seam is isTerminalWriter, which mirrors the reference's
// updater.isTerminalWriter: a *os.File whose Stat reports ModeCharDevice.
// These tests drive both branches through that seam and then through Run.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// stubTerminal swaps the isTerminalWriter seam for the duration of a test.
// statusTerminator reads the seam through this indirection so both branches
// are reachable without a real pty.
func withTerminal(t *testing.T, isTerminal bool) {
	t.Helper()
	saved := statusIsTerminal
	t.Cleanup(func() { statusIsTerminal = saved })
	statusIsTerminal = func(io.Writer) bool { return isTerminal }
}

// TestStatusTerminator_MatchesWriter pins the terminator table directly: a
// terminal gets "\r\n" (cursor returned to column 0), anything else gets the
// bare "\n" that must not leak a carriage return into a captured file.
func TestStatusTerminator_MatchesWriter(t *testing.T) {
	tests := []struct {
		name       string
		isTerminal bool
		want       string
	}{
		{"terminal in raw output mode", true, "\r\n"},
		{"piped or redirected to a file", false, "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withTerminal(t, tc.isTerminal)
			var buf bytes.Buffer
			if got := statusTerminator(&buf); got != tc.want {
				t.Errorf("statusTerminator = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIsTerminalWriter_NonFile pins that the real seam refuses the writers a
// caller captures through. None of these is a character device, so all of
// them must select the newline-only branch — this is the half of the bug the
// reference already got right, and it must not regress.
func TestIsTerminalWriter_NonFile(t *testing.T) {
	var buf bytes.Buffer
	if isTerminalWriter(&buf) {
		t.Error("bytes.Buffer reported as a terminal")
	}
	if isTerminalWriter(os.Stdout) && !isCharDevice(os.Stdout) {
		t.Error("os.Stdout reported as a terminal but is not a character device")
	}

	// A pipe is an *os.File that is not a character device: the exact shape a
	// `agy update | tee` invocation hands the updater.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if isTerminalWriter(w) {
		t.Error("pipe write end reported as a terminal")
	}
}

// isCharDevice re-derives the character-device test so the assertion above
// states the same condition the seam does, rather than assuming stdout.
func isCharDevice(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// TestPrintStatus_InteractiveReturnsCursor is the regression test for the
// staircase: on a terminal every status line must begin with the cursor back
// at column 0, which means the terminator of the previous line ended in a
// carriage return.
func TestPrintStatus_InteractiveReturnsCursor(t *testing.T) {
	withTerminal(t, true)

	var buf bytes.Buffer
	printStatus(&buf, statusGlyphWorking, "Checking for updates... (current version %s)", "1.5.17")
	printStatus(&buf, statusGlyphDone, "You are already on the latest version.")

	got := buf.String()
	want := "\u27f3 Checking for updates... (current version 1.5.17)\r\n" +
		"\u2713 You are already on the latest version.\r\n"
	if got != want {
		t.Fatalf("interactive status output:\ngot:  %q\nwant: %q", got, want)
	}

	// Every line except the last must end in "\r\n"; a line ending in a bare
	// "\n" is what leaves the cursor mid-row and draws the staircase.
	lines := strings.SplitAfter(got, "\n")
	lines = lines[:len(lines)-1] // trailing "" after the final "\n"
	for i, line := range lines {
		if !strings.HasSuffix(line, "\r\n") {
			t.Errorf("line %d does not return the cursor to column 0: %q", i, line)
		}
	}
}

// TestPrintStatus_NonTerminalNewlineOnly pins the other half: a captured
// writer must receive newline-only output, with no carriage return anywhere
// and no accumulated indentation.
func TestPrintStatus_NonTerminalNewlineOnly(t *testing.T) {
	withTerminal(t, false)

	var buf bytes.Buffer
	printStatus(&buf, statusGlyphWorking, "Checking for updates... (current version %s)", "1.5.17")
	printStatus(&buf, statusGlyphDone, "You are already on the latest version.")

	got := buf.String()
	want := "\u27f3 Checking for updates... (current version 1.5.17)\n" +
		"\u2713 You are already on the latest version.\n"
	if got != want {
		t.Fatalf("piped status output:\ngot:  %q\nwant: %q", got, want)
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("piped output carries a carriage return: %q", got)
	}
}

// TestRun_NonTerminalStatusLinesUnindented runs the whole updater through a
// non-terminal writer and asserts every status line starts at column 0 on its
// own line, with no stray carriage return and no leading whitespace.
func TestRun_NonTerminalStatusLinesUnindented(t *testing.T) {
	withTerminal(t, false)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Manifest{Version: "1.4.10"})
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr, Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	got := stdout.String()
	if strings.ContainsRune(got, '\r') {
		t.Fatalf("piped stdout carries a carriage return: %q", got)
	}

	sc := bufio.NewScanner(strings.NewReader(got))
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 status lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if strings.TrimLeft(line, " \t") != line {
			t.Errorf("status line %d has leading indentation: %q", i, line)
		}
		if !strings.HasPrefix(line, "\u27f3 ") && !strings.HasPrefix(line, "\u2713 ") {
			t.Errorf("status line %d does not start with its glyph: %q", i, line)
		}
	}
}

// TestRun_TerminalStatusLinesCarryCarriageReturn runs the whole updater
// through the interactive branch and asserts each status line carries the
// explicit cursor return that prevents the staircase.
func TestRun_TerminalStatusLinesCarryCarriageReturn(t *testing.T) {
	withTerminal(t, true)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Manifest{Version: "1.4.10"})
	}))
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr, Options{
		ManifestURL:    ts.URL + "/manifest.json",
		CurrentVersion: "1.4.10",
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	got := stdout.String()
	lines := strings.SplitAfter(got, "\n")
	lines = lines[:len(lines)-1]
	if len(lines) != 2 {
		t.Fatalf("expected 2 status lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if !strings.HasSuffix(line, "\r\n") {
			t.Errorf("status line %d does not return the cursor to column 0: %q", i, line)
		}
		// The staircase symptom is a line that starts past column 0 because
		// the previous one never came back. Every line must therefore begin
		// with its glyph, not with leftover text.
		if !strings.HasPrefix(line, "\u27f3 ") && !strings.HasPrefix(line, "\u2713 ") {
			t.Errorf("status line %d does not start at column 0: %q", i, line)
		}
	}
}

// TestRun_StderrLinesStayNewlineOnly pins that only the stdout status lines
// changed: the glog/CLI error lines on stderr keep their single "\n" whatever
// stdout is, which is what the reference does (its error path is plain
// Fprintf to stderr, not printStatus).
func TestRun_StderrLinesStayNewlineOnly(t *testing.T) {
	withTerminal(t, true)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), &stdout, &stderr, Options{
		ManifestURL:    "http://127.0.0.1:0/invalid-does-not-exist",
		CurrentVersion: "1.4.10",
	})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if strings.ContainsRune(stderr.String(), '\r') {
		t.Errorf("stderr gained a carriage return: %q", stderr.String())
	}
}
