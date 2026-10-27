// Package remotetransport — tcp_connectrpc.go: Connect-RPC WebSocket
// stream protocol bridge for the Antigravity Web UI (class G3a in main.js).
//
// The Web UI communicates over ws://<host>:<port>/connect-websocket using
// Connect-RPC stream protocol envelopes:
//   - Ping probe: {"streamId": "<uuid>", "type": "ping"} -> reply "pong"
//   - Stream start: {"streamId": "<uuid>", "type": "start", "procedure": "...", "payload": {...}, "stream": bool}
//     returns {"streamId": "...", "type": "data", "payload": {...}}
//     followed by {"streamId": "...", "type": "end", "statusCode": 0}
//   - Stream cancel: {"streamId": "<uuid>", "type": "cancel"}
package remotetransport

import (
	"context"
	"encoding/json"
	"strings"
)

// wsConnectFrame matches the Connect-RPC over WebSocket frame envelope
// sent by the browser client.
type wsConnectFrame struct {
	StreamID  string              `json:"streamId"`
	Type      string              `json:"type"` // "start", "ping", "cancel", "data"
	Procedure string              `json:"procedure,omitempty"`
	Headers   map[string][]string `json:"headers,omitempty"`
	Payload   json.RawMessage     `json:"payload,omitempty"`
	Stream    bool                `json:"stream,omitempty"`
}

func (wc *wsConn) addStream(id string, cancel context.CancelFunc) {
	wc.streamsMu.Lock()
	if wc.streams == nil {
		wc.streams = make(map[string]context.CancelFunc)
	}
	wc.streams[id] = cancel
	wc.streamsMu.Unlock()
}

func (wc *wsConn) removeStream(id string) {
	wc.streamsMu.Lock()
	if wc.streams != nil {
		delete(wc.streams, id)
	}
	wc.streamsMu.Unlock()
}

func (wc *wsConn) cancelStream(id string) {
	wc.streamsMu.Lock()
	cancel, ok := wc.streams[id]
	if ok {
		delete(wc.streams, id)
	}
	wc.streamsMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

// dispatchConnectRPC handles Connect-RPC over WebSocket frames.
func (s *TCPServer) dispatchConnectRPC(wc *wsConn, reqCtx context.Context, cf wsConnectFrame) {
	switch cf.Type {
	case "ping":
		s.replyConnectPong(wc, reqCtx, cf.StreamID)
	case "start":
		s.handleConnectStart(wc, reqCtx, cf)
	case "cancel":
		wc.cancelStream(cf.StreamID)
	}
}

// replyConnectPong replies to client liveness probes with a pong frame.
func (s *TCPServer) replyConnectPong(wc *wsConn, reqCtx context.Context, streamID string) {
	frame, err := json.Marshal(map[string]any{
		"streamId": streamID,
		"type":     "pong",
	})
	if err == nil {
		_ = wc.enqueueFrame(reqCtx, frame)
	}
}

// handleConnectStart dispatches a unary or streaming RPC call.
func (s *TCPServer) handleConnectStart(wc *wsConn, reqCtx context.Context, cf wsConnectFrame) {
	method := cf.Procedure
	if idx := strings.LastIndex(method, "/"); idx >= 0 {
		method = method[idx+1:]
	}
	streamCtx, cancel := context.WithCancel(reqCtx)
	wc.addStream(cf.StreamID, cancel)
	defer wc.removeStream(cf.StreamID)

	if cf.Stream {
		s.handleConnectStream(wc, streamCtx, cf, method)
		return
	}

	s.handleConnectUnary(wc, streamCtx, cf, method)
}

// handleConnectUnary executes a single unary RPC call and returns data + end frames.
func (s *TCPServer) handleConnectUnary(wc *wsConn, reqCtx context.Context, cf wsConnectFrame, method string) {
	payloadText := string(cf.Payload)
	if strings.TrimSpace(payloadText) == "" {
		payloadText = "{}"
	}

	res, err := s.dispatcher.DispatchDirect(reqCtx, method, payloadText)
	if err != nil {
		// If method is not yet mapped, return empty object so the Web UI doesn't crash.
		if strings.Contains(err.Error(), "Method not found") {
			s.sendConnectData(wc, reqCtx, cf.StreamID, json.RawMessage("{}"))
			s.sendConnectEnd(wc, reqCtx, cf.StreamID, 0, nil)
			return
		}
		// A failure that names its code answers with the code the
		// reference answers with; the rest keep the catch-all.
		status := grpcWebStatusInternal
		code := "internal"
		if mapped, ok := connectErrorFor(err); ok {
			status = mapped.grpc
			code = mapped.name
		}
		s.sendConnectEnd(wc, reqCtx, cf.StreamID, status, map[string]any{
			"code":    code,
			"message": err.Error(),
		})
		return
	}

	if strings.TrimSpace(res) == "" {
		res = "{}"
	}
	s.sendConnectData(wc, reqCtx, cf.StreamID, json.RawMessage(res))
	s.sendConnectEnd(wc, reqCtx, cf.StreamID, 0, nil)
}

// handleConnectStream serves streaming RPCs (e.g. projectUpdatesStream, jetboxSubscribeToState).
func (s *TCPServer) handleConnectStream(wc *wsConn, reqCtx context.Context, cf wsConnectFrame, method string) {
	payloadText := string(cf.Payload)
	if strings.TrimSpace(payloadText) == "" {
		payloadText = "{}"
	}

	// Server-streaming RPCs first: the dispatcher owns the frame
	// cadence for those and pushes frames through the emit hook.
	if s.serveConnectServerStream(wc, reqCtx, cf, method, payloadText) {
		return
	}

	// A unary method that the caller sent as a stream is complete when
	// the dispatcher answered it: the Connect protocol wants a "data"
	// frame followed by an "end" frame, and the caller waits for the
	// end frame before it treats the call as done. Holding such a
	// stream open until the peer cancels it leaves the caller waiting
	// until its deadline expires ("the operation timed out").
	//
	// The service descriptor is the authority on whether a method is a
	// server stream: the transport's own dispatcher only knows the
	// methods it implements.
	if s.isUnaryConnectMethod(method) {
		res, err := s.dispatcher.DispatchDirect(reqCtx, method, payloadText)
		if err != nil || strings.TrimSpace(res) == "" {
			res = "{}"
		}
		s.sendConnectData(wc, reqCtx, cf.StreamID, json.RawMessage(res))
		s.sendConnectEnd(wc, reqCtx, cf.StreamID, 0, nil)
		return
	}

	// Try dispatching through dispatcher to get initial streaming data (e.g. state or project updates)
	if res, err := s.dispatcher.DispatchDirect(reqCtx, method, payloadText); err == nil && strings.TrimSpace(res) != "" && strings.TrimSpace(res) != "{}" {
		s.sendConnectData(wc, reqCtx, cf.StreamID, json.RawMessage(res))
	} else if method == "projectUpdatesStream" {
		// The frame is spelled the way proto-JSON spells a oneof: by
		// its member, not by the {case, value} envelope protobuf-es
		// decodes it into. The reference's own stream says exactly
		// this — a Connect envelope carrying {"projectList":{}}.
		initialUpdate := map[string]any{
			"projectList": map[string]any{},
		}
		raw, _ := json.Marshal(initialUpdate)
		s.sendConnectData(wc, reqCtx, cf.StreamID, json.RawMessage(raw))
	} else {
		s.sendConnectData(wc, reqCtx, cf.StreamID, json.RawMessage("{}"))
	}

	// Keep stream alive until context cancelled or connection torn down.
	<-reqCtx.Done()
	s.sendConnectEnd(wc, reqCtx, cf.StreamID, 0, nil)
}

// isUnaryConnectMethod reports whether the service descriptor marks the
// method as a unary method.
//
// The descriptor closure is the schema the frontend bundle itself
// embeds (lsproto). A method the descriptor does not know is
// treated as a stream: the stream path is the one that keeps the call
// alive, and an unknown method is far more likely to be one of the
// reference's long-lived subscriptions than a unary call.
func (s *TCPServer) isUnaryConnectMethod(method string) bool {
	if s.protoCodec == nil {
		return false
	}
	if !s.protoCodec.HasMethod(method) {
		return false
	}
	return !s.protoCodec.IsServerStreaming(method)
}

// serveConnectServerStream hands a server-streaming RPC to a
// dispatcher that implements StreamDispatcher. It reports whether the
// dispatcher took the call; when it did, the stream's frames and its
// end frame have already been sent.
func (s *TCPServer) serveConnectServerStream(wc *wsConn, reqCtx context.Context, cf wsConnectFrame, method string, payloadText string) bool {
	sd, ok := s.dispatcher.(StreamDispatcher)
	if !ok {
		return false
	}
	handled, err := sd.DispatchStream(reqCtx, method, payloadText, func(raw json.RawMessage) error {
		s.sendConnectData(wc, reqCtx, cf.StreamID, raw)
		return nil
	})
	if !handled {
		return false
	}
	if err != nil {
		s.sendConnectEnd(wc, reqCtx, cf.StreamID, 13, map[string]any{
			"code":    "internal",
			"message": err.Error(),
		})
		return true
	}
	s.sendConnectEnd(wc, reqCtx, cf.StreamID, 0, nil)
	return true
}

// sendConnectData sends a data frame on wc.
func (s *TCPServer) sendConnectData(wc *wsConn, reqCtx context.Context, streamID string, payload json.RawMessage) {
	frame, err := json.Marshal(map[string]any{
		"streamId": streamID,
		"type":     "data",
		"payload":  payload,
	})
	if err == nil {
		_ = wc.enqueueFrame(reqCtx, frame)
	}
}

// sendConnectEnd sends an end frame on wc with statusCode and optional error.
func (s *TCPServer) sendConnectEnd(wc *wsConn, reqCtx context.Context, streamID string, statusCode int, rpcErr any) {
	endMap := map[string]any{
		"streamId":   streamID,
		"type":       "end",
		"statusCode": statusCode,
	}
	if rpcErr != nil {
		endMap["error"] = rpcErr
	}
	frame, err := json.Marshal(endMap)
	if err == nil {
		_ = wc.enqueueFrame(reqCtx, frame)
	}
}
