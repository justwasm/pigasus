# Cycle 380 — the remote-control surface, split

## The problem

`internal/remotecontrol` was the largest god package in the clone:
**65 561 lines across 202 files**, one flat directory. It held, among
much else:

- the **wire envelope** — the JSON-RPC 2.0 `result` / `error` shapes;
- **twelve stateful domains**, each with its own store, its own wire
  shapes and its own method names — plugins, projects, resources,
  triggers, terminals, permissions, the conversation config, the
  fastpush toggle, the origin policy, the file reader;
- the **cascade domain** — the unit of work a dashboard drives
  end-to-end;
- two **leaf vocabularies** — the reasoning-effort names and the Mendel
  flag answer;
- the **dispatcher** — the fan-out, the language-server table, the
  trajectory, the interaction host, the PiG-backed session.

Every importer named the one package, whatever it wanted from it. A
caller that only wanted the JSON-RPC envelope also imported the
browser dispatcher, the audio sessions, the terminal registry and the
conversation store. The package's dependency set was the union of a
dozen unrelated facts, and there was no way to make it smaller without
splitting it.

The split was blocked on nothing. It was simply the next tranche —
cycle 379 named `internal/remotecontrol` (~65 kLOC) and
`internal/cliapp` (~74 kLOC) as the two remaining god packages, and the
remote-control surface is the smaller of the two.

## What was done

Fifteen commits, in fifteen tranches — one domain per commit, so each
step was reviewable and green on its own.

| commit | what it did |
|---|---|
| `67d7c615` | the envelope: the two canonical JSON-RPC error bodies moved into `remotecontrol/rpc` |
| `e914f5d9` | the fastpush domain |
| `03a55699` | the origin-policy domain |
| `61faf336` | the conversation-config domain |
| `d3147337` | the permissions domain |
| `99d91aae` | the triggers domain |
| `ec93d444` | the files domain |
| `0a997936` | the effort vocabulary — a leaf package |
| `fba6d014` | the projects domain |
| `21ed3447` | the resources domain |
| `349b781c` | the plugins domain |
| `2c7e3bb2` | the terminals domain |
| `1cdd95c6` | the Mendel flag answer — a leaf package |
| `090e2493` | the cascade domain |

Two further commits in the range — `6d3fd1a9` (oauth) and `70540d86`
(models) — belong to the concurrent work in this tree and are **not** part
of this cycle.

### 1. Fourteen packages, no facade

| package | owns | production | test |
|---|---|---|---|
| `internal/remotecontrol` | the **dispatcher** — the fan-out, the language-server table, the trajectory, the interaction host, the PiG-backed session, the Web UI's terminal and voice surfaces | 20 945 | 18 832 |
| `internal/remotecontrol/rpc` | the **wire envelope** — `ErrorBody`, `CodedError`, `Result`, `Error` | 230 | 0 |
| `internal/remotecontrol/cascades` | the **cascade domain** — the `Cascade` value type, the `Registry`, the `Backend` seam, the `StubBackend` fixture, the streaming vocabulary, the pending-interaction registry | 2 572 | 1 877 |
| `internal/remotecontrol/permissions` | the permission preset, the review mode, the grants, the reference's permission vocabulary, the store | 2 073 | 531 |
| `internal/remotecontrol/terminals` | the dashboard's terminal registry: the pty sessions, the lifecycle, the store | 2 055 | 393 |
| `internal/remotecontrol/triggers` | the trigger specs, the options, the store, the handlers | 2 385 | 813 |
| `internal/remotecontrol/resources` | the resource providers, the available resources, the project's resource list | 1 963 | 557 |
| `internal/remotecontrol/projects` | the project list and the four CRUD methods | 1 699 | 464 |
| `internal/remotecontrol/convconfig` | the conversation's configuration | 1 395 | 820 |
| `internal/remotecontrol/fastpush` | the fastpush toggle | 1 370 | 597 |
| `internal/remotecontrol/origins` | the browser-origin patterns | 1 194 | 743 |
| `internal/remotecontrol/plugins` | the installed plugins, the skills, the remote plugins | 1 237 | 632 |
| `internal/remotecontrol/files` | the file and directory reader, the scope rules | 1 094 | 317 |
| `internal/remotecontrol/mendel` | the Mendel flag answer and the per-identity cache | 349 | 298 |
| `internal/remotecontrol/effort` | the reasoning-effort vocabulary | 35 | 46 |

**`internal/remotecontrol` re-exports nothing.** This is the difference
between a split and a facade, and it is the rule cycles 376 and 377
established: a package whose whole surface is aliases and forwarding
calls is a shell, and a shell is retired. The moved symbols were
**renamed** at their new home, not re-exported under their old names —
`CascadeRegistry` became `cascades.Registry`, `MessageBackend` became
`cascades.Backend`, `MessagePrompt` became `cascades.Prompt`,
`NewCascade` became `cascades.New`. The old names are gone from the
tree.

The tree grew slightly — **65 561 → 67 516 lines** — because each
package carries its own file headers, and because the test fixtures that
cannot cross a package boundary had to be copied (see §5). The *root*
shrank from 39 593 production lines to **20 945** (a 47 % cut), and the
largest file in the root is now a routing table rather than a domain.

### 2. The domain pattern

Every domain package has the same shape, and the shape is what makes the
split reversible rather than merely mechanical:

```
internal/remotecontrol/<domain>/
  route.go        the domain's own method table
  seams.go        what the host hands the domain
  dispatcher.go   the handlers
  store.go        the domain's state
  store_fs.go     the on-disk store
  store_mem.go    the in-memory store
```

`internal/remotecontrol` keeps the **fan-out** and the **assembly**;
the domain keeps the **table**, the **handlers**, the **state** and the
**vocabulary**. Concretely:

- **The dispatcher keeps the fan-out.** `(*Dispatcher).routeMethod` tries
  each router in a fixed order and answers `-32601 "Method not found"`
  when none of them claims the method. The order is the reference's
  order and it did not change: the quota route, the code-assist route,
  the language-server table, the IDE dialect's three cancel methods, and
  then the sixteen per-family routers.
- **The domain owns its method names.** `permissions.MethodSetPreset` is
  `SetRemoteAgentPermissionPreset`; `terminals.MethodCreate` is
  `CreateRemoteAgentTerminal`. A domain that owned the fan-out would own
  every other domain's method names too.
- **The domain owns its wire shapes.** The JSON-RPC envelope is built by
  `rpc`, and every domain builds its result and its error through it. A
  domain that builds its own error envelope is a domain that can drift
  from the reference's code table.
- **The domain never sees the dispatcher.** A handler receives its
  dependencies through a `Seams` struct the host assembles. The
  permissions domain's seams are the store and a `Broadcast` callback;
  the terminals domain's are the registry, the session directory and a
  `Broadcast`. A handler that needed the dispatcher would be a handler
  that owned the fan-out.

The `Route` function has one shape across eleven domains:

```go
func Route(s Seams, method string, params json.RawMessage) (any, *rpc.ErrorBody, bool)
```

The third value is **false** when the method is not the domain's — the
dispatcher then tries the next domain. `resources.Route` takes a
`context.Context` as well, because its handler needs one; that is the
only signature variation, and it is a variation the domain's own need
forced.

Two domains answer a **second** table, because they serve two
protocols: `permissions.LSRoute` (the two proto-JSON language-server
methods) and `terminals.LSRoute` (the Web UI's terminal methods). The
language server spells a method in both its proto name and its JSON
name; the domain's table accepts either, the way the host's table does.

### 3. The root's field names did not change

The split changed the **types** of the configuration fields, not their
names:

| field | before | after |
|---|---|---|
| `Config.Cascades` | `*CascadeRegistry` | `*cascades.Registry` |
| `Config.MessageBackend` | `MessageBackend` | `cascades.Backend` |
| `HostConfig.MessageBackend` | `MessageBackend` | `cascades.Backend` |

The names stayed because the names are the configuration's **own
vocabulary**, and a configuration file's member names are a contract with
the operator. `Config.MessageBackend` is still the seam a host fills
with a backend; only the type it is now spelled with moved.

### 4. The cascade domain's dependency inversion

The cascade domain is the one domain that needed something from the
host, and the interesting part of the split is how that was solved.

The interactive tools ask the **Web UI** for an answer: a question, a
permission, an approval. The mechanism is a `Host` on the turn's
context, and the host is `internal/remotecontrol`'s own
`lsInteractionHost` — the thing that publishes the waiting step the
Web UI's card reads and blocks until the user answered.

Before the split, the cascade code called the root's
`lsInstallInteractionHost` directly. That is a **domain → host**
dependency, and it is exactly the edge a split must not create.

The inversion is a capability interface the domain declares and the host
implements:

```go
// cascades/events.go
type InteractionHostInstaller interface {
    InstallInteractionHost(ctx context.Context, c *Cascade, sink EventSink) context.Context
}

// cascades/turn.go
if installer, ok := sink.(InteractionHostInstaller); ok {
    turnCtx = installer.InstallInteractionHost(turnCtx, c, sink)
}
```

The root implements it on `*lsTrajectorySink` and `*lsThoughtSink`. The
dependency now runs **from the domain to the host**: the cascade asks
the sink whether it can install a host, and the sink — which owns the
Web UI's surface — says yes.

**This was a regression caught during the split, not a foresight.** The
first extraction left `lsInstallInteractionHost` as dead code in the
root, and the Web UI's permission card would never have appeared again.
Two tests now pin the seam:

- `internal/remotecontrol/ls_interaction_test.go:TestLSInteraction_TheSinkInstallsTheHost`
  — both of the Web UI's sinks answer "yes", and the context they hand
  back carries the host;
- `internal/remotecontrol/cascades/cascade_test.go:TestSendStream_InstallsTheInteractionHost`
  — the registry actually asks.

### 5. The tests follow their subject

Cycle 379's rule applies unchanged: **a test that dispatches a tool by
name lives with the dispatcher; a test that constructs the subject
directly lives with the capability that owns it.**

So each domain package holds its own store tests, its own handler tests
and its own vocabulary tests. The root keeps the dispatcher's
round-trips — the JSON-RPC envelope the dashboard sees.

Two fixtures could not be moved and had to be **copied**:

- the root's `captureSink` — a test double for the cascade domain's
  `EventSink`. The domain's own tests carry a copy, because the domain
  package must not import its parent, and the parent must not be
  imported by the domain: sharing it would be an import cycle.
- the root's `codeAsInt` / `sizeAsInt` / `jsonMarshal` / `jsonRPCFor`
  helpers — shared across the root's own test files, so moving one out
  of the root breaks the others. The domain packages carry their own
  copies.

The duplication is a cost of the boundary, and it is paid deliberately:
**a duplicated fixture is cheaper than a cycle.**

### 6. What stayed in the root, and why

Three things could have moved and did not:

- **`Session`, `SessionFactory` and `PiagentBackend`** stay in the root.
  `Session` speaks PiG's `agent.AgentEvent` and `agent.AgentMessage`, so
  moving it would make the domain package depend on PiG — the domain
  would no longer be stdlib-only, and the "the domain knows nothing
  about the turn engine" property would be gone.
- **`internal/remotecontrol/ls_terminals.go`** is *not* the terminals
  domain. It is the **Web UI's own terminal surface** — a separate
  contract (`RemoteAgentTerminal*`) over the `rcterminal/` pty registry.
  The `terminals` domain is the dashboard's `CreateRemoteAgentTerminal`
  family. The two were confused once and the confusion is recorded here
  so it does not happen again.
- **the language-server table** stays in the root. It is the IDE's own
  surface, and the alias table (`reference_surface.go`) is the clone's
  fallback behind it.

## 硬约束

- **硬约束**: the reference's **method names did not move**. Every
  domain's table carries the reference's own spelling
  (`SetRemoteAgentPermissionPreset`, `CreateRemoteAgentTerminal`,
  `UpsertRemoteAgentConversationConfig`, …), and the dispatcher's
  fan-out order is unchanged.
- **硬约束**: the reference's **error codes did not move**. The two
  canonical bodies live in `remotecontrol/rpc`, and every domain builds
  its error through them. The `-32006` (interaction not found),
  `-32010` (timeout) and `-32601` (method not found) answers are the
  same bytes.
- **硬约束**: the **wire envelope** is unchanged. `rpc.Result` and
  `rpc.Error` build the same JSON the dispatcher built before the split;
  the envelope moved to its own package, and the *shape* did not.
- **硬约束**: the split added **no new production door**. The domain
  packages export their store interfaces, their `Route` functions, their
  value types and their vocabulary — nothing that reaches the
  transport, the tunnel, the account pool or the model catalogue.
- **硬约束**: the domain packages are **stdlib-only**. No third-party
  dependency, no dependency on PiG, no dependency on the clone's own
  packages except `remotecontrol/rpc` (the envelope) and
  `internal/core/seam` (the interaction seam, in the cascade domain).
  The seam is a *contract*, and a contract is allowed to be imported.
- **硬约束**: **zero upward edges and zero import cycles** were measured
  over the production graph after the split (the same measurement cycle
  378 introduced). The test-only graph has one pre-existing upward edge
  (`internal/core/lifecycle` → `internal/app/subagent/core`), which
  predates this cycle.
- **硬约束**: the `internal/cliapp` test failures that exist at HEAD are
  **pre-existing** and unrelated — the four changelog tests hardcode
  `## 1.4.9` / `1.4.10:` while the feed now starts at `1.4.11`, and
  `TestModelPicker_KeepsConfiguredModels` is the same staleness. They
  were present before this cycle's first tranche.

## 存疑

- **存疑**: the split is a **judgement about cohesion**, and the
  measurement only checks that the result is *acyclic*. Nothing proves
  that `files` deserves its own package rather than living beside
  `dispatch_languageserver.go`, or that `effort` — 35 production lines —
  deserves to be a package at all. The reasoning was: `effort` is the
  reference's *vocabulary* (the reasoning-tier names), and the vocabulary
  is what the catalogue and the picker both read; a vocabulary that
  lives in one of them is a vocabulary the other has to import the
  whole of. A different reader could draw the line elsewhere — the cost
  of moving one is now a package move, not a god-package surgery.
- **存疑**: the **`Route` shape is uniform but not forced**. Eleven
  domains answer `(result, error, handled)` and one (`resources`) adds a
  `context.Context`. The uniformity was worth it for the eleven; the
  twelfth is a reminder that a shared shape is a convenience, not a law.
- **存疑**: the **`Seams` structs are hand-assembled per domain**, so
  the dispatcher knows each domain's dependencies. That is the point —
  the assembly point is the host — but it means the dispatcher's
  dependency list is the union of a dozen domains' dependencies again,
  one level down. A domain that grows a dependency makes the
  dispatcher grow a field.
- **存疑**: the **cascade domain's `Session` boundary is a compromise**.
  `cascades.Backend` is a narrow interface; the PiG-backed
  implementation stays in the root because PiG's types leak into it. The
  domain is therefore stdlib-only in its *interface* but has a
  host-side implementation. That is the same shape as every other
  domain's store — the interface is the domain's, the implementation is
  the host's — so it is defensible, but the domain's "no PiG" property
  is a property of the package, not of the type.
- **存疑**: the **tree grew** by ~2 000 lines. Some of that is
  documentation headers, which is cheap. Some of it is the duplicated
  fixtures, which is a standing cost: every future change to the
  `EventSink` interface has to be made twice.

## limitations

- **limitations**: the measurement scripts are **not in the tree**. They
  are migration tools, as in cycles 378 and 379.
- **limitations**: this cycle split **one** of the two god packages
  cycle 379 named. `internal/cliapp` (~74 kLOC) is untouched, and it is
  the larger one. The clusters are known — `slash_*` (30 files),
  `tui_*` (31), `slash_panel_*` (12), `picker_*` (4) — and the same two
  rules apply: a package whose whole surface is aliases and forwarding
  calls is a shell, and a test follows its subject.
- **limitations**: the root of `internal/remotecontrol` is still **20 945
  production lines over 78 files**. The dispatcher, the language-server
  table, the trajectory, the interaction host, the Web UI's terminal and
  voice surfaces, the top-level status and the media store are still
  one package. The split made the domains movable; it did not split the
  host.
- **limitations**: the historical cycle reports under `docs/cycles/`
  were **not** rewritten. They are evidence: a report that says
  `internal/remotecontrol/mendel_flags.go` was true when it was written.
  Only the current-state documents (`docs/guide/*`, `AGENTS.md`) were
  repointed.
- **limitations**: the **`ls_terminals.go` / `terminals` confusion** was
  resolved by leaving both where they were and documenting the
  difference. A follow-up that merges them would have to decide what the
  merged package is called, and the Web UI's surface and the dashboard's
  surface have different contracts.
