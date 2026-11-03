package remotetransport

import (
	"errors"
	"net/http"
)

// NewHandler creates an http.Handler that routes Connect-RPC, WebSocket,
// and optional Web UI requests using the given Dispatcher and TCPConfig.
//
// Unlike NewTCPServer, NewHandler binds no network sockets and runs no background
// listeners, making it suitable for mounting directly into standard Go HTTP servers,
// routers, or reverse proxies.
//
// If cfg.WebUIHandler is nil, the returned handler serves Connect-RPC and WebSocket
// endpoints, returning 404 for any UI or non-RPC requests (API-only mode).
func NewHandler(disp Dispatcher, cfg TCPConfig) (http.Handler, error) {
	if disp == nil {
		return nil, errors.New("remotetransport: dispatcher is required")
	}
	if err := applyTCPConfigDefaults(&cfg); err != nil {
		return nil, err
	}
	s := newTCPServer(disp, cfg, nil)
	disp.SetNotifier(s.pushFrame)
	disp.SetBroadcaster(s.broadcastFrame)
	return s.Handler(), nil
}
