---
name: terminal
description: In-process persistent tmux-like terminal sessions (list, new, capture, send, resize, kill) shared across turns and /terminal. Read this skill before calling the terminal tool.
---

# `terminal` Tool Guide

Use the in-process `terminal` tool to manage persistent interactive PTY sessions inside the current `pigasus` instance (replacing basic `tmux` workflows). In TUI mode, sessions are shared live with the `/terminal` panel.

## When to Use `terminal` vs `run_command`

- Use **`run_command`** for one-shot commands, builds, tests, and scripts that exit cleanly.
- Use **`terminal`** when you need:
  - Persistent shell state across turns (`cd`, exported env vars, background jobs)
  - Long-running dev servers, watchers, or tailing logs you want to inspect later
  - Interactive programs (REPLs like `python3`/`node`, `vim`, `htop`, `less`, `gdb`, TUI menus)
  - Inspecting or driving sessions the user opened in `/terminal`

## Call Format

Pass `action` and an `args` object:

```json
{
  "action": "<list|new|capture|send|resize|kill>",
  "args": { ... }
}
```

## Actions

### 1. `list` — List active sessions (`tmux ls`)
```json
{"action": "list", "args": {}}
```
Returns each session's `id`, `name`, `title`, `cwd`, `pid`, `rows x cols`, `cursor`, `status`, and `scrollback_lines`.

### 2. `new` — Create a persistent session (`tmux new-session -d`)
```json
{
  "action": "new",
  "args": {
    "name": "dev",
    "cwd": "/path/to/dir",
    "command": "npm run dev",
    "rows": 24,
    "cols": 80,
    "wait_ms": 300
  }
}
```
- All `args` fields are optional.
- `name`: friendly alias you can pass as `id` in later calls.
- `command`: optional initial command (automatically followed by Enter).
- `wait_ms`: milliseconds to wait before capturing initial screen (default `150`, max `5000`).

### 3. `capture` — Capture screen & scrollback (`tmux capture-pane -p`)
```json
{
  "action": "capture",
  "args": {
    "id": "dev",
    "scrollback": 100,
    "raw": false
  }
}
```
- `id`: session ID or `name`. Omit to target the most recently active session.
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
- `id`: session ID or `name` (omit for most recently active session).
- `text`: literal text to write (does **not** append Enter automatically — include `"Enter"` in `keys` or `\r` in `text`).
- `keys`: array of key names sent in order after `text`.
- `paste`: set `true` to wrap `text` in bracketed paste (`\x1b[200~...\x1b[201~`), useful for multi-line snippets in editors/REPLs.
- `wait_ms`: milliseconds to wait after writing before returning the updated screen capture (default `150`, max `5000`).

#### Supported `keys` Names
- Navigation & editing: `Enter`, `Tab`, `Shift+Tab`, `Backspace`, `Esc`, `Space`, `Up`, `Down`, `Left`, `Right`, `Home`, `End`, `PgUp`, `PgDn`, `Delete`, `Insert`
- Control chords: `Ctrl+A` .. `Ctrl+Z` (e.g. `Ctrl+C` to interrupt, `Ctrl+D` for EOF, `Ctrl+Z` to suspend, `Ctrl+L` to clear), `Ctrl+@`, `Ctrl+[`, `Ctrl+\`, `Ctrl+]`, `Ctrl+^`, `Ctrl+_`
- Alt chords: `Alt+<key>` (e.g. `Alt+x`, `Alt+Enter`, `Alt+Left`)

### 5. `resize` — Resize terminal dimensions (`tmux resize-pane`)
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

### 6. `kill` — Terminate a session (`tmux kill-session`)
```json
{
  "action": "kill",
  "args": {
    "id": "dev"
  }
}
```

## Best Practices

1. **Inspect before interrupting**: Run `list` or `capture` before sending `Ctrl+C` or commands to an existing session so you know what program is in the foreground.
2. **Name your sessions**: Pass `name` on `new` (e.g. `"server"`, `"repl"`) so subsequent turns can address it reliably.
3. **Wait appropriately**: For slow commands or TUI redraws, pass `wait_ms` (e.g. `500`–`2000`) or follow up with `capture`.
4. **Clean up**: `kill` temporary sessions when finished (all sessions are also cleaned up automatically when `pigasus` exits).
