# pigasus-only surfaces (index)

> **This is a pointer, not a second copy.** The descriptions live in the
> canonical docs named below; this file only answers *which* surface is an
> openagy addition, and where its source of truth is.
>
> Canonical: [`remote-agents.md`](remote-agents.md) for the wire and the
> client coverage, [`hidden-commands-and-flags.md`](hidden-commands-and-flags.md)
> for anything reachable but unlisted, [`agent-server.md`](agent-server.md)
> for the `agentlink` client/server split (a different protocol).

## 1. Three kinds of surface

| Kind | Test | Where it is documented |
|---|---|---|
| **Parity** | the reference has the same command/flag/RPC | [`remote-agents.md`](remote-agents.md) §2–3.6 |
| **pigasus-only** | the reference has no CLI surface; openagy adds one | this file, §2 |
| **scoped out** | the protocol exists, but a session does not use it | [`remote-agents.md`](remote-agents.md) §3.7 |

## 2. The openagy-only surface, and its canonical doc

| Surface | What is openagy-specific | Canonical description |
|---|---|---|
| `pigasus --remote-agent` | a native client of `JetskiRemoteAgentProxyService`; it does not spawn `agy` | [`remote-agents.md`](remote-agents.md) §2.3, §3.6 |
| `pigasus agent-server` | the mock server as a subcommand; `mock-remote-agent` is the compatibility shell | [`remote-agents.md`](remote-agents.md) §2.1 |
| `/terminal` | hidden host-only command over `create/list/send/stream/closeTerminal` | [`remote-agents.md`](remote-agents.md) §3.6 |
| `/upload <path>` | hidden host-only wrapper over `uploadProjectFile` | [`remote-agents.md`](remote-agents.md) §3.6 |
| `/triggers` | hidden host-only trigger management | [`remote-agents.md`](remote-agents.md) §3.6 |
| all three commands | hidden from the `/` picker; registered in `slashcommands.Hidden` | [`hidden-commands-and-flags.md`](hidden-commands-and-flags.md) |

The hidden registration is load-bearing: `slashHandlers` and
`slashcommands.Hidden` must agree, or `TestHiddenSlashCommandsAreNotTheCatalog`
and `TestVocabularyDispatchedMatchesTheHostTable` fail. In a local session the
three commands answer `… only available in a --remote-agent session.`

Everything else a remote session uses — `@` files, `WAITING` prompts,
`/continue`, `/permissions`, `/plugin`, `/skills`, `/diff` — is **parity**,
and its row in `remote-agents.md` §3.6 is the single source of truth.

## 3. The one open design: an interactive terminal panel

`/terminal` is an **output path**: it runs a command and streams the output
into the transcript. The protocol and the server PTY are complete; the panel
is not built. This section is the only part of this file that is not a
pointer, because the design is not written down anywhere else.

Phase A — read-only panel:

- `internal/ui/views/terminalpanel`, registered like `/resume`, rendering
  through `github.com/vito/midterm` (already a dependency:
  `internal/ui/views/canvas`).
- screen + `OnScrollback` history + `PgUp`/`PgDn`; detach keeps the terminal
  and its `resumeOffset`, reattach replays from there.
- exit shows the code; `q`/`Esc` closes without killing the PTY.

Phase B — input and size:

- key encoding (`tea.Key` → PTY bytes): arrows, Home/End/PgUp/PgDn/Delete,
  `Ctrl+A..Z` → `0x01..0x1a`, `Alt+key` → `ESC` + key, bracketed paste.
- while the panel is focused `Ctrl+C` goes to the PTY and `Esc` only detaches;
  it must not reach turn cancellation (the modal rule, `AGENTS.md` §10).
- `midterm.Resize` plus a `sendTerminalInput` resize action; the PTY has no
  initial size, so the first attach must send one.

Phase C — polish: copy mode, alt-screen, reconnect on a dropped stream,
multi-terminal picker.

## 4. Limits worth knowing

- The agent-server keeps triggers **in memory**; a real service persists them.
- `/upload` stores files under `$TMPDIR/pigasus-project-uploads/<project>`
  with a basenamed file name.
- Remote `/skills` are deduped by content hash against local skills, so a
  same-machine mock collapses identical skills into the local group.
- `getCodeActionDiff` joins the step's absolute URI to the client workspace;
  a genuinely remote root falls back to `getTurnDiff` contents (see the path
  caveat in `remote-agents.md` §3.6).

## 5. Verification

```bash
make quick
make arch
go test ./internal/tui/ -run 'TestHidden|TestSlashTerminal|TestSlashUpload|TestSlashTriggers' -count=1
go test ./internal/infra/remoteagentclient/ ./internal/infra/mockremoteagent/ -count=1
```
