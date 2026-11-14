package herdr

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// Server is a Unix Domain Socket JSON-RPC server implementing the Herdr protocol.
type Server struct {
	mu         sync.Mutex
	listener   net.Listener
	socketPath string
	manager    *Manager
	closed     atomic.Bool

	onNotify func(target, message string)
}

// PaneSummary is the JSON serialization representation of a pane in Herdr.
type PaneSummary struct {
	PaneID       string `json:"pane_id"`
	Title        string `json:"title"`
	Cwd          string `json:"cwd"`
	Status       string `json:"status"`
	Agent        string `json:"agent,omitempty"`
	AgentState   string `json:"agent_state,omitempty"`
	AgentMessage string `json:"agent_message,omitempty"`
}

// Request is a single JSON-RPC request in Herdr format.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// Response is the Herdr JSON-RPC response envelope.
type Response struct {
	ID     string         `json:"id,omitempty"`
	Result any            `json:"result,omitempty"`
	Error  *ResponseError `json:"error,omitempty"`
}

// NewServer starts a new Unix domain socket server listening on socketPath.
// If socketPath is empty, a unique path in os.TempDir() is allocated.
func NewServer(socketPath string, m *Manager) (*Server, error) {
	if m == nil {
		return nil, errors.New("nil manager")
	}
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("herdr-%d.sock", os.Getpid()))
		if len(socketPath) >= 100 {
			socketPath = fmt.Sprintf("/tmp/herdr-%d.sock", os.Getpid())
		}
	}

	// Remove stale socket file if any
	_ = os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on herdr socket %s: %w", socketPath, err)
	}

	// Restrict permissions to user-only (0700)
	_ = os.Chmod(socketPath, 0700)

	s := &Server{
		listener:   l,
		socketPath: socketPath,
		manager:    m,
	}

	m.SetSocketPath(socketPath)
	m.SetServer(s)

	go s.serve()

	return s, nil
}

// SocketPath returns the Unix domain socket path.
func (s *Server) SocketPath() string {
	return s.socketPath
}

// SetOnNotify sets a callback for incoming agent doorbell notifications.
func (s *Server) SetOnNotify(fn func(target, message string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onNotify = fn
}

// Close terminates the Unix listener and cleans up the socket file.
func (s *Server) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	err := s.listener.Close()
	_ = os.Remove(s.socketPath)
	return err
}

func (s *Server) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	reader := bufio.NewReader(io.LimitReader(conn, 1<<20))
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return
	}

	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(conn, "", "invalid_request", "invalid request: "+err.Error())
		return
	}

	result, rerr := s.dispatch(req)
	if rerr != nil {
		s.writeError(conn, req.ID, rerr.Code, rerr.Message)
		return
	}

	s.writeResult(conn, req.ID, result)
}

func (s *Server) writeResult(w io.Writer, id string, result any) {
	resp := Response{
		ID:     id,
		Result: result,
	}
	data, _ := json.Marshal(resp)
	_, _ = w.Write(append(data, '\n'))
}

func (s *Server) writeError(w io.Writer, id, code, msg string) {
	resp := Response{
		ID: id,
		Error: &ResponseError{
			Code:    code,
			Message: msg,
		},
	}
	data, _ := json.Marshal(resp)
	_, _ = w.Write(append(data, '\n'))
}

func (s *Server) dispatch(req Request) (any, *ResponseError) {
	switch req.Method {
	case "system.ping", "ping":
		return map[string]any{
			"type":           "pong",
			"version":        "0.9.5+openagy",
			"protocol":       22,
			"server":         "pigasus-herdr",
			"server_version": "1.0.0",
		}, nil

	case "pane.report_agent", "pane_report_agent":
		var p struct {
			PaneID  string  `json:"pane_id"`
			Agent   string  `json:"agent"`
			State   string  `json:"state"`
			Message *string `json:"message"`
			Seq     *uint64 `json:"seq"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &ResponseError{Code: "invalid_params", Message: err.Error()}
			}
		}
		pane := s.manager.FindPaneByTarget(p.PaneID)
		if pane == nil {
			return nil, &ResponseError{Code: "agent_not_found", Message: "pane not found: " + p.PaneID}
		}
		msg := ""
		if p.Message != nil {
			msg = *p.Message
		}
		agentName := p.Agent
		if canonical, ok := LookupAgent(p.Agent); ok {
			agentName = canonical
		}
		pane.SetAgentState(agentName, p.State, msg)
		s.manager.TriggerUpdate()
		return map[string]any{"type": "ok"}, nil

	case "pane.report_metadata", "pane_report_metadata":
		var p struct {
			PaneID string `json:"pane_id"`
			Title  string `json:"title"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &ResponseError{Code: "invalid_params", Message: err.Error()}
			}
		}
		pane := s.manager.FindPaneByTarget(p.PaneID)
		if pane == nil {
			return nil, &ResponseError{Code: "agent_not_found", Message: "pane not found: " + p.PaneID}
		}
		if p.Title != "" {
			pane.SetTitle(p.Title)
		}
		s.manager.TriggerUpdate()
		return map[string]any{"type": "ok"}, nil

	case "pane.release_agent", "pane_release_agent":
		var p struct {
			PaneID string `json:"pane_id"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		pane := s.manager.FindPaneByTarget(p.PaneID)
		if pane != nil {
			pane.ClearAgentState()
			s.manager.TriggerUpdate()
		}
		return map[string]any{"type": "ok"}, nil

	case "pane.list", "pane_list":
		panes := s.manager.AllPanes()
		summaries := make([]PaneSummary, 0, len(panes))
		for _, p := range panes {
			status, _ := p.Status()
			agent, astate, amsg := p.AgentState()
			summaries = append(summaries, PaneSummary{
				PaneID:       p.ID,
				Title:        p.Title,
				Cwd:          p.Dir,
				Status:       status.String(),
				Agent:        agent,
				AgentState:   astate,
				AgentMessage: amsg,
			})
		}
		return map[string]any{"panes": summaries}, nil

	case "pane.get", "pane_get":
		var p struct {
			PaneID string `json:"pane_id"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &ResponseError{Code: "invalid_params", Message: err.Error()}
			}
		}
		target := p.PaneID
		pane := s.manager.FindPaneByTarget(target)
		if pane == nil {
			return nil, &ResponseError{Code: "agent_not_found", Message: "herdr agent not found: " + target}
		}
		status, _ := pane.Status()
		agent, astate, amsg := pane.AgentState()
		summary := PaneSummary{
			PaneID:       pane.ID,
			Title:        pane.Title,
			Cwd:          pane.Dir,
			Status:       status.String(),
			Agent:        agent,
			AgentState:   astate,
			AgentMessage: amsg,
		}
		return map[string]any{
			"pane":          summary,
			"pane_id":       summary.PaneID,
			"title":         summary.Title,
			"cwd":           summary.Cwd,
			"status":        summary.Status,
			"agent":         summary.Agent,
			"agent_state":   summary.AgentState,
			"agent_message": summary.AgentMessage,
		}, nil

	case "pane.send_text", "pane_send_text":
		var p struct {
			PaneID string `json:"pane_id"`
			Text   string `json:"text"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &ResponseError{Code: "invalid_params", Message: err.Error()}
			}
		}
		pane := s.manager.FindPaneByTarget(p.PaneID)
		if pane == nil {
			return nil, &ResponseError{Code: "agent_not_found", Message: "pane not found: " + p.PaneID}
		}
		if _, err := pane.Write([]byte(p.Text)); err != nil {
			return nil, &ResponseError{Code: "write_failed", Message: err.Error()}
		}
		return map[string]any{"type": "ok"}, nil

	case "pane.split", "pane_split":
		var p struct {
			Direction string `json:"direction"`
			Title     string `json:"title"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		np, err := s.manager.AddPane(p.Title, "")
		if err != nil {
			return nil, &ResponseError{Code: "split_failed", Message: err.Error()}
		}
		return map[string]any{
			"pane_id": np.ID,
			"type":    "ok",
		}, nil

	case "pane.close", "pane_close":
		var p struct {
			PaneID string `json:"pane_id"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.PaneID == "" {
			s.manager.CloseCurrent()
		} else {
			_ = s.manager.ClosePaneByID(p.PaneID)
		}
		return map[string]any{"type": "ok"}, nil

	case "agent.prompt", "agent_prompt":
		var p struct {
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return nil, &ResponseError{Code: "invalid_params", Message: err.Error()}
			}
		}
		if p.Target == "" || p.Message == "" {
			return nil, &ResponseError{Code: "invalid_params", Message: "empty prompt target or message"}
		}
		pane := s.manager.FindPaneByTarget(p.Target)
		if pane == nil {
			return nil, &ResponseError{Code: "agent_not_found", Message: "herdr agent not found: " + p.Target}
		}
		status, _ := pane.Status()
		if status != StatusRunning {
			return nil, &ResponseError{Code: "agent_not_ready", Message: "herdr agent not ready"}
		}

		pane.NotifyPrompt(p.Message)
		s.manager.TriggerUpdate()

		s.mu.Lock()
		notifyCb := s.onNotify
		s.mu.Unlock()
		if notifyCb != nil {
			notifyCb(p.Target, p.Message)
		}

		return map[string]any{"type": "ok"}, nil

	case "workspace.get", "workspace_get":
		ws := s.manager.ActiveWorkspace()
		panes := ws.PaneList()
		summaries := make([]PaneSummary, 0, len(panes))
		for _, p := range panes {
			status, _ := p.Status()
			agent, astate, amsg := p.AgentState()
			summaries = append(summaries, PaneSummary{
				PaneID:       p.ID,
				Title:        p.Title,
				Cwd:          p.Dir,
				Status:       status.String(),
				Agent:        agent,
				AgentState:   astate,
				AgentMessage: amsg,
			})
		}
		return map[string]any{
			"workspace_id": ws.ID,
			"name":         ws.Name,
			"dir":          ws.Dir,
			"branch":       ws.Branch,
			"panes":        summaries,
			"active_pane":  ws.ActiveIndex(),
		}, nil

	case "workspace.list", "workspace_list":
		workspaces := s.manager.Workspaces()
		out := make([]map[string]any, 0, len(workspaces))
		for _, ws := range workspaces {
			out = append(out, map[string]any{
				"workspace_id": ws.ID,
				"name":         ws.Name,
				"dir":          ws.Dir,
				"branch":       ws.Branch,
				"panes_count":  ws.Count(),
			})
		}
		return map[string]any{
			"workspaces": out,
		}, nil

	default:
		return nil, &ResponseError{
			Code:    "unknown_method",
			Message: "unknown method: " + req.Method,
		}
	}
}
