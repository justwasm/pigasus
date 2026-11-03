# Verifying the WebRTC mesh signaling wire against `agy` 1.4.11

2026-10-31. This cycle checks the `rcmesh/` signaling implementation against
the reference binary, function by function, and fixes the mismatches it finds.

The reference is the Antigravity CLI `agy` 1.4.11, SHA-256
`09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd`. Its
pclntab carries 168548 names, matching `out/local-macos/seeds/seeds.txt`.
Only the CLI's addresses are used; the IDE's `language_server` is never mixed
in.

**Verdict: the implementation's direction is right — the envelope, the SDP
shape, the deduplication key, the polling target, the request/response bodies
and the HTTP shape all match — but four wire-level errors would have made the
cloud misread or reject the clone.** Two behaviour differences that are not
wire-level were aligned as well. Three functional gaps remain open and are
listed below; the mesh must stay behind the experimental flag until they are
closed.

## Method

The read-only Ghidra project `out/local-macos/proj/agy` already holds the
1.4.11 executable, so no re-import was needed. `DecompilePriority.java`
(`out/ide-language-server/ghidra-scripts/`) exported 33 functions listed in
`out/rcmesh-1.4.11-signaling/priority*.txt` into
`out/rcmesh-1.4.11-signaling/dec/` — the poll loop, the message dispatcher,
the ICE parser, the key handlers, the answer/candidate senders and the proto
builder. `analyzeHeadless` was run from `/tmp` (Ghidra searches the working
directory before `-scriptPath`).

Pseudocode alone was not enough for the proto builder's calling convention, so
that one function was cross-checked in arm64 assembly
(`out/rcmesh-1.4.11-signaling/builder-asm.txt`).

## What matched

| Area | Reference | Evidence |
|---|---|---|
| Signaling envelope | `message_id/sender_id/recipient_id/payload_type` + `payload` | Go type 0x10988b010 |
| SDP | `{type, sdp}` | Go type 0x1096edc70 |
| Deduplication key | `sessionID + "\x00" + messageID` | `markSignalingMsgProcessed` 0x1025a6410 |
| Poll target | `instance_id` (the `session_id` argument is nil) | `builder-asm.txt`, `Build` 0x102582f50 |
| Poll request | protojson `PollSignalingMessagesRequest`, `afterTimestamp` as an RFC 3339 string | `pollAndProcessSignalingMessages` 0x1025a24a0 |
| Poll response | `SignalingMessageDetails{sessionId, payload, createdAt}` | `api.descriptor` |
| HTTP | `POST`, `application/json`, 30 s timeout, `Authorization: Bearer …` | `authTransport.RoundTrip` 0x1025a9170 |
| URL suffixes | `/v1internal:pollSignalingMessages`, `…:sendSignalingMessage`, `…:updateInstance` | constants 0x10369d784, 0x1036946d0, 0x103655b46 |
| STUN half of the ICE config | `stunServers []string` | Go type 0x1097d07e8 |

The clone's hand-rolled `encoding/json` request and response bodies are
wire-compatible with the reference's protojson ones, because protojson writes
a `google.protobuf.Timestamp` as an RFC 3339 string — which is exactly what
the clone's `AfterTimestamp` / `CreatedAt` fields carry.

## What was wrong, and what changed

### 1. The TURN servers are strings, not objects — `rcmesh/iceconfig.go` (new)

The reference's ICE configuration (Go type 0x1097d07e8) declares
`turnServers []string`. An entry is either a bare TURN URL or a JSON object
**encoded as a string**; `parseICEConfigPayload` (0x10259da00) trims the entry
and, when it starts with `{`, decodes it into
`{urls []string; username string; credential string}` before
`decodeICEServerURLs` (0x10259e110). Its three own rejections are
`Skipping TURN server entry: JSON object has no urls`,
`…: urls is not a string or a list: %v` and
`…: could not decode the JSON object: %v`.

The clone declared `TurnServers []struct{URLs, Username, Credential}` and
swallowed the mismatch behind `if err == nil`. A cloud that sends the
reference's shape made the clone drop the whole ICE configuration — a machine
with neither STUN nor TURN. The parser now follows the reference: strings
first, the JSON-object form second, `urls` accepted as a string or a list, and
a malformed entry skipped rather than fatal.

### 2. Two payload types were missing — `rcmesh/manager.go`

The reference lowercases `payload_type` and compares it against six values:

| Type | Reference target |
|---|---|
| `offer`, `sdp_offer` | `handleRemoteOffer` |
| `candidate`, `ice_candidate` | `handleRemoteCandidate` |
| `ice_config` | `parseICEConfigPayload` |
| `authorized_key` | `handleAuthorizedKey` |

`candidate` and `ice_candidate` share one jump target in
`processSignalingMessage` (0x1025a3430); the clone only knew the first, so a
cloud sending `ice_candidate` had its candidates dropped. `authorized_key` has
a dedicated handler in the reference (0x1025a42a0) whose own log lines are
`Failed to unmarshal authorized_key payload: %v` and
`Ignoring authorized_key signaling message with empty WebCryptoPubKey`; the
clone only read the trust set from the poll response's bundle, so a key
delivered as a message was discarded. Both types are handled now.

### 3. HTTP 200 is the only success — `rcmesh/signaling_client.go`

The reference checks `status != 200` on the poll path
(`pollSignalingMessages returned status %d`) and answers
`%s unexpected status code: %d` on the send path. The clone accepted any
2xx.

### 4. The public STUN fallback — `rcmesh/manager.go`

`stun:stun.l.google.com:19302` (0x10366d51b), logged by the reference as
`No ICE config delivered; falling back to public STUN`. The clone had no
default STUN at all.

### 5. A failed poll is not fatal — `rcmesh/manager.go`

`pollSignalingLoop` (0x1025a2090) logs `Signaling poll failed: %v`
(0x10366d4ff) and keeps polling; the wait is the larger of an exponential
backoff (doubling, capped at 30s — 0x6fc23ac01) and the cadence. The cadence
comes from `pollStateCadence` (0x1025a1e00): one second while a mesh session
is live, five seconds when idle. The clone returned the poll error, so one
network blip ended the mesh; it now backs off and keeps going, and the idle
cadence is configurable through the new `IdlePollInterval` field.

### 6. The answer's message id — `rcmesh/manager.go`

`sendSignalingAnswer` (0x1025a6690) formats `ans-%d` with the current Unix
nanosecond clock (0x1034c0719); the candidate counterpart is `cand-%d`
(0x1034d25a2). The clone used a random hex nonce.

## Tests

`rcmesh/iceconfig_test.go` and `rcmesh/manager_signaling_test.go` are new;
they cover the TURN string shapes, the `urls` string-or-list rule, the
`ice_candidate` type, the `authorized_key` type, the STUN fallback, the
`ans-` id, the non-fatal poll and the exact-200 rule. The pre-existing
`rcmesh` tests are unchanged and still pass.

```
go build ./cmd/... ./internal/... ./endpoints/... ./configdir/...
go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...
(cd modules/rcmesh-pion && go build ./... && go vet ./...)
```

## Still open

- **Outbound trickle ICE is not implemented.** The reference has
  `sendSignalingCandidate` (0x1025a6b00); the clone only sends a full answer
  with every candidate gathered, and its `Session` interface has no local
  candidate outlet.
- **Per-session key pinning is not implemented.** The reference's
  `authorized_key` path pins a server-vouched key on one session
  (`SetSessionPin`); the clone merges the key into the global trust set.
- **Chunk reassembly is still not wired.** The reference's DataChannel framing
  (`isFinal`, `x-cancel-request`) is understood in `rcmesh/proxy.go` but the
  signaling side does not yet carry it.
- **No live cloud run.** Everything above is binary-derived, not captured.

`webrtcMeshReady` therefore stays off, and the host keeps reporting the mesh
experiment as disabled.

## Limitations

- **Every address is 1.4.11's.** They move on every `agy` release; the seeds
  must be regenerated before an address is cited again.
- **Nothing here is a capture.** The comparison is pseudocode plus assembly
  against the binary. The authentication layout in
  `out/rcmesh-1.4.11/FINDINGS.md` is still not a golden capture, and neither
  is the signaling wire.
- **"Polls by `instance_id`" is read off the call site's first argument being
  nil.** The pseudocode was ambiguous; the arm64 assembly settles it
  (`ldr x3, [sp, #0x48]; cbz x3` skips the SessionId branch). A future
  reference that polls per session would change the clone's request.
- **The exact shape of an `authorized_key` message body is inferred.** The
  reference logs only `Failed to unmarshal authorized_key payload`; the clone
  reads the `AuthorizedKeyBundle` member name `webCryptoPubKeys` and also
  accepts a bare array.
- **`signalingTimeoutMs` is not used.** The reference derives the staging
  timeout from it (`Staging timeout set to %v from the browser signaling
  deadline of %v`); the clone has no staging counterpart yet.
- **`includeAuthorizedKeys` is unconditional in the clone.** The reference
  gates it behind `wantAuthorizedKeyRefresh` (0x1025a2e10). One extra key
  bundle per poll is harmless but not equivalent.
- **`jetski-webchannel.googleapis.com` is not in the string table.** The
  reference assembles it at run time; the host name is known from the
  reference's own log line `Resolved proxyServerURL: "jetski-webchannel.googleapis.com:443"`,
  so this cycle could not re-confirm the assembly path from the binary.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence under `out/rcmesh-1.4.11-signaling/` is Chinese, per the
repository's convention.
