//go:build !js

package websocket

import (
	"context"
	"net/http"

	coderws "github.com/coder/websocket"
)

func dial(ctx context.Context, u string, opts *DialOptions) (*Conn, *http.Response, error) {
	var coderOpts *coderws.DialOptions
	if opts != nil {
		coderOpts = &coderws.DialOptions{
			HTTPClient:   opts.HTTPClient,
			HTTPHeader:   opts.HTTPHeader,
			Subprotocols: opts.Subprotocols,
		}
	}
	return coderws.Dial(ctx, u, coderOpts)
}
