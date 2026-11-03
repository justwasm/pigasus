// Package refbackend — transport.go: the RPC transport the reference's
// own page has to run on.
//
// 【一手】 2026-10-27, the reference's bundle (the 1.4.7 frontend, byte
// identical to the IDE's own frontend/main.js — md5
// 9f96dc058705b85c1191c6c5ce7bf1a9) picks its RPC transport from the
// page's query string:
//
//	function AHc(){var a=new URLSearchParams(window.location.search),
//	b=a.get("useWebSocket");
//	return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="3"?"hub":"off"}
//
// So a page served without a query parameter runs on the fetch
// transport ("off"), and only `?useWebSocket=true` selects the
// WebSocket transport ("explicit"). The clone's own web UI patches the
// bundle instead (internal/rcfrontend + the "websocket-transport-
// default" patch in internal/rctunnel/patches), but the reference mode
// serves the reference's own bytes, so the only lever left is the URL.
//
// 【一手】 2026-10-27, the fetch transport is what makes the page hang.
// The app opens six long-lived server streams over HTTP/1.1 —
// JetboxSubscribeToState, JetboxSubscribeToSummaries,
// ProjectUpdatesStream, SubscribeToMcpServerStates, SubscribeToPlugins
// and StreamAgentStateUpdates — and Chrome keeps six connections per
// origin. The six streams take every slot, so the *send* — the
// StartCascade call — never leaves the browser: the network log shows
// it as "(pending)" and the reference never sees the message at all.
// The page sits on "Working" with no output, and no model is called.
//
// The WebSocket transport carries every call — the six streams and the
// unary calls — over one connection, so nothing queues. Measured on
// the same rig: the fetch transport leaves fourteen requests pending
// and the message never reaches the reference; the WebSocket transport
// issues no HTTP POST at all and the turn is answered in ~4s.
//
// The redirect below is the clone's own wire decision: the *answers*
// are still the reference's, only the browser's way of reaching the
// clone's server changes. `?useWebSocket=false` stays the operator's
// escape hatch back to the fetch transport — the bundle reads it as
// "off" — and so is any other `useWebSocket`/`wsTransport` value.
package refbackend

import (
	"net/http"
	"net/url"
)

// websocketTransportParam is the query parameter the reference's bundle
// reads to pick the WebSocket transport.
const websocketTransportParam = "useWebSocket"

// transportRedirect returns the URL a document request has to be
// redirected to so the page runs on the WebSocket transport, or "" when
// the request is served as it is.
//
// Only the reference's HTML document is redirected: the bundle reads
// the transport from the URL, so the document has to be *loaded* from
// an URL that carries the parameter. A request that already names a
// transport is left alone — the operator's choice wins — and so is
// every other asset, which is not the document.
//
// The path and the query string are carried over as they are: the
// document may be requested under any path (the reference's SPA
// fallback answers with it), and the secret the operator shares with
// the LAN clients travels in the query — dropping it would lock them
// out.
func transportRedirect(r *http.Request, contentType string) string {
	if r == nil || !isHTML(contentType) {
		return ""
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return ""
	}
	if q.Has(websocketTransportParam) || q.Has("wsTransport") {
		return ""
	}
	q.Set(websocketTransportParam, "true")
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	return path + "?" + q.Encode()
}
