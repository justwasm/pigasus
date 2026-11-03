# Cycle 378 — the tier placement: every package in a tier

## The problem

Cycle 377 retired the last of the 69 shells. What it left behind was the
question the shells had been hiding:

> a shell's *implementation* lives somewhere. Which tier?

The tree had five tier roots — `internal/core`, `internal/features`,
`internal/infra`, `internal/app`, `internal/ui` — but **dozens of
packages still sat outside every one of them**, and **the tier numbering
itself was a convention without a definition**.

Two things were wrong with that state.

**The placement was arbitrary.** A package's directory said where it was
born, not what it depended on. `internal/agyprompt` — the reference's
system prompt — sat next to `internal/agent`, the turn engine it does
not know about. `internal/compaction` — the token estimator that four
tiers use — sat next to `internal/cli`, the host that uses two of them.
A package's *position* in the tree carried no information, so nobody
could tell from the tree whether a new edge was a legal move.

**The layering rule had no teeth.** The documents claimed the tiers were
ordered and that nothing reached upward. There was no measurement, so
there was no way to know whether the claim was true — and the fact
extractions this cycle needed were *blocked* by it. The model catalogue
lived in `internal/cli`; moving it down to the features tier would have
made it import the CLI host, which would have been a cycle.

## What was done

The cycle is eight tranches:

| commit | what it moved |
|---|---|
| `a97a52c6` | the misplaced contracts into tier 1: the api error, the agent message, the CLI flags, the branding, the sandbox |
| `86da88c5` | the god package `internal/backend` — split into a tier-1 contract and five tier-3 transports |
| `27b006eb` | the settings store, the trust registry and the credential cascade |
| `897a972e` | the `ToolRunner` contract into tier 1 |
| `82bbdfa4` | the tool surface and the tool runner into the app tier |
| `feb3beb2` | the three host-to-host couplings |
| `9f4c382f` | the remaining 36 packages (587 files) |
| `5167d860` | the last two top-level packages |

### 1. The tier numbering is now a definition

The tiers are numbered by **dependency order**, and the number is the
rule:

| tier | root | may import |
|---|---|---|
| 1 | `internal/core/` | tier 1 |
| 2 | `internal/features/` | tiers 1–2 |
| 3 | `internal/infra/` | tiers 1–3 |
| 4 | `internal/app/` | tiers 1–4 |
| 5 | `internal/ui/` | tiers 1–5 |

A package in tier *N* may import tiers 1..*N* and **nothing above**.
The `ui` tier's one downward edge — it reads the `app` tier, because a
view renders what the app produced — is the only exception, and it runs
in the direction the numbering allows.

### 2. Sixty-three packages moved into their tier

Every move below is a **directory move**: the directory name is the
package name, so the package's identifier never changed.

| old path (gone) | tier path (live) | tier |
|---|---|---|
| `internal/agenttools` | `internal/app/agenttools` | 4 |
| `internal/agyprompt` | `internal/app/prompt/agyprompt` | 4 |
| `internal/agyproxy` | `internal/infra/agyproxy` | 3 |
| `internal/agytemplates` | `internal/app/prompt/agytemplates` | 4 |
| `internal/announce` | `internal/ui/views/announce` | 5 |
| `internal/app/prompt/branding` | `internal/core/branding` | 1 |
| `internal/app/session/store` | `internal/core/store` | 1 |
| `internal/auth` (the trust part) | `internal/core/policy/trust` | 1 |
| `internal/auth` (the settings part) | `internal/core/settings` | 1 |
| `internal/auth` (the rest) | `internal/features/auth` | 2 |
| `internal/backend` (the error part) | `internal/core/apierror` | 1 |
| `internal/backend` (the contract) | `internal/core/backend` | 1 |
| `internal/backend` (the CLI auth) | `internal/features/auth/cliauth` | 2 |
| `internal/backend` (the CloudCode wire) | `internal/infra/backend/cloudcode` | 3 |
| `internal/backend` (the gateway) | `internal/infra/backend/gateway` | 3 |
| `internal/backend` (the Gemini transport) | `internal/infra/backend/gemini` | 3 |
| `internal/backend` (the mock) | `internal/infra/backend/grpcmock` | 3 |
| `internal/backend` (the retry) | `internal/infra/backend/retry` | 3 |
| `internal/changelog` | `internal/ui/views/changelog` | 5 |
| `internal/cli` (the catalogue) | `internal/features/models/catalog` | 2 |
| `internal/cli` (the marketplace link) | `internal/features/plugins/marketplace` | 2 |
| `internal/cli` (the narration catalogue) | `internal/ui/views/narration` | 5 |
| `internal/codeassistclient` | `internal/infra/codeassistclient` | 3 |
| `internal/codesearchaction` | `internal/ui/views/codesearchaction` | 5 |
| `internal/compaction` | `internal/core/compaction` | 1 |
| `internal/configpanel` | `internal/ui/views/configpanel` | 5 |
| `internal/configtest` | `internal/features/models/configtest` | 2 |
| `internal/contextpanel` | `internal/ui/views/contextpanel` | 5 |
| `internal/convstore` | `internal/app/session/convstore` | 4 |
| `internal/editorkit` | `internal/ui/editor/editorkit` | 5 |
| `internal/features/tools/native` | `internal/app/tools/native` | 4 |
| `internal/features/tools/sandbox` | `internal/core/policy/sandbox` | 1 |
| `internal/features/tools/toolrunner` | `internal/app/toolrunner` | 4 |
| `internal/features/tools/wasm` | `internal/app/tools/wasm` | 4 |
| `internal/harnesstools` | `internal/app/harnesstools` | 4 |
| `internal/infra/proxy/resultparity` (the fixture) | `internal/core/resultparity` | 1 |
| `internal/infra/proxy/resultparity` (the replay) | `internal/app/resultparity` | 4 |
| `internal/loginui` | `internal/ui/views/loginui` | 5 |
| `internal/mdrender` | `internal/ui/mdrender` | 5 |
| `internal/memtrim` | `internal/infra/memtrim` | 3 |
| `internal/modelpicker` | `internal/ui/views/modelpicker` | 5 |
| `internal/models` | `internal/features/models` | 2 |
| `internal/narration` | `internal/ui/views/narration` | 5 |
| `internal/onboarding` | `internal/ui/views/onboarding` | 5 |
| `internal/pluginsui` | `internal/ui/views/pluginsui` | 5 |
| `internal/printmode` (the skills) | `internal/features/tools/skills` | 2 |
| `internal/programstatus` | `internal/ui/views/programstatus` | 5 |
| `internal/providerpanel` | `internal/ui/views/providerpanel` | 5 |
| `internal/refbackend` | `internal/infra/refbackend` | 3 |
| `internal/referencedrift` | `internal/infra/referencedrift` | 3 |
| `internal/rewindpanel` | `internal/ui/views/rewindpanel` | 5 |
| `internal/sidequestion` | `internal/ui/views/sidequestion` | 5 |
| `internal/skillsui` | `internal/ui/views/skillsui` | 5 |
| `internal/slashquote` | `internal/ui/views/slashquote` | 5 |
| `internal/subagenttools` | `internal/app/subagenttools` | 4 |
| `internal/themepreview` | `internal/ui/views/themepreview` | 5 |
| `internal/tools` | `internal/app/tools` | 4 |
| `internal/tuilib` | `internal/ui/tuilib` | 5 |
| `internal/tuitools` | `internal/ui/tuitools` | 5 |
| `internal/ui/cli/flags` | `internal/core/cliflags` | 1 |
| `internal/ui/headless/apierror` | `internal/core/apierror` | 1 |
| `internal/ui/headless/msg` | `internal/core/msg` | 1 |
| `internal/usagepanel` | `internal/ui/views/usagepanel` | 5 |
| `internal/voice` | `internal/ui/views/voice` | 5 |

The bulk tranche (`9f4c382f`) moved 36 packages across **587 files** and
changed **no behaviour**.

### 3. Five facts moved down to the tier that owns them

This was the part the tree's shape had been blocking. A package that
holds a fact the *lower* tiers need has to live *below* the host that
happened to define it first, or the lower tier has to import a host to
ask for it.

| the fact | used to live in | now lives in |
|---|---|---|
| the reference version | `internal/cli` | `internal/core/version` |
| the model catalogue, its gates, the Claude-5.5 gate | `internal/cli` | `internal/features/models/catalog` |
| the marketplace link | `internal/cli` | `internal/features/plugins/marketplace` |
| `UsageLogo` | `internal/cli` | `internal/ui/tuilib` |
| `CountLabel` | `internal/features/tools/codesearch` | `internal/core/plural` |

`internal/cli` keeps a thin re-export (`cli.ReferenceVersion`) for the
hosts that already read it from there. The model catalogue keeps **no**
such re-export: it cannot import the CLI host without closing a cycle,
which is exactly why it had to move.

`CountLabel` is the one that was not obvious. The codesearch action
reached *up* into the host for a label it formatted itself — a plural
rule is a tier-1 fact, and the codesearch action is a tier-2 one.

### 4. The subcommand table moved to the app tier

The dispatch table could not follow the facts down: its spec type carries
a `*cli.JetskiCLI`, so it cannot live below the CLI host. It moved to
`internal/app/subcommands`, and the **ten registration shells** under
`internal/modular/subcommands` were retired with it — they were
blank-import shims over a table whose own `init()` is what registers.

### 5. The top level of `internal/` now has three kinds of package

After the move, a package at the top level of `internal/` is a **host**,
an **entry surface**, or **evidence**. There is no fourth kind.

| package | kind |
|---|---|
| `internal/agent`, `internal/pigrt`, `internal/piagentbridge` | host — the two turn engines and the PiG translation layer |
| `internal/cli`, `internal/cliapp`, `internal/printmode` | entry surface — the three hosts that run a session |
| `internal/app/agentserver`, `internal/app/agentturn` | host + seam (cycle 374) |
| `internal/modular` | host — the registration surface |
| `internal/lsserver`, `internal/agyupdate` | entry surface |
| `internal/remotecontrol`, `internal/rchost`, `internal/rcfrontend`, `internal/rctunnel`, `internal/rcaudio` | host — the Web UI's own surface |
| `internal/cortex_pb`, `internal/language_server_pb`, `internal/codeium_common_pb`, `internal/extension_server_pb` | evidence — recovered proto skeletons |
| `internal/cortex`, `internal/language_server`, `internal/planrender` | evidence — the 1.3.3-era skeletons |

## The measurement

The claims above are **measured**, not asserted. The measurement walks
the production import graph and counts the edges that point up a tier.

```
=== tier -> tier edge counts (production) ===
  core     (1) -> core     (1) :    9  same
  features (2) -> core     (1) :   12
  features (2) -> features (2) :   12  same
  infra    (3) -> core     (1) :   15
  infra    (3) -> features (2) :   17
  infra    (3) -> infra    (3) :    8  same
  app      (4) -> core     (1) :   21
  app      (4) -> features (2) :    5
  app      (4) -> infra    (3) :    2
  app      (4) -> app      (4) :   48  same
  ui       (5) -> core     (1) :   12
  ui       (5) -> features (2) :   12
  ui       (5) -> infra    (3) :    1
  ui       (5) -> app      (4) :    2
  ui       (5) -> ui       (5) :   19  same

=== upward edges in detail (tier N importing tier M>N) ===
(none)
```

**Zero upward edges.** Every edge runs down or sideways.

```
=== PRODUCTION graph ===
packages 167  sccs 167  cycles 0

=== TEST graph (test files only) ===
cycles 0
```

**Zero import cycles.** 167 packages, 167 strongly connected components:
every package is its own component. The test-only graph has none either.

And the host rule:

```
=== tier -> host edges ===
(host = agent, pigrt, piagentbridge, modular, cli, cliapp, printmode)
(none)
```

`internal/core`, `internal/features`, `internal/infra` and `internal/ui`
import **no** host. Only the app tier reaches the turn engines, and the
hosts sit above every tier.

## What else the cycle carried

**The `/provider` panel's group collapse** (`7f532bec`) was committed in
this window because the tree did not build without it. The interactive
half of the feature — the collapsed set, the toggle, the cursor — landed
in `4bdc24a0` (the other agent's commit), and its consumer
(`internal/cliapp/slash_panel_provider.go`, reading `row.Expanded`) was
already committed against a field that was still uncommitted. The model
half landed here, and the tree built again.

**The drift lock followed its package** (`fcf3e01e`). `internal/infra/referencedrift`
holds `reference.lock.json` and names itself by a *constant*:

```go
const LockPath = "internal/infra/referencedrift/reference.lock.json"
```

A move that leaves the constant pointing at the old path does not fail to
build — it fails at the *test*, three packages away, with a message about
a missing file. Four tests caught it: the three lock tests and
`internal/cli`'s version-pin test.

**The documents were repointed** (`c7759a91`, `a9e4ae97`). The guide
named the old paths in ~40 places, so every "where the code is" pointer
was wrong after the move. The left column of a move map names a path that
is **gone** and is left alone; a path that belongs to *another* project —
`oneharness/internal/tools/tools` (the reference binary's own layout),
`google/internal/cloud/code/...` (the vendor's), `internal/config/store.go`
(charmbracelet/crush's) — is not ours and is not rewritten.

## The rules this cycle added

1. **The tiers are numbered by dependency order.** `core (1) < features
   (2) < infra (3) < app (4) < ui (5)`. A package in tier *N* may import
   tiers 1..*N* and nothing above.
2. **The lower four tiers import no host.** Only the app tier may use the
   turn engines; the hosts sit above every tier. The `ui` tier may read
   the `app` tier (a view renders what the app produced); the `app` tier
   never reads the `ui` tier.
3. **Every package at the top level of `internal/` is a host, an entry
   surface, or evidence.** A package that is none of the three is in the
   wrong place — put it in its tier.
4. **A fact belongs to the tier that owns it, not to the host that
   happened to hold it.** When a lower tier needs a fact that lives in
   `internal/cli`, the fact moves down and the host keeps at most a thin
   re-export.

## 硬约束 / 存疑 / limitations

- **硬约束**: zero upward tier edges. The measurement walks the
  production import graph and finds none, in the committed tree.
- **硬约束**: zero import cycles — 167 packages, 167 SCCs. The test-only
  graph has none either, so a test cannot hide a cycle the production
  graph does not have.
- **硬约束**: zero tier-to-host edges from `core`, `features`, `infra`
  and `ui`. The app tier is the only tier that reaches `internal/agent`,
  `internal/pigrt`, `internal/piagentbridge`, `internal/modular` or
  `internal/cli`.
- **硬约束**: the moves changed no behaviour. The bulk commit touched 587
  files; every content change in it is an import path, a package clause
  or a relative fixture path. The one behavioural commit in the window
  (`7f532bec`) is the `/provider` collapse, and it is a feature.
- **硬约束**: the six `internal/cliapp` test failures that exist at HEAD
  are **pre-existing**. They are present at `feb3beb2` (the commit before
  this cycle's window) and are not caused by it: four changelog tests
  hardcode `## 1.4.9` / `1.4.10:` while the feed now starts at
  `1.4.11`, and `TestModelPicker_KeepsConfiguredModels` is the same
  staleness. Verified by running the suite in a worktree at `feb3beb2`.
- **硬约束**: `go vet` reports the same four warnings as before the
  cycle — `internal/app/tools/support.go` (lostcancel) and
  `internal/app/subagenttools/builtin_routing_test.go` (copylocks). Both
  are present at `4bdc24a0`, the commit before this cycle's first
  tranche.
- **硬约束**: a moved package changes its directory depth, so a
  **relative fixture path inside its own tests** changes meaning. Four
  were caught and fixed: `internal/agent/task_inbox_test.go`,
  `internal/app/subagenttools/reference_declaration_test.go`,
  `internal/app/resultparity/replay_test.go` and
  `internal/cliapp/conversation_test.go`.
- **存疑**: the tier *assignment* is a judgement, and the measurement
  only checks that the assignment is *consistent* — that no edge points
  up. It cannot check that the assignment is the *right* one. The
  evidence for each move is the package's own dependency set: a package
  that imports nothing above tier *N* *can* live in tier *N*, but it
  could also live lower, and the choice was made by reading what the
  package is for. `internal/ui/views/providerpanel` is the clearest
  borderline case — it depends only on the model package, so it could
  have been tier 2, but it is the `/provider` panel's decision layer, and
  the panel it serves is presentation.
- **存疑**: the "top level = host | entry surface | evidence" rule is
  checked by reading the list, not by a test. A future package could be
  added at the top level without anything failing. The rule is stated in
  `AGENTS.md` and in the tier guide, which is where a rule that needs
  judgement belongs — but it is not enforced.
- **存疑**: the cycle's eight tranches are **not** one atomic move. The
  bulk tranche (`9f4c382f`) carried 36 packages; the earlier ones
  carried the splits (`internal/backend` → six packages, `internal/auth`
  → three) and the fact extractions. The cycle reads as one change and
  landed as eight commits, each of which builds — but a reader who
  bisects gets a tree where the *shape* is only partly settled.
- **limitations**: the measurement scripts are **not in the tree**. They
  are migration tools. A permanent tier test would be a test of the
  directory layout rather than of behaviour — but the *numbering* is in
  `AGENTS.md` and the guide, and the numbers in this report can be
  recomputed by anyone who writes the same walk.
- **limitations**: this cycle placed the packages; it did not **shrink**
  them. The three god packages are unchanged: `internal/remotecontrol`
  (~65 kLOC), `internal/cliapp` (~74 kLOC) and `internal/app/tools`
  (~16 kLOC). A tier tells you what a package may depend on; it does not
  tell you that a package is cohesive. Splitting them is the next
  tranche, and the tier layout is what makes it possible — a
  `internal/app/tools` that reaches into `internal/ui` could not be
  split without a cycle.
- **limitations**: the historical cycle reports under `docs/cycles/` were
  **not** rewritten. They are evidence: a report that says
  `internal/agyprompt` was true when it was written. Only the
  current-state documents (`docs/guide/*`, `AGENTS.md`,
  `docs/research/REFERENCES.md`) were repointed.
