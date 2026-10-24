# Cycle 98 A — permission modes, headless tool confirmation, and the non-network tools

Scope: follow-up **(2)** from the cycle-97 plan — tool-calling parity's remaining
half. Two parts, both landed:

1. the recovered `--mode` surface, and the permission policy that decides whether
   a tool call runs (including the headless confirmation behaviour);
2. real implementations for every advertised tool that can run without a Google
   service.

Parity target stays **agy 1.3.6** (`~/.local/bin/agy`, 183,407,440 B, SHA-256
`827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

## Result

| Area | Before | After |
|---|---|---|
| `--mode` validation | not implemented | byte-exact warnings, hook on every agent-session entry point |
| Permission policy | none | `internal/permissions`, five-way verdict, settings.json rules, mode rules |
| Headless tool confirmation | tool ran unconditionally | policy gate + recovered soft-deny text + end-of-run notice |
| Real tools | 5 of 32 | **26 of 32**; the 6 left are network/MCP |
| Tests | — | +46 test functions (policy 11, settings 4, tools 15, `--mode` 6, gate 10) |
| Parity harness | 53/53 vs 1.3.6 | **61/61** vs 1.3.6 (8 new `--mode` cases) |

## Part 1 — `--mode` and the permission policy

### The recovered surface

Both warnings below are **format strings** in the binary, not literals:

```
Set the agent execution mode for this session (%s)
warning: unrecognized --mode value %q (valid: %s)
warning: --mode %s is not available for this account; continuing in the default mode.
Print mode: --mode %s is unavailable (rollout flag off), continuing in the default mode
Print mode: --mode %s is not supported headless, continuing in the default mode
Print mode: applying agent mode %s
Print mode: --dangerously-skip-permissions set, auto-approving all tool permissions
Print mode: soft-denying tool confirmation %q at step %d
```

Substituting the same list into all of them is why `ModeValues` is the single
source of truth for the flag usage text, the unrecognized-value warning, and the
policy — a fourth copy cannot drift.

Valid values are exactly `accept-edits`, `auto`, `plan`.

### Emission surface (probed, not guessed)

| Entry point | `unrecognized` | `not available for this account` |
|---|---|---|
| `models` | yes | no |
| `agents` / `agent` (no args) | yes | no |
| `--print` | yes | **yes** |
| interactive (no args, `-i`, `--prompt-interactive`, `--continue`) | yes | dies at the TTY check first |
| `changelog`, `plugin`, `mcp`, `update`, `install`, `remote-control`, `--help` | no | no |

The warning sits **after** argument validation: `agy --mode bogus models extra`
prints only the unexpected-arguments error. Both warnings go to **stderr**.

### The policy engine (`internal/permissions`)

The verdict enum is recovered from a JSON-schema tag on the reference's policy
call:

```
jsonschema:"required,enum=allow,enum=deny,enum=ask,enum=force_ask,enum=deny_unless_prior_grant"
jsonschema_description:"Decision for the tool call: 'allow' to proceed, 'deny' to block, 'ask' to request user confirmation."
```

plus the follow-up sentence ``"force_ask": Always prompt the user, ignoring cached permissions.``

`Policy.Decide` applies them in the reference's order: `--dangerously-skip-permissions`
wins outright → an explicit deny rule blocks → an explicit allow rule permits →
the mode's auto-approval rules → the default.

Mode rules come from the two recovered mode banners, which name the only
classifications the evidence supports:

```
Accept-edits mode: file edits auto-approved (shift+tab to cycle)
Auto mode: edits & safe commands auto-approved (shift+tab to cycle)
```

Path rules mirror the recovered rule description ("Matches the file or
everything under the directory. Paths must be literal and absolute.") as a
case-sensitive prefix match on the call's target (`path`, `command`, `file` or
`target`), read from `permissions.allow` / `permissions.deny` in
`~/.gemini/antigravity-cli/settings.json`.

### Headless tool confirmation

This is the part that changes behaviour. The recovered strings are unambiguous:

```
the %s tool(s) required approval that headless mode cannot prompt for, so they were
auto-denied. Settings allow-rules do not apply; re-run with --dangerously-skip-permissions
to auto-approve all tools.

%s required the %s %s that headless mode cannot prompt for, so %s auto-denied.
Add an allow-rule under permissions.allow in settings.json (e.g. %s)
```

and the text handed back to the model:

```
Permission prompt for action '%s' on target '%s' was denied. Do not attempt to
circumvent this denial by rephrasing the command, using alternative tools/scripts
(e.g. python, sh, curl), or accessing the same target resource. Proceed without
performing this action.
```

So in the real `agy`, `-p "what time is it"` does **not** run `date` unless
`--dangerously-skip-permissions` (or an allow-rule, or `--mode auto`) is present.

The clone keeps the mechanism but **defaults to allow** — the operator's choice
in this cycle, so `--print` keeps running tools out of the box. Setting
`OPENAGY_PERMISSION_POLICY=strict` restores the reference default, and that is
what the tests drive:

- `permissionRunner` wraps the registry runner. `Allow` → run; anything else →
  the recovered denial text is returned as the tool result and the call is
  recorded.
- At the end of the print run, if anything was denied, the recovered notice is
  written to stderr. Nothing is printed when nothing is denied, so the cycle-50
  "stdout is the model response, stderr is empty" contract is untouched.
- With `--dangerously-skip-permissions`, the recovered log line goes to the
  silent print-mode logger (the reference emits it as a `Print mode:`-prefixed
  log line, not user output).

The loop side is verified as a composition: `TestGateThroughAgentLoop` drives a
scripted backend that asks for `run_command`, and asserts (a) the tool never
reached the registry, (b) the turn completed normally with the model's final
text, and (c) the second request carried the denial text back as a `tool`
message.

## Part 2 — the non-network tools

32 tools are advertised (unchanged); **26 are now real**:

- **already real** — `run_command`, `view_file`, `write_to_file`,
  `replace_file_content`, `list_dir`;
- **implemented this cycle** — `multi_replace_file_content`, `sed_file`,
  `grep_search`, `find_by_name`, `codebase_search`, `view_content_chunk`,
  `command_status`, `send_command_input`, `manage_task`, `send_message`,
  `notebook_edit`, `notebook_execution`, `wait`, `finish`, `schedule`,
  `ask_question`, `ask_permission`, `ask_custom_permission`, `notify_user`,
  `list_permissions`, `manage_inbox`.

**Still `ErrNotImplemented` (6, all network or MCP):** `search_web`,
`read_url_content`, `generate_image`, `call_mcp`, `list_resources`,
`read_resource`.

Details worth calling out:

- **Absolute paths are rejected** with the recovered message
  `expected path %s to not start with /`, and every path is additionally
  confined to the workspace (`..` cannot escape).
- **`view_file` honours `start_line` / `end_line`**, which the catalog had
  advertised since cycle 97 but the executor ignored.
- **`grep_search`** reports the recovered empty-result line
  `No files were searched, double check your search parameter`; `find_by_name`
  supports `**/` prefixes; `sed_file` supports a line range, capture-group
  expansion and a `global` flag.
- **Background commands** are a real process table: `run_command` with
  `background:true` returns `cmd-N`, and `command_status` /
  `send_command_input` / `manage_task list|kill` address it. The `background`
  field is this clone's extension (there is no recovered schema for it) — it
  exists so the three supervisor tools have something to supervise.
- **`codebase_search` is an offline lexical ranking.** The reference searches
  semantically server-side; the clone ranks by term frequency. Same tool name,
  different quality.
- **Interactive tools report that the user cannot be reached** rather than
  inventing an answer — grounded in the recovered headless reminder
  `... If you have completed your task call the finish tool. Remember that the
  user cannot answer any questions.` `ask_permission` / `ask_custom_permission`
  return the recovered denial text.
- **`schedule` validates and reports that nothing was queued** instead of
  pretending a scheduler exists.

## Verification

- `go build ./cmd/antigravity` ✓, `go vet ./internal/... ./cmd/antigravity/` ✓,
  `go test ./internal/... ./cmd/...` ✓.
- `/tmp/agycmp/harness136.sh` — **61/61 MATCH** vs 1.3.6 (53 pre-existing plus 8
  new `--mode` cases). `--help` remains byte-identical.
- Mode-warning matrix, 10 command shapes × 5 mode values = **50/50 MATCH** on
  the two warning lines, in order.
- 46 new test functions; `internal/permissions` carries the verbatim-string
  assertions so a typo in any recovered literal fails the build.

## 硬约束 / 存疑 / limitations

- **The default diverges from the reference, deliberately.** The real `agy`
  soft-denies unprompted tool calls in headless mode; this clone allows them
  unless `OPENAGY_PERMISSION_POLICY=strict`. Documented at the top of
  `internal/permissions/policy.go`. Anyone diffing `-p` behaviour against the
  reference should set that variable.
- **The tool→class table is 【推测】.** `Classify` maps names to
  read/edit/command from each tool's documented behaviour, not from a recovered
  per-tool table — the `cortex/tools` package is not in the decompiled set. The
  reference's actual gating is server- and LLM-driven (there is a
  `command_assessor_config`, an `enable-command-assessor` feature flag, and
  `AdminControls_AgentControls_TerminalCommandAutoExecutionPolicy`), none of
  which is reproducible offline.
- **The argument schemas are this clone's shapes 【推测】.** The reference ships
  them through a Cortex protobuf schema that was not recovered; field names are
  reconstructed from behaviour. Tool *names* and user-visible *behaviour* match;
  the argument encoding does not.
- **`settings.json` rule encoding is inferred.** Only the key path
  (`permissions.allow`, `permissions.deny`) and the rule *semantics* are
  recovered. The clone accepts both `"tool:pattern"` strings and
  `{"tool","pattern"}` objects and skips unknown entries so a real settings.json
  stays loadable.
- **The tool-confirmation UI is not implemented.** `*ToolConfirmationModel`
  (35 seed hits), `ToolConfirmationModel.canAmend` / `choices` / `handleConfirm`
  / `handleSave`, and `RespondToToolConfirmation` exist in the reference; the
  clone has no TUI-side confirmation flow. Only the headless path is covered.
- **`printmode.advertisedTools(bool)` is a separate axis.** That bool is
  `(6-char store field == "jetski")` — the agent name, not the permission mode —
  and it is a distinct payload field from `EffectivePermissionMode` in
  `buildInitPayload`. The clone does not model either.
- **`notebook_execution` shells out to `jupyter nbconvert`** and fails with an
  explicit message when `jupyter` is missing; it is not exercised in CI.
- **No end-to-end run against a live model.** The gate is proven against a
  scripted backend and a stub runner, not against a model that actually emits
  tool calls. With the clone's mock backend the notice is consequently not
  reachable from the CLI — only from a `strict` run driven by a real backend.
- **Six `--mode`-adjacent print-mode messages are recovered but unused**:
  `--mode %s is unavailable (rollout flag off)`, `--mode %s is not supported
  headless`, `applying agent mode %s`. Which one fires depends on rollout state
  and entitlement the clone has no source for; only the "not available for this
  account" variant is reproduced, because that is the one the reference actually
  emits over a fresh HOME.
- `internal/tools/command.go` and `filesystem.go` were already unformatted at
  HEAD and are left that way, consistent with the repo's existing state.

## Supersedes

Cycle 97's limitation list is now partly stale. In
`docs/cycles/cycle-97/deliverable-A-tool-calling.md`:

- "Permission modes are not implemented" — superseded by this cycle.
- "Tool confirmation UI is not implemented" — still true, but the *headless*
  behaviour now exists.
- "Only `run_command` (plus fs aliases) is real" — superseded: 26 of 32.
