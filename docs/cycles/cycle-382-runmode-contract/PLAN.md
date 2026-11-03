# Cycle 382 — the `runMode` contract and state extraction

> **Status**: plan only. Nothing in this document has been implemented.
> **Measured on**: HEAD `e5a6454e` (2026-11-01 17:48 CST), working tree clean
> for `internal/cliapp`'s `runMode` struct.
> **Authority**: this file supersedes the earlier `goal.md` and
> `goal-vs-plan.md` drafts in this folder. `/goal` and `/plan` are
> execution *modes*, not different planning products — this document is
> the single plan both would consume.

---

## 0. Why this cycle exists

`internal/cliapp` holds a `runMode` struct with **219 fields** and
**527 production methods**, all in `tui_run.go`. Cycle 381 moved 23 modal panels
out into `internal/ui/views/*`, and it worked — but it moved *panels*,
not *state*. The host type did not shrink: the panel packages read and
write the host's fields through accessor methods, so the state is
still one flat struct.

The reference does not do this. Agy 1.4.12's symbol table shows:

| | agy 1.4.12 | clone (HEAD `e5a6454e`) |
|---|---|---|
| top-level TUI type | **none named `runMode`** | `runMode`: 219 fields / 527 methods |
| model layer | **49** types implementing `Update` | none |
| shared base | `baseModel` — **7** methods, inherited by embedding | none |
| central state | `cli/store/store.Manager` — **484** methods | `internal/core/store.Manager` — 5 fields, unrelated |
| panel packages | 49 model types | 38 dirs under `internal/ui/views/` |
| `cli/` subpackages | **40** | symbol-table census |

> **硬约束**: `runMode` does not exist in agy. `grep -ic runmode` over
> agy 1.4.12's 168,717 symbols returns **0**. The single literal
> occurrence in the binary is JavaScript inside an embedded WebKit
> resource (`option.point.yrepeatsrowInforunModetheDatayOffset…`).
> So this cycle is **not** "reproduce agy's `runMode`" — agy has none.
> It is "replace the clone's `runMode` with the shape agy actually uses".

**Evidence grade**: 【反编译】for every symbol count (taken from the
recovered `pclntab` symbol table at `/tmp/agy1412seeds/funcs.txt`);
【一手】for every clone-side measurement (taken with `go/ast`, not a
regex — see §7).

---

## 1. The target architecture

### 1.1 The reference's model contract

`baseModel` implements exactly **7** methods, and they are the ones
every model inherits:

```
ActionFromMsg   HasArtifacts   Keybindings   Layout   SetLayout   Store   UIPhase
```

Seven methods have exactly **one** implementor in the whole binary —
`baseModel`. The other three of the ten-method contract are implemented
per model:

| method | implementors | meaning |
|---|---|---|
| `Store` `Layout` `UIPhase` `Keybindings` `ActionFromMsg` `HasArtifacts` `SetLayout` | 1 each | `baseModel` — inherited |
| `Update` | 49 | each model its own |
| `View` | 48 | each model its own |
| `Init` | 44 | each model its own |

The 49 model types:

```
ActiveItemsModel      AgentsModel         ArgSuggestionModel   ArtifactDetailModel
ArtifactViewModel     AskQuestionModel    AtSuggestionModel    AudioModel
AuthModel             BugReportModel      ChangelogModel       CloudPluginsModel
CodeSearchModel       CommentInputModel   ContextModel         ConversationModel
ConversationPickerModel CreditsModel      DefaultModel         DiffModel
EffortSelectorModel   FeedbackModel       GcpSetupModel        HelpModel
HooksModel            InFileSearchModel   InputModel           LiveCodeSearchModel
McpAuthModel          MCPModel            PermissionsModel     PluginsModel
ProactiveFeedbackModel ProjectsModel      PromptModel          RewindModel
RootModel             SelectorModel       SettingsErrorModel   SettingsModel
SkillsModel           SubagentDetailModel SuggestionModel      TaskDetailModel
TasksModel            ToolConfirmationModel UsageModel         WorkflowDetailModel
WorkspaceTrustModel
```

### 1.2 The router responsibility — the real divergence

Agy's root model is `RootModel`. Its exported and unexported methods:

```
ActionFromMsg   ExitProgramStatus   HasArtifacts   Init
Keybindings     Layout              Serialize      SetLayout
Store           UIPhase             Update         View
handleAction    handleActiveModelTransition    handleExitPress
handleInteractivePrompt   handleNotifications  handleOnboardingTransition
handleRebuildCommands     handleStatusLineLayout
handleViewportStrategy    update   updateActive
```

`baseModel` contributes the other seven: `Store`, `Layout`,
`UIPhase`, `Keybindings`, `ActionFromMsg`, `HasArtifacts`,
`SetLayout` — exactly the seven that have a single implementor in the
whole binary.

And its sibling methods carry the router duties by name — these are
the methods agy gives to the thing that *dispatches* between models.
All five are on `(*RootModel)`:

```
(*RootModel).handleActiveModelTransition
(*RootModel).handleRebuildCommands
(*RootModel).handleNotifications
(*RootModel).handleViewportStrategy
(*RootModel).handleStatusLineLayout
```

**This is the architectural point of the whole cycle.** In agy, "which
model is active" is a first-class piece of state with its own
transition handler, and the root model owns the rebuild, the
notification pump, the viewport strategy and the status-line layout
as named, separately-callable responsibilities. In the clone, it is a single field —
`m.panel.kind` (`internal/cliapp/slash_panels.go:70`) — dispatched by
**four** separate `switch` statements:

| file | line | switch |
|---|---|---|
| `internal/cliapp/slash_panel_keymap.go` | 92 | `switch m.panel.kind` — key routing |
| `internal/cliapp/slash_panel_routing.go` | 208 | `switch m.panel.kind` — `panelLinesCore` |
| `internal/cliapp/slash_panel_routing.go` | 250 | `switch m.panel.kind` — `panelLines` |
| `internal/cliapp/slash_viewer_spec.go` | 47 | `switch m.panel.kind` — `viewerSpecFor` |

Adding a panel means touching all four. That is the cost the cycle
pays down, and it is a better proxy for "did this cycle work" than the
field count.

> **硬约束**: the success metric is **`switch m.panel.kind` → 0
> occurrences**, with the dispatch replaced by a registry. The field
> count is a *guard*, not the goal — a cycle that cut fields to 80 but
> left four switches would have failed this cycle.

### 1.3 Where the state goes

Three new packages, one per concern:

```
internal/ui/tuimodel/        tier 5 — the contract + shared defaults
  model.go                   the Model interface (10 methods)
  base.go                    Base: the 7 inherited defaults (agy's baseModel)
  registry.go                the panel registry: kind → handler

internal/app/tuistore/       tier 4 — the state that outlives a frame
  store.go                   the Store type + accessors
  selection.go               selection / drag state
  bash.go                    bash-mode state
  btw.go                     side-question state
  streaming.go               streaming / queue state

internal/ui/panelregistry/   tier 5 — the panel registry itself
  registry.go                panelKind → Panel handler
```

`internal/ui/tuimodel` and `internal/app/tuistore` **do not exist yet**
(verified 2026-11-01 17:39 CST).

---

## 2. Baseline — measured, not estimated

All numbers taken with `go/ast` (§7 gives the script).

| metric | value | source |
|---|---|---|
| `runMode` fields | **219** | `go/ast`, `tui_run.go:105` |
| `runMode` methods | **527** | `go/ast` over `internal/cliapp/*.go` (excludes `_test.go`) |
| duplicate method names | **0** | `go/ast` |
| test-only helpers on `*runMode` | **3** | `tui_run_test.go` — not counted above |
| `switch m.panel.kind` sites | **4** | `rg` over `internal/cliapp/*.go` |
| `panelKind` constants | **25** (incl. `panelNone`; 24 real panels) | `internal/cliapp/slash_panels.go:70-97` |
| panel packages live | **38** dirs | `internal/ui/views/` |
| `internal/cliapp` files | **90** production (134 incl. tests) | `ls` over the tree |
| production lines | **19 011** | measured at HEAD; cycle 381 reported 19 498 at its own HEAD |

Field clusters (prefix census over the 219, measured with the
same script) — this is the extraction seam map:

| cluster | count | natural home |
|---|---|---|
| render cache (`cached*` `rows*` `viewCache*` `scroll` `autoScroll` `vpRows` `extraRows` `headerCache`) | 24 | `tuistore` (render cache) |
| status / live (`status*` `live*`) | 14 | `tuistore` (status line) |
| selection (`sel*` `inputSel*` `pinnedPrompt*` `stickyBar*` `userPrompt*`) | 12 | `tuistore` (selection) |
| subagent (`sub*` `subAgent*` `atRouter` `taskSubagent*`) | 12 | `tuistore` (subagent) |
| compaction (`compact*`) | 8 | `tuistore` (compaction) |
| bash mode (`bash*`) | 7 | `tuistore` (bash mode) |
| side questions (`btw*` `activeBtw*`) | 7 | `tuistore` (side questions) |
| voice (`voice*`) | 6 | `tuistore` (voice) |
| picker (`picker*`) | 5 | `tuistore` (picker state) |
| queue (`queue*` `queued*` `streamQueue`) | 3 | `tuistore` (queue) |
| announce (`announce*`) | 3 | `tuistore` (announce) |

**101 of the 219 fields are `tuistore` candidates** (measured; the
clusters do not overlap). The remaining **118** are the turn loop's own
working set — backend, engine, session messages, task registry,
compaction timers, the input line itself — and should stay on the host.

> **Note**: 219 − 101 = 118 means a literal "≤ 80 fields" target is
> **not reachable** without also moving turn-loop state. See §10.1 —
> this is an open question for the owner, and the guard test's 80 must
> be reconciled with it before tranche 6 is written.

---

## 3. Tranches

Each tranche is one commit, green before the next starts. The order is
deliberate: **contract first, then dispatch, then state, then
inheritance, then the guard.**

### Tranche 1 — the contract package

**New**: `internal/ui/tuimodel/model.go`, `internal/ui/tuimodel/base.go`

- `Model` interface with the 10 methods agy uses:
  `Init`, `Update`, `View`, `Store`, `Layout`, `UIPhase`,
  `Keybindings`, `ActionFromMsg`, `HasArtifacts`, `SetLayout`.
- `Base` struct implementing the **7** inherited defaults
  (`Store`, `Layout`, `UIPhase`, `Keybindings`, `ActionFromMsg`,
  `HasArtifacts`, `SetLayout`) — one for one with agy's `baseModel`.
- `Base` must be embeddable: no `runMode` reference, no host import.

**Acceptance**: `go build ./internal/ui/tuimodel/` passes; the package
imports nothing from `internal/cliapp`.

### Tranche 2 — the panel registry (kills switch #1)

**New**: `internal/ui/panelregistry/registry.go`

- A `Handler` interface the host can call: `Lines()`, `HandleKey(key)`,
  `Spec()`, plus whatever the specific panel needs.
- A `Registry` mapping `panelKind` → handler. The `panelKind` type
  moves here (from `internal/cliapp/slash_panels.go`) so the registry
  can key on it without importing the host.
- The registry is **populated by the host at start-up** (the host owns
  the panel instances), so the package stays free of host imports.

**Touches**: `internal/cliapp/slash_panel_keymap.go` (switch at :92),
`internal/cliapp/slash_panel_routing.go` (switches at :208 and :250),
`internal/cliapp/slash_viewer_spec.go` (switch at :47).

> **⚠️ CONCURRENCY FENCE**: `slash_panel_routing.go` and
> `slash_panel_keymap.go` are being modified by the concurrent agent's
> cycle-381 work at the time of writing (working tree shows them as
> ` M`). **Re-read before editing. If they are dirty, defer the tranche
> and take tranche 1, 3, 4 instead** — see §5.

**Acceptance**: `rg 'switch m\.panel\.kind' internal/cliapp/` returns
**0 hits**; `go test ./internal/cliapp/` green; the 24 panels still open
and route correctly (the existing `model_test.go` / `slash_content_test.go`
assertions on `panel.kind` must still pass unchanged).

### Tranche 3 — the state package (first half)

**New**: `internal/app/tuistore/store.go`, `selection.go`, `bash.go`,
`btw.go`

- `Store` type; the fields move from `runMode` into it, with
  accessors so call sites change minimally.
- Tranche 3 moves the **pure state** clusters: selection
  (`selDragging`…`selEndCol`, `inputSel*`), bash mode (`bash*`), side
  questions (`btw*`, `activeBtw*`).

**Touches**: `internal/cliapp/tui_run.go` (field declarations only),
plus every method that reads or writes a moved field.

> **硬约束**: a **pure move**. No behaviour change, no renamed
> semantics, no "while I'm here" cleanups. The compiler finds every
> call site; each one changes from `m.bashMode` to `m.store.bashMode`.
> If a tranche needs more than a mechanical rewrite, it is too big —
> split it.

**Acceptance**: `runMode` field count drops by the moved count;
`go test ./internal/cliapp/` green; `go build ./...` green.

### Tranche 4 — the state package (second half)

**New**: `internal/app/tuistore/streaming.go`, `cached.go`, `compact.go`

- Streaming/queue (`streaming`, `queueMode`, `queuedPrompt`,
  `streamQueue`), the render cache (`cached*`, `rows*`, `viewCache*`),
  and the compaction state (`compact*`).

**Acceptance**: same as tranche 3.

### Tranche 5 — the host embeds the base

**Touches**: `internal/cliapp/tui_run.go`

- `runMode` embeds `tuimodel.Base`.
- The host's own `Init`/`Update`/`View` are unchanged in behaviour.
- The 7 inherited methods now come from the embedded `Base` rather
  than the host's hand-written copies.

**Acceptance**: `go test ./internal/cliapp/` green; the host's
`tea.Model` implementation is unchanged (it already satisfies
`Init`/`Update`/`View` — `internal/cliapp/tui_run.go:581`,
`tui_view.go:125`, `tui_runmode_agent.go:63`).

### Tranche 6 — the guard

**New**: `internal/cliapp/architecture_test.go` (or a dedicated
`internal/ui/tuimodel/guard_test.go`)

Three assertions:

1. `runMode` has **≤ 120 fields** (measured with `go/ast`, in the test
   itself — not a regex). The target follows from the measurement:
   219 − 101 = 118 fields that stay on the host, plus a small
   allowance. See §10.1.
2. `switch m.panel.kind` appears **0 times** in `internal/cliapp/`.
3. `internal/ui/tuimodel` and `internal/app/tuistore` import **no**
   host package.

**Acceptance**: the test passes at the end of the cycle and would
**fail** at HEAD `e5a6454e` (assert this by running it against the
pre-cycle tree — a guard that cannot fail is not a guard).

---

## 4. Acceptance criteria (the whole cycle)

| # | criterion | how it is checked |
|---|---|---|
| 1 | `runMode` ≤ **120** fields | `go/ast` in the guard test |
| 2 | `runMode` ≤ 120 methods | `go/ast` in the guard test |
| 3 | `switch m.panel.kind` → 0 | `rg` in the guard test |
| 4 | `internal/ui/tuimodel` + `internal/app/tuistore` import no host | guard test |
| 5 | tier rules intact (no upward edges, no cycles) | existing tier measurement |
| 6 | `go build ./...` green | build |
| 7 | `go test ./cmd/... ./internal/... ./endpoints/... ./configdir/...` green | test |
| 8 | reference drift gate green | `go test ./internal/infra/referencedrift/` |
| 9 | **zero behaviour change** | the existing `internal/cliapp` test suite is the oracle |

> **硬约束**: **never `go test ./...`** — `scratch/` has never
> compiled (AGENTS.md).
> **硬约束**: the **five changelog failures in `internal/cliapp`** are
> pre-existing and unrelated (cycle 381 §硬约束). Do not "fix" them
> as part of this cycle; note them as pre-existing.

---

## 5. Concurrency — the other agent

Another agent is working in this tree, on cycle 381 tranche 26
(`/changelog` + `/usage` panels, per the working tree at 17:39 CST).
It owns `internal/cliapp` files that this cycle also needs.

**Rules for this cycle's agent**:

1. **Re-read every file before editing it.** The concurrent agent
   commits to the same tree.
2. **Stage only your own paths.** Never `git add -A`.
3. **Commit as soon as a tranche is green** — a long-lived dirty tree
   makes conflicts expensive.
4. **If a file you need is dirty in the working tree**, defer that
   tranche and take another. Order of preference:
   1 → 3 → 4 → 5 → 2 → 6.
   Tranches 1, 3, 4 and 5 touch **new packages** plus
   `internal/cliapp/tui_run.go` — and `tui_run.go` is *also* the
   concurrent agent's territory.
5. **Never `git checkout` or `git stash` the concurrent agent's
   work.** If a conflict appears, re-read and adapt.
6. **Re-check `git status` before concluding a change vanished.**

---

## 6. Risks

| risk | likelihood | mitigation |
|---|---|---|
| The concurrent agent's next tranche rewrites `slash_panel_routing.go` | **high** | the fence in §5; tranche order 1→3→4→5 first |
| The state move is not mechanical | medium | tranche 3 is defined as a pure move; split if not |
| `internal/app/tuistore` breaks the tier rule | low | tier 4 may not import tier 5; `tuistore` must not import `internal/ui/*` |
| The `tea.Model` interface changes shape | low | the host already satisfies `Init`/`Update`/`View`; tranche 5 is embedding-only |
| The field count does not reach 120 | medium | 101 of 219 are candidates; if a cluster resists, it moves to `tuistore` anyway as opaque state |
| The guard test cannot fail | low | criterion: run the guard against the pre-cycle tree and confirm it fails |

---

## 7. The measurement script

Regex counting gave 132/217/546 in three different runs — it is not
trustworthy. This `go/ast` program is authoritative. It is the same
program the guard test will embed.

```go
// count.go — prints the runMode field and method counts.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dir := os.Args[1]
	fset := token.NewFileSet()
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var fields int
	methods := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range af.Decls {
			switch dd := d.(type) {
			case *ast.GenDecl:
				for _, s := range dd.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok || ts.Name.Name != "runMode" {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, fl := range st.Fields.List {
						if len(fl.Names) == 0 {
							fields++ // embedded
							continue
						}
						fields += len(fl.Names)
					}
				}
			case *ast.FuncDecl:
				if dd.Recv != nil && len(dd.Recv.List) == 1 {
					r := dd.Recv.List[0].Type
					if star, ok := r.(*ast.StarExpr); ok {
						r = star.X
					}
					if id, ok := r.(*ast.Ident); ok && id.Name == "runMode" {
						methods[dd.Name.Name] = true
					}
				}
			}
		}
	}
	fmt.Printf("fields=%d methods=%d\n", fields, len(methods))
}
```

Run it:

```bash
cd /tmp/rmplan/dumpcmd && go run main.go /Users/gear/GitHub/openagy/internal/cliapp
```

---

## 8. Documentation deliverables

| file | change |
|---|---|
| `docs/cycles/cycle-382-runmode-contract/deliverable.md` | the cycle report — the problem, what was done, the tables, 硬约束 / 存疑 / limitations |
| `docs/guide/architecture-tiers.md` | add `internal/ui/tuimodel` and `internal/app/tuistore` to the tier table |
| `docs/guide/tui.md` | the new dispatch model — registry, contract, state ownership |
| `AGENTS.md` | the tier map + the new invariant |
| `internal/app/prompt/agyprompt/PROVENANCE.md` | only if the reference's templates changed (they did not — re-verified 2026-11-01 14:51 CST, 17 files byte-identical) |

> **硬约束**: `out/*.md` is zh-CN; `docs/cycles/*` is English. New
> reports end with a 硬约束 / 存疑 / limitations section.

---

## 9. What this cycle is explicitly not

- **Not** a reproduction of agy's `runMode` — agy has none.
- **Not** a re-run of cycle 381 — that moved panels; this moves state
  and adds the contract.
- **Not** a behaviour change — every tranche is a refactor, and the
  existing `internal/cliapp` test suite is the oracle.
- **Not** a rewrite — cycle 381's 23 panel packages are the substrate
  this cycle builds the contract around.

---

## 10. Open questions for the owner

1. **Field target**: the measurement says **≤ 120**, not 80.
   101 of the 219 fields are `tuistore` candidates, leaving 118 on the
   host — 80 is unreachable without also moving turn-loop state
   (backend, engine, session messages, task registry). Is 120 the
   right number, or should turn-loop state move too (and into which
   tier)?
2. **Method target**: 120, or tighter? 527 → 120 is a 77% cut; most of
   it comes from the state move (each moved cluster's accessors).
3. **Should the reference's `serialize` / `ExitProgramStatus` methods
   have clone counterparts?** They exist on agy's `RootModel` but the
   clone has no equivalent concept yet.
4. **Should `panelKind` move out of `internal/cliapp` entirely?** It
   would make the registry cleaner, but it touches 25 constants and
   every `panel.kind` comparison in the tests.
