// Package browserbridge speaks the Open Claude in Chrome bridge protocol.
//
// What it is
// ----------
// The Open Claude in Chrome project (the sibling checkout
// ../open-claude-in-chrome) is a Chrome extension plus a native-messaging
// host that lets an agent drive the browser the *user already has open* —
// their own window, their own tabs, their own logins. The clone's own CDP
// client cannot do that: it drives a Chrome it starts itself, with a
// profile of its own.
//
// The bridge's wire protocol
// --------------------------
// Three processes, one port (127.0.0.1:18765 by default), newline-
// delimited JSON in both directions:
//
//	extension  ⇄  native messaging (4-byte LE length prefix + JSON)
//	          ⇄  native host (host/native-host.js) — a dumb pipe
//	          ⇄  TCP 127.0.0.1:18765
//	          ⇄  the primary (host/tool-runtime.js) — or this package
//
// The primary owns the port and is what the extension's native host
// connects to; a *client* is any other process that joins it. The messages
// the primary and the clients exchange are:
//
//	{"type":"client_hello"}            → {"type":"client_ack","clientId":"N"}
//	{"type":"tool_request","id":…,"tool":…,"args":{…}}
//	                                 → {"id":…,"type":"tool_response","result":…}
//	                                 → {"id":…,"type":"tool_error","error":"…"}
//
// So a client is: connect, say hello, then ask for tools. The primary is:
// listen, wait for the native host, then ask for tools.
//
// The clone takes both roles
// --------------------------
// If something already serves the bridge port — the project's own MCP
// server, another agent — the clone joins it as a *client* and leaves that
// server alone. If the port is free, the clone takes it and becomes the
// *primary*: the extension's native host retries every 1.5s, so it finds
// the clone within a second or two and the browser becomes drivable
// without a Node process and without an MCP server anywhere.
//
// The primary role also serves the project's own clients: a tool request
// from another client is forwarded to the native host and its answer is
// routed back, so taking the port does not break a Claude Code session
// that starts later.
//
// Provenance
// ----------
// The protocol above is the project's own (../open-claude-in-chrome/host/
// tool-runtime.js and native-host.js); it is an interface, and this
// package is an independent implementation of it — the project is
// PolyForm Noncommercial, so none of its code is copied here.
package browserbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// DefaultHost is the address the bridge serves on.
const DefaultHost = "127.0.0.1"

// DefaultPort is the port the bridge serves on.
//
// The project's own default (host/tool-runtime.js: DEFAULT_PORT), and the
// port its native host connects to.
const DefaultPort = 18765

// DefaultTimeout bounds one tool call.
const DefaultTimeout = 60 * time.Second

// nativeHostGrace is how long a fresh connection may stay silent before it
// is taken for the native host.
//
// The project's own runtime uses the same rule (500ms): a client announces
// itself with `client_hello`, and the native host says nothing until the
// extension has something to say, so silence means "native host".
const nativeHostGrace = 500 * time.Millisecond

// nativeHostWait is how long a call waits for the extension to attach.
const nativeHostWait = 5 * time.Second

// ackTimeout is how long the greeting waits for the bridge's answer.
const ackTimeout = 5 * time.Second

// promotionDelay is how long a client waits before it tries to take the
// port after the primary's connection closed.
//
// The project's own client waits the same two seconds
// (`host/tool-runtime.js`: the promotion is scheduled on the primary's
// socket close, after a 2000 ms delay).
const promotionDelay = 2 * time.Second

// reconnectTimeout bounds one re-dial after a promotion attempt failed.
const reconnectTimeout = 5 * time.Second

// Config is the bridge's connection policy.
type Config struct {
	// Host is the address the bridge serves on.
	Host string
	// Port is the port the bridge serves on.
	Port int
	// Timeout bounds one tool call.
	Timeout time.Duration
}

// DefaultConfig returns the policy the clone uses when no flag says
// otherwise: the project's own port, and the project's own config file
// when it names another one.
func DefaultConfig() Config {
	return Config{
		Host:    DefaultHost,
		Port:    bridgePort(),
		Timeout: DefaultTimeout,
	}
}

// bridgePort reads the port from the bridge's own config file.
//
// The project keeps it at ~/.config/open-claude-in-chrome/config.json
// ({"port": …}); its default is DefaultPort.
func bridgePort() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultPort
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "open-claude-in-chrome", "config.json"))
	if err != nil {
		return DefaultPort
	}
	var cfg struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil || cfg.Port <= 0 {
		return DefaultPort
	}
	return cfg.Port
}

// request is a tool request on the wire.
type request struct {
	Type string          `json:"type"`
	ID   string          `json:"id"`
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args,omitempty"`
}

// response is a tool answer (or an error) on the wire.
type response struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// errNoNativeHost is the answer when the extension is not attached.
//
// The extension has to be installed, enabled and running in the browser
// the user is using; nothing on this side can make it appear.
var errNoNativeHost = errors.New("the Open Claude in Chrome extension is not connected: start Chrome with the extension installed and enabled")

// Client is a connection to the bridge.
//
// It is either a client (the port was already served) or a primary (the
// clone took the port); both roles answer the same Call.
type Client struct {
	cfg Config

	mu       sync.Mutex
	ln       net.Listener
	native   net.Conn
	clients  map[net.Conn]struct{}
	forward  map[string]forwarded
	pending  map[string]chan response
	seq      int
	clientNo int
	closed   bool

	// noNativeUntil is how long the client stops waiting for the
	// extension's native host.
	//
	// The native host retries every 1.5s, so a browser that is not
	// running is not a browser that is coming. Waiting the full grace on
	// every call would make every call slow; so one miss is remembered
	// for a while.
	noNativeUntil time.Time

	ack chan response
}

// forwarded is a client request the primary passed on to the native host.
type forwarded struct {
	conn net.Conn
	id   string
}

// Connect opens the bridge.
//
// It prefers the client role: when something already serves the port, the
// clone joins it and leaves that server alone. When the port is free, the
// clone takes it and becomes the primary — the role the extension's native
// host connects to.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}
	if cfg.Port <= 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		c := newClient(cfg)
		c.mu.Lock()
		c.ln = ln
		c.mu.Unlock()
		go c.serve(ln)
		return c, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, fmt.Errorf("listen on the bridge port %s: %w", addr, err)
	}
	return dial(ctx, addr, cfg)
}

func newClient(cfg Config) *Client {
	return &Client{
		cfg:     cfg,
		clients: map[net.Conn]struct{}{},
		forward: map[string]forwarded{},
		pending: map[string]chan response{},
		ack:     make(chan response, 1),
	}
}

// dial joins an existing bridge as a client.
func dial(ctx context.Context, addr string, cfg Config) (*Client, error) {
	c := newClient(cfg)
	if err := c.join(ctx, addr); err != nil {
		return nil, err
	}
	return c, nil
}

// serve accepts connections in the primary role.
func (c *Client) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go c.handle(conn)
	}
}

// handle takes a fresh connection and works out what it is.
//
// A client says `client_hello`; the native host says nothing until the
// extension has something to say. So: a hello makes it a client, and
// silence past the grace makes it the native host.
func (c *Client) handle(conn net.Conn) {
	lines := c.lines(conn)
	select {
	case line, ok := <-lines:
		if !ok {
			conn.Close()
			return
		}
		if isClientHello(line) {
			c.serveClient(conn, lines)
			return
		}
		// The native host, speaking first.
		c.attachNative(conn)
		c.dispatch(conn, line)
		go c.pump(conn, lines)
	case <-time.After(nativeHostGrace):
		// Silence: the extension's native host.
		c.attachNative(conn)
		go c.pump(conn, lines)
	}
}

// lines reads a connection's messages into a channel.
//
// One reader per connection, so no line is ever read twice and none is
// lost to a buffer that another goroutine owns.
func (c *Client) lines(conn net.Conn) <-chan string {
	out := make(chan string, 16)
	br := bufio.NewReader(conn)
	go func() {
		defer close(out)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				out <- string(line)
			}
			if err != nil {
				return
			}
		}
	}()
	return out
}

// serveClient handles another process that joined as a client.
//
// Its tool requests are forwarded to the native host with a prefixed id,
// and the answers are routed back with the original id — the project's own
// multiplexing, so taking the port does not break a client that starts
// later.
func (c *Client) serveClient(conn net.Conn, lines <-chan string) {
	c.mu.Lock()
	c.clientNo++
	id := c.clientNo
	c.clients[conn] = struct{}{}
	c.mu.Unlock()

	_ = c.write(conn, map[string]any{"type": "client_ack", "clientId": strconv.Itoa(id)})

	for line := range lines {
		c.fromClient(conn, id, line)
	}
	c.mu.Lock()
	delete(c.clients, conn)
	c.mu.Unlock()
	conn.Close()
}

// fromClient handles one message from a client.
func (c *Client) fromClient(conn net.Conn, clientNo int, line string) {
	var req request
	if err := json.Unmarshal([]byte(trimLine(line)), &req); err != nil {
		return
	}
	if req.Type != "tool_request" || req.ID == "" {
		return
	}
	c.mu.Lock()
	native := c.native
	c.mu.Unlock()
	if native == nil {
		_ = c.write(conn, map[string]any{
			"id":    req.ID,
			"type":  "tool_error",
			"error": "the Open Claude in Chrome extension is not connected",
		})
		return
	}
	id := fmt.Sprintf("c%d_%s", clientNo, req.ID)
	c.mu.Lock()
	c.forward[id] = forwarded{conn: conn, id: req.ID}
	c.mu.Unlock()
	if err := c.write(native, request{Type: "tool_request", ID: id, Tool: req.Tool, Args: req.Args}); err != nil {
		c.mu.Lock()
		delete(c.forward, id)
		c.mu.Unlock()
	}
}

// attachNative registers the extension's native host.
//
// Only one browser profile drives the bridge at a time: the project's own
// runtime answers a second native host with an error and drops it, and so
// does the clone.
func (c *Client) attachNative(conn net.Conn) {
	c.mu.Lock()
	busy := c.native != nil
	if !busy {
		c.native = conn
		c.noNativeUntil = time.Time{}
	}
	c.mu.Unlock()
	if busy {
		_ = c.write(conn, map[string]any{
			"type":  "error",
			"error": "Another browser profile is already connected.",
		})
		conn.Close()
	}
}

// pump consumes a connection's messages until it closes.
func (c *Client) pump(conn net.Conn, lines <-chan string) {
	for line := range lines {
		c.dispatch(conn, line)
	}
	c.mu.Lock()
	wasLink := c.native == conn
	if wasLink {
		c.native = nil
	}
	isClient := c.ln == nil
	closed := c.closed
	c.mu.Unlock()
	conn.Close()
	if wasLink && isClient && !closed {
		// The primary is gone. The project's own client promotes itself
		// here; without that, every later call in this process fails
		// with "the extension is not connected", for good.
		go c.promote()
	}
}

// promote is the client's own answer to a dead primary.
//
// Two seconds after the primary's socket closes, the client tries to
// take the port itself — that is the promotion, and it is what the
// extension's native host will find when it retries. If another
// process took the port first, the client joins that one instead,
// which is the same order the project's runtime uses.
//
// The primary role never promotes: there the lost connection is the
// extension's native host, and the primary waits for it to come back.
func (c *Client) promote() {
	time.Sleep(promotionDelay)

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}

	addr := net.JoinHostPort(c.cfg.Host, strconv.Itoa(c.cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		c.mu.Lock()
		c.ln = ln
		c.mu.Unlock()
		go c.serve(ln)
		return
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return
	}

	// Somebody else won the race: join them.
	ctx, cancel := context.WithTimeout(context.Background(), reconnectTimeout)
	defer cancel()
	_ = c.join(ctx, addr)
}

// join connects to an existing bridge as a client, on this client.
func (c *Client) join(ctx context.Context, addr string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to the bridge on %s: %w", addr, err)
	}
	c.mu.Lock()
	c.native = conn
	c.mu.Unlock()
	go c.pump(conn, c.lines(conn))

	// The client announces itself; the bridge answers with an ack.
	if err := c.write(conn, map[string]any{"type": "client_hello"}); err != nil {
		conn.Close()
		return fmt.Errorf("greet the bridge on %s: %w", addr, err)
	}
	timer := time.NewTimer(ackTimeout)
	defer timer.Stop()
	select {
	case msg := <-c.ack:
		if msg.Type == "client_ack" {
			return nil
		}
	case <-timer.C:
		conn.Close()
		return fmt.Errorf("the bridge on %s did not answer the greeting", addr)
	case <-ctx.Done():
		conn.Close()
		return ctx.Err()
	}
	conn.Close()
	return fmt.Errorf("the bridge on %s did not answer the greeting", addr)
}

// dispatch routes one message.
func (c *Client) dispatch(_ net.Conn, line string) {
	line = trimLine(line)
	if line == "" {
		return
	}
	var msg response
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return
	}
	if msg.Type == "client_ack" {
		select {
		case c.ack <- msg:
		default:
		}
		return
	}
	if msg.ID == "" {
		return // a heartbeat, a recording event: not ours
	}

	c.mu.Lock()
	if f, ok := c.forward[msg.ID]; ok {
		delete(c.forward, msg.ID)
		c.mu.Unlock()
		out, _ := json.Marshal(map[string]any{
			"id":     f.id,
			"type":   msg.Type,
			"result": msg.Result,
			"error":  msg.Error,
		})
		_ = c.write(f.conn, json.RawMessage(out))
		return
	}
	ch, ok := c.pending[msg.ID]
	c.mu.Unlock()
	if ok {
		ch <- msg
	}
}

// write sends one message on a connection.
func (c *Client) write(conn net.Conn, msg any) error {
	if conn == nil {
		return errors.New("the bridge connection is not up")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = conn.Write(b)
	return err
}

// trimLine strips the newline and the surrounding space of a wire line.
func trimLine(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	return s
}

// isClientHello reports whether a line is the client's greeting.
func isClientHello(line string) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(trimLine(line)), &probe); err != nil {
		return false
	}
	return probe.Type == "client_hello"
}

// Call runs one tool on the bridge and returns its raw result.
//
// The result is the bridge's own JSON (the project's tools answer with an
// MCP-shaped content array); the caller renders it.
func (c *Client) Call(ctx context.Context, tool string, args json.RawMessage) (json.RawMessage, error) {
	if c == nil {
		return nil, errors.New("the browser bridge is not connected")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	args = coerceArgs(args)

	c.mu.Lock()
	c.seq++
	id := fmt.Sprintf("pigasus_%d", c.seq)
	conn := c.native
	c.mu.Unlock()

	if conn == nil {
		// The primary waits for the extension's native host to attach.
		c.mu.Lock()
		until := c.noNativeUntil
		c.mu.Unlock()
		if time.Now().Before(until) {
			return nil, errNoNativeHost
		}
		conn = c.waitNative(ctx)
		if conn == nil {
			c.mu.Lock()
			c.noNativeUntil = time.Now().Add(nativeHostGrace * 60)
			c.mu.Unlock()
			return nil, errNoNativeHost
		}
	}

	ch := make(chan response, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(conn, request{Type: "tool_request", ID: id, Tool: tool, Args: args}); err != nil {
		return nil, fmt.Errorf("send %s to the browser bridge: %w", tool, err)
	}

	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case msg := <-ch:
		if msg.Type == "tool_error" {
			return nil, errors.New(msg.Error)
		}
		return msg.Result, nil
	case <-timer.C:
		return nil, fmt.Errorf("%s timed out after %s", tool, timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// waitNative waits for the extension's native host to attach.
func (c *Client) waitNative(ctx context.Context) net.Conn {
	deadline := time.Now().Add(nativeHostWait)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		conn := c.native
		c.mu.Unlock()
		if conn != nil {
			return conn
		}
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

// Addr returns the address the bridge connection is on.
//
// In the client role it is the address of the bridge that answered; in
// the primary role it is the address the clone listens on.
func (c *Client) Addr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.native != nil {
		return c.native.LocalAddr().String()
	}
	if c.ln != nil {
		return c.ln.Addr().String()
	}
	return ""
}

// Close ends the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	ln := c.ln
	native := c.native
	clients := make([]net.Conn, 0, len(c.clients))
	for conn := range c.clients {
		clients = append(clients, conn)
	}
	c.mu.Unlock()

	if ln != nil {
		ln.Close()
	}
	if native != nil {
		native.Close()
	}
	for _, conn := range clients {
		conn.Close()
	}
	return nil
}
