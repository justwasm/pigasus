// Package terminalmgr provides an in-process, per-instance persistent terminal
// session manager backed by rcterminal.Registry and headless vito/midterm
// virtual terminal emulators.
package terminalmgr

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/rcterminal"
	"github.com/vito/midterm"
)

const (
	defaultRows          = 24
	defaultCols          = 80
	defaultWaitMs        = 150
	maxWaitMs            = 5000
	maxScrollbackLines   = 2000
	maxCaptureScrollback = 500
)

// SessionInfo describes one managed terminal session.
type SessionInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name,omitempty"`
	Title           string `json:"title,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
	PID             int    `json:"pid,omitempty"`
	Rows            int    `json:"rows"`
	Cols            int    `json:"cols"`
	CursorX         int    `json:"cursor_x"`
	CursorY         int    `json:"cursor_y"`
	Exited          bool   `json:"exited"`
	ExitCode        int    `json:"exit_code,omitempty"`
	Status          string `json:"status"`
	ScrollbackLines int    `json:"scrollback_lines"`
}

// CaptureResult holds a terminal session's metadata and rendered screen/history.
type CaptureResult struct {
	Session    SessionInfo `json:"session"`
	Screen     []string    `json:"screen"`
	Scrollback []string    `json:"scrollback,omitempty"`
}

// NewOptions configures creating a new terminal session.
type NewOptions struct {
	Name    string
	Cwd     string
	Command string
	Rows    int
	Cols    int
	WaitMs  *int
}

// CaptureOptions configures capturing a terminal session's screen/scrollback.
type CaptureOptions struct {
	ID         string
	Scrollback int
	Raw        bool
}

// SendOptions configures sending text and/or keys to a terminal session.
type SendOptions struct {
	ID     string
	Text   string
	Keys   []string
	Paste  bool
	WaitMs *int
}

type managedSession struct {
	id         string
	name       string
	title      string
	cwd        string
	pid        int
	createdSeq int64
	lastActive time.Time

	mu               sync.Mutex
	term             *midterm.Terminal
	rows             int
	cols             int
	cursorKeysApp    bool
	pendingResponses [][]byte
	exited           bool
	exitCode         int
	status           string

	scrollbackMu sync.Mutex
	scrollback   []string

	cancel context.CancelFunc
}

// Manager owns in-process persistent terminal sessions for one pigasus instance.
type Manager struct {
	reg *rcterminal.Registry

	mu       sync.Mutex
	sessions map[string]*managedSession
	names    map[string]string // name -> id
	order    []string
	seq      int64
	closed   bool
}

// New creates a standalone Manager with its own rcterminal.Registry.
func New(workspace string) *Manager {
	reg := rcterminal.NewRegistry()
	if workspace != "" {
		reg.SetWorkspace(workspace)
	}
	return NewWithRegistry(reg)
}

// NewWithRegistry creates a Manager sharing an existing rcterminal.Registry
// (for example, the TUI's LocalTerminalService registry).
func NewWithRegistry(reg *rcterminal.Registry) *Manager {
	if reg == nil {
		reg = rcterminal.NewRegistry()
	}
	return &Manager{
		reg:      reg,
		sessions: make(map[string]*managedSession),
		names:    make(map[string]string),
	}
}

// Registry returns the underlying rcterminal.Registry.
func (m *Manager) Registry() *rcterminal.Registry {
	if m == nil {
		return nil
	}
	return m.reg
}

// SyncFromRegistry discovers any sessions created directly in rcterminal.Registry
// (e.g. via TUI /terminal) and attaches headless midterm tracking to them,
// while pruning sessions that were closed directly in the registry.
func (m *Manager) SyncFromRegistry() {
	if m == nil || m.reg == nil {
		return
	}
	infos := m.reg.List("", "", "")
	live := make(map[string]bool, len(infos))
	for i := range infos {
		info := infos[i]
		live[info.TerminalID] = true
		m.ensureSession(&info, "", defaultRows, defaultCols)
	}

	m.mu.Lock()
	var stale []*managedSession
	for id, s := range m.sessions {
		if !live[id] {
			stale = append(stale, s)
			delete(m.sessions, id)
			if s.name != "" && m.names[s.name] == id {
				delete(m.names, s.name)
			}
		}
	}
	if len(stale) > 0 {
		filtered := m.order[:0]
		for _, id := range m.order {
			if live[id] {
				filtered = append(filtered, id)
			}
		}
		m.order = filtered
	}
	m.mu.Unlock()

	for _, s := range stale {
		if s.cancel != nil {
			s.cancel()
		}
	}
}

func (m *Manager) ensureSession(info *rcterminal.Info, name string, rows, cols int) *managedSession {
	if info == nil || info.TerminalID == "" {
		return nil
	}
	if rows < 4 {
		rows = defaultRows
	}
	if cols < 10 {
		cols = defaultCols
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	if existing, ok := m.sessions[info.TerminalID]; ok {
		if name != "" && existing.name == "" {
			if prevID, taken := m.names[name]; !taken || prevID == info.TerminalID {
				existing.name = name
				m.names[name] = info.TerminalID
			}
		}
		m.mu.Unlock()
		existing.mu.Lock()
		if info.Title != "" && existing.title == "" {
			existing.title = info.Title
		}
		if info.Cwd != "" && existing.cwd == "" {
			existing.cwd = info.Cwd
		}
		if info.PID != 0 && existing.pid == 0 {
			existing.pid = info.PID
		}
		existing.mu.Unlock()
		return existing
	}

	m.seq++
	sess := &managedSession{
		id:         info.TerminalID,
		name:       name,
		title:      info.Title,
		cwd:        info.Cwd,
		pid:        info.PID,
		createdSeq: m.seq,
		lastActive: time.Now(),
		rows:       rows,
		cols:       cols,
		status:     "Running",
	}
	term := midterm.NewTerminal(rows, cols)
	term.Raw = false
	term.CursorVisible = true
	term.ForwardRequests = modeTrackerWriter{sess: sess}
	term.ForwardResponses = responseBufferWriter{sess: sess}
	term.OnScrollback(func(line midterm.Line) {
		styled := renderStyledLine(line)
		sess.scrollbackMu.Lock()
		defer sess.scrollbackMu.Unlock()
		sess.scrollback = append(sess.scrollback, styled)
		if len(sess.scrollback) > maxScrollbackLines {
			sess.scrollback = sess.scrollback[len(sess.scrollback)-maxScrollbackLines:]
		}
	})
	sess.term = term

	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel

	m.sessions[info.TerminalID] = sess
	if name != "" {
		m.names[name] = info.TerminalID
	}
	m.order = append(m.order, info.TerminalID)
	m.mu.Unlock()

	m.startPump(ctx, sess)
	return sess
}

func (m *Manager) startPump(ctx context.Context, sess *managedSession) {
	frames, err := m.reg.StreamChannelFrom(ctx, sess.id, 0)
	if err != nil {
		return
	}
	go func() {
		for f := range frames {
			switch f.Kind {
			case rcterminal.FrameOutput:
				if len(f.Output) > 0 {
					resps := sess.writeOutput(f.Output)
					for _, resp := range resps {
						_ = m.reg.Write(sess.id, resp)
					}
				}
			case rcterminal.FrameTitle:
				sess.mu.Lock()
				sess.title = f.Title
				sess.mu.Unlock()
			case rcterminal.FrameCwd:
				sess.mu.Lock()
				sess.cwd = f.Cwd
				sess.mu.Unlock()
			case rcterminal.FrameExitCode:
				sess.mu.Lock()
				sess.exited = true
				sess.exitCode = f.ExitCode
				if f.ExitCode == 0 {
					sess.status = "Exited (0)"
				} else {
					sess.status = fmt.Sprintf("Exited (%d)", f.ExitCode)
				}
				sess.mu.Unlock()
			}
		}
	}()
}

// List returns all tracked terminal sessions in creation order.
func (m *Manager) List() []SessionInfo {
	if m == nil {
		return nil
	}
	m.SyncFromRegistry()

	m.mu.Lock()
	ordered := make([]*managedSession, 0, len(m.order))
	for _, id := range m.order {
		if s := m.sessions[id]; s != nil {
			ordered = append(ordered, s)
		}
	}
	m.mu.Unlock()

	out := make([]SessionInfo, 0, len(ordered))
	for _, s := range ordered {
		out = append(out, s.info())
	}
	return out
}

// New creates a new persistent terminal session and returns its initial capture.
func (m *Manager) New(ctx context.Context, opts NewOptions) (CaptureResult, error) {
	if m == nil || m.reg == nil {
		return CaptureResult{}, fmt.Errorf("terminal: manager is not initialized")
	}
	name := strings.TrimSpace(opts.Name)
	if name != "" {
		m.mu.Lock()
		if existingID, ok := m.names[name]; ok {
			if existing := m.sessions[existingID]; existing != nil && !existing.isExited() {
				m.mu.Unlock()
				return CaptureResult{}, fmt.Errorf("terminal: session named %q already exists (%s)", name, existingID)
			}
			delete(m.names, name)
		}
		m.mu.Unlock()
	}

	rows := opts.Rows
	if rows < 4 {
		rows = defaultRows
	}
	cols := opts.Cols
	if cols < 10 {
		cols = defaultCols
	}

	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" {
		cwd = m.reg.Workspace()
	}
	info, err := m.reg.Create(rcterminal.CreateRequest{Workspace: cwd})
	if err != nil {
		return CaptureResult{}, fmt.Errorf("terminal: create: %w", err)
	}
	_ = m.reg.Resize(info.TerminalID, cols, rows)

	sess := m.ensureSession(info, name, rows, cols)
	if sess == nil {
		_ = m.reg.Close(info.TerminalID, true)
		return CaptureResult{}, fmt.Errorf("terminal: manager is closed")
	}

	if cmd := strings.TrimRight(opts.Command, "\r\n"); cmd != "" {
		if err := m.reg.Write(sess.id, []byte(cmd+"\r")); err != nil {
			return CaptureResult{}, fmt.Errorf("terminal: write initial command: %w", err)
		}
	}

	waitMs := clampWaitMs(opts.WaitMs, defaultWaitMs)
	if err := sleepWithContext(ctx, time.Duration(waitMs)*time.Millisecond); err != nil {
		return CaptureResult{}, err
	}

	return m.captureSession(sess, 0, false), nil
}

// Capture returns the current screen and optional scrollback of a session.
func (m *Manager) Capture(ctx context.Context, opts CaptureOptions) (CaptureResult, error) {
	if m == nil || m.reg == nil {
		return CaptureResult{}, fmt.Errorf("terminal: manager is not initialized")
	}
	if err := ctx.Err(); err != nil {
		return CaptureResult{}, err
	}
	m.SyncFromRegistry()
	sess, err := m.resolveSession(opts.ID)
	if err != nil {
		return CaptureResult{}, err
	}
	sb := opts.Scrollback
	if sb < 0 {
		sb = 0
	}
	if sb > maxCaptureScrollback {
		sb = maxCaptureScrollback
	}
	return m.captureSession(sess, sb, opts.Raw), nil
}

// Send writes text and/or encoded keys to a session, waits briefly, and returns
// the updated screen capture.
func (m *Manager) Send(ctx context.Context, opts SendOptions) (CaptureResult, error) {
	if m == nil || m.reg == nil {
		return CaptureResult{}, fmt.Errorf("terminal: manager is not initialized")
	}
	if opts.Text == "" && len(opts.Keys) == 0 {
		return CaptureResult{}, fmt.Errorf("terminal: send requires text or keys")
	}
	m.SyncFromRegistry()
	sess, err := m.resolveSession(opts.ID)
	if err != nil {
		return CaptureResult{}, err
	}

	sess.mu.Lock()
	cursorApp := sess.cursorKeysApp
	exited := sess.exited
	sess.mu.Unlock()
	if exited {
		return CaptureResult{}, fmt.Errorf("terminal: session %s has already exited", sess.id)
	}

	var payload []byte
	if opts.Text != "" {
		if opts.Paste {
			payload = append(payload, EncodePaste(opts.Text)...)
		} else {
			payload = append(payload, opts.Text...)
		}
	}
	if len(opts.Keys) > 0 {
		keyBytes, err := EncodeKeys(opts.Keys, cursorApp)
		if err != nil {
			return CaptureResult{}, err
		}
		payload = append(payload, keyBytes...)
	}

	if err := m.reg.Write(sess.id, payload); err != nil {
		return CaptureResult{}, fmt.Errorf("terminal: write: %w", err)
	}

	m.mu.Lock()
	sess.lastActive = time.Now()
	m.mu.Unlock()

	waitMs := clampWaitMs(opts.WaitMs, defaultWaitMs)
	if err := sleepWithContext(ctx, time.Duration(waitMs)*time.Millisecond); err != nil {
		return CaptureResult{}, err
	}

	return m.captureSession(sess, 0, false), nil
}

// Resize updates the PTY and virtual terminal dimensions for a session.
func (m *Manager) Resize(idOrName string, rows, cols int) (SessionInfo, error) {
	if m == nil || m.reg == nil {
		return SessionInfo{}, fmt.Errorf("terminal: manager is not initialized")
	}
	m.SyncFromRegistry()
	sess, err := m.resolveSession(idOrName)
	if err != nil {
		return SessionInfo{}, err
	}
	if rows < 4 {
		rows = 4
	}
	if cols < 10 {
		cols = 10
	}
	if err := m.reg.Resize(sess.id, cols, rows); err != nil {
		return SessionInfo{}, fmt.Errorf("terminal: resize: %w", err)
	}
	sess.mu.Lock()
	sess.rows = rows
	sess.cols = cols
	if sess.term != nil {
		sess.term.Resize(rows, cols)
	}
	sess.mu.Unlock()
	return sess.info(), nil
}

// Kill terminates a terminal session and removes it from tracking.
func (m *Manager) Kill(idOrName string) (SessionInfo, error) {
	if m == nil || m.reg == nil {
		return SessionInfo{}, fmt.Errorf("terminal: manager is not initialized")
	}
	m.SyncFromRegistry()
	sess, err := m.resolveSession(idOrName)
	if err != nil {
		return SessionInfo{}, err
	}
	info := sess.info()

	if sess.cancel != nil {
		sess.cancel()
	}
	_ = m.reg.Close(sess.id, true)

	m.mu.Lock()
	delete(m.sessions, sess.id)
	if sess.name != "" && m.names[sess.name] == sess.id {
		delete(m.names, sess.name)
	}
	filtered := m.order[:0]
	for _, id := range m.order {
		if id != sess.id {
			filtered = append(filtered, id)
		}
	}
	m.order = filtered
	m.mu.Unlock()

	return info, nil
}

// CloseAll cancels all stream pumps and closes all managed PTY sessions.
func (m *Manager) CloseAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	sessions := make([]*managedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*managedSession)
	m.names = make(map[string]string)
	m.order = nil
	m.mu.Unlock()

	for _, s := range sessions {
		if s.cancel != nil {
			s.cancel()
		}
		if m.reg != nil {
			_ = m.reg.Close(s.id, true)
		}
	}
}

func (m *Manager) resolveSession(idOrName string) (*managedSession, error) {
	key := strings.TrimSpace(idOrName)
	m.mu.Lock()
	defer m.mu.Unlock()

	if key != "" {
		if s, ok := m.sessions[key]; ok {
			return s, nil
		}
		if id, ok := m.names[key]; ok {
			if s, ok := m.sessions[id]; ok {
				return s, nil
			}
		}
		return nil, fmt.Errorf("terminal: session %q not found", key)
	}

	var bestLive *managedSession
	var bestAny *managedSession
	for _, id := range m.order {
		s := m.sessions[id]
		if s == nil {
			continue
		}
		if bestAny == nil || s.lastActive.After(bestAny.lastActive) || (s.lastActive.Equal(bestAny.lastActive) && s.createdSeq > bestAny.createdSeq) {
			bestAny = s
		}
		if !s.isExited() {
			if bestLive == nil || s.lastActive.After(bestLive.lastActive) || (s.lastActive.Equal(bestLive.lastActive) && s.createdSeq > bestLive.createdSeq) {
				bestLive = s
			}
		}
	}
	if bestLive != nil {
		return bestLive, nil
	}
	if bestAny != nil {
		return bestAny, nil
	}
	return nil, fmt.Errorf("terminal: no active sessions (create one with action=\"new\")")
}

func (m *Manager) captureSession(sess *managedSession, scrollbackCount int, raw bool) CaptureResult {
	info := sess.info()
	screen := sess.screenLines(raw)
	var sb []string
	if scrollbackCount > 0 {
		sb = sess.tailScrollback(scrollbackCount, raw)
	}
	return CaptureResult{
		Session:    info,
		Screen:     screen,
		Scrollback: sb,
	}
}

func (s *managedSession) isExited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

func (s *managedSession) info() SessionInfo {
	s.scrollbackMu.Lock()
	sbLen := len(s.scrollback)
	s.scrollbackMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	cx, cy := 0, 0
	rows, cols := s.rows, s.cols
	if s.term != nil {
		cx, cy = s.term.Cursor.X, s.term.Cursor.Y
		rows, cols = s.term.Height, s.term.Width
	}
	return SessionInfo{
		ID:              s.id,
		Name:            s.name,
		Title:           s.title,
		Cwd:             s.cwd,
		PID:             s.pid,
		Rows:            rows,
		Cols:            cols,
		CursorX:         cx,
		CursorY:         cy,
		Exited:          s.exited,
		ExitCode:        s.exitCode,
		Status:          s.status,
		ScrollbackLines: sbLen,
	}
}

func (s *managedSession) writeOutput(data []byte) [][]byte {
	s.mu.Lock()
	if s.term != nil && len(data) > 0 {
		_, _ = s.term.Write(data)
	}
	var resps [][]byte
	if len(s.pendingResponses) > 0 {
		resps = s.pendingResponses
		s.pendingResponses = nil
	}
	s.mu.Unlock()
	return resps
}

func (s *managedSession) screenLines(raw bool) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.term == nil {
		return nil
	}
	rows := s.term.Height
	lines := make([]string, rows)
	lastNonEmpty := -1
	for y := 0; y < rows; y++ {
		var b strings.Builder
		_ = s.term.RenderLine(&b, y)
		line := b.String()
		if !raw {
			line = StripANSI(line)
		}
		line = strings.TrimRight(line, " ")
		lines[y] = line
		if line != "" {
			lastNonEmpty = y
		}
	}
	if lastNonEmpty < 0 {
		return nil
	}
	return lines[:lastNonEmpty+1]
}

func (s *managedSession) tailScrollback(n int, raw bool) []string {
	s.scrollbackMu.Lock()
	defer s.scrollbackMu.Unlock()
	if len(s.scrollback) == 0 || n <= 0 {
		return nil
	}
	start := len(s.scrollback) - n
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(s.scrollback)-start)
	for _, line := range s.scrollback[start:] {
		if !raw {
			line = StripANSI(line)
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

type modeTrackerWriter struct {
	sess *managedSession
}

func (w modeTrackerWriter) Write(data []byte) (int, error) {
	if w.sess != nil {
		w.sess.applyForwardedModeLocked(data)
	}
	return len(data), nil
}

type responseBufferWriter struct {
	sess *managedSession
}

func (w responseBufferWriter) Write(data []byte) (int, error) {
	if w.sess != nil && len(data) > 0 {
		cp := make([]byte, len(data))
		copy(cp, data)
		w.sess.pendingResponses = append(w.sess.pendingResponses, cp)
	}
	return len(data), nil
}

func (s *managedSession) applyForwardedModeLocked(data []byte) {
	for len(data) > 0 {
		idx := bytes.Index(data, []byte("\x1b["))
		if idx < 0 {
			return
		}
		data = data[idx+2:]
		if len(data) > 0 && data[0] == '?' {
			data = data[1:]
		}
		end := 0
		for end < len(data) && data[end] >= '0' && data[end] <= '9' {
			end++
		}
		if end == 0 || end >= len(data) {
			return
		}
		modeNum, err := strconv.Atoi(string(data[:end]))
		op := data[end]
		data = data[end+1:]
		if err != nil || (op != 'h' && op != 'l') {
			continue
		}
		if modeNum == 1 {
			s.cursorKeysApp = (op == 'h')
		}
	}
}

func renderStyledLine(line midterm.Line) string {
	end := len(line.Content)
	for end > 0 {
		r := line.Content[end-1]
		var f midterm.Format
		if end-1 < len(line.Format) {
			f = line.Format[end-1]
		}
		if r == ' ' && f == (midterm.Format{}) {
			end--
			continue
		}
		break
	}
	if end == 0 {
		return ""
	}
	var b strings.Builder
	var lastFormat midterm.Format
	for col := 0; col < end; col++ {
		var f midterm.Format
		if col < len(line.Format) {
			f = line.Format[col]
		}
		if f != lastFormat {
			if formatLeaks(lastFormat, f) {
				b.WriteString("\x1b[0m")
			}
			b.WriteString(f.Render())
			lastFormat = f
		}
		b.WriteRune(line.Content[col])
	}
	if lastFormat != (midterm.Format{}) && !lastFormat.IsReset() {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func formatLeaks(prev, f midterm.Format) bool {
	if f.IsReset() || f == (midterm.Format{}) {
		return false
	}
	const attrs = midterm.BoldBit | midterm.FaintBit | midterm.ItalicBit | midterm.UnderlineBit | midterm.BlinkBit | midterm.ReverseBit | midterm.ConcealBit
	if (prev.Properties&attrs)&^f.Properties != 0 {
		return true
	}
	if prev.Fg != nil && f.Fg == nil {
		return true
	}
	if prev.Bg != nil && f.Bg == nil {
		return true
	}
	return false
}

func clampWaitMs(v *int, def int) int {
	if v == nil {
		return def
	}
	if *v < 0 {
		return 0
	}
	if *v > maxWaitMs {
		return maxWaitMs
	}
	return *v
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
