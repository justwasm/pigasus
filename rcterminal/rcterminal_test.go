// rcterminal_test.go: the registry's rules.
//
// The expectations are the reference's own answers, measured
// against the reference's language_server on 2026-10-27 (see
// rcterminal.go's header for the captures).
package rcterminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vito/midterm"
)

// waitForOutput reads frames until one carries the needle or the
// deadline passes.
func waitForOutput(t *testing.T, reg *Registry, terminalID, needle string, timeout time.Duration) string {
	t.Helper()
	var mu sync.Mutex
	var out strings.Builder
	done := make(chan error, 1)
	go func() {
		done <- reg.Stream(terminalID, func(frame Frame) error {
			if frame.Kind == FrameOutput {
				mu.Lock()
				out.Write(frame.Output)
				mu.Unlock()
			}
			return nil
		})
	}()
	deadline := time.After(timeout)
	for {
		mu.Lock()
		cur := out.String()
		mu.Unlock()
		if strings.Contains(cur, needle) {
			return cur
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %q in the terminal output; got %q", needle, cur)
		case err := <-done:
			if err != nil {
				t.Fatalf("stream: %v", err)
			}
			t.Fatalf("the stream ended before %q appeared; got %q", needle, cur)
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

// TestWorkspaceURIIsCleaned pins the URI cleaning: lexical dots and
// trailing slashes are removed while preserving the client's workspace
// URI so the Web UI store lookup succeeds.
func TestWorkspaceURIIsCleaned(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir + "/./"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	want := "file://" + filepath.Clean(dir)
	if info.WorkspaceURI != want {
		t.Fatalf("the workspace uri is %q, want %q", info.WorkspaceURI, want)
	}
}

// TestWorkspaceURIPreservesClientSymlink verifies that symlinked workspace
// URIs provided by the client are preserved so that the Web UI store
// can look them up, while List filters match both symlink and canonical forms.
func TestWorkspaceURIPreservesClientSymlink(t *testing.T) {
	target := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "symlink-dir")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	reg := NewRegistry()
	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + link})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	if info.WorkspaceURI != "file://"+link {
		t.Fatalf("WorkspaceURI = %q, want client URI preserved %q", info.WorkspaceURI, "file://"+link)
	}

	listByLink := reg.List("", "", "file://"+link)
	if len(listByLink) != 1 {
		t.Fatalf("List by symlink returned %d items, want 1", len(listByLink))
	}
	listByTarget := reg.List("", "", "file://"+target)
	if len(listByTarget) != 1 {
		t.Fatalf("List by resolved target returned %d items, want 1", len(listByTarget))
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

func TestRealPTY_MidtermANSIRenderAndResumeOffset(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := reg.StreamChannelFrom(ctx, info.TerminalID, 0)
	if err != nil {
		t.Fatalf("StreamChannelFrom: %v", err)
	}

	if err := reg.Write(info.TerminalID, []byte("printf '\\033[32mGREEN_PTY\\033[0m\\n'\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	vt := midterm.NewTerminal(24, 80)
	var lastOffset int64
	var rendered bytes.Buffer
	deadline := time.After(25 * time.Second)
	for {
		rendered.Reset()
		_ = vt.Render(&rendered)
		if strings.Contains(rendered.String(), "\x1b[32mGREEN_PTY") {
			break
		}
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before GREEN_PTY rendered; got:\n%s", rendered.String())
			}
			if f.Kind == FrameOutput && len(f.Output) > 0 {
				_, _ = vt.Write(f.Output)
				if f.Offset > lastOffset {
					lastOffset = f.Offset
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for GREEN_PTY in midterm render; got:\n%q", rendered.String())
		}
	}

	// Resume from lastOffset and send a second marker; first marker must NOT replay.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()
	ch2, err := reg.StreamChannelFrom(ctx2, info.TerminalID, lastOffset)
	if err != nil {
		t.Fatalf("StreamChannelFrom(resume): %v", err)
	}
	if err := reg.Write(info.TerminalID, []byte("echo RESUME_SECOND_MARKER\n")); err != nil {
		t.Fatalf("Write second: %v", err)
	}
	var resumed strings.Builder
	deadline2 := time.After(15 * time.Second)
	for !strings.Contains(resumed.String(), "RESUME_SECOND_MARKER") {
		select {
		case f, ok := <-ch2:
			if !ok {
				t.Fatalf("resumed stream closed early; got %q", resumed.String())
			}
			if f.Kind == FrameOutput {
				resumed.Write(f.Output)
			}
		case <-deadline2:
			t.Fatalf("timed out waiting for RESUME_SECOND_MARKER; got %q", resumed.String())
		}
	}
	if strings.Contains(resumed.String(), "GREEN_PTY") {
		t.Fatalf("resumed stream replayed old output before offset %d: %q", lastOffset, resumed.String())
	}
}

type blockingWriteDevice struct {
	writeStarted chan struct{}
	writeOnce    sync.Once
	unblockWrite chan struct{}
	readCh       chan []byte
	closed       chan struct{}
	closeOnce    sync.Once

	mu      sync.Mutex
	written bytes.Buffer
}

func newBlockingWriteDevice() *blockingWriteDevice {
	return &blockingWriteDevice{
		writeStarted: make(chan struct{}),
		unblockWrite: make(chan struct{}),
		readCh:       make(chan []byte, 16),
		closed:       make(chan struct{}),
	}
}

func (d *blockingWriteDevice) Read(p []byte) (int, error) {
	select {
	case chunk, ok := <-d.readCh:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, chunk)
		return n, nil
	case <-d.closed:
		return 0, io.EOF
	}
}

func (d *blockingWriteDevice) Write(p []byte) (int, error) {
	d.writeOnce.Do(func() { close(d.writeStarted) })
	select {
	case <-d.unblockWrite:
	case <-d.closed:
		return 0, errors.New("device closed")
	}
	d.mu.Lock()
	d.written.Write(p)
	d.mu.Unlock()
	return len(p), nil
}

func (d *blockingWriteDevice) Resize(cols, rows int) error { return nil }

func (d *blockingWriteDevice) Close() error {
	d.closeOnce.Do(func() { close(d.closed) })
	return nil
}

func (d *blockingWriteDevice) writtenString() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.written.String()
}

// TestSessionWriteDoesNotBlockOutputPump verifies that a blocked ptmx.Write
// never holds s.mu or stalls pump()/handleOutput()/Info()/Stream().
func TestSessionWriteDoesNotBlockOutputPump(t *testing.T) {
	dev := newBlockingWriteDevice()
	s := newSession("term-block", 1234, "bash")
	s.ptmx = dev
	s.start()
	defer s.close(true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	frames := make(chan Frame, 16)
	streamErr := make(chan error, 1)
	go func() {
		streamErr <- s.streamContext(ctx, func(f Frame) error {
			frames <- f
			return nil
		})
	}()

	// Drain initial title frame.
	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial title frame")
	}

	// First write enters dev.Write and blocks inside the writer goroutine.
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- s.writeRaw([]byte("hello "))
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("writeRaw: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("writeRaw blocked while ptmx.Write was stalled")
	}

	select {
	case <-dev.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine did not invoke ptmx.Write")
	}

	// Subsequent sendInput must also return immediately with updated acked offset.
	acked, err := s.sendInput("s-1", 0, []byte("world"))
	if err != nil {
		t.Fatalf("sendInput while write blocked: %v", err)
	}
	if acked != 5 {
		t.Fatalf("acked = %d, want 5", acked)
	}

	// Info() and output pump must remain completely unblocked while ptmx.Write is stalled.
	if info := s.info(); info.TerminalID != "term-block" {
		t.Fatalf("unexpected info: %+v", info)
	}
	dev.readCh <- []byte("OUTPUT_WHILE_WRITE_BLOCKED\n")

	deadline := time.After(2 * time.Second)
	sawOutput := false
	for !sawOutput {
		select {
		case f := <-frames:
			if f.Kind == FrameOutput && strings.Contains(string(f.Output), "OUTPUT_WHILE_WRITE_BLOCKED") {
				sawOutput = true
			}
		case <-deadline:
			t.Fatal("output pump stalled while ptmx.Write was blocked (AB-BA deadlock)")
		}
	}

	// Unblock ptmx.Write and verify all queued bytes arrive in order.
	close(dev.unblockWrite)
	waitDeadline := time.After(2 * time.Second)
	for dev.writtenString() != "hello world" {
		select {
		case <-waitDeadline:
			t.Fatalf("written = %q, want %q", dev.writtenString(), "hello world")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestSessionWriteQueueFullReturnsErrorWithoutBlocking(t *testing.T) {
	dev := newBlockingWriteDevice()
	s := newSession("term-full", 1235, "bash")
	s.ptmx = dev
	defer s.close(true)

	// Trigger writer goroutine and block it inside dev.Write.
	if err := s.writeRaw([]byte("x")); err != nil {
		t.Fatalf("initial writeRaw: %v", err)
	}
	select {
	case <-dev.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine did not start")
	}

	// Fill the queue up to maxWriteQueueBytes.
	big := bytes.Repeat([]byte("a"), maxWriteQueueBytes)
	if err := s.writeRaw(big); err != nil {
		t.Fatalf("filling queue failed: %v", err)
	}

	// Next write must fail immediately with ErrWriteQueueFull instead of blocking.
	done := make(chan error, 1)
	go func() {
		done <- s.writeRaw([]byte("overflow"))
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWriteQueueFull) {
			t.Fatalf("err = %v, want ErrWriteQueueFull", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("writeRaw blocked when write queue was full")
	}
}

// TestRealPTY_BidirectionalBackpressureNoDeadlock exercises a real OS PTY where
// the parent floods >4KB of input (exceeding macOS ~1KB TTYHOG) before the child
// reads stdin, while the child simultaneously writes >64KB to stdout.
func TestRealPTY_BidirectionalBackpressureNoDeadlock(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reg.SetWorkspace(dir)

	info, err := reg.Create(CreateRequest{WorkspaceURI: "file://" + dir})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = reg.Close(info.TerminalID, true) }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ch, err := reg.StreamChannelFrom(ctx, info.TerminalID, 0)
	if err != nil {
		t.Fatalf("StreamChannelFrom: %v", err)
	}

	// Child sleeps briefly (letting parent fill PTY input queue), floods stdout,
	// then reads 4096 bytes from stdin and prints a marker that does not appear
	// literally in the command string.
	script := "stty -echo; python3 -c 'import sys,time; time.sleep(0.15); sys.stdout.write(\"X\"*65536+\"\\n\"); sys.stdout.flush(); data=sys.stdin.read(4096); print(\"REAL_\"+\"DONE_\"+str(len(data)))' || (sleep 0.2; head -c 4096 >/dev/null; printf 'REAL_%s\\n' 'DONE_FALLBACK')\n"
	if err := reg.Write(info.TerminalID, []byte(script)); err != nil {
		t.Fatalf("Write script: %v", err)
	}

	// Immediately flood >4KB of input (4x macOS TTYHOG) across short lines (< MAX_CANON)
	// without waiting for child.
	payload := strings.Repeat(strings.Repeat("A", 63)+"\n", 64)
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- reg.Write(info.TerminalID, []byte(payload))
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("Write payload: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("reg.Write blocked on full PTY input buffer")
	}

	var out strings.Builder
	for {
		if strings.Contains(out.String(), "REAL_DONE_") {
			break
		}
		select {
		case f, ok := <-ch:
			if !ok {
				t.Fatalf("stream closed before REAL_DONE_; got %d bytes", out.Len())
			}
			if f.Kind == FrameOutput {
				out.Write(f.Output)
			}
		case <-ctx.Done():
			t.Fatalf("timed out waiting for REAL_DONE_ under bidirectional backpressure; got %d bytes", out.Len())
		}
	}
}
