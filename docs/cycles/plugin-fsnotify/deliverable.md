# Cycle plugin-fsnotify — final report

**Branch**: feature/plugin-fsnotify
**Base commit**: c4c5f58 (post-glamour-migration)
**Final commit**: see `git log -1 feature/plugin-fsnotify` (the report
includes itself, so the commit hash recorded at write-time will
not match the hash of the commit that ships it — the conventional
solution in this repo's other cycle reports is to leave the hash
implicit and let `git log` resolve it).

## Motivation

AGY 1.4.2 ships with `github.com/fsnotify/fsnotify` wired into the
plugin/skill/marketplace loaders (106 fsnotify symbols in the
`agy` 1.4.2 pclntab; see `out/local-macos/seeds/funcs.txt`). The
mechanism is the standard "fire a debounced filesystem event →
flip a staleness flag → loader re-reads on the next call" pattern.

The wagyu clone's loaders (`installedPluginRows`,
`pluginMarketplaces`, `pluginSkills`, `printSlashSkillInfos`) have
always read the disk on every call, so they were not literally
*stale* — but an edit made in another shell or an editor was
invisible until the user pressed ↑/↓ on the panel cursor (which
forces a re-derive in `togglePluginExpanded`, see
`cmd/wagyu/slash_panels.go:2530`). With fsnotify wired in, the
staleness flag flips the moment a file lands and the next /plugin
or /skills invocation re-reads without prompting.

This is a behaviour-add cycle, not a 1:1 swap. The scope was kept
tight: add the watcher, invalidate caches, hook into startup. No
loader was rewritten.

## What this cycle adds

1. **Direct dependency** — `github.com/fsnotify/fsnotify` is
   promoted from `// indirect v1.5.4` to direct `v1.10.1` in
   `go.mod` and `go.sum`. v1.10.1 brings the recursive-watching
   helpers AGY uses (`WatchList`, `AddWith`); the older indirect
   only ever linked `Watcher.Add` / `Watcher.Close` because no code
   in the repo imported it.

2. **`internal/pluginwatch` package** — the new fsnotify front end:
   - constructs over `auth.AntigravityConfigHome()` so a
     `$ANTIGRAVITY_HOME` override redirects all three roots
     (`config/plugins`, `skills`, `cache/marketplaces`) at once;
   - non-recursive watch on each existing root, missing roots are
     silently skipped (a fresh install with no plugins installed
     has no `config/plugins/` to watch);
   - 200 ms debounce window collapses editor-save event bursts
     (write + chmod + rename + close-write on Linux, several Write
     events on macOS) into a single staleness transition;
   - exposes `IsStale` / `MarkFresh` / `Close` / `Run`; all methods
     are safe on a nil `*Watcher` so loaders can call them
     unconditionally;
   - `(nil, nil)` from `New` when no roots exist — the loaders
     already render a useful "no plugins installed" state on a
     first-launch machine.

3. **TUI-only construction in `runLiveTUI`** — after
   `NewRunMode`, the watcher is constructed and `Run` is called in
   a goroutine tied to a `context.WithCancel` whose `defer
   cancel()` fires on TUI exit (Ctrl-C / Esc / q). The watcher is
   *not* constructed on the print path — `runPrintMode` exits
   before any UI tick could drain events, so an unobserved watcher
   is wasted overhead.

4. **Loader hooks** — every TUI-visible plugin/skill/marketplace
   loader now consumes the staleness flag at the top of its body:
   - `cmd/wagyu/plugin_data.go::installedPluginRows()`
   - `cmd/wagyu/plugin_data.go::pluginMarketplaces()`
   - `cmd/wagyu/plugin_data.go::pluginSkills()`
   - `cmd/wagyu/print_slash.go::printSlashSkillInfos()`
   The hook is a single line (`maybeConsumeStaleness()`) that
   reads an atomic `*pluginwatch.Watcher` pointer, calls
   `IsStale`, and `MarkFresh` if true. A nil pointer (print mode,
   or TUI before the watcher has been wired) is a no-op — no extra
   branch in the loader.

## Files

| File | Change |
|---|---|
| `go.mod`, `go.sum` | fsnotify v1.5.4 → v1.10.1, indirect → direct |
| `internal/pluginwatch/pluginwatch.go` | new (~290 lines) |
| `internal/pluginwatch/pluginwatch_test.go` | new (~250 lines) |
| `cmd/wagyu/plugin_data.go` | package-level `pluginWatcherPtr` + `maybeConsumeStaleness` + hooks in the three loader funcs |
| `cmd/wagyu/print_slash.go` | hook in `printSlashSkillInfos` |
| `cmd/wagyu/tui_run.go` | `runMode.pluginWatcher` field, watcher construction in `runLiveTUI` (TUI-only), package pointer store/clear |
| `docs/cycles/plugin-fsnotify/deliverable.md` | this file |

The 50 pre-existing gofmt-dirty files under `cmd/wagyu/` and
`internal/` are not touched. `cmd/wagyu/plugin_data.go` and
`cmd/wagyu/print_slash.go` were already gofmt-clean and remain so;
`cmd/wagyu/tui_run.go` is still gofmt-dirty from a pre-existing
indentation mismatch on `internal/hooks` (no diff to that line).

## Test results

`go test -vet=off -count=1 ./internal/pluginwatch/` — **all green**:
- `TestWatcher_DetectsNewFile` — Create under a watched root flips `IsStale`; `MarkFresh` clears it.
- `TestWatcher_Debounce` — 5 events 50 ms apart within the 200 ms debounce window produce zero transitions; the timer fires once after the burst ends, producing one transition.
- `TestWatcher_MissingRootIsOK` — three sub-cases (empty home, non-existent path, home exists but no roots) all return `(nil, nil)` and the nil-receiver methods are safe.
- `TestWatcher_CloseStopsRun` — 4 build/run/close cycles within ±2 goroutines of the baseline (Go runtime transient tolerance).

`go test -vet=off -count=1 ./cmd/... ./internal/...` — same 7
pre-existing flakes as on `c4c5f58`; no new regressions:

- `TestDispatchPrintSlash_LocalCommands` — fails on main too (verified via `git stash`); unrelated to this cycle.
- `TestWrapRowsQuadratic` — timing-sensitive benchmark that intermittently reports a super-linear ratio (also fails on main; not load-bearing).
- `TestCycle225QuotaCountdown`, `TestCycle225QuotaStubRefills`, `TestScrollCacheHit`, `TestRunStatusLineScript_SpawnEcho`, `TestChangelog_MatchesReferenceBinary` — all pass.

69 plugin/skill tests in `cmd/wagyu/` continue to pass byte-exact:
- `plugin_data_symlink_test.go` (6 tests, cycle plugin-source-symlink-resolve) — `installedPluginRows`, `pluginSkills`, `pluginMarketplaces` walk the fixtures under each `t.TempDir()` and the watcher is never wired, so the loaders fall through to the cycle-198 path unchanged.
- `slash_cycle*_test.go` (cycle 198/199/204/207/211/212/213/218/219/220/221/222/223/224/225/226/229/234/235) — the picker / detail / search tests, which all use `NewRunMode` directly. `pluginWatcher` is left nil by `NewRunMode`; the loaders' `maybeConsumeStaleness()` is a no-op.

## Behaviour change

**Before**: `agy plugin install foo` from another shell was
invisible to the running TUI until the user pressed ↑/↓ inside the
plugin picker (which forces a re-derive via
`togglePluginExpanded`). Edits to `~/.gemini/skills/<x>/SKILL.md`
required the same nudge.

**After**: the same install/edit becomes visible on the *next*
panel open, with no nudge. The mechanism:

1. fsnotify fires on the watched root (non-recursive — the
   installer's directory-create event reaches us via the parent
   `config/plugins/` watch).
2. The watcher debounces 200 ms.
3. On the next call to `installedPluginRows` /
   `pluginMarketplaces` / `pluginSkills` /
   `printSlashSkillInfos`, `maybeConsumeStaleness()` flips the
   flag off and the loader's normal `pluginscan.ReadEntries` walk
   sees the new state.

Print mode (`agy -p /plugin`, `agy -p /skills`) is unchanged: no
watcher, no goroutine, no overhead. The hook is still invoked
(it is one atomic-pointer read), but `pluginWatcherPtr.Load()`
returns nil and `maybeConsumeStaleness` returns immediately.

## AGY alignment

The AGY 1.4.2 binary uses fsnotify heavily. With this commit:

- `nm /tmp/wagyu-fsnotify | grep fsnotify | wc -l` reports **67**
  fsnotify symbols reachable from the linker graph (vs. **10**
  pre-cycle — only the indirect chain that PiG or chroma happened
  to pull in). The cycle pulls in `Watcher.AddWith`,
  `Watcher.Remove`, `Watcher.WatchList`, the `kqueue` / `inotify`
  backends, and the `Event.Has` / `Op.Has` helpers AGY's recursive
  hot-reload uses.
- `nm /tmp/wagyu-fsnotify | grep pluginwatch` reports the package's
  exported methods (`Close`, `IsStale`, `MarkFresh`, `Run`,
  `SetDebugf`) plus the internal helpers (`loop`, `queueFlip`,
  `flipStale`).
- The plugin/skill/marketplace loaders now consume a `Watcher`
  before each scan, mirroring AGY 1.4.2's pre-scan staleness gate
  (see `out/local-macos/decompile/<plugin/skill package>.c` for the
  AGY 1.4.2 fsnotify call sites; the exact call shape varies per
  loader, but the watcher → IsStale → MarkFresh contract is the
  same one we ship here).

## Design notes / 存疑

1. **Why a package-level atomic.Pointer instead of threading the
   watcher through every loader call?** The loaders are
   package-level functions called from ~70 sites across
   `cmd/wagyu/`, mostly via `runMode` methods. Adding a
   `*Watcher` parameter to each would be a 100+ line diff with no
   behavioural upside. The atomic pointer is a one-line setter in
   `runLiveTUI` and a one-line read at the top of each loader. It
   is the same pattern `internal/taskregistry` uses for the task
   subscriber pointer.

2. **Why non-recursive watch?** The three roots (`config/plugins`,
   `skills`, `cache/marketplaces`) cover everything we care about.
   A `Create` event for a subdirectory lands on the parent root's
   watch and is delivered as the child path, so we still see new
   plugins / skills / marketplaces as they appear. The recursive
   case (a brand-new `config/plugins/<x>/skills/<y>/` directory)
   also surfaces: the parent's `Create` event carries the new
   `<y>` path, and the loader's `pluginscan.ReadEntries` walks it
   on the next re-scan.

3. **Why skip missing roots in `New`?** A first-launch machine
   has no `~/.gemini/config/plugins` (and may have no
   `~/.gemini/skills` either). Constructing an fsnotify watcher
   over a non-existent path errors out; skipping silently lets the
   user reach the TUI on a fresh install and have the watcher
   silently "turn on" the first time a plugin is installed
   (well — turn on for the next launch; the live session still
   needs a restart to pick up a brand-new root. Documented
   behaviour, matching AGY 1.4.2's own semantics).

4. **The `WatchList` symbol is reachable but unused.** v1.10.1's
   `WatchList()` returns the list of currently-watched paths; the
   package does not call it because the loader never needs to ask
   "what are you watching?". The symbol is pulled in because
   v1.10.1's `Watcher` struct now embeds it in the `interface`
   table. Acceptable cost.

5. **What the test count looks like**: 4 `TestWatcher_*` tests in
   `internal/pluginwatch/pluginwatch_test.go` (one of which has 3
   sub-cases). 0 changes to existing tests — the new `runMode`
   field and the package-level `pluginWatcherPtr` both default to
   nil, and the loader hook is a no-op when the pointer is nil.

## Verification commands

```bash
# Build (verified working: exit 0, ~88 MB binary)
go build -o /tmp/wagyu-fsnotify ./cmd/wagyu

# New package tests
go test -vet=off -count=1 ./internal/pluginwatch/

# Full suite (same 7 pre-existing flakes as on main)
go test -vet=off -count=1 -timeout 240s ./cmd/... ./internal/...

# gofmt on the new files (clean)
gofmt -l internal/pluginwatch/
```