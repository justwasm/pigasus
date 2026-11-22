package voice

import (
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// MicWireStats captures real-time diagnostic metrics on the JETSKI-AUDIO/1 wire.
type MicWireStats struct {
	SentFrames int64
	SentBytes  int64
	RecvFrames int64
	RecvBytes  int64
}

// RemoteAudioStream manages a bidirectional TCP audio stream with an upstream mic server
// using the JETSKI-AUDIO/1 protocol.
type RemoteAudioStream struct {
	conn       net.Conn
	onAudio    func([]byte)
	writeMu    sync.Mutex
	muted      atomic.Bool
	running    atomic.Bool
	closed     atomic.Bool
	closeOnce  sync.Once
	doneCh     chan struct{}
	sentFrames atomic.Int64
	sentBytes  atomic.Int64
	recvFrames atomic.Int64
	recvBytes  atomic.Int64
}

// DialRemoteAudio connects to a JETSKI-AUDIO/1 mic server (e.g. ANTIGRAVITY_MIC).
func DialRemoteAudio(addr string, onAudio func([]byte)) (*RemoteAudioStream, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, fmt.Errorf("micwire: dial remote audio server at %s: %w", addr, err)
	}

	// Handshake: declare 24kHz speaker stream
	if err := SpeakerHello(conn, 24000); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("micwire: handshake failed with %s: %w", addr, err)
	}

	stream := &RemoteAudioStream{
		conn:    conn,
		onAudio: onAudio,
		doneCh:  make(chan struct{}),
	}
	stream.running.Store(true)

	// Read loop: receive 16kHz audio from server's microphone
	go stream.readPump()

	return stream, nil
}

func (s *RemoteAudioStream) readPump() {
	defer close(s.doneCh)
	for {
		frameType, payload, err := ReadFrame(s.conn)
		if err != nil {
			return
		}
		s.recvFrames.Add(1)
		s.recvBytes.Add(int64(len(payload)))
		if frameType == FrameAudio && len(payload) > 0 {
			if s.running.Load() && !s.muted.Load() && s.onAudio != nil {
				s.onAudio(payload)
			}
		}
	}
}

// WireStats answers real-time telemetry metrics across the remote audio stream.
func (s *RemoteAudioStream) WireStats() MicWireStats {
	if s == nil {
		return MicWireStats{}
	}
	return MicWireStats{
		SentFrames: s.sentFrames.Load(),
		SentBytes:  s.sentBytes.Load(),
		RecvFrames: s.recvFrames.Load(),
		RecvBytes:  s.recvBytes.Load(),
	}
}

// RemoteAudioPlayer satisfies LiveSpeaker over a JETSKI-AUDIO/1 connection.
type RemoteAudioPlayer struct {
	stream *RemoteAudioStream
}

func NewRemoteAudioPlayer(stream *RemoteAudioStream) *RemoteAudioPlayer {
	return &RemoteAudioPlayer{stream: stream}
}

func (p *RemoteAudioPlayer) Write(pcm []byte) (int, error) {
	if p.stream == nil || p.stream.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	p.stream.writeMu.Lock()
	defer p.stream.writeMu.Unlock()
	if err := WriteFrame(p.stream.conn, FrameAudio, pcm); err != nil {
		return 0, err
	}
	p.stream.sentFrames.Add(1)
	p.stream.sentBytes.Add(int64(len(pcm)))
	return len(pcm), nil
}

func (p *RemoteAudioPlayer) Clear() {
	if p.stream == nil || p.stream.closed.Load() {
		return
	}
	p.stream.writeMu.Lock()
	defer p.stream.writeMu.Unlock()
	_ = WriteFrame(p.stream.conn, FrameControl, []byte("clear"))
}

func (p *RemoteAudioPlayer) Flush() {
	// Remote audio streams over TCP drain naturally
}

func (p *RemoteAudioPlayer) Buffered() int {
	return 0
}

func (p *RemoteAudioPlayer) Close() error {
	if p.stream != nil {
		return p.stream.Close()
	}
	return nil
}

// RemoteAudioCapture satisfies LiveMicrophone over a JETSKI-AUDIO/1 connection.
type RemoteAudioCapture struct {
	stream *RemoteAudioStream
}

func NewRemoteAudioCapture(stream *RemoteAudioStream) *RemoteAudioCapture {
	return &RemoteAudioCapture{stream: stream}
}

func (c *RemoteAudioCapture) Start() error {
	if c.stream != nil {
		c.stream.running.Store(true)
	}
	return nil
}

func (c *RemoteAudioCapture) Stop() error {
	if c.stream != nil {
		c.stream.running.Store(false)
	}
	return nil
}

func (c *RemoteAudioCapture) Close() error {
	if c.stream != nil {
		return c.stream.Close()
	}
	return nil
}

func (c *RemoteAudioCapture) SetMuted(muted bool) {
	if c.stream != nil {
		c.stream.muted.Store(muted)
	}
}

func (c *RemoteAudioCapture) ToggleMute() bool {
	if c.stream == nil {
		return false
	}
	newVal := !c.stream.muted.Load()
	c.stream.muted.Store(newVal)
	return newVal
}

func (c *RemoteAudioCapture) IsMuted() bool {
	if c.stream == nil {
		return false
	}
	return c.stream.muted.Load()
}

func (s *RemoteAudioStream) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.running.Store(false)
		err = s.conn.Close()
	})
	return err
}

// MicServer serves a local microphone and speaker over the JETSKI-AUDIO/1 protocol.
type MicServer struct {
	addr       string
	ln         net.Listener
	closed     atomic.Bool
	stopCh     chan struct{}
	sentFrames atomic.Int64
	sentBytes  atomic.Int64
	recvFrames atomic.Int64
	recvBytes  atomic.Int64
}

// NewMicServer starts listening on addr.
func NewMicServer(addr string) (*MicServer, error) {
	if addr == "" {
		addr = "127.0.0.1:4713"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("mic-serve: cannot listen on %s: %w", addr, err)
	}
	return &MicServer{
		addr:   addr,
		ln:     ln,
		stopCh: make(chan struct{}),
	}, nil
}

// Addr returns the listener address.
func (s *MicServer) Addr() net.Addr {
	return s.ln.Addr()
}

// Serve accepts client connections and serves bidirectional audio.
func (s *MicServer) Serve() error {
	defer s.ln.Close()

	for {
		conn, err := s.ln.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return nil
			default:
				return err
			}
		}

		go s.handleConn(conn)
	}
}

func (s *MicServer) handleConn(conn net.Conn) {
	defer conn.Close()

	// 1. Read handshake line
	line, err := ReadLine(conn)
	if err != nil {
		return
	}
	rate, err := ParseSpeakerHello(line)
	if err != nil {
		_ = ReplyErr(conn, "invalid hello: %v", err)
		return
	}
	if err := ReplyOK(conn); err != nil {
		return
	}

	// 2. Initialize speaker player for incoming audio
	player, err := NewOtoPlayer(rate, 1)
	if err != nil {
		_ = ReplyErr(conn, "cannot init player: %v", err)
		return
	}
	defer player.Close()

	// 3. Initialize mic capture to stream to client
	var writeMu sync.Mutex
	capture, err := NewAudioCapture(16000, func(pcm16k []byte) {
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := WriteFrame(conn, FrameAudio, pcm16k); err == nil {
			s.sentFrames.Add(1)
			s.sentBytes.Add(int64(len(pcm16k)))
		}
	})
	if err != nil {
		return
	}
	_ = capture.Start()
	defer capture.Close()

	// 4. Read frames from client (speaker audio playback + control)
	for {
		frameType, payload, err := ReadFrame(conn)
		if err != nil {
			return
		}
		s.recvFrames.Add(1)
		s.recvBytes.Add(int64(len(payload)))
		switch frameType {
		case FrameAudio:
			_, _ = player.Write(payload)
		case FrameControl:
			if string(payload) == "clear" {
				player.Clear()
			}
		}
	}
}

// WireStats answers real-time telemetry metrics on the server listener.
func (s *MicServer) WireStats() MicWireStats {
	if s == nil {
		return MicWireStats{}
	}
	return MicWireStats{
		SentFrames: s.sentFrames.Load(),
		SentBytes:  s.sentBytes.Load(),
		RecvFrames: s.recvFrames.Load(),
		RecvBytes:  s.recvBytes.Load(),
	}
}

// Close stops the MicServer.
func (s *MicServer) Close() error {
	s.closed.Store(true)
	close(s.stopCh)
	return s.ln.Close()
}

// RunMicServer is the blocking entry point for the mic-serve subcommand.
func RunMicServer(addr string, errOut io.Writer) error {
	server, err := NewMicServer(addr)
	if err != nil {
		return err
	}
	defer server.Close()

	if errOut != nil {
		fmt.Fprintf(errOut, "Serving the microphone on %s. Press ctrl+c to stop.\n", server.Addr().String())
		fmt.Fprintln(errOut, "Note: Ensure this terminal has microphone access.")
	}

	return server.Serve()
}
