package agyproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// Responses over a WebSocket, as OpenAI Codex CLI speaks it to its provider.
// Codex opens a single socket per session and sends each turn as a text frame:
// {"type":"response.create", ...}.
// The server streams each SSE event back as one text frame on the WebSocket.
// Successive turns reference `previous_response_id` and send only incremental input.
//
// Ref: yetone/magpie internal/gateway/responses_ws.go

func wantsSocket(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

const (
	socketKeep   = 8
	socketErrMax = 1 << 20
)

type clientSockets struct {
	mu      sync.Mutex
	m       map[*responsesSocket]bool
	closing bool
	done    chan struct{}
}

func (cs *clientSockets) add(s *responsesSocket) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.closing {
		return false
	}
	if cs.m == nil {
		cs.m = map[*responsesSocket]bool{}
	}
	cs.m[s] = true
	return true
}

func (cs *clientSockets) ending() bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.closing
}

func (cs *clientSockets) remove(s *responsesSocket) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.m, s)
	if cs.closing && len(cs.m) == 0 && cs.done != nil {
		close(cs.done)
		cs.done = nil
	}
}

func (cs *clientSockets) drain(ctx context.Context) {
	cs.mu.Lock()
	cs.closing = true
	var done chan struct{}
	if len(cs.m) > 0 {
		done = make(chan struct{})
		cs.done = done
	}
	all := make([]*responsesSocket, 0, len(cs.m))
	for s := range cs.m {
		all = append(all, s)
	}
	cs.mu.Unlock()

	for _, s := range all {
		s.goingAway()
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
		for _, s := range all {
			s.c.CloseNow()
		}
	}
}

type heldTurnWS struct {
	id    string
	items []json.RawMessage
}

type responsesSocket struct {
	s    *Server
	req  *http.Request
	path string
	c    *websocket.Conn
	ctx  context.Context
	live context.Context

	writeMu sync.Mutex
	turns   []heldTurnWS

	mu      sync.Mutex
	away    bool
	turning bool

	frames    int
	continued int
	got       int64
	sent      int64
}

func (s *Server) serveResponsesSocket(w http.ResponseWriter, r *http.Request) bool {
	if s.sockets.ending() {
		http.Error(w, "proxy is restarting", http.StatusServiceUnavailable)
		return true
	}

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		CompressionMode:    websocket.CompressionNoContextTakeover,
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logf("responses socket from %s error: %v", r.RemoteAddr, err)
		return true
	}
	c.SetReadLimit(maxRequestBody)

	sock := &responsesSocket{
		s:    s,
		req:  r.Clone(r.Context()),
		path: r.URL.Path,
		c:    c,
		ctx:  r.Context(),
	}
	if !s.sockets.add(sock) {
		_ = c.Close(websocket.StatusGoingAway, "proxy is restarting")
		return true
	}
	defer s.sockets.remove(sock)

	sock.serve()
	return true
}

func (rs *responsesSocket) goingAway() {
	rs.mu.Lock()
	rs.away = true
	idle := !rs.turning
	rs.mu.Unlock()
	if idle {
		go rs.c.Close(websocket.StatusGoingAway, "proxy is restarting")
	}
}

type socketMsg struct {
	data []byte
	err  error
}

func (rs *responsesSocket) serve() {
	ctx, cancel := context.WithCancel(rs.ctx)
	defer cancel()
	defer rs.c.CloseNow()
	rs.live = ctx

	in := make(chan socketMsg)
	go func() {
		for {
			typ, data, err := rs.c.Read(ctx)
			if err == nil && typ != websocket.MessageText {
				err = errors.New("binary frame unsupported")
				_ = rs.c.Close(websocket.StatusUnsupportedData, "text frames only")
			}
			select {
			case in <- socketMsg{data, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	var queued []socketMsg
	for {
		var m socketMsg
		if len(queued) > 0 {
			m, queued = queued[0], queued[1:]
		} else {
			select {
			case m = <-in:
			case <-ctx.Done():
				return
			}
		}
		if m.err != nil {
			return
		}

		kind := gjson.GetBytes(m.data, "type").String()
		switch kind {
		case "response.create":
		case "response.interrupt":
			_ = rs.write(ctx, interruptReply("response.interrupt.failed", gjson.GetBytes(m.data, "response_id").String()))
			continue
		default:
			_ = rs.writeError(ctx, http.StatusBadRequest, "invalid_request_error", "unsupported_message",
				fmt.Sprintf("proxy serves response.create on this socket, not %q", kind), "type")
			continue
		}

		rs.mu.Lock()
		rs.turning = true
		rs.mu.Unlock()

		turnCtx, stop := context.WithCancel(ctx)
		t := &socketTurnRun{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			rs.turn(turnCtx, t, m.data)
		}()

		for waiting := true; waiting; {
			select {
			case <-done:
				waiting = false
			case n := <-in:
				if n.err != nil {
					stop()
					<-done
					return
				}
				if gjson.GetBytes(n.data, "type").String() != "response.interrupt" {
					queued = append(queued, n)
					continue
				}
				id := gjson.GetBytes(n.data, "response_id").String()
				if !t.interrupt(id) {
					_ = rs.write(ctx, interruptReply("response.interrupt.failed", id))
					continue
				}
				stop()
				<-done
				waiting = false
				_ = rs.write(ctx, interruptReply("response.interrupt.accepted", id))
				end, _ := json.Marshal(map[string]any{
					"type": "response.incomplete",
					"response": map[string]any{
						"id":                 id,
						"object":             "response",
						"status":             "incomplete",
						"incomplete_details": map[string]any{"reason": "interrupted"},
						"output":             []any{},
					},
				})
				_ = rs.write(ctx, end)
			}
		}
		stop()

		rs.mu.Lock()
		rs.turning = false
		away := rs.away
		rs.mu.Unlock()

		if t.cut {
			_ = rs.c.Close(websocket.StatusInternalError, "reply ended before response.completed")
			return
		}
		if away {
			_ = rs.c.Close(websocket.StatusGoingAway, "proxy is restarting")
			return
		}
	}
}

func interruptReply(kind, id string) []byte {
	b, _ := json.Marshal(map[string]string{"type": kind, "response_id": id})
	return b
}

func (rs *responsesSocket) write(ctx context.Context, frame []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rs.writeMu.Lock()
	defer rs.writeMu.Unlock()
	return rs.c.Write(rs.live, websocket.MessageText, frame)
}

func (rs *responsesSocket) writeError(ctx context.Context, status int, typ, code, msg, param string) error {
	e := map[string]any{"type": typ, "code": code, "message": msg}
	if param != "" {
		e["param"] = param
	}
	b, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": e})
	return rs.write(ctx, b)
}

type socketTurnRun struct {
	mu        sync.Mutex
	ids       []string
	cut       bool
	cancelled bool
}

func (t *socketTurnRun) interrupt(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, have := range t.ids {
		if have == id {
			t.cancelled = true
			return true
		}
	}
	return false
}

func (rs *responsesSocket) held(id string) ([]json.RawMessage, bool) {
	for i := len(rs.turns) - 1; i >= 0; i-- {
		if rs.turns[i].id == id {
			return rs.turns[i].items, true
		}
	}
	return nil, false
}

func (rs *responsesSocket) keep(id string, prior, fresh, reply []json.RawMessage) {
	t := heldTurnWS{id: id, items: make([]json.RawMessage, 0, len(prior)+len(fresh)+len(reply))}
	t.items = append(t.items, prior...)
	t.items = append(t.items, fresh...)
	t.items = append(t.items, reply...)
	rs.turns = append(rs.turns, t)
	if len(rs.turns) > socketKeep {
		rs.turns = append(rs.turns[:0:0], rs.turns[len(rs.turns)-socketKeep:]...)
	}
	globalPrevStore.put(id, t.items)
}

func (rs *responsesSocket) turn(ctx context.Context, t *socketTurnRun, frame []byte) {
	rs.frames++
	rs.got += int64(len(frame))

	var q map[string]json.RawMessage
	if json.Unmarshal(frame, &q) != nil {
		_ = rs.writeError(ctx, http.StatusBadRequest, "invalid_request_error", "invalid_json", "response.create is not a JSON object", "")
		return
	}

	fresh := parseRawInput(q["input"])
	if fresh == nil {
		_ = rs.writeError(ctx, http.StatusBadRequest, "invalid_request_error", "invalid_value", "input is neither text nor a list of items", "input")
		return
	}
	input := fresh

	var held []json.RawMessage
	if raw := q["previous_response_id"]; len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var id string
		_ = json.Unmarshal(raw, &id)
		prior, ok := rs.held(id)
		if !ok {
			if gPrior, gOk := globalPrevStore.get(id); gOk {
				prior, ok = gPrior, true
			}
		}
		if !ok {
			_ = rs.writeError(ctx, http.StatusBadRequest, "invalid_request_error", "previous_response_not_found",
				"Previous response with id '"+id+"' not found.", "previous_response_id")
			return
		}
		held = prior
		input = append(append([]json.RawMessage(nil), prior...), fresh...)
		rs.continued++
	}

	// Warmup handling (generate: false): Codex sends this ahead of its first turn
	var generate bool
	if raw := q["generate"]; len(raw) > 0 && json.Unmarshal(raw, &generate) == nil && !generate {
		id := "resp_" + newUUIDv4()
		t.mu.Lock()
		t.ids = append(t.ids, id)
		t.mu.Unlock()

		resp := map[string]any{
			"id":         id,
			"object":     "response",
			"created_at": time.Now().Unix(),
			"status":     "in_progress",
			"output":     []any{},
		}
		if m := q["model"]; len(m) > 0 {
			resp["model"] = m
		}
		b, _ := json.Marshal(map[string]any{"type": "response.created", "response": resp})
		if rs.write(ctx, b) != nil {
			return
		}
		resp["status"] = "completed"
		resp["usage"] = map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
		b, _ = json.Marshal(map[string]any{"type": "response.completed", "response": resp})
		if rs.write(ctx, b) == nil {
			rs.keep(id, held, fresh, nil)
		}
		return
	}

	delete(q, "type")
	delete(q, "previous_response_id")
	delete(q, "generate")

	inBytes, err := json.Marshal(input)
	if err != nil {
		_ = rs.writeError(ctx, http.StatusBadRequest, "invalid_request_error", "invalid_json", err.Error(), "")
		return
	}
	q["input"] = inBytes

	body, err := json.Marshal(q)
	if err != nil {
		_ = rs.writeError(ctx, http.StatusBadRequest, "invalid_request_error", "invalid_json", err.Error(), "")
		return
	}
	rs.sent += int64(len(body))

	req := rs.request(ctx, body)
	out := &socketTurnWriter{rs: rs, ctx: ctx, run: t, h: http.Header{}}
	rs.s.handleResponses(out, req, body)
	out.finish()

	if ctx.Err() != nil {
		return
	}
	if !out.ended {
		t.cut = true
		return
	}
	if out.id != "" && out.completed {
		rs.keep(out.id, held, fresh, out.items)
	}
}

func parseRawInput(raw json.RawMessage) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return nil
		}
		m, _ := json.Marshal(map[string]any{"type": "message", "role": "user", "content": s})
		return []json.RawMessage{m}
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return nil
		}
		return items
	}
	return nil
}

func hopHeader(h string) bool {
	switch http.CanonicalHeaderKey(h) {
	case "Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func (rs *responsesSocket) request(ctx context.Context, body []byte) *http.Request {
	o := rs.req
	req := o.Clone(ctx)
	req.Method = http.MethodPost
	req.Header = http.Header{}
	for k, vs := range o.Header {
		ck := http.CanonicalHeaderKey(k)
		if hopHeader(ck) || strings.HasPrefix(ck, "Sec-Websocket-") {
			continue
		}
		switch ck {
		case "Content-Type", "Content-Encoding", "Accept", "Accept-Encoding":
			continue
		case "Openai-Beta":
			var keep []string
			for _, v := range vs {
				if !strings.Contains(v, "responses_websockets") {
					keep = append(keep, v)
				}
			}
			if len(keep) > 0 {
				req.Header[ck] = keep
			}
			continue
		}
		req.Header[ck] = append([]string(nil), vs...)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if v := gjson.GetBytes(body, `client_metadata.x-codex-turn-state`).String(); v != "" {
		req.Header.Set("X-Codex-Turn-State", v)
	}
	if strings.EqualFold(gjson.GetBytes(body, `client_metadata.ws_request_header_x_openai_internal_codex_responses_lite`).String(), "true") {
		req.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", "true")
	}

	req.Body = io.NopCloser(bytes.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.RequestURI = ""
	return req
}

type socketTurnWriter struct {
	rs  *responsesSocket
	ctx context.Context
	run *socketTurnRun
	h   http.Header

	status int
	stream bool
	pw     *io.PipeWriter
	read   chan struct{}
	buf    bytes.Buffer

	id               string
	items            []json.RawMessage
	ended, completed bool
}

func (w *socketTurnWriter) Header() http.Header { return w.h }

func (w *socketTurnWriter) WriteHeader(code int) {
	if w.status != 0 || code < 200 {
		return
	}
	w.status = code
	ct := w.h.Get("Content-Type")
	if code/100 == 2 && strings.Contains(ct, "event-stream") {
		w.startStream()
	}
}

func (w *socketTurnWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		if w.h.Get("Content-Type") == "" && (bytes.HasPrefix(b, []byte("event:")) || bytes.HasPrefix(b, []byte("data:"))) {
			w.h.Set("Content-Type", "text/event-stream")
		}
		w.WriteHeader(http.StatusOK)
	}
	if w.stream {
		return w.pw.Write(b)
	}
	if w.buf.Len() < socketErrMax || w.status/100 == 2 {
		w.buf.Write(b)
	}
	return len(b), nil
}

func (w *socketTurnWriter) Flush() {}

func (w *socketTurnWriter) startStream() {
	w.stream = true
	pr, pw := io.Pipe()
	w.pw, w.read = pw, make(chan struct{})

	go func() {
		defer close(w.read)
		err := readSSE(pr, func(_, data string) error {
			data = strings.TrimSpace(data)
			if data == "" || data == "[DONE]" {
				return nil
			}
			return w.event([]byte(data))
		})
		pr.CloseWithError(err)
		if err == nil {
			_, _ = io.Copy(io.Discard, pr)
		}
	}()
}

func (w *socketTurnWriter) event(data []byte) error {
	var e struct {
		Type     string          `json:"type"`
		Response json.RawMessage `json:"response"`
		Item     json.RawMessage `json:"item"`
	}
	if json.Unmarshal(data, &e) != nil {
		return nil
	}

	switch e.Type {
	case "response.created", "response.in_progress":
		w.note(gjson.GetBytes(e.Response, "id").String())
	case "response.output_item.done":
		if len(e.Item) > 0 {
			w.items = append(w.items, append(json.RawMessage(nil), e.Item...))
		}
	case "response.completed":
		w.note(gjson.GetBytes(e.Response, "id").String())
		w.ended = true
		w.completed = true
	case "response.incomplete", "response.failed", "response.cancelled", "error":
		w.ended = true
	}
	return w.send(data)
}

func (w *socketTurnWriter) note(id string) {
	if id == "" {
		return
	}
	w.id = id
	w.run.mu.Lock()
	defer w.run.mu.Unlock()
	for _, have := range w.run.ids {
		if have == id {
			return
		}
	}
	w.run.ids = append(w.run.ids, id)
}

func (w *socketTurnWriter) send(frame []byte) error {
	return w.rs.write(w.ctx, frame)
}

func (w *socketTurnWriter) finish() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	if w.stream {
		_ = w.pw.Close()
		<-w.read
		return
	}
	if w.ctx.Err() != nil {
		return
	}
	if w.status/100 != 2 {
		var errObj map[string]any
		_ = json.Unmarshal(w.buf.Bytes(), &errObj)
		b, _ := json.Marshal(map[string]any{"type": "error", "status": w.status, "error": errObj})
		_ = w.send(b)
		return
	}

	body := bytes.TrimSpace(w.buf.Bytes())
	r := gjson.ParseBytes(body)
	if r.Get("object").String() == "response" && r.Get("id").String() != "" {
		w.note(r.Get("id").String())
		w.ended = true
		w.completed = true
		_ = w.send(body)
	}
}

func readSSE(r io.Reader, fn func(event, data string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	var event string
	var data []string

	flush := func() error {
		if len(data) == 0 {
			event = ""
			return nil
		}
		err := fn(event, strings.Join(data, "\n"))
		event = ""
		data = nil
		return err
	}

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment / keepalive
		}
		if strings.HasPrefix(line, "event:") {
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			continue
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return sc.Err()
}
