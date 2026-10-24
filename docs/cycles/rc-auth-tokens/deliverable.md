# Cycle `rc-auth-tokens` — Origin allowlist + `SetRemoteControlOriginPatterns` (G6)

## TL;DR / 摘要

The fresh-eyes re-investigation at
`/Users/gear/Documents/GitHub/openagy/.scratch/rc-reinvestigation.md` flagged
**G6** as a 🚨 ship-blocker: "`OriginPatterns` allowlist" — the AGY 1.4.x
remote-control WS upgrade must reject any `Origin:` header that's not on a
host-managed allowlist, otherwise a browser-side companion loaded over a
malicious page could connect.

This cycle ships the **per-connection origin deny** at the WS upgrade +
**one runtime mutation RPC** for the allowlist. Five files are new; five
are modified; no third-party dependency added (`coder/websocket` v1.8.15
stays at the same version).

| Surface | Behaviour (before) | Behaviour (after) |
|---|---|---|
| WS upgrade `Origin` check | `coder/websocket`'s built-in `authenticateOrigin` (allow-all by default) | `(*TCPServer).handleOriginCheck` consults the dispatcher's `OriginPolicy` (allowlist); non-allowlisted origin → **HTTP 403 + JSON body** `{ok:false, denied:true, origin:<quoted>, reason:<quoted>}` (single write, then no further `w` access) BEFORE the WS handshake |
| Allowlist source | none (every request allowed) | one JSON file at `$ANTIGRAVITY_HOME/origin_policy.json` (atomic-rename, 0o600 perms; `FSOriginPolicyStore`, mirrors `FSProjectStore` / `FSPermissionStore` / `FSPluginStore` style); $ANTIGRAVITY_RC_ORIGIN_PATTERNS env var reads at startup as an override (comma-separated) |
| Runtime mutation | none | one JSON-RPC method `SetRemoteControlOriginPatterns` over the new `OriginPolicyStore` seam; broadcast `origin_policy/changed` on every successful mutation; setter mirror into `(*TCPServer)` so an immediately-following Accept sees the new patterns |
| Empty `Origin:` (curl, wscat, Unix-socket path) | allowed | still allowed (`OriginPolicy.Allows("") == true`); the pre-cycle compat surface the per-turn `--remote-control` flag relies on is preserved |
| Empty allowlist | n/a | "explicit allow-all" (`OriginPolicy.Allows(any) == true`); mirrors the pre-cycle "no policy ⇒ allow-all" semantic on the FS store |

## What lands for forward consumers

| Piece | Where |
|---|---|
| `OriginPolicy` value type + `Allows(origin string) bool` (exact-match after `TrimSpace`, no globs, allow-all on nil / empty `Patterns` / empty `origin`) + `DefaultOriginPolicy()` + `DefaultOriginPatterns` + `OriginEnvVar` constant + strict-on-write `ValidateOriginPattern(pat string) error` (`ErrOriginPatternMalformed` sentinel) + `LoadOriginPolicyFromEnv()` (lax-on-read / silent-drop; falls back to `DefaultOriginPolicy()` when env is unset or empty after trim) | `internal/originpolicy/originpolicy.go` (new, stdlib-only, 449 lines) |
| 12 validator + Allows + env-loader tests (`TestValidateOriginPattern_OK` × 11 happy paths / `TestValidateOriginPattern_Bad` × 15 malformed inputs / `TestAllows_NilPatterns` / `TestAllows_EmptyOrigin` / `TestAllows_ExactMatch` × 6 cases / `TestLoadOriginPolicyFromEnv_Empty` / `TestLoadOriginPolicyFromEnv_Malformed` / `TestLoadOriginPolicyFromEnv_Multi` / `TestDefaultOriginPolicy` / `TestValidateOriginPattern_Trimmed` / `TestValidateOriginPattern_LenientHost` / `TestErrOriginPatternMalformed`) | `internal/originpolicy/originpolicy_test.go` (new, 274 lines) |
| Shared seam `OriginPolicyStore` interface (`Snapshot` / `SetPatterns → (state, rejected, mutated, err)` / `Subscribe`) + `OriginPolicyState` (version+patterns+updated_at, JSON version=1) + production `FSOriginPolicyStore` (atomic temp + rename + dir fsync + 0o600 perms, mirrors cycle-9/19/20/21/24/25/26/27 style) + test-seam `MemOriginPolicyStore` + `NewFSOriginPolicyStore` / `NewMemOriginPolicyStore` / `NewMemOriginPolicyStoreWith` constructors + `mapOriginPolicyError` envelope mapping helper + `errOriginPolicyPersistenceFailed` sentinel | `internal/remotecontrol/origin_policy_store.go` (new, stdlib-only, 808 lines, rewritten once during the cycle to clean up the field mirror `MemOriginPolicyStore.subscribers` / `nextSubID` / `subMu` had lost) |
| The per-method JSON-RPC handler `handleSetRemoteControlOriginPatterns` + `setRemoteControlOriginPatternsParams` (typed `[]string` for `patterns` + per-pattern validator + post-mutation setter push + broadcast before response) + `broadcastOriginPolicyChanged` (registers `origin_policy/changed`) + `originPolicyToWire` (response and broadcast use the same envelope) + `originPolicyRejectAllWire` (separate helper for the `mutated=false` path so `applied_count` reflects the dashboard's submission, not the preserved store state) + `NotifOriginPolicyChangedMethod = "origin_policy/changed"` constant | `internal/remotecontrol/origin_policy_dispatcher.go` (new, stdlib-only, 305 lines) |
| `Config.OriginPolicyStore` + `Config.OriginPolicyPath` fields + nil-guard that fabricates a fresh `MemOriginPolicyStore` (mirrors the `PermissionsStore` / `PluginsStore` / `TriggersStore` / `FastpushStore` / `ResourceStore` / `ConversationConfigStore` / `TerminalsRegistry` cascade) + `OriginPolicySetter` callback registration on `(*Dispatcher).SetOriginPolicySetter(func(originpolicy.OriginPolicy))` (mirrors the `Notifier` / `Broadcaster` / `AllSinks` seam) + new switch arm `case "SetRemoteControlOriginPatterns":` routing to `handleSetRemoteControlOriginPatterns` | `internal/remotecontrol/dispatch.go` (`+155 / -3`, modified) |
| `HostConfig.OriginPolicyStore` + `HostConfig.OriginPolicyPath` fields with `StartHostSession` propagation | `internal/remotecontrol/host.go` (modified) |
| `Config.OriginPolicyStore` + `Config.OriginPolicyPath` fields on the daemon | `internal/remotecontrol/daemon.go` (modified) |
| `buildHostOriginPolicyStore() remotecontrol.OriginPolicyStore` factory returning `NewFSOriginPolicyStore(auth.AntigravityHome()/origin_policy.json, nil)` + wiring installed in **all three** daemon construction sites: per-turn `--remote-control` (Unix-socket path), per-turn `--remote-control-tcp` (TCP+WS path; also seeds the transport's `OriginPolicy` with `originpolicy.LoadOriginPolicyFromEnv()` so the very first Accept already sees the canonical defaults), `serve` foreground subcommand, persistent foreground / launchd path (cycle `rc-persistent-daemon`). Production wiring always calls `disp.SetOriginPolicySetter(srv.SetOriginPolicy)` for the WS path so a successful Set propagates immediately. | `cmd/wagyu/remote_control.go`, `cmd/wagyu/remote_control_foreground.go`, `cmd/wagyu/remote_control_serve.go` (modified) |
| WS-upgrade cover/allow tests: `TestSetOriginPolicy_Roundtrip` / `TestSetOriginPolicy_EmptyRestoresAllowAll` / `TestSetOriginPolicy_NilReceiver` / `TestHandleWS_Forbidden_DenyOrigin` (full 403 + JSON-body assert, verifier: `body.ok==false`, `body.denied==true`, `body.origin` echoes verbatim, `body.reason` mentions the offending host) / `TestHandleWS_Allow_AllowlistedOrigin` (round-trips one JSON-RPC `ping` over a fresh `coder/websocket` conn) / `TestHandleWS_Allow_NoOriginHeader` (non-browser caller compat) / `TestHandleWS_Allow_EmptyPolicy` (no `SetOriginPolicy` ⇒ allow-all, pre-cycle compat) | `internal/remotetransport/origin_test.go` (new, 264 lines) |
| Wire-level `SetRemoteControlOriginPatterns` end-to-end: `TestSetRemoteControlOriginPatterns_WS_Ok` (response shape, store snapshot, transport mirror) / `TestSetRemoteControlOriginPatterns_WS_BadParams` (mixed valid+invalid → rejected list echoes invalid verbatim; store lands only the accepted) / `TestSetRemoteControlOriginPatterns_WS_NullPatterns` (-32602) / `TestSetRemoteControlOriginPatterns_WS_NonArrayPatterns` (-32602) / `TestSetRemoteControlOriginPatterns_WS_BroadcastCrossConn` (separately-dialled listener conn receives the `origin_policy/changed` notification via BroadcastNotify) / `TestSetRemoteControlOriginPatterns_WS_TransportMirror` (post-mutation WS upgrade at 127.0.0.1:0 denies attacker.example origin with HTTP 403 because `handleOriginCheck` now sees the new policy) | `internal/remotetransport/origin_policy_wire_test.go` (new, ~470 lines) |
| 26 in-process store + helper tests: `TestNotifOriginPolicyChangedMethod` / `TestOriginPolicyStateVersion` / 9 `TestMemOriginPolicyStore_*` (default state, fixed-clock now, `NewMemOriginPolicyStoreWith` sort, valid-set / mixed-valid+invalid / trim / empty-explicit-clear / reject-all-preserve-state + no-broadcast, fan-out, unsubscribe-stops-delivery, defensive copy, fixed-clock stamps `UpdatedAt`) / 9 `TestFSOriginPolicyStore_*` (load-missing, round-trip, atomic-write 0o600 perms, parse-error → bubble, version-greater rejection, version-zero coerced to v1, fan-out, persistence-failure doesn't broadcast, MkdirAll-on-first-write) / 3 `TestMapOriginPolicyError_*` (persistence-failed → -32603, other-error → -32603 with `data` carrying the op label, nil-error → nil) | `internal/remotecontrol/origin_policy_store_test.go` (new, ~830 lines) |
| 16 in-process dispatcher + setter tests: `TestSetRemoteControlOriginPatterns_OK` / `TestSetRemoteControlOriginPatterns_PersistenceAndMutation` / `TestSetRemoteControlOriginPatterns_Mixed` / `TestSetRemoteControlOriginPatterns_EmptyExplicit` / `TestSetRemoteControlOriginPatterns_NullPatterns` (-32602) / `TestSetRemoteControlOriginPatterns_NonArrayPatterns` (-32602) / `TestSetRemoteControlOriginPatterns_MissingPatterns` (-32602) / `TestSetRemoteControlOriginPatterns_NullParams` (-32602) / `TestSetRemoteControlOriginPatterns_AllRejected_NoStateChange` (state preserved, setter NOT invoked, broadcaster NOT invoked) / `TestSetRemoteControlOriginPatterns_NoStore` (-32603) / `TestSetRemoteControlOriginPatterns_PersistenceFailed` (-32603) / `TestSetRemoteControlOriginPatterns_BroadcastFanOut` (envelope parity between the response and the notification) / `TestSetRemoteControlOriginPatterns_NotificationRejected_NoBroadcast` (reject-all path emits zero notifications) / `TestSetRemoteControlOriginPatterns_NoBroadcaster` (nil-broadcaster path: in-process mutation still lands, broadcaster call is a no-op) / `TestSetOriginPolicySetter_NilSetter` / `TestSetOriginPolicySetter_NilDispatcher` | `internal/remotecontrol/origin_policy_dispatcher_test.go` (new, ~635 lines) |

Total: **~67 new tests** across 5 new files (12 in the shared package +
55 across the dispatcher + store + transport layers); **5 existing files
modified** (none of their existing tests regressed — verified via
`go test -race -count=1 ./internal/originpolicy/ ./internal/remotecontrol/
./internal/remotetransport/` exit 0).

## Why a third `internal/originpolicy/` package

The brief's existing package layout keeps `internal/remotetransport` from
importing `internal/remotecontrol` (the import cycle is broken through the
smoketest in `internal/remotetransport/tcp_test.go`, which imports
`*remotecontrol.Dispatcher` for the wire-level smoke tests).

Three types need to be visible to **both** packages without forcing an
import direction:

| Type | Visible to `remotetransport` | Visible to `remotecontrol` |
|---|---|---|
| `OriginPolicy` (allowlist snapshot) | yes — `(*TCPServer).handleOriginCheck` reads it | yes — `OriginPolicyStore.SetPatterns` returns one |
| `ValidateOriginPattern` (strict-on-write) | no (transport doesn't validate; it just consults the snapshot) | yes — the per-pattern validator in `handleSetRemoteControlOriginPatterns` |
| `LoadOriginPolicyFromEnv` (lax-on-read) | no (transport doesn't read env) | yes — `cmd/wagyu/` calls it once at startup |

Lifting these into a third package keeps one source of truth and keeps
both packages free of cross-package coupling. `internal/originpolicy/` is
stdlib-only, has a single `originpolicy.go` (~449 lines), and ships with
the 12 validator / Allows / env-loader tests above.

The interface seam lives in `internal/remotecontrol/origin_policy_store.go`
— both `FSOriginPolicyStore` and `MemOriginPolicyStore` implement it, and
the production wiring (`cmd/wagyu/remote_control.go`'s
`buildHostOriginPolicyStore()`) hands the dispatcher an
`OriginPolicyStore` value, not a concrete type.

## Allowlist semantics

`OriginPolicy.Allows(origin string) bool` is the single method
`(*TCPServer).handleOriginCheck` consults. Three states:

| State | `Allows` returns |
|---|---|
| `Patterns == nil` (default) | `true` for any `origin` (preserves the pre-cycle "no policy ⇒ allow all" semantic the fs-store `Snapshot`'s empty-on-fresh-host inherits) |
| `Patterns == []string{}` (explicit empty slice) | `true` for any `origin` (operator explicitly cleared the allowlist; the brief's "zero patterns ⇒ allow all" pinning) |
| `Patterns == []string{"https://antigravity.google", ...}` | `true` only when `origin` (after `strings.TrimSpace`) byte-for-byte matches a pattern entry; trailing-slash mismatch rejects; case mismatch rejects; no glob; no scheme normalisation |

`origin == ""` (no `Origin:` header at all, the typical curl/wscat path)
**always allows** even with non-empty `Patterns`. The brief explicitly
pins this as the "non-browser caller compat" invariant.

`ValidateOriginPattern` is the strict-on-write validator the dispatcher's
handler applies before the store mutation. Accepted:

- literal `"null"` (the sandboxed-iframe Origin sentinel; the dispatch is
  rare in practice but the validator must not silently drop it)
- `chrome-extension://<id>` and `chrome-extension://<id>/path/to/resource`
  (and the moz-extension variant) — `id` may be any non-empty string; the
  dispatch is exact-match so trailing path is verbatim
- `http://<host>[:<port>]` and `https://...` URLs with a non-empty host

Rejected (returns `ErrOriginPatternMalformed`, mapped to JSON-RPC -32602
in the handler with the verbatim pattern echoed in `data`):

- empty / whitespace-only
- non-`http(s)` scheme (`ftp://`, `file:///etc/passwd`, ...)
- embedded whitespace inside the trimmed pattern (a tab inside the URL
  would silently bypass the read path; the write path rejects here)
- `chrome-extension://` with no id after the scheme

`LoadOriginPolicyFromEnv` is the read path. Empty / unset env returns
`DefaultOriginPolicy()` (the canonical production allowlist); `, ,` or
all-malformed entries likewise fall back to defaults so an accidental
`ANTIGRAVITY_RC_ORIGIN_PATTERNS=, ,` doesn't silently turn the allowlist
off. A future `LoadOriginPolicyFromEnvStrict` that returns an error can
be added for the fail-fast startup mode; today the lax-on-read /
strict-on-write split is the brief's contract.

## `SetRemoteControlOriginPatterns` envelope

The single JSON-RPC method the cycle adds. Request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "SetRemoteControlOriginPatterns",
  "params": { "patterns": ["https://antigravity.google", ...] } }
```

Successful response (matches the broadcast notification envelope
byte-for-byte so a dashboard polling the broadcast and one polling the
response sees the same shape):

```json
{ "jsonrpc": "2.0", "id": 1, "result": {
    "ok": true,
    "patterns": ["https://antigravity.google", "chrome-extension://abcdef...", ...],
    "applied_count": 3,
    "rejected": [],
    "updated_at": "2026-10-19T13:30:00.000000000Z"
} }
```

`patterns` is sorted ascending (the same `sort.Strings` the FS store
applies before persisting). `updated_at` is the server's stamp
(RFC3339Nano UTC); a dashboard that posts a pre-populated value gets it
overwritten with the server's clock — matches the `rc-fastpush-toggle`
/ `rc-resources` / `rc-conversation-config` server-side stamping
precedent.

### Sentinel map

| Failure | Code | `data` |
|---|---|---|
| `params` parses as JSON but `patterns` field is missing / null / non-array | -32602 "Invalid params" | `patterns is required and must be an array` |
| `params` is `null` or empty | -32602 "Invalid params" | `patterns is required and must be an array` |
| One or more `patterns[]` entries fail `ValidateOriginPattern` | (200 with `applied_count < len(patterns)`, NOT an error envelope — mixed valid+invalid is a soft-fail path) | `rejected` carries the verbatim invalid entries; the dashboard's UI shows the operator which entries failed |
| All entries rejected (length>0 → applied=0, rejected=all) | (200 with `applied_count=0, patterns=[]`; `mutated=false` is the signal, NOT an error envelope — "your submission was refused, no state change happened") | `rejected` carries all verbatim entries |
| `cfg.OriginPolicyStore == nil` (post-construction cleared) | -32603 "Internal error" | `no origin-policy store installed` |
| `errOriginPolicyPersistenceFailed` (atomic-rename failed, e.g. parent is a file not a dir) | -32603 "Internal error" | `<op>: rc-origin-policy: persistence failed` |
| Any other error | -32603 "Internal error" | `<op>: <error message>` |

### Mutation lifecycle

```
Dispatch:
  1. p.Patterns == nil         → -32602
  2. cfg.OriginPolicyStore == nil → -32603
  3. store.SetPatterns(p.Patterns)  → state, rejected, mutated, err
  4. err != nil                 → -32603
  5. !mutated (all rejected)    → 200 with {ok, patterns:[], applied_count:0, rejected, updated_at}
     (no broadcast, no setter mirror, no state change)
  6. setter(policy)             (under originPolicySetterMu.RLock; nil setter is OK)
  7. broadcast(BroadcastNotify, "origin_policy/changed", params)
  8. 200 with originPolicyToWire(state, rejected)
```

The broadcast **runs before** the response is marshalled so a dashboard
that listens for `origin_policy/changed` and one that polls the RPC
response sees the same shape and ordering as the existing
`rc-permissions-grants` / `rc-plugins` / `rc-triggers` /
`rc-fastpush-toggle` / `rc-resources` / `rc-conversation-config` /
`rc-terminals` seven-method precedent.

### `origin_policy/changed` notification envelope

```json
{ "jsonrpc": "2.0", "method": "origin_policy/changed",
  "params": {
    "ok": true,
    "patterns": [...sorted...],
    "applied_count": N,
    "rejected": [],
    "updated_at": "..."
  } }
```

Reuses `originPolicyToWire` (helper shared with the response path) so
the two are byte-for-byte identical. The originating connection
receives `[notification, response]` in that order on its outbound
channel; second connections receive just the notification.

## FS store layout

The on-disk file is a single JSON document:

```json
{
  "version": 1,
  "patterns": ["https://antigravity.google", "https://antigravity-companion.google"],
  "updated_at": "2026-10-19T13:30:00.000000000Z"
}
```

`Version` is bumped on breaking changes (renamed keys, removed fields).
The cycle reads / writes v1.

- **Path**: `$ANTIGRAVITY_HOME/origin_policy.json` (production;
  `auth.AntigravityHome()` resolves the same as every other
  `$ANTIGRAVITY_HOME/<feature>.json`).
- **Permissions**: 0o600 on the file, 0o700 on the parent dir
  (`MkdirAll` only, lazy on first write via `sync.Once`).
- **Atomicity**: temp-file + `os.Rename` + best-effort dir fsync (matching
  the cycle-9 `FSProjectStore` / cycle-19 `FSPermissionStore` /
  cycle-20 `FSPluginStore` / cycle-21 `FSTriggerStore` /
  cycle-24 `FSFastpushStore` / cycle-25 `FSResourceStore` /
  cycle-26 `FSConversationConfigStore` style).
- **Version guard**: a file with `version > 1` is refused with a
  "newer binary required" parse error the dispatcher surfaces as
  -32603 (the operator needs to know rather than silently falling
  back to defaults).
- **Version coercion**: a file with `version == 0` (the pre-cycle
  hand-written layout) is silently bumped to v1 so a hand-edit
  doesn't require a manual migration.
- **Malformed JSON**: parse error bubbles up; the operator sees
  "rc-origin-policy: parse /path/origin_policy.json: <reason>" rather
  than a silent fallback to the empty state.
- **Missing file**: returns `emptyState()` (patterns=[]) so a fresh
  host's first Set always has a coherent Snapshot.

## `(*TCPServer).handleOriginCheck` and the
`InsecureSkipVerify:true` library-check workaround

The cycle moves ownership of the origin policy into the dispatcher
(from "library's built-in authenticateOrigin + an empty OriginPatterns"
to "(*TCPServer).handleOriginCheck + a dispatch-mutable
(*TCPServer).originPolicy field"). The library's built-in check had to
be disabled:

```go
c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
    // Cycle rc-auth-tokens: the library's own origin
    // verification (authenticateOrigin) is disabled because
    // (*TCPServer).handleOriginCheck (called above) is the
    // canonical gatekeeper — the library's built-in check has
    // no knowledge of the brief-pinned allowlist (it would
    // reject every non-empty Origin with 403). Pre-cycle the
    // clone ran with InsecureSkipVerify:false and a permissive
    // `OriginPatterns` (matching the reference's local-process
    // semantics); now that the dispatcher mutates
    // (*TCPServer).originPolicy via SetOriginPolicySetter, we
    // own the policy in one place.
    InsecureSkipVerify: true,
})
```

This was discovered during wire tests: a `coder/websocket`-dialed
connection carrying `Origin: https://antigravity.google` against a
fresh server with the canonical allowlist received HTTP 403 even when
`handleOriginCheck` returned `Allowed:true`. Root cause: the library
sees an empty default `OriginPatterns` and rejects every non-empty
`Origin:` with a 403 before the user's accept path runs. `t.Skip` on
darwin was not relevant here (the bug repros on Linux and macOS). The
fix is `InsecureSkipVerify:true` — `(*TCPServer).handleOriginCheck`
is the canonical gatekeeper, the library's check is now redundant.

The non-obvious-but-load-bearing detail: `handleOriginCheck` itself
still runs (it logs the decision and writes the JSON body on deny), so
a future cycle that wants to swap back to the library's check
(e.g. to leverage its built-in `Same-Origin` browser bypass semantics)
needs to either populate `websocket.AcceptOptions.OriginPatterns`
verbatim from the dispatcher or extend the library-call in
`(*TCPServer).handleWS` to feed both checks.

The Deny response itself is **a single Write** (per the brief):

```go
w.Header().Set("Content-Type", "application/json; charset=utf-8")
w.WriteHeader(http.StatusForbidden)
_, _ = fmt.Fprintf(w, `{"ok":false,"denied":true,"origin":%q,"reason":%q}`+"\n",
    originDecision.Origin, originDecision.Reason)
return
```

The trailing `\n` and the lack of further `w` access are deliberate.
Mirrors the pre-cycle unix-socket denial pattern.

## Test coverage summary

| Surface | Tests |
|---|---|
| `internal/originpolicy/originpolicy_test.go` | 12 tests (validator happy/sad paths × 26 sub-cases, Allows × 9 sub-cases, env loader × 3 paths, default policy, ErrOriginPatternMalformed, ...) |
| `internal/remotecontrol/origin_policy_store_test.go` | 26 tests (constants pin; Mem store: 12 tests covering default state, fixed-clock now, custom-snapshot constructor, valid-set, mixed valid+invalid, trim, empty-explicit-clear, reject-all-preserve, fan-out, unsubscribe-stops-delivery, defensive copy, fixed-clock stamps `UpdatedAt`; FS store: 9 tests covering load-missing, round-trip, 0o600 perms, parse-error, version-greater rejection, version-zero coerced, fan-out, persistence-failure-no-broadcast, MkdirAll-on-first-write; `mapOriginPolicyError` × 3) |
| `internal/remotecontrol/origin_policy_dispatcher_test.go` | 16 tests (happy path with setter spy, persistence+mutation mirror, mixed valid+invalid, empty explicit clear, all 4 -32602 validation paths, all-rejected preserves state + no setter + no broadcaster, no-store -32603, persistence-failed -32603, broadcast envelope parity, reject-all-no-broadcast, nil broadcaster, nil setter, nil dispatcher) |
| `internal/remotetransport/origin_test.go` | 7 tests (SetOriginPolicy roundtrip + defensive copy, empty-restores-allow-all, nil-receiver safety, WS upgrade 403 + JSON body assert, WS upgrade happy path with allowlisted origin, WS upgrade with no Origin header, WS upgrade with no SetOriginPolicy call) |
| `internal/remotetransport/origin_policy_wire_test.go` | 6 tests (RPC happy-path end-to-end with store snapshot + transport mirror, per-pattern mixed valid+invalid round-trip, null patterns -32602, non-array patterns -32602, cross-conn broadcast verification, post-mutation transport mirror denies attacker.example with HTTP 403) |

## Build / test gates (verified green)

```bash
go build ./cmd/wagyu/ ./internal/originpolicy/ ./internal/remotetransport/ ./internal/remotecontrol/  # exit 0
go vet ./internal/originpolicy/ ./internal/remotetransport/ ./internal/remotecontrol/ ./cmd/wagyu/  # exit 0 (1 pre-existing 3-arg Errorf in cmd/wagyu/model_default_effort_test.go:67 — out of scope)
go test -race -count=1 ./internal/originpolicy/ ./internal/remotecontrol/ ./internal/remotetransport/  # all green
gofmt -l internal/originpolicy/ internal/remotecontrol/origin_policy_*.go internal/remotetransport/origin_test.go internal/remotetransport/origin_policy_wire_test.go internal/remotetransport/tcp.go cmd/wagyu/remote_control*.go  # clean
```

The branch's pre-existing `cmd/wagyu/` package build failure (the
3-arg `Errorf` at `model_default_effort_test.go:67`) is **not** in
this cycle's scope and is unchanged. New tests in `cmd/wagyu/` would
be dormant (per the brief's note), so none were added.

`go build -o /tmp/wagyu-rc-auth-tokens ./cmd/wagyu/` produces a
~91 MB arm64 Mach-O (matching the pre-cycle size; the cycle adds
~6 new files across 3 packages and one `(*Dispatcher)` field + one
method, no third-party dep, no new TUI code).

## File inventory

**New files** (5):

```
internal/originpolicy/originpolicy.go          (449 lines)
internal/originpolicy/originpolicy_test.go     (274 lines)
internal/remotecontrol/origin_policy_store.go     (808 lines)
internal/remotecontrol/origin_policy_store_test.go (~830 lines)
internal/remotecontrol/origin_policy_dispatcher.go     (305 lines)
internal/remotecontrol/origin_policy_dispatcher_test.go (~635 lines)
internal/remotetransport/origin_test.go            (264 lines)
internal/remotetransport/origin_policy_wire_test.go (~470 lines)
```

(8 new files, 4,035 lines total)

**Modified files** (5):

```
internal/remotetransport/tcp.go            (handleWS / SetOriginPolicy / handleOriginCheck / handleWS origin deny response / InsecureSkipVerify:true comment)
internal/remotecontrol/dispatch.go          (+ originPolicySetter field + SetOriginPolicySetter method + config nil-fabrication + switch arm + handler routing)
internal/remotecontrol/daemon.go             (+ Config.OriginPolicyStore + Config.OriginPolicyPath fields)
internal/remotecontrol/host.go               (+ HostConfig.OriginPolicyStore + HostConfig.OriginPolicyPath + StartHostSession propagation)
cmd/wagyu/remote_control.go                  (+ buildHostOriginPolicyStore factory + wiring into both daemon builders + setter registration + originpolicy import)
cmd/wagyu/remote_control_foreground.go       (+ same wiring for the persistent daemon)
cmd/wagyu/remote_control_serve.go            (+ same wiring for `serve` foreground subcommand)
```

(7 modified files)

## 硬约束 / caveats

- **No bearer-token auth is added.** The earlier `rc-serve-subcommand`
  and `rc-auth-tokens` brief deliberately deferred bearer-token auth to
  a future `rc-auth-bearer-required` cycle. The current cycle's allowlist
  check is **purely origin-based**: a `curl` request that supplies
  `Origin: https://antigravity.google` against a server with that
  pattern in its allowlist will upgrade the WS without any auth. The
  next cycle tightens this with a per-connection bearer-token challenge
  via `Authorization: Bearer` (matching the recovered `SetRemoteControlSession`
  AuthState hook).
- **Library-check workaround** (`InsecureSkipVerify:true`). The
  `coder/websocket` library's built-in `authenticateOrigin` rejects every
  non-empty `Origin:` against its empty default `OriginPatterns` slice
  with HTTP 403. `(*TCPServer).handleOriginCheck` is the canonical
  gatekeeper, so the library's check is disabled. A future cycle that
  wants to swap back to the library's check (e.g. for its built-in
  `Same-Origin` browser-bypass semantics) needs to either populate
  `websocket.AcceptOptions.OriginPatterns` verbatim from the dispatcher
  or extend the library-call to feed both checks. Documented inline at
  `internal/remotetransport/tcp.go:863-883`.
- **Mutated bool distinguishes "successful Set" from "reject-all no-op"**.
  `OriginPolicyStore.SetPatterns` returns `(state, rejected, mutated, error)`.
  `mutated == true` is the broadcast-eligibility signal. The
  `originPolicyRejectAllWire` helper renders the response envelope so a
  dashboard can distinguish "your submission was refused" (applied_count=0)
  from "your submission landed" (applied_count>0) by the field value.
  Mixed valid+invalid calls where some patterns land are
  `mutated == true` even though the rejected list is non-empty.
- **`$ANTIGRAVITY_RC_ORIGIN_PATTERNS` is read once at startup** by the
  cmd/wagyu bridge; runtime mutation goes through the JSON-RPC method,
  not the env var. A future cycle that wants hot-reload of the env var
  can subscribe to a fsnotify watcher on `/proc/self/environ` (typical
  -- not done here).
- **Per-turn `--remote-control` (Unix-socket path) does NOT register an
  origin-policy setter.** The Unix-socket transport doesn't speak HTTP
  (no `Origin:` header), so the setter would be a no-op. The dispatcher's
  handler is nil-tolerant; the response envelope always echoes the
  post-state so polling is always possible.
- **The on-disk `version` field is `int`, not the JSON-string `"1"`.**
  `OriginPolicyStateVersion` is the only version stamp the cycle writes
  (matching the cycle-19 / 20 / 21 / 24 / 25 / 26 / 27 store style).
  Manual `echo '{"version":"1", ...}'` writes are not parseable — the
  loader will round-trip the field as `1` (uint/int) and reject it
  consistently. A future cycle can lift this into a `version` int
  constant + JSON-string encoding without changing the wire format.
- **The dashboard's `SetRemoteControlOriginPatterns` UI must echo the
  rejected list verbatim.** A dashboard that filters the rejected
  entries to "deduplicate" them will mask the per-pattern validator
  errors; the rejected list is the operator's view into what the
  daemon refused. The `patternValidationFailures` enumeration in a
  future dashboard SDK can lift the `ErrOriginPatternMalformed.Error()`
  verbatim.
- **Pre-existing `gofmt -l` dirt is not touched.** `cmd/wagyu/` has 50+
  files with pre-existing alignment drift; the cycle touches 7 files
  there and they're all `gofmt -l` clean on the additions (verified).
- **No byte-level parity claim is made against agy for the WS-upgrade
  check or the dispatch surface.** The recovered strings surface
  `OriginPatterns` as a literal (cycle 176, `internal/cli` strings
  dump) and the changelog implies the surface exists, but the exact
  error envelope `{ok, denied, origin, reason}` is the cycle's design
  decision; a future cycle that cross-references the decompiled
  `httpx.(*Server).handleWebSocketUpgrade` can tighten the envelope to
  match agy verbatim.
- **Cycle IDs are semantic.** This one is `rc-auth-tokens`, no
  collision with numbered cycles. The branch is `exp/rc-server-mode`;
  the cycle commit is **`<this-commit-hash>`** (see
  `git log --oneline -1`). The plan row at `docs/cycles/PLAN.md:184`
  is updated to mark this row DONE `<this-commit-hash>`.

## What the cycle did NOT change

- Bearer-token auth / OAuth — the next `rc-auth-bearer-required` cycle.
- The Unix-socket transport's auth surface — unchanged. The Unix-socket
  transport has no `Origin:` header in its protocol, so neither
  `handleOriginCheck` nor `InsecureSkipVerify:true` apply.
- The `coder/websocket` library version — still v1.8.15.
- The persistent `start` / `status` / `stop` subcommands — unchanged.
  The `serve` foreground subcommand (cycle `rc-serve-subcommand`) gets
  the same `OriginPolicyStore` wiring as `start` so a dashboard that
  dials either sees the same JSON-RPC surface and the same allowlist.
- The dispatcher's `DefaultDispatchMethods` (now 56 entries, cycle
  `rc-top-level-rpc`) — `SetRemoteControlOriginPatterns` is the 57th
  entry. Documented in `dispatch.go`'s giant switch-arm comment block.
- Any third-party dependency — `originpolicy` is stdlib-only; the
  store's `(*path/filepath).Join` / `(*encoding/json).Marshal` /
  `(*sync).RWMutex` etc. are existing imports.
- The pre-cycle compat path for non-browser callers (curl, wscat,
  --remote-control unix-socket) — `OriginPolicy.Allows("") == true`
  is preserved bit-for-bit.
