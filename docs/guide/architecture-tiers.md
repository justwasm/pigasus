# The five-tier layout of `internal/`

> **A record of what the tree looks like now**, not a plan. The plan this
> implements was executed on 2026-10-30 in nine commits (`c00afe34` →
> `9ff6d833`); the old forward-looking plan document was replaced by this
> file in cycle 371, when the tree was audited against the documents.
> Cycle 378 finished the placement it started: **every package under
> `internal/` now sits in a tier, or is a host, or is evidence.**
>
> The rule that decided everything here: **a package under `internal/` that
> holds no behaviour of its own is a shell.** A shell's whole surface is
> aliases (`type X = other.X`) and forwarding calls into one other package.
> The implementation lives at the tier path the shell names; the old path is
> a re-export. Never edit a shell expecting behaviour to change — edit the
> package it points at.
>
> Cycle 376 retired the 58 shells that were named `facade.go`. Cycle 377
> retired **eleven more that hid behind other filenames** — `branding.go`,
> `autocont.go`, `prompt.go`, `stream.go` and the `internal/modular/*`
> registration shims. The rule is now stated by **shape**, not by filename,
> because a filename is not a property a shell can hide behind.

---

## The five tiers

The tiers are numbered by **dependency order**: a tier may import the tiers
below it and nothing above it.

| Tier | Root | What lives there |
|---|---|---|
| 1 | `internal/core/` | the contracts: the registry (`registry`), the lifecycle state machine (`lifecycle`), the policies (`policy/permissions`, `policy/sandbox`, `policy/trust`), the settings store (`settings`), the seam (`seam`), the token formatter (`tokenfmt`), the log-file timestamp (`logts`), the session-flag policies (`cliflags/*`), the version (`version`), the layering guard (`architecture`) |
| 2 | `internal/features/` | the capabilities: the auth (`auth/*`), the model catalogue (`models/*`), the accounts (`accounts/*`), the plugins (`plugins/*`), the OAuth flows (`oauthflow`), the feature-local tools (`tools/*`) |
| 3 | `internal/infra/` | the infrastructure: the transports (`backend/*`), the daemon (`daemon`), the proxy (`agyproxy`, `proxy/*`), the reference's own asset backend (`refbackend`), the remote-control plumbing (`remote/*`), the subsystem registrations (`subsystems/*`), the drift checker (`referencedrift`) |
| 4 | `internal/app/` | the orchestration: the turn seam (`agentturn`), the agent server (`agentserver`), the prompt subsystem (`prompt/*`), the session stores (`session/*`), the TUI state store (`tuistore`), the tool surface (`tools/*`), the subagent machinery (`subagent/*`), the subcommand table (`subcommands`) |
| 5 | `internal/ui/` | the presentation: the TUI contract and defaults (`tuimodel`), the panel registry (`panelregistry`), the TUI widgets (`tuilib`, `tuitools`), the shared rows (`rows`), the editor (`editor/*`), the foundations (`foundations/*`), the renderers (`render/*`, `mdrender`), the views (`views/*`) |

The tier root holds only directories — a package's *own* directory is the
unit of placement, and the tier is chosen by what the package depends on,
not by where it was born.

### The layering rule

Three facts hold in the tree as it stands, and each one is checkable:

1. **Zero upward edges.** No package in tier *N* imports a package in a
   tier *M > N*. The measurement walks the production import graph and
   counts the edges that point up; there are none.
2. **Zero import cycles.** The production graph under `internal/` has
   **227 packages** and 227 strongly connected components — every
   package is its own component. The test-only graph has 207 packages
   and none of them cycles either. (The numbers move as the tree
   changes; `go test ./internal/core/architecture/` is the authority,
   and `devutils/tiergraph.py` prints the same figures out of tree.)
3. **The lower four tiers import no host.** `internal/core`,
   `internal/features`, `internal/infra` and `internal/ui` never import
   `internal/agent`, `internal/pigrt`, `internal/piagentbridge`,
   `internal/modular` or `internal/cli`. The app tier is the only tier
   that reaches the turn engines, and the hosts sit above every tier.

The `ui` tier may depend on the `app` tier — a view renders what the app
produced — and the `app` tier never depends on the `ui` tier. That is the
one downward edge the layering allows, and it runs in the right direction.

## The shells are retired

69 top-level `internal/` packages used to be shells: a file that
re-exported the tier package with `type`/`var`/`const` aliases, plus a
test that asserted the aliases still resolved. They were the *migration
mechanism* for the tier move, not a destination.

**Cycle 376 retired the 58 named `facade.go`. Cycle 377 retired the
other eleven**, which the filename rule had missed:

| old path (gone) | tier path (live) |
|---|---|
| `internal/autocont` | `internal/app/session/autocont` |
| `internal/branding` | `internal/core/branding` |
| `internal/interaction` | `internal/core/seam` |
| `internal/workspacetrust` | `internal/core/policy/trust` |
| `internal/modular/engines/stream` | `internal/app/engine/stream` |
| `internal/modular/subsystems/daemon` | `internal/infra/subsystems/daemon` |
| `internal/modular/subsystems/localharness` | `internal/infra/subsystems/localharness` |
| `internal/modular/subsystems/remotecontrol` | `internal/infra/subsystems/remotecontrol` |
| `internal/modular/subsystems/terminal` | `internal/infra/subsystems/terminal` |
| `internal/modular/toolsets/native` | `internal/app/tools/native` |
| `internal/modular/toolsets/wasm` | `internal/app/tools/wasm` |

The shells are gone. Every importer names the tier package directly, and
the old paths do not resolve any more.

The retirement was import-only. Where the target package's own name
differed from the shell's, the old name survives as an explicit import
alias, so no call site changed:

```go
import interaction "github.com/justwasm/pigasus/internal/core/seam"
```

That keeps the codebase's vocabulary — the shell's name was the good one,
the directory's name is the generic one — and it removes every possible
collision between a renamed package qualifier and a local identifier.

The map, for the record:

| old path (gone) | tier path (live) |
|---|---|
| `internal/accountrouting` | `internal/features/accounts/routing` |
| `internal/accounts` | `internal/features/accounts/core` |
| `internal/agentmessage` | `internal/core/msg` |
| `internal/agentsdir` | `internal/app/prompt/agentsdir` |
| `internal/agybin` | `internal/infra/proxy/bin` |
| `internal/agystep` | `internal/app/prompt/step` |
| `internal/apierror` | `internal/core/apierror` |
| `internal/atconversation` | `internal/app/prompt/mentions/conversation` |
| `internal/atfiles` | `internal/app/prompt/mentions/files` |
| `internal/backendselect` | `internal/infra/backend/select` |
| `internal/bashmode` | `internal/features/tools/bashmode` |
| `internal/browsersubagent` | `internal/app/subagent/browser` |
| `internal/builtinskills` | `internal/features/tools/skills` |
| `internal/builtinsubagent` | `internal/app/subagent/builtin` |
| `internal/canvas` | `internal/ui/views/canvas` |
| `internal/cascade` | `internal/core/lifecycle` |
| `internal/cliflags` | `internal/core/cliflags` |
| `internal/clipboard` | `internal/features/tools/clipboard` |
| `internal/codesearch` | `internal/features/tools/codesearch` |
| `internal/cumulativeprompt` | `internal/app/prompt/cumulative` |
| `internal/customagent` | `internal/app/prompt/customagent` |
| `internal/customcmd` | `internal/app/prompt/customcmd` |
| `internal/customize` | `internal/app/prompt/customize` |
| `internal/daemon` | `internal/infra/daemon` |
| `internal/diffaction` | `internal/ui/views/diff/action` |
| `internal/difffold` | `internal/ui/views/diff/fold` |
| `internal/diffrow` | `internal/ui/views/diff/row` |
| `internal/eligibility` | `internal/features/auth/eligibility` |
| `internal/featdiff` | `internal/ui/views/diff/feat` |
| `internal/hooks` | `internal/features/plugins/hooks` |
| `internal/implicit` | `internal/app/prompt/implicit` |
| `internal/isolation` | `internal/features/tools/isolation` |
| `internal/keybind` | `internal/ui/editor/keybind` |
| `internal/modelcatalog` | `internal/features/models/catalog` |
| `internal/modelctx` | `internal/features/models/ctx` |
| `internal/permissions` | `internal/core/policy/permissions` |
| `internal/persistentrc` | `internal/infra/remote/persistent` |
| `internal/plugins` | `internal/features/plugins/core` |
| `internal/pluginscan` | `internal/features/plugins/scan` |
| `internal/pluginwatch` | `internal/features/plugins/watch` |
| `internal/rclink` | `internal/infra/remote/link` |
| `internal/rehydration` | `internal/app/session/rehydration` |
| `internal/resultparity` | `internal/core/resultparity` |
| `internal/resumetree` | `internal/app/session/tree` |
| `internal/sandbox` | `internal/core/policy/sandbox` |
| `internal/seasonal` | `internal/ui/views/seasonal` |
| `internal/statusline` | `internal/ui/views/statusline` |
| `internal/store` | `internal/core/store` |
| `internal/subagent` | `internal/app/subagent/core` |
| `internal/subagentquestions` | `internal/app/subagent/questions` |
| `internal/subagentrouter` | `internal/app/subagent/router` |
| `internal/taskregistry` | `internal/app/task` |
| `internal/termcolor` | `internal/ui/render/color` |
| `internal/textwrap` | `internal/ui/render/wrap` |
| `internal/toolrunner` | `internal/app/toolrunner` |
| `internal/tuiviewer` | `internal/ui/foundations/viewer` |
| `internal/tuiviewport` | `internal/ui/foundations/viewport` |
| `internal/wordmove` | `internal/ui/editor/wordmove` |

A doc that names one of the old paths as "where the code is" is wrong —
name the tier path.

## Cycle 378: the placement that finished the map

The maps above are the *shell* retirement. Cycle 378 placed the packages
that were still at the top level of `internal/` because they had never
been a shell — they were simply misplaced.

| old path (gone) | tier path (live) | tier |
|---|---|---|
| `internal/agyprompt` | `internal/app/prompt/agyprompt` | 4 |
| `internal/agytemplates` | `internal/app/prompt/agytemplates` | 4 |
| `internal/agenttools` | `internal/app/agenttools` | 4 |
| `internal/subagenttools` | `internal/app/subagenttools` | 4 |
| `internal/harnesstools` | `internal/app/harnesstools` | 4 |
| `internal/convstore` | `internal/app/session/convstore` | 4 |
| `internal/compaction` | `internal/core/compaction` | 1 |
| `internal/features/tools/native` | `internal/app/tools/native` | 4 |
| `internal/features/tools/wasm` | `internal/app/tools/wasm` | 4 |
| `internal/features/tools/toolrunner` | `internal/app/toolrunner` | 4 |
| `internal/configtest` | `internal/features/models/configtest` | 2 |
| `internal/agyproxy` | `internal/infra/agyproxy` | 3 |
| `internal/refbackend` | `internal/infra/refbackend` | 3 |
| `internal/memtrim` | `internal/infra/memtrim` | 3 |
| `internal/codeassistclient` | `internal/infra/codeassistclient` | 3 |
| `internal/referencedrift` | `internal/infra/referencedrift` | 3 |
| `internal/providerpanel` | `internal/ui/views/providerpanel` | 5 |
| `internal/tuilib` | `internal/ui/tuilib` | 5 |
| `internal/tuitools` | `internal/ui/tuitools` | 5 |
| `internal/mdrender` | `internal/ui/mdrender` | 5 |
| `internal/editorkit` | `internal/ui/editor/editorkit` | 5 |
| `internal/announce` | `internal/ui/views/announce` | 5 |
| `internal/changelog` | `internal/ui/views/changelog` | 5 |
| `internal/codesearchaction` | `internal/ui/views/codesearchaction` | 5 |
| `internal/configpanel` | `internal/ui/views/configpanel` | 5 |
| `internal/contextpanel` | `internal/ui/views/contextpanel` | 5 |
| `internal/loginui` | `internal/ui/views/loginui` | 5 |
| `internal/modelpicker` | `internal/ui/views/modelpicker` | 5 |
| `internal/narration` | `internal/ui/views/narration` | 5 |
| `internal/onboarding` | `internal/ui/views/onboarding` | 5 |
| `internal/pluginsui` | `internal/ui/views/pluginsui` | 5 |
| `internal/programstatus` | `internal/ui/views/programstatus` | 5 |
| `internal/rewindpanel` | `internal/ui/views/rewindpanel` | 5 |
| `internal/sidequestion` | `internal/ui/views/sidequestion` | 5 |
| `internal/skillsui` | `internal/ui/views/skillsui` | 5 |
| `internal/slashquote` | `internal/ui/views/slashquote` | 5 |
| `internal/themepreview` | `internal/ui/views/themepreview` | 5 |
| `internal/usagepanel` | `internal/ui/views/usagepanel` | 5 |
| `internal/voice` | `internal/ui/views/voice` | 5 |

Three facts moved with them, because a package that holds a fact the
lower tiers need has to live below the host that used to hold it:

| the fact | used to live in | now lives in |
|---|---|---|
| the reference version | `internal/cli` | `internal/core/version` |
| the model catalogue, its gates and the Claude-5.5 gate | `internal/cli` | `internal/features/models/catalog` |
| the marketplace link | `internal/cli` | `internal/features/plugins/marketplace` |
| `UsageLogo` | `internal/cli` | `internal/ui/tuilib` |
| `CountLabel` | `internal/features/tools/codesearch` | `internal/core/plural` |

`internal/cli` therefore holds no fact that a lower tier needs, and the
model catalogue no longer imports the CLI host — which would have been a
cycle.

The subcommand dispatch table could not follow: its spec type carries a
`*cli.JetskiCLI`, so it cannot live below the CLI host. It moved to the
app tier at `internal/app/subcommands`, and the ten registration shells
under `internal/modular/subcommands` were retired with it.

## Cycle 379: the first god package, split

`internal/app/tools` was the last package that was still a *directory of
files* rather than a set of cohesive packages: the contract, six
executors and the advertised catalogue all lived in one directory, and
every importer named it.

Cycle 379 split it into seven packages, all still in tier 4:

| package | owns |
|---|---|
| `internal/app/tools` | the contract — `Tool`, `Registry`, `ArgsOf`, the result stamp, the failure text, the reference fixtures |
| `internal/app/tools/shell` | `run_command` and its siblings, the background-task table, the schedule state, the notification |
| `internal/app/tools/filesystem` | the file tools, the searches, the notebook, the workspace walker, the image and WebM probes |
| `internal/app/tools/browser` | the browser tools, the browser subagent, the in-Chrome bridge |
| `internal/app/tools/web` | `search_web`, `read_url_content`, `generate_image`, the three MCP tools |
| `internal/app/tools/tasks` | `manage_task`, `command_status`, `send_command_input`, `schedule`, the interactive tools, the inbox |
| `internal/app/tools/surface` | the catalogue: 60 advertised names, 12 wire declarations, the harness declarations |

`internal/app/tools` **re-exports nothing**. The split *distributed* the
public API rather than keeping a second name for it — a facade would be
a shell, and cycle 377 retired the shells.

The dependency graph is acyclic. Every capability imports the contract
(`tools`), `tasks` also imports `shell` for the background-task table,
and `surface` imports every capability — the catalogue *is* the set of
executors. Zero upward tier edges, zero import cycles.

A test follows its subject: a test that **dispatches** a tool by name
exercises the catalogue and lives in `surface`; a test that **constructs**
a tool directly lives with the capability that owns it. A capability's
test may not import `surface` — `surface` imports every capability, and
the reverse edge would be a cycle.

See [`tools.md`](tools.md#the-package-layout-cycle-379) for the whole
layout.

## Cycle 381: the panel split

`internal/cliapp` held the whole TUI surface, and twenty-three of the
modal panels lived in it. A panel is not a host concern: it is a
self-contained interactive surface with its own state, its own rows, its
own keys and its own writes. Cycle 381 moved them into tier 5, beside
the other views.

**Thirty-eight packages are new**, and ten that already existed grew into
their panel's other half:

| package | owns |
|---|---|
| `internal/ui/toolsteps` | the transcript's step model, its four tea messages and the styled rendering |
| `internal/ui/slashcommands` | the slash-command vocabulary: the 109 advertised rows, the 6 hidden ones, the alias rule (`Canonical`), the command → panel answer (`PanelFor`), the composer's "is this a command" question and the picker's filter. The panel kind of a row is *derived* from `internal/ui/panelregistry` at init |
| `internal/ui/transcript` | the transcript's row vocabulary: the four record types, the render cache, the user's prompt row and the turn separator — the document mechanics: the range replacement, the blank line, the block deduplication, the live-step batching and the two finalisations — and the **repaint engine**: the arithmetic that rewrites the rows in place when the width, the theme, the verbosity, the expansion state or the window changes. The engine is a *mechanic*: the host owns the buffer, the indexes and every decision about when to repaint |
| `internal/ui/foundations/inputselection` | the composer's selection geometry: the line span, the column offset, the row highlight and the value highlight |
| `internal/ui/resumepicker` | the `/resume` picker's state, its rendering and its keys |
| `internal/ui/views/accountspanel` | the `/accounts` panel and its Pool tab |
| `internal/ui/views/activeitemspanel` | the active-items block under the prompt: the cursor, the scroll window, the rows it writes and the keys it handles |
| `internal/ui/views/adddirpanel` | the `/add-dir` input |
| `internal/ui/views/agentspanel` | the agent picker and the running-subagent section |
| `internal/ui/views/artifactpanel` | the `/artifact` frame |
| `internal/ui/views/codesearchpanel` | the `/codesearch` panel |
| `internal/ui/views/creditspanel` | the `/credits` panel's two actions |
| `internal/ui/views/diffpanel` | the `/diff` panel: the three tabs, the cursor, the inline diff subview and the per-hunk fold state |
| `internal/ui/views/effortpanel` | the effort slider's geometry, the tier names and the `/effort` panel |
| `internal/ui/views/feedbackpanel` | the two `/feedback` stages |
| `internal/ui/views/helppanel` | the `/help` overlay: the tab bar, the four tabs, the list cursor and the window |
| `internal/ui/views/hookspanel` | the three `/hooks` stages |
| `internal/ui/views/mcppanel` | the `/mcp` frame |
| `internal/ui/views/mentionpicker` | the `@` file-mention dropdown: the candidate list, the highlight, the mention token's span, the rows it writes and the keys it handles |
| `internal/ui/views/permissionspanel` | the three `/permissions` stages |
| `internal/ui/views/renamepanel` | the `/rename` input |
| `internal/ui/views/slashpicker` | the slash-command completion popup: the filtered rows, the highlight, the scroll window, the on-screen rectangle, the rows it writes and the keys it handles |
| `internal/ui/views/statusrow` | the status row under the prompt: the left hint, the right segment, the padding arithmetic and the compaction label |
| `internal/ui/views/subagentquestionpanel` | the subagent-question picker: the scope, the two cursors, the staged answer, the alert rows and the overlay's rows |
| `internal/ui/views/taskspanel` | the `/tasks` list and its two detail views |
| `internal/ui/views/viewerpanel` | the viewport, the pager keys and the scroll arithmetic |
| `internal/ui/rows` | the transcript's notice rows: the compact, indented and dim `⎿` shapes, the two queued rows, the attachment indicator and the task-notification rows |
| `internal/core/logts` | the log file's timestamp suffix (`FormatLogTS`) — a tier-1 fact that two tiers need |
| `internal/core/cliflags/projects` | the `--project` policy: the default, the explicit id, the explicit name, the new-project path and the projects cache |
| `internal/core/cliflags/browser` | the `--browser` policy: the CDP port, the headless flag, the user-data dir and the local-Chrome flag |
| `internal/core/cliflags/logfile` | the `--log-file` policy |
| `internal/ui/foundations/history` | the prompt history: the remembered prompts, the navigation's index and the draft it stashed on its way in |
| `internal/ui/editor/vimkeys` | the vim key translation: the bubbletea key → engine key mapping and the badge, which takes the host's "is vim on" fact as a parameter |
| `internal/ui/views/modelargpopup` | the `/model <argument>` completion: the live catalogue's rows, the argument's resolution and the invalid-model message |
| `internal/ui/editor/composer` | the composer's text editing: the selection keys, the backslash-newline fallback and the textarea's two dropdowns — the widget crosses as an interface and the host keeps ownership of it |
| `internal/app/session/rewind` | the rewind policy: which messages a picked step owns, how many are removed and the echo text — the session layer owns the truncation, the panel only names the step |
| `internal/features/agents` | the markdown-agent catalogue: the `.md` files' frontmatter, the four scopes, the plugin walk and the `--agent` flag's face of it |
| `internal/core/workspace` | the process's working directory — one owner, below every tier, because the conversation store and the hosts both need it |
| `internal/core/slashparse` | the slash grammar: the host's rule (`Split`, `SplitVerb`, `Trim`) and the print mode's rule (`SplitFields`, `TrimUnicode`) — two named rules in one package, so a future unification is a one-place change |
| `internal/core/policy/hooks` | the documented `hooks.json` runtime: the six events, the document shape and its loader, the decision vocabulary, the path resolution and the handler execution (JSON in, JSON out, a per-handler timeout) — the shell a handler runs through is a **seam**, because the core tier cannot reach the app tier's shell |

They grew: `pluginsui`, `providerpanel`, `configpanel`, `skillsui`,
`modelpicker`, `rewindpanel`, `contextpanel`, `usagepanel`,
`changelog` and `voice` — the voice package gained the dictation
state machine that decides which of its own views to draw.

The host also lost **twenty-nine functions** that nothing called any
more — the Go compiler cannot catch an unused function, so they had
accumulated silently — and a **thirty more** in the sweep that
followed, when the guard stopped counting a test file as a caller.
`internal/cliapp/deadcode_test.go` is the guard that keeps them gone:
it parses the package with `go/ast` and asserts that every unexported
declaration the host makes is reachable from the host's own
**production** code. A declaration that is deliberately kept although
the product does not reach it carries a `deadcode: staged` marker with
the reason next to it, or an entry in the allowlist map — and an
allowlist entry that no longer excuses anything is an error, because the
decision it recorded is over.

One package shrank: `internal/ui/foundations/viewer` used to be a
"generic" package that held three different panels' facts — the
changelog panel's excerpt, the `/usage` panel's quota countdown and
tick, and the diff panel's inline row. Every one of them had exactly
one panel's user, which is a package with no owner. Each now lives with
the panel it belongs to, and what is left in the package is the
machinery all of them share: the document spec, the window arithmetic
and the counter row.

The rule the split established, and the one the tier rule already
implied:

- **A panel owns its state, its rows and its keys; the host keeps
  facts.** The host supplies a `Seams` struct of callbacks, or a `State`
  of values, in `internal/cliapp/<name>_host.go`.
- **A list panel owns its cursor; a viewer panel does not own the
  offset.** The host's `m.panel.cursor` and `m.panel.window` are shared
  between panels that do not own one, so a viewer takes the offset as an
  argument and hands the clamped one back.
- **The ui tier imports no host.** None of the new packages names
  `internal/cliapp`, `internal/agent`, `internal/pigrt` or
  `internal/piagentbridge`.
- **A completion popup over a live composer is not modal.** The
  slash-command popup owns the navigation keys (Up, Down, Tab, Esc) and
  answers `handled=false` for every other key, so the letters the user
  types reach the composer. A modal overlay is the opposite: it claims
  every key while it is up.
- **A widget's facts stay in the tier that owns them; the widget moves
  to the tier that renders it.** The `@` mention grammar and the
  workspace listing stayed in `internal/app/prompt/mentions/files`
  (the app tier); the dropdown that renders them is
  `internal/ui/views/mentionpicker` (the ui tier). A panel may import
  the tier below it.
- **The gate belongs to the surface it switches off.** "Show Active
  Tasks" switches *the active-items block* off, so the block reads the
  setting — `internal/ui/views/activeitemspanel`'s `Hidden()` — and the
  host asks the block. A host that keeps the gate keeps a fact about a
  surface it does not own.
- **A panel's host field is a pointer and every method of the panel
  is safe on a nil receiver.** A dozen host tests build a `&runMode{}`
  by hand; a panel that panicked on a nil receiver would take all of
  them down.
- **A view is a view: it takes the facts and writes the line.** The
  status row has no keys and no writes of its own, so it takes a `State`
  of values rather than a `Seams` of callbacks. The host keeps the facts
  it alone knows — the model, the mode, the effort tier, the running
  tasks, the compaction state, the composer and the pickers — and the
  row decides what they look like together.
- **A number has one owner.** The row's canonical width is the host's
  `separatorWidth`, and the host passes it in as `State.CanonicalWidth`;
  the row does not keep a second copy of the constant.
- **An entry surface is above tier 5, so a fact the ui tier needs cannot
  live there.** `internal/printmode` is an entry surface and the ui tier
  may not import it. The token formatter ("184 K" / "1.2 M") therefore
  lives in `internal/core/tokenfmt` (tier 1), below everyone, and the old
  `printmode.TokenK` is gone rather than left behind as an alias.

The subagent-question picker is the one panel that is **not** rebuilt on
every open and the one panel that **claims every key** while it is open.
Both exceptions have the same reason: its staged answer outlives the
overlay. The answer is typed into the composer's prompt and submitted
there, so the picker cannot be torn down and rebuilt when it closes; and
an overlay that is up swallows the keys under it, which is what the
reference's own picker did. The host's key pipeline asks the panel
(`HandleKey`) and the panel answers whether it took the key.

A panel package is an ordinary tier-5 package: it may import another
tier-5 package (the `/model` picker borrows the `/effort` slider's
geometry), and it may import the tiers below it, but never a host.

See the [cycle report](../cycles/cycle-381-cliapp-split/deliverable.md).

## The packages that did **not** move

After cycle 378, a package at the top level of `internal/` is one of
three things: a **host**, an **entry surface**, or **evidence**. There is
no fourth kind.

| package | why it stayed |
|---|---|
| `internal/agent`, `internal/pigrt`, `internal/piagentbridge` | the two turn engines and the PiG translation layer — the load-bearing core of the clone |
| `internal/cli`, `internal/cliapp`, `internal/printmode` | the three entry surfaces; only their *helpers* moved into the tiers |
| `internal/app/agentserver`, `internal/app/agentturn` | the agent server and the turn seam — the host and the seam it hangs off (cycle 374) |
| `internal/modular` | the registration surface: the contract is `internal/core/registry`, the concrete registrations are here |
| `internal/lsserver` | the LanguageServer implementation |
| `internal/agyupdate` | the updater |
| `internal/remotecontrol`, `internal/rchost`, `internal/rcfrontend`, `internal/rctunnel`, `internal/rcaudio` | the Web UI's own surface — see [`remote-control.md`](remote-control.md) |
| `internal/cortex_pb`, `internal/language_server_pb`, `internal/codeium_common_pb`, `internal/extension_server_pb` | the recovered proto skeletons — **evidence**, not program |
| `internal/cortex`, `internal/language_server`, `internal/planrender` | the 1.3.3-era reverse-engineering skeletons — **evidence**, not program |

Two planned targets became **public packages** instead of tier directories:
the DNS fallback is `dns/` (not `internal/infra/dns`) and the CEL engine is
`celpolicy/` (not `internal/core/policy/cel`) — both were extracted as
importable surfaces in the sixth public tranche (commits `e8bef8ac` and
`cfd0e1eb`).

### The entry surfaces' own sub-packages

Cycle 380 split `internal/remotecontrol` — an entry surface — into a
dispatcher and **fourteen domain packages** beside it. Those packages are
**not tier packages**: they are the entry surface's own internals, so the
tier ordering does not apply to them. They are placed by the same rule
that places everything else in this document, only *within* the entry
surface:

| package | owns |
|---|---|
| `internal/remotecontrol` | the dispatcher — the fan-out, the language-server table, the trajectory, the interaction host, the PiG-backed session |
| `internal/remotecontrol/rpc` | the JSON-RPC 2.0 envelope |
| `internal/remotecontrol/cascades` | the cascade domain |
| `internal/remotecontrol/permissions` | the permission preset, the review mode, the grants |
| `internal/remotecontrol/terminals` | the dashboard's terminal registry |
| `internal/remotecontrol/triggers` | the trigger specs and the store |
| `internal/remotecontrol/resources` | the resource providers and the project's resource list |
| `internal/remotecontrol/projects` | the project list and the four CRUD methods |
| `internal/remotecontrol/convconfig` | the conversation's configuration |
| `internal/remotecontrol/fastpush` | the fastpush toggle |
| `internal/remotecontrol/origins` | the browser-origin patterns |
| `internal/remotecontrol/plugins` | the dashboard's plugin domain |
| `internal/remotecontrol/files` | the file and directory reader |
| `internal/remotecontrol/mendel` | the Mendel flag answer and the per-identity cache |
| `internal/remotecontrol/effort` | the reasoning-effort vocabulary |

The rule that made them legal: **the dispatcher keeps the fan-out, the
domain keeps the table**, and every domain is **stdlib-only** — it
imports no tier package at all, so it cannot be the wrong side of a tier
edge. See [`remote-control.md`](remote-control.md#the-package-layout) and
the [cycle report](../cycles/cycle-380-remotecontrol-split/deliverable.md).

## `internal/modular` is the registry, not a tier

`internal/modular` is the one package that stays where it is on purpose: it
is the *registration* surface. `internal/core/registry` holds the contract;
`internal/modular` holds the concrete registrations. The `subcommands/*`
directories are gone (cycle 378) — they were blank-import shims over the
subcommand table, and the table's own `init()` is what registers.

The `engines/*`, `subsystems/*` and `toolsets/*` shims that used to sit
here are **gone** (cycle 377). They were re-exports of
`internal/app/engine/stream`, `internal/infra/subsystems/*` and
`internal/app/tools/{native,wasm}` — shells that added a hop without
adding a registration, because the tier package's own `init()` is what
registers. `cmd/pigga` already imported the tier packages directly:

```go
_ "github.com/justwasm/pigasus/internal/app/engine/stream"
_ "github.com/justwasm/pigasus/internal/app/subcommands/accounts"
```

The only importers the shims still had were three **tests that went
through the shell instead of the package they were testing** —
`internal/app/engine/stream/stream_test.go`,
`internal/app/tools/native/native_test.go` and
`internal/app/tools/wasm/wasm_test.go` all imported the shim rather
than the implementation. A test that imports a shell is a test of the
shell.

## The rules for new code

1. **Put the new package in its tier**, by dependency: a package that
   depends on nothing above it is tier 1, one that orchestrates is tier 2,
   and so on.
2. **Do not write a shell for a new package.** A shell only makes sense for
   a package that *already* had callers at the old path — and cycle 377
   retired the last of those, so the tree no longer has a shell at all.
3. **When you move a package, move the callers in the same commit** — the
   shell is for the *migration window*, not a permanent second name. A
   move that renames the package's qualifier does it by **import alias**,
   not by rewriting the call sites: the alias keeps the diff inside the
   import block and leaves no room for a qualifier to collide with a
   local identifier.
4. **Cite the tier path in the documents.** The old path is a name, not a
   location.
5. **A host never imports another host.** Three hosts run a session — the
   interactive TUI (`internal/cliapp`), the headless print path
   (`internal/printmode`) and the agent server (`internal/app/agentserver`)
   — and none of them may import another: the server cannot import
   `internal/cliapp` (an import cycle) and it must not import
   `internal/printmode` either (an entry surface is *above* a tier
   package). What the hosts share lives **below** them, in
   `internal/app/session/setup`: the permission policy, the terminal
   sandbox, the discovered subagents and the MCP wiring. Cycle 375 moved
   those four cores down from `internal/printmode` for exactly this
   reason.
6. **A fact belongs to the tier that owns it, not to the host that
   happened to hold it.** When a lower tier needs a fact that lives in
   `internal/cli`, the fact moves down — the host keeps a thin
   re-export only if a caller outside the tree needs the old name. The
   model catalogue is the worked example: it is a *model* fact, so it
   lives at `internal/features/models/catalog`, and it cannot import the
   CLI host without closing a cycle.
7. **A view never imports a host.** The `ui` tier reaches the `app` tier
   for what the app produced, and nothing above that.
8. **The layering rule is checked by `go test`, not by a script.**
   `internal/core/architecture` (tier 1) parses the tree with `go/parser`
   and asserts six rules: no upward edge (R1), no tier imports a host
   (R2), no ui-tier package imports a host (R3), no import cycle in the
   production graph (R4), none in the test-only graph (R5), and no
   public package imports an `internal/` package (R6). It agrees with
   `devutils/tiergraph.py` on every number it prints, and it is the only
   one of the two that can see a tier-to-host edge — the script's own
   host check tested the *imported* package's tier, and a host is
   untiered, so the branch never fired.

   Two findings are excused by name and printed on every run: the
   test-only cycle between `remotetransport` and
   `internal/remotecontrol`, and `remotetransport`'s test-only import
   of `internal/remotecontrol`. R2 exempts the app tier for the five
   engine hosts, and prints the exempted edge count (22) so the
   exemption stays visible.
