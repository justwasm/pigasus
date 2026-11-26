# Scenario-First Modularization Roadmap

> Status: execution plan and acceptance contract. The stages are intentionally incremental: each module extraction must leave the repository buildable and testable. A smaller file is not, by itself, a completed modularization.

## Goal

Turn oversized packages and host structs into cohesive modules that can be constructed, driven, and verified independently. Use the TUI Storybook's strongest ideas beyond visual components: **named, reproducible scenarios; explicit state transitions; observable outputs; deterministic fixtures; and a thin host that assembles modules rather than owning their rules**.

The target is not maximum package count. The target is an explicit ownership boundary that makes invalid dependencies hard to express and makes a representative behavior runnable without booting the whole application.

## Storybook-derived engineering contract

Every extracted module should provide, as appropriate:

1. **Identity** — a named capability or scenario with a short statement of what it demonstrates.
2. **Construction** — a small constructor or pure function with dependencies passed explicitly; tests must not need the production host, network, real clock, subprocess, PTY, or global mutable state.
3. **Stimulus** — explicit input/events in a stable order (request, key event, stream frame, cancellation, etc.).
4. **Observation** — externally meaningful output: rendered grid/text, response/event sequence, state transition, or typed error. Assertions must check this, not only internal fields.
5. **Determinism** — fixed clock, in-memory transport/provider, fixture bytes, explicit dimensions/configuration, and no ambient user shell/profile dependencies.
6. **Composition** — production host owns lifecycle and wiring. It adapts host facilities to narrow ports; it does not duplicate domain rules.
7. **Golden use with restraint** — use golden fixtures for stable wire/render output; use semantic assertions for behavior where formatting is incidental. Update snapshots only for reviewed intentional changes.

Do not build a universal scenario framework up front. Keep scenario catalogs local to a domain until two or more domains demonstrate a real shared need. Avoid fake modularity: same-package files, wrapper packages, aliases, and one-function forwarding shells do not satisfy this contract.

## Baseline inventory (2026-11-25)

Counts are repository measurements of non-test, non-generated Go source; they help order work, not define quality by themselves.

| Package | Production LOC | Test LOC | Current pressure | Planned direction |
|---|---:|---:|---|---|
| `internal/tui` | 25,559 | 49,718 | `RootModel` host owns many cross-domain interactions; file splitting already improved navigation but not package boundary | Extract one vertical slice at a time behind narrow ports; retain RootModel as composition root |
| `internal/remotecontrol` | 22,013 | 30,162 | `Dispatcher` routes a broad RPC surface across domains | Domain-owned handlers and explicit dispatch environment; protocol contract scenarios without starting the server |
| `internal/cli` | 11,355 | 9,795 | plugin validation, flags, lifecycle, provider setup coexist | Move domain validation into Tier 2; keep CLI parsing/formatting/exit handling at the host edge |
| `internal/ui/storybook` | 9,115 | 998 | Good scenario catalog; one large story-definition file | Keep catalog and visual fixtures deterministic; split by component domain only when ownership improves |
| `internal/infra/agyproxy` | 8,364 | 6,567 | protocol mapping mixed with HTTP/retry/stream orchestration | Pure codec package and fixture-driven round-trip/error scenarios; I/O remains in infra host |
| `internal/app/agentturn` | 8,204 | 6,101 | ACP/AHP and remote agent protocols share runtime package | Protocol state machines behind transport ports; preserve turn-engine lifecycle at app layer |
| `internal/voice` | 7,347 | 3,838 | Native session and bridge responsibilities concentrated | Separate transport/session protocol from turn orchestration; deterministic fake-session scenarios |
| `internal/app/session/convstore` | 7,164 | 5,288 | Multiple persistence formats and migrations | Format codecs + shared domain model; golden fixtures per version, filesystem boundary tests |
| `internal/app/agentserver` | 5,540 | 5,526 | AHP/ACP handlers and host/transport lifecycle intermixed | In-memory protocol scenarios; explicit host effects and cancellation boundaries |
| `internal/ui/livechat` | 5,444 | 3,391 | `tui_model` and sparse tests | Split by behavior, add story-style model/handler scenarios with fake actor/provider |
| `internal/ui/mdrender` | 5,149 | 5,009 | Rendering and formatting concerns | Markdown pipeline stage fixtures and output scenarios; avoid host-level test setup |
| `internal/cli/plugin_validate.go` | 2,129 | — | Domain validation currently tied to CLI | First domain extraction candidate: `internal/features/plugins/validate` |
| `internal/infra/agyproxy/bridge.go` | 1,606 | — | Large protocol bridge | Extract only pure conversions first; maintain transport and typed errors in infra |

Generated protobuf and compatibility patch registries are intentionally not first-wave decomposition targets: generated code is regenerated, while patch registries are wire-sensitive and need evidence/ownership analysis before moving.

## Execution plan

### Phase 0 — Establish a trustworthy baseline

- Preserve and explicitly exclude unrelated user changes from every patch and commit.
- Record package/file size, tests, imports, and existing contracts before touching a module.
- Identify public and compatibility-sensitive entry points. Read the subsystem guide and architecture tier before moving code.
- Run the package tests and architecture guard before the change; capture environment-dependent failures separately from code regressions.

**Exit gate:** a reproducible baseline and a bounded change list; no unrelated files staged.

### Phase 1 — Make the scenario contract concrete

- Extend the Storybook documentation from UI rendering to scenario-driven module validation.
- For each pilot, define named cases: valid behavior, boundary/invalid input, and lifecycle/cancellation where relevant.
- Prefer a pure domain API and table-driven named subtests; use a tiny local fake port rather than a generic cross-project test framework.
- Check observable results and typed errors. For protocol codecs, pin fixture bytes/JSON; for UI, inspect rendered content/character grids and key effects.

**Exit gate:** pilot scenario tests run without a network, TUI host, external process, or nondeterministic wall clock.

### Phase 2 — Move domain ownership out of CLI

- Extract manifest/capability/path validation from `internal/cli` into `internal/features/plugins/validate` (Tier 2).
- Keep CLI responsible for argument parsing, file selection, user-facing diagnostics, and exit codes.
- Replace direct CLI tests of internal validation branches with validator package scenarios; retain a small CLI integration test proving message/exit behavior.

**Exit gate:** validator package can be tested directly; no upward dependency; CLI tests prove the host adapter still maps inputs/errors correctly.

### Phase 3 — Separate proxy codecs from transport

- Extract pure OpenAI/Anthropic/CloudCode protocol conversions from `internal/infra/agyproxy` into a cohesive codec owner.
- Leave HTTP, auth, retry, streaming lifecycle, cancellation, and metrics in the infra server.
- Add deterministic request/response fixture scenarios, including malformed payloads, tool calls, and streaming edge boundaries.

**Exit gate:** codec tests require no HTTP server or credentials; server tests cover only composition/transport behavior; no behavior is hidden behind a forwarding facade.

### Phase 4 — Modularize TUI by vertical behavior slices

- Keep `RootModel` as the Bubble Tea host/composition root; do not split only by file or create one new package per source file.
- Select a cohesive slice (e.g. vim editing, voice handoff, attachment selection) with state ownership, effects, and tests that can move together.
- Design a narrow port for host-only effects (clipboard, session update, tool dispatch, focus/overlay). The module must not receive all of `RootModel` or an untyped callback bag.
- Add scenario fixtures for initial state, key/event sequence, resulting view/grid, emitted command, and cancellation/error path.
- Migrate only after the standalone behavior has parity; retain a thin RootModel adapter and a small integration test.

**Exit gate:** module test constructs without `RootModel`; host method delegates rather than reimplements; rendered/behavioral tests demonstrate parity.

### Phase 5 — Bound protocol handlers and persistence domains

- `internal/remotecontrol`: map RPC methods to domain handlers; inject narrow `DispatchEnv` ports; test routing/permission/error scenarios in memory.
- `internal/app/agentserver` and `internal/app/agentturn`: separate protocol state machines from process/PTY/network adapters; use in-memory transports to exercise initialize, request, permission, cancellation, and shutdown scenarios.
- `internal/app/session/convstore`: make per-format decoding/migration fixtures independently testable before relocating packages.

**Exit gate:** each domain has independent scenario tests; end-to-end tests remain few and prove only cross-boundary composition.

### Phase 6 — Add structural ratchets

- Track production package sizes and largest files in hygiene output, but make only reviewed, package-specific ceilings blocking.
- After each extraction, ratchet the affected legacy package's LOC ceiling downward; never increase the ceiling to silence a regression.
- Add architecture tests for forbidden dependencies at the extracted boundary; use AST/import analysis, not brittle string scans where practical.
- Maintain scenario coverage inventory with links from subsystem guides. Do not optimize for raw test count.

**Exit gate:** CI rejects dependency regressions and growth in migrated legacy boundaries while unrefactored packages remain reported, not falsely labeled compliant.

### Phase 7 — Validate and deliver each slice

For each slice: `gofmt` edited files → targeted scenario tests → race tests where concurrency is involved → `make arch` → `make hygiene`/`make quick` → relevant build. Run broader tests only when risk warrants it. Inspect the final diff and status, stage only task files, commit with the slice scope, and report known environment-related failures without masking them.

## Prioritization rule

Order work by **change frequency × blast radius × coupling × test cost**, balanced against extraction safety. First wave is plugin validation and proxy codecs because they have crisp pure-domain boundaries; the TUI gets a carefully chosen vertical pilot rather than another broad rewrite. Then move to remotecontrol/agent protocol seams. Generated and compatibility-sensitive files require stronger evidence before extraction.

## Definition of done for a module extraction

- [ ] Responsibility and ownership are stated in its package documentation.
- [ ] Inputs and host effects are separated by a narrow interface or pure function boundary.
- [ ] At least three named scenarios cover normal, boundary/error, and state/lifecycle behavior where applicable.
- [ ] Tests assert observable behavior; no test depends on real user shell config or ambient credentials.
- [ ] Host wiring remains thin and has a composition test.
- [ ] Architecture guard accepts the dependency direction.
- [ ] Package size/complexity is measured; no facade or alias shell was introduced.
- [ ] Relevant package tests, hygiene, architecture, and build pass.
- [ ] The commit contains only this slice and related documentation/tests.

## Delivered slices

Each entry below met the definition of done above unless explicitly noted.

| Slice | Destination | Commit |
|---|---|---|
| PTY test isolation (`terminalmgr`, `agentserver`) | hermetic `HOME`/`ZDOTDIR` per test | `af47a002` |
| Editor selection | `internal/ui/editor/selection` | `c37bf047` |
| RemoteControl file domain | `internal/remotecontrol/files` | `4d21698e` |
| Plugin validation (Tier 2) | `internal/features/plugins/validate` | `abe02efb` |
| Proxy codec boundary (cloak kept in host) | `internal/infra/agyproxy/codec` | `9db70232` |
| Scenario contracts: accountpool / permissions / failover | `scenarios_test.go` per package | `ad2e97b3` |
| TUI slice 1: voice bridge | `internal/ui/voicebridge` | `11e661e6` |
| TUI slice 2: vim bridge | `internal/ui/vimbridge` | `280a308e`, `148c7235` |
| TUI slice 3: paste/media | `internal/ui/pastemedia` | `a7478d10` |
| Structural ratchets (binding/report-only, file count, receivers) | `devutils/hygiene` | `c09be8c5` |
| Shared-prefix Tab completion (gives `slashcomplete` its caller) | `internal/ui/views/slashpicker` | `c593010b` |
| Scenario contract: tiered trajectory store | `internal/app/session/convstore/scenarios_test.go` | `0b84e2f6` |

`internal/tui.RootModel` currently has 804 methods; `internal/remotecontrol.Dispatcher` still has 247. Both remain `report_only`: splitting adapter files does not remove responsibilities from either host. The reviewed TUI baseline is 25,619 physical production lines across 85 files; its increase from the prior baseline is the additional package/import scaffolding from the mechanical split, not a completed domain extraction. Extracted packages, including the proxy codec and slash completion, have binding LOC/file ceilings.

### A–E acceptance status (2026-11-25)

| Track | Delivered evidence | Remaining acceptance work |
|---|---|---|
| A — TUI | Voice, vim and paste/media vertical slices; panel adapters and attachment/media host code organized by domain | Slash/overlay and remaining attachment rules still belong to `RootModel`; same-package file moves alone are not a thin-host completion |
| B — RemoteControl | File-domain handlers and named file scenarios | `Dispatcher` still has 247 methods; permissions/projects/terminals and language-server handlers need real ownership extraction |
| C — Agent protocols | Existing ACP/AHP integration coverage | Protocol state and subprocess/PTY lifecycle are not yet separated behind an in-memory transport seam |
| D — Structural ratchet | Binding caps for extracted packages; advisory caps for large hosts, refreshed to observed measurements | Tighten each host ceiling only after an actual ownership extraction; do not describe advisory gates as blocking |
| E — Named scenarios | Account pool, permissions, failover, file domain, completion and tiered conversation store | Extend to stream/protocol state transitions without introducing ambient network/process dependencies |

## Known leftovers

- `internal/ui/slashcomplete/` — was an unreferenced package until the shared-prefix Tab rule gave it a real caller (`slashpicker.completeSharedPrefix`). It now owns the pure matching rule; its 21 unit tests partially overlap the 6 picker scenarios and could be merged.

## Defects found while pinning scenarios

These were discovered by writing contract tests, not by refactoring. Both have landed fixes; the original evidence and remaining limitations are recorded below.

### `tieredTrajectoryStore` replay identity — fixed in `19d018e9`

`AppendStep` (`internal/app/session/convstore/tiered.go`) appends to the hot slice blindly:

```go
steps = append(steps, step)
```

while the warm tier upserts by `(conv_id, idx)` (`tiered.go` `ON CONFLICT(conv_id, idx) DO UPDATE`). The two tiers therefore disagree about identity. Re-appending an already-evicted index yields two rows for one index in `LoadTrajectory`.

Reproduced: append indices 0..9 with `HotCapacity=3`, then re-append index 0.

```
total steps=11 duplicate indices=1
DUPLICATE idx=0 appears 2 times
```

`19d018e9` deduplicates the assembled trajectory by index, preserving `GetStep` precedence (hot > warm > cold). Named scenarios now assert one row per index and the replayed content. The hot window remains append-only; the fix is the observable read contract, not a rewrite of tier eviction semantics.

### Live conversation-store parity — stabilized in `7d289e12`

`internal/app/session/convstore/parity_test.go` originally drove the real `agy` binary through tmux with fixed sleeps. The change waits for stable pane content and the expected expanded content, reports the last pane on timeout, and serializes live-store mutations within the package. The real `HOME` is retained because the reference binary needs its authentication state. These remain environment-dependent integration tests, not hermetic protocol scenarios; the package lock does not serialize separate test processes or the operator's live CLI.
