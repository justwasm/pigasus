// connect_errors.go: how a failed call is answered on the Connect
// dialects.
//
// 【一手】 2026-10-27, the reference's own answers, measured against
// its language_server (127.0.0.1:64019):
//
//	POST …/SendTerminalInput {"terminalId":"no-such-terminal",…}
//	  → 404 {"code":"not_found",
//	         "message":"not_found: terminal no-such-terminal not found (error ID: …)"}
//
//	POST …/CreateTerminal {"workspaceUri":123}
//	  → 400 {"code":"invalid_argument",
//	         "message":"unmarshal message: unmarshal into
//	           *language_server_go_proto.CreateTerminalRequest: proto:
//	           (line 1:18): invalid value for string field workspaceUri: 123"}
//
// A failure that names its code therefore answers with the HTTP
// status the code maps to and the JSON error body above. A failure
// that names no code keeps the transport's catch-all answer (HTTP
// 200 with an empty body): the Web UI's own calls that fail in the
// clone but not in the reference must not turn into a page error.
package remotetransport

import (
	"encoding/json"
	"errors"
	"net/http"
)

// connectErrorCoder is an error that carries the code of the
// failure.
//
// The interface is duck-typed on purpose: the dispatcher package
// implements it, and the import direction is the other way round (the
// dispatcher must not import the transport).
type connectErrorCoder interface {
	// JSONRPCCode is the JSON-RPC code of the failure.
	JSONRPCCode() int
	// ConnectMessage is the message a Connect client sees.
	ConnectMessage() string
}

// connectErrorCode is the Connect code a failure carries: the name
// the error body spells, the HTTP status the status line carries, and
// the gRPC status the gRPC-Web dialect carries in its header.
type connectErrorCode struct {
	name   string
	status int
	grpc   int
}

// The Connect codes the reference answers with.
const (
	// connectInvalidArgument is the code of a malformed request
	// (gRPC 3).
	connectInvalidArgument = "invalid_argument"
	// connectNotFound is the code of a request for something that
	// is not there (gRPC 5).
	connectNotFound = "not_found"
)

// The gRPC status numbers of the two codes above.
const (
	grpcStatusInvalidArgument = 3
	grpcStatusNotFound        = 5
)

// connectErrorFor maps a JSON-RPC code onto the Connect code the
// reference answers with.
//
// ok is false for a code the transport does not translate: those
// failures keep the catch-all answer.
func connectErrorFor(err error) (connectErrorCode, bool) {
	var coded connectErrorCoder
	if !errors.As(err, &coded) {
		return connectErrorCode{}, false
	}
	switch coded.JSONRPCCode() {
	case -32602:
		return connectErrorCode{
			name:   connectInvalidArgument,
			status: http.StatusBadRequest,
			grpc:   grpcStatusInvalidArgument,
		}, true
	case -32018, -32019:
		return connectErrorCode{
			name:   connectNotFound,
			status: http.StatusNotFound,
			grpc:   grpcStatusNotFound,
		}, true
	}
	return connectErrorCode{}, false
}

// connectErrorBody is the JSON body a Connect client reads for a
// failed call.
//
// The shape is the reference's: the code's name and the message.
func connectErrorBody(code connectErrorCode, err error) []byte {
	message := err.Error()
	var coded connectErrorCoder
	if errors.As(err, &coded) {
		message = coded.ConnectMessage()
	}
	return []byte(`{"code":"` + code.name + `","message":` + jsonQuote(message) + `}`)
}

// jsonQuote quotes a string for a JSON body.
func jsonQuote(text string) string {
	quoted, err := json.Marshal(text)
	if err != nil {
		return `""`
	}
	return string(quoted)
}
