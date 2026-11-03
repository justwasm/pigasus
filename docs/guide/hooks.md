# The hooks runtime

> The `hooks.json` lifecycle: the six events, the document shape, the
> decision vocabulary and the handler execution. The runtime lives in
> `internal/core/policy/hooks` (tier 1, stdlib only).

## The six events

A hook definition is keyed by an **event**, and the six names are the
reference's own:

| event | when it fires | matcher? |
|---|---|---|
| `SessionStart` | once, when a new conversation session starts, before the first model invocation | no |
| `PreToolUse` | before a tool is executed | yes — the tool |
| `PostToolUse` | after a tool completes | yes — the tool |
| `PreInvocation` | before the model is called | no |
| `PostInvocation` | immediately after each model invocation completes | no |
| `Stop` | when the execution loop terminates | no |

`hooks.Event.UsesMatcher()` answers the last column, and the hooks
panel's `IsTool` cell is derived from it rather than from a hard-coded
index.

## The documents

Two documents are read, in this order:

| document | path |
|---|---|
| global | `~/.gemini/config/hooks.json` |
| workspace | `<workspace>/.agents/hooks.json` |

The **global** path is the reference's own tree, not the clone's
configuration home (`~/.openagy`) — the two are different trees, and the
path is resolved here rather than through `configdir`. The
documentation names the global path for both the App and the CLI, and
says explicitly that the hooks in `~/.gemini/antigravity-cli/
settings.json` are **not** loaded; the document is therefore the only
global source.

The **workspace** path is the same for the App, the CLI and the IDE:
`.agents/hooks.json` at the project root.

A **missing** document is not an error — the clone simply has no hooks.
A document that cannot be read or parsed is **skipped**, and the reason
is reported in the registry's `Problems`: one broken document does not
take the other one with it. The workspace document is loaded second, so a
workspace hook fires **after** a global one.

## The document shape

The documented shape is a map from a hook name to a map from an event to
a list of handlers:

```json
{
  "my-hook": {
    "PreToolUse": [
      { "matcher": "run_command", "command": "…", "timeout": 30 }
    ]
  }
}
```

The loader accepts **three** shapes, and the acceptance of the other two
is the clone's tolerance, not the reference's:

1. the documented `{"name":{"Event":[…]}}`;
2. the Claude-Code `{"hooks":{"Event":[…]}}`;
3. the legacy flat `{"hooks":[{"event":…}]}`.

Matchers are **anchored regular expressions**, and `""` or `"*"` matches
everything.

## The decisions

A handler answers on stdout with a decision. The six names are the
reference's own:

| decision | meaning |
|---|---|
| `allow` | let the call proceed |
| `auto_approve` | approve when the default policy would otherwise prompt, honouring the user's explicit rules |
| `deny` | block the execution |
| `ask` | prompt the user, respecting the "always allow" settings |
| `force_ask` | always prompt, ignoring the cached permissions |
| `deny_unless_prior_grant` | block unless the resource was approved in a prior grant |

`hooks.Decisions()` returns them in the documented order, and
`hooks.DecisionRank` orders them by severity so that several handlers'
answers merge into one decision.

The mapping onto the permission engine's own verdict is in
[`permissions.md`](permissions.md).

## The execution

A handler is a **shell command**. The runtime writes the event's input
as JSON on the handler's **stdin** and reads the answer as JSON on its
**stdout**.

- A per-handler `timeout` is honoured; the default is **30 s**.
- The **shell is a seam**: `hooks.Runtime.Shell` is a field, and
  `hooks.FallbackShellArgv` is the default. The core tier cannot reach
  the app tier's shell, so the host passes its own
  (`shell.DefaultShellArgv`). This is the one thing the runtime does not
  decide for itself.

`hooks.Outcome` is the merged result of a firing: the decision, the
arguments a handler rewrote, and the notes it produced.

## Where it is armed

**The interactive TUI only.** The events fire from
`internal/cliapp/hooks_runtime.go`:

| event | where |
|---|---|
| `SessionStart` | once per session (reset on an `/agent` switch, which mints a new conversation id) |
| `PreInvocation` | per host turn |
| `PostInvocation` | per host turn, deferred |
| `Stop` | at the turn's end, deferred |
| `PreToolUse` | in front of the policy and the tool |
| `PostToolUse` | only when the tool actually ran |

Print mode (`internal/printmode`) and the agent server
(`internal/app/agentserver`) do **not** run hooks. The print path has
no interactive approval surface, so a `PreToolUse` handler could only
ever soft-deny there.

## What is parsed but not applied

The runtime reads more than it acts on, and the difference is recorded
as open questions in the
[cycle-383 report](../cycles/cycle-383-docs-alignment/deliverable.md):

- `injectSteps` is parsed but **not** injected into the trajectory;
- the `Stop` hook's `"continue"` is parsed but there is **no** loop
  re-entry;
- `permissionOverrides` is parsed but **not** applied;
- the `overwrite` *notice* is **not** prepended to the tool result — the
  argument **merge** itself *is* applied;
- `transcriptPath` and `artifactDirectoryPath` are left empty, because
  the clone does not write the reference's transcript files and must not
  name a path that is not there;
- `PreInvocation` / `PostInvocation` fire **per host turn**, not per
  internal model call — the reference's own boundary is not recovered.

## The panel

`internal/ui/views/hookspanel` lists the six events with their
descriptions and their `IsTool` cell. It is the **editor** surface; the
runtime is the **execution** surface, and the two are separate packages
on purpose — the panel is tier 5, the runtime is tier 1.

`internal/features/plugins/hooks` is the **plugin-scoped** hook bus: the
hooks a plugin ships, as opposed to the user's own `hooks.json`.

## Where to look

| what | where |
|---|---|
| the six events | `internal/core/policy/hooks/events.go` |
| the decision vocabulary and the severity order | `internal/core/policy/hooks/decision.go` |
| the document shape and the loader | `internal/core/policy/hooks/document.go` |
| the path resolution and the two documents | `internal/core/policy/hooks/paths.go` |
| the handler execution | `internal/core/policy/hooks/runtime.go` |
| the live arming in the TUI | `internal/cliapp/hooks_runtime.go` |
| the `/hooks` panel | `internal/ui/views/hookspanel` |
| the plugin-scoped bus | `internal/features/plugins/hooks` |

---

*Provenance: the event names, the decision names, the document shape and
the paths are 【一手】 from the official documentation
(`official-docs/hooks.md`) and from the reference binary's own string
table; the merge order and the severity ranking are the clone's.*
