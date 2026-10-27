// Package refbackend — a dispatcher that forwards the Web UI's
// Connect-RPC calls to a running reference `language_server`.
//
// The clone implements the IDE's LanguageServerService itself (see
// internal/remotecontrol). To compare the two implementations side by
// side, `pigasus web --reference-backend` swaps the clone's dispatcher
// for the one in this package: the browser still talks to the clone's
// web server, but every RPC is proxied to the reference, so whatever the
// UI shows is the reference's own answer.
//
// The reference speaks plain Connect-RPC over HTTP POST:
//
//	POST <base>/exa.language_server_pb.LanguageServerService/<Method>
//	Content-Type: application/json
//	{}
//
// A unary call answers with the proto-JSON message. A server-streaming
// call needs the Connect streaming codec (`application/connect+json`):
// the request body is one length-prefixed envelope and the response
// body is a sequence of them.
//
// Evidence grade: 一手 (the reference's own HTTP answers, captured with
// out/ide-language-server/ref_ls_query.py).
package refbackend

import (
	"context"
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

	"github.com/justwasm/pigasus/remotetransport"
)

// ServicePath is the reference's Connect-RPC service path.
const ServicePath = "/exa.language_server_pb.LanguageServerService/"

// unaryTimeout bounds one unary call. The reference answers in
// milliseconds; a minute is the ceiling before the call is a failure.
const unaryTimeout = 60 * time.Second

// Dispatcher proxies the Web UI's RPCs to a reference language_server.
//
// It satisfies remotetransport.Dispatcher and remotetransport.
// StreamDispatcher, so the clone's transport drives it exactly like the
// clone's own dispatcher.
type Dispatcher struct {
	base   string
	client *http.Client

	// mu guards the secret the reference's RPCs demand. It is learned
	// from the reference's own page the first time a call goes out.
	mu    sync.Mutex
	token string

	accepted  atomic.Uint64
	connected atomic.Int32

	notifier    func(ctx context.Context, connID string, frame []byte) error
	broadcaster func(ctx context.Context, method string, params any) error
}

// Compile-time checks: the transport asserts for these at call time.
var (
	_ remotetransport.Dispatcher       = (*Dispatcher)(nil)
	_ remotetransport.StreamDispatcher = (*Dispatcher)(nil)
)

// New returns a dispatcher that talks to the reference at base. The base
// may be "host:port", "http://host:port" or "https://host:port".
func New(base string) (*Dispatcher, error) {
	normalized, err := NormalizeBase(base)
	if err != nil {
		return nil, err
	}
	return &Dispatcher{
		base:   normalized,
		client: &http.Client{},
	}, nil
}

// NormalizeBase turns a user-supplied endpoint into a base URL.
func NormalizeBase(base string) (string, error) {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return "", errors.New("refbackend: empty reference endpoint")
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("refbackend: not a usable endpoint: %q", base)
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// Dispatch routes one JSON-RPC request frame to the reference and returns
// the response frame. The Web UI does not use this path — it speaks
// Connect-RPC — but the transport offers it, so the proxy answers it too.
func (d *Dispatcher) Dispatch(ctx context.Context, raw []byte) map[string]any {
	var frame struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &frame); err != nil {
		return map[string]any{
			"error": map[string]any{"code": -32700, "message": err.Error()},
		}
	}
	payload := "{}"
	if len(frame.Params) > 0 {
		payload = string(frame.Params)
	}
	out := map[string]any{}
	if len(frame.ID) > 0 {
		out["id"] = frame.ID
	}
	res, err := d.DispatchDirect(ctx, frame.Method, payload)
	if err != nil {
		out["error"] = map[string]any{"code": -32603, "message": err.Error()}
		return out
	}
	out["result"] = json.RawMessage(res)
	return out
}

// DispatchDirect performs one unary Connect-RPC call and returns the
// reference's answer verbatim.
//
// A server-streaming method is an error here: the transport asks for
// those through DispatchStream, which relays every frame.
func (d *Dispatcher) DispatchDirect(ctx context.Context, method string, payloadText string) (string, error) {
	if isStreamingMethod(method) {
		return "", fmt.Errorf("refbackend: %s is a server-streaming method", method)
	}
	return d.callUnary(ctx, method, payloadText)
}

// callUnary posts one JSON message and returns the reference's answer.
func (d *Dispatcher) callUnary(ctx context.Context, method string, payloadText string) (string, error) {
	body := strings.TrimSpace(payloadText)
	if body == "" {
		body = "{}"
	}
	ctx, cancel := context.WithTimeout(ctx, unaryTimeout)
	defer cancel()

	for attempt := 0; attempt < 2; attempt++ {
		raw, status, err := d.post(ctx, d.url(method), body, d.csrfToken(ctx), "application/json")
		if err != nil {
			return "", err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			// The secret is stale — the reference was restarted with
			// another one. Learn it again and try once more.
			d.forgetCSRFToken()
			continue
		}
		if status != http.StatusOK {
			return "", d.rpcError(method, status, string(raw))
		}
		return string(raw), nil
	}
	return "", fmt.Errorf("refbackend: %s returned %d", method, http.StatusUnauthorized)
}

// post sends one request to the reference and returns its body and
// status.
func (d *Dispatcher) post(ctx context.Context, url, body, token, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set(csrfHeader, token)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return raw, resp.StatusCode, nil
}

// csrfToken returns the secret the reference's RPCs demand, learning it
// from the reference's own page the first time it is asked.
//
// It returns "" when the reference's page carries no secret: a
// reference that does not want one is a reference the proxy can talk to
// without one.
func (d *Dispatcher) csrfToken(ctx context.Context) string {
	d.mu.Lock()
	token := d.token
	d.mu.Unlock()
	if token != "" {
		return token
	}
	token = discoverCSRFToken(ctx, d.client, d.base)
	if token == "" {
		return ""
	}
	d.mu.Lock()
	d.token = token
	d.mu.Unlock()
	return token
}

// forgetCSRFToken drops the learned secret, so the next call learns it
// again.
func (d *Dispatcher) forgetCSRFToken() {
	d.mu.Lock()
	d.token = ""
	d.mu.Unlock()
}

// rpcError turns the reference's error answer into the error the
// transport expects. The transport sends an empty object for a method
// the peer does not implement, so that spelling is preserved here: a
// method the reference lacks must not break the Web UI.
func (d *Dispatcher) rpcError(method string, status int, body string) error {
	trimmed := strings.TrimSpace(body)
	lower := strings.ToLower(trimmed)
	if status == http.StatusNotFound || status == http.StatusNotImplemented ||
		strings.Contains(lower, "unimplemented") || strings.Contains(lower, "not found") ||
		strings.Contains(lower, "unknown method") {
		return fmt.Errorf("refbackend: Method not found: %s", method)
	}
	if trimmed == "" {
		return fmt.Errorf("refbackend: %s returned %d", method, status)
	}
	return fmt.Errorf("refbackend: %s returned %d: %s", method, status, trimmed)
}

// url builds the reference's URL for one method.
func (d *Dispatcher) url(method string) string {
	return d.base + ServicePath + strings.TrimPrefix(method, "/")
}

// Accepted returns the cumulative connection count.
func (d *Dispatcher) Accepted() uint64 { return d.accepted.Load() }

// MarkAccepted bumps the cumulative counter.
func (d *Dispatcher) MarkAccepted() { d.accepted.Add(1) }

// NotifyConnected bumps the connected counter.
func (d *Dispatcher) NotifyConnected() { d.connected.Add(1) }

// NotifyDisconnected bumps the connected counter down.
func (d *Dispatcher) NotifyDisconnected() { d.connected.Add(-1) }

// Notify pushes a notification frame. The reference's own pushes arrive
// over its streams, so there is nothing to push locally.
func (d *Dispatcher) Notify(ctx context.Context, connID, method string, params any) error {
	return nil
}

// SetNotifier registers the callback Notify uses.
func (d *Dispatcher) SetNotifier(fn func(ctx context.Context, connID string, frame []byte) error) {
	d.notifier = fn
}

// SetBroadcaster registers the callback the dispatcher fans a
// notification out with.
func (d *Dispatcher) SetBroadcaster(fn func(ctx context.Context, method string, params any) error) {
	d.broadcaster = fn
}

// WithConnID returns the context unchanged: the reference identifies its
// own connections, so the clone's conn id has no meaning on that side.
func (d *Dispatcher) WithConnID(ctx context.Context, connID string) context.Context {
	return ctx
}
