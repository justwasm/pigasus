package rcmesh

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"
)

// chunkFrame builds one ingress frame the way the browser sends it: the first
// chunk of a request carries the rpcPath, the continuations carry none.
func chunkFrame(id, rpcPath, body string, arm BodyArm, final bool) *ChunkFrame {
	req := &ProxyRequest{RequestID: id, RPCPath: rpcPath}
	if arm == BodyBytes {
		req.Payload = []byte(body)
	} else if arm == BodyText {
		req.PayloadText = body
	}
	return &ChunkFrame{Request: req, Arm: arm, Final: final}
}

func TestChunkReassemblerSingleFramePassThrough(t *testing.T) {
	r := NewChunkReassembler()
	frame := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "1", RPCPath: "/test", PayloadText: "hello"},
		Arm:     BodyText,
		Final:   true,
	}
	res, err := r.AppendFrame("s1", frame)
	if err != nil {
		t.Fatal(err)
	}
	if res.RequestID != "1" || res.PayloadText != "hello" {
		t.Fatalf("unexpected res: %+v", res)
	}
	if r.Pending("s1") != 0 {
		t.Fatalf("pending = %d, want 0", r.Pending("s1"))
	}
}

func TestChunkReassemblerTextChunks(t *testing.T) {
	r := NewChunkReassembler()
	f1 := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "1", RPCPath: "/test", PayloadText: "hello "},
		Arm:     BodyText,
		Final:   false,
	}
	f2 := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "1", PayloadText: "world"},
		Arm:     BodyText,
		Final:   true,
	}
	res1, err := r.AppendFrame("s1", f1)
	if err != nil || res1 != nil {
		t.Fatalf("res1=%v, err=%v", res1, err)
	}
	if r.Pending("s1") != 1 {
		t.Fatalf("pending = %d, want 1", r.Pending("s1"))
	}
	res2, err := r.AppendFrame("s1", f2)
	if err != nil || res2 == nil {
		t.Fatalf("res2=%v, err=%v", res2, err)
	}
	if res2.PayloadText != "hello world" {
		t.Fatalf("payloadText = %q, want 'hello world'", res2.PayloadText)
	}
	if res2.IsFinal == nil || !*res2.IsFinal {
		t.Errorf("isFinal = %v, want true", res2.IsFinal)
	}
	if r.Pending("s1") != 0 {
		t.Fatalf("pending = %d, want 0", r.Pending("s1"))
	}
}

func TestChunkReassemblerByteChunks(t *testing.T) {
	r := NewChunkReassembler()
	f1 := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "2", RPCPath: "/upload", Payload: []byte("part1-")},
		Arm:     BodyBytes,
		Final:   false,
	}
	f2 := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "2", Payload: []byte("part2")},
		Arm:     BodyBytes,
		Final:   true,
	}
	_, _ = r.AppendFrame("s1", f1)
	res, err := r.AppendFrame("s1", f2)
	if err != nil || res == nil {
		t.Fatalf("res=%v, err=%v", res, err)
	}
	if !bytes.Equal(res.Payload, []byte("part1-part2")) {
		t.Fatalf("payload = %s, want 'part1-part2'", string(res.Payload))
	}
}

// 【反编译】 a chunk that names no active buffer and carries no rpcPath is an
// orphan: HTTP 400, "orphaned request chunk without active reassembly buffer".
func TestChunkReassemblerOrphanChunk(t *testing.T) {
	r := NewChunkReassembler()
	for _, final := range []bool{false, true} {
		_, err := r.AppendFrame("s1", chunkFrame("7", "", "orphan", BodyText, final))
		chkErr, ok := err.(*ChunkError)
		if !ok {
			t.Fatalf("final=%v: unexpected error: %v", final, err)
		}
		if chkErr.Status != 400 || chkErr.Message != "orphaned request chunk without active reassembly buffer" {
			t.Fatalf("final=%v: unexpected error: %+v", final, chkErr)
		}
		if chkErr.RequestID != "7" {
			t.Fatalf("final=%v: requestID = %q, want '7'", final, chkErr.RequestID)
		}
	}
	if r.Pending("s1") != 0 {
		t.Fatalf("pending = %d, want 0", r.Pending("s1"))
	}
}

// 【反编译】 a non-positive request id is answered with HTTP 400 and
// "request_id must be a positive integer".
func TestChunkReassemblerNonPositiveRequestID(t *testing.T) {
	r := NewChunkReassembler()
	for _, id := range []string{"0", "-1"} {
		_, err := r.AppendFrame("s1", chunkFrame(id, "/test", "body", BodyText, true))
		chkErr, ok := err.(*ChunkError)
		if !ok {
			t.Fatalf("id=%s: unexpected error: %v", id, err)
		}
		if chkErr.Status != 400 || chkErr.Message != "request_id must be a positive integer" {
			t.Fatalf("id=%s: unexpected error: %+v", id, chkErr)
		}
	}
}

// 【反编译】 "mismatched rpcPath on %s chunk" (HTTP 400).
func TestChunkReassemblerRPCPathMismatch(t *testing.T) {
	r := NewChunkReassembler()
	_, _ = r.AppendFrame("s1", chunkFrame("8", "/one", "a", BodyText, false))
	_, err := r.AppendFrame("s1", chunkFrame("8", "/two", "b", BodyText, false))
	chkErr, ok := err.(*ChunkError)
	if !ok {
		t.Fatalf("unexpected error: %v", err)
	}
	if chkErr.Status != 400 || chkErr.Message != "mismatched rpcPath on /one chunk" {
		t.Fatalf("unexpected error: %+v", chkErr)
	}
	// The reference drops the request's buffer before it answers.
	if r.Pending("s1") != 0 {
		t.Fatalf("pending = %d, want 0", r.Pending("s1"))
	}
}

func TestChunkReassemblerArmMismatch(t *testing.T) {
	r := NewChunkReassembler()
	f1 := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "3", RPCPath: "/test", PayloadText: "text"},
		Arm:     BodyText,
		Final:   false,
	}
	f2 := &ChunkFrame{
		Request: &ProxyRequest{RequestID: "3", Payload: []byte("bytes")},
		Arm:     BodyBytes,
		Final:   true,
	}
	_, _ = r.AppendFrame("s1", f1)
	_, err := r.AppendFrame("s1", f2)
	if err == nil {
		t.Fatal("expected error on arm mismatch")
	}
	chkErr, ok := err.(*ChunkError)
	if !ok || chkErr.Status != 400 || !strings.Contains(chkErr.Message, "payload arm mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChunkReassemblerCumulativeLimit(t *testing.T) {
	r := NewChunkReassembler()
	big := strings.Repeat("A", 2<<20)
	f1 := chunkFrame("4", "/test", big, BodyText, false)
	f2 := chunkFrame("4", "", big, BodyText, false)
	f3 := chunkFrame("4", "", "overflow", BodyText, false)

	_, _ = r.AppendFrame("s1", f1)
	_, _ = r.AppendFrame("s1", f2)
	_, err := r.AppendFrame("s1", f3)
	if err == nil {
		t.Fatal("expected error on cumulative limit exceed")
	}
	chkErr, ok := err.(*ChunkError)
	if !ok || chkErr.Status != 413 || !strings.Contains(chkErr.Message, "exceeds limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 【反编译】 the instance's byte budget answers HTTP 507 and
// "instance reassembly buffer capacity exceeded" — not 413.
func TestChunkReassemblerInstanceCapacity(t *testing.T) {
	r := NewChunkReassembler()
	r.instance = MaxInstanceChunkBytes - 1
	_, err := r.AppendFrame("s1", chunkFrame("9", "/test", "ab", BodyText, false))
	chkErr, ok := err.(*ChunkError)
	if !ok {
		t.Fatalf("unexpected error: %v", err)
	}
	if chkErr.Status != 507 || chkErr.Message != "instance reassembly buffer capacity exceeded" {
		t.Fatalf("unexpected error: %+v", chkErr)
	}
	if r.Pending("s1") != 0 {
		t.Fatalf("pending = %d, want 0", r.Pending("s1"))
	}
}

func TestChunkReassemblerMaxSessionRequests(t *testing.T) {
	r := NewChunkReassembler()
	for i := 0; i < MaxSessionChunkRequests; i++ {
		id := strconv.Itoa(i + 1)
		f := chunkFrame(id, "/test", "chunk", BodyText, false)
		_, err := r.AppendFrame("s1", f)
		if err != nil {
			t.Fatalf("unexpected error at %d: %v", i, err)
		}
	}
	// 33rd request exceeds limit
	overflow := chunkFrame("99", "/test", "chunk", BodyText, false)
	_, err := r.AppendFrame("s1", overflow)
	if err == nil {
		t.Fatal("expected 429 error on too many concurrent requests")
	}
	chkErr, ok := err.(*ChunkError)
	if !ok || chkErr.Status != 429 {
		t.Fatalf("unexpected error: %v", err)
	}
	if chkErr.Message != "too many concurrent chunked requests for session" {
		t.Fatalf("unexpected error: %+v", chkErr)
	}
}

func TestChunkReassemblerAbortAndPurge(t *testing.T) {
	mockTime := time.Now()
	r := &ChunkReassembler{
		sessions: map[string]*sessionChunks{},
		now:      func() time.Time { return mockTime },
	}

	f1 := chunkFrame("10", "/test", "c1", BodyText, false)
	f2 := chunkFrame("20", "/test", "c2", BodyText, false)
	_, _ = r.AppendFrame("s1", f1)
	_, _ = r.AppendFrame("s1", f2)

	if r.Pending("s1") != 2 {
		t.Fatalf("pending = %d, want 2", r.Pending("s1"))
	}

	// Abort request 10
	r.Abort("s1", "10")
	if r.Pending("s1") != 1 {
		t.Fatalf("pending = %d, want 1", r.Pending("s1"))
	}

	// Advance time past timeout and purge
	mockTime = mockTime.Add(ChunkReassemblyTimeout + time.Second)
	r.Purge(mockTime)
	if r.Pending("s1") != 0 {
		t.Fatalf("pending after purge = %d, want 0", r.Pending("s1"))
	}
}

// The reassembler is scoped by session: two sessions may hold the same
// request id without colliding.
func TestChunkReassemblerSessionScope(t *testing.T) {
	r := NewChunkReassembler()
	_, _ = r.AppendFrame("s1", chunkFrame("30", "/test", "a", BodyText, false))
	if r.Pending("s1") != 1 || r.Pending("s2") != 0 {
		t.Fatalf("pending s1=%d s2=%d, want 1 and 0", r.Pending("s1"), r.Pending("s2"))
	}
	// The same request id in another session is a whole request there: it has
	// no active buffer and carries no rpcPath, so it is an orphan.
	_, err := r.AppendFrame("s2", chunkFrame("30", "", "b", BodyText, false))
	if err == nil {
		t.Fatal("expected an orphan rejection in the other session")
	}
}
