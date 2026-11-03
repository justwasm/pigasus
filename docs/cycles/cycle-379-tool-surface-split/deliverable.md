# Cycle 379 — the tool surface, split

## The problem

Cycle 378 gave every package in `internal/` a tier, and the tier layout
says what a package *may* depend on. It does not say a package is
*cohesive*. `internal/app/tools` was the proof: it was ~9 300 lines of
production Go in one directory, and it held

- the **contract** — `Tool`, `Registry`, `ArgsOf`, the result stamp, the
  failure text, the citation strip, the reference description and variant
  fixtures;
- **six executors** — the shell, the filesystem, the browser, the web, the
  task and the notebook;
- the **catalogue** — the sixty advertised names, the twelve wire
  declarations, the harness declarations.

Every importer named the one package, whatever it wanted from it. A
caller that only wanted `ArgsOf` — the argument decoder — also imported
the browser executor, the background-task table and the catalogue. The
package's dependency set was the union of six unrelated facts, and there
was no way to make it smaller without splitting it.

The split was blocked on nothing. It was simply the next tranche.

## What was done

Two commits.

| commit | what it did |
|---|---|
| `e6d9c485` | the split: seven packages, 61 files renamed, 24 added, 59 edited inside `internal/app/tools`, and 49 importer files rewritten |
| `a13dd0ee` | one home per reference capture, and the guides repointed |

### 1. Seven packages, no facade

| package | owns | production | test |
|---|---|---|---|
| `internal/app/tools` | the **contract** — `Tool`, `Registry`, `ArgsOf`, the result stamp, the failure text, the citation strip, the reference fixtures | 1 929 | 1 303 |
| `internal/app/tools/shell` | `run_command` and its siblings, the background-task table, the schedule state, the notification | 2 290 | 2 004 |
| `internal/app/tools/filesystem` | the file tools, the searches, the notebook, the workspace walker, the image and WebM probes | 1 481 | 888 |
| `internal/app/tools/browser` | the browser tools, the browser subagent, the in-Chrome bridge | 1 039 | 315 |
| `internal/app/tools/web` | `search_web`, `read_url_content`, `generate_image`, the three MCP tools | 747 | 455 |
| `internal/app/tools/tasks` | `manage_task`, `command_status`, `send_command_input`, `schedule`, the interactive tools, the notice inbox | 991 | 703 |
| `internal/app/tools/surface` | the **catalogue** — `Catalog`, `RuntimeCatalog`, the model declarations, the harness declarations | 642 | 1 482 |

`internal/app/tools/native` and `internal/app/tools/wasm` were already their
own packages and did not move.

**`internal/app/tools` re-exports nothing.** This is the difference
between a split and a facade. A facade keeps the old name working
(`type X = other.X` plus a forwarding call) — which is exactly the
*shell* that cycles 376 and 377 retired, and a shell adds a hop without
adding a fact. The split instead **distributed** the public API: every
importer names the package that owns the fact it uses.

```
surface.Catalog(dir)              the catalogue
surface.RuntimeCatalog(dir)       the catalogue plus the runtime executors
surface.ModelDeclarations()       the twelve wire declarations
shell.RunCommandDescription()     run_command's prose
shell.RunCommandSchema            run_command's schema
shell.KillBackgroundTask(id)      the background-task table's door
filesystem.ViewFileSchema         view_file's schema
filesystem.ViewFileDescription    view_file's prose
web.SetMCPProvider(pool)          the MCP pool's door
tools.ArgsOf(raw, &v)             the argument decoder
tools.IsFailureText(body)         the failure-text test
tools.ReferenceDescriptions()      the reference's description fixture
tools.ReferenceVariants()         the reference's variant fixture
```

### 2. The dependency graph

The DAG is acyclic. An arrow means *imports*:

```
shell      ──► tools          (the contract)
filesystem ──► tools
browser    ──► tools
web        ──► tools
tasks      ──► tools, shell   (the background-task table)
surface    ──► every capability (the catalogue *is* the set of executors)
```

- `tools` is the leaf every capability imports — the contract.
- `tasks` imports `shell` for the background-task table.
- `surface` imports every capability, because the catalogue *is* the set
  of executors.

### 3. The rules this cycle added

1. **A test follows its subject.** A test that **dispatches** a tool by
   name exercises the catalogue and lives in `surface`; a test that
   **constructs** a tool directly lives with the capability that owns it.
   The rule is forced by Go: `surface` imports every capability, so a
   capability's test may not import `surface` without closing a cycle.
   The reverse edge is what the old god package made impossible to
   express — in one directory, `catalog_test.go` and `command_test.go`
   were neighbours, and the second could reach the first.
2. **No production door was added for a test.** The `tasks` package's
   tests launch a *real* background task through
   `shell.RunCommand{background:true}` and address it by the id the tool
   itself reports. The door that already existed — the one the
   remote-control surface uses to kill a task — is the door the test
   uses.
3. **A split does not leave a second name for the old package.** A
   facade is a shell; the shells are retired.

### 4. Every reference capture has one home

The first tranche of the split left **four copies** of the reference's
captures in the tree: the advertised-name lists were duplicated into
`surface/testdata`, the background-task capture into `shell/testdata`, and
the result-text capture into three capability packages. Only the copies
in the contract package's `testdata` were covered by the reference lock
(`internal/infra/referencedrift`), so a copy could drift from the
capture the lock protects and **nothing would fail**.

The second tranche gave each capture exactly one home — the home of the
package that owns the fact:

| capture | home |
|---|---|
| `tool_descriptions_1.4.7.json`, `tool_variants_1.4.7.json`, `result_texts_1.4.7.txt`, `result_texts_1.4.4.txt`, `large_json_result.json` | `internal/app/tools/testdata/` (the contract embeds the first two) |
| `advertised_tools_1.3.6.txt`, `advertised_tools_1.4.3.txt`, `tool_names_1.4.7.txt`, `tool_errors_1.4.8.txt` | `internal/app/tools/surface/testdata/` |
| `reference_background_task_1.4.7.txt` | `internal/app/tools/shell/testdata/` |

The capability packages that read a shared capture read it through a
relative path (`../testdata/…`), the way `internal/app/subagenttools`
already reads the declaration capture. The reference lock follows the
four files that moved.

### 5. The guides

`docs/guide/tools.md` gained a **package layout** section at the top and
the two rules; `docs/guide/architecture-tiers.md` records the split as the
first worked example of *a fact belongs to the tier that owns it*; every
path in the guides that named a moved file names its new home.

## The regression the split caused, and how it was caught

The call sites were rewritten by a script that qualified an identifier
with its new package. The script also rewrote **string literals** and
**comments**, because a regex cannot tell a selector from a text:

```go
// before
"Tool is running as a background task with task id: "
// after the script
"tools.Tool is running as a background task with task id: "
```

The reference's own launch acknowledgement is a **byte-exact** string —
the capture in `reference_background_task_1.4.7.txt` and the 1.4.7
result-text capture both carry `Tool is running as a background task`,
with no qualifier. The script's version reached the model.

It was caught by the tests that compare the launch acknowledgement with
the capture, in five files: the production file
(`shell/background_task.go`), four test files, and the prose in
`shell/command.go`. All were reverted to the reference's bytes.

The rule that came out of it is now invariant 73: **the reference's own
bytes never take a qualifier**. After any rename pass, grep for
`[A-Za-z_]\w*\.(tools|shell|filesystem|browser|web|tasks|surface)\.` and
for a spurious `pkg.Symbol` inside a string literal.

## 硬约束 / 存疑 / limitations

- **硬约束**: zero upward tier edges. The measurement walks the
  production import graph and finds none. All seven packages are in the
  app tier, and the app tier is where `internal/app/tools` already was.
- **硬约束**: zero import cycles — 173 packages, 173 SCCs in the
  production graph, and none in the test-only graph. The test graph is
  the one that matters here: the split moved tests across package
  boundaries, and a test that reached back into its own package's
  catalogue would have closed a cycle.
- **硬约束**: the split changed **no behaviour**. Every production file
  under `internal/app/tools` was moved whole, or edited only in its
  package clause, its imports, its qualifiers and its relative fixture
  paths. The catalogue's *body* changed in one more way: it now builds
  the executors from the capability packages instead of from its own
  neighbours — the same sixty names, the same twelve declarations, the
  same schema bytes. The one textual change that was *not* mechanical —
  the `"tools.Tool"` regression — was reverted before the commit.
- **硬约束**: the reference lock is green, and it covers the capture
  that the background-task test reads. The four paths that moved were
  updated in `reference.lock.json` and the digests regenerated with
  `go test ./internal/infra/referencedrift/ -run TestReferenceLock -update`.
- **硬约束**: `go vet` reports the same four warnings as before the
  cycle — `internal/app/tools/shell/support.go` (lostcancel ×2) and
  `internal/app/subagenttools/builtin_routing_test.go` (copylocks ×2).
  Both are present at `4aa07c34`, the commit before this cycle's first
  tranche.
- **硬约束**: the six `internal/cliapp` test failures that exist at HEAD
  are **pre-existing** and unrelated: four changelog tests hardcode
  `## 1.4.9` / `1.4.10:` while the feed now starts at `1.4.11`, and
  `TestModelPicker_KeepsConfiguredModels` is the same staleness.
  Verified by running the suite in a worktree at `4aa07c34`, the commit
  before this cycle's first tranche.
- **硬约束**: a *seventh* `internal/cliapp` failure,
  `TestStatusLinePayload_PrintModeUnaffected`, appears intermittently
  (about one run in three) in the whole-repository suite. It is **not**
  caused by this split: it does not reproduce at `e6d9c485` (0 failures
  in 7 whole-repository runs) and does reproduce at `2921d291` (2 in 6),
  which is the concurrent providers/catwalk-fallback commit that landed
  between this cycle's two tranches. The cause is visible in the failure
  text: `buildStatusLinePayload` resolves the context window through
  `modelctx.ContextWindowFor`, and the two back-to-back calls see
  `ContextWindowSize` 1 000 000 and 512 000 — a global registry that
  changed between the calls.
- **硬约束**: the split added **no new production door**. The `tasks`
  tests reach the background-task table through the doors that already
  existed for the remote-control surface.
- **硬约束**: the reference's *advertised* surface did not move. Sixty
  names in `init.tools`, twelve wire declarations, and the harness's
  eighteen — the same counts, from the same fixtures.
- **存疑**: the split is a **judgement about cohesion**, and the
  measurement only checks that the result is *acyclic*. Nothing proves
  that `shell` is the right home for the background-task table, or that
  `surface` is the right home for the model declarations rather than
  beside them in the contract. The reasoning was: the background-task
  table is the shell capability's own state (the command capability
  starts a task, the task capability asks for its status, the
  remote-control surface kills it), and the declarations are the
  catalogue's own surface (they are what the catalogue advertises). A
  different reader could draw the line elsewhere — the cost of moving
  one is now a package move, not a god-package surgery.
- **存疑**: the *test-placement* rule is partly a consequence of Go's
  import rules rather than a free choice. "Dispatch by name → `surface`"
  is a rule; "construct directly → the capability" is a rule that could
  have been the other way round. It was chosen so that a test is
  *about* the package it lives in, and so that no test needs the
  catalogue to reach the thing it is testing.
- **存疑**: `internal/app/tools` is still the **contract plus a
  directory name**, and its name still reads like "the tools" rather
  than "the tool contract". Renaming it would touch 49 importer files
  again; the split deliberately did not, because a rename that changes
  every call site's qualifier is a second, unrelated diff.
- **存疑**: `internal/app/tools/shell` at 2 290 production lines is the
  largest of the seven, and it still holds the pty, the process group,
  the Windows shell selection, the notification and the background-task
  table. It is *smaller*, not *small*.
- **limitations**: the measurement scripts are **not in the tree**. They
  are migration tools, as in cycle 378.
- **limitations**: this cycle split **one** of the three god packages
  cycle 378 named. `internal/remotecontrol` (~65 kLOC) and
  `internal/cliapp` (~74 kLOC) are untouched, and they are the larger
  two. The clusters are known — `dispatch_*` (18 files), `ls_*` (23),
  `terminals_*` (7), `cascade_*` (5) in the first; `slash_*` (30),
  `tui_*` (31), `slash_panel_*` (12), `picker_*` (4) in the second — and
  the same two rules apply: a package whose whole surface is aliases and
  forwarding calls is a shell, and a test follows its subject.
- **limitations**: the historical cycle reports under `docs/cycles/`
  were **not** rewritten. They are evidence: a report that says
  `internal/app/tools/catalog.go` was true when it was written. Only
  the current-state documents (`docs/guide/*`, `AGENTS.md`) were
  repointed.
- **limitations**: the fixture consolidation in the second tranche was
  driven by the *copy* problem, not by the split. A reader who diffs
  the two commits separately will see the second one as a
  fixture-move commit, which is what it is.
