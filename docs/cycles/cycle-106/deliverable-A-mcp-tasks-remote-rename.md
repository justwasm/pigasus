# Cycle 106 — `/mcp`, `/tasks`, `/remote-control`, `/rename`

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** four more cycle-77 stubs, each with recovered strings — the cheapest
remaining targets named at the end of cycle 105.

---

## 1. `/mcp` — list configured servers 【一手 empty case / 推测 layout】

Recovered: `No MCP servers configured.` (the reference's default — its
`mcp.json` starts as `{"servers":{}}`), the description *“Manage MCP servers”*,
and the panel strings `MCP servers` / `MCP server %d of %d  (enter to search)`.

`mcpServersFeedback(cfg)` returns the recovered empty line when there are no
servers, and otherwise a header plus one sorted row per server:

```
MCP servers
  alpha	http	disabled	https://example.test/mcp
  zeta	stdio	enabled	npx -y z
```

The per-server row is **【推测】**: the reference opens an interactive panel whose
layout cannot be captured (the account quota is exhausted, so no reference TUI
transcript is obtainable). `auth.LoadMCPConfig` reads
`~/.gemini/antigravity-cli/mcp.json` — the v4 path — and treats a missing file
as "no servers", which is what `WriteV135CompatDefaults` produces.

> Note: the `mcp list` **subcommand** resolves its config differently
> (`$ANTIGRAVITY_HOME/mcp.json`, then `~/.config/antigravity/mcp.json`; cycle 49
> matched that byte-for-byte). The two paths are left as they are; on a stock
> install `ANTIGRAVITY_HOME` is unset, so `mcp list` and `/mcp` can disagree
> about which file they read. Recorded, not fixed.

## 2. `/tasks` — background-task counters 【一手】

Recovered: `No background tasks.` for the empty case and `%d running, %d done`
for a non-empty one (the description is *“View background tasks”*).

`tasksFeedback(running, done)` picks between them. The clone runs no background
tasks, so only the empty branch is reachable today; the counted branch is
implemented and unit-tested so a future scheduler cycle gets the right line for
free.

## 3. `/remote-control [on|off]` 【一手】

Recovered: `Usage: /remote-control [on|off]`,
`Remote control on for this session.` and
`Remote control off. This machine is no longer reachable from other devices.`

The description — *“Turn on remote control for this session (off to disable)”* —
makes the **bare form mean on**, not a toggle (unlike `/statusline` and
`/title`). An unrecognised argument prints the usage line and leaves the flag
alone.

The clone never opens the tunnel (Remote Control is an enterprise-gateway
feature), so `remoteControlFeedback` only tracks the session flag.

## 4. `/rename <name>` 【一手】

Recovered: `Error: Please provide a new name for the conversation.` for an empty
or whitespace-only argument, and `Conversation renamed to: %s` on success. The
description is *“Rename the current conversation”*.

`renameFeedback` returns (name, message) and the model stores the name in
`conversationName`. `conversationTitle()` now prefers it, so `/rename` changes
what `/title on` names — the first user message remains the fallback, then the
program name.

## 5. Files changed

| File | Change |
|---|---|
| `cmd/antigravity/slash_actions.go` | `renameFeedback`, `tasksFeedback`, `remoteControlFeedback`, `mcpServersFeedback`; `conversationTitle` prefers the renamed name |
| `cmd/antigravity/slash_commands.go` | the four cases implemented |
| `cmd/antigravity/tui_v135.go` | `conversationName` and `remoteControl` fields |
| `cmd/antigravity/slash_actions_test.go` | 7 new tests |
| `internal/auth/v135_compat.go` | `LoadMCPConfig` |

## 6. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean except the
  pre-existing header-comment misalignment in `internal/auth/v135_compat.go`.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both diffs documented in cycle 101).
* Live pty probe (throwaway `ANTIGRAVITY_HOME`), four commands in one session:

  ```
  > /mcp
  No MCP servers configured.
  > /tasks
  No background tasks.
  > /rename Refactor auth
  Conversation renamed to: Refactor auth
  > /remote-control on
  Remote control on for this session.
  ```

> One harness137 run reported 22/25 with a `skills_slash` stdout diff and a
> matching rc (`0/0`); two immediate re-runs returned 23/25. `/skills` reads the
> developer's `~/.gemini/skills`, so treat that row as environment-sensitive
> noise, not a regression. Same pattern as the cycle-102/105 reference flakiness.

## 7. Divergences and limitations

* **`/mcp`'s per-server layout is 【推测】** — see §1. Only the empty case is
  recovered.
* **`/mcp` and `mcp list` may read different config files** — see §1.
* **`/tasks` can only ever print the empty line.** There is no background
  scheduler in the clone, so `%d running, %d done` is unreachable.
* **`/remote-control` opens no tunnel.** The flag is tracked and reported, but
  nothing connects; the reference's `Remote control enabled, starting
  connection` / `Remote control disabled, stopping connection` log lines have no
  counterpart.
* **`/rename` does not persist.** The reference writes a conversation title
  (`conversation_metadata.json`, `writing generated conversation title for %s:
  %w`); the clone keeps the name in memory for the session only, because it has
  no conversation store.
* **No live reference comparison** — the account quota is exhausted.
* **Still cycle-77 stubs (~10):** `/diff`, `/keybindings`, `/voice`, `/resume`,
  `/rewind`, `/fork`, `/feedback`, `/artifact`, `/btw`, `/codesearch`, `/open`,
  `/context`, `/plugin`. `/diff` (`No uncommitted changes`,
  `No file changes in conversation`, `No commit history available`) and
  `/context` (`Visualize current context usage`) are the next cheapest.
