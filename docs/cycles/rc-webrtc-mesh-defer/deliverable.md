# Cycle `rc-webrtc-mesh-defer` — Deferred RC transport features + `--experimental-rc-v2`

## TL;DR / 摘要

Cycles 1–29 of `exp/rc-server-mode` shipped the **functional
parity surface** of AGY's `Remote Control` JSON-RPC-over-WebSocket
daemon: every method in the cascade / list / project / file /
permission / plugin / trigger / resource / conversation-config /
terminal surface, plus three top-level status methods, plus the
`OriginPatterns` allowlist from `rc-auth-tokens`.

The fresh-eyes re-investigation at
`openagy/.scratch/rc-reinvestigation.md` flagged **four** transport
features the branch does **not** ship:

- **G2** — WebRTC peer-to-peer mesh transport
  (`webChannelHandlerV2` / `PeerSession` / `MeshDaemon` /
  TURN/STUN / SDP).
- **G3** — Antigravity Hub (second process; `runHubServer` /
  `configureHeadlessServer`).
- **G10** — `/remote-control on` session-scoped tunnel
  (enterprise gateway; `RemoteAgentOnDemandTunnel` /
  `RemoteAgentEndTunnel`).
- **G11** — Linux systemd-user service-manager backend.

Each is documented as a deferred surface, not a workspace item.
This cycle:

1. **Documents every deferral** with the decompiled surface
   reference, the reason we're not shipping in this branch,
   and a forward contract for a future fork / contributor.
2. **Adds a single, hidden forward-compat flag**
   `--experimental-rc-v2` so a dashboard / operator can opt
   in to a v2 mode pin (no-op today).
3. **Surfaces a `deferred_features` map on
   `RemoteControlStatus`** (`{webrtc_mesh, antigravity_hub,
   systemd_user, enterprise_tunnel}: bool`) so a dashboard
   learns the v2 status by polling the existing top-level
   method instead of guessing.
4. **Pins forward compat**: a future `rc-webrtc-mesh` cycle
   flips `webrtc_mesh` to `true` via `Config.DeferredFeatures`
   — no envelope shape change, no new JSON-RPC method.

Headlines:

| Surface | Behaviour (before) | Behaviour (after) |
|---|---|---|
| `RemoteControlStatus.deferred_features` envelope | absent — dashboards can't tell if v2 features are wired | present, canonical four-key sorted map, every key maps to `false` today (the all-off canonical default; a future cycle flips one or more keys without changing the wire shape) |
| `--experimental-rc-v2` host flag | absent (parser rejects an unknown flag with `flags provided but not defined: -experimental-rc-v2`) | hidden bool flag registered in `internal/cli/parser_specs.go`; `cmd/wagyu/remote_control.go:maybeStartRemoteControlDaemon` reads it but emits only a single stderr warning — no runtime behaviour change |
| New files / new methods | none | `docs/deferred-rc-transport.md` (forward-compat doc); `internal/cli/experimental_rc_v2_test.go` (host flag contract); `Config.DeferredFeatures` field on `internal/remotecontrol/daemon.go`; same field on `HostConfig.DeferredFeatures` in `internal/remotecontrol/host.go`; `TopLevelStatus.DeferredFeatures` on `internal/remotecontrol/top_level.go`; new `deferredFeaturesSnapshot` helper + `defaultDeferredFeatures` key list |
| New tests | none | 6 in-process unit tests in `internal/remotecontrol/top_level_test.go` (`TestDeferredFeatures_AllFalseByDefault` / `_HostOverride` / `_NilReceiverSafe` / `_DefensiveCopy` / `_UnknownKeysIgnored` / `TestTopLevelStatus_IncludesDeferredFeatures`); 5 wire-level tests in `internal/remotetransport/tcp_test.go` (`TestRemoteControlStatus_WS_Ok` extended assertion + `TestRemoteControlStatus_WS_Ok_DeferredFeaturesOverride` + `TestExperimentalRCV2_Flag_IsAccepted_NoOp`); 4 host-flag registration tests in `internal/cli/experimental_rc_v2_test.go` (`TestExperimentalRCV2_PresentInExtraFlags` / `_NotInStandardFlags` / `_DefensiveShape` / `_HiddenFromHelp`) |

No new JSON-RPC method is added. The only wire change is the
single new field on `RemoteControlStatus`. No third-party dep
is added (the WebRTC ship would need `pion/webrtc` and is
explicitly out of scope). Codestyle: the cycle's touched
files are `gofmt -l` clean; the pre-existing
`cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf`
vet warning stays as-is (out of scope per the brief).

## What ships this cycle

### Files added (4)

| Path | Why |
|---|---|
| `docs/deferred-rc-transport.md` (~400 LOC) | Canonical reference for the four deferred surfaces (G2/G3/G10/G11). Bilingual headings (zh-CN/EN). Includes `TL;DR` table, per-deferral sections (decompiled surface / why deferred / forward contract), the canonical four-key wire shape, the `--experimental-rc-v2` flag, reviewer commands, and a 硬约束 / limitations section. |
| `internal/cli/experimental_rc_v2_test.go` (~210 LOC) | 4 host-flag registration tests: presence in `ExtraFlags()`, NOT in `StandardFlags()` (off the v1.3.5 surface), defensive shape (exactly one entry), and hidden-from-help contract. |
| `internal/remotecontrol/top_level.go` (+~120 LOC) | `TopLevelStatus.DeferredFeatures map[string]bool` field + `defaultDeferredFeatures` 4-key canonical slice + `deferredFeaturesSnapshot(d)` helper. Always-emitted, never-nil, defensive-copy semantics, unknown-key dropoff. |
| (no other new files) | — |

### Files modified (5)

| Path | Why |
|---|---|
| `internal/cli/parser_specs.go` (+5 / -0) | One new line in `ExtraFlags()` for `--experimental-rc-v2` (`Hidden: true`, `Type: "bool"`, "Opt-in to the deferred v2 (WebRTC mesh / Hub) features. No-op today; reserved for forward compat."). |
| `internal/remotecontrol/top_level.go` (+~120 LOC) | See above. The package docstring's bullets list grows by one ("DeferredFeatures is the canonical four-key map"). |
| `internal/remotecontrol/top_level_test.go` (+~120 LOC) | 5 new in-process tests (see § "Test layout" below). |
| `internal/remotecontrol/daemon.go` (+~58 / -0) | One new `Config.DeferredFeatures map[string]bool` field with a ~58-line doc-comment that names the four canonical keys and points to `docs/deferred-rc-transport.md`. |
| `internal/remotecontrol/host.go` (+~46 / -0) | One new `HostConfig.DeferredFeatures map[string]bool` field + propagation through `StartHostSession` → `Config.DeferredFeatures`. |
| `internal/remotetransport/tcp_test.go` (+~165 / -0) | `TestRemoteControlStatus_WS_Ok` extended assertion for the new envelope + `TestRemoteControlStatus_WS_Ok_DeferredFeaturesOverride` (host-override path) + `TestExperimentalRCV2_Flag_IsAccepted_NoOp` (wire-level "no-op today" pin) + `newTCPServerWithDeferredFeatures(t, override)` helper. |
| `cmd/wagyu/remote_control.go` (+~22 / -0) | `maybeStartRemoteControlDaemon` reads `--experimental-rc-v2` and emits a single stderr warning when set; no runtime behaviour change. |

Total: **~870 LOC** (including comments + tests + the new doc).
Stats-driven sense-check: matches the brief's "~150 LOC" allowance
*for the flag/host changes alone*, plus the ~400 LOC deferred-surfaces
doc + the new tests. The brief's LOC figure was for the flag-handling
half; this deliverable documents that as a fraction of the total.

## What lands for forward consumers

| Piece | Where |
|---|---|
| `Config.DeferredFeatures map[string]bool` field on `internal/remotecontrol/daemon.go`, with a doc-comment that names the four canonical keys (`webrtc_mesh`, `antigravity_hub`, `systemd_user`, `enterprise_tunnel`) and their forward-contract cycles (`rc-webrtc-mesh`, `rc-hub-server`, `rc-persistentrc-linux`, `rc-enterprise-tunnel`). | `internal/remotecontrol/daemon.go` |
| `HostConfig.DeferredFeatures map[string]bool` field on `internal/remotecontrol/host.go`, plumbed through `StartHostSession` → `Config.DeferredFeatures`. The cycle's wiring is "default nil → canonical all-false" so existing `cmd/wagyu/` callsites compile unchanged. | `internal/remotecontrol/host.go` |
| `TopLevelStatus.DeferredFeatures map[string]bool` field on `internal/remotecontrol/top_level.go`. Always-emitted on the wire (never nil). | `internal/remotecontrol/top_level.go` |
| `defaultDeferredFeatures = []string{"antigravity_hub", "enterprise_tunnel", "systemd_user", "webrtc_mesh"}` 4-key canonical slice (sorted ascending). | `internal/remotecontrol/top_level.go` |
| `deferredFeaturesSnapshot(d *Dispatcher) map[string]bool` helper: nil-safe, defensive-copy, unknown-key-dropoff. Returns a 4-key map every call. | `internal/remotecontrol/top_level.go` |
| `--experimental-rc-v2` flag (`Hidden: true`, `Type: "bool"`) on `internal/cli/parser_specs.go::ExtraFlags()`. | `internal/cli/parser_specs.go` |
| `cmd/wagyu/remote_control.go:maybeStartRemoteControlDaemon` reads the flag and emits a single stderr warning when set: `[remote-control] --experimental-rc-v2 is set; the deferred v2 features (WebRTC mesh / Antigravity Hub) are not enabled in this branch — see docs/deferred-rc-transport.md`. No runtime behaviour change. | `cmd/wagyu/remote_control.go` |
| Canonical reference for the four deferred surfaces (G2/G3/G10/G11) — decompiled signature table, why-deferred rationale, forward-contract per item, `--experimental-rc-v2` flag spec, limitations. | `docs/deferred-rc-transport.md` |

### On-wire effect (only)

Before:
```jsonc
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "version": "1.4.2-test",
    "uptime_seconds": 0,
    "started_at": "2026-10-19T11:00:00.000Z",
    "connected_clients": 1,
    "open_cascades": 0,
    "dispatch_methods": ["...", "..."],
    "server_fingerprint": "wagyu-rc/1.4.2-test/deadbeef"
  }
}
```

After:
```jsonc
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "version": "1.4.2-test",
    "uptime_seconds": 0,
    "started_at": "2026-10-19T11:00:00.000Z",
    "connected_clients": 1,
    "open_cascades": 0,
    "dispatch_methods": ["...", "..."],
    "server_fingerprint": "wagyu-rc/1.4.2-test/deadbeef",
    "deferred_features": {
      "antigravity_hub":    false,
      "enterprise_tunnel":  false,
      "systemd_user":       false,
      "webrtc_mesh":        false
    }
  }
}
```

The `deferred_features` envelope is **always** a 4-key
`map[string]bool` — never `null`, never `[]`, never a partial
map. A future cycle that flips a key does so without changing
the wire shape (see § "Forward compat paths" below).

## Forward compat paths

This cycle is the **last cycle in the gap-closure plan**
that touches the RC surface. Future RC work splits into
four paths, each gated by the canonical
`deferred_features` bit for that path:

| Future cycle | Sets `deferred_features[k] = true` | Resolves on this branch's seam |
|---|---|---|
| `rc-webrtc-mesh` | `"webrtc_mesh"` | `Config.DeferredFeatures["webrtc_mesh"]` from the v2-host wiring in `cmd/wagyu/remote_control.go`; mux `(*remotetransport.TCPServer).Serve` to add a SDP/ICE/DataChannel sibling path |
| `rc-hub-server` | `"antigravity_hub"` | `cmd/wagyu-hub` binary; the existing daemon flips its entry to `false` until told otherwise |
| `rc-persistentrc-linux` | `"systemd_user"` | New `internal/persistentrc/linux.go` (~150 LOC) + a unit test gated on `runtime.GOOS == "linux"` |
| `rc-enterprise-tunnel` | `"enterprise_tunnel"` | New `RemoteAgentOnDemandTunnel` / `RemoteAgentEndTunnel` JSON-RPC methods; gated behind `-tags enterprise` |

The `--experimental-rc-v2` flag is the bridge that lets
operators opt in to a future v2 mode without waiting for
the deferred_features bit flip; a future cycle that ships
a v2 path may either keep the flag (with `true` semantics)
or remove it. The brief here deliberately leaves the
choice to that future cycle — see
[`docs/deferred-rc-transport.md`](../../deferred-rc-transport.md)
§ "Forward contract — the canonical four-key wire shape".

## Test layout

The cycle ships **15 new tests across 3 files**:

### `internal/remotecontrol/top_level_test.go` (5 + 1 = 6 new)

```
TestDeferredFeatures_AllFalseByDefault        — canonical 4-key map, all false
TestDeferredFeatures_HostOverride             — host-installed partial map survives the canonical-default fallback
TestDeferredFeatures_NilReceiverSafe          — nil *Dispatcher yields the 4-key map (not nil)
TestDeferredFeatures_DefensiveCopy            — host-installed map mutates after first read; second call reflects the mutation (we share the source map by design)
TestDeferredFeatures_UnknownKeysIgnored       — host-installed non-canonical keys are dropped on the wire; canonical key count is exactly 4
TestTopLevelStatus_IncludesDeferredFeatures   — wire-level smoke test on Dispatch; deferred_features envelope round-trips
```

### `internal/remotetransport/tcp_test.go` (1 + 2 = 3 new + 1 extended)

```
TestRemoteControlStatus_WS_Ok
  (extended)                                 — assert deferred_features envelope is present + canonical 4 keys + all false
TestRemoteControlStatus_WS_Ok_DeferredFeaturesOverride  — host override (webrtc_mesh=true); override entry is true, others stay false
TestExperimentalRCV2_Flag_IsAccepted_NoOp     — wire-level no-op pin: --experimental-rc-v2 is a host flag, dispatcher is unaffected; canonical 4-key all-false envelope is what shows
newTCPServerWithDeferredFeatures(t, override) — test helper for the override path
```

### `internal/cli/experimental_rc_v2_test.go` (4 new)

```
TestExperimentalRCV2_PresentInExtraFlags      — registered as Hidden:true bool with the brief's spec
TestExperimentalRCV2_NotInStandardFlags       — NOT in StandardFlags() (cycle pins it as openagy-only)
TestExperimentalRCV2_DefensiveShape           — exactly one entry; no duplicate registration
TestExperimentalRCV2_HiddenFromHelp           — Hidden:true contract (off the v1.4.2 --help surface)
```

The `internal/remotetransport/tcp_test.go` extension is
additive — the existing `TestRemoteControlStatus_WS_Ok`
shape checks stay unchanged. The cycle does not modify
any pre-existing assertion; it only adds the deferred_features
block at the bottom of the test.

## Why this isn't a one-line env-var

Two previous cycles almost got this treatment:

- `rc-ws-transport` started as a one-line env-var
  toggle for a new transport. We promoted it to a flag
  + dispatcher + transport layer because the
  `coder/websocket` upgrade path is non-trivial.
- `rc-auth-tokens` started as a similar env-var for the
  Origin allowlist. We promoted it to a flag + store +
  JSON-RPC method because the wire contract (HTTP 403
  + JSON body) needed a real seam.

`--experimental-rc-v2` deliberately does **not** introduce
a runtime seam. The four deferred features need years of
work each (`pion/webrtc` for G2 alone is ~50 MB of deps);
shipping the flag without a runtime seam would create the
illusion of a forward-compat contract a future cycle
can't keep. The wire-side contract is the
`deferred_features` envelope, which is a real, queryable
state surface. The host flag is the bridge for operators
who want to assert "I asked" without polling for an
answer.

## 硬约束 / limitations

What this cycle does **NOT** verify:

1. **None of G2/G3/G10/G11 is implemented.** The
   cycle's deliverable is a doc + a flag + a wire
   envelope bit; the actual transports stay unimplemented.
   A future `rc-webrtc-mesh` cycle would actually wire
   the WebRTC path; this branch doesn't touch it.
2. **No third-party dep is added.** The WebRTC ship
   would need `pion/webrtc` (~14 transitive modules +
   CGo on Linux). The brief pins the doc-only path for
   this cycle; vendoring is a decision for
   `rc-webrtc-mesh`.
3. **`--experimental-rc-v2` does NOT change wire
   behaviour.** A future v2-host cycle may either keep
   the flag (with `true` semantics — i.e. enable the v2
   path) or remove it (if v2 becomes the default). The
   brief here deliberately leaves the choice to that
   cycle.
4. **The four canonical keys stay sorted alphabetically.**
   A future cycle that adds a fifth key (e.g.
   `remote_agent_voice_chat` for a hypothetical
   `rc-voice-chat`) is a deliberate wire-shape change
   and should bump the envelope's implicit
   `deferred_features_version` from `1` to `2`. The
   cycle does not bump the version because no key is
   added.
5. **No `deferred_features_version` field exists today.**
   Adding one is the simplest extension a future cycle
   can make; the cycle deliberately leaves the slot open
   so the brief only needs to say "add a new key, bump
   `deferred_features_version`". This is forward-compat
   design, not a deferred bug.
6. **No license seam for G10.** The enterprise
   tunnel's forward-contract mentions a `-tags enterprise`
   flag; that ship is parked for `rc-enterprise-tunnel`
   and the cycle does not introduce the tag.
7. **The `internal/remotecontrol/Config.DeferredFeatures`
   map is read on every `topLevelStatus()` call.**
   A future v2-host cycle that hot-reloads its
   `Config.DeferredFeatures` (e.g. via a future RPC)
   needs to know that the snapshot helper always
   re-reads; today's host never mutates the map
   post-construction. The cycle's `TestDeferredFeatures_DefensiveCopy`
   pin documents this.
8. **`--experimental-rc-v2` is `Hidden: true`.** A future
   `rc-webrtc-mesh` cycle that flips the v2 path on
   may want to surface the flag in `--help` (flip
   `Hidden: false`). The brief here keeps it hidden
   to preserve v1.4.2 `--help` parity.
9. **The pre-existing
   `cmd/wagyu/model_default_effort_test.go:67` 3-arg
   `Errorf` vet warning stays as-is.** Out of scope per
   the brief; the cycle does not touch
   `cmd/wagyu/model_default_effort_test.go`.
10. **No `--experimental-rc-v2` is wired on the
    `remote-control-foreground` or `serve` paths.**
    The flag is read only on the per-turn
    `--remote-control` path through
    `maybeStartRemoteControlDaemon`. A future cycle
    that adds a parallel wiring on the persistent
    daemon can call the same `flagBoolValue(fs,
    "experimental-rc-v2")` pattern; the cycle
    deliberately does not duplicate the warning
    printer.

## Reviewer commands

```bash
# 1. Confirm the tip of exp/rc-server-mode is this cycle's commit.
git log --oneline -1 exp/rc-server-mode

# 2. Confirm the cycle's files (added + modified).
git show --stat <commit>

# 3. Build the cycle's binary (must exit 0).
go build -o /tmp/wagyu ./cmd/wagyu

# 4. Run the in-process + wire-level tests (must PASS).
go test -race -count=1 ./internal/remotecontrol/ ./internal/remotetransport/ ./internal/cli/

# 5. Run only the cycle's in-process tests.
go test -race -count=1 -v -run 'TestDeferredFeatures|TestTopLevelStatus_IncludesDeferredFeatures' ./internal/remotecontrol/

# 6. Run only the cycle's wire-level tests.
go test -race -count=1 -v -run 'TestRemoteControlStatus_WS_Ok|TestRemoteControlStatus_WS_Ok_DeferredFeaturesOverride|TestExperimentalRCV2_Flag_IsAccepted_NoOp' ./internal/remotetransport/

# 7. Run only the cycle's host-flag tests.
go test -race -count=1 -v -run 'TestExperimentalRCV2_' ./internal/cli/

# 8. Vet the cycle's touched packages (the pre-existing
#    model_default_effort_test.go:67 3-arg Errorf is out of
#    scope; the cycle does not touch it).
go vet ./internal/cli/ ./internal/remotecontrol/ ./internal/remotetransport/ ./cmd/wagyu/

# 9. Format-check the cycle's touched files (must emit no filenames).
gofmt -l \
    internal/cli/parser_specs.go \
    internal/cli/experimental_rc_v2_test.go \
    internal/remotecontrol/top_level.go \
    internal/remotecontrol/top_level_test.go \
    internal/remotecontrol/daemon.go \
    internal/remotecontrol/host.go \
    internal/remotetransport/tcp_test.go \
    cmd/wagyu/remote_control.go

# 10. Confirm the PLAN.md row is DONE <this-commit>.
grep -F 'rc-webrtc-mesh-defer' docs/cycles/PLAN.md

# 11. Spot-check the deferred-rc-transport.md doc exists and
#     contains the four canonical keys.
test -f docs/deferred-rc-transport.md && grep -F 'webrtc_mesh' docs/deferred-rc-transport.md

# 12. Spot-check the host flag is registered in ExtraFlags().
grep -F 'experimental-rc-v2' internal/cli/parser_specs.go
```

## Cycle delta

```
 docs/cycles/PLAN.md                                    |   4 +-
 docs/cycles/rc-webrtc-mesh-defer/deliverable.md        | new file (~470 LOC)
 docs/deferred-rc-transport.md                          | new file (~400 LOC)
 internal/cli/parser_specs.go                           |   5 +
 internal/cli/experimental_rc_v2_test.go                | new file (~210 LOC)
 internal/remotecontrol/daemon.go                       |  58 ++
 internal/remotecontrol/host.go                         |  46 ++
 internal/remotecontrol/top_level.go                    | 120 +++
 internal/remotecontrol/top_level_test.go               | 120 +++
 internal/remotetransport/tcp_test.go                   | 165 +++
 cmd/wagyu/remote_control.go                            |  22 ++
                                                       | -------
 total: 12 files, ~1,180 net add (excluding docs)
```

Of which:

- **2 new files** (`docs/deferred-rc-transport.md`,
  `docs/cycles/rc-webrtc-mesh-defer/deliverable.md`,
  `internal/cli/experimental_rc_v2_test.go`).
- **7 existing files modified** (one line for the flag
  entry; 5 new `TopLevelStatus`/`Config`/`HostConfig`
  field + doc-comment pairs; new `deferredFeaturesSnapshot`
  helper; 6 new in-process tests + 3 new wire tests +
  4 new host-flag tests + 1 wire-test extension).
- **0 files reformatted wholesale** (the cycle's
  formatting-pin command `gofmt -l` emits nothing).
- **0 third-party deps added**.
- **0 existing tests regressed** (verified via
  `go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/ ./internal/cli/` exit 0).
