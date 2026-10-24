# Cycle 205 — action panels: key actions for `/permissions`, `/mcp`, `/agents`, `/hooks`

Reference: `~/.local/bin/agy` **1.4.0** (`OPENAGY_BACKEND=agw`), private tmux socket
`tmux -L agy205`, pane **220×50**. Clone: `OPENAGY_BACKEND=mock`.
Companion deliverable for the *A-class* panels: `docs/cycles/cycle-201/deliverable-panel-key-actions.md`.

---

## TL;DR

Cycle 195/201 drew the four "action" panels but the clone only handled a flat
`↑/↓` + `esc` for them; every key the panels advertise in their own legends was
ignored. This cycle gives `/permissions` and `/hooks` their measured three-stage
navigation, implements the one measurable `/agents` action (`Enter`), and leaves
`/mcp` untouched. The final live REF-vs-clone comparison of **13 panel frames at
220×50 reports 0 mismatching lines**, across all four panels.

---

## What was wrong

`handlePanelKey()` routed every action panel through one flat handler:

* `/permissions` — no scope stage, no allow/deny/ask tabs, no `a`/`e`/`d` actions,
  no rule form. `panelPermissionsLines()` only ever drew the scope picker.
* `/hooks` — no type→matcher→form navigation; `Enter` was a no-op, so the
  `enter Select` the legend advertises did nothing.
* `/agents` — `Enter` did nothing although the reference echoes
  `Already using default agent.` and keeps the panel open.
* `/mcp` — the empty-state frame already matched; left alone.

The panels' own legend strings (`a Add rule`, `e Edit rule`, `d/⌫ Delete rule`,
`enter Select`, `e Toggle`, `backspace Delete`, `enter Confirm`, `k Kill Active
Subagent`) were unimplemented.

---

## What changed

| File | Δ lines | What |
|---|---|---|
| `cmd/wagyu/slash_action_panels.go` | **+760** (new) | The whole cycle-205 implementation: `actionPanelState` (permission + hook state), scope/list storage helpers (Global → `settings.json`, Project → the default-cli-project config, Shared → session-local), `validateGrant`, the `/permissions` and `/hooks` stage renderers, and the per-panel key handlers. |
| `cmd/wagyu/slash_cycle205_test.go` | **+380** (new) | 15 tests: scope wrap, tab row bytes, empty state, tab wrap, form render/error, `validateGrant`, add/delete, edit-delete-then-append, both hooks matcher frames, both hooks forms, hooks navigation, `/agents` echo, `/mcp` guard. |
| `cmd/wagyu/slash_panels.go` | **+23 / −2** | Minimal wiring: one `act actionPanelState` field on `slashPanel`; `panelLines()` dispatches `panelHooks`/`panelPermissions` to the stage-aware renderers; `handlePanelKey()` early-dispatches `panelPermissions`/`panelHooks`/`panelAgents`; one 存疑 comment near `exitNotice`. No other file touched. |

Nothing else was edited: `internal/convstore/sqlite.go` untouched, no file
reformatted, `gofmt -l` clean on both new files.

---

## Evidence (probe → raw line)

All captures under `…/T/opencode/agy205/ref/`. Legend/rows below are the
ANSI-stripped content of the reference frame.

**`/permissions`** — `probe_verify2.py`, `probe_perm_sep2.py`, `probe_lists.py`, `probe_project.py`, `probe_actions2.py`, `probe_read.py`

* stage 1 (`perm-after-enter.raw.txt`, Project; `permissions-global-stage2.raw.txt`, Global):
  ```
  Permissions — Project
   allowlist (0)    denylist (0)    asklist (0)   (←/→ to switch)
  No allowlist rules.
  Keyboard: ↑/↓ Navigate  ←/→ Switch View  a Add rule  e Edit rule  d/⌫ Delete rule  esc Close
  ```
  Tab **counts are live** (`Global allowlist (16)` in `permissions-global-stage2.raw.txt`)
  and a rule row is cursor-prefixed `> ` (`permissions-global-down2.raw.txt`:
  `> command(wc)`). `probe_perm_sep2.py` re-ran the legend 3/3: the separators
  between `Navigate`/`←/→` are **two spaces**.
* stage 1 empty list (`perm-s2-denylist.raw.txt`): `No denylist rules.`
* stage 0 ↑/↓ **wraps** (`probe_verify2.py`: `Up` at Project lands on Global).
* stage 2 form (`perm-global-addrule.raw.txt`, `perm-edit-after.raw.txt`):
  ```
  Add Rule — Global — allowlist
  Enter a permission rule:
  Format: action(target)
  Keyboard: ↑/↓ Navigate<TAB>enter Select  esc Close
  ```
  and after a rejected rule: `✗ invalid grant string: "command(grep)ZZ"`.
* validation (`probe_read.py`, single-candidate probes): `command` accepted;
  `read` and `foo` → `unknown action "foo" in grant string: "foo(bar)"`;
  malformed → `invalid grant string: "X"`.
* persistence (`probe_actions2.py` + `probe_project.py` marker probes):
  Global → `~/.gemini/antigravity-cli/settings.json` `permissions.<allow|deny|ask>`;
  Project → `~/.gemini/config/projects/default-cli-project.json`
  `permissionGrants.permissionGrants.<allow|deny|ask>`.

**`/hooks`** — `probe_hooks.py`, `probe_hooks3.py`

* stage 0 (`hooks-open.raw.txt`): ` Hooks` / `   5 hook types` / rows
  ` > PreToolUse      Before tool execution` … ` Stop            When agent tries to exit`
  / `   ↑/↓ Navigate · enter Select`.
* stage 1 (`hooks-enter.raw.txt`, PreToolUse):
  ```
   PreToolUse — Matchers
     Before tool execution
   > + Add new matcher...
     + Match all (no filter)
     No hooks configured
     ↑/↓ Navigate · enter Select · e Toggle · backspace Delete · esc Back
  ```
  A lifecycle type uses `+ Add new hook...` with no `Match all` row (`probe_hooks.py`).
* stage 2 (`hooks-add-enter.raw.txt`):
  ```
   Add new matcher for PreToolUse
     Before tool execution
     Matcher:
   >
     Examples:
     • Write (single tool)
     • Write|Edit (multiple tools)
     • Web.* (regex pattern)
     enter Confirm · esc Cancel
  ```
  A lifecycle type gets the Name/Command/Timeout form
  (`Event: …`, `Name:`, `Command:`, `Timeout (seconds):` / `default: 30`,
  `⚠ Hooks run shell commands with your full permissions.`,
  `tab Switch field · enter Confirm · esc Cancel`) — `probe_hooks3.py`.

**`/agents`** — `probe_agents.py` (3 runs), `probe_agents2.py`, `probe_agents3.py`

* `Enter` on the default agent echoes `Already using default agent.` **and the
  panel stays open**. The literal is also a first-hand string in the reference
  binary (`strings -a ~/.local/bin/agy | grep 'Already using default agent'`
  → present, adjacent to `List available custom agents` / `Installing plugin from
  %q...`).
* `esc-ref-agents-Esc.txt` (cycle 195) shows a closed, echo-free screen; a live
  220×50 probe showed the panel surviving two `Esc` presses → ambiguous, the
  pre-cycle `exitNotice` behaviour was left **untouched** (存疑).
* `k Kill Active Subagent` with no active subagent does nothing
  (`ref/agents-k.txt`).

**`/mcp`** — `mcp-empty-open.raw.txt`: `MCP Servers` / `  No MCP servers
configured.` / `Keyboard: ↑/↓ Navigate  enter Actions  esc Exit` — already
matched the pre-cycle clone frame, so no change.

---

## Live parity

`cmp205.py` boots REF (`agw`, 13 s) and clone (`mock`, 8 s) on `tmux -L agy205`
at 220×50, types the slash command + `Enter`, sends a key sequence, captures with
`-e`, strips SGR and compares the panel block line-by-line (title → status row).
Command: `python3 cmp205.py` (results in `/tmp/cmp205.out`).

| Panel | Case (`cmd` + keys) | Mismatched lines |
|---|---|---|
| `/permissions` | `[]` (scope picker) | 0 |
| `/permissions` | `Down Enter` (Shared tab view) | 0 |
| `/permissions` | `Down Down Enter` (Global tab view, 15 rows) | 0 |
| `/permissions` | `Down Down Enter Right` (Global denylist, empty) | 0 |
| `/permissions` | `Down Down Enter a` (Add Rule form) | 0 |
| `/permissions` | `Down Down Enter e` (Edit Rule form) | 0 |
| `/hooks` | `[]` (type list) | 0 |
| `/hooks` | `Enter` (PreToolUse matchers) | 0 |
| `/hooks` | `Down Down Enter` (PreInvocation matchers) | 0 |
| `/hooks` | `Enter Enter` (matcher form) | 0 |
| `/hooks` | `Down Down Enter Enter` (add-hook form) | 0 |
| `/agents` | `[]` (open) | 0 |
| `/mcp` | `[]` (empty open) | 0 |
| **Total** | | **0** |

---

## Tests

`cmd/wagyu/slash_cycle205_test.go` — 15 tests, all passing:

`TestCycle205PermissionsScopeWrap`, `…PermissionsTabView`, `…PermissionsEmptyState`,
`…PermissionsTabSwitchWraps`, `…PermissionsFormRender`, `…PermissionsFormError`,
`…ValidateGrant`, `…PermissionsAddDelete`, `…PermissionsEditDeletesThenAppends`,
`…HooksMatchersTool`, `…HooksMatchersLifecycle`, `…HooksMatcherForm`,
`…HooksHookForm`, `…HooksNavigation`, `…AgentsEnterEcho`,
`…MCPEmptyFrameUnchanged`.

The add/delete and edit tests exercise the **session-local Shared** scope, so
they never read or write `settings.json` (the Global/Project store helpers are
covered by the frame tests, which read the live files read-only).

Gates:

```
go build -o <probe>/our/agy ./cmd/wagyu                       # ok
go test -vet=off -run TestCycle205 ./cmd/wagyu/               # ok
go test -vet=off ./cmd/... ./internal/...                     # all ok except two
                                                              # pre-existing failures
gofmt -l cmd/wagyu/slash_action_panels.go \
        cmd/wagyu/slash_cycle205_test.go                      # empty
```

`TestCycle197_ConfigVisibleRows` and `TestPanelConfigLayout` fail on **HEAD**
too (`TestPanelConfigLayout` asserts 20 config rows; the real `settings.json`
now yields 21). Pre-existing, unrelated, not touched.

---

## 存疑 (unresolved — left inert deliberately)

1. **`/hooks` `e Toggle` and `backspace Delete`** (matcher list). Advertised in
   the legend, but with no hooks configured their effect is unobservable → both
   keys are swallowed and do nothing.
2. **`/hooks` `enter Confirm`** in the add-matcher/add-hook form. The confirm
   path persists a hook; neither the store nor the resulting frame could be
   captured → `Enter` is inert. No file was observed to be written by the
   reference's hooks confirm.
3. **`/mcp` `enter Actions`** (non-empty panel). Measured (`mcp-nonempty*.raw.txt`)
   but the live server statuses are environment-dependent, so the Actions menu
   was **not** implemented; the empty-state frame is what the clone renders.
4. **`/agents` `esc`.** Ambiguous: the live 220×50 probe kept the panel open
   across two `Esc` presses, while the cycle-195 capture
   `esc-ref-agents-Esc.txt` shows a closed, echo-free screen. The pre-cycle
   `exitNotice(panelAgents)==true` (Esc closes, with notice) was left unchanged.
5. **The full grant-action set.** Only `command` was verified as accepted;
   `read` and `foo` were verified rejected. Any other action the reference might
   accept is unknown, so `validateGrant` accepts `command` only.
6. **Shared scope persistence.** Global and Project write targets were located
   by marker probes; the "Shared with Antigravity" scope's on-disk target was
   not located, so its three lists are kept **session-local in memory**.
7. **Form legend separator.** `probe_perm_add.py` (3/3) captured the reference's
   form legend with a literal TAB (`Navigate\x1b[39m\t\x1b[38;5;69menter`). The
   clone writes **two spaces** there, because the clone's cell renderer drops
   raw control characters (measured: a literal `\t` loses both itself and its
   preceding SGR reset). At 220×50 the two render to the same cells — the final
   comparison of that line is equal, 0 mismatches — so the two-space form is
   used; an exact byte comparison of the legend is capture-dependent.

## Limitations

* Every parity claim is for **agy 1.4.0 at 220×50** on this machine; a different
  build shifts both addresses and some status-line text.
* The comparison compares the *panel block* only (title → status row). The status
  row itself differs between REF and clone (`auto · ` prefix) and is excluded.
* `/permissions` Global counts depend on the live `settings.json`, which other
  agents mutate concurrently; the two binaries in a comparison read it seconds
  apart.
* Mutating probes were run against backed-up copies of `settings.json` and
  `default-cli-project.json` and both were restored byte-for-byte (verified with
  `shasum`).
