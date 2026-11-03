package agentserver

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/ironpark/acp-go/router"
)

// ACPRouter returns the configured ProtocolRouter according to ACPVersion.
// In "auto" / default mode, it configures both v1 and draft v2, dynamically negotiating
// with the client via the initialize handshake.
func (s *Server) ACPRouter() *router.ProtocolRouter {
	r := router.New()
	ver := strings.ToLower(strings.TrimSpace(s.cfg.ACPVersion))
	switch ver {
	case "1", "v1":
		r.WithV1(func(c *acp1.AgentSideConnection) acp1.Agent {
			return NewACPV1Agent(s, c)
		})
	case "2", "v2":
		r.WithV2(func(c *acp2.AgentSideConnection) acp2.Agent {
			return NewACPV2Agent(s, c)
		})
	default: // "auto", "dual", ""
		r.WithV1(func(c *acp1.AgentSideConnection) acp1.Agent {
			return NewACPV1Agent(s, c)
		}).WithV2(func(c *acp2.AgentSideConnection) acp2.Agent {
			return NewACPV2Agent(s, c)
		})
	}
	return r
}

// ServeACP streams the ACP protocol over the given peer input and output.
// It blocks until the connection is terminated or the context is cancelled.
func (s *Server) ServeACP(ctx context.Context, peerInput io.Writer, peerOutput io.Reader) error {
	tr := acp.NewStdioTransport(peerOutput, peerInput)
	return s.ACPRouter().Serve(ctx, tr)
}

// ServeACPConn streams the ACP protocol over a raw network connection (TCP/Unix socket).
func (s *Server) ServeACPConn(ctx context.Context, conn net.Conn) error {
	defer conn.Close()
	return s.ServeACP(ctx, conn, conn)
}

// ServeACPWebSocket upgrades an incoming HTTP request to a WebSocket and serves ACP over it.
func (s *Server) ServeACPWebSocket(w http.ResponseWriter, r *http.Request) error {
	if !s.authorized(r) {
		http.Error(w, "agentserver: missing or invalid Authorization", http.StatusUnauthorized)
		return nil
	}
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return err
	}
	defer wsConn.Close(websocket.StatusNormalClosure, "closed")

	// coder/websocket provides a net.Conn wrapper over MessageText frames.
	netConn := websocket.NetConn(r.Context(), wsConn, websocket.MessageText)
	defer netConn.Close()

	tr := acp.NewStdioTransport(netConn, netConn)
	return s.ACPRouter().Serve(r.Context(), tr)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	authHdr := r.Header.Get("Authorization")
	if authHdr == "" {
		return false
	}
	// Constant-time token match
	token := s.cfg.Token
	got := authHdr
	if len(got) > 7 && got[:7] == "Bearer " {
		got = got[7:]
	}
	if len(got) != len(token) {
		return false
	}
	var diff byte
	for i := 0; i < len(token); i++ {
		diff |= token[i] ^ got[i]
	}
	return diff == 0
}
