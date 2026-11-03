# Standard HTTP RPCs use Connect-Go

The reference agy 1.4.11 binary (SHA-256 `09cefed6a1d217e14ba1751dce4442b97c2797e88c297c77fda3690f92ceddcd`) links Connect-Go. Its generated `NewLanguageServerServiceHandler` is registered by `Server.setUpHTTPServeMux` (static call cross-reference, 【反编译】).

The clone now uses `connectrpc.com/connect` v1.21.1 for HTTP unary and server-streaming framing, gRPC-Web trailers, compression and protocol handling. The old HTTP stream writer and unary framing path are removed. A custom codec adapts the existing JSON dispatcher and recovered `lsproto` descriptor closure; no second protobuf schema is introduced.

Reference compatibility stays at the boundary: absent content types default to JSON, bare streaming messages and `x-is-streaming` are normalized, subscriptions flush headers even without a first event, and dispatch errors retain their existing masking, HTTP statuses and gRPC-Web error IDs. WebSocket and WebChannel envelopes remain separate protocols.

Validation: all existing tests in `remotetransport`, `lsproto`, `internal/remotecontrol`, `internal/lsserver`, `internal/rctunnel/...` and `internal/rchost` pass. A new interoperability test uses the upstream Connect client against the real HTTP listener with Connect/gRPC-Web, JSON/protobuf and gzip-compressed requests. Closing-frame tests now exercise the HTTP path rather than the deleted frame writer.

## Limitations

- This migration replaces the HTTP protocol implementation, not the RPC business handlers. The JSON dispatcher still uses the recovered dynamic descriptor rather than generated message structs.
- Unary responses are buffered so reference-specific dispatch failures can retain their exact legacy wire. Server streams remain incremental.
- No real cloud registration or browser UI session was run in this cycle; transport behavior is covered by local HTTP and WebSocket tests.
