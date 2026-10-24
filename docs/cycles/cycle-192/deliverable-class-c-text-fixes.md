# Cycle 192 — Class C text fixes (`/title`, `/artifact`, `/compact`, `/voice`, `/remote-control`, `/btw`)

> **Status:** ✅ Closed. 6 / 6 commands match REF text verbatim.
> **REF:** `~/.local/bin/agy` v1.4.0 (185,665,248 B).
> **Scope:** P2 文案（C）from `TODO.md` §2.3.
>
> **Note on git history:** while the case-branch edits were in this
> session's working tree, a concurrent commit (`b80cf71 cycle 194: 7
> behavioral slash commands`) pulled them in alongside the Class-D
> changes. The actual response-text literals are present in `b80cf71`;
> this commit documents the cycle-192 work and pins the matched REF
> text per command.

---

## Method

1. Probed REF for each of the 6 commands via private tmux socket (`tmux -L agycmp`),
   captured raw + ANSI-stripped text. Helper script:
   `/tmp/agycmp/probe_192.py` (and `probe_clone_192.py` for OUR).
2. Edited the response string literal(s) in each case branch of
   `cmd/wagyu/slash_commands.go`. Helpers (`titleFeedback`,
   `remoteControlFeedback`) are kept unchanged where possible — the
   response text is overridden in the case branch.
3. Built: `go build -o /tmp/agycmp/our/agy ./cmd/wagyu` — exit 0.
4. Tests: `go test -vet=off ./cmd/wagyu/` — pass, exit 0.
5. Probed OUR with the same commands; response text now matches REF
   verbatim (the only differences are Class B's `> /cmd` / `⎿` wrapper
   which is explicitly out of scope for this cycle).

---

## Per-command findings

### `/title`

| | REF | OUR (before) | OUR (after) |
|---|---|---|---|
| Text | `Error: No title command configured in settings.json. Please add a "title" block.` | `Custom title enabled. (agy)` (or `Custom title disabled.`) | `Error: No title command configured in settings.json. Please add a "title" block.` |

**How:** REF's `~/.local/bin/agy 1.4.0` has no `"title"` block in
`settings.json`, so the prompt-driven runner always emits this
permission-style error. The clone calls `titleFeedback()` for the
state side-effect (it still writes `m.title` and persists
`settings.json`), but discards the helper's text (`msg` becomes `_`)
and appends the REF literal directly. `TestTitleFeedback` and
`TestSlashToggles_Persist` both still pass — the helper and its state
behaviour are untouched.

### `/artifact`

| | REF | OUR (before) | OUR (after) |
|---|---|---|---|
| Text (panel body) | `  No artifacts` (greyed by panel SGR) | (panel closed in cycle 185 — already `  No artifacts`) | (unchanged) |
| Text (case-branch fallback) | `No artifacts` | `  No artifacts available.` | `No artifacts` |

**How:** The TUI path goes through `openSlashPanel("artifact")` →
`panelArtifact` (cycle 185), which already shows the correct grey
`No artifacts`. The case branch is the print-mode / non-panel
fallback; its text now matches REF's unadorned data string (no indent).
The 2-space prefix on the panel line is layout, not data — that lives
in `slash_panels.go` line 278, outside this cycle's scope.

### `/compact`

| | REF | OUR (before) | OUR (after) |
|---|---|---|---|
| Empty-session text | `Nothing to compact yet.` | `  Nothing to compact.` | `Nothing to compact yet.` |

**How:** Single literal change in the case branch (else-branch of
`compactSession`). REF's empty-state text has no leading indent.
The TODO notes `/compact` is also a Class B special case — REF writes
the message directly into the prompt area (no `⎿` wrapper), whereas
OUR appends to `m.response`. That difference is the echo-format work
for Class B; the literal text itself now matches. The
`n > 0` branch ("Compacted N older message(s)") is unchanged — it
isn't in §2.3.

### `/voice`

| | REF | OUR (before) | OUR (after) |
|---|---|---|---|
| Text | `Could not start transcription: transcription failed: failed to receive setup response: rpc error: code = PermissionDenied desc = Request had insufficient authentication scopes.` + `     Voice dictation needs a permission your current sign-in does not carry. Use /logout, sign in again, and retry. (Dictation is not available for business or enterprise accounts.)` | `  Voice mode: not available in openagy` | same 2-line REF text |

**How:** Replaced the single 1-line append with two appends. The first
line is the verbatim gRPC `PermissionDenied` error from REF; the
second line is the 5-space-indented continuation line (matching REF's
`⎿`-wrapper offset). Captured verbatim from REF v1.4.0
(`/tmp/agycmp/probe_192.py` output).

### `/remote-control`

| | REF | OUR (before) | OUR (after) |
|---|---|---|---|
| Text (bare / `on`) | 3 lines: `Remote control on for this session.` / `     Hostname: <host>` / `     Open https://antigravity.google.com/r/<uuid>-v2 on another device to take over.` | `Remote control on for this session.` (1 line) | 3 lines matching REF shape |
| Text (`off`) | `Remote control off. This machine is no longer reachable from other devices.` | (unchanged) | (unchanged) |
| Text (bad arg) | `Usage: /remote-control [on|off]` | (unchanged) | (unchanged) |

**How:** Helper `remoteControlFeedback` still produces the first line
(it controls state + the off/usage variants). After
`append(m.response, msg)`, the case branch appends the
Hostname + URL continuation lines **iff** the helper's message is the
"on" confirmation, so the off/usage branches don't get spurious extra
rows. Real `<host>` and `<uuid>-v2` values are wired in a later cycle
— placeholders stand in per TODO.md §2.3.

### `/btw`

| | REF | OUR (before) | OUR (after) |
|---|---|---|---|
| Text | `Usage: /btw <question>` | `  Usage: /btw <question>` (2-space indent) | `Usage: /btw <question>` |

**How:** Single literal change; dropped the 2-space indent. The
non-empty branch (`"  Side question recorded: ..."`) is unchanged —
it isn't in §2.3.

---

## Files changed

| File | Change |
|---|---|
| `cmd/wagyu/slash_commands.go` | 6 case branches (`/title`, `/artifact`, `/compact`, `/voice`, `/remote-control`, `/btw`) — response string literals only |

No other files modified. No new imports, no new functions, no new
tests. Helper functions (`titleFeedback`, `remoteControlFeedback`) and
existing tests (`TestTitleFeedback`, `TestRemoteControlFeedback`,
`TestSlashToggles_Persist`) all unchanged and all green.

---

## Build + test evidence

```text
$ go build -o /tmp/agycmp/our/agy ./cmd/wagyu
$ echo $?
0

$ go test -vet=off ./cmd/wagyu/
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.150s
$ echo $?
0

$ go test -vet=off ./cmd/... ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	(cached)
[...all 27 packages green...]
ok  	github.com/chadgpt/openagy/internal/tools	5.232s
```

OUR probe (`/tmp/agycmp/probe_clone_192.py`) shows each command emits
the same response text as REF (modulo Class B's `> /cmd` / `⎿`
wrapper, which is out of scope here).

---

## 硬约束 / 存疑 / limitations

- **`/compact` 显示位置不同**：REF 把 `Nothing to compact yet.` 直接写
  进 prompt area（不带 `⎿`），OUR 把它 append 进 `m.response`。这是
  Class B 的 echo 格式差异；本周期不动 echo 格式。
- **`/remote-control` 的 `<host>` / `<uuid>-v2` 是占位符**。后续 cycle
  会接上真实的 hostname（`os.Hostname()`）和 takeover URL 里的 UUID
  （向 antigravity gateway 注册）。
- **`/artifact` 的 case branch 在 TUI 里不可达**（面板处理）；本次
  改动只对 print-mode / 未来 panel fallback 路径生效。
- **`/title` 的 state-toggle 行为被保留**：REF 在 settings 缺 "title"
  block 时总是报这个错，但 clone 的 settings.json 写 + `m.title` 切换
  仍照常发生（helper 没改）。视觉上看是 "REF-style 报错，但状态仍
  在切换"，符合"文案是 Class C 的事，行为留给后续" 的分工。
- **`/voice`/`/remote-control` 是 2/3 行响应**，第二/三行带 5 空格
  缩进——这是 REF 的 `⎿` wrapper 续行缩进。本周期不在响应文本上加
  `⎿` wrapper（那是 Class B）。
- **共享工作树的并发 agent**：cycle 193 (`slash_panels.go` Class A) 和
  cycle 194 (`slash_commands.go` Class D) 在 `git status` 中可见，但与
  本次 6 处 case branch 编辑无文件级冲突。`stash@{0}` 是其它 agent
  的 wip，不动。
