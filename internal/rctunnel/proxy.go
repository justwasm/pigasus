package rctunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/internal/rctunnel/patches"
)

const (
	wsRelayPath       = "/connect-websocket"
	upstreamReadLimit = 64 << 20
	clientReadLimit   = 16 << 20
	relayWriteTimeout = 2 * time.Minute
)

// Hop-by-hop headers to strip during WebSocket upstream dialing.
var hopHeaders = map[string]bool{
	"Connection":               true,
	"Upgrade":                  true,
	"Host":                     true,
	"Origin":                   true,
	"Keep-Alive":               true,
	"Proxy-Connection":         true,
	"Te":                       true,
	"Trailer":                  true,
	"Transfer-Encoding":        true,
	"Content-Length":           true,
	"Sec-Websocket-Key":        true,
	"Sec-Websocket-Version":    true,
	"Sec-Websocket-Extensions": true,
	"Sec-Websocket-Protocol":   true,
}

// Proxy bridges HTTP and WebSocket traffic to the local language server.
type Proxy struct {
	targetPort int
	csrfToken  string
	target     *url.URL
	httpProxy  *httputil.ReverseProxy
	transport  *http.Transport
}

// NewProxy constructs a Proxy pointing at the given loopback language server port.
func NewProxy(targetPort int, csrfToken string) (*Proxy, error) {
	target, err := url.Parse(fmt.Sprintf("https://127.0.0.1:%d", targetPort))
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		DisableCompression:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1
	rp.Transport = transport

	baseDirector := rp.Director
	rp.Director = func(req *http.Request) {
		if orig := req.Header.Get("Origin"); orig != "" {
			req.Header.Set("X-Original-Origin", orig)
		}
		if clientHost := req.Header.Get("X-Forwarded-Host"); clientHost != "" {
			req.Header.Set("X-Client-Host", clientHost)
		} else if req.Host != "" {
			req.Header.Set("X-Client-Host", req.Host)
		}
		baseDirector(req)
		req.Host = target.Host
		req.Header.Set("Origin", target.String())
		if csrfToken != "" {
			req.Header.Set("x-codeium-csrf-token", csrfToken)
		}
		if req.URL.Path == "/main.js" || req.URL.Path == "/" || strings.HasSuffix(req.URL.Path, ".html") {
			req.Header.Del("Accept-Encoding")
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		}
	}

	rp.ModifyResponse = func(resp *http.Response) error {
		if resp == nil || resp.Body == nil {
			return nil
		}
		if req := resp.Request; req != nil {
			if acao := resp.Header.Get("Access-Control-Allow-Origin"); acao != "" {
				if orig := req.Header.Get("X-Original-Origin"); orig != "" {
					resp.Header.Set("Access-Control-Allow-Origin", orig)
				}
			}
		}

		target, shouldPatch := targetDocumentFor(resp)
		if !shouldPatch {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}

		patched, _ := patches.Apply(target, body, patches.Options{
			MobileUX: true,
		})

		resp.Body = io.NopCloser(bytes.NewReader(patched))
		resp.ContentLength = int64(len(patched))
		resp.Header.Set("Content-Length", strconv.Itoa(len(patched)))
		return nil
	}

	return &Proxy{
		targetPort: targetPort,
		csrfToken:  csrfToken,
		target:     target,
		httpProxy:  rp,
		transport:  transport,
	}, nil
}

func targetDocumentFor(resp *http.Response) (patches.Target, bool) {
	if resp.StatusCode != http.StatusOK || resp.Request == nil {
		return 0, false
	}
	path := resp.Request.URL.Path
	if path == "/main.js" {
		return patches.MainJS, true
	}
	ctype := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ctype, "text/html") {
		return patches.HTML, true
	}
	return 0, false
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isWebSocketUpgrade(r) {
		p.relayWebSocket(w, r)
		return
	}
	p.httpProxy.ServeHTTP(w, r)
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

func (p *Proxy) relayWebSocket(w http.ResponseWriter, r *http.Request) {
	target := fmt.Sprintf("127.0.0.1:%d", p.targetPort)

	upstreamHeader := http.Header{}
	for name, values := range r.Header {
		if hopHeaders[http.CanonicalHeaderKey(name)] {
			continue
		}
		upstreamHeader[name] = append([]string(nil), values...)
	}
	upstreamHeader.Set("Origin", "https://"+target)
	if p.csrfToken != "" {
		upstreamHeader.Set("x-codeium-csrf-token", p.csrfToken)
	}

	dialCtx, dialCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer dialCancel()

	upstream, _, err := websocket.Dial(dialCtx, "wss://"+target+r.URL.RequestURI(), &websocket.DialOptions{
		HTTPClient:      &http.Client{Transport: p.transport},
		HTTPHeader:      upstreamHeader,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("upstream websocket dial error: %v", err), http.StatusBadGateway)
		return
	}
	defer upstream.CloseNow()

	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionContextTakeover,
	})
	if err != nil {
		return
	}
	defer client.CloseNow()

	upstream.SetReadLimit(upstreamReadLimit)
	client.SetReadLimit(clientReadLimit)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	errCh := make(chan error, 2)
	go func() {
		errCh <- pumpMessages(ctx, client, upstream)
	}()
	go func() {
		errCh <- pumpMessages(ctx, upstream, client)
	}()

	<-errCh
}

func pumpMessages(ctx context.Context, dst, src *websocket.Conn) error {
	for {
		msgType, data, err := src.Read(ctx)
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, relayWriteTimeout)
		err = dst.Write(wctx, msgType, data)
		cancel()
		if err != nil {
			return err
		}
	}
}
