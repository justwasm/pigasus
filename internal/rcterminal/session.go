// session.go: the state a live terminal keeps.
//
// The pty itself is platform code (session_unix.go /
// session_windows.go); everything here is the bookkeeping the
// registry and the stream need.
package rcterminal

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// subscriberQueue is how many frames a subscriber may fall behind
// before the pty reader waits for it.
const subscriberQueue = 256

// scrollbackLimit is how many bytes of history a terminal keeps for
// the replay frames.
const scrollbackLimit = 64 * 1024

// Session is one live terminal: a shell on a pty, the readers of
// its output, and the bookkeeping the registry needs.
type Session struct {
	id        string
	pid       int
	title     string
	createdAt time.Time

	conversationID string
	projectID      string
	workspace      string
	splitGroupID   string

	cmd  *os.Process
	ptmx *os.File
	// waitExit reaps the shell and answers its exit code. It is
	// nil on the platforms that have no pty.
	waitExit func() int

	mu       sync.Mutex
	subs     map[*subscriber]struct{}
	scroll   []byte
	acked    map[string]int64
	exited   bool
	exitCode int
	closed   bool
	titleNow string
	cwdNow   string
	scanner  oscScanner
	done     chan struct{}
}

// subscriber is one open output stream.
type subscriber struct {
	ch   chan Frame
	done chan struct{}
}

// newSession builds a session around a started shell.
func newSession(id string, pid int, title string) *Session {
	return &Session{
		id:        id,
		pid:       pid,
		title:     title,
		createdAt: time.Now().UTC(),
		subs:      map[*subscriber]struct{}{},
		acked:     map[string]int64{},
		done:      make(chan struct{}),
	}
}

// start begins reading the pty.
func (s *Session) start() {
	go s.pump()
}

// handleOutput records the scrollback and hands the bytes to the
// subscribers. The OSC sequences the shell emits (the title, the
// working directory) travel as their own frames, the way the
// reference reports them.
func (s *Session) handleOutput(chunk []byte) {
	title, cwd := s.scanner.scan(chunk)

	s.mu.Lock()
	s.scroll = append(s.scroll, chunk...)
	if len(s.scroll) > scrollbackLimit {
		s.scroll = s.scroll[len(s.scroll)-scrollbackLimit:]
	}
	s.mu.Unlock()

	if title != "" {
		s.mu.Lock()
		s.titleNow = title
		s.mu.Unlock()
		s.broadcast(Frame{Kind: FrameTitle, Title: title})
	}
	if cwd != "" {
		s.mu.Lock()
		s.cwdNow = cwd
		s.mu.Unlock()
		s.broadcast(Frame{Kind: FrameCwd, Cwd: cwd})
	}
	s.broadcast(Frame{Kind: FrameOutput, Output: chunk})
}

// broadcast hands a frame to every open stream.
func (s *Session) broadcast(frame Frame) {
	s.mu.Lock()
	subs := make([]*subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()
	for _, sub := range subs {
		select {
		case sub.ch <- frame:
		case <-sub.done:
		}
	}
}

// finish records the shell's exit and closes the streams.
func (s *Session) finish(code int) {
	s.mu.Lock()
	if s.exited {
		s.mu.Unlock()
		return
	}
	s.exited = true
	s.exitCode = code
	s.mu.Unlock()

	s.broadcast(Frame{Kind: FrameExitCode, ExitCode: code})
	s.closeSubscribers()
}

// closeSubscribers ends every open stream.
func (s *Session) closeSubscribers() {
	s.mu.Lock()
	subs := make([]*subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subs = map[*subscriber]struct{}{}
	s.mu.Unlock()
	for _, sub := range subs {
		close(sub.done)
		close(sub.ch)
	}
}

// info is the terminal's description.
func (s *Session) info() *Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &Info{
		TerminalID:     s.id,
		PID:            s.pid,
		Title:          s.title,
		ConversationID: s.conversationID,
		ProjectID:      s.projectID,
		Cwd:            s.cwdNow,
		SplitGroupID:   s.splitGroupID,
		WorkspaceURI:   s.workspace,
		CreatedAt:      s.createdAt.Format("2006-01-02T15:04:05.000000Z"),
	}
}

// setSplitGroup records the group a terminal heads or belongs to.
func (s *Session) setSplitGroup(group string) {
	s.mu.Lock()
	s.splitGroupID = group
	s.mu.Unlock()
}

// sendInput writes bytes to the shell and answers the offset the
// input stream has reached.
func (s *Session) sendInput(streamID string, offset int64, data []byte) (int64, error) {
	s.mu.Lock()
	if s.exited || s.closed {
		s.mu.Unlock()
		return 0, ErrClosed
	}
	acked := s.acked[streamID]
	if offset != acked {
		// The reference drops a chunk that does not line up with
		// the offset it has reached; the Web UI resends from the
		// acknowledged offset.
		s.mu.Unlock()
		return acked, nil
	}
	if len(data) > 0 {
		if _, err := s.ptmx.Write(data); err != nil {
			s.mu.Unlock()
			return acked, fmt.Errorf("rcterminal: write input: %w", err)
		}
		acked += int64(len(data))
		s.acked[streamID] = acked
	}
	s.mu.Unlock()
	return acked, nil
}

// stream serves the terminal's output until the terminal ends or
// emit fails.
func (s *Session) stream(emit func(Frame) error) error {
	sub := &subscriber{ch: make(chan Frame, subscriberQueue), done: make(chan struct{})}
	s.mu.Lock()
	if s.exited {
		code := s.exitCode
		s.mu.Unlock()
		// The shell is already gone: report its exit code and stop.
		return emit(Frame{Kind: FrameExitCode, ExitCode: code})
	}
	s.subs[sub] = struct{}{}
	scroll := append([]byte(nil), s.scroll...)
	title := s.title
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
	}()

	// The terminal's own title opens the stream: the reference
	// answers a stream with a `title` frame (the shell's name), the
	// same name its create answer carries.
	if err := emit(Frame{Kind: FrameTitle, Title: title}); err != nil {
		return err
	}
	if len(scroll) > 0 {
		if err := emit(Frame{Kind: FrameOutput, Output: scroll, IsScrollback: true}); err != nil {
			return err
		}
	}
	for {
		select {
		case frame, ok := <-sub.ch:
			if !ok {
				return nil
			}
			if err := emit(frame); err != nil {
				return err
			}
		case <-s.done:
			return nil
		}
	}
}
