# The permission engine

> Where the clone decides whether a tool call may run. The engine lives in
> `internal/core/policy/permissions` (tier 1, stdlib only) and is reached
> from the interactive TUI, from print mode and from the agent server.

## The rule grammar

A permission rule is **`action(target)`**. The action is one of six
names, and the target is the argument the rule is about.

| action | what it governs |
|---|---|
| `command` | a shell command |
| `read_file` | reading a file |
| `write_file` | writing a file |
| `read_url` | fetching a URL |
| `execute_url` | actuating on a web page |
| `mcp` | calling an MCP tool |

The **seventh** verb, `unsandboxed`, is the reference's **retired** one.
The reference still names it in its own format regex and rejects every
rule that uses it, with this sentence:

```
unsandboxed is no longer a valid rule, use command() instead
```

That is the reference's own text (【一手】 agy 1.4.11's string table), and
the clone quotes it verbatim — `permissions.UnsandboxedRejectedMessage`.
A rule in the clone's **old** `tool:pattern` shape no longer matches; the
permissions panel writes the new shape.

## How a target is matched

The grammar tells you **what** a rule is about; the matching rules tell
you **when** it fires. Each action matches its target differently —
there is no shared prefix rule.

| action | how the target is matched |
|---|---|
| `command` | **token-wise.** The granted target is split on whitespace and the command is split the same way; the granted tokens must match the command's **leading** tokens one for one, each as a full word. A token may carry the reference's anchored `regex:` form. |
| `read_file` / `write_file` | a **path prefix**, with the Windows path normalisation applied first. |
| `read_url` / `execute_url` | a **hostname**, and a match on a **subdomain** of the granted host. |
| `mcp` | `server/tool`, `server/*`, or `*`. |

**An implicit write is allowed by a read grant, and an implicit read is
denied by a write grant.** A tool that writes as a side effect of
reading is covered by the read grant; a tool that reads as a side effect
of writing is not covered by the write grant.

`internal/core/policy/permissions/matching.go` owns these rules.

> **A one-shared-prefix-rule-for-six-actions is a bug, not a
> simplification.** It was the state before cycle 383's fix 8.

### The unified grammar

The grant grammar is unified in `internal/core/policy/permissions` (Tier 1 Core),
and reused by `internal/remotecontrol/permissions`. The action set, the three error sentences

```
invalid grant string: %q
unknown action %q in grant string: %q
unsandboxed is no longer a valid rule, use command() instead
```

and the retired `unsandboxed` have a single source of truth in `internal/core/policy/permissions/grant.go`.

## The three outcomes and their precedence

The engine returns one of five verdicts — `Allow`, `Deny`, `Ask`,
`ForceAsk`, `DenyUnlessPriorGrant` — but the rules themselves have
**three** tiers:

| tier | the rule |
|---|---|
| allow | `permissions.allow` |
| ask | `permissions.ask` |
| deny | `permissions.deny` |

The precedence is **Deny > Ask > Allow**. An explicit deny wins over an
explicit ask, and both win over an allow. A rule the engine does not
recognise is **rejected**, never silently ignored.

The five verdicts are not five tiers: `ForceAsk` and
`DenyUnlessPriorGrant` are the *hook* decisions' shapes, and they carry
the semantics the reference's own JSON schema names:

```
Decision for the tool call: 'allow' to proceed, 'deny' to block,
'ask' to request user confirmation.
```

## The scopes

A rules document holds three lists, and the panel shows them as three
tabs:

| tab | list | key |
|---|---|---|
| Project | `allowlist` / `denylist` / `asklist` | `permissions.allow` / `.deny` / `.ask` |
| Shared with Pigasus | the same three | the same keys |
| Global | the same three | the same keys |

`permissions.PermScopeNames` and `permissions.PermListNames` own those
vocabularies; `permissions.PermListKeys` owns the keys.

## `toolPermission`

The settings document's `toolPermission` member takes **four** values:

| value | what it does |
|---|---|
| `request-review` | asks before a write, a command, a web request or an MCP call |
| `proceed-in-sandbox` | runs a command without asking while the terminal sandbox is on |
| `strict` | asks before **every** tool call |
| `always-proceed` | never asks |

**The absent case is the clone's own.** The reference's default is
`request-review`, and it asks. The clone honours the setting **when it
is set** and stays **allow-by-default when it is absent**, so an
unconfigured install does not start prompting on every write. The
reference's ask is restored with `OPENAGY_PERMISSION_POLICY=strict`.

That difference is deliberate and is recorded as an open question in
the [cycle-383 report](../cycles/cycle-383-docs-alignment/deliverable.md).
`internal/printmode` still defaults its permission mode to
`request-review`, so the two entry surfaces disagree about the absent
case — also recorded.

## The hook verdicts

A hook handler answers with one of the six documented decisions, and
`permissions.Policy.HookVerdict` maps each onto the engine's own
verdict:

| hook decision | the engine's verdict |
|---|---|
| `allow` | `Allow` |
| `auto_approve` | `Allow`, **unless** an explicit deny rule matches |
| `deny` | `Deny` |
| `ask` | `Ask` |
| `force_ask` | `ForceAsk` |
| `deny_unless_prior_grant` | `Deny` without a prior grant, `Allow` with one |

**A decision the documentation does not name is a prompt.** It is never
an allow — an unrecognised answer must not fail open.

The hook runtime itself is documented in
[`hooks.md`](hooks.md).

## The `--mode` flag

The global `--mode` flag is a different thing and has its own
vocabulary: `accept-edits`, `auto`, `plan`. The flag's own format
string is `Set the agent execution mode for this session (%s)`, and the
warning for an unknown value is
`warning: unrecognized --mode value %q (valid: %s)` — both 【一手】
recoveries from agy 1.3.6. `permissions.Modes` holds the set in the
reference's order and `permissions.ModeList()` renders it.

`permissions.CycleModes` and `permissions.NextMode` implement the
shift+tab cycle, and `permissions.ModeBanner` the banner it prints.

### The four modes and the cycle

Four modes, and they are four, not three:

| mode | stored as | what it approves |
|---|---|---|
| `default` | `""` (or the key omitted) | nothing automatically: pauses for an inline diff review on writes and asks before commands |
| `accept-edits` | `"accept-edits"` | workspace file edits |
| `auto` | `"auto"` | workspace edits, sandboxed commands, and commands an AST pass judges safe and reversible |
| `plan` | `"plan"` | reads only; prepends the `/plan` prefix |

The lap is the one `official-docs/cli/modes.md` states:

```
default → accept-edits → auto → plan → default
```

`permissions.CycleModes` is that sequence and `permissions.NextMode` its only
reader. Every entry is its own stop, `""` included: `default` and `auto` store
differently, approve differently, and both appear in `/config`'s Agent Mode
list. A host whose terminal sandbox is unavailable declines `auto` and skips
from `accept-edits` to `plan` — that is the three-stop lap the same document
describes, not evidence that the two modes are one.

Two things that look like they say otherwise and do not:

- **AGY 1.5.7 / 1.5.18 changelog** — "Changed the default Agent Mode from
  Default to Auto". That names the mode a *new session starts in*
  (`setup.DefaultMode()`, which resolves to `auto` when the sandbox is
  available and `""` when it is not). It does not remove `default` from the
  cycle or from `/config`.
- **`permissions.CycleHint`** — `Press shift+tab to cycle modes (default,
  accept-edits, plan).` is verbatim in agy 1.5.18 and still omits `auto`,
  which landed in 1.3.5. It is a stale recovered string, kept for the record;
  nothing renders it, and it is not the cycle's definition.

The pin is `devutils/tuialign/scenarios/49_mode_cycle_order.align`, which
declares the lap as a `MODE_CYCLE_ROW` table — one row per stop, each carrying
the status row's exact right segment and the composer's banner — and walks it
with `MODE_CYCLE`. Each press is checked against its own row, so a wrong order
fails on the stop it is wrong at instead of passing because some other mode
word happened to be absent.

## The headless strings

Print mode has its own verbatim texts, recovered from the binary's
string table:

- `permissions.SkipPermissionsNotice` — `Print mode:
  --dangerously-skip-permissions set, auto-approving all tool
  permissions`
- `permissions.HeadlessNotice(n)` — the count of the auto-approved
  calls
- `permissions.HeadlessDenialNotice(...)` — the denial text
- `permissions.DeniedToolResult(action, target)` — the tool result the
  model reads when a call is denied

A **denied** call is a *result*, not an error: the model reads it and
decides what to do next.

## The Terminal Sandbox and Symlink Containment

When `enableTerminalSandbox` is set to `true` (controlled via `/config` or settings), the sandbox (`internal/core/policy/sandbox`) restricts `run_command` from accessing paths outside approved roots:

1. **Allowed Roots**:
   - `Cwd`: the active workspace root.
   - `ArtifactDir`: `~/.gemini/antigravity-cli/artifacts` (brain outputs and review panes). Note this is the *sandbox's* root, distinct from the conversation's artifact directory (`<appDataDir>/brain/<conv-id>`), which artifacts.tmpl names and which is a tool write root rather than a sandbox root — see [compaction.md](compaction.md) §"artifacts.List".
   - `ScratchDir`: `~/.gemini/antigravity-cli/brain/<conv-id>/scratch` (intermediate turn artifacts).
   - `ExtraPaths`: subagent worktrees and explicit additional directories.

2. **Symlink Escape Protection**:
   - Path containment does not rely solely on string prefixes. `canonicalPath` evaluates existing path prefixes via `filepath.EvalSymlinks`.
   - If a file or symlink inside the workspace resolves to a target outside the allowed roots (e.g. `workspace/link -> /etc`), access is blocked.
   - macOS `/var` -> `/private/var` canonical roots are handled symmetrically so legitimate aliases inside allowed roots remain permitted.

3. **Sensitive Path Blocking & Safe Read Carve-outs**:
   - Sensitive paths are blocked by default: `.config`, `XDG_CONFIG_HOME`, `.git-credentials`, `AppData/Roaming`, and macOS `Library/Application Support`.
   - Safe SSH configuration reads are carved out for non-secret assets: `config`, `known_hosts`, `authorized_keys`, and `.pub` public keys.
   - Developer configuration directories (such as `.vscode`, `.idea`, `.git`) are permitted when reading configuration files.

4. **Single-Shot Bypass Contract**:
   - Bypassing the sandbox is strictly a per-call user decision (`BypassSandbox: true`).
   - The engine never stores a sticky bypass flag; subsequent commands re-evaluate the sandbox independently.
   - The one exception is the user's own session-level switch: `/yolo on` (below) turns the sandbox *off* for the session, which is a different thing from a call asking to bypass a sandbox that is still on. The engine still stores no per-call flag; the session's gate is simply off.

## `/yolo`: the session's own `--dangerously-skip-permissions`

`--dangerously-skip-permissions` is a startup flag, and the reference has no
way to set it on a session that is already running. `/yolo` is the clone's
superset command for exactly that case:

```
/yolo          same as /yolo on (the quick mode)
/yolo on       skip every prompt and turn the terminal sandbox off
/yolo off      ask again, and put the sandbox back as the session started
```

The bare form is the quick mode, not a toggle: `/yolo` means on, and a repeat
stays on ("Already on."). Turning the gates back on is always the explicit
`/yolo off`, because a user typing `/yolo` again means "keep going".

It is the one command that writes a permission gate from inside a turn. It has
to touch **both** gates, because they are independent — either one alone still
blocks a command:

| gate | what it says | owner |
|---|---|---|
| `Policy.SkipPermissions` | "do not ask" — `DecideCall` returns `Allow` outright | shared pointer (`m.session.policy`), so the parent runner, the subagent gate and `/config` all see it at once |
| `Sandbox.Enabled` | "you may not leave the workspace at all" — `SandboxBoundaryMiddleware` checks it *before* the policy runs | a **value** copied into the runner, the subagent gate and the tool declarations |

Because the sandbox is a copied value, `/yolo` writes `m.tools.sandbox` and
then re-derives the declarations and the runner from it
(`RootModel.rebuildToolSurface`). `AdvertisedToolDeclsWithSandbox` uses the
same flag to decide whether `run_command` offers its bypass parameter, so
rebuilding only the runner would leave the model looking at a surface that
describes a sandbox it does not have.

It does **not** call `reloadPluginsAndMCP`: re-reading the MCP configuration
from the config home and replacing the process-global provider is a plugin
reload's job, not a permission switch's.

Two boundaries worth knowing:

- **A turn in flight keeps the runner it captured when it started**
  (`agentturn.Env.ToolRunner`), so a switch applies from the next turn on —
  the same granularity as the flag, which is read when a run starts.
- **The flag is a floor.** A session that started with
  `--dangerously-skip-permissions` already has the gates off; `/yolo off`
  says so rather than pretending to re-arm them.

`/yolo off` does not re-implement `setup.SandboxFrom`'s rule (the settings
switch combined with the flag). It asks the same owner again
(`printmode.PrintModeSandbox`) and takes its answer, so the restored value is
the one the session would have started with.

Remote-agent sessions have no local gate; `/yolo` points them at
`/permissions <preset>` instead.

## Where to look

| what | where |
|---|---|
| the verdict enum, the modes, the grant grammar | `internal/core/policy/permissions/policy.go` |
| the rules document and its three lists | `internal/core/policy/permissions/settings.go` |
| the panel's vocabulary and the grant validator | `internal/core/policy/permissions/panel_rules.go` |
| the hook decision mapping | `internal/core/policy/permissions/policy.go` (`HookVerdict`) |
| the hook runtime | `internal/core/policy/hooks` — see [`hooks.md`](hooks.md) |
| the `/permissions` panel | `internal/ui/views/permissionspanel` |
| the settings panel's `toolPermission` row | `internal/ui/views/configpanel` |

---

*Provenance: the grant format regex, the six actions, the `unsandboxed`
rejection, the four `toolPermission` values and the verdict enum are
【一手】 recoveries from the reference binary's own string table and
JSON schema; the paths and the scopes are 【反编译】; the rule *shapes*
come from the official documentation (`official-docs/permissions.md`).
The cycle report is
[`cycle-383`](../cycles/cycle-383-docs-alignment/deliverable.md).*
