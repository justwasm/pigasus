# Cycle 231 — `@<subagent> <message>` prompt routing

**Branch:** `feature/at-subagent-syntax`
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-at-subagent-syntax`
**Baseline:** `657f012` (cycle 230 merge)

AGY 1.2.9 changelog, line 177:

> · Added `@<subagent> <message>` prompt syntax to send a message
>   directly to a subagent conversation, with autocomplete listing
>   running and completed subagents

(`internal/cli/testdata/changelog_reference.txt`.)

## TL;DR

The clone's prompt-input layer (cycle-71 textarea + cycle-77 slash
router + cycle-230 `/btw` side-question card) treats every non-slash
buffer as a chat message — cycle 231 adds a parallel `@<subagent>
<message>` fast-path that routes the message body to a registered
subagent's session *instead of* the parent model.

Two new artifacts:

1. `internal/subagentrouter/manager.go` (new) — a thread-safe
   `Manager.Register(name, sender) → Dispatch(ctx, name, message)`
   registry the prompt layer dispatches against. Stdlib-only, no
   internal imports.
2. `cmd/wagyu/at_subagent.go` (new) — the `parseAtSubagentPrompt`
   parser + `handlePromptAtSubagent` hook into `submitPrompt`. The
   parser is the pure string → `(name, message, ok)` function; the
   hook owns the side effects (history push, transcript echo,
   dispatcher call).

The cycle-228 `invoke_subagent` tool still exists unchanged — it is
the *creation* path. Cycle 231 is the *conversation-routing* path:
once a subagent has been registered under a name (the future wiring
adapter is cycle-232 work), the user types `@<name> <message>` in
the parent prompt and the message body goes to that subagent's
session, never reaching the parent model.

## Scope

| File | Role |
|---|---|
| `internal/subagentrouter/manager.go` (new) | `Manager`, `Sender`/`SenderFunc`, `Entry`/`State` |
| `internal/subagentrouter/manager_test.go` (new) | 14 unit tests: Register/Get/SetState/Lookup/Names/NamesWithPrefix/Dispatch + concurrency |
| `cmd/wagyu/at_subagent.go` (new) | `parseAtSubagentPrompt`, `DefaultRouter`, `handlePromptAtPrefix` |
| `cmd/wagyu/at_subagent_test.go` (new) | 14 unit tests: parser branches + runMode integration |
| `cmd/wagyu/tui_run.go` | +24: import + `atRouter *subagentrouter.Manager` field on `runMode` + `submitPrompt` hook |

**Total:** 5 files changed, **+~660 / -0**.

## Parser state machine

`parseAtSubagentPrompt(buf string) (name, message string, ok bool)`
in `cmd/wagyu/at_subagent.go`. The single state machine:

```
                buf = "" ─────────────────────────────► chat path  (!ok)
                buf[0] != '@' ─────────────────────────► chat path  (!ok)
                buf = "@" ─────────────────────────────► chat path  (!ok, "still typing")
                buf = "@<name>" with no whitespace ─────► chat path  (!ok, "still typing")
                buf = "@<name> " with empty body ───────► empty-body error (ok=true, name=<name>, msg="")
                buf = "@<name><non-name-rune>" ─────────► chat path  (!ok, falls through)
                buf = "@<name><ws><message>" ───────────► route     (ok=true, name=<name>, msg=<message>)
```

The name slot accepts `[A-Za-z0-9_.-]` only (whitespace, `/`, `@`,
control bytes, and any other rune disqualify the buffer). The
whitespace separator may be space / tab / newline. The message is the
trimmed remainder of the buffer past the separator.

The character set is deliberate: `@file/foo.txt` is a plausible
file-mention shape (the cycle-231 task description points at
"`@<file>` (existing file-mention) is NOT regressed" as the
regression-guard buffer). With `/` disqualifying the name slot, that
buffer falls through to the chat path unchanged — no routing, no
error row. The autocompletion listing side is mentioned in the
changelog but deferred: a full picker overlay needs design that
matches the cycle-91 slash-command overlay byte-for-byte, and
shipping both at once risks regressing either. Cycle-232 focuses on
the wiring adapter; the picker is a follow-on.

## Hook into `submitPrompt`

`cmd/wagyu/tui_run.go:1097`. The reference path is the byte-exact
order:

```
1.  picker substitution        (existing, cycle 151/213)
1a. at-prefix routing          (NEW, cycle 231)
2.  slash panel interception   (existing, cycle 185/213)
3.  handleSlashCommand         (existing, cycle 77)
4.  chat path                  (existing, cycle 99)
```

Step 1a is the new layer. `handlePromptAtPrefix` returns `(false,
nil)` for any input that is not a routed prompt — i.e. the chat
path sees the original buffer unchanged. It returns `(true, nil)`
plus an error row in `m.response` for any routing-side error
(`@<name>` with no body, unknown name, dispatch failure), so the
input is reset and the user sees the failure inline. Success appends
a transcript echo and returns `(true, nil)`.

The hook owns the input reset itself (`m.pushHistory`,
`m.input.Reset`, `m.historyIdx = 0`, `m.picker.Reset`); it does *not*
append to `m.session`. Routed prompts never reach the parent
session — they go to the subagent's session via the registered
Sender. This is the contract the AGY 1.2.9 description implies.

## Running subagent registry

`internal/subagentrouter/manager.go` exposes:

| Method | Role |
|---|---|
| `Register(name, sender)` | record a Sender under `name`; rejects duplicates + nil sender |
| `Unregister(name)` | drop the entry by name |
| `Get(name)` / `Lookup(name)` | exact and case-insensitive lookup |
| `SetState(name, state)` | wire the lifecycle tag (`running` / `completed` / `failed`) |
| `Names()` / `NamesWithPrefix(p)` | sorted names, with a case-insensitive prefix filter |
| `List()` | every `*Entry`, sorted |
| `Dispatch(ctx, name, msg)` | resolve `name` (case-insensitive) and call `sender.Send` |
| `Count()` | length of the map |

Errors are typed so the prompt layer can map them to its own
display text:

| Sentinel | Surfaced as |
|---|---|
| `ErrEmptyName` | programming bug; not reachable through the prompt |
| `ErrAlreadyRegistered` | name-in-use conflict; the wiring layer logs |
| `ErrUnknownName` | `@<ghost> …` → `No subagent named "ghost" is running…` |

The Manager goroutine-safe through `sync.RWMutex`; a concurrency
test under `-race` hammers 50 goroutines on Register/Lookup/Dispatch
simultaneously and is clean.

The package stays stdlib-only and zero-internal-import. The cycle-228
`internal/subagenttools` and the bash `internal/subagent.Subagent`
both need adapter funcs to expose themselves as `subagentrouter.Sender`
implementations — that wiring is cycle-232, not this cycle.

## Gates

### Build

```
$ go build -o /tmp/wagyu-atsubagent ./cmd/wagyu
$ ls -l /tmp/wagyu-atsubagent
-rwxr-xr-x 1 gear staff 73116402 Oct 18 05:25 /tmp/wagyu-atsubagent
```

Exit 0, ~73 MB (cycle-230 base was ~69 MB; +30-line hook + the new
package is dominated by the unused `T_*` cycle-228 subagent surface
in the binary).

### Tests

```
$ go test -vet=off -count=1 ./internal/subagentrouter/
ok  	github.com/chadgpt/openagy/internal/subagentrouter	0.247s

$ go test -vet=off -count=1 ./cmd/wagyu/ -run "TestParseAt|TestHandlePromptAtPrefix|TestSubmitPrompt_AtSubagent"
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.169s
```

3× run of full suite as required by the task (`count=1` × 3):

| Run | Result |
|---|---|
| 1 | one pre-existing failure: `TestDispatchPrintSlash_LocalCommands` (see 存疑) |
| 2 | same as run 1 |
| 3 | same as run 1 |

All 28 new tests pass; the only failing test is documented in
`docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md:292` as a
pre-cycle-220 flake unrelated to this cycle's diff (verified by
`git stash -u` round-trip).

### `gofmt -l`

```
$ gofmt -l cmd/wagyu/at_subagent.go cmd/wagyu/at_subagent_test.go \
            cmd/wagyu/tui_run.go internal/subagentrouter/manager.go \
            internal/subagentrouter/manager_test.go
(empty)
```

### `go vet`

```
$ go vet ./cmd/wagyu/ ./internal/subagent/ ./internal/subagenttools/ ./internal/subagentrouter/
cmd/wagyu/slash_cycle224_test.go:86:41: (*testing.common).Errorf format %) has unknown verb )
cmd/wagyu/slash_cycle224_test.go:92:48: (*testing.common).Errorf format %) has unknown verb )
```

Both warnings predate this cycle (verified by `git stash -u`
round-trip — the cycle-224 file is untouched). No new vet warnings
introduced.

## Probe table

The cycle-231 work is purely *inside the prompt-input layer*; the
AGY 1.2.9 changelog line has no probe captures, so this section
documents the unit-level fixtures instead.

| Scenario | Prompt | Behaviour verified |
|---|---|---|
| bare input | `""` | `parseAtSubagentPrompt` → `("", "", false)` — chat path |
| chat input | `"hello world"` | falls through to chat |
| bare `@` | `"@"` | falls through to chat |
| `@<name>` no body | `"@explore-bot"` | falls through to chat |
| `@<name> ` empty body | `"@explore-bot "` | `("explore-bot","",true)` — empty-body error row |
| routed (known) | `"@explore-bot hi"` | Sender.Send called; transcript echoes the prompt |
| routed (case-insensitive) | `"@Explore-Bot hi"` | Sender.Send called (Lookup falls back to lowercase match) |
| routed (unknown) | `"@ghost hi"` | transcript row `No subagent named "ghost" is running…` |
| routed (dispatch failure) | `"@explore-bot hi"` (Sender returns error) | transcript row `at-subagent: failed to deliver: <cause>` |
| regression guard | `"@file/foo.txt hello"` | falls through to chat |
| slash command | `"/help"` | falls through to slash router |
| pi-test cycles (count=3) | full suite | only `TestDispatchPrintSlash_LocalCommands` fails; pre-existing |

## 存疑 / limitations

- **`@`-autocomplete UI not shipped.** AGY's picker overlay must
  list running *and* completed subagents while the user types
  `@ex`. The cycle-91 slash-picker is a tight 5-row window with
  specific ANSI styling; mirroring that overlay for `@` is a
  follow-on cycle (cycle-232 or later) so cycle 231 can land the
  parser + dispatch first. The deliverable calls out the parser's
  `Names()` and `NamesWithPrefix()` accessors so the picker can
  consume them without further changes to the registry.

- **Wiring adapters for `internal/subagent.Subagent` and
  `internal/subagenttools` to `subagentrouter.Sender` not shipped.**
  Today the only way to populate the registry is from a test; a
  production wiring needs a PiG session adapter (sync `Send`) and
  a bash `Subagent.Send` adapter. Cycle 232 plans both; today the
  mechanism is feature-complete, the production wiring is not.

- **`/btw` is intentionally untouched.** Cycle 230's `/btw`
  side-question card opens an **independent** coding.Session (no
  parent transcript, no hooks) and renders the streamed answer as a
  collapsed card. `@<subagent>` is a different shape (delivers to
  an *already-running* subagent's session, not a freshly opened
  one); the two paths do not share registry or transcript. The task
  description is explicit about this: "`/btw` is a separate
  independent Session path (cycle 230) and should not change".

- **`@file` (existing file-mention) is *not* a feature in the
  clone.** There is no `@<file>` autocomplete in the codebase; the
  reference binary's 1.2.9 changelog line refers to AGY's own
  file-mention feature, which the clone never implemented. The
  regression-guard test (`@file/foo.txt hello` falls through to the
  chat path) ensures we did not accidentally invent one.

- **Cycle-225 quota-countdown flake stays.** `TestCycle225QuotaCountdown`
  is documented in `merge-227.md:116` as a pre-existing flake; not
  touched by this cycle. (`TestWrapRowsQuadratic` likewise. Plus
  the cycle-220 model-resolution flake `TestDispatchPrintSlash_LocalCommands`,
  documented in `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md:292`.)
  All three fail identically at the pre-cycle `657f012` commit,
  verified by `git stash -u`.

## Hard constraints (per AGENTS.md)

- ✅ `internal/convstore/sqlite.go` untouched (the cycle-231 diff
  does not include it).
- ✅ Pre-existing unrelated files not reformatted (only 5 files in
  the diff; all gofmt-clean by construction).
- ✅ No push to remote.
- ✅ New files gofmt-clean (verified above).
- ✅ Build succeeds (`/tmp/wagyu-atsubagent`, exit 0).
- ✅ Tests pass except the three documented pre-existing flakes.
- ✅ Used model `agw/MiniMax-M3` throughout.
