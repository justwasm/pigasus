# Dependency migration delivery review and next work

2026-10-31. Read-only review of committed Connect-Go migration `b11bb0a5` and
MCP SDK migration `c3ed99b5`, with the shared tree at HEAD `25603bb9`. Other
agents' uncommitted changes were neither reverted nor included in this work.
The reviewer authored the earlier Connect change; that part is a self-review,
not an independent sign-off. This review does not approve unrelated account,
proxy or configuration changes being edited concurrently.

## Assessment

Both dependency choices have stronger support than a strings hit: the matching
agy 1.4.11 binary contains business call edges into Connect handlers and the
official MCP client. The migrations are useful module-level deliveries. The
evidence does not establish complete application-wide migration or original
browser interoperability. Avoid a percentage or a full-parity claim based on
tests of a small adapter surface.

The upstream dependency investigation is archived in
[`out/dependency-audit-1.4.11`](../../../out/dependency-audit-1.4.11/FINDINGS.md).
That report predates these commits. It distinguishes linked symbols, actual
call edges, and hypotheses; notably it withdraws the prior claim that an
independent SSE library should be the first upstream-aligned replacement.

| Delivery | What is accepted by this review | What remains open |
|---|---|---|
| Connect-Go HTTP transport | Standard HTTP framing delegated to the library; existing dispatcher, recovered descriptor codec, WebSocket dialect and reference error behavior retained | Generated service/message types, production browser acceptance, broader stream compression/cancellation/slow-peer tests |
| Official MCP SDK pool | Explicit MCP tool/resource pool now creates SDK ClientSessions and SDK transports; rendering/configuration remain in project code | Public factory compatibility and the separate PiG runtime MCP stack |
| Standard Gemini SDK migration | Correct next candidate based on an actual upstream genai.NewClient call | No completed migration observed in these commits; root backend still manually implements the standard Gemini API |
| WebRTC | Separate local prototype delivered by the companion mesh cycle | Cloud signaling, original browser/TURN/fallback and production composition |

## Actionable findings

### P2: MCP migration is scoped to the explicit pool, not the runtime's MCP owner

`mcp/client.go` creates official SDK sessions. However,
`internal/pigrt/options.go` still configures PiG's built-in MCP extension via
`builtin.Options.ConfigureMcp`, `o.Mcp.LoadConfig`, and optional
`mcpext.TransportFactory`. These are two separate connection paths. The new
SDK pool does not replace the runtime's default MCP transport, discovery or
session lifecycle merely by being present in go.mod.

Next: enumerate callers of both paths and choose one session/process owner.
Bridge SDK-backed transport behind the runtime seam if appropriate, retaining
the shared extension runtime and lazy/always-loaded tool rules. An acceptance
test must run a real PiG turn with the same configured MCP server used by the
explicit tools, asserting the intended process/session count, tool discovery,
permission hooks, resource reads and shutdown. Do not remove PiG's extension
without replacing those behaviors.

### P2: the public transport-factory change needs an explicit compatibility plan

Before `c3ed99b5`, `mcp.TransportFactory` returned PiG's `mcp.Transport`.
Afterwards it returns `github.com/modelcontextprotocol/go-sdk/mcp.Transport`.
The same exported setter name now requires a different function signature;
external callers that inject the old factory will not compile. The current
in-repository tests were migrated and do not prove source compatibility.

Next: either preserve a separately named legacy adapter during transition, or
document and version the breaking public API change with a minimal downstream
compile example. Include a lifecycle test for connect/close/cancel races and
disconnected-session recovery. This review identifies test obligations; it
does not claim those races are newly introduced defects.

### WASI build boundary still requires platform work, independently of WebRTC

`cmd/pigga` builds successfully for `GOOS=js GOARCH=wasm`, before and after the
mesh work, and its import graph contains no Pion packages. `wasip1/wasm`
separately fails in existing PTY and Unix process code: `creack/pty` ioctl and
SysProcAttr fields, `procgroup/procgroup_unix.go`, and
`internal/infra/daemon/{process_status_unix,tracker_sys_unix}.go` use APIs the
target does not provide. Their tags exclude JS but admit WASI. This was present
before adding rcmesh; the new protocol and adapter stub compile for both.

Next: the platform owner should decide the supported WASM target matrix and
split PTY/process implementations accordingly. Keep that work separate from
Pion, and record full-command builds in CI for each supported target. Do not
report the JS build as broken, or infer a native dependency problem from the
WASI error output.

## Recommended order for the other agents

1. Close the MCP public API and lifecycle ownership questions above. Verify a
   runtime turn rather than treating client-pool tests as application migration.
2. Finish standard Gemini SDK integration with captured wire fixtures: API-key
   placement, URL escaping, client headers, system/tool conversion, streaming
   partial errors, HTTPError preservation and existing retry policy. Keep the
   CloudCode dialect and continuation behavior in `geminicode`.
3. Add original-browser acceptance for the Connect migration and service schema
   coverage. Preserve reference-specific unknown-error masking and the custom
   WebSocket stream envelope when expanding use of generated types.
4. For mesh, follow the independent protocol/native seam and the explicit
   remaining steps in the [mesh report](../rcmesh-modular/deliverable.md).
   A feature flag cannot stand in for authenticated signaling and browser
   interoperability. Keep readiness off until the composed path works.

Do not replace model retrybudget with RetryableHTTP defaults or select an SSE
library merely because a package name looked familiar. The confirmed upstream
RetryableHTTP caller is SCM initialization; model SSE still has custom
Scanner/event conversion. Those need behavioral evidence before replacement.

## Validation and limitations

`go test -race ./rcmesh ./mcp ./remotetransport` passed against the inspected
shared tree. Earlier targeted Connect migration checks also covered
`lsproto`, `internal/remotecontrol`, `internal/lsserver`, `internal/rctunnel/...`
and `internal/rchost`. No root `go test ./...` was run. The companion native
module was tested independently with actual Pion peers and the race detector.

These checks do not cover live Google services, original browser mesh, all
MCP server implementations, production OAuth behavior, all concurrent edits,
or application-wide dependency elimination. No complete test suite or Gemini
SDK delivery was asserted. Line numbers and HEAD can move while the other
agents work; findings are pinned to the two named commits and the cited code
paths rather than another agent's uncommitted diff.
