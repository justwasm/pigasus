package browserbridge

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// The bridge connection the clone shares.
//
// The bridge is one process-wide resource: the browser agent's tools all
// call the same connection, and the connection is the same whether the
// clone joined an existing bridge or took the port itself. So it lives
// here, next to the protocol, and is created on the first call.

var (
	sharedMu sync.Mutex
	shared   *Client
	policy   = DefaultConfig()
)

// Configure sets the bridge's connection policy.
//
// The command calls it once, with the values the flags carry. An empty
// host or port means the project's own default.
func Configure(c Config) {
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.Port <= 0 {
		c.Port = DefaultPort
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	sharedMu.Lock()
	defer sharedMu.Unlock()
	policy = c
}

// CurrentConfig returns the policy in force.
func CurrentConfig() Config {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	return policy
}

// GetClient returns the shared bridge connection, opening it on first use.
//
// The connection is kept: the bridge is a local socket to the user's own
// browser, and reconnecting for every tool call would cost a handshake
// per call.
func GetClient(ctx context.Context) (*Client, error) {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if shared != nil {
		return shared, nil
	}
	c, err := Connect(ctx, policy)
	if err != nil {
		return nil, err
	}
	shared = c
	return c, nil
}

// CloseShared ends the shared connection.
func CloseShared() error {
	sharedMu.Lock()
	c := shared
	shared = nil
	sharedMu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// probeTimeout bounds the reachability check.
const probeTimeout = 3 * time.Second

// Check reports whether the bridge can serve a call.
//
// It asks for the session's tabs — the one call that touches nothing: no
// tab is opened, no page is read, nothing in the user's browser moves.
// A bridge that answers it is a bridge whose extension is attached, which
// is the question the browser agent has before it starts.
func Check(ctx context.Context) error {
	c, err := GetClient(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout+nativeHostWait)
	defer cancel()
	_, err = c.Call(ctx, "tabs_context_mcp", json.RawMessage(`{}`))
	return err
}

// Probe reports whether the bridge answers, within a short deadline.
//
// It asks the cheapest question there is — one connection and one
// greeting — so it does not prove that the extension is attached; Check
// does that.
func Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	c, err := Connect(ctx, policy)
	if err != nil {
		return err
	}
	c.Close()
	return nil
}
