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
| `pigasus livechat` / `--debug-wire` | standalone & embedded live multimodal chat TUI/Web UI with real-time duplex wire diagnostics | `internal/ui/livechat`, `cmd/livechat`, [`remote-agents.md`](remote-agents.md) §3.6 |
| `pigasus stats` | hidden CLI subcommand generating local session usage & cost HTML dashboard | `internal/app/subcommands/stats`, `internal/features/stats` |
| `pigasus mic-serve` | local audio server over `JETSKI-AUDIO/1` (`micwire`); exposes real-time `MicWireStats` | [`remote-agents.md`](remote-agents.md) §3.6 |
| `/wire` & `Ctrl+Shift+W` | interactive TUI Wire Diagnostics HUD overlay; real-time RTT, TTFT, packets and tok/s rate | `internal/ui/views/wirehud` |
| Trajectory Cold Storage | tiering folded turns (<= checkpoint boundary) to disk to bound heap in 200+ turn sessions | `actor/trajectory.go` |
| Dynamic Routing Failover | multi-provider fallback chain (CloudCode -> Gateway -> Claude/GPT) with typed error interception | `internal/infra/backend/failover` |
| Plugin Runtime Hot-Reload | hot-reloading tool catalogs & MCP providers during active turn on filesystem changes | `internal/features/plugins/watch` |
| Upstream Parity Fuzzer | AST and binary string symbol fuzzing against upstream reference binary to detect schema drift | `scripts/re/parity_fuzz.py` |
| `/terminal` | interactive PTY panel (`midterm`) & transcript runner across local (`rcterminal`), server, and `--remote-agent` sessions; listed in `/` autocomplete | `internal/ui/views/terminalpanel`, `internal/tui/terminal_service.go`, [`remote-agents.md`](remote-agents.md) §3.6 |
| `terminal` tool & built-in skill | in-process persistent tmux-like PTY tool (`list`/`new`/`capture`/`send`/`resize`/`kill`) shared with `/terminal` in TUI and available in `--print` / `agentserver` without approval prompts; full usage in embedded `terminal/SKILL.md` unpacked to `~/.openagy/builtin-skills/` | `internal/infra/terminalmgr`, `internal/app/tools/terminal`, `internal/features/tools/skills/pigasusskills` |
| `/upload <path>` | hidden host-only wrapper over `uploadProjectFile` | [`remote-agents.md`](remote-agents.md) §3.6 |
| `/triggers` | hidden host-only trigger management | [`remote-agents.md`](remote-agents.md) §3.6 |
| `/upload`, `/triggers` | hidden from the `/` picker; registered in `slashcommands.Hidden` | [`hidden-commands-and-flags.md`](hidden-commands-and-flags.md) |

The hidden registration is load-bearing: `slashHandlers` and
`slashcommands.Hidden` must agree, or `TestHiddenSlashCommandsAreNotTheCatalog`
and `TestVocabularyDispatchedMatchesTheHostTable` fail. In a local session
`/upload` and `/triggers` answer `… only available in a --remote-agent session.`
(`/terminal` was promoted to `slashcommands.Default` and works locally via
`LocalTerminalService` backed by `rcterminal`).

Everything else a remote session uses — `@` files, `WAITING` prompts,
`/continue`, `/permissions`, `/plugin`, `/skills`, `/diff` — is **parity**,
and its row in `remote-agents.md` §3.6 is the single source of truth.

## 3. Interactive terminal panel (`/terminal`)

`/terminal` supports both an **output path** (`/terminal run <command>` streams
into the transcript) and an **interactive modal panel** (`/terminal`,
`/terminal new`, `/terminal open|attach [id]`) unified across local, server,
and `--remote-agent` sessions via `TerminalService`
(`internal/tui/terminal_service.go`).

Implemented (Phase A & Phase B):

- `internal/ui/views/terminalpanel`, registered in `panelregistry.Terminal` and
  `slashcommands.Default`, rendering through `github.com/vito/midterm`.
- Screen + `OnScrollback` history (up to 2000 lines) + `PgUp`/`PgDn`/`Home`/`End`;
  `Esc` exits scrollback or detaches without killing the PTY (`resumeOffset`
  preserved), and never leaks to global turn cancellation (`AGENTS.md` §10).
- Key encoding (`internal/ui/views/terminalpanel/key_encoder.go`): arrows,
  Home/End/PgUp/PgDn/Delete, `Ctrl+A..Z` → `0x01..0x1a` (including `Ctrl+C`
  passthrough to the PTY), `Alt+key` → `ESC` + key, bracketed paste.
- `Panel.Resize` updates `midterm` and forwards dimensions to `LocalTerminalService`
  (`rcterminal.Registry.Resize`).

Remaining (Phase C — polish):

- Copy mode, alt-screen passthrough, reconnect on a dropped stream,
  multi-terminal picker, and wiring `RemoteAgentTerminalService.Resize` (currently
  a no-op).

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
go test ./internal/ui/views/terminalpanel/... -count=1
go test ./internal/tui/ -run 'TestHidden|TestSlashTerminal|TestSlashUpload|TestSlashTriggers' -count=1
go test ./internal/infra/remoteagentclient/ ./internal/infra/mockremoteagent/ -count=1
```
