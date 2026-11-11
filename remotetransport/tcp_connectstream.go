// HTTP streaming is implemented by Connect-Go (connect_handler.go).
package remotetransport

import (
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/rctoken"
)

const (
	headerViaRemoteControl = rctoken.ViaRemoteControlHeader
	headerStreamingFlag    = "x-is-streaming"
)

// isStreamingRequest reports whether the caller asked for a server
// stream. The tunnel sets `x-is-streaming: true` on the proxied
// request; a browser client asks for the same thing with the Connect
// streaming content type — `application/connect+json` or
// `application/connect+proto`.
//
// gRPC-Web does not mark the stream in the request: its content type is
// the same for a unary and a server-streaming call, so the method's own
// descriptor decides. The reference's web bundle relies on it.
func (s *TCPServer) isStreamingRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(headerStreamingFlag)), "true") {
		return true
	}
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.Contains(ct, "connect+") {
		return true
	}
	if strings.Contains(ct, "grpc-web") {
		return s.protoCodec != nil && s.protoCodec.IsServerStreaming(methodFromPath(r.URL.Path))
	}
	if s.protoCodec != nil && s.protoCodec.IsServerStreaming(methodFromPath(r.URL.Path)) {
		return true
	}
	return false
}

// isRemoteControlRequest reports whether the request arrived through
// the remote-control tunnel.
//
// Cycle rc-auth-bearer-required: the answer now comes from
// rctoken, which owns the marker's spelling.
func isRemoteControlRequest(r *http.Request) bool {
	return rctoken.IsTunnelRequest(r)
}
