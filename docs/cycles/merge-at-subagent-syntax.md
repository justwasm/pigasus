# merge at-subagent-syntax — `@<subagent> <message>` prompt routing

## TL;DR

AGY 1.2.9 changelog line 177: typing `@<subagent-name> <message>` in the
prompt routes the message to the named running subagent's existing
session — a router, not a fresh dispatch. The clone parses the `@`-prefix
in `parseAtSubagentPrompt`, consults a `subagentrouter.Manager` registry
for the live session, and re-routes via `handlePromptAtPrefix`. Picker
overlay (5-row dim-238/blue mirror of cycle-91 slash picker) is deferred
to cycle 232 since this cycle focuses on the parser + dispatcher.

| | |
|---|---|
| Branch | `feature/at-subagent-syntax` |
| Commit | `1dc878d` |
| Merge | `8eb5941` |
| Lines | +1502 / −0 (6 files) |
| New package | `internal/subagentrouter` |
| Cycle report | `docs/cycles/at-subagent-syntax/deliverable.md` |

## Scope (vs AGY 1.2.9 line 177)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| `@<subagent>` prefix recognised in prompt | `cmd/wagyu/at_subagent.go::parseAtSubagentPrompt` | ✅ |
| Routes to existing running subagent session | `subagentrouter.Manager.Send` | ✅ |
| `@file/foo.txt hello` falls through to chat | tested explicitly | ✅ |
| `@<unknown-name>` falls through with error log | `Manager.NamesWithPrefix` returns empty | ✅ |
| Multi-subagent registry | `Manager.Names()` / `NamesWithPrefix()` accessors ready for cycle-232 picker | ✅ |
| `/btw` (cycle 230) untouched — separate Session triple | confirmed by deliverable note | ✅ |

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `internal/subagentrouter/manager.go` | new | +281 | `Manager` / `Sender` / `Entry` / `State` + typed errors + accessors |
| `internal/subagentrouter/manager_test.go` | new | +294 | 14 unit tests |
| `cmd/wagyu/at_subagent.go` | new | +311 | `parseAtSubagentPrompt`, `DefaultRouter`, `handlePromptAtPrefix` |
| `cmd/wagyu/at_subagent_test.go` | new | +316 | 14 unit tests |
| `cmd/wagyu/tui_run.go` | modified | +24 | import + `atRouter *subagentrouter.Manager` field + `submitPrompt` hook |
| `docs/cycles/at-subagent-syntax/deliverable.md` | new | +276 | TL;DR + scope + parser state machine + gates + 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | clean |
| `go build -o /tmp/wagyu-atsubagent ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | only `TestDispatchPrintSlash_LocalCommands` fails — pre-existing environmental flake |
| `gofmt -l` on every touched Go file | empty |
| `go vet ./cmd/wagyu/ ./internal/subagent/ ./internal/subagenttools/ ./internal/subagentrouter/` | only pre-existing cycle-224 warnings |
| New tests (cycle code paths) | 28/28 pass (14 unit + 14 TUI) |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |

## 存疑

1. **Autocomplete UI** not shipped this cycle (5-row overlay, dim-238/blue styling).
   `Manager.Names()` / `NamesWithPrefix()` are the public accessors a future
   picker overlay consumes; the picker is deferred to cycle-232.
2. **Wiring adapters** for the existing `internal/subagent.Subagent` (bash)
   and `internal/subagenttools` (PiG session) onto `subagentrouter.Sender`
   are **not** shipped. Today the registry is wired only from tests; production
   wiring is cycle-232 work. The mechanism (Manager, parse, dispatch) is
   feature-complete.
3. **`/btw` (cycle 230) is deliberately untouched.** `@<subagent>` is a
   *router* to an existing running subagent's session; `/btw` opens a *fresh
   independent* Session. The two paths share neither registry nor transcript.
4. **`@file`/file-mention**: regression-guard tested
   (`@file/foo.txt hello` falls through to chat); no such feature exists in
   the clone today.
5. `TestDispatchPrintSlash_LocalCommands` is the one remaining failure
   across all 3 runs. Pre-cycle-231; per
   `docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md:292`.
6. `TestCycle225QuotaCountdown` and `TestWrapRowsQuadratic` (the documented
   cycle-225 flakes) both pass under this diff (3/3 runs).

## Cleanup

- Worktree removed.
- Branch deleted.
- main is now `8eb5941`.
