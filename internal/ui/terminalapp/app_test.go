package terminalapp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/infra/terminalmgr"
)

type fakeSession struct {
	id     string
	cwd    string
	title  string
	writes [][]byte
	rows   int
	cols   int
	closed bool
	events chan StreamEvent
}

type fakeBackend struct {
	mu       sync.Mutex
	seq      int
	sessions map[string]*fakeSession
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		sessions: make(map[string]*fakeSession),
	}
}

func (b *fakeBackend) Create(cwd string) (SessionInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := fmt.Sprintf("term-%d", b.seq)
	s := &fakeSession{
		id:     id,
		cwd:    cwd,
		title:  "zsh",
		events: make(chan StreamEvent, 16),
	}
	b.sessions[id] = s
	return SessionInfo{
		TerminalID: id,
		Title:      s.title,
		Cwd:        cwd,
		PID:        1000 + b.seq,
	}, nil
}

func (b *fakeBackend) Write(id string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sessions[id]
	if s == nil {
		return fmt.Errorf("not found: %s", id)
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	s.writes = append(s.writes, cp)
	return nil
}

func (b *fakeBackend) Resize(id string, rows, cols int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.sessions[id]; s != nil {
		s.rows = rows
		s.cols = cols
	}
	return nil
}

func (b *fakeBackend) Stream(ctx context.Context, id string, offset int64) (<-chan StreamEvent, error) {
	b.mu.Lock()
	s := b.sessions[id]
	b.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return s.events, nil
}

func (b *fakeBackend) Close(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.sessions[id]; s != nil {
		s.closed = true
	}
	return nil
}

func (b *fakeBackend) writtenBytes(id string) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sessions[id]
	if s == nil {
		return nil
	}
	var out []byte
	for _, w := range s.writes {
		out = append(out, w...)
	}
	return out
}

func (b *fakeBackend) clearWrites(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.sessions[id]; s != nil {
		s.writes = nil
	}
}

func TestParseArgs(t *testing.T) {
	opts, help, err := ParseArgs([]string{"-C", "/tmp/ws", "-c", "vim main.go", "-t", "editor", "--remain-on-exit"})
	if err != nil || help {
		t.Fatalf("unexpected err=%v help=%v", err, help)
	}
	if opts.Cwd != "/tmp/ws" || opts.Command != "vim main.go" || opts.Title != "editor" || !opts.RemainOnExit {
		t.Fatalf("unexpected opts: %+v", opts)
	}

	opts2, help2, err2 := ParseArgs([]string{"--cwd=/work", "--", "htop", "-d", "5"})
	if err2 != nil || help2 {
		t.Fatalf("unexpected err=%v help=%v", err2, help2)
	}
	if opts2.Cwd != "/work" || opts2.Command != "htop -d 5" {
		t.Fatalf("unexpected opts2: %+v", opts2)
	}

	_, help3, err3 := ParseArgs([]string{"--help"})
	if err3 != nil || !help3 {
		t.Fatalf("expected help=true, err=nil; got help=%v err=%v", help3, err3)
	}

	opts4, help4, err4 := ParseArgs([]string{"mcp", "--socket", "/tmp/pg.sock"})
	if err4 != nil || help4 {
		t.Fatalf("unexpected err=%v help=%v", err4, help4)
	}
	if !opts4.MCPMode || opts4.SocketPath != "/tmp/pg.sock" {
		t.Fatalf("unexpected mcp opts: %+v", opts4)
	}
}

func TestModel_EscPassesThroughToSubprocessAndDoesNotQuit(t *testing.T) {
	fb := newFakeBackend()
	m := NewModel(fb, Options{Cwd: "/tmp"})
	info, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Verify borderless Omni Bar dimensions: 30 rows total -> 29 body rows, 100 cols
	if fb.sessions[info.TerminalID].rows != 29 || fb.sessions[info.TerminalID].cols != 100 {
		t.Fatalf("PTY size = %dx%d, want 100x29", fb.sessions[info.TerminalID].cols, fb.sessions[info.TerminalID].rows)
	}
	v := m.View()
	lines := strings.Split(v.Content, "\n")
	if len(lines) != 30 {
		t.Fatalf("rendered lines = %d, want 30", len(lines))
	}
	if !v.AltScreen {
		t.Fatal("expected AltScreen=true")
	}
	plain := ansi.Strip(v.Content)
	if strings.Contains(plain, "╭─") || strings.Contains(plain, "╰─") {
		t.Fatalf("borderless Omni Bar must not render outer box borders:\n%s", plain)
	}
	topPlain := ansi.Strip(lines[0])
	if strings.Contains(topPlain, "Esc: Detach") {
		t.Fatalf("standalone terminal header must not advertise Esc: Detach:\n%s", topPlain)
	}
	if !strings.Contains(topPlain, "1:zsh") || !strings.Contains(topPlain, "[≡ Menu]") {
		t.Fatalf("expected Omni Bar tab and menu button in top row:\n%s", topPlain)
	}

	// Press Esc in live mode -> MUST pass 0x1B to subprocess and MUST NOT quit
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Fatal("Esc must NOT quit standalone terminal")
			}
		}
	}
	if m.quitting {
		t.Fatal("m.quitting must remain false after Esc")
	}
	got := fb.writtenBytes(info.TerminalID)
	if !bytes.Equal(got, []byte{0x1B}) {
		t.Fatalf("Esc written bytes = %v, want [0x1B]", got)
	}
	fb.clearWrites(info.TerminalID)

	// Ctrl+C and Ctrl+D also pass through directly to subprocess
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'd', Mod: tea.ModCtrl}))
	got = fb.writtenBytes(info.TerminalID)
	if !bytes.Equal(got, []byte{0x03, 0x04}) {
		t.Fatalf("Ctrl+C/Ctrl+D written bytes = %v, want [0x03, 0x04]", got)
	}
	fb.clearWrites(info.TerminalID)

	// Esc still exits Copy Mode without forwarding 0x1B to subprocess
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModAlt, Text: "c"}))
	if !m.activePanel().InCopyMode() {
		t.Fatal("expected Alt+C to enter copy mode")
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.activePanel().InCopyMode() {
		t.Fatal("expected Esc to exit copy mode")
	}
	if got := fb.writtenBytes(info.TerminalID); len(got) != 0 {
		t.Fatalf("Esc exiting copy mode must not forward bytes to PTY, got %v", got)
	}

	// Esc still closes Menu without forwarding 0x1B to subprocess
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'm', Mod: tea.ModAlt, Text: "m"}))
	if !m.activePanel().MenuOpen() {
		t.Fatal("expected Alt+M to open menu")
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.activePanel().MenuOpen() {
		t.Fatal("expected Esc to close menu")
	}
	if got := fb.writtenBytes(info.TerminalID); len(got) != 0 {
		t.Fatalf("Esc closing menu must not forward bytes to PTY, got %v", got)
	}
}

func TestModel_MultiTabAndExitLifecycle(t *testing.T) {
	fb := newFakeBackend()
	m := NewModel(fb, Options{Cwd: "/tmp"})
	first, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Alt+N creates a second tab inside the 1-row Omni Bar without shrinking body rows
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModAlt, Text: "n"}))
	if len(m.order) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(m.order))
	}
	secondID := m.activeID
	if secondID == first.TerminalID {
		t.Fatal("expected activeID to switch to new tab")
	}
	v := m.View()
	lines := strings.Split(v.Content, "\n")
	if len(lines) != 30 {
		t.Fatalf("2-tab rendered lines = %d, want 30", len(lines))
	}
	if fb.sessions[secondID].rows != 29 {
		t.Fatalf("2-tab PTY rows = %d, want 29", fb.sessions[secondID].rows)
	}

	// Alt+1 switches back to tab 1
	m.Update(tea.KeyPressMsg(tea.Key{Code: '1', Mod: tea.ModAlt, Text: "1"}))
	if m.activeID != first.TerminalID {
		t.Fatalf("Alt+1 activeID = %q, want %q", m.activeID, first.TerminalID)
	}

	// Alt+Right cycles to tab 2
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight, Mod: tea.ModAlt}))
	if m.activeID != secondID {
		t.Fatalf("Alt+Right activeID = %q, want %q", m.activeID, secondID)
	}

	// Subprocess exit in tab 2 auto-closes tab 2 and switches back to tab 1
	_, cmd := m.Update(termExitMsg{TerminalID: secondID, Code: 0})
	if m.quitting {
		t.Fatal("closing 1 of 2 tabs must not quit")
	}
	if len(m.order) != 1 || m.activeID != first.TerminalID {
		t.Fatalf("expected 1 tab (%q) remaining, got order=%v active=%q", first.TerminalID, m.order, m.activeID)
	}
	_ = cmd

	// Subprocess exit in last tab quits cleanly
	_, cmd = m.Update(termExitMsg{TerminalID: first.TerminalID, Code: 0})
	if !m.quitting || cmd == nil {
		t.Fatal("expected last tab exit to trigger quit")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatal("expected tea.QuitMsg on last tab exit")
	}
}

func TestModel_RemainOnExitAndAltQ(t *testing.T) {
	fb := newFakeBackend()
	m := NewModel(fb, Options{Cwd: "/tmp", RemainOnExit: true})
	first, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(termExitMsg{TerminalID: first.TerminalID, Code: 2})
	if m.quitting || len(m.order) != 1 {
		t.Fatal("RemainOnExit=true must keep exited tab open")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Exited (2)") {
		t.Fatalf("expected Exited (2) badge in view:\n%s", ansi.Strip(m.View().Content))
	}

	// Alt+Q quits and closes all sessions
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Mod: tea.ModAlt, Text: "q"}))
	if !m.quitting || cmd == nil {
		t.Fatal("expected Alt+Q to quit")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatal("expected tea.QuitMsg on Alt+Q")
	}
	if !fb.sessions[first.TerminalID].closed {
		t.Fatal("expected Alt+Q to close backend session")
	}
}

func TestLocalBackend_RealPTYRoundTripAndEscPassthrough(t *testing.T) {
	dir := t.TempDir()
	lb := NewLocalBackend(dir)
	m := NewModel(lb, Options{Cwd: dir})
	info, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}
	defer m.CloseAll()

	msgCh := make(chan tea.Msg, 128)
	m.sendMsg = func(msg tea.Msg) {
		msgCh <- msg
	}

	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Send a command that prints a marker and exits
	if err := lb.Write(info.TerminalID, []byte("echo PIGASUS_TERM_OK; exit\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	deadline := time.After(5 * time.Second)
	sawMarker := false
	sawExit := false
	for !sawExit {
		select {
		case msg := <-msgCh:
			m.Update(msg)
			if out, ok := msg.(termOutputMsg); ok && strings.Contains(string(out.Data), "PIGASUS_TERM_OK") {
				sawMarker = true
			}
			if _, ok := msg.(termExitMsg); ok {
				sawExit = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for real PTY output/exit (sawMarker=%v, sawExit=%v)", sawMarker, sawExit)
		}
	}
	if !sawMarker {
		t.Fatal("did not observe PIGASUS_TERM_OK from real PTY")
	}
	if !m.quitting {
		t.Fatal("expected model to enter quitting state after last PTY exited")
	}
}

func TestModel_ControlBackend_IPCAndSelfCapture(t *testing.T) {
	fb := newFakeBackend()
	m := NewModel(fb, Options{Cwd: "/tmp"})
	first, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Simulate Bubbletea event loop processing messages from m.emit
	msgCh := make(chan tea.Msg, 64)
	m.sendMsg = func(msg tea.Msg) {
		msgCh <- msg
	}
	stopLoop := make(chan struct{})
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		for {
			select {
			case <-stopLoop:
				return
			case msg := <-msgCh:
				_, cmd := m.Update(msg)
				if cmd != nil {
					go func(c tea.Cmd) {
						if res := c(); res != nil {
							m.emit(res)
						}
					}(cmd)
				}
			}
		}
	}()
	defer func() {
		close(stopLoop)
		<-loopDone
	}()

	// Feed output into first tab
	m.emit(termOutputMsg{TerminalID: first.TerminalID, Data: []byte("PIGASUS_TUI_SELF_LINE\r\n")})

	sockDir, err := os.MkdirTemp("/tmp", "pgt-app-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(sockDir)
	sockPath := filepath.Join(sockDir, "term.sock")

	srv, err := terminalmgr.StartIPCServer(sockPath, m.ControlBackend())
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer srv.Close()

	t.Setenv(terminalmgr.EnvSessionID, first.TerminalID)
	client := terminalmgr.NewIPCClient(sockPath)
	ctx := context.Background()

	// Capture "self"
	capSelf, err := client.Capture(ctx, terminalmgr.CaptureOptions{ID: "self"})
	if err != nil {
		t.Fatalf("Capture(self): %v", err)
	}
	if !capSelf.Session.Self || capSelf.Session.ID != first.TerminalID {
		t.Fatalf("unexpected self session info: %+v", capSelf.Session)
	}
	if !strings.Contains(strings.Join(capSelf.Screen, "\n"), "PIGASUS_TUI_SELF_LINE") {
		t.Fatalf("expected self screen content, got:\n%v", capSelf.Screen)
	}

	noWait := 0
	// Create a background tab without stealing focus
	created, err := client.New(ctx, terminalmgr.NewOptions{
		Name:    "worker",
		Command: "echo hi",
		Focus:   false,
		WaitMs:  &noWait,
	})
	if err != nil {
		t.Fatalf("New(worker): %v", err)
	}
	if created.Session.Name != "worker" {
		t.Fatalf("expected name=worker, got %+v", created.Session)
	}
	if m.activeID != first.TerminalID {
		t.Fatalf("Focus=false should keep activeID=%q, got %q", first.TerminalID, m.activeID)
	}
	if got := string(fb.writtenBytes(created.Session.ID)); !strings.Contains(got, "echo hi\r") {
		t.Fatalf("expected command written to worker tab, got %q", got)
	}

	// Switch and rename via IPC
	if _, err := client.Switch("worker"); err != nil {
		t.Fatalf("Switch(worker): %v", err)
	}
	if m.activeID != created.Session.ID {
		t.Fatalf("expected activeID=%q after Switch, got %q", created.Session.ID, m.activeID)
	}
	if _, err := client.Rename("worker", "build"); err != nil {
		t.Fatalf("Rename(worker, build): %v", err)
	}
	if _, err := client.Send(ctx, terminalmgr.SendOptions{ID: "build", Keys: []string{"Enter"}, WaitMs: &noWait}); err != nil {
		t.Fatalf("Send(build): %v", err)
	}
	if got := fb.writtenBytes(created.Session.ID); !bytes.HasSuffix(got, []byte{'\r'}) {
		t.Fatalf("expected Enter written to build tab, got %v", got)
	}

	// Verify Omni Bar reflects renamed tab
	list := client.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions in List, got %d", len(list))
	}
}

func TestModel_AltBackspaceAndModifiedKeysPassthrough(t *testing.T) {
	fb := newFakeBackend()
	m := NewModel(fb, Options{Cwd: "/tmp"})
	first, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Create a second tab so Alt+Shift+Left would switch tabs if not guarded
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModAlt, Text: "n"}))
	secondID := m.activeID
	if secondID == first.TerminalID {
		t.Fatal("expected second tab to be active")
	}

	// Alt+Backspace (macOS Option+Backspace) must pass ESC+DEL (0x1B 0x7F) to PTY
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace, Mod: tea.ModAlt}))
	if got := fb.writtenBytes(secondID); !bytes.Equal(got, []byte{0x1B, 0x7F}) {
		t.Fatalf("Alt+Backspace written = %v, want [0x1B, 0x7F]", got)
	}
	fb.clearWrites(secondID)

	// Alt+Shift+Left must pass through to PTY instead of switching tabs
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt | tea.ModShift}))
	if m.activeID != secondID {
		t.Fatalf("Alt+Shift+Left should not switch tabs; activeID=%q want %q", m.activeID, secondID)
	}
	if got := fb.writtenBytes(secondID); string(got) != "\x1b[1;4D" {
		t.Fatalf("Alt+Shift+Left written = %q, want \\x1b[1;4D", got)
	}
}

func TestLocalBackend_FullInputBufferDoesNotDeadlockUpdateLoop(t *testing.T) {
	dir := t.TempDir()
	lb := NewLocalBackend(dir)
	m := NewModel(lb, Options{Cwd: dir})
	info, err := m.CreateInitialSession()
	if err != nil {
		t.Fatalf("CreateInitialSession: %v", err)
	}
	defer m.CloseAll()

	msgCh := make(chan tea.Msg, 256)
	m.sendMsg = func(msg tea.Msg) {
		msgCh <- msg
	}
	if cmd := m.Init(); cmd != nil {
		cmd()
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Child pauses briefly before reading stdin, floods stdout, then reads 4096 bytes and exits.
	cmdStr := "stty -echo; python3 -c 'import sys,time; time.sleep(0.15); sys.stdout.write(\"Y\"*32768+\"\\n\"); sys.stdout.flush(); data=sys.stdin.read(4096); print(\"APP_\"+\"REAL_\"+str(len(data)))'; exit\r"
	if err := lb.Write(info.TerminalID, []byte(cmdStr)); err != nil {
		t.Fatalf("Write cmd: %v", err)
	}

	// Flood >4KB via Model.Update(PasteMsg) across short lines (< MAX_CANON) synchronously
	// on the Update loop; this must return immediately without blocking Update or stalling output.
	pasteDone := make(chan struct{})
	go func() {
		m.Update(tea.PasteMsg{Content: strings.Repeat(strings.Repeat("B", 63)+"\n", 65)})
		close(pasteDone)
	}()
	select {
	case <-pasteDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Model.Update(PasteMsg) blocked on full PTY input buffer")
	}

	deadline := time.After(15 * time.Second)
	sawDone := false
	sawExit := false
	for !sawExit {
		select {
		case msg := <-msgCh:
			m.Update(msg)
			if out, ok := msg.(termOutputMsg); ok && strings.Contains(string(out.Data), "APP_REAL_") {
				sawDone = true
			}
			if _, ok := msg.(termExitMsg); ok {
				sawExit = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for APP_REAL_/exit (sawDone=%v, sawExit=%v)", sawDone, sawExit)
		}
	}
	if !sawDone {
		t.Fatal("did not observe APP_REAL_ from real PTY")
	}
}
