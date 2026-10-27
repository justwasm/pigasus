# Cycle 346 deliverable: a simplification survey — what the clone's own design can lose

**Cycle:** `cycle-346-refactoring-survey`
**Subject:** "搜寻现在这个项目哪些地方的设计应该通过重构来简化" — an audit of
the clone's own design for places that carry avoidable weight.
**Method:** measurements over the whole tree (line counts, the import
graph, function sizes), three read-only audits of three areas (the
remote-control family, the CLI/config plumbing, the turn/event/error
plumbing), and a first-hand verification of every claim that mattered.

---

## 0. The short answer

The clone is not big for its own sake — 254,872 lines of Go, of which
~25,000 are generated protobuf. But a meaningful part of it is **weight
without behaviour**: a second tool registry nobody calls, a transport that
nobody starts, a struct of 69 fields of which 50 are never read, two
copies of the same turn-recovery vocabulary, and a set of hand-kept
tables that drift away from the code they describe.

| # | what | payoff | risk | owner |
|---|---|---|---|---|
| A1 | `internal/tools`' **second registry** (`RegisterAll` + the `stubs*`/`git.go` files) is reachable only from a test | **−1,486 lines** | low | ours |
| A2 | `internal/remotetransport/unix.go` is a dead twin of the daemon's own Unix transport | **−401 lines** | low | **theirs** |
| A3 | `internal/printmode/main_print_tools.go` is a byte-for-byte twin of `internal/toolrunner/runner.go` | **−90 lines** | very low | ours |
| A4 | `internal/lsserver.Flags`: 69 fields, **50 never read**; plus a second argument scanner | **−90 lines, −50 fields** | low | theirs |
| A5 | `internal/agent/auto_continue.go` and `internal/pigrt/auto_continue.go` carry the same recovery vocabulary twice | **−150 lines** | low | ours |
| A6 | `internal/agent`'s `errorSlot` / `recordingProvider` duplicate `internal/pigrt`'s | **−50 lines** | low | ours |
| A7 | two lifecycle state machines exist **only for tests** | **−904 lines** | low | ours |
| A8 | `internal/cli/parser.go`'s `AliasMap`, the parked `serve_bridge` symbols, four import anchors | **−60 lines** | very low | ours |
| A9 | `internal/browserbridge`'s `Configure`/`CurrentConfig` are called from nowhere | **−20 lines** | very low | theirs (in flight) |
| B1 | `internal/remotecontrol`: **8 copies** of the same atomic-write helper; 2 spawners of the same reference binary; the CSRF constant hard-coded 4× | **−300 lines** | low | **theirs** |
| B2 | the streaming-method table re-declared instead of read from `lsproto` | **−27 lines** | low | **theirs** |
| B3 | the SSE wire schema written twice; the usage→`ChunkMeta` mapping written **4×**; the two provider implementations that must stay identical | **−100 lines** | medium | ours |
| C1 | one-line alias wrappers and pass-through layers (`cliapp/run.go`, `cliapp/tui_help.go`, `cliapp/browser_flags.go`) | **−120 lines** | very low | ours |
| **D3** | **`rchost`'s TCP dispatcher drops `WebTerminals` + `WebAudio` → the Web UI's terminal and voice surfaces are dead on `--remote-control-tcp`** | **a live bug** | very low | **theirs** (fix first) |

**The win is not the lines** — it is the number of places a change has to
be made. One example from this cycle's own work: the browser's headless
default lives in **three** places (`internal/cli/parser_flags.go` says
`"true"`, `internal/browsercdp/config.go` says `false`,
`internal/cliapp/browser_flags.go` only honours the flag when it is
*given*). Any change to that behaviour is a three-file edit, and the
three can disagree silently.

---

## 1. Method

| step | what it produced |
|---|---|
| line counts per file and per package | the size map: `internal/remotecontrol` 36,378 lines / 137 files; `internal/cliapp` 24,332 / 84; `internal/tools` 9,889 / 44 |
| the import graph (`go list`) | fan-in: `internal/auth` 21 importers, `internal/backend` 19, `internal/cli` 11; fan-out: `internal/cliapp` 126 clone packages |
| function-size scan | 38 functions over 60 lines; the biggest real one is `internal/rctunnel/patches/registry.go:95` (2,860 lines of data) |
| three read-only audits | the remote-control family, the CLI/config plumbing, the turn/event/error plumbing |
| first-hand verification of the claims | every number below was re-measured in the tree before it was written down |

**What did not survive verification:** one audit claimed the OAuth scope
hint is printed twice (once by `internal/apierror/api_error.go:97`, once
by `internal/cliapp/tui_stream_prompt.go:110`). It is not: the two are
different paths (the headless classifier and the TUI's own renderer) and
each shows the hint once. What is true is weaker — the hint is appended
by **two different mechanisms** for the same condition, which is a drift
risk rather than a visible bug. It is recorded in §8, not as a finding.

---

## 2. Class A — code that can simply go

### A1. `internal/tools` has a second registry that nothing calls

`internal/tools/catalog.go:102` builds the live catalogue (the
reference's 63 names). `internal/tools/tools.go:141` builds **another**
one:

```go
func coreTools(workspace string) []Tool   // tools.go:85
func stubTools(workspace string) []Tool   // tools.go:108
func RegisterAll(reg *Registry, workspace string) // tools.go:141
```

and the only caller in the whole tree is a test
(`internal/tools/tools_test.go:458`). With it go:

| file | lines |
|---|---|
| `internal/tools/stubs_search.go` | 464 |
| `internal/tools/stubs_git.go` | 264 |
| `internal/tools/stubs.go` | 246 |
| `internal/tools/stubs_tasks.go` | 240 |
| `internal/tools/git.go` | 272 |
| `internal/tools/tools.go` (the three functions) | ~65 |

**1,486 lines, no product path.** The stubs are also the reason eight
tools bypass the canonical argument canonicaliser (`argsOf`,
`internal/tools/tools_params.go:72`) with a raw `json.Unmarshal` — the
inconsistency disappears with the files.

**Caveat:** `internal/harnesstools` is a *fourth*, parallel tool
surface; checked first-hand — it never imports `internal/tools`, so
nothing there expects `tools.RegisterAll`. The only reader of
`RegisterAll` in the whole tree is `internal/tools/tools_test.go:458`.

### A2. `internal/remotetransport/unix.go` — 401 lines, no callers

`NewUnixServer` is referenced nowhere outside the file. It is a
line-for-line twin of the daemon's own transport
(`internal/remotecontrol/daemon.go:176` `bindUnixSocket`,
`:244` `acceptLoop`, `:293` `handleConn`, `:345` `writeJSONLine`) — the
daemon's own comment says it exists rather than wrapping
`*remotetransport.UnixServer`. Only the `Dispatcher`/`StreamDispatcher`
interfaces in it are worth keeping.

**Owner:** `internal/remotetransport` is the other agent's area — this is
a finding to hand over, not a change to make now.

### A3. `internal/printmode/main_print_tools.go` is a twin

```go
// printmode                          // toolrunner
toolDeclsFromRegistry  (:19)      ≡   ToolDeclsFromRegistry      (runner.go:16)
advertisedToolDecls    (:34)      ≡   AdvertisedToolDecls        (runner.go:31)
registryRunner.Run     (:53)      ≡   RegistryRunner.Run         (runner.go:53)
permissionRunner.Run   (:65)      ≡   PermissionRunner.Run       (runner.go:66)
sandboxDeniedToolResult(:87)     ≡   SandboxDeniedToolResult     (runner.go:89)
toolTarget             (:91)      ≡   ToolTarget                 (runner.go:94)
```

Identical bodies, including the long sandbox-denial string. The
canonical copy is exported and `internal/cliapp/main_dispatch.go:165`
already calls it; `internal/printmode` imports neither. **Six call sites
become six one-liners; ~90 lines go.**

### A4. `internal/lsserver.Flags`: 69 fields, 50 of them dead

The struct exists so that `--help` is byte-identical to the reference's
language server. But a `flag.FlagSet` prints its usage string without
any destination at all:

```go
fs.String(name, def, usage)   // no destination: the help text is the same
```

Measured: of the 69 exported fields, **19** are read in product code
(`AppDataDir`, `ConfigDir`, `CSRFToken`, `EnableSidecars`, `GeminiDir`,
`LSPPort`, …) and **50** are not read outside the registration file —
including the four Chrome flags (`CDPPort`, `LocalChromeHeadless`,
`LocalChromeUserDataDir`, `UseLocalChrome`), which duplicate
`browsercdp.Config`'s four fields and are never copied between them.

There is also a **second argument scanner**: `cmd/langserver/main.go:57`
calls `lsserver.Register(fs)`, then `lsserver.InspectArgs`
(`internal/lsserver/args.go:64`) scans the same `os.Args` a second time
before `fs.Parse` runs.

**Owner:** `internal/lsserver` is the other agent's area.

### A5. The auto-continue vocabulary exists twice

`internal/agent/auto_continue.go` (344 lines) and
`internal/pigrt/auto_continue.go` (285 lines) carry the same strings and
the same parsing:

| | agent | pigrt |
|---|---|---|
| `AutoContStreamInterrupted = "The stream was interrupted. Please continue the task you were working on."` | `:52` | `:69` |
| `AutoContIterationCapFormat = "**Status**: Cancelled after %d iterations"` | `:57` | `:77` |
| `AutoContMaxDepth` / `ErrAutoContMaxDepth` | ✓ | ✓ |
| the digit parser | `turnsFromErr` `:196` | `iterationCountFromErr` `:160` |
| the classifier | `classifyInterruption` | `classifyInterruption` |

The *policy* differs (the TUI recurses, the print path stops) — only the
vocabulary is shared, and it belongs in one package.

### A6. `errorSlot` and `recordingProvider` exist twice

`internal/agent/pig.go:198` and `internal/agent/pig.go:223` are
functional twins of `internal/pigrt/errors.go:26` and
`:54`. Both exist because PiG copies only `err.Error()` across the
stream boundary (AGENTS.md invariant #4), so the typed error has to be
recorded out of band. One implementation, exported from `pigrt`, would do.

### A7. Two lifecycle state machines exist only for tests

| | lines | reachable from |
|---|---|---|
| `internal/backend/lifecycle.go` + `lifecycle_state.go` | 275 + 184 | `backend/lifecycle_test.go`, `backend/transcript_test.go:200`, `cascade/real_lifecycle_test.go:104` |
| `internal/cortex/lifecycle.go` | 445 | `cortex/cortex_lifecycle_test.go` only |

The live one is `internal/agent/cascade.go`. **904 lines** of production
code that only tests use.

### A8. Dead API and import anchors

| | |
|---|---|
| `internal/cli/parser.go:264` `AliasMap` / `:267` `LookupFlagValueCmd` / `:285` `StandardAliasMap` | "kept for source compatibility with cycle 33-37 callers" — the only readers are `internal/cli/parser_test.go:250` |
| `internal/cli/serve_bridge.go:116` `errServeHandlerNotRegistered` | "parked for a future cycle", never referenced |
| `internal/cli/serve_bridge.go:110` `EnsureServeHandlerRegistered` | a test seam used by one test file |
| `internal/cli/parser.go:314-316`, `internal/cli/foreground_bridge.go:74`, `internal/rchost/remote_control_serve.go:293` | four `var _ = …` import anchors |

### A9. `internal/browserbridge`'s policy is frozen

`internal/browserbridge/session.go:27,43` — `Configure`/`CurrentConfig`
are called from nowhere outside the package, so the policy is permanently
`DefaultConfig()`; `Check`, `Probe` and `GetClient` all read that frozen
value. (The other agent's in-flight area.)

---

## 3. Class B — duplication that should be hoisted

### B1. `internal/remotecontrol`: the same helpers, eight times

| duplicate | where |
|---|---|
| `atomicWrite` + the dir-fsync block | `triggers_store_fs.go:99`, `fastpush_store_fs.go:183`, `plugins_store_fs.go:117`, `resources_store_fs.go:100`, `permissions_store_fs.go:161`, `conversation_config_store_fs.go:129`, `origin_policy_store_fs.go:99` (7 copies) |
| `ensureParentDir` | the same seven files, one each |
| the CSRF header name | `internal/rctoken/rctoken.go:60` declares it; `internal/refbackend/csrf.go:30` re-declares it; `internal/rctunnel/proxy.go:87,178` hard-code the literal |
| the token minters | `rctoken.Generate` (32 B base64url), `refbackend/spawn.go:343 newSecret` (24 B), `rctunnel/types.go:75 NewCSRFToken` (a UUID) |
| the reference `language_server` spawner | `internal/refbackend/spawn.go:153 referenceArgs` vs `internal/rctunnel/supervisor.go:63-84` — the same flags, the same endpoint constants, the same port discovery |
| the streaming-method table | `internal/refbackend/connect.go:28` (24 rows) vs `internal/lsproto/lsproto.go:129 ServerStreamingMethods()`, whose own doc says a hand-written list drifts from the descriptor |
| the Connect content-type dialect | `internal/remotetransport/connect_codec.go:34-41,77,93` vs `internal/rctunnel/httpproxy.go:53-77,180` |

`internal/atomicfile` already exists (5 importers) — the store helpers
should call it.

### B2. The hand-kept method tables in `internal/remotecontrol`

Four tables describe the same router:

| table | rows | note |
|---|---|---|
| `reference_surface.go:46` `ReferenceRPCMethods` | 79 | the reference's names |
| `reference_surface.go:131` `referenceRPCAliases` | 34 | **14 of them are unreachable** — the LS tables are consulted first (`dispatch.go:426` before `:439`), so `StartCascade`, `ReadFile`, `ListProjects`, `CreateTerminal`, `DeleteAgentMessage`, … are already answered earlier |
| `top_level.go:337` `DefaultDispatchMethods` | 52 | hand-kept "one entry per switch arm", against **135 real `case` arms** across `dispatch*.go`; it also advertises `GetRemoteAgentFile`/`GetRemoteAgentDir`, which have no arm |

Generating the list from the router slice (`dispatch.go:441`) removes two
drift sources at once.

### B3. The stream plumbing: one schema, four mappers

| thing | copies |
|---|---|
| the OpenAI SSE wire schema | `internal/backend/gateway_stream.go:19-45` and `internal/piagentbridge/gatewayprovider_stream.go:20-64` |
| usage → `ChunkMeta` | `piagentbridge/stream.go:415`, `piagentbridge/gatewayprovider_stream.go:278`, `agent/pig_events.go:168`, `printmode/print_pig.go:334` |
| stop-reason mapping | `piagentbridge/stream.go:406` and `:262` |
| the PiG text-delta extraction | `agent/pig_events.go:105`, `pigrt/hooks.go:61`, `sidequestion/sidequestion.go:210` |
| `resultStamp` (the tool-result timestamp) | defined **four times**: `tools/filesystem_edit.go:16`, `tools/command.go:548`, `tools/background_task.go:62`, `subagenttools/subagenttools.go:239` |

The pair at `piagentbridge/gatewayprovider.go` and
`piagentbridge/backendprovider.go` is the sharp one: AGENTS.md
invariant #7 demands they stay **behaviourally identical**, and today
they are two independent parsers of the same wire — a rule enforced by
convention rather than by code.

### B4. `subagenttools.Invoke` is a third turn shape that drops two things

| | error recorded out of band | events flushed |
|---|---|---|
| `internal/agent` (`Loop.Send`) | ✓ `errorSlot` | ✓ `drainEvents` |
| `internal/pigrt` (`Runtime.RunWith`) | ✓ `ErrorSlot` | ✓ `session.FlushEvents` |
| `internal/subagenttools` (`Invoke`) | ✗ | ✗ |

Swapping `subagenttools.Invoke` onto `pigrt.Runtime` buys both with one
change.

---

## 4. Class C — ceremony

| | |
|---|---|
| `internal/cliapp/run.go:275-320` | 18 one-line aliases of `cliflags` / `modelcatalog` (`formatModelLabel` → `modelcatalog.FormatModelLabel`, …) |
| `internal/cliapp/run.go:293-301` | a local copy of `modelcatalog.BaseModelSpec` and its effort table |
| `internal/cliapp/tui_help.go:50-63` | four one-line pass-throughs to `internal/tuilib` |
| `internal/cliapp/browser_flags.go:36-62` | a wrapper that copies four values into a global |
| `internal/models/provider.go:46-77` | `googleProvider` implements the `Provider` interface with every method returning "not implemented", and `NewProvider` still routes `"google"` to it |
| `internal/cli/subcommand_parity.go:161` `legacySignedIn` | re-hardcodes the three token paths that `internal/auth/has_token.go:11` already owns — and ignores `ANTIGRAVITY_HOME` |
| `internal/cli/provider_actions.go:57` and `model_actions.go:106` | every flag registered twice (dash and underscore spellings), then OR-ed by hand — a loop over one table halves both files |

---

## 5. Class D — layering

### D1. `internal/cli` ↔ `internal/cliapp` is a cycle broken by six bridge files

`internal/cli` cannot import `internal/cliapp`, so the bridge runs the
other way: `internal/cli/agents_bridge.go:5`,
`foreground_bridge.go:41`, `serve_bridge.go:86`, `logo_bridge.go:11`,
`web_bridge.go:46`, `dispatch_interactive.go:65` — six files of function
variables set at process start (`internal/cliapp/run.go:101-139`). The
same trick appears in `internal/printmode` (six hooks),
`internal/rchost` (`BackendPicker`), `internal/daemon`
(`Global()`/`SetGlobal()`) and `internal/auth` (four test seams): **~18
mutable globals**, all written at start-up and read from anywhere —
order-dependent and untestable in parallel.

### D2. `internal/remotecontrol` is a god package by size

36,378 lines in 137 files: the JSON-RPC engine, the IDE's
`LanguageServerService` handlers, eight stores, two terminal registries,
the trajectory, the queue, the Mendel flags, the Unix daemon, the
reference surface, the host wiring. It has only three importers, so it
is a god *implementation*, not a god dependency — but a feature-sized
package inside a package.

### D3. Two config structs and three builders — and one of them drops fields

`remotecontrol.HostConfig` (`internal/remotecontrol/host.go:31`, ~32
fields) and `remotecontrol.Config`
(`internal/remotecontrol/daemon_config.go:33`, ~35 fields) exist only to
be translated into each other, and the translation exists twice:

- `internal/remotecontrol/host_helpers.go:97` `hostConfigToDispatcherConfig` — carries every field;
- `internal/rchost/remote_control.go:125` `buildHostDispatcherConfig` — carries fewer.

**Verified first-hand: the one drop that is a live bug.** The second
translation is used by exactly one call site,
`internal/rchost/remote_control.go:60` (`MaybeStartTCPDaemon`, the
`--remote-control-tcp=<addr>` path). It drops `WebTerminals` and
`WebAudio` — the two registries the Web UI's own terminal and voice
surfaces need (AGENTS.md invariant #29). The consequence is
observable: `internal/remotecontrol/ls_terminals.go:122` reads
`d.cfg.WebTerminals` and, when it is nil, answers `CreateTerminal` /
`ListTerminals` with `-32603 "Internal error"` / `"terminals are not
wired"`; `internal/remotecontrol/ls_audio.go:74` does the same for the
voice stream, where nil means the call silently does nothing.

So **the TCP remote-control daemon comes up with the Web UI's terminal
and voice surfaces dead**, while the Unix-socket path
(`StartHostSession` → `hostConfigToDispatcherConfig`) has both. The
other dropped fields (`Commit`, `BuildLabel`, the `*Path` fields) are
zero at that call site — `buildHostConfig` never sets them — so
dropping them is harmless *there*; the shape is still a trap for the
next caller.

On top of that, three builders re-type the store wiring by hand, each
with a *different* field set (verified):

| builder | file | misses |
|---|---|---|
| `buildHostConfig` | `rchost/remote_control.go:92` | — (the host config itself) |
| `buildHostDispatcherConfig` | `rchost/remote_control.go:125` | **`WebTerminals`, `WebAudio`** (live bug) |
| `buildForegroundDispatcher` | `rchost/remote_control_foreground.go:111` | `PermissionsStore`, `PluginsStore`, `TriggersStore`, `FastpushStore`, `AccountEmail`, `RemoteControlInstanceID` |
| `buildServeDispatcher` | `rchost/remote_control_serve.go:169` | — (the fullest of the four) |

**This is the one finding that is a bug, not just duplication.** The
foreground daemon's permission, plugin, trigger and fastpush stores
come up nil, and it reports no account and no instance id.

---

## 6. The recommended order

> **Executed.** Cycle 347
> ([deliverable](../cycle-347-refactoring-implementation/deliverable.md))
> ran this list: items 1–5 and 8 landed (D3, A3, A8, A1, A5, C1), items
> 6, 7, 9 and 10 were deferred with reasons. The estimates below were
> written before the work; three of them were wrong (A1 was −2,011 lines,
> not −1,486; A5 was +46, not −150; A7's risk was higher than "low"), and
> the newer report has the measurements.

Each item is a delete-or-delegate; none of them is a rewrite.

| order | item | payoff | why first |
|---|---|---|---|
| 1 | **D3 (the dropped `WebTerminals`/`WebAudio` on the TCP path)** | a live bug | two lines of fix; it restores the Web UI's terminal and voice on `--remote-control-tcp` |
| 2 | A3 (the printmode/toolrunner twin) | −90 lines | mechanical, byte-identical, one import |
| 3 | A8 (the dead alias API, the parked symbol, the import anchors) | −60 lines | mechanical |
| 4 | A1 (the second tool registry) | −1,486 lines | dead code, but check `harnesstools` first |
| 5 | A5 + A6 (hoist the auto-continue vocabulary and the error slot) | −200 lines | mechanical, and it removes a drift source |
| 6 | A7 (the two test-only lifecycles) | −904 lines | test-only, but the tests must be re-pointed |
| 7 | B3's `resultStamp` (four definitions → one) + B3's usage mapper | −60 lines | mechanical |
| 8 | C1 (the alias wrappers) | −120 lines | mechanical |
| 9 | B4 (`subagenttools` → `pigrt.Runtime`) | one bug class closed | touches the turn loop |
| 10 | B3's two SSE parsers → one | a real invariant enforced by code | wire-level risk |

Everything in §2.2 (A2), §3.1–3.2 (B1, B2), §5.3 (D3) belongs to the
other agent's area and should be handed over, not started in parallel.

---

## 7. 硬约束 (hard constraints)

- **The reference's behaviour is the spec.** A simplification that changes
  what the user sees — a help text, an error string, a flag's default —
  is not a simplification. Every candidate above is either dead code or
  a duplicate whose two copies are *already* identical.
- **The generated protobuf packages stay.** `internal/localharness/pb/*`
  (21,863 + 10,970 lines) is generated code and is not a design
  candidate.
- **`internal/localharness`, `internal/lifecyclehooks` and
  `internal/budget` stay standalone.** They are deliberately not wired
  into the turn loop (AGENTS.md, *the clone in one map*); "unused" is
  their designed state, not debt.
- **The remote-control family belongs to the other agent.** Findings A2,
  A4, A9, B1, B2 and D3 are handover items.
- **A second agent shares the tree.** Every candidate must be staged by
  path, never with `git add -A`, and the tree re-checked before the
  conclusion "it is gone".

## 8. 存疑 (open questions)

- **The size of `internal/remotecontrol` may be a plan, not an accident.**
  36,378 lines in one package is unusual for a clone, but the package
  mirrors a reference binary that *is* that large, and the IDE's
  `LanguageServerService` has ~135 methods. Splitting it would trade one
  kind of complexity for another; the survey only claims the
  *duplicates inside it* are worth deleting.
- **`internal/harnesstools` (2,489 lines) is a fourth tool surface**,
  parallel to `internal/tools`. Whether A1's stubs are dead or merely
  *not yet wired* is not established — the comment in `tools.go:33`
  ("superseded in v4 by status/diff/branch/etc.") suggests history
  rather than a plan.
- **The double OAuth-scope hint does not happen.** Two audits proposed
  it; the code shows two different paths (the headless classifier and
  the TUI's renderer), each printing the hint once. The drift risk is
  real; the visible bug is not.
- **The `--help` byte-identity argument for A4 was not tested.** It is
  plausible that a `flag.FlagSet` prints the same usage string with and
  without a destination — it does — but the golden file has not been
  re-checked against that claim.
- **The god-package claim is about size, not coupling.** A package can
  be huge and still be a fine citizen; `internal/remotecontrol` has
  three importers, so nothing depends on its internals.

## 9. Limitations

- The survey is **read-only**: nothing was refactored, and no candidate
  was proven to compile-and-pass after the change. The "payoff" numbers
  are line counts of what *would* be deleted, not measured diffs.
- Three of the four areas were audited by subagents with the same
  evidence discipline, but the **first-hand verification** was done by
  the author only for the claims quoted above (A1, A3, A4's field count,
  A5, A8, B1's store-helper list, B3's `resultStamp`, and D3's dropped
  registries and their nil-handling). The remaining line numbers are
  the auditors' and are cited as such.
- The audits did not cover: `internal/mdrender` (the chroma rendering
  layer), `internal/vim` (2,667 lines), `internal/auth` (4,047), the
  `internal/remotecontrol` store *implementations* (only their write
  helpers were compared), and the `out/` evidence directory.
- The import graph was taken with `go list` on the current tree, which
  includes the other agent's in-flight packages; the fan-in numbers
  move while they work.
- "Dead" means *unreachable from a product path in this tree today*. A
  package kept for a later cycle (AGENTS.md names `localharness`,
  `lifecyclehooks` and `budget` as such) is dead by that definition and
  is explicitly **not** in scope.

## Files

| | |
|---|---|
| `docs/cycles/cycle-346-refactoring-survey/deliverable.md` | **new** — this survey |
| `internal/tools/{stubs,stubs_git,stubs_search,stubs_tasks,git}.go` | A1 — the second registry (candidate for deletion) |
| `internal/printmode/main_print_tools.go` | A3 — the twin of `internal/toolrunner/runner.go` |
| `internal/agent/auto_continue.go`, `internal/pigrt/auto_continue.go` | A5 — the duplicated vocabulary |
| `internal/agent/pig.go`, `internal/pigrt/errors.go` | A6 — the duplicated error slot |
| `internal/backend/lifecycle*.go`, `internal/cortex/lifecycle.go` | A7 — the test-only state machines |
| `internal/cli/parser.go`, `internal/cli/serve_bridge.go` | A8 — the dead API and the parked symbol |
| `internal/lsserver/flags.go`, `internal/lsserver/args.go` | A4 — 69 fields, 50 unread (theirs) |
| `internal/remotetransport/unix.go` | A2 — the dead twin (theirs) |
| `internal/remotecontrol/{host_helpers.go,reference_surface.go,top_level.go,*_store_fs.go}` | B1, B2, D3 (theirs) |
| `internal/piagentbridge/{stream.go,gatewayprovider_stream.go}` | B3 — the second SSE parser |
| `internal/cliapp/{run.go,tui_help.go,browser_flags.go}` | C1 — the alias wrappers |
