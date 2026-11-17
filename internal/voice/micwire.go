// Package voice — micwire.go implements the upstream Antigravity/Jetski
// audio framing protocol (JETSKI-AUDIO/1).
package voice

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	// ProtocolVersion is the wire protocol identifier for streaming audio.
	ProtocolVersion = "JETSKI-AUDIO/1"

	// FrameMaxPayload is the maximum allowed frame size (1MB).
	FrameMaxPayload = 1024 * 1024

	// FrameAudio indicates raw PCM audio frame.
	FrameAudio byte = 0x01

	// FrameControl indicates stream control or metadata.
	FrameControl byte = 0x00

	// FrameError indicates a protocol or execution error.
	FrameError byte = 0x02
)

// ServerError indicates an error response returned by the audio server.
type ServerError struct {
	Msg string
}

func (e *ServerError) Error() string {
	return "micwire: " + e.Msg
}

// WriteFrame encodes and writes a 5-byte header followed by the frame payload.
// Header format: [frameType: 1 byte][length: 4 bytes uint32 little-endian].
func WriteFrame(w io.Writer, frameType byte, payload []byte) error {
	if len(payload) > FrameMaxPayload {
		return fmt.Errorf("micwire: payload size %d exceeds max frame size %d", len(payload), FrameMaxPayload)
	}
	var header [5]byte
	header[0] = frameType
	binary.LittleEndian.PutUint32(header[1:], uint32(len(payload)))

	if _, err := w.Write(header[:]); err != nil {
		return fmt.Errorf("micwire: write frame header: %w", err)
	}
	if len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return fmt.Errorf("micwire: write frame payload: %w", err)
		}
	}
	return nil
}

// ReadFrame reads a 5-byte header followed by the frame payload.
func ReadFrame(r io.Reader) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	frameType := header[0]
	length := binary.LittleEndian.Uint32(header[1:])
	if length > FrameMaxPayload {
		return 0, nil, fmt.Errorf("micwire: frame size %d exceeds max %d", length, FrameMaxPayload)
	}

	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, fmt.Errorf("micwire: read payload: %w", err)
		}
	}
	return frameType, payload, nil
}

// ReadLine reads a single line from an io.Reader up to \n, stripping trailing whitespace.
func ReadLine(r io.Reader) (string, error) {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	line, err := br.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// SpeakerHello sends the client speaker handshake and waits for the server confirmation.
// Client sends: "JETSKI-AUDIO/1 speaker rate=%d\n\n"
// Server returns: "JETSKI-AUDIO/1 OK" or "JETSKI-AUDIO/1 ERR <reason>"
func SpeakerHello(rw io.ReadWriter, rate int) error {
	hello := fmt.Sprintf("%s speaker rate=%d\n\n", ProtocolVersion, rate)
	if _, err := io.WriteString(rw, hello); err != nil {
		return fmt.Errorf("micwire: send hello: %w", err)
	}

	reply, err := ReadLine(rw)
	if err != nil {
		return fmt.Errorf("micwire: read hello reply: %w", err)
	}
	if strings.HasPrefix(reply, ProtocolVersion+" OK") {
		return nil
	}
	if strings.HasPrefix(reply, ProtocolVersion+" ERR ") {
		return &ServerError{Msg: strings.TrimPrefix(reply, ProtocolVersion+" ERR ")}
	}
	return fmt.Errorf("micwire: unexpected handshake reply: %q", reply)
}

// ParseSpeakerHello parses a handshake line sent by a client.
// Format: "JETSKI-AUDIO/1 speaker rate=<rate>"
func ParseSpeakerHello(line string) (int, error) {
	line = strings.TrimSpace(line)
	prefix := ProtocolVersion + " speaker rate="
	if !strings.HasPrefix(line, prefix) {
		return 0, fmt.Errorf("micwire: invalid hello line: %q", line)
	}
	rateStr := strings.TrimPrefix(line, prefix)
	rate, err := strconv.Atoi(rateStr)
	if err != nil || rate <= 0 {
		return 0, fmt.Errorf("micwire: invalid sample rate %q: %w", rateStr, err)
	}
	return rate, nil
}

// ReplyErr writes an error handshake reply to the writer.
func ReplyErr(w io.Writer, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	_, err := fmt.Fprintf(w, "%s ERR %s\n", ProtocolVersion, msg)
	return err
}

// ReplyOK writes a successful handshake reply to the writer.
func ReplyOK(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s OK\n", ProtocolVersion)
	return err
}
