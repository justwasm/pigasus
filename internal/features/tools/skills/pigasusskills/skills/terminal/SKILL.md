---
name: terminal
description: Persistent tmux-like terminal sessions and host multiplexer tabs (list, new, capture, send, switch, rename, resize, kill), including self-inspection via id="self" inside `pigasus terminal`. Read this skill before calling the terminal tool.
---

# `terminal` Tool Guide

Use the `terminal` tool to manage persistent interactive PTY sessions (replacing basic `tmux` workflows):
- When running inside **`pigasus terminal`**, the tool automatically connects to the host multiplexer (`PIGASUS_TERMINAL_SOCKET`), so you can inspect your own TUI tab (`"id": "self"`) and create/switch/rename/drive tabs in the user's live Omni Bar.
- When running standalone in TUI mode, sessions are shared live with the `/terminal` panel.

## When to Use `terminal` vs `run_command`

- Use **`run_command`** for one-shot commands, builds, tests, and scripts that exit cleanly.
- Use **`terminal`** when you need:
  - Persistent shell state across turns (`cd`, exported env vars, background jobs)
  - Long-running dev servers, watchers, or tailing logs you want to inspect later
  - Interactive programs (REPLs like `python3`/`node`, `vim`, `htop`, `less`, `gdb`, TUI menus)
  - Inspecting your own TUI screen (`"id": "self"`) or orchestrating tabs in `pigasus terminal` / `/terminal`

## Call Format

Pass `action` and an `args` object:

```json
{
  "action": "<list|new|capture|send|switch|rename|resize|kill>",
  "args": { ... }
}
```

## Actions

### 1. `list` — List active sessions / tabs (`tmux ls`)
```json
{"action": "list", "args": {}}
```
Returns each session's `id`, `name`, `title`, `cwd`, `active`, `self`, `rows x cols`, `cursor`, `status`, and `scrollback_lines`.

### 2. `new` — Create a persistent session / tab (`tmux new-session -d`)
```json
{
  "action": "new",
  "args": {
    "name": "dev",
    "cwd": "/path/to/dir",
    "command": "npm run dev",
    "focus": false,
    "rows": 24,
    "cols": 80,
    "wait_ms": 300
  }
}
```
- All `args` fields are optional.
- `name`: friendly alias you can pass as `id` in later calls.
- `command`: optional initial command (automatically followed by Enter).
- `focus`: when running inside `pigasus terminal`, set `true` to switch the visible tab to the new session (default `false` keeps the current tab focused).
- `wait_ms`: milliseconds to wait before capturing initial screen (default `150`, max `5000`).

### 3. `capture` — Capture screen & scrollback (`tmux capture-pane -p`)
```json
{
  "action": "capture",
  "args": {
    "id": "self",
    "scrollback": 100,
    "raw": false
  }
}
```
- `id`: session ID, `name`, `"self"` (the current tab running `pigasus` inside `pigasus terminal`), or omit to target the active session.
- `scrollback`: number of scrollback history lines above the screen to include (default `0`, max `500`).
- `raw`: set `true` to keep ANSI escape sequences (default `false` strips ANSI for clean reading).

### 4. `send` — Send text and/or special keys (`tmux send-keys`)
```json
{
  "action": "send",
  "args": {
    "id": "dev",
    "text": "ls -la",
    "keys": ["Enter"],
    "paste": false,
    "wait_ms": 200
  }
}
```
- `id`: session ID, `name`, `"self"`, or omit for active session.
- `text`: literal text to write (does **not** append Enter automatically — include `"Enter"` in `keys` or `\r` in `text`).
- `keys`: array of key names sent in order after `text`.
- `paste`: set `true` to wrap `text` in bracketed paste (`\x1b[200~...\x1b[201~`), useful for multi-line snippets in editors/REPLs.
- `wait_ms`: milliseconds to wait after writing before returning the updated screen capture (default `150`, max `5000`).

#### Supported `keys` Names
- Navigation & editing: `Enter`, `Tab`, `Shift+Tab`, `Backspace`, `Esc`, `Space`, `Up`, `Down`, `Left`, `Right`, `Home`, `End`, `PgUp`, `PgDn`, `Delete`, `Insert`
- Control chords: `Ctrl+A` .. `Ctrl+Z` (e.g. `Ctrl+C` to interrupt, `Ctrl+D` for EOF, `Ctrl+Z` to suspend, `Ctrl+L` to clear), `Ctrl+@`, `Ctrl+[`, `Ctrl+\`, `Ctrl+]`, `Ctrl+^`, `Ctrl+_`
- Alt chords: `Alt+<key>` (e.g. `Alt+x`, `Alt+Enter`, `Alt+Left`)

### 5. `switch` — Switch active tab (`tmux select-window`)
```json
{
  "action": "switch",
  "args": {
    "id": "dev"
  }
}
```

### 6. `rename` — Rename a tab (`tmux rename-window`)
```json
{
  "action": "rename",
  "args": {
    "id": "dev",
    "title": "backend-server"
  }
}
```

### 7. `resize` — Resize terminal dimensions (`tmux resize-pane`)
```json
{
  "action": "resize",
  "args": {
    "id": "dev",
    "rows": 40,
    "cols": 120
  }
}
```
Minimum size is `4x10`.

### 8. `kill` — Terminate a session / close tab (`tmux kill-session`)
```json
{
  "action": "kill",
  "args": {
    "id": "dev"
  }
}
```

## MCP Server Mode

External MCP clients running inside `pigasus terminal` can also connect via stdio MCP:
```bash
pigasus terminal mcp
```
This exposes `terminal_list`, `terminal_new`, `terminal_capture`, `terminal_send`, `terminal_switch`, `terminal_rename`, `terminal_close`, plus resources `terminal://sessions`, `terminal://self/screen`, and `terminal://session/<id>/screen`.

## Best Practices

1. **Inspect before interrupting**: Run `list` or `capture` before sending `Ctrl+C` or commands to an existing session so you know what program is in the foreground.
2. **Self-inspection**: Inside `pigasus terminal`, use `capture` with `"id": "self"` to inspect your own rendered TUI layout, colors (`raw: true`), or scrollback.
3. **Name your sessions**: Pass `name` on `new` (e.g. `"server"`, `"repl"`) so subsequent turns can address it reliably.
4. **Wait appropriately**: For slow commands or TUI redraws, pass `wait_ms` (e.g. `500`–`2000`) or follow up with `capture`.
5. **Clean up**: `kill` temporary sessions when finished.
