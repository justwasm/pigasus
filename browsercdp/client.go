package browsercdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// ErrBrowserUnavailable indicates that no Chrome/Chromium browser could be
// located, attached to or started.
//
// The browser starts on its own, the way the reference's does: the
// chrome-devtools-mcp skill the reference ships says "Browser starts
// automatically on first tool call using a persistent Chrome profile"
// 【一手 — out/ide-language-server/skills/chrome-devtools/SKILL.md】. This
// error therefore means "there is no browser on this machine", and the two
// environment variables are overrides rather than switches:
// CHROME_REMOTE_DEBUGGING_PORT attaches to a running Chrome, CHROME_BIN
// names the binary to start.
var ErrBrowserUnavailable = fmt.Errorf("no Chrome browser is available: install Google Chrome, or set CHROME_BIN to the browser binary, or CHROME_REMOTE_DEBUGGING_PORT to attach to a running one")

// NetworkRequest records intercepted network activity.
type NetworkRequest struct {
	RequestID string `json:"requestId"`
	URL       string `json:"url"`
	Method    string `json:"method"`
	Status    int    `json:"status,omitempty"`
	Type      string `json:"type,omitempty"`
}

// Caller is the CDP transport the action layer speaks through. It is an
// interface so the actions can be exercised without a browser.
type Caller interface {
	// Call sends a CDP command and awaits its result.
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	// Evaluate evaluates a JavaScript expression in the current execution context.
	Evaluate(ctx context.Context, expr string) (any, error)
}

// attachTimeout bounds how long the client waits for Chrome to hand it a page.
const attachTimeout = 15 * time.Second

// Client owns the Chrome process (when it launched one) and the CDP session.
type Client struct {
	mu      sync.Mutex
	closed  bool
	browser *chromedp.Browser
	target  *chromedp.Target
	cancel  context.CancelFunc
	cmd     *exec.Cmd

	// tabID is the tab the clone opened in a Chrome that was already
	// running. It is the clone's own tab, and it goes away with the
	// client.
	tabID target.ID

	consoleMu   sync.Mutex
	consoleLogs []string
	networkMu   sync.Mutex
	networkReqs []NetworkRequest
}

var (
	defaultClient *Client
	clientMu      sync.Mutex
)

// browserScopedDomains live on the browser connection. Every other domain is
// page-scoped: it needs the session of an attached target, and Chrome
// rejects it on the browser connection.
var browserScopedDomains = map[string]bool{
	"Browser":    true,
	"Target":     true,
	"SystemInfo": true,
	"Tracing":    true,
}

// GetClient returns a singleton CDP Client, launching Chrome if not already running.
func GetClient(ctx context.Context) (*Client, error) {
	clientMu.Lock()
	defer clientMu.Unlock()

	if defaultClient != nil {
		if defaultClient.isClosed() {
			defaultClient = nil
		} else if err := defaultClient.ping(ctx); err == nil {
			return defaultClient, nil
		} else {
			_ = defaultClient.Close()
			defaultClient = nil
		}
	}

	c, err := startOrConnectBrowser(ctx)
	if err != nil {
		return nil, err
	}
	defaultClient = c
	return c, nil
}

func (c *Client) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *Client) ping(ctx context.Context) error {
	pCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	_, err := c.Call(pCtx, "Browser.getVersion", nil)
	return err
}

// startOrConnectBrowser gets hold of a browser.
//
// The browser the clone drives is the *user's* browser. The first thing
// it does is look for a Chrome that is already running and offers a
// debugging port: that Chrome has the user's own profile, so the
// session — the cookies, the logins, the extensions — is the user's, and
// nothing has to be logged in again. Only when there is no such Chrome
// does the clone start one, and then it seeds the profile it starts with
// from the user's own session (see seedUserSession).
//
// The reference reaches the same browser by another road: its browser
// agent drives Chrome through the chrome-devtools-mcp server 【一手 — the
// 1.4.7 binary's own launcher: "exec npx -y chrome-devtools-mcp@1.10.1
// \"$@\""】, and that server attaches to the Chrome the user has running
// — the reference's agent prompt says "Do NOT Attempt to Launch Chrome"
// and asks the user to turn remote debugging on at
// chrome://inspect/#remote-debugging 【一手】.
//
// The clone keeps its own road: no node, no npm package, no MCP server,
// no process but Chrome itself. It speaks the DevTools protocol to the
// same Chrome over a websocket.
//
// The browser starts on the first tool call ("Browser starts
// automatically on first tool call using a persistent Chrome profile" —
// the chrome-devtools-mcp skill the reference ships 【一手】), and the
// clone's does the same.
func startOrConnectBrowser(ctx context.Context) (*Client, error) {
	cfg := CurrentConfig()
	if attachRequested(cfg) {
		return attachLocalChrome(ctx, cfg)
	}
	// The user's own Chrome first: attaching to it is what makes the
	// session the user's.
	if port, wsURL, err := resolveRunningChrome(ctx, cfg); err == nil {
		return connectCDP(ctx, wsURL, nil, port)
	}
	return launchChrome(ctx, cfg)
}

// resolveRunningChrome finds a Chrome that is already running and answers
// on a DevTools port.
//
// The order is the reference's own discovery order: the port the
// DevToolsActivePort file of the standard Chrome profiles names — Chrome
// writes that file when remote debugging is on, and Chrome 136 and later
// ask for it at chrome://inspect/#remote-debugging 【一手 — the
// reference's agent prompt names exactly that page】 — then the
// environment override, then the configured port. The reference discovers
// the browser the same way: its language server exposes a
// CheckDevToolsActivePort method, and the CLI logs
// "[CDP Discovery] Failed: %v" 【strings — the 1.3.4 strings dump】.
func resolveRunningChrome(ctx context.Context, cfg Config) (string, string, error) {
	ports := localChromePorts(cfg)
	var lastErr error
	for _, port := range ports {
		wsURL, err := queryWebSocketURL(ctx, cdpEndpoint(port))
		if err == nil && wsURL != "" {
			return port, wsURL, nil
		}
		lastErr = err
	}
	return "", "", fmt.Errorf("no Chrome is listening on port %s (%v)", strings.Join(ports, ", "), lastErr)
}

// attachLocalChrome connects to the Chrome that is already running.
//
// It is what --use_local_chrome asks for, and it never starts a browser:
// the user asked for the Chrome they have.
func attachLocalChrome(ctx context.Context, cfg Config) (*Client, error) {
	port, wsURL, err := resolveRunningChrome(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w — start Chrome with --remote-debugging-port=%d, or turn remote debugging on for the running Chrome at chrome://inspect/#remote-debugging, or drop --use_local_chrome to let the clone start its own",
			ErrBrowserUnavailable, err, cfg.Port)
	}
	return connectCDP(ctx, wsURL, nil, port)
}

// launchChrome starts the Chrome the clone drives.
//
// The launch arguments are the reference's own: "--headless=new" (unless
// the local Chrome runs with a window), "--no-first-run",
// "--user-data-dir=" and "--remote-debugging-port=0" 【strings — the
// 1.3.4 strings dump】. Port 0 lets Chrome pick a free one — the
// reference discovers the port afterwards, from the DevToolsActivePort
// file Chrome writes into the profile.
//
// The profile it starts with carries the user's own session whenever
// that can be read, so the browser is logged in already.
func launchChrome(ctx context.Context, cfg Config) (*Client, error) {
	bin := findChromeBinary()
	if bin == "" {
		return nil, ErrBrowserUnavailable
	}

	dataDir, err := browserProfileDir(cfg.UserDataDir)
	if err != nil {
		return nil, err
	}

	// The user's session, when the profile can be read. A profile the
	// user named himself is left alone: that is his decision, and the
	// copy would overwrite what is there.
	if cfg.UserDataDir == "" {
		seedUserSession(dataDir)
	}

	args := launchArgs(cfg, dataDir)

	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch chrome (%s): %w", bin, err)
	}

	// Wait for Chrome to write the port it chose and to answer on it.
	var wsURL string
	var queryErr error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		port := devToolsActivePort(dataDir)
		if port == "" {
			continue
		}
		wsURL, queryErr = queryWebSocketURL(ctx, cdpEndpoint(port))
		if queryErr == nil && wsURL != "" {
			break
		}
	}
	if wsURL == "" {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("failed to connect to the chrome CDP endpoint: %v", queryErr)
	}

	return connectCDP(ctx, wsURL, cmd, "")
}

// launchArgs are the arguments the clone starts Chrome with.
//
// The reference's own launch arguments are "--headless=new" (unless the
// local Chrome runs with a window), "--no-first-run", "--user-data-dir="
// and "--remote-debugging-port=0" 【strings — the 1.3.4 strings dump】.
func launchArgs(cfg Config, dataDir string) []string {
	args := []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--remote-debugging-port=0",
		"--user-data-dir=" + dataDir,
	}
	if cfg.Headless {
		args = append(args, "--headless=new")
	}
	args = append(args, "about:blank")
	if runtime.GOOS == "linux" {
		args = append(args, "--no-sandbox", "--disable-dev-shm-usage")
	}
	return args
}

// cdpEndpoint is the DevTools HTTP endpoint of a Chrome on a port.
func cdpEndpoint(port string) string {
	return "http://127.0.0.1:" + port
}

// devToolsActivePort reads the port Chrome wrote into the profile's
// DevToolsActivePort file.
func devToolsActivePort(dataDir string) string {
	b, err := os.ReadFile(filepath.Join(dataDir, "DevToolsActivePort"))
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// browserProfileDir is the profile the clone's own Chrome uses.
//
// The reference's browser keeps a persistent profile ("Browser starts
// automatically on first tool call using a persistent Chrome profile" —
// the chrome-devtools-mcp skill the reference ships 【一手】), so the
// clone's does too: one directory under the user's cache, and the
// browser's state — cookies, logins — survives a run.
//
// The reference's `--local_chrome_user_data_dir` names the profile
// instead ("Chrome user data directory." — its own usage string
// 【strings】); an empty value keeps the clone's own.
func browserProfileDir(dir string) (string, error) {
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("locate the user cache dir: %w", err)
		}
		dir = filepath.Join(base, "pigasus", "browser-profile")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create the browser profile dir: %w", err)
	}
	return dir, nil
}

// CheckBrowser reports whether the browser tools could run: the local
// Chrome is listening, or a browser binary exists to start.
//
// It starts nothing. The browser starts on the first tool call — the
// reference's rule — so a caller that only asks "could a browser work?"
// must not start one.
func CheckBrowser(ctx context.Context) error {
	cfg := CurrentConfig()
	// The Chrome that is already running is the one the clone would
	// drive, so it answers the question first.
	if _, _, err := resolveRunningChrome(ctx, cfg); err == nil {
		return nil
	}
	if attachRequested(cfg) {
		return fmt.Errorf("%w: no Chrome is listening on port %d — start one with --remote-debugging-port=%d, or turn remote debugging on for the running Chrome at chrome://inspect/#remote-debugging, or drop --use_local_chrome to let the clone start its own",
			ErrBrowserUnavailable, cfg.Port, cfg.Port)
	}
	if findChromeBinary() == "" {
		return ErrBrowserUnavailable
	}
	return nil
}

// Shutdown stops the singleton client and the Chrome it started.
//
// The clone starts the browser itself, so the browser has to stop with
// the clone: a headless Chrome left behind holds its profile and its
// port. The command calls this before it exits.
func Shutdown() {
	clientMu.Lock()
	c := defaultClient
	defaultClient = nil
	clientMu.Unlock()
	if c != nil {
		_ = c.Close()
	}
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

// connectCDP attaches the CDP transport to a running Chrome.
//
// The transport is chromedp: it owns the websocket, the message ids and the
// event pump, and it attaches to a page so that the page-scoped commands
// (Page.*, Runtime.*, Input.*) carry a session id. Chrome rejects those on
// the browser connection.
//
// A port means "a Chrome that was already running": the clone opens a tab
// of its own in it and drives that one. The session is the user's — it is
// the user's profile the Chrome runs — but the tabs the user has open are
// theirs, and the agent does not navigate them away. Without a port the
// clone started the Chrome itself, and the tab it starts with is already
// its own.
func connectCDP(ctx context.Context, wsURL string, cmd *exec.Cmd, port string) (*Client, error) {
	// The browser connection has to outlive the turn that opened it, so it is
	// rooted in its own context, not in the caller's.
	base, cancelBase := context.WithCancel(context.Background())
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(base, wsURL)

	// The tab the clone opened in the user's Chrome, if it opened one.
	var ownTab target.ID
	if port != "" {
		if id, err := createTab(ctx, port); err == nil {
			ownTab = id
		}
	}

	var cctx context.Context
	var cancelCtx context.CancelFunc
	if ownTab != "" {
		cctx, cancelCtx = chromedp.NewContext(allocCtx, chromedp.WithTargetID(ownTab))
	} else {
		cctx, cancelCtx = chromedp.NewContext(allocCtx)
	}

	cleanup := func() {
		cancelCtx()
		cancelAlloc()
		cancelBase()
	}

	// chromedp.Run dials the browser and attaches to a page. It waits for
	// Chrome to offer one, so it runs off the caller's goroutine and the
	// caller decides how long to wait.
	runErr := make(chan error, 1)
	go func() { runErr <- chromedp.Run(cctx) }()
	select {
	case err := <-runErr:
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("attach chrome cdp (%s): %w", wsURL, err)
		}
	case <-time.After(attachTimeout):
		cleanup()
		return nil, fmt.Errorf("attach chrome cdp (%s): no page within %s", wsURL, attachTimeout)
	}

	cc := chromedp.FromContext(cctx)
	if cc == nil || cc.Browser == nil || cc.Target == nil {
		cleanup()
		return nil, fmt.Errorf("attach chrome cdp (%s): no target attached", wsURL)
	}

	c := &Client{
		browser: cc.Browser,
		target:  cc.Target,
		cmd:     cmd,
		cancel:  cleanup,
		tabID:   ownTab,
	}

	// The console log and the network log are the two event streams the
	// browser tools read.
	chromedp.ListenTarget(cctx, c.handleEvent)

	return c, nil
}

// createTab opens a tab of its own in a Chrome that is already running.
//
// The DevTools HTTP endpoint is the way in: a PUT to /json/new asks the
// browser for a new tab and names its target id. A browser that refuses
// the request is not a failure — the caller falls back to the tab the
// browser offers by itself.
func createTab(ctx context.Context, port string) (target.ID, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, cdpEndpoint(port)+"/json/new?about:blank", nil)
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
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	if v.ID == "" {
		return "", errors.New("the browser named no target")
	}
	return target.ID(v.ID), nil
}

// Call sends a CDP command and awaits its result.
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	closed, browser, target := c.closed, c.browser, c.target
	c.mu.Unlock()

	if closed {
		return nil, errors.New("cdp client is closed")
	}
	if browser == nil || target == nil {
		return nil, errors.New("cdp client is not attached")
	}

	res, err := c.execute(ctx, c.executorFor(method), method, params)
	if err != nil && !browserScoped(method) && isUnknownMethod(err) {
		// The page session does not carry that domain; the browser connection
		// may. One retry, on the other connection.
		res, err = c.execute(ctx, browser, method, params)
	}
	if err != nil {
		return nil, cdpError(method, err)
	}
	return res, nil
}

// executorFor picks the connection a CDP domain lives on.
func (c *Client) executorFor(method string) cdp.Executor {
	if browserScoped(method) {
		return c.browser
	}
	return c.target
}

func (c *Client) execute(ctx context.Context, executor cdp.Executor, method string, params any) (json.RawMessage, error) {
	var res json.RawMessage
	err := cdp.Execute(cdp.WithExecutor(ctx, executor), method, params, &res)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// Chrome answered without a result member.
			return nil, nil
		}
		return nil, err
	}
	return res, nil
}

// browserScoped reports whether a CDP method belongs to the browser
// connection rather than to a page session.
func browserScoped(method string) bool {
	domain, _, ok := strings.Cut(method, ".")
	if !ok {
		return false
	}
	return browserScopedDomains[domain]
}

// isUnknownMethod reports whether Chrome said it does not know the method.
func isUnknownMethod(err error) bool {
	var cdpErr *cdproto.Error
	if !errors.As(err, &cdpErr) {
		return false
	}
	return strings.Contains(cdpErr.Message, "wasn't found")
}

// cdpError renders a protocol error the way the client always rendered it.
func cdpError(method string, err error) error {
	var cdpErr *cdproto.Error
	if errors.As(err, &cdpErr) {
		return fmt.Errorf("cdp %s: %s (code %d)", method, cdpErr.Message, cdpErr.Code)
	}
	return err
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

// handleEvent records the two event streams the browser tools read.
func (c *Client) handleEvent(ev any) {
	switch e := ev.(type) {
	case *cdproto.Message:
		// A command response, not an event.
		return
	case *cdpruntime.EventConsoleAPICalled:
		var parts []string
		for _, a := range e.Args {
			if a == nil {
				continue
			}
			parts = append(parts, fmt.Sprint(remoteValue(a.Value)))
		}
		c.appendConsole(fmt.Sprintf("[%s] %s", strings.ToUpper(string(e.Type)), strings.Join(parts, " ")))
	case *log.EventEntryAdded:
		if e.Entry == nil {
			return
		}
		c.appendConsole(fmt.Sprintf("[%s] %s", strings.ToUpper(string(e.Entry.Level)), e.Entry.Text))
	case *network.EventRequestWillBeSent:
		if e.Request == nil {
			return
		}
		c.appendNetwork(NetworkRequest{
			RequestID: string(e.RequestID),
			URL:       e.Request.URL,
			Method:    e.Request.Method,
			Type:      string(e.Type),
		})
	case *network.EventResponseReceived:
		if e.Response == nil {
			return
		}
		c.setNetworkStatus(string(e.RequestID), int(e.Response.Status))
	}
}

// remoteValue decodes a console argument the way the console log always
// rendered it: as the value itself, not as its JSON text.
func remoteValue(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

func (c *Client) appendConsole(line string) {
	c.consoleMu.Lock()
	defer c.consoleMu.Unlock()
	c.consoleLogs = append(c.consoleLogs, line)
}

func (c *Client) appendNetwork(req NetworkRequest) {
	c.networkMu.Lock()
	defer c.networkMu.Unlock()
	c.networkReqs = append(c.networkReqs, req)
}

func (c *Client) setNetworkStatus(requestID string, status int) {
	c.networkMu.Lock()
	defer c.networkMu.Unlock()
	for i := range c.networkReqs {
		if c.networkReqs[i].RequestID == requestID {
			c.networkReqs[i].Status = status
			break
		}
	}
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

// Close terminates the client and cleans up the child process.
//
// The profile directory is *not* removed: it is the browser's persistent
// profile, and it outlives the browser on purpose.
//
// The tab the clone opened in the user's own Chrome is closed again: the
// session was the user's, but the tab is the clone's.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cancel := c.cancel
	cmd := c.cmd
	browser := c.browser
	tabID := c.tabID
	c.mu.Unlock()

	// The tab goes before the session does, while the browser connection
	// can still talk to the browser.
	if tabID != "" && browser != nil {
		ctx, cancelTab := context.WithTimeout(context.Background(), 5*time.Second)
		_ = target.CloseTarget(tabID).Do(cdp.WithExecutor(ctx, browser))
		cancelTab()
	}

	// The cancel closes the session (and with it the tab chromedp opened)
	// before the process goes away.
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return nil
}
