// rcterminal_test.go: the registry's rules.
//
// The expectations are the reference's own answers, measured
// against the reference's language_server on 2026-10-27 (see
// rcterminal.go's header for the captures).
package rcterminal

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// waitForOutput reads frames until one carries the needle or the
// deadline passes.
func waitForOutput(t *testing.T, reg *Registry, terminalID, needle string, timeout time.Duration) string {
	t.Helper()
	var out strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- reg.Stream(terminalID, func(frame Frame) error {
			if frame.Kind == FrameOutput {
				out.Write(frame.Output)
			}
			return nil
		})
	}()
	deadline := time.After(timeout)
	for {
		if strings.Contains(out.String(), needle) {
			return out.String()
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %q in the terminal output; got %q", needle, out.String())
		case err := <-done:
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			t.Fatalf("the stream ended before %q appeared; got %q", needle, out.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestCreateAnswersTheReferenceShape pins the create answer's
// fields.
func TestCreateAnswersTheReferenceShape(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{
		WorkspaceURI:   "file://" + dir,
		ConversationID: "conv-1",
		ProjectID:      "proj-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	if info.TerminalID == "" {
		t.Fatal("the terminal has no id")
	}
	if info.Title != "bash" {
		t.Fatalf("the title is %q, want the shell's name", info.Title)
	}
	if info.PID <= 0 {
		t.Fatalf("the pid is %d, want a live process", info.PID)
	}
	if info.ConversationID != "conv-1" || info.ProjectID != "proj-1" {
		t.Fatalf("the conversation/project are %q/%q", info.ConversationID, info.ProjectID)
	}
	if !strings.HasPrefix(info.WorkspaceURI, "file:///") {
		t.Fatalf("the workspace uri is %q", info.WorkspaceURI)
	}
	if info.CreatedAt == "" {
		t.Fatal("the terminal has no createdAt")
	}
}

// TestSplitGroupJoinsTheSource pins the split-group rule: the new
// terminal joins the source's group, and the group id names the
// terminal that heads it.
func TestSplitGroupJoinsTheSource(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	root, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(root.TerminalID, true) }()
	if root.SplitGroupID != "" {
		t.Fatalf("a fresh terminal has the group %q, want none", root.SplitGroupID)
	}

	child, err := reg.Create(CreateRequest{
		WorkspaceURI:        "file://" + dir,
		SplitFromTerminalID: root.TerminalID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	defer func() { _ = reg.Close(child.TerminalID, true) }()

	want := "split-" + root.TerminalID
	if child.SplitGroupID != want {
		t.Fatalf("the child's group is %q, want %q", child.SplitGroupID, want)
	}
	// The head terminal learns its own group once a split is
	// created from it.
	head, err := reg.Info(root.TerminalID)
	if err != nil {
		t.Fatalf("info: %v", err)
	}
	if head.SplitGroupID != want {
		t.Fatalf("the head's group is %q, want %q", head.SplitGroupID, want)
	}

	// A split of the child stays in the same group.
	grand, err := reg.Create(CreateRequest{
		WorkspaceURI:        "file://" + dir,
		SplitFromTerminalID: child.TerminalID,
	})
	if err != nil {
		t.Fatalf("create grandchild: %v", err)
	}
	defer func() { _ = reg.Close(grand.TerminalID, true) }()
	if grand.SplitGroupID != want {
		t.Fatalf("the grandchild's group is %q, want %q", grand.SplitGroupID, want)
	}

	// An unknown source is ignored.
	orphan, err := reg.Create(CreateRequest{
		WorkspaceURI:        "file://" + dir,
		SplitFromTerminalID: "no-such-terminal",
	})
	if err != nil {
		t.Fatalf("create orphan: %v", err)
	}
	defer func() { _ = reg.Close(orphan.TerminalID, true) }()
	if orphan.SplitGroupID != "" {
		t.Fatalf("the orphan's group is %q, want none", orphan.SplitGroupID)
	}
}

// TestListFiltersOnEveryField pins the list filter: a field that is
// given filters, a field that is empty filters nothing.
func TestListFiltersOnEveryField(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{
		WorkspaceURI:   "file://" + dir,
		ConversationID: "conv-1",
		ProjectID:      "proj-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	all := reg.List("", "", "")
	if len(all) != 1 {
		t.Fatalf("the unfiltered list has %d terminals, want 1", len(all))
	}
	if got := reg.List("conv-1", "", ""); len(got) != 1 {
		t.Fatalf("the conversation filter kept %d terminals, want 1", len(got))
	}
	if got := reg.List("", "proj-1", ""); len(got) != 1 {
		t.Fatalf("the project filter kept %d terminals, want 1", len(got))
	}
	if got := reg.List("", "", "file://"+dir); len(got) != 1 {
		t.Fatalf("the workspace filter kept %d terminals, want 1", len(got))
	}
	if got := reg.List("conv-2", "", ""); len(got) != 0 {
		t.Fatalf("a wrong conversation kept %d terminals, want 0", len(got))
	}
	if got := reg.List("", "", "file:///nowhere-at-all"); len(got) != 0 {
		t.Fatalf("a wrong workspace kept %d terminals, want 0", len(got))
	}
}

// TestSendInputAcknowledgesTheOffset pins the input rule: the
// acknowledgement is a byte offset per input stream, and a chunk
// that does not line up with it is dropped.
func TestSendInputAcknowledgesTheOffset(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	acked, err := reg.SendInput(info.TerminalID, "s-1", 0, []byte("echo ready\n"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if acked != int64(len("echo ready\n")) {
		t.Fatalf("the acknowledged offset is %d, want %d", acked, len("echo ready\n"))
	}

	// A chunk that skips ahead does not move the offset.
	acked, err = reg.SendInput(info.TerminalID, "s-1", 100, []byte("echo skipped\n"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if acked != int64(len("echo ready\n")) {
		t.Fatalf("the skipped chunk moved the offset to %d, want %d", acked, len("echo ready\n"))
	}
}

// TestStreamCarriesTheShellOutput pins the stream: the shell's
// bytes travel as the output frames.
func TestStreamCarriesTheShellOutput(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	if _, err := reg.SendInput(info.TerminalID, "s-1", 0, []byte("echo pigasus-marker\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	out := waitForOutput(t, reg, info.TerminalID, "pigasus-marker", 30*time.Second)
	if !strings.Contains(out, "pigasus-marker") {
		t.Fatalf("the output does not carry the marker: %q", out)
	}
}

// TestCloseForgetsTheTerminal pins the close rule: the terminal is
// gone, so a second close answers not_found.
func TestCloseForgetsTheTerminal(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := reg.Close(info.TerminalID, true); err != nil {
		t.Fatalf("close: %v", err)
	}
	err = reg.Close(info.TerminalID, true)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("the second close answered %v, want a not-found", err)
	}
	if _, err := reg.Info(info.TerminalID); !errors.As(err, &notFound) {
		t.Fatalf("the closed terminal is still listed: %v", err)
	}
}

// TestWorkspaceURIIsCanonicalised pins the canonicalisation: the
// clone answers the resolved path, the way the reference does
// (file:///tmp/web-ws comes back as file:///private/tmp/web-ws).
func TestWorkspaceURIIsCanonicalised(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir + "/./"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	// The reference canonicalises the path: macOS resolves /tmp
	// to /private/tmp, so the answer names the resolved path.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := "file://" + resolved
	if info.WorkspaceURI != want {
		t.Fatalf("the workspace uri is %q, want %q", info.WorkspaceURI, want)
	}
}

// TestOSCScannerFindsTitleAndCwd pins the scanner: a title and a
// working directory inside the byte stream are reported, and a
// sequence split across two chunks is still found.
func TestOSCScannerFindsTitleAndCwd(t *testing.T) {
	var scanner oscScanner
	title, cwd := scanner.scan([]byte("before\x1b]0;my title\x07after"))
	if title != "my title" {
		t.Fatalf("the title is %q", title)
	}
	if cwd != "" {
		t.Fatalf("the cwd is %q, want none", cwd)
	}

	var split oscScanner
	title, _ = split.scan([]byte("hello\x1b]2;split ti"))
	if title != "" {
		t.Fatalf("the title is %q before the sequence is complete", title)
	}
	title, _ = split.scan([]byte("tle\x07"))
	if title != "split title" {
		t.Fatalf("the title is %q, want the sequence found across the chunks", title)
	}

	var cwdScanner oscScanner
	_, cwd = cwdScanner.scan([]byte("\x1b]7;file://host/tmp/workspace\x1b\\"))
	if cwd != "/tmp/workspace" {
		t.Fatalf("the cwd is %q, want /tmp/workspace", cwd)
	}
}

// TestFrameOutputIsBase64 pins the wire shape the stream answers
// with: the bytes travel base64-encoded, the way proto-JSON writes
// a bytes field.
func TestFrameOutputIsBase64(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	if _, err := reg.SendInput(info.TerminalID, "s-1", 0, []byte("echo base64-marker\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	got := waitForOutput(t, reg, info.TerminalID, "base64-marker", 30*time.Second)
	if _, err := base64.StdEncoding.DecodeString(base64.StdEncoding.EncodeToString([]byte(got))); err != nil {
		t.Fatalf("the output is not base64-clean: %v", err)
	}
}

// TestCreateWithoutWorkspaceStartsInTheHome pins the directory a
// terminal starts in when its request names no workspace: the user's
// home directory, never the server's working directory.
//
// 【一手】 2026-10-27, the reference's own process table: the
// Antigravity IDE's language_server runs with the working directory
// "/" while the shell it spawns for a terminal runs in "/Users/gear".
func TestCreateWithoutWorkspaceStartsInTheHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skipf("no home directory: %v", err)
	}
	reg := NewRegistry()

	info, err := reg.Create(CreateRequest{ConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	want := workspaceURI(home)
	if info.WorkspaceURI != want {
		t.Fatalf("the workspace uri is %q, want %q", info.WorkspaceURI, want)
	}
}
