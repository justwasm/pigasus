# The remote-control surface asks for a secret — deliverable

**Cycle:** `rc-auth-bearer-required`
**Subject:** the remote-control surface (`/rpc`, the Connect-RPC
prefix and the Web UI's own paths) now demands a secret from every
client that did not come in over the loopback interface.
**Evidence:** 【一手】 the reference's own web bundle
(`internal/rcfrontend/assets/main.js`) sends the header
`x-codeium-csrf-token` on every request, and the clone's own proxy to
the reference's language server sets the same header
(`internal/rctunnel/proxy.go:87`) — the header is the reference's own
credential for its local HTTP surface; 【一手】 the clone's own code:
`internal/persistentrc/helpers.go:147` (the loopback refusal),
`internal/originpolicy/originpolicy.go:430` (the empty Origin is
admitted); 【自研】 the gate itself — the reference has no counterpart
for this surface (see §1).

---

## 0. Summary

| | before | after |
|---|---|---|
| **a client on the LAN** | could drive the agent — read a file, delete a conversation, run a turn — as long as it could reach the port | has to present the secret: `401` + `{"ok":false,"denied":true,"reason":"…"}` otherwise |
| **the credential** | none | a random 32-byte secret per instance, kept in `$ANTIGRAVITY_HOME/rc_secret` (0600) or pinned by `$ANTIGRAVITY_RC_TOKEN` |
| **the channel** | — | the reference's own `x-codeium-csrf-token` header, `Authorization: Bearer`, the `csrfToken` cookie, or `?token=` |
| **the page** | served to anybody who could reach the port | served to the loopback, to the tunnel, and to a peer that presented the secret — a peer that presented nothing gets the "token required" page, which does not carry the secret |
| **the cloud path** | worked | works — a tunnel request is authenticated by the cloud and never crosses the socket the gate guards |

---

## 1. The gap

### 1.1 The surface is network-reachable on purpose

`pigasus remote-control serve` and `pigasus web` bind `0.0.0.0`, and
the persistent daemon *refuses* a loopback bind:

```go
// internal/persistentrc/helpers.go:147
// Loopback is refused (both 127.0.0.0/8 and ::1) because the
// persistent daemon is meant to be reachable from another
// machine on the LAN.
```

`pigasus web` says so in its own output — the network URL is printed
as *"share with local LAN users"*.

### 1.2 The only gate was the Origin allowlist, and the allowlist admits the empty Origin

```go
// internal/originpolicy/originpolicy.go:430
origin = strings.TrimSpace(origin)
if origin == "" {
    // Empty origin = non-browser caller; preserve compat …
    return true
}
```

An empty `Origin` is what `curl`, `wscat` and a python client send —
they are not browsers. So the allowlist never stopped them, and the
methods on the other side of it are `ReadRemoteAgentFile` and
`DeleteRemoteAgentConversation` (the gap STATUS.md §2 named).

### 1.3 The reference has no such surface — this gate is the clone's own

【一手】 `TODO-rc-parity.md` §Priority 1: *"`agy remote-control start`
has no `--bind` and the daemon listens for nothing but the language
server (`HTTPS (gRPC)` + `HTTP`, random loopback ports, no `/rpc`);
remote control is an **outbound** V2 webchannel to
`jetski-webchannel.googleapis.com:443`. The clone's `/rpc` surface is
therefore an invention, not a port."*

So there is no reference behaviour to copy here. The gate is the
clone's own design, and it is marked as such throughout this report.

What *is* the reference's own is the credential it already used on
its own local HTTP surface:

* 【一手】 the reference's web bundle sets `x-codeium-csrf-token` on
  every request — the CLI bundle: `e["x-codeium-csrf-token"]=[b]` in
  the header builder, `c.header.set("x-codeium-csrf-token",a)` in the
  Connect middleware, `n.header.set("x-codeium-csrf-token",G9())` in
  the fallback;
* 【一手】 the clone's own proxy to the reference's language server
  sets the same header — `internal/rctunnel/proxy.go:87`:
  `req.Header.Set("x-codeium-csrf-token", csrfToken)`.

The clone ships the reference's bundle, so the header was already on
the wire — it was decoration. This cycle makes it the credential.

---

## 2. The rule

`(*TCPServer).handleTokenCheck` (`internal/remotetransport/tcp_auth.go`)
decides in this order:

| # | the request | the verdict | why |
|---|---|---|---|
| 1 | no secret configured | **allowed** | a server wired without a secret keeps the pre-cycle behaviour — the Unix-socket rig and the existing tests are unaffected |
| 2 | arrived through the remote-control tunnel (`X-Jetski-Via-Remote-Control: true`) | **allowed** | the cloud's WebChannel client authenticated it with Google's own bearer token before the relay handed it to this process; it never crossed the socket the gate guards |
| 3 | came in over the loopback interface | **allowed** | it is the operator's own machine, and `r.RemoteAddr` is the one part of a request a client cannot forge |
| 4 | anything else | **must present the secret** | this is the peer the gate is for |

`TCPConfig.RequireTokenOnLoopback` turns rule 3 off — the secret then
answers every client.

### 2.1 The channels

`internal/rctoken.Presented` reads the secret from, in order:

1. `x-codeium-csrf-token` — the reference's own header;
2. `Authorization: Bearer <secret>` — the standard one;
3. the `csrfToken` cookie — the browser's channel (the bundled UI
   reads it: `document.cookie.split(";").find(a=>a.startsWith("csrfToken="))`);
4. `?token=<secret>` — the link an operator hands to somebody else.

The first one that is present wins, and a *wrong* first channel is not
masked by a right second one.

The comparison is constant-time (`crypto/subtle`), and the expected
side is trimmed first — a constant-time comparison of two different
lengths leaks the length through its early return.

### 2.2 The answer to a refused request

```json
{"ok":false,"denied":true,"reason":"no secret presented"}
```

with `401 Unauthorized` and `WWW-Authenticate: Bearer realm="remote-control"`.
The body has the same shape as the Origin deny body
(`{"ok":false,"denied":true,"origin":…,"reason":…}`), so a client that
reads either envelope finds the same fields. The reason names the
rule that decided the request and never carries the secret.

---

## 3. The page

The page is where the secret travels: the bundled UI reads it from
`window.__APP_CONFIG__.csrfToken` and sends it on every request. So a
peer that must not have the secret must not get the page.

| the peer | the page | the secret in it |
|---|---|---|
| the loopback | served | the server's secret |
| the tunnel | served | the server's secret |
| a remote peer that presented the secret | served | the secret it presented |
| a remote peer that presented nothing | **refused** (`401`, the "token required" page) | none |

The refused page says what to do — *open `http://<host>/?token=<secret>`* —
and does not carry the secret. A wrong secret is not laundered into the
page either: the value is verified against the server's own before it
is injected, and the injection point is a JavaScript string literal, so
a secret that could close that literal never reaches it (the value is
either the server's own base64url secret or a query parameter that was
verified against it).

### 3.1 The cookie

The transport hands the browser the `csrfToken` cookie when a peer
proves it knows the secret, so the secret only has to travel in the
query string once:

```
GET /?token=SECRET          → 200 + Set-Cookie: csrfToken=SECRET
GET /main.js                → 200 (the cookie is sent)
GET /exa.language_server_pb.… → 200 (the cookie is sent)
```

The cookie is set by the *transport*, not by the Web UI handler: the
gate is at the transport, so a handler that knows nothing about the
secret cannot be trusted to hand it out.

### 3.2 The gate covers the page, not only the RPC

The gate sits in front of every path of the mux that is not the WS
upgrade or the Connect prefix — the Web UI's own paths included. The
page is served by the clone's handler or by the reference backend's
assets; the gate is above both.

---

## 4. Where the secret lives

`internal/rchost/rctoken.go`:

| | |
|---|---|
| `$ANTIGRAVITY_RC_TOKEN` | the operator pinned it — the same secret on every machine and every restart |
| `$ANTIGRAVITY_HOME/rc_secret` | the file the secret is kept in — 0600, written atomically (temp file + rename), the way the OAuth token is |
| neither | a fresh secret, minted for this process (32 bytes from `crypto/rand`, base64url without padding) |

The file is what makes the *persistent* daemon usable: its output goes
to a log file, not to a terminal, so a secret that only existed in the
process would be unreachable from the network. That daemon therefore
prints the *path* of the secret file, and `pigasus web` /
`remote-control serve` print the secret itself:

```
pigasus web: server listening on 0.0.0.0:8787
  - Local URL:   http://127.0.0.1:8787
  - Network URL: http://192.168.1.5:8787/?token=SECRET (share with local LAN users)
  - Secret:      SECRET (a LAN client has to present it)
```

---

## 5. What changed in the clone

| file | what |
|---|---|
| `internal/rctoken/rctoken.go` | **new** — the secret's shape: `Generate`, `Verify` (constant-time), `Presented` (the four channels), `IsWellFormed`, `IsTunnelRequest`, `IsLoopbackPeer`, and the `X-Jetski-Via-Remote-Control` marker (moved here so the tunnel, the transport and the web UI read one spelling) |
| `internal/remotetransport/tcp_auth.go` | **new** — `AuthDecision`, `handleTokenCheck`, `writeTokenUnauthorized` |
| `internal/remotetransport/tcp.go` | `TCPConfig.CSRFToken` + `TCPConfig.RequireTokenOnLoopback` |
| `internal/remotetransport/tcp_handlers.go` | the gate in `handleWS`, before the upgrade |
| `internal/remotetransport/tcp_server.go` | the gate in `handleRPCCall` and in `newConnectRPCRouter` (the Web UI's paths), plus the cookie |
| `internal/remotetransport/tcp_connectstream.go` | the tunnel marker now comes from `internal/rctoken` |
| `internal/rctunnel/httpproxy.go` | the same |
| `internal/rcfrontend/token.go` | **new** — the page's secret: `tokenForRequest`, `pageAllowed`, `applyRequestToken`, `etagFor`, the "token required" page |
| `internal/rcfrontend/frontend.go` | the page is built once with a placeholder and the secret is substituted per request; the ETag follows the substituted body |
| `internal/rcfrontend/accountsapi.go` | the account switcher accepts the surface's secret too — a remote peer that reached the page at all holds it |
| `internal/rchost/rctoken.go` | **new** — `resolveRCToken` (env → file → minted), the secret file, the announcements |
| `internal/rchost/web.go`, `remote_control.go`, `remote_control_serve.go`, `remote_control_foreground.go` | the wiring: the secret goes to the transport and to the web UI, and is announced |

### 5.1 The tests

| package | tests |
|---|---|
| `internal/rctoken` | the shape of a minted secret, the comparison (7 cases), the four channels and their order, the shape check, the tunnel marker, the peer address (7 cases), the pinned-secret lookup |
| `internal/remotetransport` | the four rules of the gate, the 401 + JSON body on the WS upgrade, the 401 on the Connect path, the happy path, the page's gate and the cookie |
| `internal/rcfrontend` | the page per peer (loopback / tunnel / remote-with / remote-without / wrong), the SPA fallback, the ETag |
| `internal/rchost` | the pinned secret, the blank environment variable, the file's survival across a restart, the file's 0600 permissions, the two announcements |

---

## 6. 硬约束 / the load-bearing rules

1. **The gate answers the peer, not the browser.** The Origin
   allowlist cannot be the gate for this surface: a non-browser client
   sends no Origin, and the allowlist admits the empty one on purpose.
2. **The peer address is the only unforgeable part of a request.**
   The loopback exemption reads `r.RemoteAddr`, never the `Origin`
   header, never the `Host` header — a client can set all three.
3. **The tunnel is exempt.** Its request was authenticated by the
   cloud and it never crossed the socket the gate guards. The marker
   is set by the clone's own tunnel
   (`internal/rctunnel/httpproxy.go:117`).
4. **The secret is never in a page a peer may not have.** The page is
   the biggest leak on this surface: the bundled UI reads the secret
   out of it. A peer that presented nothing gets a page that says so.
5. **A wrong secret is not laundered into the page.** The value that
   reaches the JavaScript string literal is verified against the
   server's own secret first.
6. **The comparison is constant-time**, and the expected side is
   trimmed before the comparison, so the length does not leak.
7. **No secret configured ⇒ everything is allowed.** The pre-cycle
   behaviour is the default, so the Unix-socket rig, the existing
   tests and any third-party wiring of `remotetransport` keep working.
8. **The header is the reference's own** — `x-codeium-csrf-token` —
   and the bundled UI already sends it. The gate is the clone's own
   design; the header is not.

---

## 7. 存疑 / the open questions

1. **What does the reference do in its LAN mode?** Unknown, and
   unknowable from this checkout: the reference has no `/rpc` surface
   (§1.3). The gate is the clone's own answer to a question the
   reference never had to answer.
2. **Should the loopback be trusted at all?** The rule says yes —
   the operator's own machine holds the secret — but a second local
   process could use the surface without knowing it. The strict
   alternative is `TCPConfig.RequireTokenOnLoopback: true`; the
   surface answers to it, and the flag is wired but not set by
   default. 【推测】 the reference's own local surface is protected by
   the loopback bind alone, which is why its language server's
   CSRF token is per-process and short-lived.
3. **The secret's lifetime.** Per process by default, per file
   otherwise. A file-backed secret survives a restart, so a URL that
   was shared once keeps working — and a leaked file stays leaked
   until the operator deletes it. `$ANTIGRAVITY_RC_TOKEN` is the
   operator's own choice.
4. **The account switcher's own token.** It is written into the page
   as well (`accountsWidgetHTML`), and the page's gate now keeps it
   away from the peers that may not have the page. The switcher also
   accepts the surface's secret, so a remote peer that has it can
   switch accounts — that is the point of the gate, but it makes the
   surface's secret a *credential* rather than a door key.
5. **The reference backend's page.** The reference's own page carries
   the reference's own secret, and the clone's transport accepts the
   secret the reference backend brought
   (`internal/rchost/web.go`, the reference-backend branch). The
   reference's handler does not set the clone's `csrfToken` cookie,
   so a remote peer in that mode may have to keep the secret in the
   query string. 【推测】 — the reference's own language server sets
   that cookie, and the clone's reference-backend handler does not
   reproduce it yet.
6. **The Unix-socket path is unchanged.** `--remote-control` (the
   per-turn daemon) speaks newline-delimited JSON-RPC over a unix
   socket: there is no HTTP, no Origin and no peer address. Its
   protection is the socket's own permissions.

---

## 8. Limitations

* The gate was verified in-process — a real HTTP server on
  `127.0.0.1:0`, a real `coder/websocket` dial, a real HTTP request.
  It was **not** verified against a second machine on a LAN, and the
  live cloud path (the tunnel's proxied request) was not re-measured
  after the change.
* The loopback exemption is what the tests exercise as "a remote
  peer" — a test on `127.0.0.1` cannot produce a non-loopback peer
  address, so the deny paths are reached with
  `RequireTokenOnLoopback: true`.
* The page's secret is substituted per request, so the ETag is
  computed per request as well (a sha256 over ~4 KB). The page is
  still cached; only the hash is recomputed.
* The secret is printed on the terminal by `pigasus web` and
  `remote-control serve`. A terminal scrollback, a shell history or a
  CI log holds it. `$ANTIGRAVITY_RC_TOKEN` is the quieter path.
* The reference's own remote-control transport is the **outbound**
  WebChannel, authenticated by Google's bearer token
  (`internal/rctunnel/client.go`). Nothing here changes that path —
  it was already authenticated, and it stays exempt.

---

## 9. The files

| file | what |
|---|---|
| `internal/rctoken/rctoken.go` | the secret's shape, its comparison, its channels |
| `internal/remotetransport/tcp_auth.go` | the gate |
| `internal/remotetransport/tcp.go` | `TCPConfig.CSRFToken`, `TCPConfig.RequireTokenOnLoopback` |
| `internal/remotetransport/tcp_handlers.go` | the gate on the WS upgrade |
| `internal/remotetransport/tcp_server.go` | the gate on the Connect path and on the Web UI's paths, the cookie |
| `internal/rcfrontend/token.go` | the page's secret |
| `internal/rcfrontend/frontend.go` | the per-request substitution |
| `internal/rcfrontend/accountsapi.go` | the account switcher accepts the secret |
| `internal/rchost/rctoken.go` | the secret's lifecycle, the announcements |
| `internal/rchost/web.go`, `remote_control.go`, `remote_control_serve.go`, `remote_control_foreground.go` | the wiring |
| `internal/rctunnel/httpproxy.go` | the tunnel marker, one spelling |
| `internal/rcfrontend/assets/main.js` | the bundle — the evidence of §1.3 |
| `internal/persistentrc/helpers.go` | the loopback refusal — the evidence of §1.1 |
| `internal/originpolicy/originpolicy.go` | the empty Origin — the evidence of §1.2 |
