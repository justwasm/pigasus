// Package websocket provides unified cross-platform WebSocket utilities across native and js/wasm platforms.
package websocket

import (
	"context"
	"net/http"

	coderws "github.com/coder/websocket"
)

// Conn wraps the underlying WebSocket connection.
type Conn = coderws.Conn

// MessageType indicates the type of a WebSocket message.
type MessageType = coderws.MessageType

const (
	// MessageText indicates a text data message.
	MessageText = coderws.MessageText
	// MessageBinary indicates a binary data message.
	MessageBinary = coderws.MessageBinary
)

// StatusCode represents a WebSocket close status code.
type StatusCode = coderws.StatusCode

const (
	StatusNormalClosure   = coderws.StatusNormalClosure
	StatusGoingAway       = coderws.StatusGoingAway
	StatusProtocolError   = coderws.StatusProtocolError
	StatusUnsupportedData = coderws.StatusUnsupportedData
	StatusPolicyViolation = coderws.StatusPolicyViolation
	StatusAbnormalClosure = coderws.StatusAbnormalClosure
	StatusInternalError   = coderws.StatusInternalError
)

// CloseStatus returns the close status from err, or -1 if none is present.
func CloseStatus(err error) StatusCode {
	return coderws.CloseStatus(err)
}

// AcceptOptions wraps the WebSocket handshake acceptance options.
type AcceptOptions = coderws.AcceptOptions

// Accept accepts a WebSocket handshake from an HTTP request.
func Accept(w http.ResponseWriter, r *http.Request, opts *AcceptOptions) (*Conn, error) {
	return coderws.Accept(w, r, opts)
}

// DialOptions configures a WebSocket handshake in a cross-platform manner.
// On desktop/native platforms, HTTPHeader and HTTPClient are passed to the handshake.
// On js/wasm (browser environments), HTTPHeader and HTTPClient are safely omitted because
// browser WebSockets do not support custom request headers or custom transport clients.
type DialOptions struct {
	HTTPClient   *http.Client
	HTTPHeader   http.Header
	Subprotocols []string
}

// Dial connects to the WebSocket URL, adapting options to the target platform.
func Dial(ctx context.Context, u string, opts *DialOptions) (*Conn, *http.Response, error) {
	return dial(ctx, u, opts)
}
