# Cycle 381 — the `internal/cliapp` panel split

## The problem

`internal/cliapp` was the larger of the two god packages cycle 379
named: **28 196 production lines over 139 files**, and the whole TUI
surface lived in it. The turn loop, the transcript, the streaming, the
input line, the viewport scrolling, the command dispatch — and, mixed
into all of it, **twenty-three modal panels**.

The panels were the reason the package could not be split along any
other seam. A panel is not a host concern: it is a self-contained
interactive surface with its own state, its own rows, its own keys and
its own writes. `/config` typed into its own search box and changed its
own setting; `/hooks` and `/permissions` each ran a three-stage editor;
`/tasks` had a list and two detail views; `/plugin` had two tabs, a
search box and a shelf of installable plugins. None of that needed the
runMode, and all of it needed it, so it all sat there.

The cost was not only size. Every panel reached the host's *shared*
state — `m.panel.cursor`, `m.panel.window`, `m.panel.tab` — so two
panels could move one another's cursor, and the host's generic list
navigation could reach a panel that had already answered the key
itself. `/tasks` was the visible symptom: the reference binds the
arrows inside `/tasks`, the host's generic navigation never got there,
and the cursor stood still.

## What was done

Fifty-three tranches, in fifty-three commits — one panel family per
commit, so every step was reviewable and green on its own. The
tranches are not in the package order: they follow the panels' own
size, and the small static frames came last because they were the
cheapest. The last six are not panel moves: they are the tree-wide
dead-export sweep, the retirement of the orphaned `internal/rctunnel`
supervisor half, the host's own remaining one-line forwarders, and the
subagent-question picker. The 36th tranche is the `/help` overlay —
the last modal surface the host carried, and the last one whose state
was spread over three files.

The five tranches that followed (37–41 in the numbering below) are the
turn of the cycle from *panels* to *facts*: the status row, the turn's
empty-turn classifier, the transcript's pure geometry, the composer's
selection geometry and the transcript's row vocabulary. Each one moved
a fact to the tier that owns it, and each proved it with a diff of two
dumps.

The twelve tranches after them (45–57) continue that turn. The dead-code
guard grew teeth and took thirty declarations with it; the tier guard
moved into the tree as a test; the transcript's notice rows, the status
line's payload, the viewer documents, the bash block, the themed banner
and the eligibility notice each found a home below the host; the voice
dictation's state machine moved next to the views it decides between;
the composer's selection arithmetic and the prompt history moved to
tier 5; the panel registry's key routing moved into the registry; and
the three session flags' policies moved down to tier 1. Every one of
them proved itself with a diff of two dumps.

| commit | what it did |
|---|---|
| `45ec33f2` | the transcript's tool-step model → `internal/ui/toolsteps` |
| `025a039b` | the /resume picker → `internal/ui/resumepicker` |
| `a1d63d4c` | the /provider panel → `internal/ui/views/providerpanel` |
| `b5727f11` | the /config panel → `internal/ui/views/configpanel` |
| `f9d215df` | the /model picker → `internal/ui/views/modelpicker` |
| `c5b5578d` | the /accounts and /pool panels → `internal/ui/views/accountspanel` |
| `d9d58fc9` | the /plugin panel → `internal/ui/views/pluginsui` |
| `978e25ce` | the /skills picker → `internal/ui/views/skillsui` |
| `675a2a5f` | the /permissions editor → `internal/ui/views/permissionspanel` |
| `6eeee157` | the /hooks editor → `internal/ui/views/hookspanel` |
| `5976ae2f` | the /agents picker → `internal/ui/views/agentspanel` |
| `3bc32594` | the /feedback panel → `internal/ui/views/feedbackpanel` |
| `f1526946` | the /credits panel → `internal/ui/views/creditspanel` |
| `732ef0cc` | the three viewport viewers → `internal/ui/views/viewerpanel` |
| `f615fc8f` | the forwarding shells over contextpanel and rewindpanel |
| `b6d88759` | the /tasks panel → `internal/ui/views/taskspanel` |
| `c1bea802` | the /codesearch panel → `internal/ui/views/codesearchpanel` |
| `4612a96e` | /rename and /add-dir → `renamepanel`, `adddirpanel` |
| `49310008` | the /effort slider → `internal/ui/views/effortpanel` |
| `dcd6b19d` | the /mcp and /artifact frames → `mcppanel`, `artifactpanel` |
| `b93bdb67` | the /rewind picker → `internal/ui/views/rewindpanel` |
| `959fdba2` | the /context panel → `internal/ui/views/contextpanel` |
| `ec43b0c6` | the dead forwarding shells in `internal/cliapp` |
| `1e01b49e` | the /usage panel → `internal/ui/views/usagepanel` |
| `2d7a74a6` | the /diff panel → `internal/ui/views/diffpanel` |
| `1a52f990` | the viewer facts move to the panels that own them |
| `009afd62` | the host's dead code goes, and a guard keeps it gone |
| `83f9db91` | the ui tier's dead exports go |
| `2167b33b` | the tree-wide dead-export sweep outside `internal/ui` |
| `13b52a88` | the rctunnel supervisor half retires; `internal/remotecontrol` swept |
| `eaca7ab6` | the last three dead exports of the production tree |
| `e581ebf1` | the marketplace link's duplicate endpoint alias |
| `33e502eb` | the host's 31 `tuilib` forwarders |
| `f99d2500` | the host's remaining 71 one-line forwarders |
| `b6f7b482` | the subagent-question picker → `internal/ui/views/subagentquestionpanel` |
| `9db7bb80` | the /help overlay → `internal/ui/views/helppanel` |
| `80d964d1` | the status row → `internal/ui/views/statusrow` (the 40th tranche) |
| `551716d4` | the turn's empty-turn classifier → `internal/app/agentturn` |
| `b79b46ce` | the transcript's pure geometry → `internal/ui/foundations/viewport` |
| `b06fbbde` | the composer's selection geometry → `internal/ui/foundations/inputselection` |
| `d64f39b9` | the transcript's row vocabulary → `internal/ui/transcript` |
| `cad8ea17` | the dead-code guard grows teeth; thirty dead declarations go (the 45th tranche) |
| `bad48768` | the tier guard moves into the tree → `internal/core/architecture` |
| `912ba179` | the transcript's notice rows → `internal/ui/rows` |
| `8c26e9e9` | the viewer documents → their own homes |
| `838d4356` | the status line's payload and runner → `internal/ui/views/statusline` |
| `ea261176` | the bash-mode block → `internal/features/tools/bashmode` |
| `5a7723b4` | the themed startup banner → `internal/ui/views/seasonal` |
| `b6de913d` | the eligibility notice → `internal/ui/views/announce` |
| `a7a428b4` | the voice dictation's state machine → `internal/ui/views/voice` |
| `68197af1` | the composer's selection arithmetic and the prompt history → tier 5 |
| `8e2bdda1` | the panel registry's key routing → `internal/ui/panelregistry` |
| `9b63e7c4` | the project, browser and log-file policies → `internal/core/cliflags` |
| `dca32c08` | the vim key translation → `internal/ui/editor/vimkeys` |
| `930b60ab` | the model argument resolution → `internal/ui/views/modelargpopup` |
| `edd14f0b` | the transcript's document mechanics → `internal/ui/transcript` |
| `10625070` | the attachment and task-notification rows → `internal/ui/rows` |
| `3141384f` | the accounts registry owns the quota snapshot |
| `0404e55f` | the composer's text editing → `internal/ui/editor/composer` |
| `c807244e` | the session layer owns the rewind policy → `internal/app/session/rewind` |
| `5e339f91` | the status line package owns its rows → `internal/ui/views/statusline` |
| `efb2a8b3` | the markdown-agent catalogue leaves the engine host → `internal/features/agents` |
| `2cc8e40f` | the conversation store owns the trajectory → `internal/app/session/convstore` |
| `44e62fef` | the second owners of the default model, the width and the scroll rule |
| `d8c456c4` | the slash grammar gets one owner → `internal/core/slashparse` |
| `df495bfc` | the server owns its bind address → `internal/app/agentserver` |
| `8e0defa6` | go.mod: catwalk is a direct dependency |
| `02c54777` | the transcript's repaint engine → `internal/ui/transcript` (the 87th tranche) |
| `ae037473` | the live-step batch mechanic → `internal/ui/transcript` + `internal/ui/toolsteps` (the 91st tranche) |
| `edc286e9` | the slash-command vocabulary → `internal/ui/slashcommands` (the 88th tranche) |

### 1. The panel pattern

Every tranche wrote the same shape, and the shape is now the rule:

- **the package owns the panel** — its state, its rows, its keys and
  its writes. It lives at `internal/ui/views/<name>/`, with
  `panel.go`, `panel_render.go`, `panel_keys.go` and `panel_test.go`.
- **the host keeps facts, not behaviour** — a `Seams` struct of
  callbacks, or a `State` of values, assembled in
  `internal/cliapp/<name>_host.go`. The host routes the panel's keys
  and asks the panel for its rows.
- **the panel is rebuilt on every open**, so a stale cursor, a stale
  stage or a stale form text is never shown.
- **a panel answers `handled=false` for the keys it does not own**, so
  Ctrl+C still reaches the global handlers.
- **the panel reads `palette.*` when it draws**, so a row follows the
  selected colour scheme without a refresh hook.

Two variants exist, and both are deliberate:

- A **list panel** (a cursor over rows) owns its cursor and its scroll
  window. The host's shared `m.panel.cursor` / `m.panel.window` are no
  longer used by any panel that owns its own.
- A **viewer panel** (a document with a window over it) does *not* own
  the offset: the host's panel struct carries one window that
  `/codesearch` and the `/tasks` detail views scroll through as well,
  so the viewer takes the offset as an argument and hands the clamped
  one back.

### 2. The packages

Seventeen packages are new — fifteen under `internal/ui/views/` and two
under `internal/ui/`:

| package | owns | production | test |
|---|---|---|---|
| `internal/ui/toolsteps` | the transcript's step model, its four tea messages and the styled rendering | 688 | 982 |
| `internal/ui/resumepicker` | the /resume picker's state, its rendering and its keys | 1 356 | 598 |
| `internal/ui/views/accountspanel` | the /accounts panel and its Pool tab | 1 380 | 759 |
| `internal/ui/views/agentspanel` | the agent picker and the running-subagent section | 472 | 284 |
| `internal/ui/views/artifactpanel` | the /artifact frame | 46 | 55 |
| `internal/ui/views/creditspanel` | the credits panel's two actions | 200 | 167 |
| `internal/ui/views/effortpanel` | the effort slider's geometry, the tier names and the /effort panel | 612 | 475 |
| `internal/ui/views/feedbackpanel` | the two feedback stages | 406 | 370 |
| `internal/ui/views/hookspanel` | the three hook stages | 518 | 298 |
| `internal/ui/views/mcppanel` | the /mcp frame | 70 | 58 |
| `internal/ui/views/permissionspanel` | the three permission stages | 589 | 367 |
| `internal/ui/views/renamepanel` | the /rename input | 267 | 185 |
| `internal/ui/views/adddirpanel` | the /add-dir input | 203 | 188 |
| `internal/ui/views/taskspanel` | the /tasks list and its two detail views | 690 | 554 |
| `internal/ui/views/codesearchpanel` | the /codesearch panel | 650 | 463 |
| `internal/ui/views/diffpanel` | the /diff panel: the three tabs, the inline diff subview and the per-hunk fold state | 1 112 | 1 453 |
| `internal/ui/views/viewerpanel` | the viewport, the pager keys and the scroll arithmetic | 245 | 206 |

Nine packages that already existed grew into their panel's other half:

| package | production | test |
|---|---|---|
| `internal/ui/views/pluginsui` | 848 → 2 223 | 314 → 4 123 |
| `internal/ui/views/providerpanel` | 1 007 → 2 982 | 995 → 2 092 |
| `internal/ui/views/configpanel` | 380 → 1 305 | 265 → 1 588 |
| `internal/ui/views/skillsui` | 1 135 → 1 663 | 286 → 1 885 |
| `internal/ui/views/modelpicker` | 870 → 1 114 | 647 → 1 243 |
| `internal/ui/views/rewindpanel` | 135 → 486 | 144 → 423 |
| `internal/ui/views/contextpanel` | 527 → 650 | 451 → 711 |
| `internal/ui/views/usagepanel` | 313 → 482 | 285 → 448 |
| `internal/ui/views/changelog` | 441 → 498 | 83 → 111 |

One package shrank: `internal/ui/foundations/viewer` went from **202 →
77** production lines and **91 → 54** test lines. It used to be a
"generic" package that held three different panels' facts — the
changelog panel's excerpt, the `/usage` panel's quota countdown and
tick, and the diff panel's inline row. Every one of them had exactly
one panel's user, which is a package with no owner. Each now lives with
the panel it belongs to; what is left in the package is the machinery
all of them share: the document spec, the window arithmetic and the
counter row.

The host shrank accordingly: **28 196 → 18 754 production lines**
(−9 442) over **139 → 132 files**, and its test lines went from
**46 514 → 32 543** — the tests that moved with their subjects were
more than the ones that came with them, because the host's duplicates
went (cycle 379's rule).

The 26th tranche is the first to give a line back: the compaction
spinner's tick moved *into* the host (it had been in the shared viewer
package, which is where the bug came from), and the changelog and usage
shells went away. The host's production lines went from 18 754 to
**19 011** over 134 files — of which 225 belong to a concurrent agent's
commit that landed in between (see limitations).

The 27th tranche is the first to remove *code* rather than move it:
**twenty-nine functions** that nothing called any more are gone, and
the host's production lines went from 19 011 to **18 729** over 90
files. The 45th tranche removed **thirty more** of the same kind, once
the guard stopped counting a test file as a caller (section 16).

The Go compiler cannot catch this class of defect — it complains about
an unused import or an unused local variable, never about an unused
function — so the host had quietly accumulated two dozen of them,
several of them lying in their own comments about who called them.

### 3. The shells

Two tranches were pure retirement, and they are the reason the host
looks smaller than the panels it lost:

- `f615fc8f` retired thirteen forwarding wrappers and two type
  aliases over `contextpanel` and `rewindpanel` — nine of them with no
  caller at all.
- `ec43b0c6` retired twenty-one more: the dead forwarders over
  `diff/action`, `changelog`, `bashmode`, `narration` and
  `sidequestion`, plus the `narrationSampleSeeds` alias that carried a
  `var _ =` keep-alive.

Both tranches retired the shells **nobody called**. The ones that were
still alive are the 34th tranche's business (section 5), and the
host's forwarder count is now zero: the scan for the same shape — a
one-line forward into another package — finds only the key bindings,
the test helpers and the real `tea.Cmd` producers, none of which is a
shell.

### 4. The tree-wide dead-export sweep

The host's own dead code was the first target, and it was the easy
one: the guard in `internal/cliapp/deadcode_test.go` catches an
unexported function with no caller. The **exported** surface is the
other half of the problem, and no guard covers it — an exported
function may be called from another package, and the compiler never
looks.

**The sweep is instrumented, not grepped.** A `go/parser` tool
resolves every `pkgpath.Symbol` through the import map, so a name
counts as live only when a call site really names it. Bare-name
`rg` sweeps produce both false positives (a name that appears in a
string) and false negatives — during this cycle a truncated
`grep … | head -4` hid a *live* use of `printSlashSkillInfos` and
nearly deleted a function the tree still calls. The instrument is not
committed, as in cycles 378, 379 and 380.

**The scope** is the production tree, minus the evidence packages
(`internal/cortex`, `internal/language_server`, `internal/planrender`),
the generated protobuf packages, the `out/` reconstruction sources, the
public top-level packages (a compatibility promise — `palette`,
`grpc`, `agentlink`, `browserbridge`, `lifecyclehooks`, `budget`, …)
and `scratch/`, which has never compiled.

**A dead export has three possible ends**, and the sweep decides
which one applies:

- **delete it** — dead, and with no reference provenance;
- **wire it** — where a *literal duplicate* already existed, byte for
  byte, the call site now names the symbol instead of repeating the
  literal;
- **keep it** — a recovered reference string or behaviour, or a
  guide-protected alias. The reason is recorded *here*, not left to
  the next reader to work out.

#### What went

Tranche 29 removed **about eighty-five exports over some sixty
files**, and five whole files or packages that nothing imported:
`internal/cli/steps.go` (the orphaned reference `--output-format=steps`
engine — the evidence stays in `out/local-macos/gosrc/cli/steps/`),
`internal/cli/logfile.go`, `internal/features/auth/has_token.go`,
`internal/infra/backend/native/` (the live native provider is
`internal/piagentbridge/native_provider.go`) and
`internal/infra/backend/grpcmock/` (the live mock is in the public
`grpc` package).

Tranche 30 retired the **orphaned `internal/rctunnel` supervisor
half**. Five files went (`supervisor.go`, `proxy.go`,
`runtime_config.go`, `discovery.go` and their test), and the last of
`types.go` went with them: `Instance`, `Instance.BaseURL`, `Config`
and `NewCSRFToken` had no caller left. What remains of `rctunnel` is
the client, the HTTP proxy, the instance update and the wire types —
plus `rctunnel/patches`, which `internal/rcfrontend` still uses.

The same tranche swept `internal/remotecontrol`: `DefaultListBackend`
(the live wiring is the host's `buildHostListBackend()`), the dead
`plugins.StateWireFormat` and `triggers.StateWireFormat` copies of
the live `permissions.StateWireFormat`, and the `terminals.State`
type with its `StateVersion` — the terminals domain has no on-disk
store at all, the registry is in-memory only.

#### What was wired

Three call sites repeated a literal that a named constant already
carried: `internal/features/models/catwalk.go` now names
`HyperProviderID`, `cmd/harness/{sidetools,events,prompt}.go` now
name `harnesstools.ToolInvokeSubagen` / `ToolSendMessage`, and
`internal/features/models/ctx/profiles_1.4.8.go` now names the
`Strategy*` constants — that enum was dead only because the literals
shadowed it.

#### What was wired the other way round

`permissions.LSRoute` was the one dead export in the sweep that was a
*missing wire* rather than a leftover. The permissions domain already
had its own language-server table, and the host's dispatcher was
re-implementing the same two cases inline — naming the domain's method
names in the dispatcher's own switch. The dispatcher now asks the
domain, which is the shape every other domain already has
(`dispatch_route.go`): **the dispatcher keeps the fan-out, the domain
keeps the table.** The "language-server table is consulted first"
ordering and the flat-vs-nested `PermissionSettings` note are
preserved verbatim.

#### What was kept, and why

| symbol | why it stays |
|---|---|
| `internal/features/auth`'s endpoint aliases | a guide-protected alias (`docs/guide/endpoints.md`); the name is the contract |
| `internal/infra/codeassistclient`'s eight reconstruction symbols | a recovered reference surface — the names are the evidence |
| `permissions.CycleHint`, `permissions.NoToolCallNudge(+Headless)` | a recovered reference string the clone does not implement |
| `convstore.ResumeHintBanner` | the v1.4.0 resume hint, a recovered reference string |
| `files.ErrTooLarge` | a documented error that the `-32603` mapping never implements |
| `cascades.StatePhaseWorking`, `InteractionKindChoice`, `InteractionKindConfirmation`, `NotifStreamRemoteAgentStateUpdates` | a closed set the reference names; the clone emits only part of it |
| `remotecontrol.AuthMethodADC` | 【反编译】 the reference's own `auth.(*adcAuth).Name` (`out/local-macos/dec/10272f980_Name.c`) |

Deleting a symbol that carries reference provenance would delete the
*record* of what the reference does, and the clone's own report would
then claim a behaviour it never had. The keep list is the honest
answer: **the clone does not implement these, and the record says so.**

### 5. The host's remaining forwarders

The sweep (section 4) finds code **nobody calls**. A forwarder with a
caller is alive — and it is still a shell: invariant 68 says "a shell
is a shape, not a filename", and a one-line forward into another
package is that shape whether or not anything calls it. The host was
full of them: the panels had moved out, and what they left behind was
a vocabulary layer over the packages that had taken their rows.

The 34th tranche retired the last of them: **70 host functions** (69 in
production files, one test helper), **5 type aliases** and **7
package-level constants and variables** — 82 symbols in all. The host's
production lines went from 18 517 to **18 109** over 91 files.

62 of the 70 were mechanical: the body was one call with the
parameters passed straight through, so the call sites name the package
directly. Four of those 62 were *value-use* — the forwarder was not
called but handed to somebody as a value (a function field, a seam
variable) — and the seam names the package's own function instead.

The other eight needed a hand, and each of them had one clean
retirement:

| forwarder | what it was | where it went |
|---|---|---|
| `bashBlockLines` | the block's rows | a method on the host's own `bashBlock` (`(*bashBlock).lines`) |
| `bashPromptGlyph` | the prompt's glyph | `bashmode.PromptGlyph(inputView, cBlueOpen)` at the call site |
| `computeCompactTokens` | the compact token counts | `compaction.ComputeCompactTokens(…)` inlined at the one call site |
| `isReferenceModel` | "is this a catalogue model?" | `backendselect.IsReferenceModel(model, modelpicker.ResolveCatalogueID)` |
| `openFeedback` | `/open`'s feedback | `editorkit.OpenFeedback(…, diffaction.PathExists)` |
| `pickBackendWithLog` | the transport split | `backendselect.PickWithLog(…, modelpicker.ResolveCatalogueID)` |
| `pickBackend` | the transport split | `backendselect.Pick(…, modelpicker.ResolveCatalogueID)` |
| `seasonalEvent` | the event in season | `seasonal.Current(seasonalNow())` at both call sites |

`resolvePrintMode` was the second shell-of-a-shell: it called
`flagWasSet`, which was itself a forwarder over `cliflags.WasSet`. It
names `cliflags.WasSet` directly now.

The host's facade over the mention package went with them:
`AtConversationSource`, the `atConversation*` aliases, the `atSource*`
constants and `defaultAtConversationSearcher` — twelve symbols that
named `internal/app/prompt/mentions/conversation` and nothing else. The
`EligibilityError` alias went too: `type EligibilityError =
eligibility.Error` is the same shape as a forwarding function, and the
tests now name `eligibility.Error`.

**The facts move down, or they die with the shell.** A retired
forwarder's doc comment was the only place some of the tree's facts
were written down, so every doc sentence was checked against the
target's. Five names survived the check at the end of the tranche:

- three are mechanical ("delegates to the package's `X`");
- one is the atotto/clipboard provenance, which moved into the
  clipboard package's own header;
- and one is **false**: the retired host forwarder `IsBusinessAccount`
  said its copy was "for the first-run opt-in and the account
  switcher". Neither surface exists. The host's three call sites gate
  `/remote-control` under ADC, and the `/config` telemetry row reads
  the signal through `internal/features/auth` directly — the accurate
  fact was already on `auth.IsBusinessAccount`. The claim is dropped,
  not moved.

### 6. The subagent-question picker

The picker was the last modal surface in the host that was not a slash
panel, and it was the host's own worst piece of shared state: **eight
fields of the runMode** — the scope, the open flag, the two cursors,
the staged answer, the alert text and the answer buffer — plus a
365-line file that drew them.

Two of the eight were **write-only**. `subAgentQuestionAlert` was
assigned on open and read by nothing; `subAgentQuestionAnswerBuffer`
was cleared on two paths and read by nothing. The test that asserted
the buffer was empty was therefore vacuous: it asserted that a field
nobody wrote was still empty.

The 35th tranche made the picker a panel,
`internal/ui/views/subagentquestionpanel`. The panel owns the scope,
the open flag, the two cursors, the staged answer, the alert rows, the
picker rows, the keys and the answer the Manager receives. The host
keeps the **Manager** — the session owns it, and the tool seam
(`tasks.AskQuestion`) enqueues into the same store — and supplies the
three seams the panel cannot answer itself: where a row is echoed, what
the composer holds, and how it is cleared.

The runMode's field count went from 193 to **187** (the guard's cap is
195), and the host's production lines from 18 361 to **18 026** over 91
files.

#### The two exceptions, and why they are not exceptions

The panel rule says a panel is rebuilt on every open and answers
`handled=false` for the keys it does not own. The picker does neither,
and both departures have the same cause: **its staged answer outlives
the overlay.**

The answer is typed into the *composer's* prompt, not into a modal of
the panel's own, and the composer's submit path is what sends it. A
panel rebuilt on open would take the staged answer with it, so this one
is built once with the session. The cursors are still reset when the
picker opens.

And while the picker is up it swallows the keys under it — including
Ctrl+C — which is what the host's own overlay did before the move. The
host's key pipeline asks the panel (`HandleKey`) and the panel answers
whether it took the key; a closed picker takes none.

#### The bytes are pinned

A move that changes a byte is a change. The panel asserts the alert
rows, the picker rows, the "no pending questions" notice, the
"> Answer for …" prompt and the two confirmations **byte for byte**
against the host's own output, taken from the implementation the panel
replaces. The comparison caught one: the keyboard hint row's last
segment was written with the *closing* dim colour where the reference
writes the *opening* one, so the " Cancel" text was drawn in the wrong
colour.

### 7. The /help overlay

`/help` was the last modal surface the host carried, and it was the
only one whose state was **spread over three files**: the tab and the
open flag lived in `tui_run.go`, the list cursor and the scrolling
window in `history.go`, the tab list and the vim tab in `vim_mode.go`,
and the three renderers in `tui_help.go`. Four `runMode` fields, four
files, one overlay.

The 36th tranche made the overlay a panel,
`internal/ui/views/helppanel`. The panel owns the tab, the cursor, the
window, the rows and the keys; the host keeps the facts the panel
cannot know — the model's name, the tier, the working directory, the
email, the conversation id, the terminal's size and whether vim mode is
on — and hands them to the panel as seams. The host's `/help` slash
command and its `?` keybinding only open it.

The overlay is **modal**: while it is up it claims every key it
receives, so nothing reaches the composer or the host's global handlers.
That is the same exception class as the picker (invariant 101) and for
the same reason — it is what the reference's own overlay did.

The **exit echo moved with the overlay**: the panel's `Cancel()` writes
the "/help" block and the "Exited /help command" line, and both the
host's Esc path and the panel's own "q" go through it, so the two
paths write the same bytes. The host keeps no echo of its own.

The runMode's field count went from 187 to **184** (the guard's cap is
195), and the host's production lines from 18 026 to **17 745** over 92
files (the host gained `help_host.go`).

#### The host's colour aliases went with it

The host kept a second copy of the overlay's colours — `helpHeaderOpen`,
`cHelpDescOpen`, `helpBlueOpen`, `helpDimGrayOpen`, the tab bar's eight
aliases and `helpKeyboardHint` — and `renderer.go` re-read all of them
from `tuilib` whenever the scheme changed. That is a copy of a fact
`tuilib` already owns, and `tuilib` refreshes its own aliases on the
same event, so the panel names `tuilib`'s colours directly and the
host's refresh lines are gone. A second copy of a colour is a copy that
goes stale when a panel is added.

#### The bytes are pinned, and the proof was a diff of two dumps

The pre-tranche tree (`88a0c70f`) rendered the four tabs; this panel
renders them; the two dumps are **byte for byte identical**. The only
difference was the log file's timestamp — the clock talking, not the
renderer.

The GENERAL, SHORTCUTS and VIM tabs are pinned in full in the panel's
own test. The COMMANDS tab renders the clone's whole slash-command
catalogue — 109 rows today — and that catalogue grows with every new
command, so a full pin would fail on a feature that has nothing to do
with the overlay. It is pinned by **shape** instead: the tab bar, the
section header, the number of visible rows, the counter's format and
the footer, byte for byte, plus the cursor's marker on its own row.

The dump harness is kept (`panel_dump_test.go`): it writes the panel's
rows to a file named by `HELP_DUMP`, so the next comparison of this
kind has a way to produce its input.

### 8. The slash-command popup

The composer raises a completion popup when the buffer starts with `/` —
the rows of the matching slash commands, the scroll window, the
`/model <query>` model list, and the on-screen rectangle the mouse
handlers answer. The state was **spread over two packages**: the filter
and the state machine in `internal/ui/tuilib` (`PickerState`,
`PickerKind`, `ModelRow`, `readToken` — 361 lines), the layout and the
rectangle next to them (`PickerLayout`, `PickerGeom` — 59 lines), and the
rendering, the mouse handling and the commit in `internal/cliapp`
(`picker_view.go` — 364 lines).

The 37th tranche made the popup a panel,
`internal/ui/views/slashpicker`. The panel owns the filtered rows, the
highlight, the scroll window, the on-screen rectangle, the rows it
writes, the keys it handles (Up, Down, Tab, Esc) and the mouse events it
consumes (click, hover, wheel). The host keeps the facts the panel cannot
know — whether the alternate screen is up, the terminal's width, which
screen row is the composer and what the composer holds — and hands them
over as seams.

**The catalogue stayed where it was.** `tuilib.SlashCommand`,
`DefaultSlashCommands`, `FilterSlashCommands` and `containsFold` are the
catalogue: the list of what the reference offers. The popup is the view
over that catalogue. The two had grown into one package, and the view is
the half that moved.

**The popup is not modal.** It is a completion popup over a *live*
composer: the letters the user types have to reach the composer, so the
popup owns the navigation keys and nothing else, and the keys it does not
own fall through. That is the opposite of the `/help` overlay (tranche
36), which claims every key while it is up.

The host's five picker fields — the state, the scroll offset, the
layout, the body's first row and the rectangle — collapse into one
`*slashpicker.Panel`: the runMode's field count went from 184 to **180**
(the guard's cap is 195), and the host's production lines from 17 746 to
**17 427** over 93 files.

#### The colours follow the scheme, so the panel refreshes them

The popup's four colours (`pickerNameHi`, `pickerDescDim`,
`pickerLabelDim`, `pickerKeyOpen`) were the host's aliases, re-read from
the palette whenever the scheme changed. The panel reads the palette
directly now, and — because a copy made at init would keep the old
scheme's escape — registers its own `palette.OnSchemeChange` callback.
The host's refresh lines went with the aliases.

#### The rectangle is the panel's

The host used to compute the popup's on-screen rectangle out of the rows
it had just painted (`pickerLayoutLocal`, `pickerBodyStart`,
`pickerGeom`). Now the host tells the panel where its rows start
(`SetBodyStart`) and how many rows the body lost to the clip
(`ClipBody`), and the panel answers the rectangle; the mouse handlers map
an (x, y) onto a row with it. The host keeps no geometry of its own.

#### `type atFileDropdown = atfiles.Dropdown` was a shell

A type alias to another package's type is a shell (invariant 98), and
the field names the type directly. The mention dropdown's rows are the
dropdown's own.

#### The `/model` argument and the `@` prefix left the file they shared

`internal/cliapp/picker_model.go` held two unrelated things: the
`/model <query>` argument handling and the `@` prefix — the named
subagent dispatch and the `@` file-mention dropdown. Neither of them is
the slash-command popup, so the second moved to
`internal/cliapp/prompt_at.go`.

#### The bytes are pinned, and the proof was a diff of two dumps

The pre-tranche tree (`ec5c7985`) and this tree rendered the popup for
the same buffers (`/`, `/a`, `/xyz`, `/clear`, `/model`, `/model `,
`/model claude`, `/btw`) and the same scroll positions (1, 4, 5, 8, 10
and 30 Down presses): the two dumps are **9289 bytes each and byte for
byte identical**.

The panel's own bytes are pinned in
`internal/ui/views/slashpicker/panel_bytes_test.go`: the "No matches"
branch, one command row, the model rows and the keyboard hint row.

### 9. The "@" file-mention dropdown

The composer's "@" opens two different things: the named-subagent
dispatch — the host's own business, it delivers a message and echoes
the reference's answer — and the **file-mention dropdown**, the
overlay that lists the workspace's files and completes the mention
into the composer.

The dropdown was a **widget in the app tier**:
`internal/app/prompt/mentions/files/dropdown.go` (134 lines) and
`dropdown_render.go` (212 lines). The host drove it through 36 call
sites, and the dropdown's own colours (`ColorNameHi`, `ColorDescDim`,
`ColorKeyOpen`, `ColorKeyClose`) and its two exported renderers
(`RenderRow`, `HintRow`) were public API of a package that had no
other user.

The 38th tranche made the dropdown a panel,
`internal/ui/views/mentionpicker`. The panel owns the candidate list,
the highlight, the mention token's span, the rows it writes and the
keys it handles (Up, Down, Tab). The host keeps the facts the panel
cannot know — what the composer holds, the workspace root the
candidates are listed from, the terminal's width — and hands them over
as seams.

**The facts stayed in the app tier.** `Parse` (the mention grammar),
`List` (the workspace listing) and `FilterEntries` (the ranking) are
not view facts, and a panel may import the tier below it — fourteen
ui-tier files already do. The app tier's package went from 690
production lines to **345**.

**The host's two helpers are gone.** `tabSpliceAtFile(keepOpen bool)`
was the host's word for "complete the mention" and `atFileView()` was
its word for "the rows"; the panel's words are `Complete(keepOpen
bool)` and `Rows()`. The ten call sites that re-derived the dropdown
from the buffer and the cwd became one `m.atFilePicker.Update()`.

**The dropdown's dead `empty` field is gone.** `Empty()` always
answered from the candidate list, so the field was written on every
keystroke and never read.

**Enter is routed by the host.** The dropdown's Enter is swallowed
even when the dropdown holds no candidate — the prompt is not
submitted — but the host's Enter path carries the newline, the
queue-mode and the submit branches, so the panel cannot claim the key
wholesale. The host asks the panel to complete and keeps the history
reset, which is the host's fact.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`99d2dad7`) and this tree rendered the dropdown over
the same fixture workspace: the same eleven buffers (`@`, `@ma`,
`@mai`, `@ot`, `@cmd/`, `@cmd/wagyu/`, `@with space/`, `look at @ma`,
`@zzznope`, `@main.go`, `@internal/pkg/sub/`), the same Tab and Enter
completions and the same visible/empty/len/idx facts produce the same
**5718 bytes**.

The panel's own bytes are pinned in
`internal/ui/views/mentionpicker/panel_bytes_test.go`: the query mode,
the placeholder listing and the listing's first row.

### 10. The active-items block

Under the prompt box the reference draws the work the session has in
flight: the running subagents and the running shells, one line each,
with a cursor and a window over them. The block is not a modal panel —
it has no frame and no legend — but it is a surface with its own state,
its own rows and its own keys, and it was a **widget in the host**:
`internal/cliapp/tui_active_items.go` (273 lines) held the
`activeItemsModel` type with its cursor, its scroll offset, its focus
flag and its window, and four more functions around it —
`getActiveItems`, `isHideActiveItems`, `renderActiveItem` and
`newActiveItemsModel`.

The 39th tranche made the block a panel,
`internal/ui/views/activeitemspanel`. The panel owns the cursor, the
scroll window, the rows it writes and the keys it handles (↑, ↓, Esc,
Enter). The host keeps the facts the panel cannot know — the task
registry the rows come from, the terminal's size, whether vim mode is
on and the way into the `/tasks` detail view — and hands them over as
seams (`internal/cliapp/active_items_host.go`, 36 lines).

**The facts stayed in the app tier.** The registry now answers which
records are in flight — `Registry.Active()`, new in
`internal/app/task/taskregistry.go` — and it still draws a single row
(`ActiveRowLine` for a task, `ActiveSubagentRowLine` for a subagent).
The registry is the app tier's, and the panel may import the tier below
it.

**The gate moved with the surface.** "Show Active Tasks" is the
setting that switches *this block* off, so the block reads it: the
panel's `Hidden()` is the host's old `isHideActiveItems()`. The host
asks the panel two separate questions, because they are two separate
questions: `Hidden()` (is the block switched off?) and `Count()` (how
much is in flight?). The alt+j gesture toggles the focus on a *running*
task even when the block is hidden — the host's `Count() > 0` test is
ungated, exactly as it was — while the prompt's ↓ only hands the focus
over when the block is visible *and* non-empty.

**The host's field became a pointer.** `m.activeItems` was a value
(`activeItemsModel`); it is a `*activeitemspanel.Panel` now, and every
method of the panel is safe on a nil receiver: a dozen host tests
build a `&runMode{}` by hand, and a panel that panicked on a nil
receiver would take them all down.

**One method did not make the move.** `SelectLast(items)` had no
caller — the host never selected the last row. As a method of an
exported type it would have been a dead export, so it went with the
widget (invariant 94).

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`65534493`) and this tree rendered the block over the
same registry: the same rows (a running task, a running subagent, a
pending task), the same "↑ N more" / "↓ N more" tails on a short
terminal, the same cursor trajectory over ↓/↓/↑/Esc and the same
focus/cursor/count facts after every key produce the same **3 259
bytes**.

The dump masks two cells that move on their own — the row's time cell
("[15:04:05]") and the subagent row's elapsed cell (" · 15s") — because
they carry the clock. It also had to give every task a **distinct**
start time: the registry's rows are read from a map and sorted with an
unstable sort, so two tasks that started in the same second come out
in an arbitrary order. That is the clone's own behaviour and it did
not change — the pre-tranche tree produces a different order on two
consecutive runs of the same fixture — but it makes a byte-for-byte
comparison of two trees impossible without distinct timestamps.

### 11. The status row

Under the prompt box the reference draws one line that carries the
session's identity and the hint the user needs right now:

```
? for shortcuts                    Gemini 3.8 Flash · high · 2 tasks
esc to cancel                      auto · MiniMax M3
press ctrl+c again to exit         MiniMax M3
⠿  Compacting context… (12s)        MiniMax M3
```

It is the smallest surface in the TUI and the most crowded: the left
half is a *hint* whose text is decided by five rules (the slash popup,
a streaming turn, queued messages, a non-empty composer, the bash
mode) and then overridden by four more (the vim badge, the `/help`
overlay and the modal panels' clearing of it, the two-step Ctrl+C
confirmation, the compaction label); the right half is the model, the
`--agent`'s name around it, the mode, the effort tier and the running
tasks; and between them sits the padding that makes the line exactly
the terminal's width.

The 40th tranche made the row a package,
`internal/ui/views/statusrow` (310 + 57 lines). The package owns the
two segments, the rules that decide the hint, the padding arithmetic
and the compaction label. The host keeps the facts it alone knows and
hands them over as a `State` (`internal/cliapp/statusrow_host.go`, 88
lines): the model, the mode, the effort tier, the running tasks, the
compaction state, the composer, the pickers, the panels and the vim
badge.

**The row is a view, not a panel.** It has no keys and no writes of its
own — the host asks it for a line — so it takes a `State` of values
rather than a `Seams` of callbacks. The rule the cycle established
("a panel owns its state, its rows and its keys; the host keeps facts")
holds in its weaker form here: the row owns the rows, the host keeps
the facts.

**One number has one owner.** The row's canonical width is the host's
`separatorWidth` (220), the same width the session's rules are drawn
at. The host passes it in as `State.CanonicalWidth`; the row keeps no
second copy of the constant.

**The palette is read directly.** The row's dim grey comes from
`palette.DimGrayOpen` / `palette.DimGrayClose`. The host used to hold
aliases for it; the row does not, so a new scheme cannot leave a stale
copy behind.

**One fact moved down twice.** The compaction's elapsed time
(`compactDuration`) travelled with the label that prints it, into the
row's package. The token formatter (`"184 K"` / `"1.2 M"`) could not:
it lived in `internal/printmode`, which is an **entry surface above
tier 5**, and the ui tier may not import it. It moved down to a new
tier-1 package, `internal/core/tokenfmt`, below everyone. The old name
is gone — `printmode.TokenK` is not left behind as an alias, because a
one-line forward into another package is a shell.

**Two of the reference's own asymmetries were preserved on purpose.**
The right-padding count *excludes* `" · Queue mode"` in the built-in
case and *includes* it in the `--agent` case; and the static non-tty row
keeps its own literal grey (`38;2;154;160;166`) rather than the
palette's. Both look like mistakes and neither is one: the dump proves
that the pre-tranche tree and this tree agree on every one of them.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`24424b31`) and this tree rendered the row over the
same host: idle, a narrow terminal, an unknown width, a streaming turn,
the bash mode, a queued stream prompt, a queued shell command, a
non-empty composer, the exit confirmation, the four modes, the
`--agent`, the `--agent` with the queue mode, the queue mode alone, a
running task, the two open panels, the `/help` overlay, the two picker
states, the three compaction shapes, the vim badge, the vim badge with
a buffer, and the static non-tty row — the same **15 465 bytes**.

The dump masks one cell, the compaction's elapsed time (`(12s)`), because
it carries the clock.

### 12. The transcript's row vocabulary

The transcript's four record types — the tool-step block, the user
block, the assistant block and the live-step run — were the host's
private vocabulary. The 41st tranche gave them a home,
`internal/ui/transcript` (167 + 101 + 23 lines plus 220 lines of
tests), and the host now names them there.

**The names were shortened because they are now qualified.** The host's
`turnBlock` became `transcript.Block`, `userBlock` became
`transcript.UserBlock`, and so on. The move renames; it never
re-exports — a `type turnBlock = transcript.Block` alias in the host
would be a shell (a package-level alias into another package is the
shape the cycle retired), so the old names are gone and every call
site names the new package.

**The render cache stayed inside the package.** The block carries a
two-slot cache — the collapsed rows and the expanded rows, each keyed
by the width and the verbosity it was painted at. The host used to
reach into `b.cachedCol` / `b.cachedExp` / `b.cachedColW` /
`b.cachedExpV` and seed them itself; it cannot any more, and it must
not: a cache that the host can stomp is a cache with two owners. The
package exports `NewBlock(b Block, rendered []string, width int, verb
toolsteps.Verbosity) Block`, which picks the slot from `b.Expanded`
— exactly what the three old populating sites did by hand — and the
host keeps no second copy of the cache's rule.

**The bash block became an interface.** The block's one optional
payload is the bash command's own state (`cmd`, `body`, `failed`,
`running`), which stays in the host because the host owns the process
lifecycle. The transcript package cannot name the host's type, so it
declares the shape it needs:

```go
type BashBlock interface {
	Lines(expanded bool, height int) []string
}
```

The host's `bashBlock` satisfies it. The rows themselves were already
owned by `internal/features/tools/bashmode` — tier 2 owns the bash
block's *rendering*, and the tier-5 package names it.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`686fd3ed`) and this tree rendered the same things:
the user's prompt row over nine texts × three widths, the single-line
prompt row over the same nine texts, the turn separator at six widths,
and a block with 0/1/3 steps × expanded/collapsed × two widths × two
heights × the three verbosities, in three variants (uncached, cached
with the rows the caller painted, and cached-then-rewidened) — the
same **141 285 bytes**.

### 13. The turn's empty-turn classifier

Two of the host's turn facts did not belong to the host.

`agent.IsEmptyAssistantTurn(content, toolCalls, reasoning)` classified
an assistant turn as empty. It is a **classifier over the turn's own
content**, and the turn seam already lives in the app tier, so it moved
to `internal/app/agentturn` (`internal/app/agentturn/empty_turn.go`).
The old name is gone: `internal/agent/empty_step.go` held nothing else
and was deleted.

`agent.TurnErrorRow(err)` was one function doing two jobs: it decided
*whether* an error has a message, and it *rendered* the row. The split
followed the cycle's own rule — a classifier is a fact, a row is a view:

- `agent.TurnErrorMessage(err) (string, bool)` — the classifier, in
  the host, because it classifies the host's error type;
- `toolsteps.ErrorRow(message) string` — the row, in the ui tier,
  beside the other step rows.

`toolsteps.ErrorRow` is `"Error: " + strings.TrimSpace(message)`, and
the trim is part of the contract: the classifier returns the raw
message and the row normalises it.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`686fd3ed`) and this tree rendered the same ten
error shapes (nil, a plain error, an empty `Error()`, an HTTP 429, the
two auto-continue errors with and without their cause, a padded message,
a padded message with a newline) and the same eight
`(content, toolCalls, reasoning)` triples — the same **770 bytes**.

### 14. The transcript's pure geometry

Four of the host's transcript helpers were functions of their
parameters:

| helper | what it answers |
|---|---|
| `isPromptTurnSeparator(row)` | is this row the rule between two turns? |
| `promptTurnStart(rows, promptIdx)` | where does the prompt's turn begin? |
| `isPromptTurnVisible(rows, idx, scroll, userPromptRows)` | is this row inside the visible window? |
| `transcriptCellAt(x, y, avail, width)` | which transcript cell is this mouse position over? |

They were pure, so they moved to a new tier-5 foundations package,
`internal/ui/foundations/viewport`, beside the package that already
holds the viewport's cache and its scroll arithmetic. `internal/ui/
foundations/` holds *pure arithmetic over parameters*; that is
exactly what these four are.

**The mouse helper lost a hidden evaluation.** The old method read
`m.viewportHeight()` itself, and it returned early for a negative
coordinate *before* reading it. The pure function receives `avail` as
an argument, so the host now evaluates `m.viewportHeight()` at the
call site — a negative mouse coordinate now warms the height cache.
Bubble Tea never reports a negative coordinate, and the dump covers
negative `x` and `y` explicitly: the two trees agree.

**Three dead aliases went with them.** `wrapSpan`,
`pinnedPromptJumpHint` and `selHighlightClose` were one-line
forwards into the viewport package that nothing called. A one-line
forward is a shell whether or not anything calls it, so they are gone.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`686fd3ed`) and this tree computed the same things:
15 separator fixtures; 4 row sets × 5 prompt indices; 4 row sets × 3
prompt-row sets × 8 scroll offsets × 8 indices; and a 20-point
`(x, y)` grid × 9 `(height, width)` configurations — the same
**53 913 bytes**.

### 15. The composer's selection geometry

The composer's selection had four pure helpers of its own:

| helper | what it answers |
|---|---|
| `findInputLineSpan(val, targetLine)` | which byte range is this line? |
| `byteOffsetInLine(lineText, targetCol)` | which byte is this column? |
| `highlightSingleInputLine(...)` | the highlight over one row |
| `applyInputSelectionHighlight(...)` | the highlight over the value |

They moved to a new tier-5 foundations package,
`internal/ui/foundations/inputselection` (180 lines plus 191 lines of
tests), beside `internal/ui/foundations/viewport`.

**Two of them were methods that read the host's state**, and the split
followed the pattern: the *arithmetic* moved, the *host's facts* stayed.
`inputselection.OffsetAt(value, lineCount, promptScreenY, x, y)` is the
mouse-to-offset arithmetic; the host keeps a thin
`(m *runMode) inputOffsetAt(x, y int) int` that supplies `m.input.Value()`,
`m.inputLineCount()` and `m.promptScreenY`. That method is not a
shell — it reads host state and is called from inside the package — but
it is the host's half of the split, and the orchestrator may inline it
in a later tranche.

**The range normalisation travelled with the function.** The old host
method took its range from the store, which normalises `start <= end`;
a raw reversed range could never reach it. `ApplyHighlight`
normalises itself (swap when `start > end`), so the pure function and
the old host behaviour agree — the dump's reversed fixtures prove it.

**Two reference quirks are now pinned** that the move made visible:
`FindLineSpan` with a negative line returns the *last* line's span,
and a selection that starts inside the value but ends past it is
clamped — and because the highlight offsets by the prompt's two cells,
a rendered row *without* the prompt gets the highlight pushed off its
end. These are the reference's own behaviours, not defects the move
introduced; the tests pin them so a later change cannot "fix" them
silently.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`686fd3ed`) and this tree computed the same things:
16 line-span cases, 26 column cases (ASCII, CJK, emoji, a combining
`e`+`U+0301`, a precomposed `é`, a tab), 13 single-row highlight
cases, 14 apply cases and 17 mouse-to-offset cases — the same
**4 726 bytes**.

### 16. The host-local dead-code guard

The tree-wide dead-export sweep (section 4) found what *no package* calls.
It could not find what the **host** declares and nothing reaches, because
the host is 146 files and Go never complains about an unused function. The
sweep had measured the exported surface; the host's own unexported surface
was larger than it looked, and it was growing with every tranche that
moved a function out of the host and left its scaffolding behind.

`internal/cliapp/deadcode_test.go` is the guard. It parses the host with
`go/ast` — a regex is not trustworthy for this — and asserts that every
unexported declaration the host makes is reachable from the host's own
**production** code. Three properties of the guard matter more than the
tooling it uses:

**A test file is not a caller.** A function only the tests call is a
function the product never runs. It reads as a live seam, and it is not
one. The guard counts a production declaration as live only when a
**production** file names it.

**Liveness is transitive.** A declaration whose every production caller is
itself dead is dead as well. `statusRow` was called only by the tests, so
the three status-line helpers it called went with it. The guard computes
the greatest fixpoint over the call graph, not a one-hop check.

**The allowlist records a decision, and a decision that has expired is an
error.** `deadCodeAllowlist` maps a declaration to the reason it is kept
although nothing reaches it. `TestDeadCodeAllowlistIsMinimal` fails when an
entry no longer excuses anything — a stale entry is a decision that was
never revisited, and the map would rot silently.

The sweep's result on the host was **thirty deleted declarations** and
**four unexported names**: `NewRunMode` → `newRunMode`, `EnableTools` →
`enableTools`, `DefaultRouter` → `defaultRouter` and `BtwScrollBy` →
`btwScrollBy`. Each had no caller outside `internal/cliapp`, so its
exported status was a promise to nobody. The names that carry **reference
provenance** kept theirs: `ShowBtwHistory`, `IsBtwActive`,
`DismissBtwResponse`, `ToggleBtwExpanded` and `NavigateBtw` are the
reference's own surface and stay exported. The host's exported surface is
now **eleven names**: those five, plus `Run`, `RunLiveTUI` and `RenderTUI`,
and the `tea.Model` methods the interface requires.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree and the final trunk computed the same host surface — the
same 10 057 bytes at the base and the same 9 849 bytes at the trunk, both
`cmp`-clean. The byte count falls because the deletions removed rendered
rows, not because a rendering changed.

Two follow-ups landed after the sweep, and both were the guard's own
findings: the constructor's orphaned doc comment had been left on the
wrong declaration by an earlier move (it moved onto `newRunMode`), and the
write-only `runMode.btwExpanded` field — written by one method, read by
none — was deleted. Two comments elsewhere in the tree still named the old
exported name and were reworded.

### 17. The tier guard, in the tree

The layering rule — `core (1) < features (2) < infra (3) < app (4) <
ui (5)`, no upward edge, no import cycle — was measured by
`devutils/tiergraph.py`, a script outside the module. Nothing in
`go test` failed when the tree broke it; the rule held because
every cycle report ended with the script's output.

The measurement moved into the tree as a new **tier-1** package,
`internal/core/architecture` (60 lines of `doc.go`, 872 lines of
guard, 152 lines of classifier tests). It imports the standard
library only — `go/parser`, `go/build`, `go/token`, `io/fs` — so it
can sit in the core tier itself: it measures the tree rather than
being part of it.

| rule | what it asserts | scope |
|---|---|---|
| R1 | no upward edge — a tier-N package imports no tier-M package with M > N | production graph (fatal); a test-graph upward edge is **printed, not failed** |
| R2 | no tier package imports a host | production **and** test edges (fatal), with one documented exemption (below) |
| R3 | no ui-tier package imports a host | production **and** test edges (fatal) |
| R4 | no import cycle in the production graph | module scope (fatal) |
| R5 | no import cycle in the test-only graph | module scope (fatal) |
| R6 | the public packages import no `internal/` package | production **and** test edges (fatal) |

**The two tools agree.** The guard and `devutils/tiergraph.py` report
the same two numbers for the `internal/` subtree and the same
fifteen-row tier-to-tier edge table:

```
architecture: internal/ subtree — 225 production packages, 204 test packages
              (devutils/tiergraph.py reports the same two numbers)
```

The whole module is 282 production packages, 242 test packages, 872
production edges and 426 test edges; the guard prints both scopes, and
its own package is the one production package the subtree gained.

**Two rules did not hold as written, and are excused by name.** The
test-only cycle between `remotetransport` and `internal/remotecontrol`
and `remotetransport`'s test-only import of `internal/remotecontrol`
are both excused with a one-line reason: the cycle lives entirely in
`_test.go` files, and `remotetransport`'s shipped code imports
nothing from `internal/`. The exception list is printed on every run,
and an exception that no longer excuses anything is reported as
stale — the list cannot rot silently.

**One rule needed an exemption, and it prints its size.** R2 as
literally specified fails: 20 tier-4-to-host edges exist — `internal/app/agentturn`
→ `internal/agent`, `internal/app/subcommands/*` → `internal/cli`,
`internal/app/tools/{native,wasm}` → `internal/modular`,
`internal/app/agentserver` → `internal/pigrt`. AGENTS.md says
"only the app tier may use the turn engines", so the app tier is
exempted for the five *engine* hosts, and the exempted edge count is
printed on every run rather than hidden in a list of 20 names. (The
count was 21 until the 86th tranche took `internal/cli` out of
`internal/app/agentserver`.)

**The script's own host check was dead code.** `devutils/tiergraph.py`
tests `if it <= 4 and is_host(imp)` — but `it` is the tier of the
**imported** package, and a host is untiered, so the branch never
fired and the script printed `(none)` while 19 tier-to-host edges
existed. The in-tree guard is the first tool that reports them.

**Two findings the guard reports but does not fail.**

- *Invariant 63 is contradicted by the tree.* "A host never imports
  another host" says the server must not import `internal/printmode`
  either — but `internal/cliapp` imports `internal/printmode` in nine
  files (`main_dispatch.go`, `run.go`, `server_host.go`,
  `slash_browser.go`, `slash_commands_handlers.go`,
  `slash_commands.go`, `slash_helpers.go`, `tui_entry.go`,
  `tui_runmode_agent.go`). The guard has no rule for it — a rule would
  fail immediately — so the edge is recorded here instead.
- *The test-only graph has one upward edge*:
  `internal/core/lifecycle` → `internal/app/subagent/core` in
  `clear_approvals_integration_test.go`. R1 governs the shipped graph;
  the test edge is printed.

The guard runs in **0.6–0.7 s** on an idle machine (it parses about
2 000 files), and it was negative-tested with temporary probes — a
tier-1 → ui edge, a tier-1 → host edge, a public → internal
production import and a two-package cycle — each of which fired with
the right edge, file and rule.


### 18. The transcript's notice rows

Ten places in the host wrote the same **⎿** notice: the compacting
rows, the remote-control card's echo row, the slash command's echo
row, the queue-mode hint, the alt-key hint, and the two queued rows
under the composer. They were written as inline byte expressions, each
one spelling out its own escape sequences — and they came in **three**
distinct shapes:

| shape | bytes | call |
|---|---|---|
| compact | `WarningBlue` + `⎿  ` + `WarningBlueClose` + `SubtleOpen` + text + `\x1b[39m` | `uirows.NoticeCompact(text)` |
| indented | `WarningBlue` + `  ⎿  ` + `WarningBlueClose` + `SubtleOpen` + text + `\x1b[39m` | `uirows.NoticeIndented(text)` |
| dim | `WarningBlue` + `  ⎿  ` + `WarningBlueClose` + `DimGrayOpen` + text + `DimGrayClose` | `uirows.NoticeDim(text)` |

They moved into a new tier-5 package, `internal/ui/rows`, which imports
the public `palette` package and nothing else. The two queued rows
(`▸ <prompt> (alt+m to send immediately)` and `▸ <command>`) went with
them as `QueuedPromptRow` and `QueuedCommandRow`.

**The functions are named by shape, not by purpose.** The same bytes
serve different callers — the remote-control card's echo row and the
slash command's echo row are the *same* indented shape — so a
purpose-named function would be wrong for the other caller. There are
three shapes, so there are three functions.

**The colours are read at the use site.** The package reads
`palette.WarningBlueOpen`, `palette.SubtleOpen`, `palette.DimGrayOpen`
and `palette.DimGrayClose` directly, so a scheme change is picked up
without a refresh alias. The `"\x1b[39m"` close has no palette export,
so the package declares it locally — the same precedent as the other
ui-tier panels.

**The bytes are pinned, and the proof was a diff of two dumps.** Eleven
fixture texts (empty, plain, ellipsis, the compaction-cancelled
literal, the failure prefix, an embedded newline, a CJK wide rune, a
4 096-byte body, three format verbs, quoted text) — the same
**25 458 bytes**.

**Four dead-code sites were noticed and left alone.** Four other places
in the tree write the same notice bytes and could call the new
functions: `internal/cliapp/slash_commands.go` (`slashRespond`),
`internal/ui/tuilib/panel_hint.go` (`SlashEchoLines`),
`internal/cliapp/tui_prompt_submit.go` (the `» queued:` row) and
`internal/cliapp/tui_interrupted.go`. They were out of this tranche's
scope.

### 19. The three viewer documents

The `/changelog`, `/feature-diff` and `/usage` panels each had a
document built in the host — the rows, the visible-row count, the
viewer's spec and the counter row under the window. Three packages
already existed for exactly these three documents, and each one now
owns its own:

| package | what it owns now |
|---|---|
| `internal/ui/views/changelog` | `Document(width, height)`, `VisibleRows(height)`, `PanelLines(rows, start, end, total)` |
| `internal/ui/views/diff/feat` | `PanelLines()`, `Spec(height)` |
| `internal/ui/views/usagepanel` | `StaticSpec()`, `StaticLines(rows, start, end, total)` |

**The host keeps the offset, and that is why the window is an
argument.** `viewerWindow` clamps into `m.panel.window` — the offset
shared with `/codesearch` and `/tasks` — so the *window* is the host's
fact and the *rows inside it* are the panel's. The three functions
therefore take `(rows, start, end, total)` rather than computing the
window themselves. `viewerSpecFor` stays too: it reads the host's
panel registry, and that table is host wiring.

**The height-0 / height>0 distinction stays in the host** for `/usage`
(the terminal's height is the host's fact), while `/changelog`'s
visible-row rule (height − 4, minimum 1) moved into the package,
because that one is the panel's own geometry.

**One deviation, and it is a behaviour change.** `BuildRestRows`
parses the whole 180 KB embedded changelog markdown and measures
**511 760 ns/op**. The document is rebuilt on every TUI draw, and the
"no source" branch — the common case for a user outside the checkout
— would still have paid for the parse. The parse is now memoised
inside the package with a `sync.Once`, so the bytes are identical
(the scratch-directory dump proves it) and the frame cost is
unchanged.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree (`ba7aaca6`) and this tree computed the same things:
the `/changelog` document at three geometries, its spec at two
heights, its panel frame at three offsets on the height-aware *and*
the height-unknown path, the `/feature-diff` frame and spec, and the
`/usage` fallback and live frames — the same **1 259 880 bytes** from
the project root and the same **1 313 442 bytes** from a scratch
directory (the "no source" and "data file not found" branches).

**Four test-only seams were noticed.** The host's
`m.usageViewerLines()` had no production caller, and
`changelog.FilePath()`, `changelog.Loaded()` and
`feat.SourceLoaded()` are called by tests only. The 45th tranche
deleted `m.usageViewerLines()` — once the guard stopped counting a test
caller, it flagged the declaration. The three in the ui tier are
**exported** from another package, so the host-local guard says nothing
about them; the tree-wide dead-export sweep (section 4) is where a
decision about them belongs, and it is still open.


### 20. The status line's payload, its runner and its title store

The custom status-line script is fed a JSON document on stdin, and the
script's output is rendered into the status area; the same payload also
drives the terminal's window title. All three pieces lived in the host.

| from | to | what |
|---|---|---|
| `internal/cliapp/statusline_payload.go` (the builder half) | `internal/ui/views/statusline/payload_build.go` | `PayloadState` + `PayloadFrom(state)` — the JSON document |
| `internal/cliapp/slash_statusline_external.go` (the runner half) | `internal/ui/views/statusline/runner.go` | `RenderStatusLine(state, cfg, payload)` and `RenderWindowTitle(titles, cfg, payload)` |
| `internal/cliapp/windowtitle.go` | `internal/ui/views/statusline/title.go` | `TitleStore` with `Set`/`Get` |

**The package owns the wire vocabulary.** `Product = "antigravity"`,
the `exceeds_200k_tokens` threshold (200 000), the transcript path,
`DetectVCS`, `PricingForModel` and `version.Reference` are the
package's facts now, and the state's `Streaming bool` maps to the
`agent_state` field's `"working"` / `"idle"`.

**The host keeps the facts.** `buildStatusLinePayload` stays in the
host as the *collector*: it gathers what the host alone knows (the
session's token counts, the last turn's usage, the quota bucket, the
model's display name, the context window, the conversation id and
title, the plan tier, the email, the cycle mode, the terminal width)
into one `PayloadState` literal and hands it to the package. The
*shape* of the wire moved; the *facts* did not.

**Five shells went.** `statusLineExternalConfig`, `statusLinePayload`,
`pricingRow`, `runStatusLineScriptResult` and
`statusLineExternalState` were aliases into another package; the call
sites name `statusline.*` directly now.

**One fact moved DOWN to tier 1.** `FormatLogTS` — the timestamp
suffix for the CLI's log file name — lived in `internal/ui/tuilib` and
was used both by tuilib's own help panel and by the host's status-line
renderer. The status-line package must not import tuilib (tuilib pulls
in `internal/features/auth` and `internal/app/prompt/customcmd`), so
the formatter moved to a new tier-1 package, `internal/core/logts`, and
both callers use it.

**The bytes are pinned, and the proof was a diff of two dumps.** The
host dump grew from 3 363 to 3 405 bytes and the package dump from
1 578 to 2 808 bytes — the *only* differences are the sections the
move added, and every shared section is byte-identical. The permanent
golden re-verifies against the pre-move capture: **1 088 bytes ==
1 088 bytes**.

### 21. The bash-mode block

The reference's `!` mode draws one block per command: a head row, a
body, and — for a command that printed nothing — no body at all. The
host's `bashBlock` struct and its rendering moved into
`internal/features/tools/bashmode`, the tier-2 package that already
owned the block's *rows* (`HeadRow`, `BodyRows`, `OutputRows`,
`ContRows`, `BodyCap`, `MarkerColour`, `BlockLines`):

```go
type Block struct { Cmd, Body string; Failed, Running bool }
func (b Block) Lines(expanded bool, height int) []string
```

The host's only reference to the block is now the interface assertion
`var _ transcript.BashBlock = (*bashmode.Block)(nil)` — one owner, not
two.

**Two colours went with the block, and the refresh went with them.**
The host declared `bashBodyOKOpen` and `bashBodyFailOpen` and
re-assigned them in `renderer.go`'s `refreshPaletteAliases`. The
package reads `palette.WarningBodyOpen` and
`palette.ToolRemoveBodyOpen` **directly at the use site** — the
palette's own variables are live, refreshed by the palette — so both
host variables and the two lines in `renderer.go` are gone. A refresh
alias is a second owner of a colour.

**A type alias into another package was a shell.**
`type bashOutput = tools.SafeBuffer` was deleted: the host names
`*tools.SafeBuffer` directly, and the tier-2 package never needed the
buffer at all, so nothing crosses the tier boundary.

**The process stayed in the host.** `startBashCommand`,
`execBashCommand`, `finishBashCommand`, `placeBashBlock`,
`preemptBashCommand` and `cancelBashCommand` own the shell, the
`procgroup` kill and the implicit-trajectory write. `bashDoneMsg`
stayed as well: it carries the generation counter of the
stale-completion guard and the exit status the trajectory records, and
its reference sites live outside this tranche's file scope.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree and this tree computed the same things: eleven block
branches (success with and without output, failure with and without
output including the `exit status 3` text, a running block with a
partial body, the `  ⎿  ` marker's colour, and the body cap in its
collapsed, expanded, exact and over-tall forms) plus the package's
seven helper surfaces and the host's `▸ <cmd>` queued row — the same
**6 285 bytes**.


### 22. The themed startup banner

The reference's themed banner is one of the event's own five scenes,
the session's four information rows in the column that follows the
art, and a seasonal sentence. It lived in the host
(`internal/cliapp/seasonal_banner.go`) next to a plain banner that
stayed. The themed one moved to `internal/ui/views/seasonal`, the
tier-5 package that already owns the event, its art and its
greetings:

```go
type State struct { Email, Tier, Model, Cwd, CLIName, Version string }
func RenderBanner(w io.Writer, ev *Event, st State)
```

**Three facts arrive in the `State` because the package cannot reach
them.** `internal/ui/tuilib` imports `internal/ui/views/seasonal`, so
the seasonal package may not import tuilib — the **host** shortens the
working directory (`tuilib.ShortenCwd`) and passes the result in. The
CLI's name and the version travel in the state as well; the host
supplies `branding.Current().CLIName` and `cliVersion`, and
`internal/cli/version.go` was checked to be
`const ReferenceVersion = version.Reference` — byte-identical to the
host's constant.

**The two process-wide picks moved with the banner.** The reference
draws one of the five scenes and one of the four sentences per launch
and keeps them for the whole session; the package keeps that state
and exposes `SetBannerPicks(art, greeting)` so the host's test can
pin the scene and the sentence without an alias back into the host.

**What stayed, and why.** `seasonalTheme()` stays in the host: it
reads `internal/core/settings` and calls `configpanel.RawString`, and
`internal/ui/views/configpanel` imports `internal/ui/views/seasonal`,
so moving it would close a cycle. So do `seasonalNow`,
`seasonalSpinnerFPS` and `refreshSeasonalTheme` (they own the host's
spinner). The plain banner — `logoLines`, `cliVersion` and the logo
art — stayed in the host too.

**The bytes are pinned, and the proof was a diff of two dumps.** The
pre-tranche tree and this tree rendered the same five branches (a
tier, no tier, the greeting with a name, the greeting without one, a
nil event) — the same **32 717 bytes**
(`md5 d21f1dc6b38f2ea9088286e3551d490d`). The temporary harness
became a permanent golden,
`internal/ui/views/seasonal/testdata/banner.golden`, and the golden's
version is a fixture literal, so a version bump does not break it.


### 23. The eligibility notice and the announcement queue

The reference shows an eligibility error **inline in the input loop**,
above the prompt, with a verification URL — not stacked above the
transcript. Cycle 262 built that, and the row renderer ended up in a
tier-2 *features* package, which is the wrong tier for a row: it made
`internal/features/auth/eligibility` depend on `palette` and `render`
for a single function.

| from | to | what |
|---|---|---|
| `internal/features/auth/eligibility/eligibility.go` | `internal/ui/views/announce/notice.go` | `RenderNoticeRow(reason, verificationURL, caps)` |
| `internal/cliapp/elig_inline.go` | `internal/ui/views/announce/queue.go` | `Queue{Cards, SessionHiddenID, SaveError}` with `Load`, `Active`, `Row`, `Dismiss`, `HideForSession`, `SaveErrorMessage` |
| `internal/cliapp/elig_inline.go` | `internal/ui/views/announce/notice.go` | `Notice{Reason, VerificationURL}` |

**The eligibility package keeps the facts, and is now free of UI
deps.** The typed `Error`, `Reason`, `VerificationURL`, `HTTPStatus`,
`AsError` and `ClassifyError` (the print-mode `ELIGIBILITY_DENIED`
envelope) stay in tier 2; only the *row* moved. `eligibility.RenderNoticeRow`
is gone — not aliased, not forwarded.

**The host keeps the wiring.** The tea message, the notice setter (and
its stderr diagnostic), the clear, the Esc dismiss, the submit's
hide-for-session, the load at start-up and the two render slots
between the separator and the composer stay in the host. The
announcement catalogue (`Catalog`, `VisibleCards`, the ids and the
kinds) was **not** touched: its members are the reference's contract,
and a renumbering would be a wire change.

**One shell went.** The host's `eligibilityHTTPStatus` was a re-export
of `eligibility.HTTPStatus` that nothing called.

**The bytes are pinned, and the proof was a diff of two dumps.** The
notice row (empty reason, reason only, reason + URL as an OSC 8
hyperlink, reason + URL as a plain fallback) and the queue's visible
rows (zero, one and all cards, the dismissed set, the session-hide and
a persistence failure) — the same **4 019 bytes**. The permanent pin is
`internal/ui/views/announce/testdata/notice_queue.golden`.

### 24. The voice dictation's state machine

The dictation overlay is a four-phase state machine — idle, connecting,
recording, finishing — with two exits: the draft is confirmed into the
composer, or the session is discarded. The banner, the running timer
and the failure notice all follow from the phase, and all of it lived
in the host's `tui_help.go`.

| from | to | what |
|---|---|---|
| `internal/cliapp/tui_help.go` | `internal/ui/views/voice/surface.go` | `voice.Surface` — the phase, the trigger, the recorder and the cancel flag |

**The package already owned the banner, the timer and the recorder.**
The voice package was a home for the *views*; the state machine that
decided which view to draw was in the host. The move puts the decision
next to the thing it decides.

**The host keeps the facts the surface cannot know.** The four seams
are the wiring: `Connect` establishes the transcription session (the
`ANTIGRAVITY_MIC` TCP probe included), `InsertDraft` writes a confirmed
draft into the composer, `Respond` echoes a failure into the
conversation, and `Width` is the terminal's width. `internal/cliapp/voice_host.go`
carries them.

**The surface is safe on a nil receiver**, like every other surface in
the host: a host that never built one has no dictation and asks it
nothing. Every seam is optional, so a surface built without one behaves
as if the seam were answered with the zero value — the package is
usable from a test without a running TUI.

**The bytes are pinned, and the proof was a diff of three dumps.** The
host's own rendering before the move, the host's own rendering after it
and the surface's own rendering — all three **1 231 bytes**,
byte for byte identical. The permanent golden is
`internal/ui/views/voice/testdata/surface.golden`.

### 25. The composer's selection arithmetic and the prompt history

Two independent facts lived in the host, and both moved down into tier
5.

**Part A — the selection *value* arithmetic** moved into
`internal/ui/foundations/inputselection`, beside the geometry that was
already there (`CursorPosition`, `DeleteRange`, `ReplaceRange`). They
are pure over their arguments, so they moved whole. `DeleteRange` and
`ReplaceRange` normalise a reversed range — the convention the sibling
`ApplyHighlight` already used — and `CursorPosition` clamps a negative
or past-the-end offset.

**The selection *store* stayed in the host.** `m.store.InputSelStart/End`,
the textarea writes, the select-all gesture, the paste insert and the
overlay close all read and write host state, and a tier-5 package
cannot reach into it. The host keeps a thin supplier and calls the
package's pure functions — the rule that a pure helper which reads host
state must be **split**, not moved.

**Part B — the prompt history** became
`internal/ui/foundations/history`. `history.List` owns the entries, the
index and the draft, and with them `Push`, `Load`, `Up`, `Down` and
`Reset`. The host keeps the composer, the `history.jsonl` persistence
(`convstore`) and the composer's own overlays.

**The blank-prompt rule moved with it.** A whitespace-only prompt used
to slip past the `prompt == ""` guard and land in the history, the
transcript and the session at once. The rule was introduced for the
history, and the submit path asks the same question — so the answer has
one owner: `history.IsBlank`.

**One shell went.** The host's `historyReset()` was a one-line
forwarder, and a one-line forwarder is a shell: it was **deleted**, not
forwarded, and its ten call sites now say `m.promptHistory.Reset()`
directly.

**The bytes are pinned, and the proof was a diff of two dumps.** The
base tree → **8 603 bytes**; the post-move tree → **8 603 bytes**,
byte-identical. The permanent golden is
`internal/cliapp/testdata/tui_input_history.golden`.

### 26. The panel registry's key routing

The host answered "which panel owns this key?" with two hand-written
chains of `if` statements, and it answered "how many rows does this
panel have?" with a fourteen-case `switch` on the panel's kind. Both
belonged to the registry — the package that already knows which panel a
command opens.

| from | to | what |
|---|---|---|
| `internal/cliapp/slash_panel_keymap.go` | `internal/ui/panelregistry/route.go` | `RouteNone` / `RoutePanel` / `RouteSearch` / `RouteList` / `RouteViewer` — the route table |
| `internal/cliapp/slash_panel_keymap.go` | `internal/ui/panelregistry/registry.go` | `Registry.HandleKey` — the two early-route chains |
| `internal/cliapp/slash_panel_routing.go` | `internal/ui/panelregistry/registry.go` | `Registry.RowCount(kind)` |
| `internal/cliapp/slash_panel_routing.go` | `internal/ui/panelregistry/command.go` | `CommandPanel(cmd)` — the command → panel table |
| `internal/cliapp/slash_panel_keymap.go` | `internal/ui/foundations/viewport/panelcursor.go` | `PanelCursor(cursor, action, total)` — the ↑/↓/PgUp/PgDn/Home/End arithmetic |

**The registry keeps the fan-out; the host keeps the facts.** The row
counts are the host's — they depend on the panel's own content — and
they arrive through `internal/cliapp/slash_panel_wiring.go`, which also
carries the `/codesearch` "did the panel claim this key?" handler and
the two seams (`Close`, `ViewerKey`).

**The cursor arithmetic moved into the viewport package**, beside the
list navigation it is built on. `PanelCursor` is the same `ListNav`
arithmetic with a panel's page size (`PanelPageRows = 10`).

**Two residual shells were recorded, not fixed.** `panelForCommand` and
`listPanelKind` are one-line forwarders, and the cycle-381 rule says a
forwarder is a shell whatever calls it — but their two call sites
(`slash_panels.go:488` and `tui_update_keys.go:36`) were outside this
tranche's scope. A follow-up should inline them and delete them.

**The bytes are pinned, and the proof was a diff of two dumps.** The
command → kind table, every kind's row count, every kind's answer to
22 keys and the rendered rows of four panels: **460 274 bytes**,
byte-identical. The permanent golden is
`internal/cliapp/testdata/panel_keymap_golden.txt`.

### 27. The project, browser and log-file flags

Three session flags are policies, not host behaviour: `--project` picks
a workspace, `--browser` points the browser tool at a CDP endpoint, and
`--log-file` redirects the log. All three lived in the host.

| from | to | what |
|---|---|---|
| `internal/cliapp/project_flags.go` (project half) | `internal/core/cliflags/projects/projects.go` | `projects.Apply` — the default, the explicit id, the explicit name, the new-project path and the projects cache |
| `internal/cliapp/browser_flags.go` | `internal/core/cliflags/browser/browser.go` | `browser.Apply` — the CDP port, the headless flag, the user-data dir and the local-Chrome flag |
| `internal/cliapp/project_flags.go` (log half) | `internal/core/cliflags/logfile/logfile.go` | `logfile.Apply` |
| — (new host file) | `internal/cliapp/session_flags.go` | the wiring: `applySessionFlags(fs)` calls the three policies and supplies the seam |

**The policies are tier 1**, because a flag's *policy* — what the
value means, where the projects cache lives, what the store directory
is — is a fact below the host. The **registration** stayed where it
was: `internal/cli/parser_flags.go` holds the flag table, and
`cli.FlagSpec` is the parser's own vocabulary — moving the type would
rename it across the whole `internal/cli` package, which is a different
tranche's scope.

**The conversation id arrives as a seam.** A new project's id *is* a
conversation id, and the generator (`agyprompt.NewConversationID`)
lives in tier 4, so a tier-1 package cannot reach it — it arrives
through the host's wiring.

**One dead export was kept on purpose.** `internal/cli.RedirectStderr`
now has no production caller in the module, but it is the recovered
termio surface — reference provenance, invariant 94 — so it stays. The
reason is recorded here rather than left in a comment.

**The bytes are pinned, and the proof was a diff of two dumps.** The
seven flag-table rows, twelve project cases, the cache before and
after, six browser cases and three log-file cases: **3 426 bytes**,
md5-identical. The permanent golden is
`internal/cliapp/testdata/session_flags.golden`.

### 28. The vim key translation

The public `vim/` package deliberately knows nothing about bubbletea —
its own doc comment says so: *"Nothing here knows about bubbletea,
lipgloss or the run loop."* The mapping from a bubbletea key to the
engine's key therefore had no home, and it sat in the host as four
functions: `vimMod`, `vimNamedKey`, `vimRuneKey` and `vimBadge`.

They moved to `internal/ui/editor/vimkeys` — `Modifiers`, `Named`,
`Rune`, `Badge` — a pure, stateless package that imports `tea` and the
public `vim` package and nothing else.

**The badge's rule is split across two owners, on purpose.** The three
badge *strings* belong to the engine (`vim.Mode.Badge()` — the public
package owns the reference's bytes); *whether vim is on* is the host's
fact, so the ui tier's `vimkeys.Badge(enabled, mode)` supplies it as a
parameter. The host keeps a small `m.vimMode()` fact-collector, and it
deliberately **does not build the engine when vim is off** — the host's
pinned invariant is that the engine must not exist in that case, and a
collector that built one to ask it a question would break it.

**The bytes are pinned twice.** A host-level dump drove the host through
a 58-key scripted sequence under three settings (vim off, vim on, vim
with insert-first) and wrote the composer text, the status row (which
carries the badge) and the engine state: **56 706 bytes**, byte-identical
between the base tree and the branch (`sha256 f9e5a175…`). A
translation-level matrix compared the old implementation against the new
package over **5 702 key events** — 66 codes × 13 modifier sets × 6 text
variants, plus 8 badge cases — with **0 mismatches** and the same digest
from both sides (427 746 bytes).

The matrix needed a trick: the base tree has no *importable* copy of the
old translation, because it was unexported host code. The digest was
therefore produced on each side by a throwaway test inside that tree,
using its own live functions, and the two digests were compared.

### 29. The model argument resolution

`/model <argument>` has two surfaces that look alike and are not. The
**panel** (`internal/ui/views/modelpicker`) is a static row list over
the reasoning-tier and effort models, matched by *subsequence* over the
reference's own display labels. The **completion popup** filters the
*live* catalogue — the reference's models plus the user's configured
ones — by *substring* over the label **or** the id, and resolves a typed
argument by a *full, case-insensitive* match.

Different row source, different match rule, different owner. So the
popup's resolution went to its own package, `internal/ui/views/modelargpopup`:
`Rows() []slashpicker.ModelRow`, `Resolve(arg) (catalog.ModelEntry, bool)`
and `InvalidMessage(arg) string`. The *rendering* stays with
`slashpicker`, which owns `ModelRow`.

The host's `completionModelRows`, `resolveModelArgument` and
`invalidModelMessage` are gone — no forwarders, no aliases.
`applyModelArgument` and `submitModelCompletion` stayed in the host:
they write settings, re-sync the backend and touch the session.

The proof was a host-level dump of the popup rows for five queries, the
invalid-model transcript, four valid-model transcripts and the
highlighted-row completion: **2 426 bytes**, `cmp`-clean.

### 30. The transcript's document mechanics

`internal/ui/transcript` already owned the transcript's record types
(`Block`, `UserBlock`, `AssistantBlock`, `LiveStepRun`, `NewBlock`,
`RenderUserPrompt`, `TurnSeparator`). What it did not own was the
*mechanics*: the range replacement, the blank line, the block
deduplication, the live-step batching and the two finalisations. Eleven
host functions carried them.

The shape chosen is **hybrid**, and the reason is the host's buffer.
`m.response` is read from twenty-odd files the transcript package does
not own, so a type that *owned* the buffer would have put the whole host
behind a constructor. Instead:

- **package-level functions** for the incremental call sites —
  `ReplaceRange`, `BlankBefore`, `DeduplicateBlocks`,
  `CollectLiveStepBatches`;
- **a `Document` value** (not a reference) for the composite
  finalisation — `FinaliseLiveTurn`, `FinaliseTurnReply`,
  `ReplaceLiveStepBatches`. The host builds it from `m.response`,
  `m.blocks` and `m.liveSteps`, hands it over and takes it back; the
  slice headers stay the host's.

Two facts moved with them and one deliberately did not. The thought step
(`AppendThoughtStep`) belongs to `internal/ui/toolsteps`; the prompt
section's token arithmetic (`SectionTokenCount`) is **not** a view
concern and belongs to `internal/app/prompt/agyprompt`. The turn's
emptiness rule stays in the host (`agentturn.IsEmptyAssistantTurn`) and
travels in as `Options.EmptyTurn` — so the ui tier never imports
`internal/app/agentturn`, and through it never imports the engine host.

`applyBlockUpdates` stayed behind on purpose: it owns the user-block
index, which the `Document` does not hold.

The proof was a dump of the transcript state **and** the rendered view,
against a pristine `git archive` of the base commit: **31 102 bytes**,
`cmp`-identical (`md5 bcad2228b03137eea259ebef95147a93`).

### 31. The attachment and task-notification rows

Two more row shapes joined `internal/ui/rows`: the "▸ 📎 N media
attached" indicator above the composer (`AttachmentRows`) and the
dim rows a shell task notification renders (`NotificationRows`). The
names are shape names, in the package's existing style
(`QueuedPromptRow`, `QueuedCommandRow`); the doc comments spell out the
four attachment shapes and the notification shape.

**The colour is read at the use site, and the palette's value is the
owner's.** The host's `cDimGrayOpen` / `cDimGrayClose` were direct
assignments from `palette.DimGrayOpen` / `palette.DimGrayClose`
(`renderer.go:119` and `:137`), and the palette's values are
`ESC[38;2;101;123;131m` and `ESC[39m` — byte-identical to the aliases
the host carried. The moved code now reads the palette directly, so a
scheme change is picked up without a refresh alias.

The host's `drainTaskNotifications` was **mixed**: it appends to the
session *and* appends rendered rows. Only the row half moved; the
session half stayed.

The proof was a dump of ten cases — media none, one collapsed, two
collapsed, two expanded, cleared, and six notifications (title only,
title+body, body-only with a trailing newline, whitespace-only, empty,
title with an empty body): **1 117 bytes**, `cmp`-clean
(`sha256 250cba38…`). The two host tests that exercised the attachment
rows moved with the code and were strengthened into exact-byte goldens.

### 32. The accounts registry owns the quota snapshot

The accounts registry already kept a per-account quota snapshot —
`Account.Quota *auth.QuotaSnapshot`, `SetQuota`, and a `FetchQuota`
that writes the snapshot straight into the registry. The package's own
doc said the intent: *"so the /usage panel can reuse a snapshot the
/accounts panel already fetched instead of querying twice."*

The TUI host kept a **second** cache in a **different representation**:
`m.quotaCache map[string]quotaCacheEntry`, holding `*auth.QuotaSummary`
where the registry holds `*auth.QuotaSnapshot`. The two representations
are convertible, which is exactly why the duplication survived so long
— nothing looked broken.

Now the registry is the only owner. `accounts.FreshQuota(email)` reads
`Account.Quota`, applies the freshness rule and returns the summary;
`accounts.QuotaFreshness` (two minutes) sits next to the snapshot it
governs, and `accounts.QuotaTimeout` (45 seconds) sits next to
`FetchQuota` — the budget that had no owner at all until now. The
host's `m.quotaCache`, `quotaCacheEntry` and `quotaCacheFresh` are gone.

`registerAccount` — a nil-check plus a one-line forward to
`accounts.Register` — is gone as well; the sign-in path names the
owner directly.

**The move fixed a real defect, and the fix has a visible edge.** The
snapshot was **not** a lossless cached form of the summary: it dropped
the bucket's wire id, and the status line keys its quota map by that
id. A quota read back from the registry's cache therefore rendered
`"quota":null` instead of `"quota":{"3p-5h":…}`. `QuotaBucketSnapshot`
now carries the id. The visible effect is in
`internal/infra/agyproxy`: the **cached** path of the aggregate models
endpoint now emits the real `bucketId`, where it used to emit an empty
one. The live path already emitted it, so the two now agree.

The proof was a host-level probe that wrote a quota through the host's
seam, read it back through the host's seam and rendered the status
line's payload JSON, the `/usage` panel's document and the cached
summary member by member: **1 664 bytes**, `cmp`-clean
(`sha256 6812b952…`). The repo's own golden — the status line's payload
bytes — still passes, and it now passes through the registry's round
trip.

### 33. The composer's text editing

The composer's *selection geometry* moved to
`internal/ui/foundations/inputselection` in an earlier tranche, but the
*decisions* — which key does what to the selection, what the
backslash-newline fallback does, how the textarea and the two dropdowns
stay in step — stayed in the host, in
`internal/cliapp/tui_update_keys_edit.go`.

They moved to `internal/ui/editor/composer`, a sibling of the existing
`internal/ui/editor/{editorkit,keybind,vimkeys,wordmove}`.

**Three things cross the seam, and each crosses in its own shape.**

- **The selection's range crosses as a value** — `composer.Range{Start,
  End}` with an `Empty()` — replacing the host's `hasInputSelection`
  flag. The selection *store* stays in `internal/app/tuistore` (the app
  tier, below the ui tier), so the range cannot be a store reference;
  it is a number the host supplies and receives back.
- **The widget crosses as an interface** — `composer.Editor`, with
  `TextArea` as the adapter over the host's `textarea.Model`. The
  adapter **writes through** to `m.input`: the host keeps ownership of
  the widget, and the ui tier never holds the textarea itself.
- **The OS write stays host-side.** The composer decides *that* a
  selection is copied and hands the text over; the host performs
  `clipboard.Write` and returns the command. A ui-tier package that
  wrote to the system clipboard would be doing I/O the host owns.

**The seam's two kinds of member are distinguished.** `Picker`, `Files`
and `History` are **nil-safe** — a panel that was never opened is nil,
and the composer must not panic on it. The **callbacks** in `Seams`
(`SelectAll`, `ClearSelection`, `DeleteSelection`, `ReplaceSelection`,
`Copy`, `ClearEligibilityNotice`, `DismissModeHint`) are **required**:
a nil one is a programming error, and the doc comment says so.

**Four host key handlers stayed, and the reason is a behaviour fact.**
`handleKeyUp`, `handleKeyDown`, `handleEnterKey` and
`handleWordMoveAndChords` mix the composer with the queue, the BTW card
and the turn engine. The Enter key's alt/shift branch and the ctrl+j
branch have **different tails** — Enter dismisses the mode hint, ctrl+j
does not — so splitting them would need two near-duplicate composer
functions and would reorder the key handling. The mode-hint divergence
is the reference's own, and a split that duplicated it would be a
second owner of the rule.

The proof compared **three** dumps — the pre-change tree, the branch
with the host's own copy of the extracted pieces, and the branch with
the new package: **8 595 bytes** each, `md5 c60ae496…`, all three
identical.

### 34. The session layer owns the rewind

The rewind had **two implementations**, and the divergence was already
documented — in the host's own comment
(`internal/cliapp/rewind_host.go:62-66`):

> *"rewindpanel.RewindSession pops only the trailing pair, while this
> routine pops trailing pairs until the picked step's user message is
> the trailing user message."*

1. `internal/ui/views/rewindpanel/rewindpanel.go:121` —
   `RewindSession(sess []backend.Message) int`: popped the trailing
   user/assistant pair **from a local slice copy** and never wrote
   back, so it *could not* truncate the caller's session. It was
   exercised only by its own unit test and by the bare `/rewind`
   handler.
2. `internal/cliapp/rewind_host.go:70-102` — `rewindPickStep`: the
   **live** implementation.

The host also carried `rewindSteps` — a **shell**, a one-line forward to
`rewindpanel.Steps(m.session)`. Per AGENTS.md invariants 68/97 a
one-line forward into another package is a shell whether or not anything
calls it, so it went too.

**Where the policy belongs.** The rewind policy — which messages a
picked step owns, how many are removed, and the echo text — is a
**session** fact, not a view fact. The panel's job is to *name the
picked step*; the session layer's job is to *truncate*. So the policy
moved to a new **tier-4** package, `internal/app/session/rewind`:
`Step`, `Steps`, `Result`, `ToStep`, `PopTail`, `StepEcho`, `TailEcho`.

The existing session packages were not the right home, and the reason
is what they own: `convstore` is the sqlite/trajectory store and
`rehydration` rebuilds agent state from a stored trajectory. The rewind
policy is about the **live in-memory session slice** (`m.session
[]backend.Message`), which neither owns.

**Two operations, two functions, one owner.** `ToStep` names a picked
step (the picker's cursor index) and `PopTail` names no step (the bare
command). They are genuinely different rules — the tail pop is
role-based, the step rule is user-anchor-based — and their echo texts
differ, so merging them would have changed bytes.

**How the truncation reaches the caller's session now.** The policy
returns the truncated session in `Result.Session` and the *caller*
assigns it (`m.session = r.Session`). The old ui copy could not do
that: it truncated its own copy and threw it away, so the bare
`/rewind` reported *"Rewound 2 message(s) from the session tail"* while
the session was untouched.

**The move is a behaviour change on an unreachable path.** The bare
`/rewind` handler is dead in practice — `panelregistry.CommandPanel
("rewind")` claims the command first, so `/rewind` always opens the
picker. Its session truncation now really happens; the count and the
echo are unchanged.

The proof drove the two host entry points that exist in **both** trees
— `rewindPickStep(idx)` and `slashRewind("")` — over 9 session fixtures
× 6 cursor positions, dumping the picker's rows, the removed count, the
resulting session and every echo line: **12 721 bytes** in the base,
**12 449 bytes** in the branch. The difference is **16 lines, all of
them the bare command's caller session** — the copy bug. Everything
else — the rows, the counts, the picker's sessions and every echo line
— is byte-identical: **12 037 bytes**, `cmp` exit 0.

### 35. The status line package owns its rows

`internal/ui/views/statusline` already owned the status line's
`State`, `Config`, `TitleStore`, the payload builder, the script and
the *"disabled after 30 consecutive failures"* message. What it did
not own was the **rows** the host drew.

`internal/cliapp/statusline_render.go` carried:
- `statusAreaRows` — the status area's layout rules (`stack_with_default`,
  the padding, the disabled message, the error card). All of those are
  facts the package already owns (`Config.StackWithDefault`,
  `Config.Padding`, `State.Current()`, `State.Disabled()`,
  `DisabledMessage`).
- `statusLineErrorRows` — **the reference's status-line error card
  bytes, in the host.** The strings `"⚠ Statusline Error"`,
  `"⎿  Statusline error:"` and `"Full logs at: …"` exist nowhere else
  in the tree. The sibling *"disabled after 30 consecutive failures"*
  message was already owned by the package; the error card now sits
  beside it, as `statusline.ErrorRows(err, logPath)`.
- `statusLineLogPath` — **the third copy of the log-file path fact.**
  The same shape appears at `internal/features/auth/compat.go:269` and
  `internal/ui/tuilib/help_panel.go:84`. The owner is
  `internal/core/confhome` (tier 1, owner of `confhome.Antigravity()`),
  and the log path is now `confhome.CLILogPath(ts)`.

`internal/cliapp/statusline_payload.go` carried `statusLineQuota` — a
**pure function of (`summary`, `now`)** with zero host state, producing
the wire shape `statusline.QuotaBucket`. The *bucket map* had no owner
today; it is now `statusline.QuotaBuckets(summary, now)`.

**The three one-line forwards are gone.** `statusLineContextSize`,
`statusLineTaskCount` and `titleScriptActive` were shells; they are
inlined at their call sites. Two of the inlines are load-bearing:
`modelctx.ContextWindowFor("")` returns `DefaultContextWindow`, not
zero, so the empty-model guard has to stay with the expression, and the
task count's nil `taskReg` check likewise.

The host keeps **`statusAreaState()`** — a *collector*, not a
forwarder: it gathers the built-in row, the active flag, the block, the
state and the log path into a `statusline.AreaState` value.

The proof rendered five fixtures (default, stacked + padding, replaced,
disabled, error card) through the host's row path, quoting the rows so
the escapes are visible: **1 229 bytes** on both sides, `cmp` exit 0.
The only normalisation is the log file's timestamp suffix, which moves
with the clock.

### 36. The markdown-agent catalogue leaves the engine host

`internal/agent` is the PiG-backed **engine host** — the turn loop,
`pig.go`, `loop.go`, `pig_events.go`. It also owned `list.go` (273
lines): the markdown-agent catalogue. That catalogue loads `.md` files
from the workspace and the user's config tree, parses their YAML
frontmatter, and exposes `Discover`, `DiscoverWith`, `Lookup`,
`AvailableAgents`, `ValidateAgent` and `AgentNotFoundError`.

It is a **feature**, not an engine concern, and its only clone import
was `internal/features/plugins/core` — tier 2.

**Why the host was the only reachable home.** `internal/ui/views/
agentspanel/panel.go` declares its own `Agent` type with `Name`,
`Description` and `Body` — field-for-field identical to the
catalogue's `Agent` (which also has `Path`). The host's
`discoverPanelAgents` maps one into the other, field by field. The
duplication was not laziness: **the ui tier may not import a host**
(AGENTS.md invariant 87), so the panel could not name the engine host's
type, and the host had to duplicate it so the panel could reach it.

Moving the catalogue to tier 2 removes the copy **without a forwarder**:
the panel now names `agents.Agent` directly (a ui → features edge, and
there are already 21 of those), `agentspanel.Agent` is gone, and the
host's `discoverPanelAgents` — a pure forward plus a field-by-field
copy, i.e. a shell under invariant 68 — is gone with it.

The new home is **`internal/features/agents`** (tier 2). Tier 2 rather
than tier 4, because tier 2 lets the ui tier name it directly, which
is the whole point of the move; tier 4 would have forced the panel
across two tiers for a catalogue that has no app-tier facts.

**The flag's face of the catalogue moved with it.** `CustomAgentFromFlags`
and `AgentBody` lived in `internal/printmode/agent_helpers.go` and are
now `agents.CustomAgentFromFlags` and `agents.AgentBody`.

**A host-to-host edge fell away with it.** `internal/cliapp/server_host.go`
reached for `printmode.AgentBody(printmode.CustomAgentFromFlags(fs))` —
the host-to-host edge AGENTS.md invariant 63 forbids, and invariant 132
records. It now names the tier-2 package directly, and the production
files in `internal/cliapp` that import `internal/printmode` went from
**nine to eight**. The app tier's edge to an engine host dropped with
it too: the architecture guard's printed *app tier → engine-host edges*
went from **22 to 21**.

The proof dumped the catalogue over a fixture covering all four scopes,
both `agents.json` stem-exclusion rules, a folder without `plugin.json`,
a plugin disabled by `config.json`, the `default` agent, four malformed
files, a non-`.md` file, a duplicate name across scopes and the ambient
`Discover()` — every agent's name, description, body and path,
`AvailableAgents`, five `Lookup`s and five `ValidateAgent` results:
**8 772 bytes** on both sides, `cmp` exit 0.

### 37. The conversation store owns the trajectory

The reference stores every step of a conversation in **one trajectory
table** — the user inputs, the model responses, the tool calls — and
derives the agent's live state from it when the conversation is loaded.
The clone used to write only the model messages, so a resumed
conversation lost every tool call it had made — and, because the clone
writes to the same store the reference uses, it also replaced the
reference's stored trajectory with that thinner record.

The **assembly** of that record was in the host
(`internal/cliapp/trajectory.go`): `trajectorySteps`,
`nextTrajectoryIndex`, the `conversationSelection` alias and the
`workspaceDir` helper.

The assembly moved to `internal/app/session/convstore/trajectory.go`:
`Trajectory{Loaded, Calls, Session, SeededLen}`, `BuildSteps` and
`NextStepIndex`. **The store owns the assembly because the store owns
the trajectory** — the host holds the four facts (they are the live
state of a running session) and the store decides what they mean.

**The host keeps one collector**, `trajectoryFacts()`, which gathers
the four facts into a `convstore.Trajectory`. The two product call
sites — `saveSession` and `/fork` — and the three test call sites name
the store directly.

**Two second owners went with it.** The seed read the rehydration
status through the host's own `loadedStepErr` helper; the status table
now has one owner, `rehydration.Succeeded(status)`. And the working
directory was read through `os.Getwd` in four places; it is
`internal/core/workspace.Current()` now — a **tier-1** package, because
the conversation store (the app tier) and the hosts (above every tier)
both need it.

**One alias died with the move.** `type conversationSelection =
convstore.Selection` was a package-level alias into another package —
a shell under invariant 98. The call sites name `convstore.Selection`
directly now.

The proof built the trajectory from three fixtures (adopted + turn,
unreplied turn, empty) against a copy of the pre-change tree carrying
the host's implementation verbatim in the store: **485 bytes** on both
sides, `cmp` clean (`sha1 4b87fc3b…`). An in-repo harness agreed:
**608 bytes** each, identical apart from the test's own timing line.

### 38. The second owners of the default model, the width and the scroll rule

The panels moved, but five facts about them stayed behind in the host —
the host kept a *second* copy of a fact whose owner already existed.

- **The default model.** The `/context` panel's title row carried the
  literal `"Gemini 3.8 Flash (High)"`. The catalogue names the default
  (`modelcatalog.DefaultModel`) and the host already had the alias for
  it (`defaultModel`). The literal is gone; the host asks.
- **The viewport's fallback width.** The host returned `200` for a
  terminal of unknown width, and the viewer panel carried the same
  number as its own fallback. The number now has one owner
  (`viewerpanel.DefaultWidth`).
- **The `/changelog` scroll rule.** The host re-implemented the
  reference's *"one row less when the terminal size is unknown"* rule.
  It belongs to the `/changelog` panel, so it lives there
  (`changelog.PageRows`) and the host asks.
- **The system-tools row.** The host's `toolTokenEstimate` was a
  one-line forward into `contextpanel.ToolTokenEstimate` — a shell
  under invariant 68. The call site names the owner.
- **The tool-result collection.** The host decided which steps are tool
  steps and gathered their raw outputs. The step vocabulary owns that
  (`toolsteps.ToolResults`); the host only gathers the steps.

The bytes do not move: the default model is the same string, the
fallback width is the same number, the scroll rule is the same rule and
the tool row counts the same outputs. The proof dumped the `/context`
panel's rows for the default model and a named model, the three viewer
panels' page sizes at heights 0/24/40, the viewport width and the
`/changelog` frame — **36 438 bytes** on both sides, `cmp` clean
(`sha256 ac7a6dc9…`).

### 39. The slash grammar gets one owner

The tree held **two** slash grammars and **three** copies of a trim.

The **host's** rule (`splitSlash`) requires a leading `/`, separates on
space / tab / newline and trims space + tab. The **print mode's** rule
requires no leading `/`, separates on `strings.Fields`' set and trims
`strings.TrimSpace`. They are observably different, and the difference
is reachable from a real dispatcher: `agy -p $'/help\vfoo'` dispatches
`/help` under the print rule and is an unknown command under the
host's.

The grammar moved to `internal/core/slashparse` (tier 1, `strings`
only): `Split` (the host's rule), `SplitVerb`, `SplitFields` (the
print rule), `Trim` and `TrimUnicode`. **The two rules are neighbours,
not one rule** — unifying them would be a behaviour change, not a
refactor, so the package holds both and a future unification is a
one-place change.

**The third copy of the trim** was `internal/ui/tuilib`'s private
`trimSpace`, byte-identical to the host's. It was deleted; the two call
sites name `slashparse.Trim`. The print mode's own `trimSpace` was a
one-line forward into the stdlib with a rule of its own; it is
`slashparse.TrimUnicode` now.

The proof crossed every rune in `` \t\n\v\f\r`` plus eleven Unicode
whitespace runes with fifteen shapes each, plus thirty-seven literal
shapes — 289 fixtures, 271 distinct inputs — and dumped six labelled
blocks per fixture: **76 772 bytes** on both sides, `cmp` clean. The
committed golden is byte-identical to the **base** dump, so the pin is
the pre-change behaviour and not the new behaviour.

### 40. The server owns its bind address

`internal/cliapp/server_host.go` declared the server's default bind
address (`127.0.0.1:7777`), the loopback-by-default policy and the
address resolution that maps the flags onto it. The **server** is the
thing that binds — it runs shell commands against its own workspace, so
it must not listen on the network unless the user asks for it. Those
facts belong to `internal/app/agentserver` (tier 2), not to the host.

The policy moved to `internal/app/agentserver/addr.go`:
`DefaultHost`, `DefaultPort` and `ResolveAddr(addr, host string, port int) string`.
The host's `resolveServerAddr`, `defaultServerPort` and
`defaultServerHost` are **deleted** — no forwarder, no alias.

**The tier rule shaped the signature.** `internal/app/agentserver` is
tier 2 and may not import `internal/cli` — a host. `cli.ServerArgs` is
the host's typed argv carrier, so the server package takes **plain
values** and the host maps its own struct onto them. `serverFlagSet`
stays in the host: registering flags belongs to the parser.

**The tranche also removed a host edge.** `internal/app/agentserver`
imported `internal/cli` only for `cli.ReferenceVersion`; it now reads
`internal/core/version` (tier 1), so the app tier → engine-host edge
count dropped from **21 to 20**. The guard prints the number on every
run.

The address resolution is the same function, moved verbatim: twelve
inputs (an address, a host, a port, their combinations, a trimmed
address, a trimmed host, a negative port) produce the same address on
both sides.

### 41. The transcript's repaint engine

`internal/cliapp/tui_turn_blocks.go` was the host's biggest file at
**874 lines**, and it held two different things: the **repaint
arithmetic** — the pure logic that rewrites the response row buffer in
place when the width, the theme, the verbosity, the expansion state or
the scroll window changes — and the **host facts** (`m.response`,
`m.blocks`, `m.assistants`, `m.userBlocks`, the scroll state, the
expansion flags, the cache-dirty flags). The arithmetic is transcript
mechanics; the facts are the host's.

The arithmetic moved to `internal/ui/transcript/repaint.go` (**771
lines**): `RerenderBlocks`, `EnsureVisibleBlocksExpanded`,
`RerenderForTheme`, `Reflow`, `SyncLiveThoughtStep`,
`FinishLiveThinking` and `RerenderLiveSteps`. `blockUpdate` and
`applyBlockUpdates` are now unexported inside the package. The host
file is **271 lines** — a reduction of **603 lines**.

**The document stays a value.** `transcript.Document` is not a pointer
and not a buffer the tier owns: the host builds it from its own
slices, hands it to a mechanic and takes it back. `ReplaceRange`
mutates the caller's backing array exactly as the host's method did, so
a caller passes a fresh slice where the old behaviour required one.
The tranche added one field to the document — `UserBlocks` — because
the base's repaint code reached the host's `m.userBlocks` directly and
the mechanic cannot see the host.

**The seam is one options struct, not two.** The existing
`transcript.Options` gained `Height`, `Streaming`,
`ViewportHeight func() int` and `Thought LiveThought`; the engine
takes the document and the options.

**The host keeps its seven wrapper methods.** Each owns a cache
invalidation (`rowsCacheDirty`, `viewCacheValid`, `rowsCache`,
`rowsView`, `cachedHeaderKey`) that the ui tier has no business
knowing. Deleting them would move the host's cache facts into the
tier, which is the *other* half of the split and a much larger
tranche.

**Byte parity is the acceptance criterion, and it was proved against
the base tree.** The golden (`internal/cliapp/testdata/transcript_repaint.golden`,
**16 307 bytes**) was produced by the **base** commit `9a2857e7` — a
scratch copy outside the repo's worktree list — running the same test
file with only its tail patched to write instead of compare. The
golden was copied back verbatim (`cmp` clean) and the unmodified test
passes. Appending one byte makes the test fail; restoring it makes it
pass. The dump stages initial → theme-dark → reflow-50 →
verbosity-high → expanded → live-thought → finished-thought →
visible-window, and masks only the thought's elapsed time — the clock
is not part of the transcript's bytes. `COLUMNS=80` and a fixed
palette scheme keep it machine-independent.

The golden lives at **host level** (`internal/cliapp/testdata/`) and
not under `internal/ui/transcript/testdata/`, because it must exercise
the whole host → transcript path; the package-level `repaint_test.go`
pins the engine's own contract separately.

The tiergraph moved by one edge and by no package: **225 production
packages / 205 test packages**, 0 upward edges, 0 tier → host edges, 0
cycles. The one new edge is `internal/ui/transcript →
internal/ui/foundations/viewport` (for `IsUserPromptRow`), which the
base host code used directly.

### 42. The slash-command vocabulary gets one owner

The slash-command surface was **three tables** that each held a part of the
vocabulary, and they disagreed:

| table | what it held |
|---|---|
| `internal/cliapp/slash_commands.go` | the host's dispatch table — 56 keys, five of them alias spellings spelled out by hand (`"new"`, `"quit"`, `"featdiff"`, `"diff-agy"`, `"record"`) |
| `internal/ui/tuilib/commands.go` | the picker's catalogue — 109 rows, the aliases in an `Aliases` field, and a **third copy** of the alias rule (`canonicalPanelCommand` in the host) that scanned the catalogue at runtime |
| `internal/ui/panelregistry/command.go` | the command → panel table, keyed by the canonical name only |

The new package **`internal/ui/slashcommands`** (tier 5) owns the
vocabulary: the rows, the alias rule and the answer to "which panel does
this command open".

| symbol | what it answers |
|---|---|
| `Command` | one row: `Name`, `Description`, `Aliases`, `Panel`, `Dispatched` |
| `Default` | the 109 advertised rows, in registration order |
| `Hidden` | the 6 commands the host answers and the picker does not advertise |
| `Canonical` | the alias rule: a name resolves to itself, an alias to the command it names, anything else to nothing |
| `PanelFor` | the command → panel answer, alias resolved first |
| `Known` | the composer's "is this a command" question |
| `Sorted` | the `/help` COMMANDS tab's alphabetical order |
| `Filter` | the picker's case-insensitive substring filter |
| `FilterWithCustom` | the picker's integration point for custom commands |

The catalogue is **byte-identical** to the old `tuilib` table — 109 rows,
the same names, descriptions, aliases and order, checked row by row
against the pre-tranche tree. The rendered bytes are unchanged: the
`/help` COMMANDS tab's golden and the picker's rows are the same bytes.

**The command → panel table still has exactly one owner.** The panel kind
of a row is *derived* from `internal/ui/panelregistry` at init, so the
vocabulary reads it rather than carrying a second copy.

**The host's table is keyed by the canonical name only**, and the typed
name is resolved through the vocabulary before the lookup. Two handlers
that were pure forwarders are gone: `slashNew` (a forward to
`slashClear`) and `slashRecord` (a forward to `slashVoice` under a
different trigger label, which nothing read).

**The alias rule reaches the handler now**, so the two advertised aliases
the host's own table never carried — `/switch` and `/conversation`, the
spellings the `/help` COMMANDS tab shows for `/resume` — answer with
`/resume` instead of `Unknown command: /switch`. Every other spelling
resolves as it did before.

**One conflict is resolved by hand.** The catalogue lists `"branch"` as an
alias of `/fork`, and the host answers it with a command of its own — so
the vocabulary resolves the word to **itself** and never to `/fork`.

**The print mode keeps its own copy** of the alias spellings
(`internal/printmode/print_slash.go`) on purpose: it has its own slash
grammar, and canonicalising the names there would change what a
print-mode run answers.

The moved catalogue's own tests move with it (the order, the CJK filter
and the custom-command merge), and the host's pins are new: the
vocabulary's dispatched set against the host's table, the table's keys
against the canonical names, and every advertised alias against the
handler it reaches.

The tranchegraph moved by one package: **227 production packages / 207
test packages**, 0 upward edges, 0 tier → host edges, 0 cycles.

### 43. The live-step batch mechanic

`internal/cliapp/tui_update_stream.go` held the **batch arithmetic**:
whether a running step continues the previous batch or starts a new one,
where the batch starts in the response buffer, which rows the batch
replaces and how many rows it occupies. It is mechanics over the host's
buffer (`m.response`, `m.liveStepRuns`) and the host's own state
(`m.liveBatch`, `m.batchBreak`, `m.verbosity`, `m.expanded`, `m.width`).

The mechanic moved to `internal/ui/transcript/livesteps.go` as
`*Document` methods — `AppendRunningStep`, `AppendLiveStep`,
`FinishLiveStep` — and the batch's membership rule moved next to the
step model as `internal/ui/toolsteps/batch.go`'s `BatchSteps`. The host
file went from **489 to 356 lines**; the host is now **15 050 production
lines over 94 files**.

**The step travels by pointer**, so the batch number the mechanic assigns
is the exact value the host's `m.liveSteps` keeps. `transcript.Options`
stays the single options struct, and `Document` stays a value.

**The host keeps its own resets** (`m.replyStart = -1`,
`m.liveTextLines = 0`, `m.liveReplyBuf = ""`) *before* the document round
trip, so `doc.ReplyStart == -1` keeps the delta shift a no-op exactly as
before.

**Byte parity was proved against the base tree** (invariant 182): a copy
of the base commit outside the repo's worktree list, running the same test
file with its tail patched to write instead of compare. The two goldens
are `cmp`-identical (md5 `018f0c465eb07d9e674ad112b33cefb6` on both),
**26 208 bytes**. Appending one byte makes the test fail; restoring it
makes it pass. The fixture gained a second, independent `runMode` so the
live-batch path does not disturb the pre-existing fixture's
`liveStepRuns`, and the shared dump now also prints
`liveBatch=%d batchBreak=%t`.

**Two pre-existing quirks are now pinned.** A thought between two calls of
the same batch is wiped by the join (the running-join branch replaces to
`len(Lines)`) while the thought run stays in the index with a stale
start; and a finished step whose running predecessor is not the last run
falls into the append path. Neither is a defect the move introduced — both
are pinned so a later change cannot "fix" them silently.

### 44. The host's one-line forwarders

A **forwarder** is a host method whose whole body is a call into another
package. It adds a hop and adds nothing — the call site could name the
owner directly. Twenty-three of them were gone in this tranche, and
every call site now names the package that owns the fact:

```
reloadAccountsRows      -> m.accountsPanel().Reload()
queueAccountsQuota      -> m.accountsPanel().QueueQuota()
updateAccountsQuotaMsg  -> m.accountsPanel().QuotaDone(...)
codesearchStart         -> m.codesearchPanel().Start(args)
maxBtwAnswerLines       -> sidequestion.MaxBtwAnswerLines(...)
changelogViewerLines    -> changelog.Document(...)
featDiffPanelLines      -> featdiff.PanelLines()
featDiffViewerSpec      -> featdiff.Spec(m.height)
usageViewerSpecREF      -> usagepanel.Spec(m.usageState())
builtinStatusRow        -> statusrow.Render(m.statusRowState())
hasPendingQuestion      -> m.subAgentQuestionPanel.HasPendingQuestion()
inputSelectionRange     -> m.store.InputSelectionRange()
clearInputSelection     -> m.store.ResetInputSelection()
selectionRange          -> tuiviewport.SelectionRange(...)
stickyUserPromptRow     -> tuiviewport.StickyUserPromptRow(...)
formatVisibleRows       -> tuiviewport.FormatVisibleRows(...)
pageRows/halfPageRows   -> tuilib.ClampInt(...)
viewerSpecFor           -> m.getPanelRegistry().ViewerSpec(...)
moveWordLeft/Right      -> m.moveWord(true/false)
loadHistory             -> m.promptHistory.Load(convstore.LoadHistory(...))
windowTitleValue        -> m.titleStoreFor().Get()
```

**Four were deferred** — `resetSessionTokenCounters`, `rcURLRows`,
`statusLineConfig` and `titleConfig` — because their production call
sites live in `slash_commands_handlers.go`, which another tranche owned.
They went in tranche 92 (below).

**Eleven were kept, with a reason each.** A forwarder that also
*adapts* — a different arity, a different type, a decision — is not a
forwarder; it is the host's own logic and it stays. Each kept one
carries the reason next to it.

**No behaviour change and no rendered-byte change.** The host went from
**15 050 to 14 922 production lines over 94 files**.

### 45. The four deferred forwarders

The four tranche 90 deferred are gone, and their call sites name the
owner directly:

| forwarder (old) | owner of the fact | new call site |
|---|---|---|
| `resetSessionTokenCounters()` | `internal/app/tuistore` (`Store.ResetSessionTokens`) | `m.store.ResetSessionTokens()` |
| `rcURLRows(url)` | the public `urlchunk` (the chunking and the OSC 8 frame) | the call site calls `urlchunk.Rows(...)`; the `buildRemoteControlCard(… urlRows func(string) []string)` **seam stays** |
| `statusLineConfig()` | the host's own field `runMode.statusLineCfg` | `m.statusLineCfg` |
| `titleConfig()` | the host's own field `runMode.titleCfg` | `m.titleCfg` |

Two of the four were not forwards into another package at all: they
returned the host's **own** fields through an accessor. An accessor over
a field is the same shape as a forwarder — a hop that adds nothing — and
the rest of the file already read the field directly.

**No alias or const forwarder was left behind**, and no new wrapper was
introduced to bridge two packages: the alternative would have been a new
shell. The host's exported surface is unchanged — all four were
unexported.

**The facts the forwarders' doc comments carried did not die with
them.** The caching rationale the `statusLineConfig` doc carried is on
the field itself ("read from disk once at startup and refreshed whenever
`/statusline` changes it"), and the URL-rows fact is in `urlchunk`'s own
package doc.

## Behaviour changes
The cycle was a move, not a redesign, but the move fixed what the
shared state had broken. Every change below was pinned by a test that
moved with the code it exercises.

- **The bare `/rewind` now really truncates the session.** The ui
  package's copy of the policy popped the trailing pair from a *local
  slice* and never wrote it back, so the bare command reported
  *"Rewound 2 message(s) from the session tail"* while the session was
  untouched. The session layer owns the policy now, and the caller
  assigns the truncated session. The count and the echo text are
  unchanged — and the path is unreachable in practice, because the
  `/rewind` picker claims the command first.
- **A cached quota snapshot now carries its bucket's id.** The snapshot
  was not a lossless cached form of the summary: it dropped the bucket
  id, and the status line keys its quota map by that id — so a quota
  read from the registry's cache rendered `"quota":null` instead of
  `"quota":{"3p-5h":…}`. `QuotaBucketSnapshot` now carries the id.
  The visible effect is in `internal/infra/agyproxy`: the **cached**
  path of the aggregate models endpoint now emits the real `bucketId`,
  where it used to emit an empty one. The live path already emitted it,
  so the cached path now agrees with the live one; no test pinned the
  old value.
- **The `/tasks` cursor moves.** The reference binds the arrows inside
  `/tasks`; the host's generic list navigation never reached the
  panel, so the cursor stood still. The panel owns its keys now.
- **The `/tasks` kill and remove act on the row the panel renders**,
  not on the registry's uncapped list.
- **The `/rewind` cursor is the panel's own**, not the host's shared
  list cursor — a `/rewind` cursor that moved the shared one was a
  cursor in two places at once.
- **The `/rewind` visible window follows the cursor.** It used to stay
  put, so the cursor could walk off the top of the visible rows.
- **The `/config` panel no longer shares the host's cursor or window**
  at all: the panel owns the search query, the cursor, the scroll
  window, the submenu, the submenu's cursor and the memory-only values.
- **The `/effort` slider owns its marker**, so the /effort panel no
  longer shares a cursor with the panels that use the list navigation;
  the panel seeds itself on the tier the session runs at, which is what
  the reference opens the slider on.
- **The bare `/rename` works.** It opened a panel that was never built
  there: an empty, unfocused input, no prompt, and the typed name went
  nowhere.
- **A `/codesearch` that has not started yet is no longer "over".**
  The panel created its own `Matches` channel, and a nil channel read
  as "the search is over", so a search whose runner goroutine had not
  been scheduled left the panel stuck on "Searching…".
- **Closing `/codesearch` cancels the search.** The old `csCancel`
  field was write-only dead code, so a live search could outlive its
  panel — the runner's goroutine and the ripgrep child it spawned kept
  running.
- **The /feedback picker wraps**, the reference's own rule, and the
  host's `panelKind.wraps()` is gone with it.
- **The `/usage` panel's note colours come from the palette** instead
  of a literal. The palette's default scheme carries exactly the
  reference's values (238;232;213 and 101;123;131), so the rendered
  bytes are unchanged under the default scheme.
- **The `/changelog` document is parsed once.** The document is built
  from 180 KB of embedded markdown, and the build ran on every TUI
  draw — **511 760 ns/op** and about 150 KB of garbage per frame,
  even in the common "no source" case where the parse is thrown away.
  The parse is now memoised inside `internal/ui/views/changelog` with
  a `sync.Once`. The rendered bytes are unchanged (the two-tree dump
  proves it), and the frame no longer pays for the parse.
- **The `/tasks` panel's "kill the subagent behind this row" branch is
  gone.** The branch looked up a *package-level* subagent manager that
  `cmd/wagyu` used to wire at start-up. `cmd/wagyu` was removed, so
  the manager was never set and the branch could never fire — the
  lookup always returned nil and the panel's own session manager
  served the kill. The branch, the package-level variable and its
  exported setter are gone; the behaviour is the one the code
  already had.
- **The manual `/compact` path loses its unreachable adapter.** Cycle
  229 shipped `pigrtCompactor`, a thin adapter over
  `*pigrt.Runtime`, as the production implementation of the
  `manualCompactor` seam. It had no caller and no test: the TUI's
  turn engine is PiG's `agent.Agent`, not `pigrt.Runtime`, so the host
  had no runtime for it to wrap. The manual `/compact` has therefore
  always taken the generic summarizer branch (`m.compactor` is nil in
  production; only the tests set it). The interface and the branch
  stay — they are the seam — and the unreachable implementation goes.
- **The auto-compaction spinner moves.** Its tick command produced
  `tuiviewer.CompactSpinnerTickMsg` while the host's update switch
  named its own `compactSpinnerTickMsg` — two distinct types, so the
  switch never matched and the status row sat on its first Braille
  glyph for the whole compaction. Commit `64e71d67` (the tick commands'
  move into the shared viewer package) aliased the `/usage` panel's
  tick message and forgot this one. The message and the command that
  arms it now live in one file, and the pin is end to end: the
  command's message advances the frame.

The 34th tranche changed no behaviour: every retired forwarder's body
was a call the call sites now make themselves. What it changed is the
*source* — the host no longer holds a vocabulary layer over the
packages that own the facts, so a reader of `internal/cliapp` sees the
package that answers, not a name that answers it.

The 35th tranche changed no behaviour either — the picker's rows, its
keys and its two confirmations are the bytes the host wrote before the
move, and the panel asserts them. What it changed is the *shape*: eight
runMode fields and a 365-line host file became a package with one
owner.

The 36th tranche changed no behaviour either — the four tabs render the
bytes the host wrote before the move, and the panel asserts them. What
it changed is the *shape* again: four runMode fields and three host
files became a package with one owner, and the host's second copy of
the overlay's colours went with it.

The 37th and the 38th tranches changed no behaviour either — the
slash-command popup's rows and the mention dropdown's rows are the
bytes the host wrote before the moves, and both panels assert them.
What they changed is the *shape* again: the slash popup's five host
fields became one panel, and the mention dropdown's widget left the app
tier for the ui tier, where the panels live.

The 39th tranche changed no behaviour either — the active-items block's
rows, its window's tails and its cursor's trajectory are the bytes the
host wrote before the move, and the two-tree dump proves it (3 259
bytes, identical). What it changed is the *shape* again: a 273-line
host widget with a cursor, a scroll offset, a focus flag and a window
became a package with one owner, and the host's field became a pointer
to it.

The 40th tranche changed no behaviour either — the status row's two
segments, its padding and its compaction label are the bytes the host
wrote before the move, and the two-tree dump proves it (15 465 bytes,
identical). What it changed is the *shape* again: 172 lines of the host
became a package with one owner, a fact that lived in an entry surface
above tier 5 moved down to tier 1, and the host's second copy of the
row's grey went with it.

The 54th tranche changed no behaviour either — the dictation overlay's
four phases, its timer and its hint line are the bytes the host wrote
before the move, and the three-way dump proves it (1 231 bytes,
identical). What it changed is the *shape*: the state machine that
decides which view to draw moved next to the package that already owned
the banner, the timer and the recorder.

The 55th tranche changed no behaviour either — the composer's selection
arithmetic and the prompt history's push, trim, dedupe, navigation and
reset are the bytes the host wrote before the move, and the two-tree
dump proves it (8 603 bytes, identical). What it changed is the *shape*:
two independent facts left the host for tier 5, and a one-line forwarder
(`historyReset()`) was deleted rather than forwarded.

The 56th tranche changed no behaviour either — the command → kind table,
every kind's row count and every kind's answer to 22 keys are the bytes
the host wrote before the move, and the two-tree dump proves it
(460 274 bytes, identical). What it changed is the *shape*: the host's
two hand-written route chains and its fourteen-case row-count switch
became the registry's own tables.

The 57th tranche changed no behaviour either — the three flags'
defaults, the explicit values, the invalid values, the new-project path
and the projects cache are the bytes the host wrote before the move, and
the two-tree dump proves it (3 426 bytes, md5-identical). What it changed
is the *shape*: a flag's *policy* is a tier-1 fact, and the flag's
*registration* stays with the parser that owns the table.

## 硬约束

- **硬约束**: a **panel owns its state, its rows and its keys**; the
  host keeps facts. A `Seams` struct of callbacks, or a `State` of
  values — never the behaviour.
- **硬约束**: a **panel is rebuilt on every open**. A panel that
  survives a close carries a stale cursor, a stale stage or a stale
  draft into the next open. **The exception is a panel whose staged
  answer outlives its overlay**: the answer is typed into the
  composer's prompt and submitted there, so the picker is built once
  with the session and its cursors are reset on open.
- **硬约束**: a **panel answers `handled=false` for the keys it does
  not own**, so Ctrl+C still reaches the global handlers. **The
  exception is a modal overlay**: while it is up it claims every key
  under it, which is what the reference's own picker did.
- **硬约束**: **a panel reads the palette directly; the host keeps no
  colour alias for it.** The host used to re-read the overlay's colours
  into its own aliases whenever the scheme changed — a second copy of a
  fact `tuilib` already owns, and `tuilib` refreshes its own aliases on
  the same event. A copy of a colour is a copy that goes stale when a
  panel is added.
- **硬约束**: **a completion popup over a live composer is not modal.**
  It owns the navigation keys (Up, Down, Tab, Esc) and answers
  `handled=false` for every other key, so the letters the user types
  reach the composer. A modal overlay is the opposite: it claims every
  key while it is up.
- **硬约束**: **a panel that keeps its own copy of a colour refreshes
  it.** The host used to re-read the popup's colours into its own
  aliases whenever the scheme changed; the panel reads the palette
  directly now and registers its own `palette.OnSchemeChange`
  callback, because an alias copied once keeps the old scheme's escape.
- **硬约束**: **a catalogue and the view over it are different
  owners.** `tuilib.SlashCommand`, `DefaultSlashCommands`,
  `FilterSlashCommands` and `containsFold` are the catalogue — the list
  of what the reference offers. The popup is the view over it, and the
  view is the half that moves.
- **硬约束**: **a widget's facts stay in the tier that owns them; the
  widget moves to the tier that renders it.** The mention grammar and
  the workspace listing stayed in `internal/app/prompt/mentions/files`
  (the app tier); the dropdown that renders them is
  `internal/ui/views/mentionpicker` (the ui tier). A panel may import
  the tier below it.
- **硬约束**: **a completion popup that holds no candidate still
  swallows its completion key.** The mention dropdown's Enter
  completes the mention; when there is nothing to complete, the Enter
  is swallowed anyway and the prompt is not submitted. A popup that
  falls through to the host's submit on an empty list is a different
  popup.
- **硬约束**: **a host asks a panel to complete; it does not claim the
  key itself.** The host's Enter path carries the newline, the
  queue-mode and the submit branches, so the panel cannot claim Enter
  wholesale — the host asks the panel to complete and keeps the
  history reset, which is the host's fact.

- **硬约束**: **the overlay that owns a surface writes its own exit
  line.** The "/help" block and the "Exited /help command" line are the
  panel's, so the host's Esc path and the panel's own "q" produce the
  same bytes and the host keeps no echo of its own.
- **硬约束**: **an overlay's open resets its window and its cursor.**
  The reference does not keep a per-tab scroll position, and it does not
  keep one across an open either.
- **硬约束**: a **list panel owns its cursor**. The host's shared
  `m.panel.cursor` / `m.panel.window` are for the panels that do not
  own one; a cursor in two places is a cursor in one place by
  accident.
- **硬约束**: a **viewer panel does not own the offset**. The host's
  window offset is shared with /codesearch and the /tasks detail views,
  so the viewer takes it as an argument and hands the clamped one back.
- **硬约束**: **the ui tier imports no host.** None of the new
  packages names `internal/cliapp`, `internal/agent`, `internal/pigrt`
  or `internal/piagentbridge`.
- **硬约束**: **a test follows its subject.** A test that drives a
  panel's rows, keys or writes belongs to the panel's package; the host
  keeps the wiring test and the host's own facts. A duplicate of a
  package's own test is deleted, not copied.
- **硬约束**: **a shell is a shape, not a filename** (cycle 377). A
  one-line forward into another package is a shell whether or not
  anything calls it; the call sites name the package that owns the
  fact.
- **硬约束**: **a tick's message and the switch that handles it are one
  fact.** The message the command produces and the message the host's
  update switch names have to be the *same type*, and they live in one
  file so a second type cannot be introduced beside them.
- **硬约束**: **a "generic" package that holds one owner's facts has no
  owner.** A fact belongs to the tier that owns it, not to the package
  that happened to be near it.
- **硬约束**: **the compiler does not catch an unused function.** Go
  complains about an unused import or an unused local variable, never
  about an unused function, so dead code accumulates silently in the
  biggest package. `internal/cliapp/deadcode_test.go` is the guard: it
  parses the package with `go/ast` and asserts that every unexported
  declaration the host makes is reachable from the host's own
  **production** code — a test file is not a caller, and liveness is
  transitive. A declaration that is deliberately kept although the
  product does not reach it carries a `deadcode: staged` marker with
  the reason next to it, or an entry in the allowlist map.
- **硬约束**: **a dead export is deleted unless it carries reference
  provenance.** A recovered reference string, a recovered behaviour or
  a guide-protected alias is kept, and the reason is recorded in the
  cycle report — not in a comment the next reader has to interpret.
- **硬约束**: **the sweep is instrumented, not grepped.** A name
  counts as live only when a call site really names it; a bare-name
  search has false positives (a name inside a string) and false
  negatives (a truncated head).
- **硬约束**: **a domain keeps its own routing table.** The dispatcher
  asks the domain and does not name the domain's method names; a
  dispatcher that re-implements a domain's cases inline owns the
  domain's vocabulary too.
- **硬约束**: **a forwarder with a caller is still a shell.** The
  dead-export sweep finds what nobody calls; a one-line forward into
  another package is a shell whether or not anything calls it
  (invariant 68), and the call sites name the package that owns the
  fact.
- **硬约束**: **a type alias to another package's type is a shell.**
  `type X = otherpkg.X` is the same shape as a forwarding function, and
  it goes the same way.
- **硬约束**: **a retired forwarder's doc facts move down, or they die
  with the shell.** A sentence that is only true of the shell dies
  with it; a sentence about the behaviour moves to the package that
  owns the behaviour; a sentence that turns out to be false is
  dropped, not relocated.
- **硬约束**: **the gate belongs to the surface it switches off.**
  "Show Active Tasks" is the setting that switches *the active-items
  block* off, so the block reads it — the panel's `Hidden()` is the
  host's old `isHideActiveItems()`. A host that keeps the gate and
  asks the panel "are you visible?" keeps a fact about a surface it
  does not own.
- **硬约束**: **a panel's host field is a pointer, and every method
  of the panel is safe on a nil receiver.** A dozen host tests build a
  `&runMode{}` by hand; a panel that panicked on a nil receiver would
  take all of them down. The panel answers "nothing is running" and
  writes nothing.
- **硬约束**: **a move may drop a method that nothing called.** The
  active-items widget's `SelectLast` had no caller. As a method of an
  exported type it would have been a dead export (invariant 94), so it
  went with the widget instead of travelling to the new home.
- **硬约束**: **a view takes a `State`; only a surface that acts takes
  a `Seams`.** The status row has no keys and no writes of its own, so
  it draws from a `State` of values. A `Seams` of callbacks for a view
  that never calls back is a shell in a struct.
- **硬约束**: **an entry surface is above tier 5, so a fact the ui tier
  needs cannot live there.** `internal/printmode` is an entry surface;
  the ui tier may not import it. The token formatter moved down to
  `internal/core/tokenfmt` (tier 1), and `printmode.TokenK` is gone
  rather than left behind as an alias.
- **硬约束**: **a number has one owner.** The row's canonical width is
  the host's `separatorWidth`, handed in as `State.CanonicalWidth`. A
  second copy of the constant is a copy that goes stale when the first
  one moves.
- **硬约束**: **a move renames; it never re-exports.** The transcript's
  four record types were the host's `turnBlock` / `userBlock` /
  `assistantBlock` / `liveStepRun`; they are now `transcript.Block` /
  `UserBlock` / `AssistantBlock` / `LiveStepRun`. The host keeps no
  `type turnBlock = transcript.Block` alias — a package-level alias
  into another package is a shell, whether or not anything calls it.
- **硬约束**: **a render cache has one owner.** The tool-step block
  carries a two-slot cache (collapsed rows, expanded rows, each keyed
  by the width and the verbosity it was painted at). The host used to
  reach into `b.cachedCol` / `b.cachedExp` and seed them itself; the
  package owns the cache now and exports `NewBlock`, which picks the
  slot from the block's own `Expanded`. A cache the host can stomp is
  a cache with two owners.
- **硬约束**: **a classifier and a row are different owners.** The turn
  error's message is classified in the host (`agent.TurnErrorMessage`),
  because it classifies the host's error type; the row that prints it
  is a view (`toolsteps.ErrorRow`), and it owns the trim.
- **硬约束**: **a pure helper that reads host state must be split, not
  moved.** The composer's mouse-to-offset arithmetic was a method that
  read the value, the line count and the prompt's screen row. The
  arithmetic moved (`inputselection.OffsetAt`); the host kept a thin
  method that supplies the three facts.
- **硬约束**: **the five changelog failures in `internal/cliapp`** are
  pre-existing and unrelated — the four changelog tests hardcode
  `## 1.4.9` / `1.4.10:` while the feed now starts at `1.4.11`, and
  `TestChangelogSource_LoaderFindsReference` looks for the reference
  binary's changelog on disk.
- **硬约束**: **a state machine moves next to the views it decides
  between.** The dictation overlay is four phases — idle, connecting,
  recording, finishing — and every view (the banner, the timer, the
  failure notice) follows from the phase. The package that owned the
  views did not own the state machine that chose one, so the machine
  was in the host; it moved next to the views.
- **硬约束**: **a surface's seams are the facts the host owns.** The
  voice surface answers "which phase are we in?" and "what does the
  banner say?"; it cannot answer "how is a transcription session
  established?" or "where does a confirmed draft go?". Those are the
  seams, and every one of them is optional so the package is usable
  from a test without a running TUI.
- **硬约束**: **a store stays with the host; the arithmetic over it
  moves.** The composer's selection *store* reads and writes host
  state (`m.store.InputSelStart/End`), so it stays; the *value*
  arithmetic over a range (`CursorPosition`, `DeleteRange`,
  `ReplaceRange`) is pure and moved. A pure helper that reads host
  state is split, not moved.
- **硬约束**: **a rule that two callers ask has one owner.** The
  blank-prompt rule was introduced for the prompt history, and the
  submit path asks the same question; the rule moved with the history
  as `history.IsBlank` rather than staying in two places.
- **硬约束**: **the registry owns the routing; the host owns the
  facts.** The registry knows which panel a command opens and what each
  panel's kind means; the row counts and the `/codesearch` handler
  are the host's and arrive through the wiring file. A dispatcher that
  re-implements a domain's cases inline owns the domain's vocabulary
  too.
- **硬约束**: **a flag's policy is a tier-1 fact; its registration
  stays with the parser.** `--project`, `--browser` and `--log-file`
  say what a value *means*, so their policies moved to tier 1; the
  flag table, the defaults, the aliases and the help texts are the
  parser's own vocabulary (`cli.FlagSpec`) and stayed.
- **硬约束**: **a tier-1 policy that needs a tier-4 fact takes it as
  a seam.** A new project's id is a conversation id, and the generator
  lives in tier 4, so `internal/core/cliflags/projects` cannot call it
  — the host supplies it.

- **硬约束**: **a badge's bytes and the "is it on" fact have two
  owners, and the split is by question.** The three badge strings belong
  to the engine (`vim.Mode.Badge()`); whether vim is on is the host's
  fact, so `vimkeys.Badge(enabled, mode)` takes it as a parameter. The
  host's collector must not build the engine to ask it a question —
  the engine must not exist when vim is off.
- **硬约束**: **the transcript's buffer stays the host's; the ui tier
  owns the mechanics.** `m.response` is read from twenty-odd files the
  transcript package does not own, so a type that *owned* the buffer
  would have put the whole host behind a constructor. A `Document`
  value is built from the host's slices, handed over and taken back.
- **硬约束**: **a tier that needs a host's emptiness rule receives it
  as a value.** The turn's emptiness rule stays in the host and travels
  in as `Options.EmptyTurn`, so the ui tier never imports
  `internal/app/agentturn` and through it never imports the engine host.
- **硬约束**: **a colour is read at the use site, and the palette's
  value is the owner's.** The host's `cDimGrayOpen` was a direct
  assignment from `palette.DimGrayOpen`; the moved code reads the
  palette directly, and a refresh alias is a second owner of a colour.

- **硬约束**: **a seam's callbacks are required; its widgets are
  nil-safe.** The composer's `Picker`, `Files` and `History` are nil
  when the panel was never opened, and the ui tier must not panic on
  them; the `Seams` callbacks are required, and a nil one is a
  programming error the doc comment names.
- **硬约束**: **the OS write stays host-side.** A ui-tier package may
  *decide* that a selection is copied and hand the text over; the
  host performs `clipboard.Write`. A ui package that wrote to the
  system clipboard would be doing I/O the host owns.
- **硬约束**: **a widget crosses a seam as an interface, and the host
  keeps ownership of it.** The composer's `Editor` interface and its
  `TextArea` adapter write *through* to the host's textarea; the ui
  tier never holds the widget itself.
- **硬约束**: **two key handlers with different tails are not one
  handler.** The Enter key's alt/shift branch dismisses the mode hint
  and the ctrl+j branch does not; a split that duplicated the composer
  half would be a second owner of that rule.


- **硬约束**: **the policy of a mutating operation belongs to the layer
  that owns the state it mutates.** The rewind truncates the live
  in-memory session, and the session layer owns that slice — so the
  policy lives in `internal/app/session/rewind`. A ui package that
  mutates a slice it does not own cannot write the result back, and
  that is not an implementation detail: it is the bug.
- **硬约束**: **a panel that names a thing does not own the thing's
  mutation.** The rewind panel's job is to name the picked step; the
  session's job is to truncate. The panel keeps the cursor, the window
  and the keys; the policy keeps the count and the echo.
- **硬约束**: **a second copy of a path shape is a second owner of the
  fact.** The CLI's log file's name was assembled in three places;
  `confhome.CLILogPath(ts)` is now the one owner. The two remaining
  copies are recorded below.
- **硬约束**: **an inlined shell keeps its guards.** The empty-model
  check in the context-size expression and the nil-task-reg check are
  load-bearing, not boilerplate: `modelctx.ContextWindowFor("")`
  returns the *default* window, not zero.


- **硬约束**: **a type that a ui panel needs must live below the ui
  tier.** The catalogue was in an engine host, so the panel carried a
  second copy of `Agent` and the host mapped one into the other field
  by field. The duplication was not laziness — the host was the only
  reachable home for the type. Moving the owner below the tier
  removes the copy without a forwarder.


- **硬约束**: **the store that owns a record owns its assembly.** The
  conversation store owns the trajectory table, so it owns the steps
  the table is built from. The host holds the four facts — they are
  the live state of a running session — and hands them over; the
  store decides what they mean.


- **硬约束**: **a second copy of a fact is a copy, even when it is a
  literal.** The `/context` panel's title row carried the literal
  `"Gemini 3.8 Flash (High)"` while the catalogue named the default;
  the viewport's fallback width was `200` in two packages; the
  `/changelog` scroll rule was re-implemented in the host. A literal
  is a second owner exactly as much as a copied function is.


- **硬约束**: **the slash grammar has one owner, and its two rules are
  neighbours, not one rule.** The host's rule and the print mode's rule
  differ observably at `\v`, `\f`, `\r` and at every Unicode whitespace
  rune, and the difference is reachable from a real dispatcher. They
  live in one package (`internal/core/slashparse`) so a future
  unification is a one-place change — but unifying them is a
  **behaviour change** and needs its own evidence.
- **硬约束**: **the repaint engine is a mechanic, and the host keeps
  the decisions.** The engine rewrites rows; the host owns
  `m.response`, `m.blocks`, `m.assistants`, `m.userBlocks`, the scroll
  state, the expansion flags, the cache-dirty flags and every "when to
  repaint" decision. The engine's seven entry points are
  `*Document` methods over a `Document` **value** — the host hands the
  document over and takes it back.
- **硬约束**: **the transcript's rendered bytes are pinned against the
  base tree, not against the new tree.** The golden was produced by the
  base commit and copied back verbatim; a golden produced by the new
  tree would pin the change, not the behaviour.
- **硬约束**: **the slash-command vocabulary has one owner.**
  `internal/ui/slashcommands` owns the rows, the alias rule and the
  command → panel answer. The host's dispatch table is keyed by the
  canonical name only, and the panel kind of a row is *derived* from
  `internal/ui/panelregistry` at init — never copied.
- **硬约束**: **an alias the host answers with a command of its own
  resolves to itself.** `"branch"` is listed as an alias of `/fork` in the
  catalogue, and the host has a `/branch` of its own; the vocabulary
  resolves the word to `/branch`, never to `/fork`.
- **硬约束**: **the print mode keeps its own alias spellings.**
  `internal/printmode/print_slash.go` has its own slash grammar, and
  canonicalising the names there would change what a print-mode run
  answers.
- **硬约束**: **the live-step batch mechanic is the document's; the host
  keeps the buffer and the decisions.** The batch arithmetic, the batch's
  membership rule and the step-row growth live in `internal/ui/transcript`
  and `internal/ui/toolsteps`; `m.response`, `m.liveStepRuns`,
  `m.liveBatch`, `m.batchBreak`, the cache-invalidation flags and every
  "when to render" decision stay in the host.
- **硬约束**: **the step travels by pointer** into the mechanic, so the
  batch number the mechanic assigns is the exact value the host's index
  keeps.


## 存疑

- **存疑**: the **`Seams`/`State` split is a judgement, not a
  law**. Six panels take callbacks (they have to *do* something: apply
  a model, close the panel, kill a task) and seven take values (they
  only have to draw). A panel that grows a write will move from one to
  the other, and the move is a small diff — but the two shapes are not
  interchangeable, and the guide does not say which to pick.
- **存疑**: **the panel packages are not independent of each other**.
  `effortpanel` owns the slider that `modelpicker` borrows, and
  `contextpanel` and `rewindpanel` own vocabulary their host files
  also mention. The cycle did not enforce a "panels never import
  panels" rule; it enforced the tier rule, which allows a tier-5
  package to import another tier-5 package.
- **存疑**: **the /usage panel's document has two shapes** — the
  height-aware one the TUI renders and the cycle-193 static one the
  unit tests render. The static path is a fallback for "the terminal
  size is unknown", which in the real TUI never happens. It is kept
  because the reference's captured bytes are pinned through it, but it
  is a second implementation of the same panel.
- **存疑**: **the status row's hint rules live in the wrong order for a
  reader.** The row applies the five *rules* (the popup, the stream,
  the queue, the buffer, the bash mode) and then four *overrides* (the
  vim badge, the overlays' clearing, the exit confirmation, the
  compaction label). The overrides are not a separate phase in the
  reference — they are the last writer of the same cell — and the split
  into "rules" and "overrides" is this clone's reading of it. The bytes
  are pinned either way; what is not pinned is whether a rule that
  would fire *after* an override is supposed to lose to it. Every
  current rule loses to every current override, and the reference's own
  order is not recovered.
- **存疑**: **the tree grew** by roughly 4 000 production lines across
  the new packages. Some of that is documentation headers, which are
  cheap. Some of it is the per-panel style constants and the per-panel
  copies of the panel hint rows — the packages are independent, so the
  constants are duplicated rather than shared.
- **存疑**: **the `/diff` panel's fold state is the panel's own, and
  the host reaches into it.** The 25th tranche moved the viewer out of
  the host, and the panel owns its per-hunk fold map — but the host's
  global Ctrl+O handler runs *before* the panel's own key handler, so
  the panel has to expose `ToggleFoldAtCursor()` for the host. The
  panel is not yet the only thing that can reach its state.
- **存疑**: **the manual `/compact` has never used PiG's compaction.**
  The `manualCompactor` seam and its `m.compactor` field are wired by
  the tests only; in production the field is nil and the generic
  summarizer answers. The 27th tranche deleted the unreachable
  `pigrtCompactor` adapter, so the question is now open rather than
  hidden: either the TUI grows a `pigrt.Runtime` and the seam is
  wired for real, or the seam and its branch go as well. The
  reference's own `/compact` is a real summarisation either way, so
  the clone's *output* is not affected — but the two paths differ in
  what they summarise.
- **存疑**: **the compaction spinner borrows its frame from the `/usage`
  panel.** `usagepanel.SpinnerFrame` is the eight-frame Braille set,
  and the compaction status row draws it — so a `/usage` panel's fact
  renders in a row that has nothing to do with `/usage`. The frame set
  is a *shared spinner* fact and wants its own owner (a small
  `internal/ui/foundations/spinner`); the 26th tranche left it where it
  was, because moving it would rename a public symbol that the panel
  and the host both name.
- **存疑**: **the mention dropdown's Enter is routed by the host, not
  by the panel.** The panel's `HandleKey` owns Up, Down and Tab; the
  host calls `Complete(false)` for the Enter path. A panel that owns
  its surface should own its keys, but the Enter path's branches are
  the host's, and the reference's swallowed Enter has to happen in the
  middle of them. The two should be one.
- **存疑**: **the mention dropdown's colours are hard-coded, not the
  palette's.** The slash popup's four colours follow the scheme; the
  mention dropdown's five are the reference's own values and do not
  repaint. Two popups, two colour policies, in the same composer.
- **存疑**: **the panel's `Update()` is called from ten places.** The
  host re-derives the dropdown's state after every keystroke, and the
  derivation is the panel's. The host's ten call sites are the price
  of the dropdown living in a package that cannot see the composer.

- **存疑**: **the host closes the popup from sixteen places.** The
  panel exposes `Cancel()` for the Esc path and `Reset()` for the "the
  composer no longer names a command" path, and the host calls the
  second one sixteen times (`tui_prompt_submit.go`, `slash_commands.go`,
  `history.go`, `picker_model.go`, `prompt_at.go`,
  `tui_update_mouse.go`). A panel that owns its surface closes it with
  one call; the popup's close is two vocabularies, and the host picks
  between them by reading the call site. The move did not change the
  count — it is the same `m.picker.Reset()` the host called before — but
  the panel now has a `Cancel()` that says the same thing in the
  panel's own words, and the two should be one.
- **存疑**: **the popup is built once with the session, not rebuilt on
  every open.** It is a *derived* view: its rows are re-derived from the
  composer's buffer on every keystroke (`Update`), so an open has
  nothing to reset. The rule "a panel is rebuilt on every open" covers
  the panels that carry state, and it does not say what a derived view
  does.

- **存疑**: **the `/help` overlay's COMMANDS tab is pinned by shape, not
  byte for byte.** The tab renders the clone's whole slash-command
  catalogue — 109 rows today — and that catalogue grows with every new
  command, so a full byte pin would fail on a feature that has nothing
  to do with the overlay. The GENERAL, SHORTCUTS and VIM tabs are pinned
  in full; the COMMANDS tab's frame (the tab bar, the header, the row
  count, the counter's format, the footer) is pinned exactly, and its
  rows are checked structurally. A row's own bytes are therefore only
  pinned by the one-time diff of two dumps.
- **存疑**: the **`/help` overlay's GENERAL tab carries two tokens that
  change on their own** — the reference's version string and the log
  file's timestamp. The pinned bytes mask both, so a change in the
  version or in the clock does not fail the test; a change in the
  overlay's shape does.

- **存疑**: the **reference's `--output-format=steps` engine is gone
  from the clone**. `internal/cli/steps.go` was orphaned — nothing
  imported it — so it went with the sweep; the evidence stays in
  `out/local-macos/gosrc/cli/steps/`. The clone's `--print` does not
  implement the steps format, and it never did.
- **存疑**: **the clone implements none of the reference strings it
  keeps.** The v1.4.0 resume hint (`convstore.ResumeHintBanner`), the
  no-tool-call nudge (`permissions.NoToolCallNudge`) and the
  default-mode shift+tab hint are records of what the reference says,
  not behaviour the clone has. They are kept because deleting them
  would delete the record.
- **存疑**: **`files.ErrTooLarge` has no `-32603` mapping.** The error
  is documented and the error value exists; the mapping the guide
  describes was never implemented.
- **存疑**: **`cascades.StatePhaseWorking` is never emitted.** The
  phase set is closed in the reference and the clone emits the rest of
  it, so a dashboard that branches on the phase sees an incomplete
  set.
- **存疑**: **the doc-fidelity check is a heuristic.** It compares the
  retired shell's doc against the target's doc word by word, so a
  target that carries the same fact in different words is flagged as
  if it were missing. The check is a prompt for a decision, not a
  verdict: four of the five names it left standing at the end of the
  tranche were mechanical or equivalent, and one was a false claim
  that had to be read before it could be dropped.
- **存疑**: **the eight hand-handled forwarders were a judgement.** A
  forwarder whose body binds a host-local value — the palette's
  colour, the host's clock, the catalogue resolver — has no mechanical
  rewrite. Each was given one clean retirement (a method on the host's
  own type, an inline at the call site, or a package call), but the
  rule does not say which of the three to pick, and a different reader
  could have picked differently.
- **存疑**: the **`patches` package keeps three knobs with no
  producer** (`Disabled`, `Debug`, `WebSocketTransportMissing`). They
  gate real patches — the websocket-transport default, the mobile
  geometry tracer — but no caller sets them, so the tracer never
  applies and the transport default is always on. They are the
  package's operator-facing configuration surface, so they stay; the
  `cache-bust` patch did not, because its knob's only producer was the
  deleted `CacheKey()` and without it the patch was the identity
  rewrite of its own anchor.

- **存疑**: **the registry's tie order is nondeterministic.** The rows
  are read from a map and sorted with an unstable sort, so two records
  that started in the same second come out in an arbitrary order — the
  pre-tranche tree produces a different order on two consecutive runs
  of the same fixture. The move did not change this, and the
  reference's own order is unknown: the clone sorts by start time, and
  nothing in the evidence says whether the reference does the same or
  keeps the registration order.
- **存疑**: **the gate is read per frame.** The panel's `Hidden()`
  reads `settings.json` on every draw, where the host's old
  `isHideActiveItems()` also read it per call — the cost is the same,
  but a per-frame settings read is a per-frame file read, and the
  settings store's own cache is what makes it cheap. It was not
  measured.
- **存疑**: **the block's bytes are pinned by a one-time diff, not by
  a golden.** The panel's own tests assert the row's cells, the
  overflow tails and the key trajectory as *fragments*; the exact
  bytes are pinned by the two-tree dump, which is not in the tree
  (limitations above). A golden would need a stable clock, and the
  rows carry one.

- **存疑**: **the dead-code guard's production-only rule is a
  judgement about what "live" means.** A function only the tests call
  is not run by the product, so it is dead — but a test-only function
  can be a *seam* the tests are exercising on purpose, and deleting it
  removes the coverage. The rule answers this with the allowlist: a
  declaration that is deliberately kept carries a reason, and the
  reason is in one map. The judgement the map records is a human one.
- **存疑**: **the guard's transitive liveness has a boundary the tree
  does not have.** A declaration whose every *in-package* caller is
  dead is dead. A declaration called only from another package is
  alive — the host-local guard says nothing about it, and the
  tree-wide sweep is where that half is resolved. The two halves meet
  at the package boundary and neither crosses it.
- **存疑**: **the voice surface's phase model is the clone's own.**
  The four phases (idle, connecting, recording, finishing) and the
  overlay's four texts are the reference's — 【一手】 agy 1.4.3's
  capture — but the *state machine* that produces them is a reading of
  that capture, and the reference may hold the phase somewhere the
  clone does not model (a session object, a transport field).
- **存疑**: **the selection *store* stayed in the host, and that is a
  split, not a move.** `m.store.InputSelStart/End` is the composer's
  own state; the arithmetic over a range is pure and moved. The split
  is defensible, but a future cycle could argue the whole selection
  belongs to the tier-5 package with a seam for the textarea — the
  textarea's own API is the obstacle, and it was not measured.
- **存疑**: **the flag *registration* stayed with the parser.** The
  policy moved to tier 1 and the table stayed in `internal/cli`, so
  the flag's two halves live in two tiers. A tier-1 package that
  carried the whole flag — name, default, help and policy — would be
  one owner, but the type (`cli.FlagSpec`) and the golden
  (`internal/cli`'s help golden) belong to the parser, and moving them
  was outside this tranche's scope.
- **存疑**: **the registry's route table is a table of the host's
  *current* panels.** A new panel that claims a key early has to be
  added to `internal/ui/panelregistry/route.go` as well as to the
  registry's panel list — two places, in two packages. The dispatcher
  rule says a domain keeps its own routing table, and the registry is
  the domain, so this is the shape the rule asks for; whether the
  table can be derived from the panel list instead is open.

- **limitations**: **the tranche-45 parity dumps are not in the tree.**
  The proof is a diff of two scratch dumps (10 057 bytes at the base,
  9 849 bytes at the final trunk, both `cmp`-clean), and the harness
  that produced them was deleted. The permanent pin is the guard
  itself, which asserts the *shape* of the host's dead surface and not
  the rendered bytes; a rendered-byte pin for the fourteen fixtures
  would need a golden file the tranche did not leave behind.
- **limitations**: **the four names the sweep unexported are renamed,
  not re-exported.** `NewRunMode`, `EnableTools`, `DefaultRouter` and
  `BtwScrollBy` are gone from the host's exported surface. A third
  party that imported `internal/cliapp` and called them would break —
  the package is `internal/`, so no third party may, and the
  compatibility promise covers the *public* top-level packages only.

- **存疑**: **the data package depends on the view package for one
  two-field struct.** `modelargpopup.Rows()` returns
  `[]slashpicker.ModelRow`, so the *data* side of the `/model` completion
  imports the *view* side for its row type. Inverting the edge means
  `slashpicker.SetModelCatalogueSource` changes its signature and three
  test files follow; it was not done in this tranche. The coupling is
  deliberate, not accidental — but it is a coupling.
- **存疑**: **`internal/ui/rows` is no longer stdlib-ish.** It imports
  `internal/features/tools/clipboard` and `internal/app/tools/shell`. The
  ui → app edge is legal and precedented, but the alternative — leaving
  the notification's title/body composition in the host — would either
  leave a host helper behind (the dead-code guard's teeth) or leak the
  composition to the call site. A deliberate trade-off.
- **存疑**: **`ReplaceRange` mutates the input slice's backing array**,
  the same as the host method it replaced, so a caller must pass a fresh
  slice. A `Document` that owned the buffer would not have this
  hazard, and it cannot own the buffer for the reason above.
- **存疑**: **the parity proof for the vim translation needed a
  trick.** The base tree has no importable copy of the old translation
  — it was unexported host code — so the matrix digest had to be
  produced on each side by a throwaway test inside that tree, using its
  own live functions. The two digests being equal is what makes the
  sides comparable; it is not the same as comparing one implementation
  to the other directly.

- **存疑**: **the 2-minute quota freshness rule still has three
  copies.** `accounts.QuotaFreshness` is now the owner, but
  `internal/rcfrontend/accountsapi.go:79` and
  `internal/remotecontrol/dispatch_quota.go:64` each carry their own
  constant with the same value. Both packages already import the
  accounts package, so each is a one-line change — they were outside
  the tranche's scope.
- **存疑**: **a stale doc reference survives the shell's retirement.**
  `internal/rcfrontend/accountsignin.go:38-39` still points at
  "internal/cliapp/slash_panel_accounts.go: registerAccount after the
  sign-in" — a file and a function that are both gone.
- **存疑**: **the registry's write fails for an account the registry
  does not hold.** `SetQuota` errors on an unknown account, so a
  session whose credential is not in the shared locations caches
  nothing and re-queries on every `/usage` open. In a TUI session the
  active account is registered at start-up, so this is an edge.


- **存疑**: **the bare `/rewind` handler is unreachable, and its fix
  lives on that unreachable path.** The `/rewind` picker claims the
  command first, so the tail-pop branch never runs. The session now
  really truncates there, which is the intended fix — but it is a
  behaviour change on a path no user can reach, and the alternative
  (a `TailCount` that keeps the copy bug) was not chosen.
- **存疑**: **three exported symbols in the rewind panel have no
  caller outside their own package** — `StepHeight`, `RenderStepRows`
  and `StepCounter`. They predate this tranche and were left alone to
  keep the diff inside the rewind; the tree-wide dead-export sweep is
  their home.
- **存疑**: **`StepEcho` has an unreachable branch.** The "current, no
  messages removed" form cannot happen today (the newest step always
  owns the whole tail). It is kept verbatim so the reference's wording
  does not drift, and `StepEcho` is the only place that knows both
  echo texts.
- **存疑**: **two copies of the log-path shape remain**, outside the
  tranche: `internal/features/auth/compat.go:269` and
  `internal/ui/tuilib/help_panel.go:84`. The second one prints the
  *literal* `~/.gemini/…` form rather than an absolute path, so
  naming the new owner there is a visible-string decision, not a
  mechanical one.
- **存疑**: **a stale doc reference survives the status line's move.**
  `internal/core/logts/logts.go` still says *"`internal/cliapp`
  renders the status line's error card"*; it is
  `internal/ui/views/statusline` now.
- **存疑**: **the status area computes the log path on every frame.**
  The old code computed it only when an error card was drawn; the
  `AreaState` value is built per frame, so it now runs one
  `time.Now()` and one `filepath.Join` per frame. The rendered bytes
  are unchanged; the cost is not.


- **存疑**: **a feature package now reads a CLI flag name.**
  `internal/features/agents/flags.go` parses `--agent` out of a
  `flag.FlagSet`. That is CLI vocabulary in a feature package; it was
  judged acceptable because the reference's `--agent` flag is part of
  the catalogue's contract and `internal/features/auth` already
  imports `flag`, but a stricter reading would put
  `CustomAgentFromFlags` in `internal/core/cliflags/agents` and leave
  only the catalogue in tier 2.
- **存疑**: **a stale comment names a package that never owned the
  reading.** `internal/pigrt/skills.go:108` says *"readPluginEnabled
  mirrors internal/agent's config.json reading"* — the reading has
  always lived in `internal/features/plugins/core` (`Enabled` /
  `LoadedDirs`).
- **存疑**: **the catalogue's parity harness is throwaway.** The
  8 772-byte dump that proved the move lives outside the repo; the
  tree keeps only the moved table-driven cases and a new test for the
  two moved flag helpers. A permanent golden dump of the catalogue is
  not in the tree.


- **存疑**: **three callers still read the working directory
  themselves.** `defaultWorkspaceForRunMode` and
  `panelAgentsWorkspace` still call `os.Getwd`, as does
  `internal/features/auth/compat.go`. They can name
  `workspace.Current()` now; the three were outside the tranche.
- **存疑**: **a stale doc reference survives the seed's move.**
  `internal/app/session/rehydration/rehydration.go:112` still names
  `cliapp.loadedStepErr`; it should name `rehydration.Succeeded`.
- **存疑**: **the trajectory's parity harness is throwaway.** The
  485-byte proof lives outside the repo; the tree keeps only the
  store's own table-driven cases.

- **存疑**: **the slash grammar's two rules are inherited clone
  behaviour, not reference evidence.** Neither carries a 【一手】
  provenance: the reference's own handling of a vertical tab, a form
  feed or a carriage return inside a slash command is not recovered.
  The package holds both rules side by side so a capture can retire
  one of them.
- **存疑**: **a third trim rule lives in the tree.**
  `internal/remotecontrol/terminals/registry_helpers.go`'s
  `trimSpaceASCII` (space, tab, CR, LF) normalises terminal ids. It is
  neither of the slash grammar's rules and is not a copy of either — but
  it is the same *shape* of helper, and it deserves a look in a later
  tranche.
- **存疑**: **`Options.ViewportHeight func() int` is the first
  *behavioural* callback in a ui-tier `Options`.** The base's repaint
  asked the host for the viewport height only when the transcript had
  more than 30 blocks; a plain `int` would force the host to measure
  the viewport on every repaint and change the perf profile. The
  callback keeps the measurement lazy, but it is a host fact inside a
  tier-5 struct — the render facts are otherwise all values.
- **存疑**: **`FinishLiveThinking` requires `Thought.Active == true`.**
  The contract reads oddly — the thought must still be *active* in the
  options to be finished — but it preserves the base's semantics
  exactly: the base captured the thought *before* clearing `m.thinking`
  and painted the finished row from the thought that was running. A
  dedicated `FinishThought(thought LiveThought, opts Options)` would
  read better; it was not worth a second options struct.
- **存疑**: **`Document.UserBlocks` was added to the document.** The
  base's engine read the host's `m.userBlocks` directly; the mechanic
  cannot see the host, so the index had to travel with the document.
  The document is now the only place a repaint can shift the user
  prompts — the host still owns the slice.
- **存疑**: **the host's seven repaint wrappers are not shells, but
  they look like them.** Each owns a cache invalidation the tier has no
  business knowing; a reader without the invariant in front of them
  will read them as forwards.
- **存疑**: **the live-batch golden does not pin the thought's elapsed
  time.** The `Thought for <dur>` cell is masked by a regexp, because the
  duration is clock-dependent. The clock-independent cells of the batch
  path — the block's row count, the batch grouping, the batch break —
  are pinned byte for byte.
- **存疑**: **the live-batch fixture's second `runMode` is built by
  hand**, not through the real session setup. It proves the *document*'s
  behaviour and not the wiring of a live turn's batch counter: the host's
  `m.liveBatch` / `m.batchBreak` transitions are exercised only as the
  fixture drives them.
- **存疑**: **the slash-command vocabulary's alias rule reaches the
  handler now**, so two advertised aliases the host's own table never
  carried (`/switch`, `/conversation`) answer with `/resume` instead of
  `Unknown command: /switch`. That is a **behaviour change** the
  catalogue's own rows imply, and it was not verified against the
  reference binary — the catalogue is the clone's own table, and its
  alias spellings came from the `/help` COMMANDS tab.


## limitations

- **limitations**: the measurement scripts are **not in the tree**,
  as in cycles 378, 379 and 380.
- **limitations**: **the host is still 15 476 production lines over
  93 files.** The split moved the panels; it did not split the host.
  (The 37th tranche — the slash-command popup — took it to 17 427 lines
  over 93 files; the 38th — the mention dropdown — to 17 429 lines over
  94 files; the 39th — the active-items block — to **17 193 lines over
  94 files**; the 40th — the status row — to **17 099 lines over 95
  files**: 183 lines of the row left the host and 88 lines of seams
  came back. The second wave — the vim keys (58), the model argument
  (59), the composer's editing (61), the transcript's mechanics (62),
  the attachment rows (64) and the quota snapshot (65) — took it from
  **16 047 lines over 93 files** to **15 676 over 93 files**; the
  concurrent planning-mode feature added 111 of those lines back, so
  the wave's own share is roughly 400. The third wave — the rewind
  policy (75), the agent catalogue (76) and the status line's rows (77)
  — took it from **15 676** to **15 537**; the trajectory (69) took it
  to **15 445**, and a concurrent feature commit (`/btw` made
  non-blocking) took it back to **15 513**; the second owners (78) and
  the slash grammar (81) took it to **15 476**; the transcript's
  repaint engine (87) took it to **15 196** over **93 files** — 603
  lines left the host and 51 came back as the wrappers' cache
  ownership; the live-step batch mechanic (91) took it to **15 063**,
  and the slash-command vocabulary (88) took it to **15 050** over
  **94 files**.)
- **limitations**: **the dead-code guard covers the host's *unexported*
  surface only.** An exported function with no caller in its own
  package may be called from another one, so the guard says nothing
  about the exported half. The tree-wide sweep (section 4) took that
  half on, and **117 dead exports remain tree-wide** — of which the
  production `internal/` tree holds **13**, and nine of those are the
  recorded keeps above. The rest are the public packages, the
  evidence packages, the generated protobuf code and `out/`.
- **limitations**: **the sweep's instrument is not in the tree**, as in
  cycles 378, 379 and 380. A dead export outside the `internal/` tree
  is therefore not guarded; the public packages are a compatibility
  promise and are not swept at all.
- **limitations**: the host's **clusters of dead surface are still
  there** — the turn loop (`tui_run.go`, `tui_update_*.go`,
  `tui_stream_prompt.go`, `tui_turn_blocks.go`), the transcript
  (`tui_turn_blocks.go`) and the slash dispatch
  (`slash_commands*.go`). The exported sweep cannot see them: they
  are unexported, and the guard that does see them is the
  host-local one. The transcript's *vocabulary* left the host in the
  41st tranche and its *repaint engine* in the 87th; what is left of
  the transcript in the host is the decision — when to repaint, what
  the cache keys are and which rows the window holds.
- **limitations**: **the transcript's repaint engine was not a panel
  and not a foundation** when the 87th tranche moved it. It has no
  keys, no cursor and no rows of its own — it rewrites rows that
  already live in the host's buffer — and it is not a function of its
  parameters, because it mutates the buffer and the index it is
  handed. Its natural home was `internal/ui/transcript`, and the
  host's `m.response`, `m.blocks`, `m.userBlocks`, `m.assistants` and
  `m.liveStepRuns` stay in the host: they are appended to from about
  fifty sites, so moving the *fields* is a separate and much larger
  tranche than moving the *arithmetic*.
- **limitations**: **the host's `m.expanded` and
  `m.effectiveExpanded(verb)` disagree, and the asymmetry is
  load-bearing.** The live-step streaming path paints with the raw
  `m.expanded`; the block repaint path paints with
  `m.effectiveExpanded(verb)`. They disagree when the verbosity is
  high and the user has not pressed ctrl+o: the live rows are painted
  collapsed and the final block is painted expanded. Do not unify them.
- **limitations**: **the composer's selection arithmetic is half
  moved.** The pure half is in `internal/ui/foundations/inputselection`;
  the host keeps `inputOffsetAt` and `applyInputSelectionHighlight` as
  thin methods that supply the value, the line count and the prompt's
  screen row. They are not shells — they read host state — but they
  are the host's half of the split and may be inlined later.
- **limitations**: **the status row's `State` has twenty-three fields.**
  The row is the surface with the most facts in the TUI — five hint rules
  and four overrides on the left, six facts on the right — and the `State`
  is the price of that. A `Seams` of callbacks would have been the same
  size and worse: a view that only draws should not hold callbacks it
  never calls.
- **limitations**: **the row's static (non-tty) form is a second
  implementation of the row.** `statusrow.Static` paints the hint, the
  model and a literal `" · high"` in a literal grey, while
  `statusrow.Render` paints the full row from a `State`. The two do not
  share a code path, because the non-tty form has no session to read the
  mode, the effort tier or the tasks from. The reference has the same
  two forms.
- **limitations**: **the `/diff` panel's fold is still reachable from
  the host**, for the reason in 存疑 above. The panel itself moved in
  the 25th tranche.
- **limitations**: the historical cycle reports under `docs/cycles/`
  were **not** rewritten. They are evidence: a report that says
  `internal/cliapp/slash_panel_context.go` was true when it was
  written. Only the current-state documents (`docs/guide/*`,
  `AGENTS.md`) were repointed.
- **limitations**: **the popup's own bytes are pinned by three rows
  and the hint row**, not by the whole catalogue: the command rows grow
  with every new slash command, so a full byte pin would fail on a
  feature that has nothing to do with the popup. The panel's bytes are
  pinned by the "No matches" branch, one command row, the model rows
  and the hint row, and the whole popup is pinned by the one-time diff
  of two dumps.
- **limitations**: **the mention dropdown's golden pins the rows that
  carry no path.** The listing's path column holds the workspace's
  absolute path, so the golden pins the query mode and the placeholder
  listing (which carry no path) and the listing's first row at a width
  narrow enough for the path cell to hold the reference's "/..."
  truncation. A golden that depends on the length of the temporary
  directory is a golden that fails on another machine.
- **limitations**: the cycle's commits are **interleaved with a
  concurrent agent's** work in the same tree (the drag-and-drop
  attachments, the actor model, the term guard). Those commits are not
  part of this cycle and are not counted in the tables above.
