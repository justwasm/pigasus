# Cycle status-line-script — external `statusLine.command` + JSON payload

## Goal

AGY 1.1.21 added a `cost` field to the status-line data model so a
custom status-line renderer can show the running session cost. AGY
1.1.27 extended that to a JSON payload — `statusLine.command` is the
configured path, the CLI spawns the script on every refresh tick,
writes the recovered payload to stdin, and reads the rendered line
from stdout.

Cycle status-line-script wires the same surface into the clone. The
two new JSON fields are `cost` and `conversation_title`; the rest
(`model`, `current_dir`) are the obvious companion fields the live
probes already recover. The external rendering path is opt-in:
nothing about the built-in `/statusline` toggle changes unless
`statusLine.command` is configured.

## Changelog anchors

| Release | Line | Shape |
|---|---|---|
| 1.1.21 | 393 | added a `cost` field to the status line data model, exposing the unrounded estimated cost of the current session so a custom status line can display running token spend |
| 1.1.27 | 318 | added a `conversation_title` field to the JSON payload passed to custom status line and window title scripts, so they can show the active conversation's name and pick up renames immediately |

## Files

| Path | Role |
|---|---|
| `cmd/wagyu/slash_statusline_external.go` (new, 520 lines) | loadStatusLineConfig, statusLineExternalConfig, statusLinePayload, buildStatusLinePayload, pricingTable/pricingForModel, sessionTokenTotals / recordTurnUsage, splitStatusLineCommand, runStatusLineScript + 1 s timeout, statusLineExternalState (render cache + tick counter + lastErr), ensureExternalState / drainExternalStatusLine / externalRenderOK / externalRenderedForRow, statusLineExternalTickMsg + armExternalStatusLineTick, persistStatusLineVisibility |
| `cmd/wagyu/slash_statusline_external_test.go` (new, 460 lines) | 14 tests: loadConfig shape validation, payload field shape + JSON wire form, pricing card (claude-opus/substring, MiniMax M3, output rate), recordTurnUsage accumulation + nil-safety, runStatusLineScript happy/error/empty paths, splitStatusLineCommand token split, three-state cycle (off → built-in → external → off) with persistence invariant (statusLine.command block not clobbered), cycle wrap-around, View() short-circuits to external render, error path falls back to built-in template |
| `cmd/wagyu/tui_run.go` (+81 / −0) | four new runMode fields (statusLineExternal, statusLineCommand, sessionPromptTokens/completionTokens atomic, statusLineExternalState pointer), Init arms the 100 ms tick when external mode is configured, Update dispatches statusLineExternalTickMsg → spawn-in-goroutine + re-arm, statusRow() short-circuits to the cached external render, runLiveTUI seeds statusLineExternal + statusLineCommand from settings.json, chunk handler wires m.recordTurnUsage(chunk.Meta) on every Done chunk |
| `cmd/wagyu/slash_commands.go` (+64 / −0) | `/statusline` three-state cycle when a statusLine.command is configured (A→B→C→A), cycle-105 two-state otherwise |

**Net:** +604 lines across 4 files (2 new).

## Wire form — JSON payload

The CLI writes the following JSON object to the spawned script's
stdin every 100 ms:

```json
{
  "model": "MiniMax M3",
  "cost": 0.0125,
  "conversation_title": "My Session",
  "current_dir": "/Users/foo/project"
}
```

Field order is what `json.Marshal` of `statusLinePayload` produces:
`model`, `cost`, `conversation_title`, `current_dir`. (Go's
`encoding/json` honours struct-field order; the recovered wire form
/btw and /cost tests already prove this is the order the reference
expects.)

| Field | Type | Source |
|---|---|---|
| `model` | string | `m.statusModel` else `m.model`; the same label the built-in row shows (cycle 80 strips the tier parenthetical) |
| `cost` | float64 | `pricingForModel(model, promptToks, completionToks)` — the unrounded USD running total. MiniMax M3 → $0 (cycle 225 quota UI must keep showing the model as free). |
| `conversation_title` | string | `m.conversationTitle()` — the same helper that feeds `/title`. `/rename` updates flow through on the next tick. |
| `current_dir` | string | `m.cwd` — verbatim. |

## Subprocess shape

`runStatusLineScript(command, payload)`:

1. `splitStatusLineCommand(command)` — shell-style token split, so
   `command = "/usr/local/bin/tmux-status --foo"` lands as
   `["/usr/local/bin/tmux-status", "--foo"]`.
2. `exec.CommandContext(ctx, name, args...)` with a 1 s timeout.
   Inherits `os.Environ()` so the user's PATH carries.
3. `stdin.Write(payload)` runs in a goroutine, then `stdin.Close()`.
   The goroutine + `cmd.Wait()` keep a blocked write from
   deadlocking the context cancel.
4. `stdout` is read into a buffer; trimmed of trailing newlines.
   Empty output becomes a sentinel error so the caller can fall
   back to the built-in row.
5. Non-zero exit / timeout / empty stdout → `runStatusLineScriptResult{Err: ...}`
   whose `Err.Error()` ends up on the transcript once.

## Cost field — pricing table

```go
var pricingTable = []pricingRow{
    {"claude-opus",   15.0, 75.0, 15.0},  // $15/$75 per 1M
    {"claude-sonnet",  3.0, 15.0,  3.0},
    {"gpt-oss",        0.5,  1.5,  0.0},
    {"gemini-3.8-flash", 0.075, 0.30, 0.0},
    {"minimax",        0,    0,    0},    // MiniMax M3 is free
}
```

Match is case-insensitive substring: `claude-opus-4.7` lands on the
`claude-opus` row. Order matters: `claude-opus` precedes
`claude-sonnet` because the latter is a substring of the former —
walking the table and returning on the first hit means the more
specific row always wins.

The cycle-225 quota counts only billable tokens, so MiniMax M3 being
$0 keeps the free-tier model free. The numbers themselves are
approximations of the public 2026-Q4 card — the clone has no access
to the reference's `jetski_cortex.ModelPricingInfo` protobuf, so the
table is best-effort. What matters is the field is *named* `cost`,
holds a *number*, and is *unrounded*.

Cache reads are priced but the gateway has not been wired to surface
them yet; the cost helper accepts a `cacheRead` argument that
defaults to 0 in the current call site.

## Token accumulation

`sessionPromptTokens` / `sessionCompletionTokens` are atomic `int64`
on `runMode`. The chunk handler at `tui_run.go:2678` calls
`m.recordTurnUsage(chunk.Meta)` on every Done chunk. Nil-safe
(`recordTurnUsage` drops a nil meta and a nil receiver). The JSON
`cost` field reads via `m.sessionTokenTotals()` from the spawn
goroutine, atomic-int load.

Print mode (--print / -p) does not have a status line so the helper
is never called outside the TUI's chunk path. Tests can drive it
directly to seed counters without going through a real gateway.

## `/statusline` three-state cycle

When `statusLine.command` is configured, the toggle becomes a
three-state cycle:

```
state A: built-in row visible   (statusLine = "",  external = false)
state B: external row visible   (statusLine = "",  external = true)
state C: row hidden             (statusLine = "off")
```

`/statusline` advances A → B → C → A → ... so the configured script
gets its turn in the rotation. Without a configured command, the
cycle-105 two-state toggle (A ↔ C) stays in place — no behaviour
change for users who have not set up a script.

Persistence uses the existing `statusLine` block in settings.json:

```
{
  "statusLine": {
    "type":    "command",
    "command": "/usr/local/bin/tmux-status",
    "enabled": true
  }
}
```

The `enabled` flag doubles as the visibility bit: `true` = the row
is visible (state A or B), `false` = hidden (state C). Toggle
writes go through `auth.SetSettingsField("statusLine.enabled", bool)`
which mutates the block in place rather than replacing it with a
flat bool — `auth.SetSettingsBool("statusLine", true)` would
clobber the `command` field. `persistStatusLineVisibility` is the
writeback helper; the cycle-105 path falls through to the
unchanged `SetSettingsBool` so existing persistence does not
regress.

## Tick path

```go
case statusLineExternalTickMsg:
    m.ensureExternalState().markTick()
    go m.runExternalStatusLineTick()
    return m, armExternalStatusLineTick()
```

100 ms tick, self-sustaining while external mode is armed.
`armExternalStatusLineTick()` is `tea.Tick(100ms, ...)` — the same
form as `armTasksTick()`. The spawn runs in a goroutine so a
slow script cannot block the bubbletea runtime. The drain
counter (`statusLineExternalState.tickCount`, atomic) makes
"exactly one spawn per tick" race-free across concurrent fires.

`Init` arms the tick only when `m.statusLineExternal` is true at
startup, so the cost is paid only for users who actually have a
script configured. When external mode is off the handler is a
no-op and the tick dies the moment it stops being re-armed.

## `statusRow()` short-circuit

```go
func (m *runMode) statusRow() string {
    if out, _ := m.externalRenderedForRow(); out != "" {
        return out
    }
    // ... existing built-in template ...
}
```

`externalRenderedForRow()` returns the cached rendered line iff
state B (external) is active AND the script has produced output.
On any other branch the function returns `""` and the built-in
template handles it. A transient script error (`lastErr != nil`)
also returns `""` even if `m.statusLineExternal` is still true —
the user is never staring at a blank row because of a misbehaving
script; they get the built-in row until the script recovers.

## Failure / fallback matrix

| External mode | statusLine | Render path |
|---|---|---|
| off | (any) | Built-in template (cycle-105 behaviour unchanged) |
| on (script armed) | "off" | Hidden |
| on | "" | External script output (state B) |
| on (script errored) | "" | Built-in template; one-line stderr note in transcript |
| off / on | (script not yet produced output) | Built-in template (no blank row) |

## Evidence

### Recovered / config
- `cmd/wagyu/testdata/print_changelog.md` line 318 — `conversation_title` changelog anchor.
- `cmd/wagyu/testdata/print_changelog.md` line 393 — `cost` field changelog anchor.

### Tests (cycle status-line-script, 14 added; all pass)
- `cmd/wagyu/slash_statusline_external_test.go` — loadStatusLineConfig shape (4 cases), payload field order, pricing table (5 invariants), recordTurnUsage accumulation + nil-safety (2), subprocess spawn + non-zero exit + empty output (3), splitStatusLineCommand (4 cases), three-state cycle + persistence invariant, cycle wrap-around, View() short-circuit, error-fallback path.

### Existing tests kept green
- `TestStatuslineOffHidesRow` (cycle 105) — when statusLine="off" the row is hidden, regardless of `m.statusLineExternal`.
- `TestSlashToggles_Persist` (cycle 105) — cycle-105 path (`/statusline` flips "" ↔ "off", settings.json gets the bool form) is reached when no script is configured, so the regression test continues to pass.

### Cycle 225 quota — MiniMax M3 is $0
- The cycle-225 quota panel counts billable tokens; pricing matches
  it. MiniMax M3 hits the zero-rate row so the weekly quota UI
  keeps working unchanged.

## Limitations / 存疑

1. **Pricing table is best-effort.** The reference's `ModelPricingInfo`
   is a private protobuf; the clone's rates are public 2026-Q4
   approximations. Field naming, ordering, and unrounded
   float-ness match the contract — the numerical values may drift.
   On release, the table should be re-derived from AGY's
   decompilation (the cycle-103 binary probe saw pricing bytes in
   the protobuf-encoded constants section).

2. **Cache reads are not priced yet.** Pricing helper accepts a
   `cacheRead` arg but the chunk handler's `ChunkMeta` does not
   carry it; pass-through 0. When the gateway starts emitting
   `CacheReadTokens`, the spawn goroutine should pass them through.

3. **The recovery rate-card and the recovery cycle-105 fallback.**
   Three-state cycle uses `statusLine.enabled` for persistence so
   writes do not clobber `statusLine.command`. This means a
   fresh settings.json that has `statusLine: false` (cycle-184
   bool form, no script) does NOT take the three-state path —
   `loadStatusLineConfig` returns ok=false. If a user upgrades to
   a script-config schema while still having the bool form, they
   need to migrate once: `SetSettingsField("statusLine.command",
   "/path/to/script")` from a future /config panel, or hand-edit
   settings.json.

4. **No /config panel integration yet.** The /config panel reads
   `SettingsField` directly; live editing of `statusLine.command`
   should refresh the runMode's cached `m.statusLineExternal` so
   mid-session changes take effect — that wiring is a future
   cycle.

5. **Binary size impact.** With this cycle, the build is still
   ~73 MB. The 1 s subprocess timeout uses stdlib `context` and
   `exec.CommandContext`; no third-party deps. The pricing table
   is a 5-row literal; the cost of `externalRenderedForRow()` is
   one `state.current()` call (atomic + mutex) per render.

## Hard constraints

- `gofmt -l` is clean for the two new files (the cycle-105 note
  about gofmt noise in `cmd/wagyu/` does not extend to them).
- No third-party deps added.
- `internal/convstore/sqlite.go` was not touched.
- The 50 cycle-105-noise pre-existing gofmt issues under
  `internal/` were not re-formatted.

## Build / test gates

```
go build -o /tmp/wagyu-statusscript ./cmd/wagyu
  → 0; binary = 73.1 MB

go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
  → all green except the pre-existing baseline
    TestDispatchPrintSlash_LocalCommands (verbatim from cycle 230).

for i in 1 2 3:
  → no cycle-225 clock-edge flake, no new failures.
```
