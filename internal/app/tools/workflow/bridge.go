package workflow

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

// AgentRequest describes a single-shot or multi-turn agent invocation from a
// workflow script.
type AgentRequest struct {
	AgentID   string          `json:"agent_id"`
	Prompt    string          `json:"prompt"`
	Schema    json.RawMessage `json:"schema"`
	Role      string          `json:"role,omitempty"`
	TypeName  string          `json:"type_name,omitempty"`
	Workspace string          `json:"workspace,omitempty"`
	Model     string          `json:"model,omitempty"`
	TurnIndex int             `json:"turn_index"`
	Close     bool            `json:"close,omitempty"`
}

// AgentExecutor executes or closes a workflow agent.
type AgentExecutor func(ctx context.Context, req AgentRequest) (any, error)

// ToolExecutor executes a tool call requested via call_tool(name, **args).
// When denied is true, the Python runtime raises ToolError(..., denied=True).
type ToolExecutor func(ctx context.Context, name string, args map[string]any) (output string, denied bool, err error)

// TrajectoryFilter filters earlier conversation steps returned by trajectory().
type TrajectoryFilter struct {
	Last     int      `json:"last,omitempty"`
	Types    []string `json:"types,omitempty"`
	Tools    []string `json:"tools,omitempty"`
	MinIndex *int     `json:"min_index,omitempty"`
	MaxIndex *int     `json:"max_index,omitempty"`
}

// TrajectoryStep represents one step returned by trajectory().
type TrajectoryStep struct {
	Index  int            `json:"index"`
	Type   string         `json:"type"`
	Status string         `json:"status"`
	Text   string         `json:"text,omitempty"`
	Tool   string         `json:"tool,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
	Output string         `json:"output,omitempty"`
}

// TrajectoryProvider returns conversation steps prior to the workflow call.
type TrajectoryProvider func(ctx context.Context, filter TrajectoryFilter) ([]TrajectoryStep, error)

// EventKind identifies a streaming workflow progress event.
type EventKind string

const (
	EventPhase      EventKind = "phase"
	EventLog        EventKind = "log"
	EventAgentStart EventKind = "agent_start"
	EventAgentDone  EventKind = "agent_done"
)

// Event is emitted to RunWorkflow.OnEvent as the workflow executes.
type Event struct {
	Kind      EventKind `json:"kind"`
	Phase     string    `json:"phase,omitempty"`
	Message   string    `json:"message,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	Role      string    `json:"role,omitempty"`
	TurnIndex int       `json:"turn_index,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type logEntry struct {
	Phase   string
	Message string
}

type scriptError struct {
	Message   string `json:"message"`
	Line      int    `json:"line"`
	Traceback string `json:"traceback"`
}

type rpcRequest struct {
	Token  string          `json:"token"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Message string `json:"message"`
	Denied  bool   `json:"denied,omitempty"`
}

type rpcResponse struct {
	Result any       `json:"result,omitempty"`
	Error  *rpcError `json:"error,omitempty"`
}

type bridgeServer struct {
	listener net.Listener
	host     string
	port     int
	token    string

	parentCtx context.Context
	agentExec AgentExecutor
	toolExec  ToolExecutor
	trajProv  TrajectoryProvider
	onEvent   func(Event)

	mu        sync.Mutex
	phase     string
	logs      []logEntry
	scriptErr *scriptError
	closed    bool
	wg        sync.WaitGroup
}

func newBridgeServer(
	ctx context.Context,
	agentExec AgentExecutor,
	toolExec ToolExecutor,
	trajProv TrajectoryProvider,
	onEvent func(Event),
) (*bridgeServer, error) {
	tokBytes := make([]byte, 32)
	if _, err := rand.Read(tokBytes); err != nil {
		return nil, fmt.Errorf("generate bridge token: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen workflow bridge: %w", err)
	}
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("parse bridge addr: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("parse bridge port: %w", err)
	}

	b := &bridgeServer{
		listener:  ln,
		host:      host,
		port:      port,
		token:     hex.EncodeToString(tokBytes),
		parentCtx: ctx,
		agentExec: agentExec,
		toolExec:  toolExec,
		trajProv:  trajProv,
		onEvent:   onEvent,
	}
	b.wg.Add(1)
	go b.acceptLoop()
	return b, nil
}

func (b *bridgeServer) acceptLoop() {
	defer b.wg.Done()
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			return
		}
		b.wg.Add(1)
		go func(c net.Conn) {
			defer b.wg.Done()
			b.handleConn(c)
		}(conn)
	}
}

func (b *bridgeServer) close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()
	_ = b.listener.Close()
	b.wg.Wait()
}

func (b *bridgeServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	line = []byte(strings.TrimSpace(string(line)))
	if len(line) == 0 {
		return
	}

	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		b.writeResp(conn, rpcResponse{Error: &rpcError{Message: "invalid rpc request: " + err.Error()}})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(b.token)) != 1 {
		b.writeResp(conn, rpcResponse{Error: &rpcError{Message: "unauthorized"}})
		return
	}

	rpcCtx, rpcCancel := context.WithCancel(b.parentCtx)
	defer rpcCancel()

	// Watch for client disconnect (e.g. asyncio task cancellation in parallel()).
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		_, _ = reader.ReadByte()
		rpcCancel()
	}()

	resp := b.dispatch(rpcCtx, req.Method, req.Params)
	b.writeResp(conn, resp)
	_ = conn.Close()
	<-watchDone
}

func (b *bridgeServer) writeResp(conn net.Conn, resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		data, _ = json.Marshal(rpcResponse{Error: &rpcError{Message: "marshal response: " + err.Error()}})
	}
	data = append(data, '\n')
	_, _ = conn.Write(data)
}

func (b *bridgeServer) emit(ev Event) {
	if b.onEvent != nil {
		b.onEvent(ev)
	}
}

func (b *bridgeServer) currentPhase() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.phase
}

func (b *bridgeServer) dispatch(ctx context.Context, method string, params json.RawMessage) rpcResponse {
	switch method {
	case "log":
		var p struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		b.mu.Lock()
		ph := b.phase
		b.logs = append(b.logs, logEntry{Phase: ph, Message: p.Message})
		b.mu.Unlock()
		b.emit(Event{Kind: EventLog, Phase: ph, Message: p.Message})
		return rpcResponse{Result: true}

	case "phase":
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		b.mu.Lock()
		b.phase = p.Name
		b.mu.Unlock()
		b.emit(Event{Kind: EventPhase, Phase: p.Name})
		return rpcResponse{Result: true}

	case "agent":
		var req AgentRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		if req.Close {
			if b.agentExec != nil {
				_, _ = b.agentExec(ctx, req)
			}
			return rpcResponse{Result: true}
		}
		if b.agentExec == nil {
			return rpcResponse{Error: &rpcError{Message: "agent executor is not configured"}}
		}
		ph := b.currentPhase()
		b.emit(Event{
			Kind:      EventAgentStart,
			Phase:     ph,
			AgentID:   req.AgentID,
			Role:      req.Role,
			TurnIndex: req.TurnIndex,
		})
		res, err := b.agentExec(ctx, req)
		if err != nil {
			b.emit(Event{
				Kind:      EventAgentDone,
				Phase:     ph,
				AgentID:   req.AgentID,
				Role:      req.Role,
				TurnIndex: req.TurnIndex,
				Error:     err.Error(),
			})
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		b.emit(Event{
			Kind:      EventAgentDone,
			Phase:     ph,
			AgentID:   req.AgentID,
			Role:      req.Role,
			TurnIndex: req.TurnIndex,
		})
		return rpcResponse{Result: res}

	case "call_tool":
		var p struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		if b.toolExec == nil {
			return rpcResponse{Error: &rpcError{Message: "tool executor is not configured"}}
		}
		out, denied, err := b.toolExec(ctx, p.Name, p.Args)
		if denied || err != nil {
			msg := ""
			if err != nil {
				msg = err.Error()
			} else {
				msg = "tool call denied"
			}
			return rpcResponse{Error: &rpcError{Message: msg, Denied: denied}}
		}
		return rpcResponse{Result: out}

	case "trajectory":
		var filter TrajectoryFilter
		if len(params) > 0 {
			if err := json.Unmarshal(params, &filter); err != nil {
				return rpcResponse{Error: &rpcError{Message: err.Error()}}
			}
		}
		if b.trajProv == nil {
			return rpcResponse{Result: []TrajectoryStep{}}
		}
		steps, err := b.trajProv(ctx, filter)
		if err != nil {
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		const maxStepOutput = 200000
		out := make([]TrajectoryStep, len(steps))
		for i, s := range steps {
			if len(s.Output) > maxStepOutput {
				s.Output = s.Output[:maxStepOutput]
			}
			out[i] = s
		}
		return rpcResponse{Result: out}

	case "report_error":
		var se scriptError
		if err := json.Unmarshal(params, &se); err != nil {
			return rpcResponse{Error: &rpcError{Message: err.Error()}}
		}
		b.mu.Lock()
		b.scriptErr = &se
		b.mu.Unlock()
		return rpcResponse{Result: true}

	default:
		return rpcResponse{Error: &rpcError{Message: fmt.Sprintf("unknown rpc method %q", method)}}
	}
}

func (b *bridgeServer) formattedLogs() string {
	b.mu.Lock()
	entries := make([]logEntry, len(b.logs))
	copy(entries, b.logs)
	b.mu.Unlock()

	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, e := range entries {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if e.Phase != "" {
			sb.WriteByte('[')
			sb.WriteString(e.Phase)
			sb.WriteString("] ")
		}
		sb.WriteString(e.Message)
	}
	return sb.String()
}

func (b *bridgeServer) getScriptError() *scriptError {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.scriptErr == nil {
		return nil
	}
	cp := *b.scriptErr
	return &cp
}
