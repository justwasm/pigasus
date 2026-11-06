package agentserver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"uuid"

	"github.com/creack/pty"
	"github.com/microsoft/agent-host-protocol/clients/go/ahptypes"
)

type ahpTerminal struct {
	channel string
	name    string
	cwd     string
	cols    int64
	rows    int64
	state   ahptypes.TerminalState
	host    *AHPHost

	mu     sync.Mutex
	cmd    *exec.Cmd
	ptmx   *os.File
	closed bool
	done   chan struct{}
}

type terminalSnapshotWire struct {
	Resource string            `json:"resource"`
	State    terminalStateWire `json:"state"`
	FromSeq  int64             `json:"fromSeq"`
}

type terminalStateWire struct {
	Title                    string                         `json:"title"`
	Cwd                      *ahptypes.URI                  `json:"cwd,omitempty"`
	Cols                     *int64                         `json:"cols,omitempty"`
	Rows                     *int64                         `json:"rows,omitempty"`
	Content                  []ahptypes.TerminalContentPart `json:"content"`
	Claim                    ahptypes.TerminalClaim         `json:"claim"`
	SupportsCommandDetection *bool                          `json:"supportsCommandDetection,omitempty"`
	IsPty                    *bool                          `json:"isPty,omitempty"`
}

func (t *ahpTerminal) snapshotWire(fromSeq int64) *terminalSnapshotWire {
	t.mu.Lock()
	defer t.mu.Unlock()
	content := t.state.Content
	if content == nil {
		content = []ahptypes.TerminalContentPart{}
	}
	return &terminalSnapshotWire{
		Resource: t.channel,
		FromSeq:  fromSeq,
		State: terminalStateWire{
			Title:                    t.state.Title,
			Cwd:                      t.state.Cwd,
			Cols:                     t.state.Cols,
			Rows:                     t.state.Rows,
			Content:                  content,
			Claim:                    t.state.Claim,
			SupportsCommandDetection: t.state.SupportsCommandDetection,
			IsPty:                    t.state.IsPty,
		},
	}
}

func (t *ahpTerminal) writeInput(data string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.ptmx == nil {
		return errors.New("terminal closed")
	}
	_, err := t.ptmx.Write([]byte(data))
	return err
}

func (t *ahpTerminal) resize(cols, rows int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cols <= 0 || rows <= 0 {
		return nil
	}
	t.cols = cols
	t.rows = rows
	t.state.Cols = &cols
	t.state.Rows = &rows
	if t.closed || t.ptmx == nil {
		return errors.New("terminal closed")
	}
	return pty.Setsize(t.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (t *ahpTerminal) clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Content = []ahptypes.TerminalContentPart{}
}

func (t *ahpTerminal) close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	proc := t.cmd
	ptmx := t.ptmx
	t.ptmx = nil
	t.mu.Unlock()

	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
}

func (t *ahpTerminal) readLoop() {
	buf := make([]byte, 4096)
	for {
		t.mu.Lock()
		ptmx := t.ptmx
		closed := t.closed
		t.mu.Unlock()
		if closed || ptmx == nil {
			break
		}

		n, err := ptmx.Read(buf)
		if n > 0 {
			out := string(buf[:n])
			t.host.broadcastAction(t.channel, ahptypes.StateAction{
				Value: &ahptypes.TerminalDataAction{
					Type: ahptypes.ActionTypeTerminalData,
					Data: out,
				},
			})
		}
		if err != nil {
			break
		}
	}
}

func (t *ahpTerminal) waitLoop() {
	err := t.cmd.Wait()
	exitCode := int64(0)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = int64(exitErr.ExitCode())
		} else {
			exitCode = -1
		}
	}

	t.mu.Lock()
	t.closed = true
	if t.ptmx != nil {
		_ = t.ptmx.Close()
		t.ptmx = nil
	}
	select {
	case <-t.done:
	default:
		close(t.done)
	}
	t.mu.Unlock()

	// Broadcast terminal/exited
	t.host.broadcastAction(t.channel, ahptypes.StateAction{
		Value: &ahptypes.TerminalExitedAction{
			Type:     ahptypes.ActionTypeTerminalExited,
			ExitCode: &exitCode,
		},
	})

	// Update in-memory lifecycle on root state
	t.host.updateTerminalExit(t.channel, exitCode)
}

func (h *AHPHost) createTerminal(conn *ahpConn, params ahptypes.CreateTerminalParams) (*ahpTerminal, error) {
	ch := string(params.Channel)
	if ch == "" || ch == ahptypes.RootResourceURI {
		ch = "ahp-terminal:/" + uuid.New().String()
	} else {
		ch = canonicalURI(ch)
	}

	h.mu.Lock()
	if _, exists := h.terminals[ch]; exists {
		h.mu.Unlock()
		return nil, fmt.Errorf("terminal already exists: %s", ch)
	}
	h.mu.Unlock()

	cwd := h.srv.cfg.Workspace
	if params.Cwd != nil && *params.Cwd != "" {
		c := string(*params.Cwd)
		if strings.HasPrefix(c, "file://") {
			c = strings.TrimPrefix(c, "file://")
		}
		if c != "" {
			cwd = c
		}
	}
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}

	cols := int64(80)
	if params.Cols != nil && *params.Cols > 0 {
		cols = *params.Cols
	}
	rows := int64(24)
	if params.Rows != nil && *params.Rows > 0 {
		rows = *params.Rows
	}

	name := ""
	if params.Name != nil && *params.Name != "" {
		name = *params.Name
	}

	ptmx, cmd, err := startPTY("", cwd, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("failed to spawn terminal pty: %w", err)
	}

	if name == "" {
		name = filepath.Base(cmd.Path)
		if name == "" || name == "." {
			name = "Terminal"
		}
	}

	claim := params.Claim
	if claim.Value == nil {
		clientID := "client"
		if conn != nil && conn.clientID != "" {
			clientID = conn.clientID
		}
		claim = ahptypes.TerminalClaim{
			Value: &ahptypes.TerminalClientClaim{
				Kind:     ahptypes.TerminalClaimKindClient,
				ClientId: clientID,
			},
		}
	}

	cwdURI := ahptypes.URI("file://" + cwd)
	isPty := true
	term := &ahpTerminal{
		channel: ch,
		name:    name,
		cwd:     cwd,
		cols:    cols,
		rows:    rows,
		host:    h,
		cmd:     cmd,
		ptmx:    ptmx,
		done:    make(chan struct{}),
		state: ahptypes.TerminalState{
			Title:   name,
			Cwd:     &cwdURI,
			Cols:    &cols,
			Rows:    &rows,
			Content: []ahptypes.TerminalContentPart{},
			Lifecycle: ahptypes.TerminalLifecycleState{
				Value: &ahptypes.TerminalRunningLifecycleState{
					Status: ahptypes.TerminalLifecycleStatusRunning,
				},
			},
			Claim: claim,
			IsPty: &isPty,
		},
	}

	h.mu.Lock()
	h.terminals[ch] = term
	info := ahptypes.TerminalInfo{
		Resource:  ahptypes.URI(ch),
		Title:     name,
		Claim:     claim,
		Lifecycle: term.state.Lifecycle,
	}
	h.root.Terminals = append(h.root.Terminals, info)
	terminalsCopy := append([]ahptypes.TerminalInfo(nil), h.root.Terminals...)
	h.mu.Unlock()

	// Broadcast root/terminalsChanged on root resource
	h.broadcastAction(ahptypes.RootResourceURI, ahptypes.StateAction{
		Value: &ahptypes.RootTerminalsChangedAction{
			Type:      ahptypes.ActionTypeRootTerminalsChanged,
			Terminals: terminalsCopy,
		},
	})

	go term.readLoop()
	go term.waitLoop()

	return term, nil
}

func (h *AHPHost) disposeTerminal(channel string) error {
	channel = canonicalURI(channel)
	h.mu.Lock()
	term, exists := h.terminals[channel]
	if !exists {
		h.mu.Unlock()
		return fmt.Errorf("terminal not found: %s", channel)
	}
	delete(h.terminals, channel)
	delete(h.subs, channel)

	newTerms := make([]ahptypes.TerminalInfo, 0, len(h.root.Terminals))
	for _, ti := range h.root.Terminals {
		if string(ti.Resource) != channel {
			newTerms = append(newTerms, ti)
		}
	}
	h.root.Terminals = newTerms
	h.mu.Unlock()

	term.close()

	h.broadcastAction(ahptypes.RootResourceURI, ahptypes.StateAction{
		Value: &ahptypes.RootTerminalsChangedAction{
			Type:      ahptypes.ActionTypeRootTerminalsChanged,
			Terminals: newTerms,
		},
	})
	return nil
}

func (h *AHPHost) updateTerminalExit(channel string, exitCode int64) {
	h.mu.Lock()
	for i := range h.root.Terminals {
		if string(h.root.Terminals[i].Resource) == channel {
			h.root.Terminals[i].Lifecycle = ahptypes.TerminalLifecycleState{
				Value: &ahptypes.TerminalExitedLifecycleState{
					Status:   ahptypes.TerminalLifecycleStatusExited,
					ExitCode: &exitCode,
				},
			}
			break
		}
	}
	terminalsCopy := append([]ahptypes.TerminalInfo(nil), h.root.Terminals...)
	h.mu.Unlock()

	h.broadcastAction(ahptypes.RootResourceURI, ahptypes.StateAction{
		Value: &ahptypes.RootTerminalsChangedAction{
			Type:      ahptypes.ActionTypeRootTerminalsChanged,
			Terminals: terminalsCopy,
		},
	})
}
