# Modular WebRTC mesh: authenticated native prototype

2026-10-31. The user assigned the remaining dependency migrations to other
agents and requested independent WebRTC work without affecting their files or
bringing native dependencies into `cmd/pigga`'s WASM build.

## Delivered boundary

Only new paths: `rcmesh/`, `modules/rcmesh-pion/`, this report, and
`out/rcmesh-1.4.11/`. No root dependency, command, existing handler, endpoint,
feature flag, or guide was changed by this work. Existing staged changes belong
to other agents and must not enter this commit.

| Layer | Implementation | Dependencies |
|---|---|---|
| Recovered wire and authentication | `rcmesh` | standard library |
| Session interface and injected answerer | `rcmesh.Session`, `PeerConfig`, `Answerer` | no Pion types |
| Native ICE/DTLS/SCTP/DataChannel | independent `modules/rcmesh-pion` | Pion v4.2.26, normal certificate verification enabled |
| WASM adapter entry | same `NewAnswer` signature, explicit unsupported result | no Pion imports |
| Daemon / cloud signaling / browser adapter | next integration boundary | not wired or advertised |

The native adapter accepts a gathered offer, returns a gathered answer, and can
consume remote trickle candidates. It authenticates against supplied authorized
P-256 keys before serving local LanguageServerService HTTP requests. Request IDs
stay int64 strings, binary/text body oneofs retain presence, streaming writes
produce response envelopes, and cancellation addresses active request IDs.

Each peer accepts one ordered reliable channel, has authentication/gather/send
timeouts, a 32-request cap, and a 256 KiB send-buffer cap with low-buffer wakeups.
Long streams do not block reception of unrelated unary calls. Peer cancellation
propagates to all request contexts. Unknown fields, incomplete chunks, duplicate
active IDs and malformed frames fail closed in this prototype. Handler code
must honor context cancellation.

## Evidence

The reference is macOS arm64 agy 1.4.11, SHA-256
`09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd`.
18 functions decompiled successfully from a matching read-only Ghidra project.
The channel-binding payload was additionally checked against arm64 stores and
copies. Go type metadata supplies exact JSON tags; the embedded api.proto
descriptor supplies field numbers, optional presence, and oneofs. See
[`out/rcmesh-1.4.11/FINDINGS.md`](../../../out/rcmesh-1.4.11/FINDINGS.md).

The reference canonical fingerprint separator is a **space**. Authentication
uses SHA-256 over nested BE32 length prefixes and raw WebCrypto P-256 `r||s`,
not ASN.1 DER. The public key in an incoming auth frame does not establish trust.
The signal payload is a snake_case JSON envelope embedded inside an outer
proto-JSON string; its own payload is raw JSON. The native send path uses the
confirmed `/v1internal:sendSignalingMessage` suffix. Polling routes should be
recovered rather than inferred from an older `$rpc` note.

## Validation

All commands run locally; no cloud registration, model request, or message was
sent. Tests open ephemeral local peer sockets.

- `go test -race ./rcmesh`: passed. Independent binding-layout golden; nonce,
  trust and peer binding; replay; invalid fingerprints; binary/text frames;
  int64 IDs; final-body presence; unsafe routes; invalid/incomplete frames;
  sender failures.
- `GOWORK=off go -C modules/rcmesh-pion test -race -count=1 .`: passed. Actual
  Pion peers exchange SDP, authenticate and return an 80 KB response; pre-auth
  RPC and untrusted keys are blocked; authentication times out; a unary call
  proceeds beside a parked stream; individual cancellation preserves the
  peer; parent cancellation closes the peer and cancels the parked handler.
- `CGO_ENABLED=0` native adapter build: passed. Data-only Pion does not require
  the media/CGO stack claimed in the older deferred document.
- Both packages' test binaries compile for `js/wasm` and `wasip1/wasm`.
  These are compile checks, not WASM runtime tests.
- `GOOS=js GOARCH=wasm go build ... ./cmd/pigga`: passed before and after this
  work. Its dependency list contains no Pion package. The adapter's own WASM
  dependency list also contains no Pion package.
- `cmd/pigga` on `wasip1/wasm`: pre-existing PTY/process-group syscall failures,
  separately from this feature. Do not describe the JS build as failing or
  attribute the WASI issue to Pion. No unrelated fixes were made here.

## Next integration steps

1. Implement the account-authenticated signaling adapter and its session
   manager: instance/session target oneof, poll cursor, deduplication, recipient
   validation, deadlines, bounded peer count, refreshed/revoked authorized keys.
   Inject named service endpoints and OAuth clients. Never source trust from
   the offer's own key. Preserve WebChannel while no usable mesh exists.
2. Compose the native answerer at a native entry module through `rcmesh.Answerer`.
   Keep Pion out of the root module; add explicit nested-module CI and a release
   version for the parent protocol package. Shared WASM commands receive an
   implementation through the interface rather than importing native code.
3. Validate the recovered challenge and wire against the original browser.
   Exercise Chrome's client, actual LanguageServerService Connect/proto streams,
   authorization expiry, key withdrawal, TURN-only connectivity, multiple peers,
   slow readers, reconnect and WebChannel fallback. A local two-Pion roundtrip
   cannot substitute for this acceptance test.
4. Recover chunk metadata, reassembly and cancellation interaction before
   enabling `supportsChunking`. Enable mesh experiment/readiness only after the
   production composition works and has passed those checks.

## Limitations

This is an authenticated local integration prototype, not a production Google
mesh client. No existing readiness flag changed. Cloud signaling, TURN-only
networks, original-browser interoperability, chunking, per-account policy,
revocation and reconnect/failover remain open. Bounds and strict rejection
behavior are local policy, not recovered reference defaults. Empty byte-slice
JSON-v2 representation has not been captured from the reference. The current
nested module uses a local parent replacement and is not independently
publishable until release configuration is supplied. Shared-tree validation
does not establish that every concurrently edited package is green.
