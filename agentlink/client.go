package agentlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// MaxFrameBytes is the largest frame the client accepts.
//
// The transcript a turn replays is the biggest thing on the wire, and a
// conversation with images in it is bigger still; 64 MiB leaves room for
// both without letting a hostile peer stream without bound.
const MaxFrameBytes = 64 << 20

// TokenHeader carries the server's bearer token.
const TokenHeader = "Authorization"

// Client is a connection to a pigasus server.
//
// The client is deliberately thin: it frames and unframes the protocol and
// leaves the sequencing to whoever drives it. The turn engine on the client
// side reads frames with Next and writes them with Send.
type Client struct {
	conn *websocket.Conn

	// writeMu serialises the writes. coder/websocket allows one
	// concurrent writer, and a turn's frames and the cancel frame that
	// interrupts it are written from different goroutines.
	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// DialOptions configure a client.
type DialOptions struct {
	// Token is the server's bearer token. Empty sends no token.
	Token string
	// HTTPClient overrides the client used for the handshake. It is
	// what a test points at its own listener.
	HTTPClient *http.Client
	// HandshakeTimeout bounds the connection setup. Zero uses the
	// default.
	HandshakeTimeout time.Duration
}

// Dial connects to a pigasus server.
//
// The url is a ws:// or wss:// address. A token, when the server wants
// one, travels in the Authorization header.
func Dial(ctx context.Context, url string, opts DialOptions) (*Client, error) {
	if url == "" {
		return nil, errors.New("agentlink: no server address")
	}
	if opts.HandshakeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.HandshakeTimeout)
		defer cancel()
	}
	dialOpts := &websocket.DialOptions{}
	if opts.Token != "" {
		dialOpts.HTTPHeader = http.Header{TokenHeader: []string{"Bearer " + opts.Token}}
	}
	if opts.HTTPClient != nil {
		dialOpts.HTTPClient = opts.HTTPClient
	}
	conn, _, err := websocket.Dial(ctx, url, dialOpts)
	if err != nil {
		return nil, fmt.Errorf("agentlink: cannot reach %s: %w", url, err)
	}
	conn.SetReadLimit(MaxFrameBytes)
	return &Client{conn: conn}, nil
}

// Send writes one frame to the server.
func (c *Client) Send(ctx context.Context, f Frame) error {
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("agentlink: cannot encode a %s frame: %w", f.Kind, err)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("agentlink: cannot send a %s frame: %w", f.Kind, err)
	}
	return nil
}

// Next reads the next frame from the server.
//
// It returns io.EOF when the server closed the connection, and the
// transport's error when the connection broke.
func (c *Client) Next(ctx context.Context) (Frame, error) {
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, fmt.Errorf("agentlink: cannot decode a frame: %w", err)
	}
	return f, nil
}

// Close closes the connection. It is safe to call more than once.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.conn.Close(websocket.StatusNormalClosure, "")
	})
	return c.closeErr
}
