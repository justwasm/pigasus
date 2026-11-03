# Remote-control mesh protocol

`rcmesh` contains the agy 1.4.11 DataChannel wire, channel-bound P-256
authentication, and an authenticated RPC-to-HTTP bridge. Runtime imports are
standard-library only; the package builds for native Go, `js/wasm`, and
`wasip1/wasm`. It never obtains tokens, opens peer connections, or enables mesh
feature flags.

The native answerer lives in the independent module
[`modules/rcmesh-pion`](../modules/rcmesh-pion). Hosts depend on `Session`,
`PeerConfig`, and `Answerer`; they inject an implementation at their composition
root. A browser implementation can use `RTCPeerConnection` behind the same seam.
Do not import the native adapter from shared commands or add Pion to the root
module to register it implicitly.

The host supplies authorized SPKI PEM keys from its account-authenticated
signaling service. A public key included in an offer or a DataChannel response
does not establish trust. Authentication signs SHA-256 over:

```text
LP(nonce) || LP(LP(min(localFingerprint, remoteFingerprint))
                || LP(max(localFingerprint, remoteFingerprint)))
LP(x) = BE32(byte length of x) || x
fingerprint = "sha-256 " || lowercase(hex certificate digest without colons)
signature = raw P-256 r[32] || s[32]
```

`HTTPBridge.Handle` must only be called after authenticating a channel. It
routes complete `ConnectInstanceResponseV2.proxyRequest` messages to the local
LanguageServerService HTTP handler and emits
`ConnectInstanceRequestV2.proxyResponse` messages. It preserves int64 request IDs
as strings, the body oneof, binary bytes, streaming writes, and a final empty
frame. It supplies trusted remote-control headers after caller headers.
Per-request `x-cancel-request: true` messages belong to the session's active
request registry, before HTTP dispatch.

Current bounds are deliberate adapter policy: 64 KiB JSON frames, 16 KiB response
body writes, and no fragmented request reassembly. `supportsChunking` must remain
false. These limits are not claimed to be the reference's defaults. Unknown
fields and non-LanguageServerService routes are rejected by this prototype.

Evidence and remaining integration work:
[`out/rcmesh-1.4.11`](../out/rcmesh-1.4.11/FINDINGS.md) and
[`docs/cycles/rcmesh-modular`](../docs/cycles/rcmesh-modular/deliverable.md).
