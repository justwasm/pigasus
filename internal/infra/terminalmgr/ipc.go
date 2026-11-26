package terminalmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/rcterminal"
)

// DefaultSocketPath returns a short, user-private Unix socket path for a
// `pigasus terminal` multiplexer instance.
func DefaultSocketPath(pid int) string {
	if pid <= 0 {
		pid = os.Getpid()
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		runDir := filepath.Join(home, ".openagy", "run")
		cand := filepath.Join(runDir, fmt.Sprintf("term-%d.sock", pid))
		// Keep well below macOS/BSD 104-byte sun_path limit.
		if len(cand) < 96 {
			return cand
		}
	}
	return filepath.Join("/tmp", fmt.Sprintf("pigasus-term-%d.sock", pid))
}

// DiscoverSocket returns the host multiplexer socket from PIGASUS_TERMINAL_SOCKET
// or the newest live socket under ~/.openagy/run/term-*.sock.
func DiscoverSocket() string {
	if env := strings.TrimSpace(os.Getenv(EnvSocket)); env != "" {
		return env
	}
	var patterns []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		patterns = append(patterns, filepath.Join(home, ".openagy", "run", "term-*.sock"))
	}
	patterns = append(patterns, filepath.Join("/tmp", "pigasus-term-*.sock"))

	type candidate struct {
		path    string
		modTime time.Time
	}
	var cands []candidate
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if fi, err := os.Stat(m); err == nil {
				cands = append(cands, candidate{path: m, modTime: fi.ModTime()})
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].modTime.After(cands[j].modTime)
	})
	for _, c := range cands {
		cl := NewIPCClient(c.path)
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		err := cl.Ping(ctx)
		cancel()
		if err == nil {
			return c.path
		}
	}
	return ""
}

// ResolveBackend connects to a host `pigasus terminal` multiplexer when
// PIGASUS_TERMINAL_SOCKET is set and reachable; otherwise it creates an
// in-process Manager (sharing localReg if provided).
func ResolveBackend(workspace string, localReg *rcterminal.Registry) (Backend, func()) {
	if sock := strings.TrimSpace(os.Getenv(EnvSocket)); sock != "" {
		cl := NewIPCClient(sock)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		err := cl.Ping(ctx)
		cancel()
		if err == nil {
			return cl, func() {}
		}
	}
	var mgr *Manager
	if localReg != nil {
		mgr = NewWithRegistry(localReg)
	} else {
		mgr = New(workspace)
	}
	return mgr, mgr.CloseAll
}

// IPCServer exposes a Backend over a local Unix domain socket.
type IPCServer struct {
	sockPath string
	listener net.Listener
	srv      *http.Server
	backend  Backend
	once     sync.Once
}

// StartIPCServer starts an HTTP-over-Unix-socket control server for backend.
func StartIPCServer(sockPath string, backend Backend) (*IPCServer, error) {
	if strings.TrimSpace(sockPath) == "" {
		return nil, errors.New("terminal ipc: empty socket path")
	}
	if backend == nil {
		return nil, errors.New("terminal ipc: nil backend")
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		return nil, fmt.Errorf("terminal ipc: mkdir: %w", err)
	}
	_ = os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("terminal ipc: listen %s: %w", sockPath, err)
	}
	_ = os.Chmod(sockPath, 0o600)

	s := &IPCServer{
		sockPath: sockPath,
		listener: ln,
		backend:  backend,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/sessions/capture", s.handleCapture)
	mux.HandleFunc("/v1/sessions/send", s.handleSend)
	mux.HandleFunc("/v1/sessions/resize", s.handleResize)
	mux.HandleFunc("/v1/sessions/kill", s.handleKill)
	mux.HandleFunc("/v1/sessions/switch", s.handleSwitch)
	mux.HandleFunc("/v1/sessions/rename", s.handleRename)

	s.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = s.srv.Serve(ln)
	}()
	return s, nil
}

// SocketPath returns the Unix socket path the server is listening on.
func (s *IPCServer) SocketPath() string {
	if s == nil {
		return ""
	}
	return s.sockPath
}

// Close shuts down the IPC server and removes the socket file.
func (s *IPCServer) Close() error {
	if s == nil {
		return nil
	}
	var err error
	s.once.Do(func() {
		if s.srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err = s.srv.Shutdown(ctx)
			cancel()
		}
		if s.listener != nil {
			_ = s.listener.Close()
		}
		if s.sockPath != "" {
			_ = os.Remove(s.sockPath)
		}
	})
	return err
}

type ipcError struct {
	Error string `json:"error"`
}

type ipcIDReq struct {
	ID string `json:"id"`
}

type ipcResizeReq struct {
	ID   string `json:"id"`
	Rows int    `json:"rows"`
	Cols int    `json:"cols"`
}

type ipcRenameReq struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	return json.Unmarshal(body, dst)
}

func (s *IPCServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *IPCServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.backend.List())
	case http.MethodPost:
		var opts NewOptions
		if err := decodeJSON(r, &opts); err != nil {
			writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
			return
		}
		res, err := s.backend.New(r.Context(), opts)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
	}
}

func (s *IPCServer) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
		return
	}
	var opts CaptureOptions
	if err := decodeJSON(r, &opts); err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	res, err := s.backend.Capture(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *IPCServer) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
		return
	}
	var opts SendOptions
	if err := decodeJSON(r, &opts); err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	res, err := s.backend.Send(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *IPCServer) handleResize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
		return
	}
	var req ipcResizeReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	info, err := s.backend.Resize(req.ID, req.Rows, req.Cols)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *IPCServer) handleKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
		return
	}
	var req ipcIDReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	info, err := s.backend.Kill(req.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *IPCServer) handleSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
		return
	}
	var req ipcIDReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	info, err := s.backend.Switch(req.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *IPCServer) handleRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ipcError{Error: "method not allowed"})
		return
	}
	var req ipcRenameReq
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	info, err := s.backend.Rename(req.ID, req.Title)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ipcError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// IPCClient implements Backend by talking to a running `pigasus terminal`
// multiplexer over its Unix domain socket.
type IPCClient struct {
	sockPath string
	selfID   string
	http     *http.Client
}

var _ Backend = (*IPCClient)(nil)

// NewIPCClient creates a client connected to sockPath.
func NewIPCClient(sockPath string) *IPCClient {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", sockPath)
		},
	}
	return &IPCClient{
		sockPath: sockPath,
		selfID:   SelfSessionIDFromEnv(),
		http: &http.Client{
			Transport: tr,
			Timeout:   15 * time.Second,
		},
	}
}

// SetSelfID overrides the session ID treated as "self".
func (c *IPCClient) SetSelfID(id string) {
	if c != nil {
		c.selfID = strings.TrimSpace(id)
	}
}

func (c *IPCClient) resolveID(id string) string {
	trimmed := strings.TrimSpace(id)
	if strings.EqualFold(trimmed, "self") && c.selfID != "" {
		return c.selfID
	}
	return trimmed
}

func (c *IPCClient) markSelf(info *SessionInfo) {
	if info != nil && c.selfID != "" && info.ID == c.selfID {
		info.Self = true
	}
}

// Ping checks whether the host multiplexer socket is alive.
func (c *IPCClient) Ping(ctx context.Context) error {
	if c == nil || c.sockPath == "" {
		return errors.New("terminal ipc: empty socket path")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("terminal ipc: health status %d", resp.StatusCode)
	}
	return nil
}

func (c *IPCClient) doJSON(ctx context.Context, method, path string, reqBody, respBody any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		raw, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, bodyReader)
	if err != nil {
		return err
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e ipcError
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("terminal ipc: %s %s returned %d", method, path, resp.StatusCode)
	}
	if respBody != nil && len(bytes.TrimSpace(raw)) > 0 {
		return json.Unmarshal(raw, respBody)
	}
	return nil
}

func (c *IPCClient) List() []SessionInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var out []SessionInfo
	if err := c.doJSON(ctx, http.MethodGet, "/v1/sessions", nil, &out); err != nil {
		return nil
	}
	for i := range out {
		c.markSelf(&out[i])
	}
	return out
}

func (c *IPCClient) New(ctx context.Context, opts NewOptions) (CaptureResult, error) {
	var res CaptureResult
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions", opts, &res)
	c.markSelf(&res.Session)
	return res, err
}

func (c *IPCClient) Capture(ctx context.Context, opts CaptureOptions) (CaptureResult, error) {
	opts.ID = c.resolveID(opts.ID)
	var res CaptureResult
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/capture", opts, &res)
	c.markSelf(&res.Session)
	return res, err
}

func (c *IPCClient) Send(ctx context.Context, opts SendOptions) (CaptureResult, error) {
	opts.ID = c.resolveID(opts.ID)
	var res CaptureResult
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/send", opts, &res)
	c.markSelf(&res.Session)
	return res, err
}

func (c *IPCClient) Resize(idOrName string, rows, cols int) (SessionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var info SessionInfo
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/resize", ipcResizeReq{
		ID:   c.resolveID(idOrName),
		Rows: rows,
		Cols: cols,
	}, &info)
	c.markSelf(&info)
	return info, err
}

func (c *IPCClient) Kill(idOrName string) (SessionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var info SessionInfo
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/kill", ipcIDReq{
		ID: c.resolveID(idOrName),
	}, &info)
	c.markSelf(&info)
	return info, err
}

func (c *IPCClient) Switch(idOrName string) (SessionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var info SessionInfo
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/switch", ipcIDReq{
		ID: c.resolveID(idOrName),
	}, &info)
	c.markSelf(&info)
	return info, err
}

func (c *IPCClient) Rename(idOrName, title string) (SessionInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var info SessionInfo
	err := c.doJSON(ctx, http.MethodPost, "/v1/sessions/rename", ipcRenameReq{
		ID:    c.resolveID(idOrName),
		Title: title,
	}, &info)
	c.markSelf(&info)
	return info, err
}
