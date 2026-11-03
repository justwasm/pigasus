package rcmesh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The chunk protocol's limits.
//
// 【反编译】 handleWebRTCMessage (0x10259ed10) resolves an ingress frame
// against the session's reassembly buffers and answers a rejected frame with
// the reference's own status and message:
//
//	non-positive request id    400  "request_id must be a positive integer"
//	orphan chunk (no buffer)   400  "orphaned request chunk without active reassembly buffer"
//	rpcPath mismatch           400  "mismatched rpcPath on %s chunk"
//	body arm mismatch          400  "payload arm mismatch in chunk stream"
//	per-request body limit     413  "cumulative size %d exceeds limit %d"
//	too many chunked requests  429  "too many concurrent chunked requests for session"
//	instance buffer capacity   507  "instance reassembly buffer capacity exceeded"
//
// The reference has **no per-session byte limit**: a session's buffered bytes
// count against the instance-wide budget, and the session's own guard is the
// number of concurrent chunked requests.
const (
	// MaxChunkBytes is the per-request cumulative body limit.
	//
	// 【反编译】 requestChunkReassembler.appendChunk (0x10259e630) compares the
	// reassembler's byte counter plus the incoming chunk against 0x400001 and
	// answers "cumulative size %d exceeds limit %d" (HTTP 413).
	MaxChunkBytes = 4 << 20
	// MaxInstanceChunkBytes is the instance-wide buffered body limit.
	//
	// 【反编译】 handleWebRTCMessage (0x10259ed10) compares the instance's byte
	// counter plus the incoming chunk against 0x4000000 (64 MiB) and answers
	// "instance reassembly buffer capacity exceeded" (HTTP 507).
	MaxInstanceChunkBytes = 64 << 20
	// MaxSessionChunkRequests is how many chunked requests a session may hold
	// at once.
	//
	// 【反编译】 the session's reassembly map is read with
	// runtime_mapaccess1_faststr and the frame is rejected with HTTP 429 when
	// the map holds more than 0x1f (31) entries.
	MaxSessionChunkRequests = 32
	// ChunkReassemblyTimeout is how long an idle chunk buffer survives.
	//
	// 【反编译】 purgeStaleReassemblies (0x1025ad750) compares the buffer's
	// age against fifteen seconds (0x37E11D600) and answers "chunk reassembly
	// timed out".
	ChunkReassemblyTimeout = 15 * time.Second
	// ChunkCleanerInterval is the cleaner's tick.
	//
	// 【反编译】 startReassemblyCleaner (0x1025ad640) ticks every five seconds
	// (0x12A05F200).
	ChunkCleanerInterval = 5 * time.Second
)

// BodyArm names the member of the request body oneof a frame carried.
//
// 【反编译】 the reference's ingress frame has two body arms — the bytes
// (0x109c766c8) and the text (0x109c766a8) — and a chunk stream must keep to
// one arm: "payload arm mismatch in chunk stream" (HTTP 400).
type BodyArm int

const (
	// BodyNone is a frame that carried no body member.
	BodyNone BodyArm = iota
	// BodyBytes is the payload (bytes) arm.
	BodyBytes
	// BodyText is the payloadText (string) arm.
	BodyText
)

// ChunkError is a rejected chunk. The status is the reference's own HTTP
// status for the rejection, so the caller can answer with it.
//
// 【反编译】 every rejection handleWebRTCMessage (0x10259ed10) answers with
// is a ConnectInstanceRequestV2 proto-JSON frame carrying the status and the
// reference's own message.
type ChunkError struct {
	// RequestID is the request the rejection belongs to. It is empty when the
	// frame carried no usable request id.
	RequestID string
	Status    int
	Message   string
}

func (e *ChunkError) Error() string { return e.Message }

func chunkError(requestID string, status int, format string, args ...any) *ChunkError {
	return &ChunkError{RequestID: requestID, Status: status, Message: fmt.Sprintf(format, args...)}
}

// CheckRequestID validates a request id the way the reference does before it
// touches the reassembly buffers.
//
// 【反编译】 handleWebRTCMessage (0x10259ed10) answers a non-positive request
// id with HTTP 400 and "request_id must be a positive integer" ("%s Rejecting
// ingress message on session %s: non-positive request_id %d").
//
// A request id that is not an integer never reaches this check: protojson
// refuses to decode it, so the frame is an unparseable ingress message and
// the reference drops it.
func CheckRequestID(requestID string) *ChunkError {
	id, err := strconv.ParseInt(requestID, 10, 64)
	if err != nil {
		return nil
	}
	if id <= 0 {
		return chunkError(requestID, 400, "request_id must be a positive integer")
	}
	return nil
}

// ChunkFrame is one frame of a proxy request.
//
// 【反编译】 the ingress frame is api.proto's ProxyRequestV2: request_id,
// rpc_path, headers, one body arm and is_final. A frame with is_final absent
// or true is a whole request; a frame with is_final false is one chunk of a
// larger body.
type ChunkFrame struct {
	Request *ProxyRequest
	// Arm is the body member the frame carried.
	Arm BodyArm
	// Final is the frame's is_final member. An absent is_final means true.
	Final bool
}

// DecodeChunk decodes one ingress frame, chunked or not.
func DecodeChunk(frame []byte) (*ChunkFrame, error) {
	if len(frame) > MaxFrameBytes {
		return nil, errors.New("rcmesh: incoming frame too large")
	}
	var envelope RequestEnvelope
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("rcmesh: invalid proxy frame: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("rcmesh: trailing data in proxy frame")
	}
	req := envelope.ProxyRequest
	if req == nil {
		return nil, errors.New("rcmesh: missing proxy request")
	}
	if _, err := strconv.ParseInt(req.RequestID, 10, 64); err != nil {
		return nil, errors.New("rcmesh: invalid request ID")
	}
	var raw struct {
		ProxyRequest map[string]json.RawMessage `json:"proxyRequest"`
	}
	_ = json.Unmarshal(frame, &raw) // The typed decode above already validated JSON.
	payload, hasPayload := raw.ProxyRequest["payload"]
	text, hasText := raw.ProxyRequest["payloadText"]
	if hasPayload && hasText && string(payload) != "null" && string(text) != "null" {
		return nil, errors.New("rcmesh: multiple proxy body oneof members")
	}
	arm := BodyNone
	switch {
	case hasPayload && string(payload) != "null":
		arm = BodyBytes
	case hasText && string(text) != "null":
		arm = BodyText
	}
	final := true
	if req.IsFinal != nil {
		final = *req.IsFinal
	}
	return &ChunkFrame{Request: req, Arm: arm, Final: final}, nil
}

// chunkBuffer is one request's buffered body.
//
// 【反编译】 requestChunkReassembler (0x10259e630) keeps the request's
// identity, the body chunks and the arm they arrived in, plus the byte
// counter the cumulative limit is checked against.
type chunkBuffer struct {
	requestID string
	rpcPath   string
	headers   map[string]string
	arm       BodyArm
	armKnown  bool
	body      []byte
	text      strings.Builder
	total     int
	updated   time.Time
}

// sessionChunks holds one session's in-flight chunked requests.
type sessionChunks struct {
	requests map[string]*chunkBuffer
	bytes    int
}

// ChunkReassembler folds the frames of a chunked request back into one.
//
// 【反编译】 the reference's dataChannelReassemblyManager keeps one buffer per
// (session, request) pair, counts the session's concurrent chunked requests,
// enforces an instance-wide byte budget, and drops a buffer no frame touched
// for fifteen seconds (dataChannelReassemblyManager.purgeStaleReassemblies,
// 0x1025ad750).
//
// One reassembler serves one instance: the instance-wide byte budget only
// makes sense when every session of the instance shares it.
type ChunkReassembler struct {
	mu       sync.Mutex
	sessions map[string]*sessionChunks
	instance int
	now      func() time.Time
}

// NewChunkReassembler returns an empty reassembler.
func NewChunkReassembler() *ChunkReassembler {
	return &ChunkReassembler{sessions: map[string]*sessionChunks{}, now: time.Now}
}

// Append folds one frame into the reassembler.
//
// It returns the assembled request when the frame completes one, and nil when
// the request still waits for more chunks. A frame that carries an rpcPath and
// no buffered counterpart is a whole request and is returned unchanged.
func (r *ChunkReassembler) Append(sessionID string, frame []byte) (*ProxyRequest, error) {
	decoded, err := DecodeChunk(frame)
	if err != nil {
		return nil, err
	}
	return r.AppendFrame(sessionID, decoded)
}

// AppendFrame folds one decoded frame into the reassembler.
//
// 【反编译】 handleWebRTCMessage (0x10259ed10) resolves a frame against the
// session's reassembly buffers:
//
//   - a frame that carries no active buffer and no rpcPath is an orphan
//     (HTTP 400, "orphaned request chunk without active reassembly buffer");
//     the reference logs "Rejecting orphaned terminal chunk with empty rpcPath"
//     for a final frame and "Rejecting orphaned continuation chunk with empty
//     rpcPath" for a non-final one.
//   - a frame that carries no active buffer and a rpcPath either is a whole
//     request (is_final true) or starts a chunked request (is_final false).
//     A session that already holds 0x1f (31) chunked requests answers HTTP
//     429 ("too many concurrent chunked requests for session").
//   - a frame that carries an active buffer must keep the buffer's rpcPath
//     (HTTP 400, "mismatched rpcPath on %s chunk") and its body arm (HTTP 400,
//     "payload arm mismatch in chunk stream").
//
// The reference drops the request's buffer before it answers a rejected chunk,
// so the clone does the same.
func (r *ChunkReassembler) AppendFrame(sessionID string, frame *ChunkFrame) (*ProxyRequest, error) {
	if frame == nil || frame.Request == nil {
		return nil, errors.New("rcmesh: missing proxy request")
	}
	req := frame.Request
	if _, err := strconv.ParseInt(req.RequestID, 10, 64); err != nil {
		// A request id that is not an integer never reaches the reference's
		// reassembly: protojson refuses the frame, so the frame is an
		// unparseable ingress message and the reference drops it.
		return nil, errors.New("rcmesh: invalid request ID")
	}
	if rejected := CheckRequestID(req.RequestID); rejected != nil {
		return nil, rejected
	}
	length := frameLength(frame)
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	var buffer *chunkBuffer
	if session != nil {
		buffer = session.requests[req.RequestID]
	}
	if buffer == nil {
		if req.RPCPath == "" {
			// 【反编译】 "orphaned request chunk without active reassembly
			// buffer" (HTTP 400).
			return nil, chunkError(req.RequestID, 400, "orphaned request chunk without active reassembly buffer")
		}
		if frame.Final {
			// A whole request: no buffered counterpart to fold it into.
			return req, nil
		}
		if session == nil {
			session = &sessionChunks{requests: map[string]*chunkBuffer{}}
			r.sessions[sessionID] = session
		}
		if len(session.requests) >= MaxSessionChunkRequests {
			// 【反编译】 "too many concurrent chunked requests for session"
			// (HTTP 429).
			return nil, chunkError(req.RequestID, 429, "too many concurrent chunked requests for session")
		}
		if r.instance+length > MaxInstanceChunkBytes {
			// 【反编译】 "instance reassembly buffer capacity exceeded"
			// (HTTP 507).
			return nil, chunkError(req.RequestID, 507, "instance reassembly buffer capacity exceeded")
		}
		buffer = &chunkBuffer{
			requestID: req.RequestID,
			rpcPath:   req.RPCPath,
			headers:   req.Headers,
			updated:   r.now(),
		}
		session.requests[req.RequestID] = buffer
	} else if req.RPCPath != "" && req.RPCPath != buffer.rpcPath {
		// 【反编译】 "mismatched rpcPath on %s chunk" (HTTP 400).
		r.abortLocked(sessionID, session, req.RequestID)
		return nil, chunkError(req.RequestID, 400, "mismatched rpcPath on %s chunk", buffer.rpcPath)
	}
	if r.instance+length > MaxInstanceChunkBytes {
		// 【反编译】 "instance reassembly buffer capacity exceeded" (HTTP 507).
		r.abortLocked(sessionID, session, req.RequestID)
		return nil, chunkError(req.RequestID, 507, "instance reassembly buffer capacity exceeded")
	}
	if frame.Arm != BodyNone {
		if buffer.armKnown && buffer.arm != frame.Arm {
			// 【反编译】 "payload arm mismatch in chunk stream" (HTTP 400).
			r.abortLocked(sessionID, session, req.RequestID)
			return nil, chunkError(req.RequestID, 400, "payload arm mismatch in chunk stream")
		}
		buffer.arm = frame.Arm
		buffer.armKnown = true
	}
	if buffer.total+length > MaxChunkBytes {
		// 【反编译】 "cumulative size %d exceeds limit %d" (HTTP 413).
		r.abortLocked(sessionID, session, req.RequestID)
		return nil, chunkError(req.RequestID, 413, "cumulative size %d exceeds limit %d", buffer.total+length, MaxChunkBytes)
	}
	switch frame.Arm {
	case BodyBytes:
		buffer.body = append(buffer.body, req.Payload...)
	case BodyText:
		buffer.text.WriteString(req.PayloadText)
	}
	buffer.total += length
	session.bytes += length
	r.instance += length
	buffer.updated = r.now()
	if !frame.Final {
		return nil, nil
	}
	assembled := &ProxyRequest{
		RequestID: buffer.requestID,
		RPCPath:   buffer.rpcPath,
		Headers:   buffer.headers,
		IsFinal:   boolPtr(true),
	}
	switch buffer.arm {
	case BodyBytes:
		assembled.Payload = buffer.body
	case BodyText:
		assembled.PayloadText = buffer.text.String()
	}
	r.abortLocked(sessionID, session, req.RequestID)
	return assembled, nil
}

// frameLength is the number of body bytes a frame carries.
func frameLength(frame *ChunkFrame) int {
	switch frame.Arm {
	case BodyBytes:
		return len(frame.Request.Payload)
	case BodyText:
		return len(frame.Request.PayloadText)
	}
	return 0
}

// Abort drops a request's buffer — the reference drops the chunks of a
// request the caller cancelled ("%s Dropping a chunk for aborted request %d on
// session %s").
func (r *ChunkReassembler) Abort(sessionID, requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.abortLocked(sessionID, r.sessions[sessionID], requestID)
}

// abortLocked drops a request's buffer and returns its bytes to the budgets.
// The caller holds the lock.
func (r *ChunkReassembler) abortLocked(sessionID string, session *sessionChunks, requestID string) {
	if session == nil {
		return
	}
	buffer := session.requests[requestID]
	if buffer == nil {
		return
	}
	delete(session.requests, requestID)
	session.bytes -= buffer.total
	r.instance -= buffer.total
	if session.bytes < 0 {
		session.bytes = 0
	}
	if r.instance < 0 {
		r.instance = 0
	}
	if len(session.requests) == 0 {
		delete(r.sessions, sessionID)
	}
}

// Purge drops the buffers no frame touched for longer than the timeout. The
// reference's own words are "chunk reassembly timed out".
func (r *ChunkReassembler) Purge(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for sessionID, session := range r.sessions {
		for requestID, buffer := range session.requests {
			if now.Sub(buffer.updated) < ChunkReassemblyTimeout {
				continue
			}
			r.abortLocked(sessionID, session, requestID)
		}
	}
	if r.instance < 0 {
		r.instance = 0
	}
}

// StartCleaner runs the purge loop until the context is cancelled.
//
// 【反编译】 the reference starts the cleaner with the daemon
// (startReassemblyCleaner, 0x1025ad640) and ticks every five seconds.
func (r *ChunkReassembler) StartCleaner(stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(ChunkCleanerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.Purge(time.Now())
			}
		}
	}()
}

// Pending reports how many chunked requests a session holds.
func (r *ChunkReassembler) Pending(sessionID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	if session == nil {
		return 0
	}
	return len(session.requests)
}

func boolPtr(v bool) *bool { return &v }
