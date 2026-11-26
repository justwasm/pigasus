// session.go: the state a live terminal keeps.
//
// The pty itself is platform code (session_unix.go /
// session_windows.go); everything here is the bookkeeping the
// registry and the stream need.
package rcterminal

import (
	"context"
	"errors"
	"fmt"
	"io"
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

// maxWriteQueueBytes caps buffered PTY input when a child stops reading stdin.
const maxWriteQueueBytes = 4 * 1024 * 1024

// ErrWriteQueueFull is returned when a terminal's pending input buffer is full.
var ErrWriteQueueFull = errors.New("rcterminal: input buffer full")

// terminalDevice wraps the underlying platform terminal (PTY on Unix, ConPTY on Windows).
type terminalDevice interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

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
	ptmx terminalDevice
	// waitExit reaps the shell and answers its exit code. It is
	// nil on the platforms that have no pty.
	waitExit func() int

	mu         sync.Mutex
	subs       map[*subscriber]struct{}
	scroll     []byte
	totalBytes int64
	acked      map[string]int64
	exited     bool
	exitCode   int
	closed     bool
	titleNow   string
	cwdNow     string
	scanner    oscScanner
	done       chan struct{}

	writeMu      sync.Mutex
	writeBuf     []byte
	writeErr     error
	writeClosed  bool
	writeStarted bool
	writeNotify  chan struct{}
	writeStop    chan struct{}
}

// subscriber is one open output stream.
type subscriber struct {
	ch   chan Frame
	done chan struct{}
}

// newSession builds a session around a started shell.
func newSession(id string, pid int, title string) *Session {
	return &Session{
		id:          id,
		pid:         pid,
		title:       title,
		createdAt:   time.Now().UTC(),
		subs:        map[*subscriber]struct{}{},
		acked:       map[string]int64{},
		done:        make(chan struct{}),
		writeNotify: make(chan struct{}, 1),
		writeStop:   make(chan struct{}),
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
	s.totalBytes += int64(len(chunk))
	offset := s.totalBytes
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
	s.broadcast(Frame{Kind: FrameOutput, Output: chunk, Offset: offset})
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

	s.closeWriteQueue()
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

// enqueueWrite appends input bytes to the session's non-blocking write queue
// and wakes the dedicated writer goroutine. It never holds s.mu or blocks on
// ptmx.Write, preventing AB-BA deadlocks when a child's PTY input/output
// buffers fill simultaneously.
func (s *Session) enqueueWrite(ptmx terminalDevice, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	s.writeMu.Lock()
	if s.writeClosed {
		s.writeMu.Unlock()
		return ErrClosed
	}
	if s.writeErr != nil {
		err := s.writeErr
		s.writeMu.Unlock()
		return err
	}
	if len(s.writeBuf)+len(data) > maxWriteQueueBytes {
		s.writeMu.Unlock()
		return ErrWriteQueueFull
	}
	s.writeBuf = append(s.writeBuf, data...)
	if !s.writeStarted {
		s.writeStarted = true
		if s.writeNotify == nil {
			s.writeNotify = make(chan struct{}, 1)
		}
		if s.writeStop == nil {
			s.writeStop = make(chan struct{})
		}
		go s.writeLoop(ptmx)
	}
	s.writeMu.Unlock()

	select {
	case s.writeNotify <- struct{}{}:
	default:
	}
	return nil
}

func (s *Session) writeLoop(ptmx terminalDevice) {
	for {
		s.writeMu.Lock()
		if len(s.writeBuf) == 0 {
			if s.writeClosed {
				s.writeMu.Unlock()
				return
			}
			s.writeMu.Unlock()
			select {
			case <-s.writeNotify:
				continue
			case <-s.writeStop:
				return
			}
		}
		chunk := s.writeBuf
		s.writeBuf = nil
		s.writeMu.Unlock()

		for len(chunk) > 0 {
			n, err := ptmx.Write(chunk)
			if n > 0 {
				chunk = chunk[n:]
			}
			if err != nil {
				s.writeMu.Lock()
				s.writeErr = err
				s.writeBuf = nil
				s.writeMu.Unlock()
				return
			}
			if n == 0 {
				s.writeMu.Lock()
				s.writeErr = io.ErrShortWrite
				s.writeBuf = nil
				s.writeMu.Unlock()
				return
			}
		}
	}
}

func (s *Session) closeWriteQueue() {
	s.writeMu.Lock()
	alreadyClosed := s.writeClosed
	s.writeClosed = true
	s.writeBuf = nil
	stopCh := s.writeStop
	s.writeMu.Unlock()
	if !alreadyClosed && stopCh != nil {
		close(stopCh)
	}
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
	ptmx := s.ptmx
	if len(data) > 0 {
		if ptmx == nil {
			s.mu.Unlock()
			return acked, fmt.Errorf("rcterminal: device is nil")
		}
		if err := s.enqueueWrite(ptmx, data); err != nil {
			s.mu.Unlock()
			return acked, fmt.Errorf("rcterminal: write input: %w", err)
		}
		acked += int64(len(data))
		s.acked[streamID] = acked
	}
	s.mu.Unlock()
	return acked, nil
}

// writeRaw writes bytes directly to the ptmx device without requiring a stream offset.
func (s *Session) writeRaw(data []byte) error {
	s.mu.Lock()
	if s.exited || s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	ptmx := s.ptmx
	s.mu.Unlock()
	if ptmx == nil {
		return fmt.Errorf("rcterminal: device is nil")
	}
	return s.enqueueWrite(ptmx, data)
}

// stream serves the terminal's output until the terminal ends or
// emit fails.
func (s *Session) stream(emit func(Frame) error) error {
	return s.streamContext(context.Background(), emit)
}

// streamContext serves the terminal's output until ctx is canceled,
// the terminal ends, or emit fails.
func (s *Session) streamContext(ctx context.Context, emit func(Frame) error) error {
	return s.streamContextFrom(ctx, 0, emit)
}

// streamContextFrom serves the terminal's output starting from resumeOffset
// until ctx is canceled, the terminal ends, or emit fails.
func (s *Session) streamContextFrom(ctx context.Context, resumeOffset int64, emit func(Frame) error) error {
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
	totalBytes := s.totalBytes
	title := s.titleNow
	if title == "" {
		title = s.title
	}
	cwd := s.cwdNow
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
	if cwd != "" {
		if err := emit(Frame{Kind: FrameCwd, Cwd: cwd}); err != nil {
			return err
		}
	}
	if resumeOffset < 0 {
		resumeOffset = 0
	}
	lastSentOffset := resumeOffset
	scrollStart := totalBytes - int64(len(scroll))
	if len(scroll) > 0 && resumeOffset < totalBytes {
		sliceFrom := int64(0)
		if resumeOffset > scrollStart {
			sliceFrom = resumeOffset - scrollStart
		}
		replay := scroll[sliceFrom:]
		if len(replay) > 0 {
			if err := emit(Frame{Kind: FrameOutput, Output: replay, Offset: totalBytes, IsScrollback: true}); err != nil {
				return err
			}
		}
		lastSentOffset = totalBytes
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-sub.ch:
			if !ok {
				return nil
			}
			if frame.Kind == FrameOutput && frame.Offset > 0 {
				if frame.Offset <= lastSentOffset {
					continue
				}
				startOffset := frame.Offset - int64(len(frame.Output))
				if startOffset < lastSentOffset {
					trim := int(lastSentOffset - startOffset)
					if trim >= len(frame.Output) {
						continue
					}
					frame.Output = frame.Output[trim:]
				}
				lastSentOffset = frame.Offset
			}
			if err := emit(frame); err != nil {
				return err
			}
		case <-s.done:
			return nil
		}
	}
}
