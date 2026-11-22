package tui

import (
	"context"
	"sync"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/rcterminal"
)

// TerminalEvent carries output or exit events from an active terminal.
type TerminalEvent struct {
	Output   []byte
	ExitCode *int32
	Offset   int64
}

// TerminalInfo describes an active terminal session.
type TerminalInfo struct {
	TerminalID string
	Title      string
	Cwd        string
	PID        int
}

// TerminalService unifies terminal lifecycle and I/O across Local, Server, and RemoteAgent modes.
type TerminalService interface {
	CreateTerminal(ctx context.Context, cwd string) (TerminalInfo, error)
	ListTerminals(ctx context.Context) ([]TerminalInfo, error)
	SendTerminalInput(ctx context.Context, id string, data []byte) error
	StreamTerminal(ctx context.Context, id string, offset int64) (<-chan TerminalEvent, error)
	CloseTerminal(ctx context.Context, id string) error
	Resize(ctx context.Context, id string, rows, cols int) error
}

// LocalTerminalService manages local terminal sessions backed by rcterminal.Registry.
type LocalTerminalService struct {
	reg *rcterminal.Registry
	mu  sync.Mutex
}

// NewLocalTerminalService returns a TerminalService backed by local pseudo-terminals.
func NewLocalTerminalService(workspace string) *LocalTerminalService {
	reg := rcterminal.NewRegistry()
	reg.SetWorkspace(workspace)
	return &LocalTerminalService{reg: reg}
}

// CreateTerminal spawns a local login shell in cwd.
func (s *LocalTerminalService) CreateTerminal(ctx context.Context, cwd string) (TerminalInfo, error) {
	req := rcterminal.CreateRequest{Workspace: cwd}
	if req.Workspace == "" {
		req.Workspace = s.reg.Workspace()
	}
	info, err := s.reg.Create(req)
	if err != nil {
		return TerminalInfo{}, err
	}
	return TerminalInfo{
		TerminalID: info.TerminalID,
		Title:      info.Title,
		Cwd:        info.Cwd,
		PID:        info.PID,
	}, nil
}

// ListTerminals returns all active local terminal sessions.
func (s *LocalTerminalService) ListTerminals(ctx context.Context) ([]TerminalInfo, error) {
	list := s.reg.List("", "", "")
	out := make([]TerminalInfo, 0, len(list))
	for _, item := range list {
		out = append(out, TerminalInfo{
			TerminalID: item.TerminalID,
			Title:      item.Title,
			Cwd:        item.Cwd,
			PID:        item.PID,
		})
	}
	return out, nil
}

// SendTerminalInput writes raw input bytes to the local PTY.
func (s *LocalTerminalService) SendTerminalInput(ctx context.Context, id string, data []byte) error {
	return s.reg.Write(id, data)
}

// StreamTerminal streams output frames from the local PTY.
func (s *LocalTerminalService) StreamTerminal(ctx context.Context, id string, offset int64) (<-chan TerminalEvent, error) {
	frames, err := s.reg.StreamChannel(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make(chan TerminalEvent, 64)
	go func() {
		defer close(out)
		for f := range frames {
			switch f.Kind {
			case rcterminal.FrameOutput:
				out <- TerminalEvent{Output: f.Output}
			case rcterminal.FrameExitCode:
				code := int32(f.ExitCode)
				out <- TerminalEvent{ExitCode: &code}
			}
		}
	}()
	return out, nil
}

// CloseTerminal terminates the local terminal session.
func (s *LocalTerminalService) CloseTerminal(ctx context.Context, id string) error {
	return s.reg.Close(id, true)
}

// Resize changes the local terminal window dimensions.
func (s *LocalTerminalService) Resize(ctx context.Context, id string, rows, cols int) error {
	return s.reg.Resize(id, cols, rows)
}

// RemoteAgentTerminalService wraps agentturn.RemoteAgentEngine.
type RemoteAgentTerminalService struct {
	engine *agentturn.RemoteAgentEngine
}

// NewRemoteAgentTerminalService returns a TerminalService backed by RemoteAgentEngine.
func NewRemoteAgentTerminalService(engine *agentturn.RemoteAgentEngine) *RemoteAgentTerminalService {
	return &RemoteAgentTerminalService{engine: engine}
}

// CreateTerminal calls RemoteAgentEngine's CreateTerminal.
func (s *RemoteAgentTerminalService) CreateTerminal(ctx context.Context, cwd string) (TerminalInfo, error) {
	info, err := s.engine.CreateTerminal(ctx)
	if err != nil {
		return TerminalInfo{}, err
	}
	return TerminalInfo{
		TerminalID: info.TerminalID,
		Title:      info.Title,
		Cwd:        info.Cwd,
		PID:        int(info.Pid),
	}, nil
}

// ListTerminals calls RemoteAgentEngine's Terminals.
func (s *RemoteAgentTerminalService) ListTerminals(ctx context.Context) ([]TerminalInfo, error) {
	list, err := s.engine.Terminals(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]TerminalInfo, 0, len(list))
	for _, item := range list {
		out = append(out, TerminalInfo{
			TerminalID: item.TerminalID,
			Title:      item.Title,
			Cwd:        item.Cwd,
			PID:        int(item.Pid),
		})
	}
	return out, nil
}

// SendTerminalInput forwards raw bytes to RemoteAgentEngine.
func (s *RemoteAgentTerminalService) SendTerminalInput(ctx context.Context, id string, data []byte) error {
	return s.engine.SendTerminalInput(ctx, id, data)
}

// StreamTerminal streams output events from RemoteAgentEngine.
func (s *RemoteAgentTerminalService) StreamTerminal(ctx context.Context, id string, offset int64) (<-chan TerminalEvent, error) {
	events, err := s.engine.StreamTerminal(ctx, id, offset)
	if err != nil {
		return nil, err
	}
	out := make(chan TerminalEvent, 64)
	go func() {
		defer close(out)
		for e := range events {
			out <- TerminalEvent{
				Output:   e.Output,
				ExitCode: e.ExitCode,
				Offset:   e.Offset,
			}
		}
	}()
	return out, nil
}

// CloseTerminal terminates the remote terminal session.
func (s *RemoteAgentTerminalService) CloseTerminal(ctx context.Context, id string) error {
	return s.engine.CloseTerminal(ctx, id)
}

// Resize is a no-op on RemoteAgentEngine.
func (s *RemoteAgentTerminalService) Resize(ctx context.Context, id string, rows, cols int) error {
	return nil
}
