# Cycle 347 deliverable: the survey, executed — six simplifications, two live bugs closed

**Cycle:** `cycle-347-refactoring-implementation`
**Subject:** "得出结论之后你挑最有收益的去实现" — implement the highest-value
items of [cycle 346](../cycle-346-refactoring-survey/deliverable.md).
**Method:** the survey's ranked list, re-verified item by item against the
tree before each edit, one commit per item, each commit gated on
`go build ./cmd/... ./internal/...` and `go test ./cmd/... ./internal/...`.

---

## 0. The short answer

Six of the survey's items landed, in six commits, **−2,037 lines net**, and
two of them closed a **live bug** rather than removing a line.

| # | item | what changed | lines | commit |
|---|---|---|---|---|
| A1 | `internal/tools`' second registry | `RegisterAll` + the `stubs*`/`git.go` files are gone; the catalogue is the one registry | **−2,011** | `10b10f51` |
| A3 | the printmode/toolrunner twin | print mode reads `internal/toolrunner` | −81 | `5baec589` |
| A8 | the dead alias API | `AliasMap`/`LookupFlagValueCmd`/`StandardAliasMap`, the parked sentinel, the test seam, the import anchors | −20 | `848a03dc` |
| **D3** | **two HostConfig→Config translators, each dropping fields** | **one translator; the browser bridge, the Web UI's terminals and the Web UI's voice now reach the dispatcher** | +76 | `94802bf0` |
| C1 | the alias wrappers | eighteen one-line wrappers deleted; the call sites name the package | −47 | `bcad19e1` |
| A5 | the auto-continue vocabulary | one declaration of the reference's own bytes; one parser | +46 | `ad0fb81a` |

**What the lines are not.** A1 is the bulk of the win and it is *dead code
removal*: the tools it deleted were reachable from exactly one place — a
test. The two D3 drops were *live*: the browser bridge answered nothing on
the Unix-socket daemon, and the Web UI's terminal and voice surfaces
answered `-32603` on the TCP daemon.

**One behaviour changed**, and it was a bug: the print path's
iteration-count parser concatenated every digit in the error message, so
`budget (8) after 3 tries` read as `83` (see §3).

---

## 1. What landed, item by item

### 1.1 A1 — one tool registry (`10b10f51`, −2,011)

`internal/tools` carried a second registry beside the live one:
`coreTools` + `stubTools` + `RegisterAll` built 42 entries, and the only
caller in the whole tree was `TestRegisterAll_32PlusAlias`.

The verification that made the deletion safe was not "grep found no
caller" — it was the shape of the catalogue itself:

```
internal/tools/catalog.go:342  func Catalog(ws string) *Registry
```

`Catalog` builds the registry the product path uses, from
`referenceSpecs` — 60 names, 52 with a real executor, 8 `pendingTool`s
for the names the reference advertises and the clone has not implemented.
The `stubs*` files were a **parallel copy** of that executor set
(a second `Finish`, a second `Wait`, a second `SendMessage`, a second
`GrepSearch`…), plus 9 `git_*` and 28 v4 stub tools the reference's
1.4.7 surface does not advertise at all.

| removed | replaced by |
|---|---|
| `stubs.go` (`Finish`, `Schedule`, `Cron`) | — (the catalogue has its own `FinishTool`, `ScheduleTool`) |
| `stubs_git.go` (11 v4 git ops) | — (none of the names is in the 1.4.7 catalogue) |
| `git.go` (9 `git_*` tools, the 1.3.3 surface) | — (same) |
| `stubs_search.go` (`GrepSearch`, `Grep`, `WebSearch`, `Codesearch`, `FileSearch`, `SkillSearch`) | — (the catalogue has `GrepSearchTool`; the rest are unadvertised) |
| `stubs_tasks.go` (`Wait`, `CreateTask`, `SendMessage`, `NotifyUser`, `TaskBoundary`, `Unzip`) | — (the catalogue has `WaitTool`, `SendMessageTool`, `ManageTask`) |
| `tools.go`'s `coreTools`/`stubTools`/`RegisterAll` | `internal/tools/catalog.go`'s `Catalog` |

Three things stayed, because the catalogue or a product path points at
them:

- `SearchWeb` and `ReadURLContent` → **`internal/tools/web_content.go`**,
  with the HTML helpers they share;
- `GenerateImage` → **`internal/tools/image_generate.go`**;
- the package-level task registry and subagent manager (the `/tasks`
  panel and the status line read them) → **`internal/tools/shared_state.go`**.

The tests went with the code they covered: `stubs_test.go` kept only the
`read_url_content` case (now `web_content_test.go`), `stubs_bg_name_test.go`
kept only the `subagent.SetTitle` nil-receiver case (now
`internal/subagent/title_test.go`), and `tools_test.go`'s schema sweep now
walks `Catalog("/tmp")` — the registry the product actually uses — instead
of the deleted one.

### 1.2 A3 — the printmode/toolrunner twin (`5baec589`, −81)

`internal/printmode/main_print_tools.go` was a function-for-function copy
of `internal/toolrunner/runner.go`: `toolDeclsFromRegistry`,
`advertisedToolDecls`, `registryRunner`, `permissionRunner`,
`sandboxDeniedToolResult`, `toolTarget`. The TUI (`internal/cliapp`) and
the harness (`cmd/harness`) already read the shared copy; print mode now
does too.

The one seam left is a one-line `toolTarget` delegate, because the
print path's hook lives in `internal/printmode/print_pig.go` — the other
agent's in-flight file. The delegate keeps that file untouched, and it
follows the codebase's own convention (`cmd/harness` delegates the same
way).

### 1.3 A8 — the dead alias API (`848a03dc`, −20)

- `AliasMap`, `LookupFlagValueCmd` and `StandardAliasMap` — "kept for
  source compatibility with cycle 33-37 callers"; `cmd/wagyu` is gone and
  the only reader was the test that asserted the table's own contents.
  **The knowledge stayed, and moved with the assertion**: the reference's
  `-c` / `-i` / `-p` short forms are registered as real flags in the spec
  table (`internal/cli/parser_flags.go:35,50,68`), and the `--prompt →
  --print` fold is the parser's own (`firstSetFlag`, `jetski.go:193`).
  `TestAliasMatching` now pins those, not a copy of them.
- `errServeHandlerNotRegistered` — parked "for a future cycle".
- `EnsureServeHandlerRegistered` — a **test seam in production code**; it
  is a test concern, so it moved into `internal/cli/rc_serve_test.go`.
- The two import anchors in `parser.go` and the imports they existed to
  keep.

### 1.4 D3 — one HostConfig→Config translation (`94802bf0`, +76)

The survey's headline finding: **two** translations of
`remotecontrol.HostConfig` into `remotecontrol.Config`, and each dropped
something the other kept.

| translator | used by | dropped |
|---|---|---|
| `hostConfigToDispatcherConfig` (`internal/remotecontrol/host_helpers.go`) | the Unix-socket host (`StartHostSession`) | `Browser` |
| `buildHostDispatcherConfig` (`internal/rchost/remote_control.go`) | the TCP host (`--remote-control-tcp`) | `WebTerminals`, `WebAudio` |

Both were live bugs:

- `Config.Browser` is the agent↔browser bridge seam — the three
  `agent-browser` RPCs read `d.cfg.Browser` and return early when it is
  nil (`internal/remotecontrol/dispatch_agentbrowser.go:55`). On the Unix
  socket the broker was **always** nil, so the IDE's browser bridge
  answered nothing.
- `WebTerminals`/`WebAudio` are the Web UI's terminal and voice surfaces.
  On the TCP path the dispatcher had neither, so `CreateTerminal` answered
  `-32603 "terminals are not wired"` and the voice stream had no session.

There is now **one** translation, `remotecontrol.ConfigFromHost(hc, sock)`,
exported from `internal/remotecontrol` (the socket is a parameter because
the Unix host binds one and the TCP host does not). It carries every
field the two structs share — 30 of `HostConfig`'s 35 fields have a
`Config` counterpart, and all 30 are carried; the other five
(`Version`, `Backend`, `Model`, `Workspace`, `Out`) are host-only
conveniences with no counterpart. `internal/remotecontrol/config_from_host_test.go`
pins the fields that were lost.

### 1.5 C1 — the alias wrappers (`bcad19e1`, −47)

`internal/cliapp` carried eighteen one-line wrappers over
`internal/cliflags`, `internal/modelcatalog` and `internal/tuilib`:
`flagBoolValue`, `flagStringValue`, `flagIntValue`, `resolveBoolAlias`,
`formatModelLabel`, `resolveEffectiveModel`, `canonicalModelLabel`,
`wireModelLabel`, `resolveDefaultEffort`, `availableModelsBlock`,
`resolveModelSelection`, `modelSlug` and four `renderHelpGeneral*`
helpers — **three of which had no caller at all**.

Two *copies of data* went with them: `baseModelSpec` (a private mirror of
`modelcatalog.BaseModelSpec`) and `baseModelEfforts` (a map nobody read —
`modelcatalog` holds the catalogue). The four default-effort chain cases
moved to `internal/modelcatalog/default_effort_test.go`, next to the code
they cover; they had been driving the local wrapper.

### 1.6 A5 — one declaration of the recovery vocabulary (`ad0fb81a`, +46)

The TUI's turn engine (`internal/agent`) and the print path's
(`internal/pigrt`) each declared their own copy of the auto-continue
vocabulary — the verbatim AGY stream-interrupt copy (loadfile
`0x33dc41b`), the verbatim `**Status**` banner (`0x33a3d44`), the depth
ceilings — and each carried its own iteration-count parser.

The agent's header claimed *"a TUI/print divergence is impossible by
construction"*. Two declarations are not a construction; they are two
chances to drift, and the strings have to be byte-exact against the
binary. `internal/autocont` now holds the vocabulary; both engines alias
it, so the TUI's exported names (`AutoContStreamInterrupted`, …) still
read the same and no caller moved.

---

## 2. The one behaviour change

The two parsers were not the same parser.

| | rule | `"budget (8) after 3 tries"` | `"exceeded at 21 iterations"` |
|---|---|---|---|
| `internal/agent`'s `turnsFromErr` | the **largest** digit run | 8 | 21 |
| `internal/pigrt`'s `iterationCountFromErr` | **every** digit, concatenated | **83** | 21 |

One parser now (`autocont.IterationCount`, the largest digit run). The
print path's recovery banner can therefore name a different number than it
did before — but only in the case where it was naming the wrong one. Both
engines' existing test cases pass unchanged; the merged case table adds
the two-number shape.

---

## 3. What was deliberately not done

| item | why not |
|---|---|
| **A6** (`errorSlot` / `recordingProvider` exist twice) | the two copies are **not** the same: the agent's `set` keeps the **first** error, the print path's keeps the **last**. Merging them changes one path's behaviour, and the net line count is ~0 (a shared package costs what the duplicate cost). Deferred as a behaviour decision, not a simplification. |
| **A7** (two lifecycle state machines, −904 lines) | both are reachable only from tests, but they are **recovered artefacts**: `internal/backend/lifecycle.go` documents the reference's own lifecycle by address (`0x8754d20`, `0x875c020`, …). Deleting them is a research-value judgement, not a cleanup, and it would pull `internal/cascade` with it. Deferred to a cycle that decides what the repo does with recovered-but-unreachable reconstructions. |
| **A2, A4, A9, B1, B2** | the other agent's area (`internal/remotetransport`, `internal/lsserver`, `internal/browserbridge`, `internal/remotecontrol`). Handed over, not started in parallel. |
| **B3** (`resultStamp` ×4 → 1; the usage→`ChunkMeta` mapper ×4 → 1) | re-measured: `resultStamp` is now defined **twice** (4 lines each — `internal/tools/filesystem_edit.go`, `internal/subagenttools/subagenttools.go`), not four times; the two copies live in packages that do not import each other, so sharing them means a new package for 4 lines. The usage mapper is the bigger half, but it sits across the provider implementations the survey itself calls out as "must stay behaviourally identical" (AGENTS.md invariant 7) — that is a behaviour-risk item, not a mechanical one. Deferred. |
| **B4** (`subagenttools.Invoke` → `pigrt.Runtime`) and the SSE-parser merge | both touch the turn loop and the wire; they are bug-class work, not simplification work, and the other agent is in `internal/subagenttools` right now. |

---

## 4. The survey's numbers, corrected

The survey estimated; the tree disagreed twice.

| item | survey said | actual | why |
|---|---|---|---|
| A1 | −1,486 lines | **−2,011** | the survey counted the *files*; the *tests* for the deleted tools went with them (another ~800 lines). |
| A5 | −150 lines | **+46** | the two files are 629 lines, but most of that is the two engines' own machinery (the loop, the classifier, the transcript handling). The duplicated **vocabulary** is four constants and one parser. |
| A5's risk | "the two digit parsers differ" | confirmed, and it is a **bug** in the print path | see §2. |
| C1 | −120 lines | −47 | eighteen wrappers are ~50 lines; the rest of the survey's estimate counted the *data copies* and the moved tests, which are a relocation rather than a deletion. |
| A7 | "low risk" | **not low** | the survey read them as ordinary dead code; they are recovered artefacts carrying the reference's addresses. |

---

## 5. 硬约束 (hard constraints)

- **The reference's behaviour is the spec.** Every deletion above was
  either unreachable code or a duplicate whose two copies were already
  identical — with one exception, the iteration-count parser, where the
  duplicate was *not* identical and the fix changes what the print path
  prints. That one is recorded here, in §2, and pinned by a test.
- **The advertised surface did not move.** A1 deleted the *second*
  registry; the catalogue's 60 names and their backends are untouched
  (`internal/tools/catalog.go`).
- **`internal/tools`'s shared task registry stays** — the `/tasks` panel
  and the status line read it (`internal/cliapp/tui_runmode_agent.go:289`,
  `internal/printmode/rehydrate.go:43`).
- **The remote-control family is the other agent's area.** D3 was
  implemented as a *merge* (one exported translator both call sites use)
  rather than as two independent edits, precisely so the two sides agree
  by construction.
- **A second agent shares the tree.** Every commit was staged by path;
  `internal/printmode/print_pig.go` was left untouched (A3 keeps a
  one-line delegate for it), and `git status` was re-checked after each
  commit.

---

## 6. 存疑 (open questions)

- **The `/tasks` panel has no producer on the live path.** The shared
  registry's only writers were the deleted `create_task`/`send_message`
  stubs — so the panel was already empty before this cycle (the stubs
  were unreachable), and now it is empty *by construction*. The live
  1.4.7 catalogue registers `define_subagent` / `invoke_subagent` /
  `manage_subagents` as `pendingTool`s, and `internal/subagenttools` keeps
  its own store. Whoever wires the subagent tools into the catalogue has
  to decide whether the panel reads that store or this one. **This is a
  handover, not a fix.**
- **The removed v4/1.3.3 tool implementations are recovered artefacts.**
  They implemented names the 1.4.7 reference does not advertise
  (`git_status`, `codesearch`, `skill_search`, the v4 git ops). The code
  is in the git history (`10b10f51^`) and the *names* are in the
  catalogue's own history; but if a future cycle wants to offer the
  withheld names (`code_search`, `moma_search`, `skill_search` are
  withheld from `init.tools` by the reference itself), it will be writing
  new implementations, not resurrecting these.
- **D3's fix changes the Unix path's behaviour** (the browser bridge is
  now wired there). That is the intended behaviour, but the bridge's
  first live Unix-socket exercise is untested end to end.

---

## 7. Limitations

- **Everything here was verified by the compiler and the test suite, not
  by running the reference.** No `agy` capture was re-taken; the parity
  claims rest on the catalogue and the recovered constants the tree
  already pins.
- **`TestChangelog_MatchesReferenceBinary` fails in this environment** —
  the installed `~/.local/bin/agy` now emits a 177,232-byte changelog
  against the embedded 174,284-byte blob. Confirmed pre-existing at HEAD
  in a clean worktree; it is reference drift, not a regression from this
  cycle, and it belongs to `internal/referencedrift`'s territory.
- **The line counts are diff statistics**, not a measure of complexity.
  A1 is the only item where the deletion is large *and* the behaviour is
  provably unchanged.
- **A7, B3 and B4 were not costed**, only deferred; the survey's
  estimates for them are unverified.

---

## Files

| file | what |
|---|---|
| `internal/tools/{shared_state.go,web_content.go,image_generate.go}` | the survivors of the deleted stub files |
| `internal/tools/web_content_test.go`, `internal/subagent/title_test.go` | the tests that came with the code they covered |
| `internal/tools/tools.go` | the one registry: `Tool`, `Registry` |
| `internal/toolrunner/runner.go` | the canonical tool runner (unchanged; it was already canonical) |
| `internal/printmode/{main_print.go,main_print_tools.go}` | print mode reads the shared runner |
| `internal/cli/{parser.go,parser_test.go,serve_bridge.go,rc_serve_test.go}` | the dead alias API and the parked sentinel |
| `internal/remotecontrol/{host_helpers.go,host.go,config_from_host_test.go}` | the one HostConfig→Config translation |
| `internal/rchost/remote_control.go` | the TCP host uses it |
| `internal/cliapp/*`, `internal/modelcatalog/default_effort_test.go` | the alias wrappers and the tests that moved |
| `internal/autocont/{autocont.go,autocont_test.go}` | the recovery vocabulary |
| `internal/agent/auto_continue.go`, `internal/pigrt/auto_continue.go` | the two engines, aliasing it |
