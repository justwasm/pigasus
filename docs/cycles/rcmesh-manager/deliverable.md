# Injected signaling/session composition and positive end-to-end acceptance

2026-10-31. rcmesh.Manager composes Signaler and Answerer into an end-to-end
WebRTC mesh session manager. It polls for instance messages and authorized keys,
validates recipient/session/message identity, applies dynamic `ice_config`
(STUN/TURN servers and max concurrent sessions), buffers and applies trickle ICE
candidates (`candidate`), creates peers exclusively with trusted public keys,
sends gathered answers, and manages peer lifecycle.

Absent or undelivered key bundles preserve the current trust set. A delivered
replacement closes existing peers conservatively on any key change, including
an empty replacement. Duplicate messages use a rolling 4096-entry deduplication
window to prevent memory growth while ensuring continuous operation without
premature termination. Unknown non-critical signaling messages are skipped
matching reference behavior.

## Positive End-to-End Validation

In addition to rejection paths and unit tests:
1. `GOWORK=off go -C modules/rcmesh-pion test -race -count=1 .`:
   - `TestPositiveEndToEndFlow`: verifies the complete forward flow:
     - Signaling polling (`PollRequest{IncludeAuthorizedKeys: true}`)
     - Authorized ECDSA P-256 public key delivery
     - SDP Offer processing and `rcmeshpion.NewAnswer` creation
     - SDP Answer delivery via `Signaler.Send`
     - WebRTC PeerConnection and DataChannel establishment
     - Challenge generation and P-256 channel-binding signature verification
     - `ConnectInstanceRequestV2` dispatch through local HTTP handler
     - `ConnectInstanceResponseV2` verification over DataChannel
   - `TestPositiveEndToEndFlowWithTrickleCandidates`: verifies remote trickle ICE
     candidates arriving via signaling before and during handshake.
2. `go test -race ./rcmesh -count=1`: passes with race detector enabled.
3. Both `rcmesh` and `modules/rcmesh-pion` build cleanly for `GOOS=js GOARCH=wasm`.

## Host Wiring and Endpoints Integration

Following local verification, the stack was wired into the host runtime:
1. `endpoints`:
   - Added `poll_signaling` (`/v1internal:pollSignalingMessages`) and `send_signaling` (`/v1internal:sendSignalingMessage`) to named endpoints.
   - Values automatically derive from the configured `webchannel` host or configuration override (`PollSignalingURL()`, `SendSignalingURL()`).
2. `rcmesh`:
   - Added `TokenFunc` to `SignalingClient` to inject `Authorization: Bearer <token>` on signaling HTTP requests.
   - Added `RegisterDefaultAnswerer` / `DefaultAnswerer` to allow dynamic and modular registration of WebRTC drivers without polluting root module dependencies.
3. `modules/rcmesh-pion`:
   - Auto-registers `rcmeshpion.NewAnswer` via `init()` without requiring Pion imports in WASM or root builds.
4. `internal/rchost`:
   - `startServeMesh` runs `rcmesh.Manager` alongside `rctunnel.Client`, automatically resolving active account tokens and routing DataChannel requests to `srv.Handler()`. Returns a no-op when no WebRTC answerer is registered.
5. `internal/remotecontrol`:
   - Added experimental opt-in `OPENAGY_EXPERIMENTAL_WEBRTC_MESH` for staged browser testing; default remains safely disabled (`webrtcMeshReady: false`).

## Limitations

This wires the host runtime, signaling endpoints, and Pion driver. No live
cloud exchange with Google's signaling servers was performed in this cycle.
Readiness flags (`webrtcMeshReady`) remain disabled by default in mendel flags,
keeping WebChannel as the active transport for production remote control until
live cloud acceptance is conducted.
