# Mesh signaling HTTP boundary

2026-10-31. Adds an injected, standard-library-only signaling HTTP client under
rcmesh. Poll and Send shapes follow the recovered api.proto descriptor in
out/rcmesh-1.4.11/api.descriptor. The inner payload stays a string containing the
snake_case SignalingEnvelope; it is not base64 or a nested outer JSON object.

Polling requires exactly one session/instance target and validates the optional
RFC3339 timestamp. Authorized-key bundle delivery/presence is retained, without
interpreting a missing bundle as an empty replacement key set. The host supplies
complete method URLs and an account-authenticated HTTP client. No endpoint is
guessed from older notes, and no token storage or native dependency is added.
Requests have a 30-second context bound, responses a 4 MiB bound, and redirects
are refused. HTTP errors do not expose response bodies containing credentials.

Validation: local httptest covers poll wire, nested send payload, bundle decode,
invalid targets/timestamps, redirects and HTTP errors. go test -race ./rcmesh
passes. The package builds for js/wasm and wasip1/wasm.

## Limitations and next work

This delivers the HTTP boundary only. No new binary decompilation or live cloud
interoperability was performed in this cycle. Poll URL, authentication headers,
retry/backoff, cursor advancement, deduplication, authorized-key revocation,
peer lifetime management and host wiring still need alignment. In particular,
the session manager must never trust a public key arriving in an offer, and
must distinguish an absent authorized bundle from a delivered replacement.
The client does not enable mesh readiness. Existing Pion code, root go.mod and
other agents' files remain unchanged.
