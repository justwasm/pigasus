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
