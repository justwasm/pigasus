// Package remotetransport — connect_codec.go: the Connect dialect
// bridge.
//
// The Antigravity web UI speaks Connect-RPC against the language
// server, and in the v2 "binary unary" protocol it speaks *binary
// protobuf*: the request carries `content-type: application/proto`
// and the answer has to be a binary protobuf body. The clone's
// dispatcher speaks JSON, so without a bridge the UI cannot decode
// the answer — it reports "invalid end group tag" and "premature
// EOF", never boots, never tells the cloud page that it is ready,
// and the remote-control page spins forever on "Connecting to
// instance…".
//
// The bridge lives at the HTTP boundary, which is where the
// reference puts it: the reference's tunnel turns every inbound
// request into an `http.Request` and serves it against its own
// language server, so the loopback socket and the cloud tunnel
// take the identical code path.
//
// Evidence:
//   - out/ide-language-server/extract_ls_protoset.py 【一手】
//   - the CLI web bundle's "[XChannelTransport] Unary mode: binary
//     (proto)" log line, observed live in the cloud remote-control page
package remotetransport

import (
	"encoding/binary"
	"strings"

	"github.com/justwasm/pigasus/lsproto"
)

// The content types the two Connect dialects use.
const (
	contentTypeJSON         = "application/json"
	contentTypeProto        = "application/proto"
	contentTypeConnectJSON  = "application/connect+json"
	contentTypeConnectProto = "application/connect+proto"
	contentTypeGRPCWebJSON  = "application/grpc-web+json"
	contentTypeGRPCWebProto = "application/grpc-web+proto"
)

// connectDialect is the encoding a Connect-RPC body uses.
type connectDialect int

const (
	// dialectJSON is the JSON flavour of Connect-RPC.
	dialectJSON connectDialect = iota
	// dialectProto is the binary-protobuf flavour.
	dialectProto
	// dialectGRPCWebJSON is the gRPC-Web flavour with JSON payloads.
	//
	// The reference's own web bundle speaks gRPC-Web, not Connect: it
	// announces `application/grpc-web+json` and reads the answer in
	// the gRPC-Web framing — the message inside an envelope, then a
	// trailers envelope with the status. See grpcweb_codec.go.
	dialectGRPCWebJSON
	// dialectGRPCWebProto is the gRPC-Web flavour with binary
	// protobuf payloads.
	dialectGRPCWebProto
)

// isGRPCWeb reports whether the dialect is one of the gRPC-Web ones.
func (d connectDialect) isGRPCWeb() bool {
	return d == dialectGRPCWebJSON || d == dialectGRPCWebProto
}

// isProto reports whether the dialect's payload is binary protobuf.
func (d connectDialect) isProto() bool {
	return d == dialectProto || d == dialectGRPCWebProto
}

// dialectFor picks the dialect from a request's content type. The
// reference's v2 tunnel sends `application/proto`; the v1 tunnel and
// the loopback UI send JSON; the reference's own web bundle sends
// gRPC-Web.
func dialectFor(contentType string) connectDialect {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	proto := strings.Contains(ct, "proto")
	if strings.Contains(ct, "grpc-web") {
		if proto {
			return dialectGRPCWebProto
		}
		return dialectGRPCWebJSON
	}
	if proto {
		return dialectProto
	}
	return dialectJSON
}

// contentTypeFor answers the content type a response must carry.
func (d connectDialect) contentTypeFor(streaming bool) string {
	if d.isGRPCWeb() {
		if d.isProto() {
			return contentTypeGRPCWebProto
		}
		return contentTypeGRPCWebJSON
	}
	if streaming {
		if d == dialectProto {
			return contentTypeConnectProto
		}
		return contentTypeConnectJSON
	}
	if d == dialectProto {
		return contentTypeProto
	}
	return contentTypeJSON
}

// decodeRequestBody turns a wire body into the JSON the dispatcher
// speaks.
//
// A streaming request carries its message inside a Connect envelope —
// one flag byte, a four byte big-endian length, then the message — so
// the envelope is stripped first. The cloud's client always sends the
// envelope; the local UI sends the bare message.
//
// A gRPC-Web request always carries the envelope, unary calls too: the
// two protocols differ exactly there.
//
// A method the schema does not know keeps its raw body: the clone's
// dispatcher may still implement it, and the schema is not the
// authority on what the clone serves.
func (s *TCPServer) decodeRequestBody(method string, dialect connectDialect, body []byte, streaming bool) []byte {
	if streaming || dialect.isGRPCWeb() {
		body = stripConnectEnvelope(body)
	}
	if !dialect.isProto() || s.protoCodec == nil {
		return body
	}
	if !s.protoCodec.HasMethod(method) {
		return body
	}
	out, err := s.protoCodec.DecodeRequest(method, body)
	if err != nil {
		return body
	}
	return out
}

// stripConnectEnvelope removes the Connect envelope from a streaming
// request body when there is one.
//
// The envelope is one flag byte, a four byte big-endian length and the
// message. A body that does not match that shape is returned unchanged:
// it is a bare message.
func stripConnectEnvelope(body []byte) []byte {
	if len(body) < connectEnvelopeHeader {
		return body
	}
	if body[0] != ConnectFlagMessage && body[0] != ConnectFlagEndStream {
		return body
	}
	size := int(binary.BigEndian.Uint32(body[1:connectEnvelopeHeader]))
	if size != len(body)-connectEnvelopeHeader {
		return body
	}
	return body[connectEnvelopeHeader:]
}

// encodeResponseBody turns the dispatcher's JSON answer into the
// bytes the caller expects.
//
// The answer of a method the schema knows is encoded even when the
// dispatcher had nothing to say: an empty proto message parses, an
// empty JSON object does not parse as proto.
func (s *TCPServer) encodeResponseBody(method string, dialect connectDialect, jsonBody []byte) []byte {
	if !dialect.isProto() || s.protoCodec == nil {
		return jsonBody
	}
	if !s.protoCodec.HasMethod(method) {
		return jsonBody
	}
	out, err := s.protoCodec.EncodeResponse(method, jsonBody)
	if err != nil {
		return jsonBody
	}
	return out
}

// protoCodecFor returns the process-wide language-server codec, or nil
// when the descriptor set could not be loaded. A nil codec keeps the
// server on the JSON dialect instead of failing to start.
func protoCodecFor() *lsproto.Codec {
	codec, err := lsproto.Shared()
	if err != nil {
		return nil
	}
	return codec
}
