# Closing the Three WebRTC Mesh Gaps: Trickle ICE, Session Key Pinning, and Chunk Reassembly

2026-10-31. This cycle closes all three functional gaps identified in
Cycle 372 (`docs/cycles/cycle-372-rcmesh-signaling-verification/deliverable.md`),
verifies the behaviors against the Antigravity CLI `agy` 1.4.11 reference binary
(`09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd`), and
integrates end-to-end DataChannel chunk reassembly with the Pion adapter.

---

## The Three Gaps Closed

### 1. Outbound Trickle ICE (`rcmesh/peer.go`, `rcmesh/manager.go`, `modules/rcmesh-pion/peer.go`)

- **Wiring**: Added `OnLocalCandidate func(ICECandidate)` to `rcmesh.PeerConfig`.
  `modules/rcmesh-pion/peer.go` wires `pc.OnICECandidate` to this hook.
- **Envelope & Ordering**: `handleRemoteOffer` (0x1025a50e0) formats answer
  messages as `ans-%d` and candidate messages as `cand-%d` with current Unix
  nanosecond timestamps. Answer delivery is guaranteed before candidate streaming;
  local candidates discovered during SDP answer generation are queued and flushed
  immediately after the answer is posted to avoid `SetRemoteDescription` failures
  at the remote client.
- **Wire Shape**: Outbound candidates carry the bare pion `ICECandidateInit` JSON
  in `SendSignalingMessageRequest.payload` (`postSignalingMessage` 0x1025a6dc0),
  while routing parameters (`message_id`, `sender_id`, `recipient_id`, `payload_type: "candidate"`)
  are populated in the outer protojson envelope.

### 2. Per-Session Key Pinning and Revocation (`rcmesh/auth.go`, `rcmesh/manager.go`, `rcmesh/peer.go`)

- **Session Pinning (`SetSessionPin` 0x102596850)**: When an `authorized_key`
  signaling message arrives, the server-vouched public keys are pinned specifically
  to that `sessionID`.
- **Pending Pins (`expirePendingPinsLocked` 0x102596b90, `evictOldestPendingPinLocked` 0x102597260)**:
  Pins arriving prior to SDP offer processing are recorded in a bounded
  32-entry `pendingPins` map with a 5-second expiration deadline. If the handshake
  arrives before timeout, the session inherits the pinned keys rather than the
  global trust bundle.
- **Dynamic Revocation**: If a pin update arrives after authentication has
  completed and the refreshed key set no longer covers the session's
  authenticated key (`SessionIdentity.AuthenticatedKey`), the session is closed
  immediately (`[MeshDaemon] Revoking session %q: a refreshed authorized bundle no longer contains its authenticated key`).

### 3. DataChannel Chunk Reassembly (`rcmesh/chunks.go`, `modules/rcmesh-pion/peer.go`)

- **Decompiled Framing**:
  - Multi-frame requests arrive with `isFinal: false` across intermediate chunks
    and `isFinal: true` on the terminating chunk (or absent, defaulting to `true`
    for single-frame requests).
  - The **first** chunk of a request carries the `rpcPath`; the continuations
    carry none. A chunk that finds no active buffer **and** carries no `rpcPath`
    is an orphan (`handleWebRTCMessage` 0x10259ed10: "Rejecting orphaned
    terminal chunk with empty rpcPath" / "Rejecting orphaned continuation chunk
    with empty rpcPath").
  - Intermediate frames are gathered by `rcmesh.ChunkReassembler` (`0x10259e630`).
- **Safety Limits & Error Codes** (verified against `handleWebRTCMessage`
  0x10259ed10 and `appendChunk` 0x10259e630 — the evidence is
  `out/rcmesh-1.4.11-reassembly/`):
  - **4 MiB cumulative body limit per request**: `MaxChunkBytes = 4 << 20`
    (HTTP 413, `cumulative size %d exceeds limit %d`).
  - **64 MiB instance-wide buffer limit**: `MaxInstanceChunkBytes = 64 << 20`
    (HTTP **507**, `instance reassembly buffer capacity exceeded` — *not* 413).
  - **No per-session byte limit.** The reference has no session byte budget: a
    session's bytes count against the instance budget, and the session's own
    guard is the number of concurrent chunked requests. The clone's
    `MaxSessionChunkBytes` (4 MiB, HTTP 413) was invented and has been removed.
  - **Max concurrent chunked requests**: 32 concurrent requests per session
    (`MaxSessionChunkRequests = 32`, HTTP 429, `too many concurrent chunked
    requests for session`). The reference rejects once the session's map holds
    more than 0x1f (31) entries.
  - **Payload arm consistency**: Request frames within the same chunk stream must
    not mix `payload` (bytes) and `payloadText` (string) (`payload arm mismatch in chunk stream`, HTTP 400).
  - **RPC path consistency**: a chunk that carries a `rpcPath` different from the
    buffer's is rejected (`mismatched rpcPath on %s chunk`, HTTP 400).
  - **Request id**: a non-positive `request_id` is rejected with HTTP 400 and
    `request_id must be a positive integer`. A `request_id` that is not an
    integer never reaches the reassembler — protojson refuses the frame, so the
    frame is an unparseable ingress message.
  - The reference drops the request's buffer before it answers a rejected chunk;
    the clone does the same.
- **Rejection Frames**:
  - A rejected chunk is answered with the reference's own error frame, not with a
    closed channel. `errorResponseV2` (0x1025ac8f0) builds a `ProxyResponseV2`
    (request id, status code, `is_final` true, the message as `payload_text`),
    wraps it in a `ConnectInstanceRequestV2`, and `sendErrorResponse` (0x1025b22a0)
    marshals it with protojson — the same envelope a normal response uses:
    `{"proxyResponse":{"requestId":"42","statusCode":400,"isFinal":true,"payloadText":"…"}}`.
    The clone's `rcmesh.ErrorResponse(requestID, status, message)` builds it.
  - An **unparseable** ingress frame is dropped and the session survives it
    ("%s Dropping an unparseable %d-byte ingress message on mesh session %s: %v").
    The clone used to fail the peer on it.
- **Session Scoping**:
  - The reference keys its reassembly buffers by the mesh session. `PeerConfig`
    gained a `SessionID` field and `rcmesh.Manager` fills it from the signaling
    session, so the clone's buffers are scoped the same way.
- **Cancellation & Cleanup**:
  - When `x-cancel-request: true` arrives, `Abort(sessionID, requestID)` drops
    accumulated chunks and active RPC contexts are canceled.
  - Stale reassembly cleaner ticks every 5 seconds and evicts chunk buffers idle
    for more than 15 seconds (`ChunkReassemblyTimeout = 15 * time.Second`).

---

## Test Verification

1. `go test -v ./rcmesh/... -count=1`:
   - 42 unit tests pass cleanly, covering:
     - Channel binding payload format and arm64 layout parity
     - Authenticator ECDSA P-256 verification and session key pinning
     - Chunk reassembler text, binary, orphan chunk, non-positive request id,
       rpcPath mismatch, arm mismatch, cumulative limit (413), instance capacity
       (507), session concurrency (429), session scope, abort and purge
     - The error frame's wire shape
     - Trickle ICE and answer message ID formatting
     - Signaling client wire and status code handling
2. `(cd modules/rcmesh-pion && go test -v ./... -count=1)`:
   - 15 integration tests pass cleanly, covering:
     - `TestPositiveEndToEndFlow`: full poll → key delivery → offer → answer → DTLS channel-binding → RPC roundtrip
     - `TestPositiveEndToEndFlowWithTrickleCandidates`: trickle candidate streaming during setup
     - `TestChunkedRequestReassemblyOverDataChannel`: multi-chunk requests reassembled and served over WebRTC DataChannel
     - `TestChunkedRequestAbortedOnCancellation`: cancellation aborts in-flight chunk reassembly
     - `TestOrphanChunkAnswersErrorFrameAndKeepsPeer`: an orphan chunk is answered
       with the reference's 400 frame and the peer survives
     - `TestNonPositiveRequestIDAnswersErrorFrame`: a non-positive request id is
       answered with the reference's 400 frame
     - `TestUnparseableFrameIsDroppedAndPeerSurvives`: an unparseable frame is
       dropped and the peer survives
     - `TestSetSessionPinAllowsLateKeyUpdate`: key pinning late arrivals
3. `go test -v ./internal/rchost/... -count=1` and `go test -v ./internal/remotecontrol/... -count=1`:
   - All tests pass, verifying no regressions across host runtime, daemon, and Connect/gRPC-Web endpoints.

---

## State of the WebRTC Mesh

| Capability | Status |
|---|---|
| Inbound trickle ICE | Supported (`handleRemoteCandidate`) |
| Outbound trickle ICE | Supported (`OnLocalCandidate` → `cand-%d`) |
| Channel-binding layout & P-256 auth | Verified against 1.4.11 decompilation |
| Session key pinning & revocation | Verified (`SetSessionPin`, `pendingPins`) |
| Multi-chunk request reassembly | Supported (`ChunkReassembler`, verified statuses) |
| Rejection frames & status codes | Verified (`errorResponseV2`, `sendErrorResponse`) |
| Signaling wire envelope / bare payload | Verified against `postSignalingMessage` |
| Live Google Cloud exchange | Pending real browser / cloud test |
| Production flag (`webrtcMeshReady`) | Disabled (`false`) by default; opt-in with `OPENAGY_EXPERIMENTAL_WEBRTC_MESH` |

The implementation is functionally complete and locally verified. Live cloud
integration and readiness activation will be performed when end-to-end cloud
handshake testing is scheduled.

## Limitations

- The clone's `ChunkReassembler` is **per peer**, while the reference's
  `dataChannelReassemblyManager` is **per instance**. The instance-wide 64 MiB
  budget is therefore per peer in the clone. The buffers themselves are scoped by
  session (`PeerConfig.SessionID`), as the reference's are.
- The reference's HTTP 503 (`server instance is shutting down`) has no clone
  counterpart: the clone's peer has no "instance is closing" state.
- The evidence for this cycle lives in
  `out/rcmesh-1.4.11-reassembly/` (32 decompilations, `FINDINGS.md` in
  zh-CN). Every address in it belongs to `agy` 1.4.11 and moves on every
  release.
