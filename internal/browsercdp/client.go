package browsercdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// ErrBrowserUnavailable indicates no Chrome/Chromium browser could be located or connected.
var ErrBrowserUnavailable = fmt.Errorf("browser tools require Chrome DevTools Protocol; not available in recovered build")

// Target represents a Chrome DevTools Protocol target/tab.
type Target struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// Client manages the Chrome process and CDP WebSocket connection.
type Client struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	dataDir      string
	wsURL        string
	ws           *websocket.Conn
	msgID        atomic.Int64
	pendingMu    sync.Mutex
	pending      map[int64]chan cdpResponse
	consoleMu    sync.Mutex
	consoleLogs  []string
	networkMu    sync.Mutex
	networkReqs  []NetworkRequest
	activeTarget string
	closed       bool
}

type cdpRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NetworkRequest records intercepted network activity.
type NetworkRequest struct {
	RequestID string `json:"requestId"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Status    int    `json:"status,omitempty"`
	Type      string `json:"type,omitempty"`
}

var (
	defaultClient *Client
	clientMu      sync.Mutex
)

// GetClient returns a singleton CDP Client, launching Chrome if not already running.
func GetClient(ctx context.Context) (*Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()

	if defaultClient != nil && !defaultClient.closed {
		// Ping connection to verify it's still alive
		if err := defaultClient.ping(ctx); err == nil {
			return defaultClient, nil
		}
		_ = defaultClient.Close()
		defaultClient = nil
	}

	c, err := startOrConnectBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defaultClient = c
	return c, nil
}

func (c *Client) ping(ctx context.Context) error {
	pCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err := c.Call(pCtx, "Browser.getVersion", nil)
	return err
}

func startOrConnectBrowser(ctx context.Context) (*Client, error) {
	// If explicit opt-in is not set and we are not in an active user session,
	// or if CHROME_BIN / CHROME_REMOTE_DEBUGGING_PORT is not explicitly provided,
	// check if headless auto-launch is enabled via PIGASUS_ENABLE_BROWSER or CDP port.
	debugPort := os.Getenv("CHROME_REMOTE_DEBUGGING_PORT")
	if debugPort == "" && os.Getenv("PIGASUS_ENABLE_BROWSER") == "" && os.Getenv("CHROME_BIN") == "" {
		// Try already running port 9222 first
		if wsURL, err := queryWebSocketURL(ctx, "http://127.0.0.1:9222"); err == nil && wsURL != "" {
			return connectWS(ctx, wsURL, nil, "")
		}
		// Return unavailable if no explicitly running port or opt-in
		return nil, ErrBrowserUnavailable
	}

	if debugPort == "" {
		debugPort = "9222"
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%s", debugPort)
	if wsURL, err := queryWebSocketURL(ctx, endpoint); err == nil && wsURL != "" {
		return connectWS(ctx, wsURL, nil, "")
	}

	// 2. Locate Chrome/Chromium executable
	bin := findChromeBinary()
	if bin == "" {
		return nil, ErrBrowserUnavailable
	}

	// 3. Launch Chrome headless with random or fixed debug port
	dataDir, err := os.MkdirTemp("", "pigasus-chrome-*")
	if err != nil {
		return nil, fmt.Errorf("create chrome user data dir: %w", err)
	}

	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--remote-debugging-port=" + debugPort,
		"--user-data-dir=" + dataDir,
		"about:blank",
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--no-sandbox", "--disable-dev-shm-usage")
	}

	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dataDir)
		return nil, fmt.Errorf("launch chrome (%s): %w", bin, err)
	}

	// Wait for browser to listen on port
	var wsURL string
	var queryErr error
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		wsURL, queryErr = queryWebSocketURL(ctx, endpoint)
		if queryErr == nil && wsURL != "" {
			break
		}
	}
	if wsURL == "" {
		_ = cmd.Process.Kill()
		_ = os.RemoveAll(dataDir)
		return nil, fmt.Errorf("failed to connect to chrome CDP on %s: %v", endpoint, queryErr)
	}

	return connectWS(ctx, wsURL, cmd, dataDir)
}

func findChromeBinary() string {
	if env := os.Getenv("CHROME_BIN"); env != "" {
		if _, err := os.Stat(env); err == nil {
			return env
		}
	}

	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	case "linux", "android":
		candidates = []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"brave-browser",
		}
	case "windows":
		candidates = []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		}
	}

	for _, c := range candidates {
		if filepath.IsAbs(c) {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		} else {
			if path, err := exec.LookPath(c); err == nil {
				return path
			}
		}
	}
	return ""
}

func queryWebSocketURL(ctx context.Context, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/json/version", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.WebSocketDebuggerURL, nil
}

func connectWS(ctx context.Context, wsURL string, cmd *exec.Cmd, dataDir string) (*Client, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if dataDir != "" {
			_ = os.RemoveAll(dataDir)
		}
		return nil, fmt.Errorf("dial cdp ws (%s): %w", wsURL, err)
	}

	c := &Client{
		cmd:         cmd,
		dataDir:     dataDir,
		wsURL:       wsURL,
		ws:          conn,
		pending:     make(map[int64]chan cdpResponse),
		consoleLogs: make([]string, 0),
		networkReqs: make([]NetworkRequest, 0),
	}

	go c.readPump()

	// Enable standard domains
	initCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, _ = c.Call(initCtx, "Page.enable", nil)
	_, _ = c.Call(initCtx, "Runtime.enable", nil)
	_, _ = c.Call(initCtx, "DOM.enable", nil)
	_, _ = c.Call(initCtx, "Network.enable", nil)
	_, _ = c.Call(initCtx, "Log.enable", nil)

	return c, nil
}

func (c *Client) readPump() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()

		c.pendingMu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()
	}()

	for {
		_, msg, err := c.ws.Read(context.Background())
		if err != nil {
			return
		}

		var resp cdpResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			continue
		}

		// Handle events
		if resp.Method != "" {
			c.handleEvent(resp.Method, resp.Params)
			continue
		}

		// Handle call response
		c.pendingMu.Lock()
		ch, ok := c.pending[resp.ID]
		if ok {
			delete(c.pending, resp.ID)
		}
		c.pendingMu.Unlock()

		if ok {
			ch <- resp
		}
	}
}

func (c *Client) handleEvent(method string, params json.RawMessage) {
	switch method {
	case "Runtime.consoleAPICalled":
		var ev struct {
			Type string `json:"type"`
			Args []struct {
				Value any `json:"value"`
			} `json:"args"`
		}
		if err := json.Unmarshal(params, &ev); err == nil {
			var parts []string
			for _, a := range ev.Args {
				parts = append(parts, fmt.Sprint(a.Value))
			}
			logLine := fmt.Sprintf("[%s] %s", strings.ToUpper(ev.Type), strings.Join(parts, " "))
			c.consoleMu.Lock()
			c.consoleLogs = append(c.consoleLogs, logLine)
			c.consoleMu.Unlock()
		}
	case "Log.entryAdded":
		var ev struct {
			Entry struct {
				Level string `json:"level"`
				Text  string `json:"text"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(params, &ev); err == nil {
			logLine := fmt.Sprintf("[%s] %s", strings.ToUpper(ev.Entry.Level), ev.Entry.Text)
			c.consoleMu.Lock()
			c.consoleLogs = append(c.consoleLogs, logLine)
			c.consoleMu.Unlock()
		}
	case "Network.requestWillBeSent":
		var ev struct {
			RequestID string `json:"requestId"`
			Request   struct {
				URL    string `json:"url"`
				Method string `json:"method"`
			} `json:"request"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(params, &ev); err == nil {
			c.networkMu.Lock()
			c.networkReqs = append(c.networkReqs, NetworkRequest{
				RequestID: ev.RequestID,
				URL:       ev.Request.URL,
				Method:    ev.Request.Method,
				Type:      ev.Type,
			})
			c.networkMu.Unlock()
		}
	case "Network.responseReceived":
		var ev struct {
			RequestID string `json:"requestId"`
			Response  struct {
				Status int `json:"status"`
			} `json:"response"`
		}
		if err := json.Unmarshal(params, &ev); err == nil {
			c.networkMu.Lock()
			for i := range c.networkReqs {
				if c.networkReqs[i].RequestID == ev.RequestID {
					c.networkReqs[i].Status = ev.Response.Status
					break
				}
			}
			c.networkMu.Unlock()
		}
	}
}

// Call sends a CDP command and awaits its result.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("cdp client is closed")
	}
	c.mu.Unlock()

	id := c.msgID.Add(1)
	req := cdpRequest{
		ID:     id,
		Method: method,
		Params: params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan cdpResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = ch
	c.pendingMu.Unlock()

	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("cdp connection closed while awaiting response")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("cdp %s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
		}
		return resp.Result, nil
	}
}

// Evaluate evaluates a JavaScript expression in the current execution context.
func (c *Client) Evaluate(ctx context.Context, expr string) (any, error) {
	params := map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	res, err := c.Call(ctx, "Runtime.evaluate", params)
	if err != nil {
		return nil, err
	}

	var evalResult struct {
		Result struct {
			Type  string `json:"type"`
			Value any    `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &evalResult); err != nil {
		return nil, err
	}
	if evalResult.ExceptionDetails != nil {
		desc := evalResult.ExceptionDetails.Text
		if evalResult.ExceptionDetails.Exception.Description != "" {
			desc = evalResult.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("javascript error: %s", desc)
	}
	return evalResult.Result.Value, nil
}

// GetConsoleLogs returns captured console logs.
func (c *Client) GetConsoleLogs(limit int) []string {
	c.consoleMu.Lock()
	defer c.consoleMu.Unlock()
	if limit <= 0 || limit >= len(c.consoleLogs) {
		logs := make([]string, len(c.consoleLogs))
		copy(logs, c.consoleLogs)
		return logs
	}
	return append([]string(nil), c.consoleLogs[len(c.consoleLogs)-limit:]...)
}

// GetNetworkRequests returns intercepted network requests.
func (c *Client) GetNetworkRequests() []NetworkRequest {
	c.networkMu.Lock()
	defer c.networkMu.Unlock()
	reqs := make([]NetworkRequest, len(c.networkReqs))
	copy(reqs, c.networkReqs)
	return reqs
}

// Close terminates the client and cleans up child processes and temp folders.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	if c.ws != nil {
		_ = c.ws.Close(websocket.StatusNormalClosure, "shutdown")
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	if c.dataDir != "" {
		_ = os.RemoveAll(c.dataDir)
	}
	return nil
}
