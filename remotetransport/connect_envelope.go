// Package remotetransport — connect_envelope.go: the Connect streaming
// envelope, the five bytes that frame every message of a server stream.
//
// The envelope is one flag byte, a four byte big-endian length and the
// message itself. The end-of-stream envelope carries the 0x02 flag.
//
// Both halves of the clone speak it: this package writes it towards the
// caller, and the dispatcher (internal/refbackend) reads the reference's
// own stream back. The codec lives here so the two halves cannot drift
// apart.
//
// Evidence grade: 一手 (the reference's own `JetboxSubscribeToState`
// answer, captured off the wire with a raw socket).
package remotetransport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// ConnectFlagMessage marks an ordinary message envelope.
	ConnectFlagMessage byte = 0x00

	// ConnectFlagEndStream marks the end-of-stream envelope.
	ConnectFlagEndStream byte = 0x02

	// connectEnvelopeHeader is the size of an envelope's header: the
	// flag byte and the four byte length.
	connectEnvelopeHeader = 5
)

// ErrTruncatedMessage is returned when a stream ends inside an
// envelope's message — the header was read, the message was not
// complete.
//
// It is a distinct error from io.ErrUnexpectedEOF because the two
// truncations mean different things to a reader: a header that is cut
// short means the stream simply ended, while a message that is cut
// short means the peer broke the protocol mid-message.
var ErrTruncatedMessage = errors.New("remotetransport: truncated connect message")

// ConnectEnvelope frames one message for the Connect streaming protocol.
func ConnectEnvelope(flags byte, payload []byte) []byte {
	out := make([]byte, connectEnvelopeHeader+len(payload))
	out[0] = flags
	binary.BigEndian.PutUint32(out[1:connectEnvelopeHeader], uint32(len(payload)))
	copy(out[connectEnvelopeHeader:], payload)
	return out
}

// ReadConnectEnvelope reads one envelope from r and returns its flag
// byte and its message.
//
// The three failures are distinct:
//
//   - io.EOF — the stream ended at an envelope boundary.
//   - io.ErrUnexpectedEOF — the stream ended inside the header.
//   - ErrTruncatedMessage — the stream ended inside the message.
func ReadConnectEnvelope(r io.Reader) (byte, []byte, error) {
	var head [connectEnvelopeHeader]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	size := int(binary.BigEndian.Uint32(head[1:connectEnvelopeHeader]))
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrTruncatedMessage, err)
	}
	return head[0], payload, nil
}
