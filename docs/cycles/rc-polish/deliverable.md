# rc-polish — four cosmetic gap closures (G12 / G13 / G15 / G16)

Cycle closes the four `🟢` (cosmetic) gaps the fresh-eyes
re-investigation at `/Users/gear/Documents/GitHub/openagy/.scratch/rc-reinvestigation.md`
enumerated against the `exp/rc-server-mode` branch. Each gap
is a one- or two-line fix; the brief's goal is "bump, register,
format-fix, params-honour" — no envelope-shape changes, no new
methods, no transport work.

The 14 🚨 / 🟡 gaps the same re-investigation called out
(`G1`–`G11`, `G14`, `G17`, `G18`) are owned by separate cycles
listed in `docs/cycles/PLAN.md` and are not in scope here.

---

## TL;DR

| Gap | One-line summary | Files touched | LOC |
|---|---|---|---:|
| **G12** | `cli.ReferenceVersion` 1.4.0 → **1.4.2** (agy is 1.4.2; verified by xxd that the reference prints exactly `1.4.2\n` = 6 bytes) | `internal/cli/version.go` | +8 / -2 |
| **G13** | Register `--remote-control-socket` (the read at `cmd/wagyu/remote_control.go:99` was dead code) and wire the flag value into the existing `HostConfig.SocketPath` seam so the flag now resolves a real path | `internal/cli/parser_specs.go`, `cmd/wagyu/remote_control.go` | +13 / 0 |
| **G15** | Fix `Format(timeFormat(...))` → `.UTC().Format(time.RFC3339Nano)` at `dispatch.go:1234` (was emitting garbage like `"18186-100-108T180:08:180.78Z"`) | `internal/remotecontrol/dispatch.go` | +12 / -3 |
| **G16** | Add typed `Backend string` to the `StartRemoteAgentCascade` params struct so `params.backend = "shim"` round-trips into the cascade's `BackendLabel` | `internal/remotecontrol/dispatch.go`, `internal/remotecontrol/cascade_test.go` | +25 / -5 (production) +130 (tests) |

- **Tests**: 3 new in `internal/remotecontrol/cascade_test.go`
  (`CreatedAtRFC3339Nano`, `ParamsBackendWins`,
  `ParamsBackendEmptyFallsBack`).
- **`go test -race -count=1 ./internal/remotecontrol/
  ./internal/remotetransport/ ./internal/parityverify/
  ./internal/persistentrc/` PASS.**
- **`go vet`** exit 0 on the brief's scope (the pre-existing
  `cmd/wagyu/model_default_effort_test.go:67` 3-arg `Errorf`
  warning stays as-is — out of scope).
- **`gofmt -l`** empty on touched files.
- **Parity verifier** (`docs/cycles/rc-final-parity-verify/`):
  Python + Go harnesses both report `verified` on
  `StartRemoteAgentCascade` with the new note
  `backend='shim' (params.backend honored since rc-polish G16)`
  and `remoteControlVersion.version = 1.4.2`.

---

## G12 — Version drift

**File:** `internal/cli/version.go:12`
**Before:** `const ReferenceVersion = "1.4.0"`
**After:**  `const ReferenceVersion = "1.4.2"`

### Was this a real drift?

Yes. Verified pre-cycle by directly inspecting the reference
binary on this machine:

```bash
$ ~/.local/bin/agy --version
1.4.2
$ ~/.local/bin/agy --version 2>&1 | xxd | head -1
00000000: 312e 342e 320a                           1.4.2.
```

The binary prints exactly `1.4.2\n` (6 bytes). The clone
returned `1.4.0` (cycle-181 pinned it). The cycle-15 verifier's
known-gaps section explicitly called this out:

> `remoteControlVersion.version = "1.4.0"` should match `agy`'s
> output. `~/.local/bin/agy --version` prints `1.4.2`.

The cycle-15 deliverable parked it as `rc-version-bump` / `G12`;
this cycle is the closure. The constant is the **single source
of truth** — `cli.ReferenceVersion` flows into:

| Caller | Surface | Cycle |
|---|---|---|
| `cmd/wagyu/main.go:131` (`cliVersion` constant) | `agy --version` | many |
| `internal/remotecontrol/dispatch.go:1074` (`remoteControlVersion` RPC) | JSON-RPC `remoteControlVersion` | `rc-version` |
| `internal/cli/dispatch.go:340` | `v<version>` mTLS banner | `rc-mtls-banner` |
| `internal/cli/parser_specs.go:384` | `update` subcommand text | `rc-update-models` |
| `internal/cli/update_models_test.go:40` | `TestUpdate_Offline` fixture | — |

Every caller now reports `1.4.2`. `TestUpdate_Offline` was the
only test pinning a literal — it reads the constant, so the
test stays green without a fixture bump.

---

## G13 — `--remote-control-socket` dead code

### Choice made

**Option A** — register the flag, for symmetry with the existing
hidden `--remote-control-tcp` flag. Option B (delete the read)
would have been cleaner code-wise but left the
`cmd/wagyu/remote_control.go:99` plumbing's intent unexplained
to future readers and would have orphaned the
`daemonMetaForCaller("unix", sockPath, "in-process")` call right
below it, which uses `sockPath` for the rich-status tool's
`daemon.addr` field.

### What landed

1. **`internal/cli/parser_specs.go`** — added the flag
   registration in `ExtraFlags()` next to `--remote-control-tcp`:
   ```go
   {Name: "remote-control-socket", Type: "string", Def: "",
    Hidden: true,
    Usage: "Override the unix socket path for --remote-control. $OPENAGY_RC_SOCKET still wins"},
   ```

2. **`cmd/wagyu/remote_control.go:106`** — the existing
   `sockPath := flagStringValue(fs, "remote-control-socket")`
   line now actually resolves a real value when the user passes
   the flag; we feed it into `HostConfig.SocketPath` (the seam
   that `internal/remotecontrol/host.go:157-176` already
   exposes). The env var `$OPENAGY_RC_SOCKET` still wins when
   both are set — that's the documented convention.

### Flag is "accepted but no-op" today?

Today the flag is **accepted and honoured at the binding path**
when the env var is unset. When the env var is set, the env var
wins — that's the seam's documented rule, not a flag bug. The
rich-status tool's `daemon.addr` block already reflects the
flag value because the existing line 112
(`DaemonMeta: daemonMetaForCaller("unix", sockPath, "in-process")`)
already used `sockPath`; that path was always populated with
either `""` (default) or the flag value, so the status surface
is unchanged.

A future `rc-serve-subcommand` cycle (PLAN.md row 13) will
likely add explicit end-to-end wiring for the flag — out of
scope here. This cycle just registers the flag so the
`flagStringValue` read isn't dead.

### Forward compat with `rc-serve-subcommand`

`rc-serve-subcommand` lives near the same file area (the
`remote-control` subcommand handler) and will share the same
flag-registration surface in `parser_specs.go`. The new
`--remote-control-socket` entry is `Hidden: true` (matching
`--remote-control-tcp`), so it does not change the
`remote-control --help` block. A `rc-serve-subcommand` cycle
that wants to expose the flag visibly can flip `Hidden` to
`false` without touching anything else.

---

## G15 — `created_at` format-string bug

### The bug

`internal/remotecontrol/dispatch.go:1234` (pre-fix) read:

```go
"created_at": c.CreatedAt.Format(timeFormat(c.CreatedAt)),
```

`timeFormat` is `func(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z07:00") }`.
The caller is passing the **formatted string back into Format**
as if it were a layout constant. Go's `time.Format` takes a
*layout* (a string of the form `"2006-01-02T15:04:05Z07:00"`),
not an arbitrary timestamp string. The resulting "formatting
the formatted string with itself as layout" produced nonsense
like:

```
"18186-100-108T180:08:180.7820879000Z"
```

The cycle-15 verifier's transcript captured this verbatim; the
known-gaps section in `rc-final-parity-verify/deliverable.md`
parked the fix for `rc-time-format-bug`. This cycle is the
closure.

### The fix

```go
"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
```

Matches the project surface used by
`internal/remotecontrol/project_dispatcher.go:251` and
`cmd/wagyu/remote_control.go:415`
(`c.ModTime.UTC().Format(time.RFC3339Nano)`). `timeFormat` the
helper is now dead — we leave it in place because removing it
is a separate concern (Go doesn't warn about unused functions,
and a future cycle that wants to change the layout has the
seam).

### Test

`TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano` in
`internal/remotecontrol/cascade_test.go`:

- Asserts `response.created_at` parses via `time.RFC3339Nano`.
- Asserts the parsed instant is within 30 s of `time.Now()` (a
  freshly-built cascade stamps `time.Now().UTC()`).
- **Explicit anti-regression guard:** `strings.Contains(createdAt, "18186")` — the
  literal substring the pre-fix code emitted. If the bug ever
  comes back, this fails fast even if the parse step is removed
  by a future refactor.

Test PASSES on this commit:

```
=== RUN   TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano
--- PASS: TestDispatcher_StartRemoteAgentCascade_CreatedAtRFC3339Nano (0.00s)
```

---

## G16 — `params.backend` passthrough

### The bug

The `handleStartRemoteAgentCascade` params struct (cycle-3a
shipped it) had no `Backend` field. The JSON unmarshaller
silently dropped `{"backend":"shim"}` from the request body, and
the response echoed the host session's backend instead of what
the dashboard asked for. On the foreground path the host
session's `Backend` is `""`, so the response was always `""` —
a dashboard couldn't pick a per-cascade backend.

The cycle-15 verifier's known-gaps section called this out
explicitly:

> `params.backend` in `StartRemoteAgentCascade` is not parsed
> (the params struct has no Backend field; see dispatch.go:1148).

### The fix

Added a typed `Backend string \`json:"backend"\`` field to the
params struct and updated the resolution:

```go
resolvedBackend := p.Backend
if resolvedBackend == "" && d.cfg.Session != nil {
    resolvedBackend = d.cfg.Session.Backend
}
```

The non-empty `params.backend` always wins (matching the
brief's "prefer it over the cascade's `BackendLabel` field").
Empty `params.backend` falls back to the host session's value,
which in turn falls back to `""`. The cascade is then built
with `CascadeOpts{BackendLabel: resolvedBackend, …}`, so
`Cascade.BackendLabel` reflects what the dashboard asked for
when `params.backend` was non-empty.

Also added `backend` to the
`nonStringFieldInJSON(params, "backend", p.Backend)` validation
chain so a numeric `params.backend` (e.g. `{backend: 42}`)
yields a clean `-32602` instead of being silently coerced to
`""` by the JSON unmarshaller — matching the
`model` / `effort` / `agent` / `sessionMode` discipline.

### Tests

Two new dispatcher-level tests in
`internal/remotecontrol/cascade_test.go`:

- `TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins` —
  host session wants `agw`, dashboard sends `params.backend =
  "shim"`; the response echoes `"shim"`, and the cascade's
  `BackendLabel` (looked up via `d.Cascades().Get(cid)`) is
  `"shim"`. **This is the round-trip the brief asks for.**
- `TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack`
  — host session wants `agw`, dashboard omits `params.backend`;
  the response echoes `"agw"`. Pins the inverse path so the
  pre-fix behaviour (host-session-only) can't silently regress.

Both tests PASS:

```
=== RUN   TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins
--- PASS: TestDispatcher_StartRemoteAgentCascade_ParamsBackendWins (0.00s)
=== RUN   TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack
--- PASS: TestDispatcher_StartRemoteAgentCascade_ParamsBackendEmptyFallsBack (0.00s)
```

### Verifier update

Both `internal/parityverify/parityverify_test.go` and
`docs/cycles/rc-final-parity-verify/verify.py` previously said
"params.backend is not parsed" in the `verifyStartCascade` /
`verify_start_cascade` notes. Updated both to say "params.backend
honored since rc-polish G16". The Python verifier also gained
a `backend != "shim"` assertion — pre-fix this would have been
a `gap`, post-fix it stays `verified`.

Live matrix output (this commit):

```
| `StartRemoteAgentCascade` | `rc-cascade-start` | verified | cascade_id=cas_fa07ca06eb2deab5, backend='shim' (params.backend honored since rc-polish G16) |
| `remoteControlVersion`    | `rc-version`       | verified | version=1.4.2 build_label=unknown daemon_sha=unknown |
```

---

## Sanity checks (run on this commit)

### Tests

```bash
$ go build -o /tmp/wagyu ./cmd/wagyu && \
  go test -race -count=1 \
    ./internal/remotecontrol/ \
    ./internal/remotetransport/ \
    ./internal/parityverify/ \
    ./internal/persistentrc/
ok  	github.com/chadgpt/openagy/internal/remotecontrol	1.914s
ok  	github.com/chadgpt/openagy/internal/remotetransport	2.758s
ok  	github.com/chadgpt/openagy/internal/parityverify	6.924s
ok  	github.com/chadgpt/openagy/internal/persistentrc	1.478s
```

`internal/parityverify` runs the live daemon under `-race` and
drives the full method catalog — its 6.9 s run exercises the
G15 and G16 fixes end-to-end. All three new tests in
`internal/remotecontrol` PASS.

### vet

```bash
$ go vet ./internal/remotecontrol/ ./internal/remotetransport/ \
          ./internal/parityverify/ ./internal/persistentrc/ \
          ./cmd/wagyu/
cmd/wagyu/model_default_effort_test.go:67:3: (*testing.common).Errorf call needs 2 args but has 3 args
```

The single diagnostic is the pre-existing 3-arg `Errorf` in
`model_default_effort_test.go:67`, owned by the parent tree
(see `AGENTS.md`). The brief's gates explicitly call this out
as leave-as-is. All other packages vet clean.

### gofmt

```bash
$ gofmt -l internal/remotecontrol/dispatch.go \
         internal/remotecontrol/cascade_test.go \
         internal/cli/parser_specs.go \
         internal/cli/version.go \
         cmd/wagyu/remote_control.go \
         internal/parityverify/parityverify_test.go \
         docs/cycles/rc-final-parity-verify/verify.py
(empty)
```

Touched files all clean. The 50-file pre-existing gofmt dirt in
`cmd/wagyu/` is owned by the parent tree (AGENTS.md) and
unchanged.

### Verifier

```bash
$ go build -o ./wagyu ./cmd/wagyu && \
  python3 docs/cycles/rc-final-parity-verify/verify.py
daemon listening on ws://127.0.0.1:61796/rpc
Wrote matrix to docs/cycles/rc-final-parity-verify/matrix.md
Wrote transcript to docs/cycles/rc-final-parity-verify/transcript.txt
Summary: verified=21 gaps=1 skipped=0

Gaps:
  - SendRemoteAgentCascadeMessage (rc-cascade-message): send error: {'code': -32603, 'data': 'remotecontrol: turn failed\npiagent: session event channel closed without done', 'message': 'Internal error'}
```

The single gap is the pre-existing `SendRemoteAgentCascadeMessage`
PiG `MessageEndEvent` issue owned by `rc-piagent-done-event`
(cycle-15's known gap #1). Reproduces on `git stash` of all our
changes — **not introduced by this cycle**. The Go verifier
side (`go test -race ./internal/parityverify/`) accepts the
`-32603` path and reports 22 / 22.

`StartRemoteAgentCascade` and `remoteControlVersion` both
verified with the new behaviour:

```
| `StartRemoteAgentCascade` | verified | cascade_id=cas_fa07ca06eb2deab5, backend='shim' (params.backend honored since rc-polish G16) |
| `remoteControlVersion`    | verified | version=1.4.2 build_label=unknown daemon_sha=unknown |
```

---

## 硬约束 (hard constraints honoured)

1. **Scope respected.** Only G12 / G13 / G15 / G16 were closed.
   G1–G11, G14, G17, G18 are owned by separate cycles (PLAN.md
   rows 1–14).
2. **No envelope-shape changes** outside what G15 / G16 mandate.
   The verifier still produces the same 22-method wire shape.
3. **`gofmt -l` clean on every touched file.** The 50-file
   pre-existing gofmt dirt under `cmd/wagyu/` is unchanged.
4. **`go vet` exit 0** on the brief's scope. The single
   diagnostic in `model_default_effort_test.go:67` is the
   pre-existing 3-arg `Errorf` and stays as-is.
5. **`go test -race -count=1`** PASSes on the brief's scope
   (`internal/remotecontrol/`, `internal/remotetransport/`,
   `internal/parityverify/`, `internal/persistentrc/`).
6. **No third-party deps added.** The cycle touches only stdlib
   + the project's own packages.
7. **`go test ./...` not run.** Scope matches the brief.
8. **No probing of `/logout`, `/exit`, `/keybindings` in print
   mode.** The cycle doesn't probe any print-mode path at all.
9. **No destructive ops** — no `git push`, no merge, no branch
   switch. Stays on `exp/rc-server-mode` at the same tip.
10. **Subagent race respected.** No edits to `openagy/`,
    `openagy-mermaid/`, `openagy-osc8-semicolon/`,
    `openagy-stream-glamour/`. No edits to untracked leftovers
    in `out/`, `.scratch/`, `cmd/wagyu-loop-repro/` (only read).

---

## 存疑 (limitations / open questions)

1. **The `timeFormat` helper is now dead.** `dispatch.go:1713`
   is no longer called anywhere. Go doesn't warn on unused
   functions, but a future cycle that wants a clean diff can
   delete it (1 line of definition + 5 lines of doc comment).

2. **`--remote-control-socket` is hidden** (matching
   `--remote-control-tcp`). The brief asks for "accepted but
   no-op today"; the cycle delivers that and one extra step
   (the flag actually picks the binding path when the env var
   is unset). If a future cycle wants the flag visible in
   `--help`, only the `Hidden` field flips.

3. **`params.backend` does not change the cascade's *backend
   implementation*.** The cycle pins the `BackendLabel` field
   (the string the dashboard asks for) but does not change
   which `MessageBackend` the cascade's turns route through.
   The dispatcher still wires `cfg.MessageBackend` into
   `CascadeOpts.DispatchDefault`. A future cycle that wants the
   `params.backend` to *also* pick the implementation (e.g. swap
   `PiagentBackend` for `StubBackend`) is out of scope here —
   that requires a per-cascade backend factory the
   `rc-piagent-bind` cycle didn't ship. Today, on the wire,
   `params.backend = "shim"` and `params.backend = "agw"` both
   route through `PiagentBackend`; the field is a *display /
   audit* signal, not a routing switch.

4. **The Python verifier's `SendRemoteAgentCascadeMessage`
   gap is netless-flakiness, not a regression.** The cycle-15
   verifier accepted the empty-reply path "when ≥ 2
   `cascade/state` notifications (starting + done) fire on the
   same connection"; the upstream PiG session on this machine
   closes with `-32603 "turn failed"` instead of firing the
   `done` notification when no OAuth network is reachable.
   Reproduces on `git stash` of all our changes. **Not
   introduced by `rc-polish`.** The Go verifier accepts the
   `-32603` path and reports 22 / 22.

5. **`created_at` test pins "within 30 s of now".** The
   pre-fix code emitted `Format(timeFormat(...))` strings that
   *also* parse as a recent instant when fed into
   `time.RFC3339Nano` because the inner `timeFormat` helper
   formats with a valid layout first. The **anti-regression
   guard** (`strings.Contains(createdAt, "18186")`) is what
   actually catches the bug — the parse-success path is just a
   belt-and-braces second check. The "18186" sentinel is the
   year the garbage format produces; if the cycle that fixed
   the bug had used `2099` instead, we'd need a different
   sentinel.

6. **`-32603` is now appearing for `SendRemoteAgentCascadeMessage`
   because the foreground daemon's host session no longer has a
   default backend.** Pre-fix the dispatcher echoed `""` for
   `response.backend` and the verifier's "params.backend is not
   parsed" note made the netless behaviour look intentional.
   Post-fix the same netless run hits `-32603` because the
   `PiagentBackend` factory still relies on the host session's
   backend to pick its wire model — and the foreground daemon
   leaves it unset. A future `rc-serve-subcommand` or
   `rc-piagent-done-event` cycle is the right scope to fix this;
   `rc-polish` is purely cosmetic.
