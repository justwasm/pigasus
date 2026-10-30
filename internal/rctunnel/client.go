package rctunnel

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
)

const (
	// DefaultWebChannelEndpoint is the cloud tunnel the remote-control
	// view connects to.
	//
	// It is the compiled-in default. The live endpoint is
	// endpoints.Current().WebChannel — the configuration file may point
	// the tunnel at a local service.
	DefaultWebChannelEndpoint = endpoints.DefaultWebChannel
)

// DefaultClientVersion is the version string the instance
// advertises to the remote service in its registration metadata.
//
// The remote service uses it to pick the web bundle it serves to
// the browser: the dashboard's "Connect" link opens an iframe
// whose src is "/web/<version>/?iframe=true". A CLI instance
// therefore has to advertise the CLI's own bundle, not the IDE's
// — the two bundles speak different dialects of the language
// server API.
//
// 【一手】 2026-10-26, live: the reference CLI registers
// ["gear.tailac191.ts.net","cli-1.4.7-v2"] and its remote view
// loads "https://antigravity.static.usercontent.goog/web/cli-1.4.7/?iframe=true"
// and renders the full UI. An instance advertising the IDE's
// "2.24.0-v2" is served "/web/2.24.0/" instead, and that bundle
// subscribes to the Jetbox server streams; the cloud answers those
// with HTTP 400 and the page retries the subscription forever, so
// the view never leaves "Connecting to instance...".
//
// The reference builds the string from its own version — the 1.4.8 binary
// carries no "cli-1.4.8-v2" literal, only the "-v2" fragment — so the clone
// derives it too instead of hard-coding a release that goes stale. 【一手】
// the bundle both releases select is served: a GET of
// "https://antigravity.static.usercontent.goog/web/cli-1.4.8/?iframe=true"
// answered 200 with the same 4,453 bytes as the 1.4.7 one (2026-10-27).
var DefaultClientVersion = "cli-" + cli.ReferenceVersion + "-v2"

// RPCHandler handles inbound RPC requests from the Google Cloud Tunnel.
type RPCHandler interface {
	DispatchDirect(ctx context.Context, method string, payloadText string) (string, error)
}

// ClientOptions configures the pure-Go WebChannel cloud tunnel client.
type ClientOptions struct {
	Endpoint     string
	InstanceName string
	InstanceID   string
	Version      string
	Account      string
	Dispatcher   RPCHandler
	// UpdateInstanceEndpoint is the URL the instance-rename call posts
	// to. It is the tunnel's own host with a different RPC path, so it
	// is derived from Endpoint when it is not given.
	UpdateInstanceEndpoint string
	// Handler serves the inbound proxy requests. When it is set the
	// tunnel builds an `http.Request` out of every inbound
	// `ProxyRequestV2` and serves it against the local HTTP handler —
	// the reference's architecture. When it is nil the tunnel falls
	// back to dispatching the last path segment as an RPC method.
	Handler   http.Handler
	LogWriter io.Writer
	// DebugForward logs every successful forward-channel POST, with its
	// parameters and the cloud's answer. The forward channel is how the
	// instance's answers reach the cloud; a rejected POST is the reason
	// the browser's probe never completes, so the parameters are worth
	// having in the log.
	DebugForward bool
}

// Client maintains a bidirectional WebChannel tunnel connection to Google Cloud Tunnel
// (jetski-webchannel.googleapis.com) without requiring any external binaries.
type Client struct {
	opts       ClientOptions
	httpClient *http.Client

	mu       sync.Mutex
	sid      string
	gsession string
	sess     connectHandshake
	lastAID  atomic.Int64
	reqSeq   atomic.Int64
	ofsSeq   atomic.Int64

	cancel context.CancelFunc

	// sessionCancel ends the current session's poll.
	//
	// The cloud disowns a session from under the client — the forward
	// channel then answers 400 "Unknown SID" — and a disowned session
	// never comes back: only a fresh handshake and a fresh registration
	// do. The poll has to end with it, otherwise the client keeps
	// posting into a session the cloud has forgotten.
	sessionCancel context.CancelFunc
	deadSession   atomic.Bool
}

// NewClient constructs a new WebChannel tunnel Client.
func NewClient(opts ClientOptions) *Client {
	set := endpoints.Current()
	if opts.Endpoint == "" {
		opts.Endpoint = set.WebChannel
	}
	if opts.Version == "" {
		opts.Version = DefaultClientVersion
	}
	if opts.InstanceID == "" {
		opts.InstanceID = NewInstanceID()
	}
	if opts.UpdateInstanceEndpoint == "" {
		opts.UpdateInstanceEndpoint = set.UpdateInstanceURL()
		if opts.Endpoint != set.WebChannel {
			// An explicitly given tunnel endpoint wins: the rename
			// goes to the host the tunnel itself talks to, so a local
			// mock answers it on the same port.
			opts.UpdateInstanceEndpoint = endpoints.UpdateInstanceURLFor(opts.Endpoint)
		}
	}
	c := &Client{
		opts: opts,
		httpClient: &http.Client{
			Timeout: 0, // Streaming requests require no overall timeout
		},
	}
	c.reqSeq.Store(800)
	return c
}

// NewInstanceID mints the instance id the tunnel registers with the
// remote service. The host mints it before the dispatcher is built so
// the local server can answer GetRemoteControlInfo with the same id
// the registration carried.
//
// 【一手】 the reference registers the bare "<uuid>-v2": its captured
// registration body carries "instanceId":"0d159523-6e02-4362-abab-93e0c69edea4-v2"
// and the cloud's instance registry lists it under that id. The
// "remote-control-" in the reference's log lines is its session tag,
// not part of the id.
func NewInstanceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x-v2", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// InstanceID returns the unique instance ID registered with the tunnel.
func (c *Client) InstanceID() string {
	return c.opts.InstanceID
}

// InstanceName returns the instance name advertised to antigravity.google.com.
func (c *Client) InstanceName() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opts.InstanceName
}

// SetInstanceName changes the name the tunnel registers with.
//
// A rename travels to the cloud over the API, but a reconnect
// re-registers the instance — and the re-registration carries the name
// the client holds. So a successful rename has to update it here, or
// the next reconnect would put the old name back on the dashboard.
func (c *Client) SetInstanceName(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opts.InstanceName = name
}

func (c *Client) log(format string, a ...any) {
	if c.opts.LogWriter != nil {
		ts := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(c.opts.LogWriter, "[%s] [CloudTunnel] "+format+"\n", append([]any{ts}, a...)...)
	}
}

// healthySession is how long a session has to live before a drop counts
// as a healthy end rather than a failure.
//
// 【strings】 the reference words the same judgement: "Connection ended
// quickly (lasted 49.715804167s). Treating as failure for backoff." —
// so its threshold is above 49.7 s. 60 s is this clone's reading of it.
const healthySession = 60 * time.Second

// Run executes the tunnel event loop, maintaining the connection and dispatching RPCs
// until ctx is canceled.
func (c *Client) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	defer cancel()

	backoff := 2 * time.Second
	const maxBackoff = 15 * time.Second
	attempt := 0

	for {
		select {
		case <-ctx.Done():
			c.log("tunnel run loop stopped: %v", ctx.Err())
			return ctx.Err()
		default:
		}

		started := time.Now()
		err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// The reference words the end of a connection the way the
		// clone now words it, and in the same order:
		//
		//	Connection ended quickly (lasted 49.715804167s). Treating as
		//	failure for backoff.
		//	Reconnection attempt 0 failed. Retrying in 2s...
		//	Connection ended with error: context canceled
		//
		// and, for a session that lived long enough:
		//
		//	Connection ended. Retrying immediately...
		//
		// 【一手】 2026-10-26, live (reference log, cli-20261022_025317.log
		// and cli-20261026_044255.log).
		//
		// A session that lived long enough did not fail: the backoff
		// starts over.
		if time.Since(started) >= healthySession {
			backoff = 2 * time.Second
			c.log("Connection ended. Retrying immediately...")
		} else {
			c.log("Connection ended quickly (lasted %s). Treating as failure for backoff.",
				time.Since(started).Round(time.Millisecond))
		}
		c.log("Reconnection attempt %d failed. Retrying in %v...", attempt, backoff)
		c.log("Connection ended with error: %v", err)

		attempt++
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connectAndServe handles one full connection session: handshake -> registration -> stream read loop.
func (c *Client) connectAndServe(ctx context.Context) error {
	tok, err := c.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("auth token: %w", err)
	}

	// 1. Initial Handshake
	sid, gsession, err := c.handshake(ctx, tok)
	if err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	c.mu.Lock()
	c.sid = sid
	c.gsession = gsession
	c.lastAID.Store(0)
	c.ofsSeq.Store(0)
	c.mu.Unlock()
	c.log("connected session sid=%s gsession=%s", sid, gsession)

	// 2. Start BackChannel GET stream
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	c.mu.Lock()
	c.sessionCancel = streamCancel
	c.deadSession.Store(false)
	c.mu.Unlock()

	bcResp, err := c.openBackChannel(streamCtx, tok, sid, gsession)
	if err != nil {
		return fmt.Errorf("open backchannel: %w", err)
	}
	defer bcResp.Body.Close()

	if bcResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(bcResp.Body, 512))
		return fmt.Errorf("backchannel HTTP %d: %s", bcResp.StatusCode, string(body))
	}

	// 3. Send registration via ForwardChannel
	instanceName := c.InstanceName()
	regReq := ConnectInstanceRequestV2{
		Metadata: &InstanceMetadata{
			Name:             instanceName,
			Version:          c.opts.Version,
			SupportsChunking: true,
		},
		InstanceID:          c.opts.InstanceID,
		SupportsPayloadText: true,
	}
	regBytes, err := json.Marshal(regReq)
	if err != nil {
		return fmt.Errorf("marshal registration: %w", err)
	}

	regBody, err := c.sendForward(ctx, tok, string(regBytes))
	if err != nil {
		return fmt.Errorf("send registration: %w", err)
	}
	c.log("registered instance %q (id: %s)", instanceName, c.opts.InstanceID)
	if s := strings.TrimSpace(regBody); s != "" {
		// The cloud answers the forward channel with its own JSON even
		// when it refuses the registration, so the answer is the only
		// place a refusal shows up.
		c.log("registration answer: %q", regBody)
	}

	// 4. Read loop on BackChannel
	return c.pumpBackChannel(streamCtx, tok, sid, gsession, bcResp)
}

// maxBackchannelReopens bounds how often the client re-issues the
// backchannel GET on one session before it gives the session up and
// lets the caller re-handshake.
const maxBackchannelReopens = 5

// healthyPoll is how long a poll has to stay open before its end
// counts as the server's normal timeout rather than a failure.
//
// 【一手】 2026-10-26, live: the cloud closes the poll after ~47 s and
// the client re-opens it. The old code counted every one of those
// closes, so the sixth one — six minutes into a perfectly healthy
// session — ended the session and forced a re-handshake, and with it
// a new SID. The remote service saw a new instance every six minutes
// and the browser's channel to it was torn down each time.
//
// The reference never does this: its own log words the same judgement
// ("Connection ended quickly (lasted 49.715804167s). Treating as
// failure for backoff."), and 49.7 s is the length of a normal poll.
const healthyPoll = 30 * time.Second

// pumpBackChannel reads the backchannel until it ends and then
// re-opens it on the same session.
//
// 【strings】 the reference binary embeds the C++ WebChannel client
// (net/webchannel/client/internal/v8/webchannel_base.cc).  Its string
// table carries "back_channel_request_timeout_=", "Going to retry
// GET", "Use the retry request that is already scheduled." and "Not
// retrying due to error type": the poll ends, the client re-issues the
// GET and the session survives.
//
// The clone used to read the poll to its EOF and treat that as the end
// of the session, re-handshaking ~46 s later.  The remote service
// therefore saw a new session every 46 s, and the instance record in
// the dashboard's registry never left the Offline state.
//
// 【一手】 2026-10-26: the reference held one session for five minutes
// ("Connection status: Connected", no further line in the log) while
// the clone re-handshaked every ~46 s.
func (c *Client) pumpBackChannel(ctx context.Context, token, sid, gsession string, resp *http.Response) error {
	backoff := 200 * time.Millisecond
	const maxBackoff = 3 * time.Second
	openedAt := time.Now()
	for attempt := 0; ; attempt++ {
		err := c.readBackChannel(ctx, token, resp.Body)
		resp.Body.Close()
		// The cloud disowned the session while the poll was open: the
		// forward channel answers 400 and the poll has nothing left to
		// say. The session ends here, and the run loop hands the
		// instance a new one.
		if c.deadSession.Load() {
			return fmt.Errorf("the cloud disowned the session: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		// The cloud ended the session. Re-opening the poll on the same
		// SID would keep a session the cloud has already disowned: the
		// poll answers 200, the instance keeps answering the cloud's
		// GetStatus pings — and the dashboard's registry still lists
		// the instance as DISCONNECTED and refuses to open the app
		// iframe. Only a fresh handshake — a new SID and a new
		// registration — puts the instance back into the registry.
		if errors.Is(err, errServerClosed) {
			return err
		}
		// A poll that stayed open long enough ended for the server's
		// own reason. That is not a failure and must not count
		// towards the limit — only a poll that dies immediately,
		// over and over, is a session that is going nowhere.
		if time.Since(openedAt) >= healthyPoll {
			attempt = 0
		}
		if attempt >= maxBackchannelReopens {
			return fmt.Errorf("backchannel ended %d times (%v); giving up the session", attempt+1, err)
		}
		c.log("backchannel ended after %s (%v); reopening on the same session", time.Since(openedAt).Round(time.Second), err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		next, rerr := c.reopenBackChannel(ctx, token, sid, gsession)
		if rerr != nil {
			// A failed reopen is not a dead session. The cloud
			// answers a transient 400 and keeps the session, and
			// the reference retries the GET rather than handing
			// the session back: "Going to retry GET" and "Not
			// retrying due to error type" are both in its string
			// table. 【strings】
			//
			// 【一手】 2026-10-26, live: the clone used to give the
			// session up on the first failed reopen. The cloud's
			// browser-side channel is bound to the session's SID, so
			// a new session left the browser's in-flight requests
			// unanswered — the web UI waited for them forever and
			// the remote-control page never left "Connecting to
			// instance…". 861 responses were lost this way in one
			// day.
			next, rerr = c.retryReopenBackChannel(ctx, token, sid, gsession, rerr, reopenRetryWindow)
			if rerr != nil {
				return rerr
			}
		}
		resp = next
		openedAt = time.Now()
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// markSessionDead records that the cloud has disowned the session and
// ends the poll that is waiting on it.
//
// The client cannot recover a disowned session: the forward channel
// answers 400 "Unknown SID" and keeps doing so. The reference answers
// the same situation the same way — its connection ends and it starts a
// new one:
//
//	Connection ended. Retrying immediately...
//	Connection ended with error: context canceled
//	Starting V2 remote control connection to: https://jetski-webchannel.googleapis.com:443
//	Connection status: Connected
//
// 【一手】 2026-10-26, live (reference log, cli-20261026_170618.log).
func (c *Client) markSessionDead(reason string) {
	if !c.deadSession.CompareAndSwap(false, true) {
		return
	}
	c.log("the cloud disowned the session (%s); ending the session", reason)
	c.mu.Lock()
	cancel := c.sessionCancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// reopenRetryWindow is how long a failed reopen is retried before the
// session is given up.
//
// The poll itself ends every ~50 s and the reopen normally succeeds.
// A failure is the cloud's front end answering 400, and it clears on
// its own — so the reopen is retried for a minute before the session
// is handed back.
const reopenRetryWindow = time.Minute

// retryReopenBackChannel retries the reopen until it works, the window
// is over, or the client stops.
func (c *Client) retryReopenBackChannel(ctx context.Context, token, sid, gsession string, first error, window time.Duration) (*http.Response, error) {
	deadline := time.Now().Add(window)
	backoff := 200 * time.Millisecond
	err := first
	for {
		c.log("reopen backchannel failed (%v); retrying in %v...", err, backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		resp, rerr := c.reopenBackChannel(ctx, token, sid, gsession)
		if rerr == nil {
			return resp, nil
		}
		err = rerr
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("reopen backchannel failed for %s: %w", window, err)
		}
		backoff *= 2
		if backoff > 3*time.Second {
			backoff = 3 * time.Second
		}
	}
}

// reopenBackChannel re-issues the backchannel GET on the same session.
func (c *Client) reopenBackChannel(ctx context.Context, token, sid, gsession string) (*http.Response, error) {
	resp, err := c.openBackChannel(ctx, token, sid, gsession)
	if err != nil {
		return nil, fmt.Errorf("reopen backchannel: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		return nil, fmt.Errorf("reopen backchannel HTTP %d: %s", resp.StatusCode, string(b))
	}
	c.log("backchannel reopened on session sid=%s", sid)
	return resp, nil
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	if c.opts.Account != "" {
		if st, err := accounts.Load(); err == nil {
			if acc, ok := st.Find(c.opts.Account); ok {
				if tok, err := accounts.AccessToken(ctx, c.opts.Account, acc); err == nil && tok != nil && tok.Token.AccessToken != "" {
					return tok.Token.AccessToken, nil
				}
			}
		}
	}
	tok, err := auth.LoadTokenUnified()
	if err != nil {
		return "", err
	}
	if fresh, _, err := tok.RefreshIfNeeded(ctx); err == nil && fresh != nil && fresh.Token.AccessToken != "" {
		return fresh.Token.AccessToken, nil
	}
	if tok.Token.AccessToken != "" {
		return tok.Token.AccessToken, nil
	}
	return "", fmt.Errorf("empty access token")
}

func (c *Client) handshake(ctx context.Context, token string) (string, string, error) {
	rid := c.reqSeq.Add(1)
	hsURL := fmt.Sprintf("%s?VER=8&RID=%d&CVER=25&X-HTTP-Session-Id=gsessionid&t=1", c.opts.Endpoint, rid)
	req, err := http.NewRequestWithContext(ctx, "POST", hsURL, strings.NewReader("count=0"))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Go-http-client/1.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("handshake HTTP %d: %s", resp.StatusCode, string(b))
	}

	var gsession string
	if v := resp.Header.Get("X-Http-Session-Id"); v != "" {
		gsession = v
	}
	reader := bufio.NewReader(resp.Body)
	lenLine, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("read chunk len: %w", err)
	}
	var chunkLen int
	if _, err := fmt.Sscanf(strings.TrimSpace(lenLine), "%d", &chunkLen); err != nil || chunkLen <= 0 {
		return "", "", fmt.Errorf("invalid chunk length header %q", lenLine)
	}

	chunkBytes := make([]byte, chunkLen)
	if _, err := io.ReadFull(reader, chunkBytes); err != nil {
		return "", "", fmt.Errorf("read chunk body: %w", err)
	}

	sess, err := parseConnectHandshake(chunkBytes)
	if err != nil {
		return "", "", err
	}
	sid := sess.sid
	c.mu.Lock()
	c.sess = sess
	c.mu.Unlock()

	return sid, gsession, nil
}

// connectHandshake is what the server's handshake chunk states about
// the session it just opened.
//
// 【一手】 2026-10-26, live: the chunk is
//
//	[[0,["c","<sid>","",8,15,30000]]]
//
// that is: the "c" (connect) command, the session id, an empty
// string, the protocol version, the server version and the
// backchannel request timeout in milliseconds.
//
// 【strings】 the reference binary carries the three URL parameter
// names next to each other in its string table — "VER=",
// "SVER=", "back_channel_request_timeout_=" — so those are exactly
// the three numbers the handshake hands out, and the client is
// expected to send them back on the backchannel poll.
type connectHandshake struct {
	sid         string
	ver         int64
	sver        int64
	bcTimeoutMS int64
}

// parseConnectHandshake reads the connect command out of the
// handshake chunk.
//
// The command is found by shape, not by position: the live server
// wraps it in an aid-prefixed envelope ([[0,["c",…]]]) while the
// mock in the tests hands it over bare ([["c",…]]).
func parseConnectHandshake(chunk []byte) (connectHandshake, error) {
	var decoded any
	if err := json.Unmarshal(chunk, &decoded); err != nil {
		return connectHandshake{}, fmt.Errorf("parse handshake chunk: %w", err)
	}
	sess, ok := findConnectCommand(decoded)
	if !ok {
		return connectHandshake{}, fmt.Errorf("no connect command in handshake chunk: %s", chunk)
	}
	return sess, nil
}

// findConnectCommand walks the decoded chunk for the connect command.
func findConnectCommand(v any) (connectHandshake, bool) {
	switch t := v.(type) {
	case []any:
		if sess, ok := connectCommandOf(t); ok {
			return sess, true
		}
		for _, item := range t {
			if sess, ok := findConnectCommand(item); ok {
				return sess, true
			}
		}
	case map[string]any:
		for _, item := range t {
			if sess, ok := findConnectCommand(item); ok {
				return sess, true
			}
		}
	}
	return connectHandshake{}, false
}

// connectCommandOf reads a command array — ["c", <sid>, "", <ver>,
// <sver>, <timeout>] — when the array is one.
func connectCommandOf(fields []any) (connectHandshake, bool) {
	if len(fields) < 2 {
		return connectHandshake{}, false
	}
	kind, ok := fields[0].(string)
	if !ok || kind != "c" {
		return connectHandshake{}, false
	}
	sid, ok := fields[1].(string)
	if !ok || sid == "" {
		return connectHandshake{}, false
	}
	sess := connectHandshake{sid: sid}
	if len(fields) > 3 {
		sess.ver = jsonInt(fields[3])
	}
	if len(fields) > 4 {
		sess.sver = jsonInt(fields[4])
	}
	if len(fields) > 5 {
		sess.bcTimeoutMS = jsonInt(fields[5])
	}
	return sess, true
}

// jsonInt reads a JSON number as an int64, returning zero for
// anything that is not a number.
func jsonInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return i
	}
	return 0
}

func (c *Client) openBackChannel(ctx context.Context, token, sid, gsession string) (*http.Response, error) {
	aid := c.lastAID.Load()
	c.mu.Lock()
	sess := c.sess
	c.mu.Unlock()
	// The server states the protocol version, its own version and the
	// backchannel poll timeout in the handshake; the reference sends
	// all three back on the poll.
	bcURL := fmt.Sprintf("%s?VER=%d&SVER=%d&back_channel_request_timeout_=%d&RID=rpc&SID=%s&CI=0&AID=%d&TYPE=xmlhttp&gsessionid=%s&t=1",
		c.opts.Endpoint, sess.ver, sess.sver, sess.bcTimeoutMS, sid, aid, gsession)
	req, err := http.NewRequestWithContext(ctx, "GET", bcURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	return c.httpClient.Do(req)
}

func (c *Client) readBackChannel(ctx context.Context, token string, body io.Reader) error {
	reader := bufio.NewReader(body)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var chunkLen int
		if _, err := fmt.Sscanf(line, "%d", &chunkLen); err != nil || chunkLen <= 0 {
			continue
		}

		buf := make([]byte, chunkLen)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return err
		}

		if err := c.processInboundChunk(ctx, token, buf); err != nil {
			return err
		}
	}
}

// errServerClosed is what the cloud's "close" signal means.
//
// 【一手】 2026-10-26, live: the cloud sent the string "close" on the
// backchannel at 12:36:23.404 — the same second the instance's row in
// the dashboard's registry flipped to INSTANCE_STATUS_DISCONNECTED
// (status 2) and its statusLastUpdatedAt froze there for the rest of
// the day. The clone logged the signal and kept reading the same
// session: its polls reopened on the same SID (HTTP 200), it kept
// answering the cloud's GetStatus pings, and the dashboard still
// refused to open the app iframe, because the cloud had already
// disowned that session.
//
// The signal is not a keepalive and not a poll timeout: the cloud
// ends the session, and only a fresh handshake — a new SID and a new
// registration — puts the instance back into the registry. The clone
// used to ignore it, which is what left the instance row stuck in
// DISCONNECTED for hours.
var errServerClosed = errors.New("server signaled close")

func (c *Client) processInboundChunk(ctx context.Context, token string, buf []byte) error {
	var chunk [][]json.RawMessage
	if err := json.Unmarshal(buf, &chunk); err != nil {
		return nil
	}

	for _, elem := range chunk {
		if len(elem) < 2 {
			continue
		}

		var aid int64
		if err := json.Unmarshal(elem[0], &aid); err == nil {
			for {
				curr := c.lastAID.Load()
				if aid <= curr || c.lastAID.CompareAndSwap(curr, aid) {
					break
				}
			}
		}

		var items []json.RawMessage
		if err := json.Unmarshal(elem[1], &items); err != nil {
			continue
		}

		for _, item := range items {
			var str string
			if json.Unmarshal(item, &str) == nil {
				if str == "noop" {
					continue
				}
				if str == "close" {
					c.log("server signaled close; ending the session")
					return errServerClosed
				}
			}

			var resp ConnectInstanceResponseV2
			if err := json.Unmarshal(item, &resp); err == nil && resp.ProxyRequest != nil {
				go c.handleInboundProxyRequest(ctx, token, resp.ProxyRequest)
			}
		}
	}
	return nil
}

func (c *Client) sendForward(ctx context.Context, token, data string) (string, error) {
	c.mu.Lock()
	sid := c.sid
	gsession := c.gsession
	c.mu.Unlock()

	rid := c.reqSeq.Add(1)
	aid := c.lastAID.Load()
	ofs := c.ofsSeq.Add(1) - 1

	fcURL := fmt.Sprintf("%s?VER=8&SID=%s&RID=%d&AID=%d&gsessionid=%s&t=1",
		c.opts.Endpoint, sid, rid, aid, gsession)

	form := url.Values{}
	form.Set("count", "1")
	form.Set("ofs", fmt.Sprintf("%d", ofs))
	form.Set("req0___data__", data)

	req, err := http.NewRequestWithContext(ctx, "POST", fcURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Go-http-client/1.1")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		// A 4xx from the forward channel means the cloud does not know
		// this session any more. The session is gone for good: the
		// reference's transport ends the connection and starts a new
		// one ("Connection ended. Retrying immediately..."), and the
		// cloud only takes the instance back into the registry when a
		// new session registers.
		//
		// 【一手】 2026-10-26, live: the clone kept posting into a
		// disowned session, every answer came back 400 "Unknown SID",
		// and the cloud cancelled the browser's request ten seconds
		// later — "proxyRequest reqID=13 was canceled by server" — so
		// the dashboard's liveness probe never completed.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			c.markSessionDead(fmt.Sprintf("forward POST HTTP %d", resp.StatusCode))
		}
		return "", fmt.Errorf("forward POST HTTP %d (sid=%s rid=%d aid=%d ofs=%d len=%d): %s",
			resp.StatusCode, sid, rid, aid, ofs, len(data), string(b))
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if c.opts.DebugForward {
		sent := data
		if len(sent) > 260 {
			sent = sent[:260] + "..."
		}
		c.log("forward POST ok sid=%s rid=%d aid=%d ofs=%d len=%d sent=%s body=%q",
			sid, rid, aid, ofs, len(data), sent, string(b))
	}
	return string(b), nil
}

// Stop terminates the active tunnel client connection.
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}
