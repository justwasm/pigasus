# Cycle `rc-ace-real` — remove `EnsureRemoteAgentACEVM` (no honest local probe target)

## TL;DR / 摘要

Cycles 1–21 of `exp/rc-server-mode` shipped the transport + JSON-RPC envelope
(rc-ws-transport), the cascade lifecycle surface (rc-cascade-start / -message /
-stream / -cancel), the bi-directional user-interaction RPC
(rc-handle-user-interaction), the four read-only catalog probes
(rc-list-readonly), the two read-only filesystem probes (rc-file-ops), the
three project CRUD methods (rc-projects), the deep-link surface (rc-deep-link),
the four permission-mutation methods (rc-permissions-grants), the two plugin
methods (rc-plugins), and the five trigger methods (rc-triggers). Cycle
rc-status-tool also shipped a fifth method — `EnsureRemoteAgentACEVM` — as a
**parity stub** that returned a canned `{ensured:false, version:"0.0.0-stub",
capabilities:[]}` envelope. The gap-closure row in `docs/cycles/PLAN.md` was:

> `rc-ace-real` | G8 | replace the `EnsureRemoteAgentACEVM` stub with a real
> probe, or accept `-32601 Method not found` and remove the method from help
> | ~150 | TODO

This cycle **removes** the method entirely. Brief Option A (an honest local
probe over `$ACE_VM_SOCKET` or `~/.ace-vm.sock`) is rejected: there is **no
honest local probe target**. Brief Option B (remove the method) is the chosen
path. Dashboards that still post `EnsureRemoteAgentACEVM` over the wire now get
the JSON-RPC 2.0 standard envelope `-32601 "Method not found"` with the method
name echoed in `data`.

## Why Option B — why a local probe is not honest

The brief's Option A assumed an env var (`$ACE_VM_SOCKET`) or a default socket
path (`~/.ace-vm.sock`) that an honest probe could dial. The investigation
**found neither**. Every ACE-related string in the recovered binary points at
the *remote* gRPC path, not at a local socket:

1. **Decompiled reference** (`out/local-macos/dec/1018c8b70_EnsureRemoteAgentACEVM.c`)
   is `codeassistclient.(*CodeAssistClient).EnsureRemoteAgentACEVM` — a
   `codeassistclient` method that posts over the recovered binary's `postRemoteAgentProto`
   RPC channel (the same channel `SendRemoteAgentCascadeMessage` and friends use
   to talk to the remote Code Assist server). The 165-line body contains a
   single `runtime_mallocgc` + `runtime_newobject` setup, one `postRemoteAgentProto`
   call, and a small typed-pointer walk on the reply — there is **no local
   socket dial**, no `os.OpenFile`, no env-var read.

2. **ACE probe siblings** (`mayNeedACEVM`, `ensureACEVM`,
   `(*Backend).ensureACEVM`) live on the `cloudproxy` package —
   `cloudproxy/backend/cloudproxy.mayNeedACEVM` and
   `cloudproxy/backend/cloudproxy.(*Backend).ensureACEVM` (addresses
   `0x101e64640` / `0x101e64790` on this build, `0x80615a0` / `0x80616c0` on
   the prior build). Both names reach the **remote** Code Assist server to
   decide whether the *server side* should spin up an ACE VM. Neither writes
   to or reads from a local socket; neither is reachable from the daemon.

3. **Strings on the binary** (`strings ~/.local/bin/agy | grep -iE
   "ace_vm|ace.vm|mayNeedACEVM|ensureACEVM"`) yield only:

   - `ace_vm_id`, `aceVmId` — proto field names on `StartCascadeRequest` /
     `CancelCascadeRequest` (a *request* field, not a socket path).
   - `AceConfig`, `AceMode`, `AceOptions`, `agySurfaceConfig`,
     `ExperienceSurfaceConfig` — proto types on
     `FetchConfigResponse.SurfaceConfig.Environment.AceConfig`. The ACE config
     is a *server-side* config object, fetched over the wire.
   - `ace_vm_id` and friends — also proto field names (`AceVmIdTprotobuf`,
     `AceVmIdUprotobuf`).
   - `cloudproxy.mayNeedACEVM`, `cloudproxy.(*Backend).ensureACEVM` —
     function names, no socket path.

4. **`--help` / subcommand help**:

   - `~/.local/bin/agy --help 2>&1 | grep -i ace` → no matches. The CLI
     exposes no ACE-related flag.
   - `~/.local/bin/agy remote-control --help` lists `start`, `status`, `stop`
     only. No ACE plumbing.
   - `~/.local/bin/agy remote-control start --help` lists only `--name` and
     `--session`.

5. **Out-directory search** (`rg "ACE_VM|ACE VM|ace.vm.socket|ace-vm.sock"
   out/`): zero matches across `out/seeds/`, `out/decompile/`,
   `out/local-macos/seeds/`, `out/local-macos/dec/`.

**The honest finding**: openagy doesn't ship an ACE runtime, the recovered
binary's ACE plumbing reaches a *remote* gRPC server (the Google-internal Code
Assist service), and there is no local Unix socket / env var / binary to
probe. The Option A envelope `{ok, reachable, socket, latency_ms, version,
probed_at}` is therefore not implementable on this machine — every field
except `probed_at` would be a lie. Option B (removal) is the only path that
doesn't lie to dashboards.

## What lands for forward consumers

- **No method named `EnsureRemoteAgentACEVM`** is registered on the
  `Dispatcher`. The JSON-RPC dispatcher's default arm
  (`internal/remotecontrol/dispatch.go:553`) emits the standard envelope
  `-32601 "Method not found"` for any caller that still posts the name.

- **`Config.ACE` is removed** from `internal/remotecontrol/daemon.go`. The
  rc-status-tool seam had two states (`Stub=true` / `Stub=false`) — both fed
  the same canned shape today, and the production wiring (`cmd/wagyu/
  remote_control.go`, `remote_control_foreground.go`) never populated
  `Config.ACE` (verified by grep). Removing the field is safe.

- **`HostConfig.ACE` is removed** from `internal/remotecontrol/host.go`. The
  field was a sibling of `Config.ACE` (mirroring the
  `Session/TriggersStore/PermissionsStore` pattern); with the seam gone, the
  sibling field is gone too.

- **`internal/remotecontrol/ace.go` is deleted**. The 137-line file
  contained `ACEConfig` / `ACEInfo` / `DefaultACEConfig` / `EnsureACE` — all
  seam types and the canned `stubACEInfo()`. The cycle's deliverable lists
  the seam and the canned shape in this report for archaeology.

- **Parity verifier** (`internal/parityverify/parityverify_test.go`):
  `verifyEnsureACEVM` is rewritten to expect `-32601 "Method not found"` and
  records the row as `"gap"` (parity-verifier "gap" rows do *not* fail the
  test — see `runParity`'s doc on `t.Fail` not being called on gaps). The
  matrix row's cycle attribution moves from `rc-status-tool` to `rc-ace-real`
  so the report attributes the removal correctly.

- **A future `rc-ace-real-bridge` cycle** that wires a thin client over the
  recovered binary's `postRemoteAgentProto` channel (i.e. speaks the
  `EnsureRemoteAgentACEVM` proto from the decompiled
  `codeassistclient` package) is parked in this report's 存疑 section. The
  recovery work would need:

  1. The proto definition of `EnsureRemoteAgentACEVM` (request + reply).
  2. The gRPC channel (`cloudproxy.Backend`'s setup).
  3. The OAuth / ADC plumbing (rc-auth-tokens or the future OAuth cycle).

  Out of scope for this branch.

## JSON-RPC surface (precise)

### Before (cycle rc-status-tool)

```json
{ "jsonrpc": "2.0", "id": 1, "method": "EnsureRemoteAgentACEVM", "params": {} }
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "ensured":      false,
    "version":      "0.0.0-stub",
    "capabilities": []
  }
}
```

### After (cycle rc-ace-real)

Same request, response is now the JSON-RPC 2.0 standard `-32601` envelope:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code":    -32601,
    "message": "Method not found",
    "data":    "EnsureRemoteAgentACEVM"
  }
}
```

- `code: -32601` is the JSON-RPC 2.0 standard "Method not found" code.
- `message: "Method not found"` matches the dispatcher's other
  `-32601` paths (`internal/remotecontrol/dispatch.go:554`).
- `data` carries the request's `method` field verbatim (the
  dispatcher's default arm `&rpcErrorBody{Code: -32601, Message: "Method not
  found", Data: req.Method}`); this lets a dashboard log "we tried method X
  and the daemon doesn't know about it".

## `$ACE_VM_SOCKET` env var resolution

There is no `$ACE_VM_SOCKET` env var (verified via `rg -l "ACE_VM_SOCKET" out/`
→ zero matches) and no default `~/.ace-vm.sock` path (no string matches in
`strings ~/.local/bin/agy | grep ace`). The cycle therefore resolves **no
env var** — the probe target the brief's Option A would have dialled does not
exist. Documented in § 存疑.

## Forward compatibility with `rc-ace-real-bridge`

A future cycle that bridges to the remote gRPC channel (out of scope for this
branch) would:

1. **Restore the seam**: `Config.ACE` (or `Config.ACEBridge`) plus a new
   `ACEProbe func(ctx) ACEInfo` closure.
2. **Re-register the handler**: a new `handleEnsureRemoteAgentACEVM` that
   calls the closure and surfaces `{ensured, version, capabilities}` (the
   brief-pinned shape from rc-status-tool) or a richer envelope
   `{ok, reachable, socket, latency_ms, version, probed_at}` if the bridge
   supports latency probing.
3. **Wire the gRPC client**: a new `internal/aceclient/` package (or
   extension of `internal/piagentbridge/`) that wraps
   `codeassistclient.(*CodeAssistClient).EnsureRemoteAgentACEVM` over the
   recovered proto. The `gateway.NewRemoteCascadeSession` factory pattern
   (rc-piagent-bind) is the model.
4. **Wire OAuth**: the `codeassistclient` requires an OAuth bearer (the same
   auth path `rc-auth-tokens` parks). A bridge cycle can't ship until
   `rc-auth-tokens` lands.

For this cycle, none of the above is on hand, so the method is removed.

## Pieces touched (file-by-file)

| Piece | What changed |
|---|---|
| `internal/remotecontrol/ace.go` | **deleted** (137 lines: `ACEConfig`, `ACEInfo`, `DefaultACEConfig`, `EnsureACE`, `stubACEInfo`). The seam types are now archaeology; this report records them. |
| `internal/remotecontrol/dispatch.go` | removed `case "EnsureRemoteAgentACEVM":` switch arm (10 lines); removed `handleEnsureRemoteAgentACEVM` function (~50 lines); updated the `// ---- cycle rc-status-tool handlers ----` comment to reflect 4 methods, not 5; added a tombstone comment pointing future readers at the cycle deliverable. |
| `internal/remotecontrol/daemon.go` | removed `Config.ACE ACEConfig` field (7 lines of comment + 1 line of code). The production wiring (`cmd/wagyu/remote_control.go` + `cmd/wagyu/remote_control_foreground.go`) never populated this field (verified via grep) so production behavior is unchanged. |
| `internal/remotecontrol/host.go` | removed `HostConfig.ACE ACEConfig` field (7 lines). Mirrored the daemon.go change; production wiring never populated this field. |
| `internal/remotecontrol/status_test.go` | replaced `TestEnsureRemoteAgentACE_Stub` with `TestEnsureRemoteAgentACEVM_Removed` (asserts `-32601 "Method not found"` with `data == "EnsureRemoteAgentACEVM"`); updated the file's header doc-comment from "Five methods" → "Four methods" and added a tombstone noting the cycle rc-ace-real change. |
| `internal/remotetransport/tcp_test.go` | replaced `TestEnsureRemoteAgentACE_Stub_WS` with `TestEnsureRemoteAgentACEVM_WS_Removed` (same envelope, end-to-end on the WS transport); updated the cycle rc-status-tool section header from "four new JSON-RPC methods (RemoteAgentErrorStatus / EnsureRemoteAgentACEVM / SetRemoteControlSession / remoteControlVersion)" → "three new JSON-RPC methods (RemoteAgentErrorStatus / SetRemoteControlSession / remoteControlVersion)". |
| `internal/parityverify/parityverify_test.go` | rewrote `verifyEnsureACEVM` to expect `-32601` and return `"gap"` (parity-verifier gaps do not fail the test — see doc on `runParity`); updated the matrix row's cycle attribution from `rc-status-tool` → `rc-ace-real`. The matrix still records this method (so a future rc-ace-real-bridge cycle can flip it back to `"verified"`). |

## Stats

```
internal/remotecontrol/ace.go                    | -137
internal/remotecontrol/dispatch.go               | -65 / +6
internal/remotecontrol/daemon.go                 | -8 / +0
internal/remotecontrol/host.go                   | -8 / +0
internal/remotecontrol/status_test.go            | -32 / +34
internal/remotetransport/tcp_test.go             | -38 / +44
internal/parityverify/parityverify_test.go       | -12 / +25
```

Net: **−187 lines** across 7 files. The seam types `ACEConfig` / `ACEInfo`
move from runtime to archaeology; the parity-verifier harness keeps a row
that returns `"gap"` until a future bridge cycle flips it back to
`"verified"`.

## Tests

### In-process (replaces rc-status-tool parity stub)

- `TestEnsureRemoteAgentACEVM_Removed` (`internal/remotecontrol/status_test.go`):
  posts `EnsureRemoteAgentACEVM` to a fresh dispatcher; asserts the
  envelope is `{"error":{"code":-32601,"message":"Method not found","data":"EnsureRemoteAgentACEVM"}}`.
  Uses the shared `errorCode` helper (defined in
  `internal/remotecontrol/cascade_test.go:1596`) which accepts both the
  in-process `int` code and the wire-decoded `float64` code.

### Wire-level

- `TestEnsureRemoteAgentACEVM_WS_Removed` (`internal/remotetransport/tcp_test.go`):
  end-to-end on the WS transport; the `coder/websocket` round-trip emits
  `code` as `float64` (verified — the wire test passes). The same
  `-32601 "Method not found"` envelope shape is asserted, plus the
  `data == "EnsureRemoteAgentACEVM"` echo.

### Parity verifier

- `TestParityMatrix` (`internal/parityverify/parityverify_test.go`):
  `verifyEnsureACEVM` returns `("gap", "removed in rc-ace-real: method no
  longer registered; -32601 on wire")`. The matrix row's
  `Status` is `gap` (which the harness does not fail on, per its doc).

### What deliberately does **not** exist

- A `Config.ACE` / `HostConfig.ACE` field. Removing the seam means the
  dispatcher has no plumbing to wire an `ACEProbe` closure into — that's the
  next cycle (`rc-ace-real-bridge`) job.
- A `internal/remotecontrol/ace.go` (deleted). Any future code that wants to
  reference the old seam must move it back from this deliverable.

## Limitations / 存疑 / 硬约束

- **No `rc-ace-real-bridge` plan yet.** The future cycle that bridges to the
  recovered binary's remote gRPC channel (via `codeassistclient` /
  `cloudproxy`) requires (a) the proto definition of
  `EnsureRemoteAgentACEVM` (currently absent from this repo), (b) the
  gRPC channel setup, and (c) OAuth bearer plumbing (the same auth path
  `rc-auth-tokens` parks). All three are months-of-work items; the cycle is
  **not scheduled** in `docs/cycles/PLAN.md`. This report's
  "Forward compatibility with rc-ace-real-bridge" section is the
  architectural note.

- **Dashboards that still call `EnsureRemoteAgentACEVM`** will see `-32601`.
  This is the documented contract. The cycle's deliverable does not wire
  any backward-compat path — the brief's Option B explicitly says "remove
  the method from the dispatcher". Dashboards that want to detect "ACE VM
  not configured on this host" should treat `-32601` as that signal.

- **No help-text removal**. The dispatcher does not advertise methods via
  help text (the only `tools/list` reply carries `status` / `list_agents` /
  `get_output`). Removing the method required no help-text edit. The brief
  asked to "update any help text" defensively; this report confirms no such
  edit was needed.

- **`gofmt -l` clean** on every file the cycle added or modified
  (`internal/remotecontrol/dispatch.go`, `daemon.go`, `host.go`,
  `status_test.go`; `internal/remotetransport/tcp_test.go`;
  `internal/parityverify/parityverify_test.go`). Verified.

- **`go vet` clean** on `./internal/remotecontrol/ ./internal/remotetransport/
  ./cmd/wagyu/` except the pre-existing
  `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf` warning (out of
  scope, recorded in `AGENTS.md`).

- **`go build -o /tmp/wagyu ./cmd/wagyu` exit 0** on this machine (macOS/arm64,
  Go 1.27.0). Verified.

- **Pre-existing `gofmt` dirt untouched.** 50 files under `internal/` plus
  `cmd/wagyu/picker_view.go` and `cmd/wagyu/tui_v135_test.go` retain their
  pre-cycle formatting; the cycle only formats files it touched.

- **In-process tests** (`go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/`) PASS on this machine. Verified.

- **WebSocket lib unchanged** (`github.com/coder/websocket v1.8.15`). No
  third-party deps added.

- **No probe of `/logout`, `/exit`, or `/keybindings`**. Cycle has no print
  path; the wire-level test only round-trips `EnsureRemoteAgentACEVM`.

- **No sub-agent race** with other openagy-rc subagents: the only
  modifications are inside `internal/remotecontrol/{ace.go removed,
  dispatch.go, daemon.go, host.go, status_test.go}`,
  `internal/remotetransport/tcp_test.go`, and
  `internal/parityverify/parityverify_test.go`. The pre-existing
  `/tmp/rc-triggers-worktree-snapshot.tar.gz` snapshot was left alone (the
  brief explicitly called it out).

---

## Reviewer commands

```bash
cd /Users/gear/Documents/GitHub/openagy-rc

# 1. Build
go build -o /tmp/wagyu ./cmd/wagyu

# 2. Vet (the pre-existing model_default_effort_test.go:67
#    3-arg Errorf warning stays; everything else clean)
go vet ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/

# 3. gofmt (clean on touched files)
gofmt -l \
  internal/remotecontrol/dispatch.go \
  internal/remotecontrol/daemon.go \
  internal/remotecontrol/host.go \
  internal/remotecontrol/status_test.go \
  internal/remotetransport/tcp_test.go \
  internal/parityverify/parityverify_test.go

# 4. In-process + wire-level tests (race detector)
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/

# 5. Run just the ACE tests for a tight loop
go test -race -count=1 ./internal/remotecontrol/ -run "TestEnsureRemoteAgentACEVM"
go test -race -count=1 ./internal/remotetransport/ -run "TestEnsureRemoteAgentACEVM"

# 6. Parity verifier (skipped under -short because it needs the
#    built binary; run it explicitly if you want the matrix row)
go test -race -count=1 ./internal/parityverify/

# 7. Confirm no stale references to the deleted seam
git grep -n "EnsureACE\b\|ACEConfig\b\|stubACEInfo\|cfg\.ACE\|HostConfig\.ACE" -- \
  internal/ cmd/ docs/cycles/rc-ace-real/
# (must print no matches inside cmd/wagyu or the dispatcher package)
```

## Diff stat (this commit)

```
internal/remotecontrol/ace.go                    | 137 ---------------
internal/remotecontrol/dispatch.go               |  65 +------
internal/remotecontrol/daemon.go                 |   8 -
internal/remotecontrol/host.go                   |   8 -
internal/remotecontrol/status_test.go            |  32 ++---
internal/remotetransport/tcp_test.go             |  38 ++---
internal/parityverify/parityverify_test.go       |  12 ++-
docs/cycles/PLAN.md                              |   4 +-
docs/cycles/rc-ace-real/deliverable.md           | (new file, ~360 lines)
```

(One commit, one cycle, one report. No reformatting of pre-existing files.)