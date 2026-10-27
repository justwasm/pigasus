// connect_envelope_test.go — the shared Connect envelope codec.
//
// The codec is the one both halves of the clone use: the transport
// writes an envelope towards the caller, the dispatcher reads the
// reference's own stream back. The test pins the layout and the three
// ways a read can fail.
package remotetransport

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// TestConnectEnvelopeLayout pins the five byte header: the flag byte,
// then the message length as a big-endian uint32.
func TestConnectEnvelopeLayout(t *testing.T) {
	got := ConnectEnvelope(ConnectFlagMessage, []byte(`{"a":1}`))
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x07, '{', '"', 'a', '"', ':', '1', '}'}
	if !bytes.Equal(got, want) {
		t.Errorf("ConnectEnvelope = %v, want %v", got, want)
	}
	if size := binary.BigEndian.Uint32(got[1:5]); size != 7 {
		t.Errorf("the length field = %d, want 7", size)
	}
}

// TestConnectEnvelopeFlags pins the two flag bytes.
func TestConnectEnvelopeFlags(t *testing.T) {
	if ConnectFlagMessage != 0x00 {
		t.Errorf("ConnectFlagMessage = %#x, want 0x00", ConnectFlagMessage)
	}
	if ConnectFlagEndStream != 0x02 {
		t.Errorf("ConnectFlagEndStream = %#x, want 0x02", ConnectFlagEndStream)
	}
}

// TestReadConnectEnvelopeRoundTrip pins that a written envelope reads
// back with the same flag and message.
func TestReadConnectEnvelopeRoundTrip(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	buf.Write(ConnectEnvelope(ConnectFlagMessage, []byte("one")))
	buf.Write(ConnectEnvelope(ConnectFlagMessage, []byte("two")))
	buf.Write(ConnectEnvelope(ConnectFlagEndStream, []byte("{}")))

	for _, want := range []string{"one", "two"} {
		flags, payload, err := ReadConnectEnvelope(buf)
		if err != nil {
			t.Fatalf("ReadConnectEnvelope: %v", err)
		}
		if flags != ConnectFlagMessage {
			t.Errorf("flags = %#x, want %#x", flags, ConnectFlagMessage)
		}
		if string(payload) != want {
			t.Errorf("payload = %q, want %q", payload, want)
		}
	}
	flags, payload, err := ReadConnectEnvelope(buf)
	if err != nil {
		t.Fatalf("ReadConnectEnvelope: %v", err)
	}
	if flags != ConnectFlagEndStream {
		t.Errorf("the last envelope's flags = %#x, want %#x", flags, ConnectFlagEndStream)
	}
	if string(payload) != "{}" {
		t.Errorf("the last envelope's payload = %q, want %q", payload, "{}")
	}
}

// TestReadConnectEnvelopeFailures pins the three failures: a clean end,
// a header that is cut short and a message that is cut short.
func TestReadConnectEnvelopeFailures(t *testing.T) {
	t.Run("clean end", func(t *testing.T) {
		_, _, err := ReadConnectEnvelope(bytes.NewReader(nil))
		if !errors.Is(err, io.EOF) {
			t.Errorf("err = %v, want io.EOF", err)
		}
	})
	t.Run("header cut short", func(t *testing.T) {
		_, _, err := ReadConnectEnvelope(bytes.NewReader([]byte{0x00, 0x00}))
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
		}
	})
	t.Run("message cut short", func(t *testing.T) {
		_, _, err := ReadConnectEnvelope(bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x00, 0x04, 'a'}))
		if !errors.Is(err, ErrTruncatedMessage) {
			t.Errorf("err = %v, want ErrTruncatedMessage", err)
		}
	})
}
