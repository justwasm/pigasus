// Package remotetransport — grpcweb_codec.go: the gRPC-Web framing.
//
// The reference's own web bundle speaks gRPC-Web, not Connect: every
// call announces `content-type: application/grpc-web+json`, and the
// answer is a sequence of envelopes — one flag byte, a four byte
// big-endian length, then the payload. A unary call carries the
// message in the first envelope and the status in a second one, the
// trailers envelope, whose flag byte is 0x80. A server-streaming call
// carries its messages and then the same trailers envelope.
//
// The trailers envelope's payload is a header block, not JSON: the
// reference writes `grpc-status: 0\r\n`. The bundle's gRPC-Web client
// parses it exactly that way — it splits the block on "\r\n" and reads
// the `key: value` lines into a header map.
//
// Evidence:
//   - 【一手】 2026-10-26, live against the reference at 127.0.0.1:8802:
//     a unary call answers one message envelope and one trailers
//     envelope with `grpc-status: 0\r\n`; a failed call answers no
//     body at all and carries the status in the HTTP headers
//     (`Grpc-Status: 2`, `Grpc-Message: …`).
//   - 【一手】 2026-10-26, the bundle's own gRPC-Web reader: a frame
//     with the flag byte 0x80 is the trailers, and the stream ends
//     with "missing trailer" when there is none.
package remotetransport

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	// grpcWebFlagTrailers marks the trailers envelope (0x80).
	grpcWebFlagTrailers byte = 0x80

	// grpcWebStatusOK is the gRPC status of a call that succeeded.
	grpcWebStatusOK = 0

	// grpcWebStatusInternal is the gRPC status of a call the server
	// could not answer. The gRPC status codes are the canonical ones:
	// 13 is INTERNAL.
	grpcWebStatusInternal = 13
)

// grpcWebTrailers renders the trailers envelope's payload: the header
// block the reference writes.
func grpcWebTrailers(status int, message string) []byte {
	var b strings.Builder
	b.WriteString("grpc-status: ")
	b.WriteString(strconv.Itoa(status))
	b.WriteString("\r\n")
	if message != "" {
		b.WriteString("grpc-message: ")
		b.WriteString(message)
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

// writeGRPCWebMessage writes the message envelope of a unary answer.
func writeGRPCWebMessage(w http.ResponseWriter, flusher http.Flusher, payload []byte) error {
	return writeConnectEnvelope(w, flusher, ConnectFlagMessage, payload)
}

// grpcWebErrorMessage renders the message a failed gRPC-Web call
// carries.
//
// 【一手】 2026-10-27, measured against the reference binary: its
// failures read "<code>: <text> (error ID: <32 hex>)" — the Connect
// code, the handler's own text, and the id of the call:
//
//	invalid_argument: conversation id is required (error ID: c807a4…)
//	not_found: trajectory not found (error ID: 5e18d4…)
//
// The error id is the reference's own tracing id: a fresh 32 hex
// digits per call.
//
// The code is only added when the text does not carry it already:
// the reference's own text sometimes spells it out ("not_found:
// terminal … not found"), and that text is the same one the Connect
// dialect answers with.
func grpcWebErrorMessage(err error) string {
	text := err.Error()
	var coded connectErrorCoder
	if errors.As(err, &coded) {
		text = coded.ConnectMessage()
		if code, ok := connectErrorFor(err); ok && !strings.HasPrefix(text, code.name+": ") {
			text = code.name + ": " + text
		}
	}
	return text + " (error ID: " + grpcErrorID() + ")"
}

// grpcErrorID mints the id a failed call carries.
//
// It is the reference's tracing id: 32 hex digits, a new one per
// call.
func grpcErrorID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buf[:])
}

// writeGRPCWebTrailers writes the trailers envelope: the status of the
// call, in the header block the reference writes.
func writeGRPCWebTrailers(w http.ResponseWriter, flusher http.Flusher, status int, message string) error {
	return writeConnectEnvelope(w, flusher, grpcWebFlagTrailers, grpcWebTrailers(status, message))
}
