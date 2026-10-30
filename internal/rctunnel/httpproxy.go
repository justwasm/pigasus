// Package rctunnel — httpproxy.go: the inbound proxy-request bridge.
//
// The reference does not treat a tunnel message as an RPC method
// name: it turns every inbound `ProxyRequestV2` into an
// `http.Request` and serves it against its own language server
// handler (`remotecontrol.newProxiedHTTPRequest` →
// `routeAllowlistFilter.ServeHTTP` → `proxyResponseFramer.send`).
// The proxy request therefore carries an HTTP path, HTTP headers and
// an HTTP body — the Connect-RPC dialect the local Web UI speaks.
//
// The clone does the same against its own local HTTP handler, so a
// request that arrives over the cloud tunnel and a request that
// arrives over the loopback socket take the identical code path.
//
// Evidence:
//   - out/local-macos/dec/10250ef10_newProxiedHTTPRequest.c      【反编译】
//   - out/local-macos/dec/10250de80_executeRequest.c              【反编译】
//   - out/local-macos/dec/102384720_proxyResponseFramer_marshalFrame.c 【反编译】
package rctunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/justwasm/pigasus/rctoken"
)

const (
	// headerViaRemoteControl marks a request that reached the local
	// server through the remote-control tunnel. The reference sets it
	// on every proxied request, and the local handler uses it to tell
	// a tunnel caller apart from a browser on the loopback socket.
	//
	// Cycle rc-auth-bearer-required: the spelling now lives in
	// rctoken, so the tunnel, the transport and the web UI
	// read one name for it.
	headerViaRemoteControl = rctoken.ViaRemoteControlHeader

	// headerRemoteTransport names the transport the request came in
	// on: "webchannel" for the cloud tunnel, "datachannel" for the
	// WebRTC data channel.
	headerRemoteTransport = "X-Jetski-Remote-Control-Transport"

	// headerConnectVersion is the Connect protocol version the
	// reference adds when the caller did not send one.
	headerConnectVersion = "Connect-Protocol-Version"

	// headerStreamingFlag marks a request whose response is a
	// server stream.
	headerStreamingFlag = "x-is-streaming"

	// transportWebChannel is the transport name for the cloud tunnel.
	transportWebChannel = "webchannel"

	// contentTypeJSON is the Connect unary content type.
	contentTypeJSON = "application/json"

	// contentTypeProto is the Connect unary content type of the
	// binary-protobuf dialect.
	contentTypeProto = "application/proto"

	// contentTypeConnectJSON is the Connect streaming content type of
	// the JSON dialect.
	contentTypeConnectJSON = "application/connect+json"

	// contentTypeConnectProto is the Connect streaming content type of
	// the binary-protobuf dialect. The reference's language server
	// speaks protobuf, so this is what it answers a stream with.
	contentTypeConnectProto = "application/connect+proto"

	// contentTypeGRPCWebProto is the gRPC-web content type of the
	// binary-protobuf dialect: the same messages, a different framing.
	contentTypeGRPCWebProto = "application/grpc-web+proto"

	// contentTypeGRPCWebJSON is the gRPC-web content type of the JSON
	// dialect.
	contentTypeGRPCWebJSON = "application/grpc-web+json"
)

// buildProxiedRequest turns an inbound proxy request into the
// `http.Request` the reference builds.
//
// The reference always uses POST, copies every inbound header, adds
// the two `X-Jetski-*` markers, rewrites `Content-Type` to the
// Connect flavour the local handler speaks, and adds
// `Connect-Protocol-Version: 1` when the caller left it out.
//
// The rewrite follows the caller, not the other way round. The cloud's
// v2 client announces the binary-protobuf dialect with
// `content-type: application/proto` on a unary call, and the answer has
// to come back in the same dialect — the client decodes the response
// with the same switch it used for the request 【一手, the CLI bundle's
// XChannelTransport.unary: `this.binaryUnaryEnabled ? Pc(output, chunk)
// : Re(output, text, {ignoreUnknownFields:true})`]. A streaming call
// carries no content type at all; the reference's language server
// speaks protobuf, so the proto flavour is the default there.
func buildProxiedRequest(ctx context.Context, req *ProxyRequestV2) (*http.Request, error) {
	u, err := url.Parse(cleanRoutePath(req.RPCPath))
	if err != nil {
		return nil, err
	}
	body := req.Payload
	if len(body) == 0 && req.PayloadText != "" {
		body = []byte(req.PayloadText)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range req.Headers {
		if key == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	httpReq.Header.Set(headerViaRemoteControl, "true")
	httpReq.Header.Set(headerRemoteTransport, transportWebChannel)
	isStreaming := isStreamingRPC(req.Headers, req.RPCPath)
	httpReq.Header.Set("Content-Type", proxiedContentType(req.Headers, isStreaming))
	if isStreaming {
		httpReq.Header.Set(headerStreamingFlag, "true")
	}
	if httpReq.Header.Get(headerConnectVersion) == "" {
		httpReq.Header.Set(headerConnectVersion, "1")
	}
	return httpReq, nil
}

// callerContentType reads the content type the cloud relayed with an
// inbound request.
//
// The cloud forwards the caller's own headers, and the caller is not
// always the web UI: the dashboard's host page sends no content type
// at all, while the UI's Connect client names one. The value decides
// which dialect the local handler answers in, so it is worth having in
// the log.
func callerContentType(headers map[string]string) string {
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// proxiedContentType picks the content type the local handler should
// see.
//
// The caller's own content type wins whenever it names a Connect
// dialect: the answer has to come back in the dialect the caller
// asked for. A caller that named none gets the JSON flavour for a
// *unary* call, which is Connect's own default — and it is what the
// reference answers in:
//
// 【一手】 2026-10-26, live: the dashboard's host page asks the
// instance for its Mendel flags without naming a content type, and the
// reference answers with JSON text ("{"experimentConfig":{…}}"). The
// host page reads that answer with JSON.parse, so a protobuf answer
// breaks it — "SyntaxError: Unexpected token '\u0000'" — and the page
// never learns whether the WebRTC mesh is enabled.
//
// A *streaming* call with no content type is the other way round: the
// answer has to be binary protobuf, because the remote-control app
// reads a stream frame as protobuf and nothing else.
//
// 【一手】 2026-10-26, live: the CLI bundle's own stream readers —
// the MessagePort reader (`mHc`) and the webchannel reader (`xHc`) —
// both decode a frame with `Kc(output, data)`, the *binary* protobuf
// decoder; neither has a JSON branch. The app's stream request names
// no content type at all (the bundle sets `x-is-streaming: true` and
// nothing else), so the instance has to answer protobuf: a JSON frame
// is dropped as a malformed message, the app state store never
// initialises, the manager layout never mounts, and the app never
// posts its `uiReady` message — the dashboard spins on "Connecting to
// instance…" forever. The reference's language server speaks protobuf,
// so its streaming handler answers binary frames.
func proxiedContentType(headers map[string]string, streaming bool) string {
	caller := ""
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			caller = strings.ToLower(strings.TrimSpace(value))
			break
		}
	}
	switch caller {
	case contentTypeProto:
		if streaming {
			return contentTypeConnectProto
		}
		return contentTypeProto
	case contentTypeJSON:
		if streaming {
			return contentTypeConnectJSON
		}
		return contentTypeJSON
	case contentTypeConnectProto:
		return contentTypeConnectProto
	case contentTypeConnectJSON:
		return contentTypeConnectJSON
	case contentTypeGRPCWebProto:
		// gRPC-web carries the same protobuf messages; the framing is
		// the caller's business.
		if streaming {
			return contentTypeConnectProto
		}
		return contentTypeProto
	case contentTypeGRPCWebJSON:
		if streaming {
			return contentTypeConnectJSON
		}
		return contentTypeJSON
	}
	if streaming {
		// A stream with no caller dialect is binary protobuf: the
		// remote-control app decodes a stream frame as protobuf only.
		return contentTypeConnectProto
	}
	return contentTypeJSON
}

// cleanRoutePath normalises the proxied path the way the reference's
// `cleanRoutePath` does: `path.Clean`, with a trailing slash kept
// when the original had one.
func cleanRoutePath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	cleaned := path.Clean(p)
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return cleaned
}

// isStreamingRequest reports whether the caller asked for a server
// stream. The reference reads the same header.
func isStreamingRequest(headers map[string]string) bool {
	for key, value := range headers {
		if !strings.EqualFold(key, headerStreamingFlag) {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(value), "true")
	}
	return false
}

// isStreamingRPC reports whether the request is a server stream either via header
// or well-known streaming RPC method name.
func isStreamingRPC(headers map[string]string, rpcPath string) bool {
	if isStreamingRequest(headers) {
		return true
	}
	method := rpcPath
	if idx := strings.LastIndex(rpcPath, "/"); idx != -1 {
		method = rpcPath[idx+1:]
	}
	switch method {
	case "StreamTerminalOutput", "StreamAgentStateUpdates", "JetboxSubscribeToState",
		"ProjectUpdatesStream", "SubscribeToPlugins", "SubscribeToMcpServerStates",
		"JetboxSubscribeToSummaries", "SubscribeToPlanState":
		return true
	}
	return false
}

// recordedResponse captures what the local handler wrote, chunk by
// chunk. It is the clone's counterpart of the reference's response
// recorder.
//
// The reference's recorder is a framer: every write the local handler
// makes becomes one tunnel frame, and the frames carry `IsFinal:false`
// until the last one. That is what makes a live stream possible —
// the web UI sees the agent's output while the turn is still running
// instead of after it has finished.
type recordedResponse struct {
	header http.Header
	status int
	// onChunk is called for every write, in order, with the bytes
	// exactly as the handler wrote them.
	onChunk func([]byte)
}

// newRecordedResponse returns a recorder with the default 200 status.
func newRecordedResponse(onChunk func([]byte)) *recordedResponse {
	return &recordedResponse{header: http.Header{}, status: http.StatusOK, onChunk: onChunk}
}

// Header implements http.ResponseWriter.
func (r *recordedResponse) Header() http.Header { return r.header }

// Write implements http.ResponseWriter.
func (r *recordedResponse) Write(p []byte) (int, error) {
	if len(p) > 0 && r.onChunk != nil {
		chunk := make([]byte, len(p))
		copy(chunk, p)
		r.onChunk(chunk)
	}
	return len(p), nil
}

// WriteHeader implements http.ResponseWriter.
func (r *recordedResponse) WriteHeader(code int) {
	if code != 0 {
		r.status = code
	}
}

// proxyResponseChunk renders one chunk of a recorded response as a
// tunnel frame.
//
// The chunk goes into `payload` when it is binary data (protobuf,
// Connect/gRPC-Web streaming frames, non-UTF8 bytes, or chunks with NUL bytes),
// and into `payloadText` when it is plain UTF-8 text (e.g. unary JSON).
// The remote-control web client's WebChannel transport (Vcc / fKc) decodes
// binary frames from field 1 (`payload`); sending binary protobuf or Connect
// envelopes in `payloadText` causes the browser to drop the chunk or fail
// decoding.
func proxyResponseChunk(reqID string, rec *recordedResponse, chunk []byte) *ProxyResponseV2 {
	resp := &ProxyResponseV2{
		RequestID:  reqID,
		StatusCode: rec.status,
		Headers:    flattenHeaders(rec.header),
		IsFinal:    false,
	}
	if isBinaryResponse(rec, chunk) {
		resp.Payload = chunk
	} else {
		resp.PayloadText = string(chunk)
	}
	return resp
}

func isBinaryResponse(rec *recordedResponse, chunk []byte) bool {
	if !utf8.Valid(chunk) {
		return true
	}
	if rec != nil {
		ct := strings.ToLower(rec.header.Get("Content-Type"))
		if strings.Contains(ct, "proto") ||
			strings.Contains(ct, "connect+") ||
			strings.Contains(ct, "grpc-web") ||
			strings.Contains(ct, "octet-stream") {
			return true
		}
	}
	// Connect/gRPC-Web framing and binary protobuf frequently contain NUL bytes.
	// Valid JSON or text never contains raw NUL bytes.
	if bytes.IndexByte(chunk, 0) != -1 {
		return true
	}
	return false
}

// proxyResponseFinal renders the last frame of a recorded response: it
// carries no payload and marks the response final.
func proxyResponseFinal(reqID string, rec *recordedResponse) *ProxyResponseV2 {
	return &ProxyResponseV2{
		RequestID:  reqID,
		StatusCode: rec.status,
		Headers:    flattenHeaders(rec.header),
		IsFinal:    true,
	}
}

// flattenHeaders turns the recorder's header map into the proto's
// map<string,string>. A repeated header is joined with ", ", the
// same way `net/http` writes it on the wire.
func flattenHeaders(header http.Header) map[string]string {
	if len(header) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) == 0 {
			continue
		}
		out[key] = strings.Join(values, ", ")
	}
	return out
}

// handleInboundProxyRequest answers one inbound proxy request.
//
// The reference serves the request against its own language server
// handler; the clone serves it against the local HTTP handler. A
// tunnel without a local handler falls back to dispatching the last
// path segment as an RPC method name.
func (c *Client) handleInboundProxyRequest(ctx context.Context, token string, req *ProxyRequestV2) {
	if req == nil {
		return
	}
	if req.Headers != nil && strings.EqualFold(req.Headers["x-cancel-request"], "true") {
		c.log("proxyRequest reqID=%s was canceled by server", req.RequestID)
		return
	}

	c.log("inbound RPC path=%s reqID=%s callerContentType=%q", req.RPCPath, req.RequestID, callerContentType(req.Headers))
	if c.opts.Handler != nil {
		c.serveProxiedRequest(ctx, token, req)
		return
	}
	c.serveDispatchedRequest(ctx, token, req)
}

// serveProxiedRequest serves the inbound request against the local
// HTTP handler and ships the recorded response back through the
// tunnel. This is the reference's path: the proxy request becomes an
// `http.Request`, the local handler answers it, and the recorded
// response becomes the `ProxyResponseV2`.
//
// The response is shipped as a *sequence* of frames, one per write the
// local handler made, and the last frame carries `IsFinal: true`. The
// reference frames the same way, and it is what lets the web UI render
// the agent's output while the turn is still running: the local
// handler flushes one Connect envelope per update, and every envelope
// becomes its own frame on the way to the cloud.
func (c *Client) serveProxiedRequest(ctx context.Context, token string, req *ProxyRequestV2) {
	httpReq, err := buildProxiedRequest(ctx, req)
	if err != nil {
		c.log("build proxied request for %s failed: %v", req.RPCPath, err)
		return
	}
	var rec *recordedResponse
	rec = newRecordedResponse(func(chunk []byte) {
		c.sendProxyResponse(ctx, token, proxyResponseChunk(req.RequestID, rec, chunk))
	})
	c.opts.Handler.ServeHTTP(rec, httpReq)
	c.sendProxyResponse(ctx, token, proxyResponseFinal(req.RequestID, rec))
}

// serveDispatchedRequest is the fallback for a tunnel without a local
// handler: the last path segment is dispatched as an RPC method name
// and the result is returned as JSON.
func (c *Client) serveDispatchedRequest(ctx context.Context, token string, req *ProxyRequestV2) {
	rpcPath := req.RPCPath
	payloadText := "{}"

	// 1. Built-in health/status checks
	if strings.HasSuffix(rpcPath, "/GetStatus") || strings.HasSuffix(rpcPath, "/Heartbeat") {
		payloadText = "{}"
	} else if c.opts.Dispatcher != nil {
		// 2. Dispatch to remotecontrol.Dispatcher
		method := rpcPath
		if idx := strings.LastIndex(rpcPath, "/"); idx != -1 {
			method = rpcPath[idx+1:]
		}
		res, err := c.opts.Dispatcher.DispatchDirect(ctx, method, req.PayloadText)
		if err != nil {
			c.log("dispatch RPC %s error: %v (returning empty response)", method, err)
			payloadText = "{}"
		} else {
			payloadText = res
		}
	}

	c.sendProxyResponse(ctx, token, &ProxyResponseV2{
		RequestID:   req.RequestID,
		StatusCode:  http.StatusOK,
		Headers:     map[string]string{"content-type": contentTypeJSON},
		PayloadText: payloadText,
		IsFinal:     true,
	})
}

// responsePostTimeout bounds one response POST to the cloud.
const responsePostTimeout = 30 * time.Second

// sendProxyResponse ships one ProxyResponseV2 back through the
// tunnel, wrapped in the ConnectInstanceRequestV2 envelope the
// reference marshals with protojson.
//
// The reference's frame carries the instance metadata, the instance
// id and the payload-text flag next to the response itself
// (remotecontrol.ConnectInstanceRequestV2_builder_Build), so the
// clone sends the same four fields.
//
// The POST does not inherit the request's cancellation. The request's
// context is the session's, and the session's context is canceled the
// moment the session ends — but the cloud's browser-side channel is
// bound to that session, so a response that was ready when the session
// ended is still the answer the browser is waiting for.
//
// 【一手】 2026-10-26, live: 861 responses were lost this way in one
// day, every one of them with "context canceled", and each loss left
// the web UI waiting for an answer that never came.
func (c *Client) sendProxyResponse(ctx context.Context, token string, resp *ProxyResponseV2) {
	c.log("sendProxyResponse reqID=%s status=%d isFinal=%v payloadTextLen=%d payloadLen=%d", resp.RequestID, resp.StatusCode, resp.IsFinal, len(resp.PayloadText), len(resp.Payload))
	wrapReq := ConnectInstanceRequestV2{
		Metadata: &InstanceMetadata{
			Name:             c.opts.InstanceName,
			Version:          c.opts.Version,
			SupportsChunking: true,
		},
		InstanceID:          c.opts.InstanceID,
		ProxyResponse:       resp,
		SupportsPayloadText: true,
	}

	data, err := json.Marshal(wrapReq)
	if err != nil {
		c.log("marshal proxyResponse error: %v", err)
		return
	}

	postCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), responsePostTimeout)
	defer cancel()
	if _, err := c.sendForward(postCtx, token, string(data)); err != nil {
		c.log("sendForward proxyResponse error: %v", err)
	}
}
