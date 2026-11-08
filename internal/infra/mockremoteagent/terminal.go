// terminal.go — the remote-agent proxy's terminal RPCs.
//
// The reference's terminal surface is a server-side PTY: createTerminal
// spawns a shell in the conversation's workspace, sendTerminalInput writes
// bytes, streamTerminalOutput replays/streams the bytes with offsets, and
// closeTerminal ends it. The session keeps the scrollback so a reconnecting
// client can resume from an offset.
package mockremoteagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// terminalEvent is one StreamTerminalOutput frame. Bytes marshal as
// base64 in proto-JSON.
type terminalEvent struct {
	Output       []byte `json:"output,omitempty"`
	ExitCode     *int32 `json:"exitCode,omitempty"`
	Offset       int64  `json:"offset,omitempty"`
	Title        string `json:"title,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	IsScrollback bool   `json:"isScrollback,omitempty"`
}

// terminalSession is one PTY and its scrollback.
type terminalSession struct {
	info TerminalInfo
	file *os.File
	cmd  *exec.Cmd

	mu       sync.Mutex
	buf      []byte
	offset   int64
	subs     map[chan terminalEvent]struct{}
	exitCode *int32
	closed   bool
}

// terminalManager owns every terminal this proxy has created.
type terminalManager struct {
	mu    sync.Mutex
	terms map[string]*terminalSession
}

func newTerminalManager() *terminalManager {
	return &terminalManager{terms: make(map[string]*terminalSession)}
}

// create spawns a login shell in workspace.
func (m *terminalManager) create(conversationID, workspace string) (TerminalInfo, error) {
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		}
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	file, err := openPTY(cmd)
	if err != nil {
		return TerminalInfo{}, err
	}
	id := fmt.Sprintf("term-%d", time.Now().UnixNano())
	info := TerminalInfo{
		TerminalID:     id,
		Title:          "Terminal",
		Cwd:            workspace,
		Pid:            int32(cmd.Process.Pid),
		ConversationID: conversationID,
		WorkspaceURI:   fileURI(workspace),
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	ts := &terminalSession{info: info, file: file, cmd: cmd, subs: make(map[chan terminalEvent]struct{})}

	m.mu.Lock()
	m.terms[id] = ts
	m.mu.Unlock()

	go ts.readLoop()
	return info, nil
}

// list returns terminals, filtered by conversation when set.
func (m *terminalManager) list(conversationID string) []TerminalInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TerminalInfo, 0, len(m.terms))
	for _, ts := range m.terms {
		if conversationID != "" && ts.info.ConversationID != conversationID {
			continue
		}
		out = append(out, ts.info)
	}
	return out
}

// get returns one terminal.
func (m *terminalManager) get(id string) (*terminalSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts, ok := m.terms[id]
	return ts, ok
}

// close ends a terminal.
func (m *terminalManager) close(id string) error {
	ts, ok := m.get(id)
	if !ok {
		return fmt.Errorf("terminal %q not found", id)
	}
	ts.close()
	return nil
}

// send writes input bytes to the PTY.
func (m *terminalManager) send(id string, input []byte) error {
	ts, ok := m.get(id)
	if !ok {
		return fmt.Errorf("terminal %q not found", id)
	}
	ts.mu.Lock()
	closed := ts.closed
	ts.mu.Unlock()
	if closed {
		return fmt.Errorf("terminal %q is closed", id)
	}
	_, err := ts.file.Write(input)
	return err
}

// readLoop copies the PTY into the scrollback.
func (t *terminalSession) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := t.file.Read(buf)
		if n > 0 {
			t.append(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	code := int32(0)
	if err := t.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = int32(exitErr.ExitCode())
		}
	}
	t.finish(code)
}

func (t *terminalSession) append(data []byte) {
	t.mu.Lock()
	t.buf = append(t.buf, data...)
	t.offset = int64(len(t.buf))
	event := terminalEvent{Output: data, Offset: t.offset}
	for ch := range t.subs {
		select {
		case ch <- event:
		default:
		}
	}
	t.mu.Unlock()
}

func (t *terminalSession) finish(code int32) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.exitCode = &code
	event := terminalEvent{ExitCode: &code, Offset: t.offset}
	for ch := range t.subs {
		select {
		case ch <- event:
		default:
		}
		close(ch)
		delete(t.subs, ch)
	}
	t.mu.Unlock()
}

func (t *terminalSession) close() {
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	_ = t.file.Close()
}

// subscribe returns a stream from resumeOffset. An already-finished
// terminal yields the exit frame and a closed channel.
func (t *terminalSession) subscribe(resumeOffset int64) chan terminalEvent {
	ch := make(chan terminalEvent, 64)
	t.mu.Lock()
	if resumeOffset < 0 {
		resumeOffset = 0
	}
	if resumeOffset < int64(len(t.buf)) {
		ch <- terminalEvent{
			Output:       append([]byte(nil), t.buf[resumeOffset:]...),
			Offset:       t.offset,
			IsScrollback: true,
		}
	}
	if t.exitCode != nil {
		code := *t.exitCode
		ch <- terminalEvent{ExitCode: &code, Offset: t.offset}
		close(ch)
	} else {
		t.subs[ch] = struct{}{}
	}
	t.mu.Unlock()
	return ch
}

func (t *terminalSession) unsubscribe(ch chan terminalEvent) {
	t.mu.Lock()
	if _, ok := t.subs[ch]; ok {
		delete(t.subs, ch)
		close(ch)
	}
	t.mu.Unlock()
}

// fileURI renders a workspace path as the file URI the protocol uses.
func fileURI(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

// ---- service handlers -----------------------------------------------------

func (s *Service) handleCreateTerminal(w http.ResponseWriter, body []byte) {
	var req CreateTerminalRequest
	_ = json.Unmarshal(body, &req)
	workspace := ""
	if session, ok := s.cascades.GetSession(req.ConversationID); ok && session != nil {
		workspace = sessionWorkspace(session)
	}
	info, err := s.terminals.create(req.ConversationID, workspace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, CreateTerminalResponse{Terminal: info})
}

func (s *Service) handleListTerminals(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		ConversationID string `json:"conversationId"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	if req.ConversationID == "" {
		req.ConversationID = r.URL.Query().Get("conversationId")
	}
	writeJSON(w, ListTerminalsResponse{Terminals: s.terminals.list(req.ConversationID)})
}

func (s *Service) handleCloseTerminal(w http.ResponseWriter, body []byte) {
	var req CloseTerminalRequest
	_ = json.Unmarshal(body, &req)
	if err := s.terminals.close(req.TerminalID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"success": true})
}

func (s *Service) handleSendTerminalInput(w http.ResponseWriter, body []byte) {
	var req SendTerminalInputRequest
	_ = json.Unmarshal(body, &req)
	if err := s.terminals.send(req.TerminalID, req.Input); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ackedOffset": req.InputOffset + int64(len(req.Input))})
}

func (s *Service) handleStreamTerminalOutput(w http.ResponseWriter, r *http.Request, body []byte) {
	var req StreamTerminalOutputRequest
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	if req.TerminalID == "" {
		req.TerminalID = r.URL.Query().Get("terminalId")
	}
	if raw := r.URL.Query().Get("resumeOffset"); raw != "" {
		fmt.Sscanf(raw, "%d", &req.ResumeOffset)
	}
	ts, ok := s.terminals.get(req.TerminalID)
	if !ok {
		http.Error(w, "terminal not found", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := ts.subscribe(req.ResumeOffset)
	defer ts.unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if event.ExitCode != nil {
				return
			}
		}
	}
}
