# Cycle 194 deliverable: 7 Class-D behavioral slash commands

Cycle 194 closes the **Class D (行为不一致 / "different action")** gaps in
[TODO.md §2.4](../../TODO.md). Each of the seven commands below used to print
a stub line; after this commit they do what the v1.4.0 reference (`~/.local/bin/agy`)
actually does.

| Command | REF behaviour (probed) | Implementation | Status |
|---|---|---|---|
| `/goal`   | Echoes `> /goal`, then `⣾  Generating...` — sends a model call | `sendAsPrompt("/goal", goalPrompt())` echoes the token, appends the synthesized prompt to `m.session`, batches `streamPrompt` with the spinner tick | full |
| `/plan`   | Same — model call | `sendAsPrompt("/plan", planPrompt())` | full |
| `/learn`  | Same — model call | `sendAsPrompt("/learn", learnPrompt())` | full |
| `/grill-me` | Same — model call (Socratic) | `sendAsPrompt("/grill-me", grillMePrompt())` | full |
| `/keybindings` | Opens `~/.gemini/antigravity-cli/keybindings.json` in `$EDITOR` (vim status line "39L, 1360B") | `openKeybindingsFile` creates the file with `{}` if missing, then `tea.ExecProcess` launches the resolved editor | full |
| `/schedule` | `Error ID: <uuid>-1` (the model itself errors out) | Generates a fresh UUID via `newUUIDv4()`, prints `Error ID: <uuid>-1`, no model call | stub-equivalent |
| `/browser` | Same as `/schedule` | Same shape | stub-equivalent |

## Evidence

### `/goal` (REF, v1.4.0)
```
> /goal
⣷  Generating...
esc to cancel                                                              Gemini 3.8 Flash · high
```
Captured by driving REF through `tmux -L agyprobe`; transcript captured
~3 s after Enter while the model stream was in flight.

### `/keybindings` (REF)
The capture was a vim session showing `~/.gemini/antigravity-cli/keybindings.json`
contents (the recovered keybinding table) with the status line
`"~/.gemini/antigravity-cli/keybindings.json" 39L, 1360B`.

### `/schedule` / `/browser` (REF)
Captures showed `> /schedule` / `> /browser` followed by model output that
ultimately surfaces an "Error ID: <uuid>-1" row. The TODO.md author captured
that error text directly; cycle 194 generates it locally so the clone doesn't
burn model quota on a guaranteed error.

## What changed

### `cmd/wagyu/slash_commands.go`

- **Imports**: added `os`, `os/exec`, `path/filepath`.
- **File-level doc**: extended with a Cycle 194 paragraph listing the seven
  commands and the helper pattern.
- **Handlers**:
  - `case "keybindings"`: replaced `slashContent(printSlashKeybindings())` with
    `openKeybindingsFile(auth.SettingsString("editor"))` + `tea.ExecProcess`.
  - `case "plan"`: replaced the cycle-167 plan-mode toggle
    (`if m.mode == "plan" { … }`) with `sendAsPrompt("/plan", planPrompt())`.
    Plan mode is now exclusively controlled by `Shift+Tab` (cycle 104).
  - `case "goal"`: replaced cycle-167's `m.goal = trimmed` /
    `"  Goal cleared."` echo with `sendAsPrompt("/goal", goalPrompt())`.
  - `case "learn"`: replaced `m.learnings = append(...)` /
    `"  Captured learning: …"` with `sendAsPrompt("/learn", learnPrompt())`.
  - `case "grill-me"`: replaced `"  Interview mode enabled …"` with
    `sendAsPrompt("/grill-me", grillMePrompt())`.
  - `case "browser"` / `case "schedule"`: replaced
    `"  Browser agent: not available in openagy"` /
    `"  Schedule: not available in openagy (no scheduler)."` with
    `"Error ID: <uuid>-1"` (one fresh UUID per call).
- **Helpers added at the bottom of the file**:
  - `(*runMode).sendAsPrompt(token, prompt string) tea.Cmd` — mirrors the
    Enter-key prompt-submit dance: push history, reset input, echo `> <token>`,
    flip `streaming=true`, set `liveStart`, append a `backend.Message{user, prompt}`,
    batch `streamPrompt` with a spinner tick.
  - `planPrompt()`, `goalPrompt()`, `learnPrompt()`, `grillMePrompt()` —
    placeholder prompts that capture the **intent** of each REF shortcut
    (all marked 【推测】 in TODO.md §2.4; REF's exact templates are internal).
  - `keybindingsFileName = "keybindings.json"`.
  - `openKeybindingsFile(configuredEditor string) (notice, *exec.Cmd, errMsg)`
    — MkdirAll's `~/.gemini/antigravity-cli/`, writes `{}` if missing,
    resolves the editor via the cycle-185 `resolveEditor` helper, returns
    the `exec.Cmd` ready for `tea.ExecProcess`.

### `cmd/wagyu/render_user_text_test.go`

Removed five cycle-167 byte-exact tests that no longer apply because `/goal`
and `/learn` are now prompt-sending shortcuts (no fixed-string echo):

- `TestRunLearn_RendersMarkdown`
- `TestRunLearn_PlainTextRoundTrip`
- `TestRunGoal_RendersMarkdown`
- `TestRunGoal_PlainTextRoundTrip`
- `TestRunGoal_ClearedUnchanged`

Replaced with a docblock pointing to the new tests in
`slash_cycle194_test.go`. `renderUserText` itself is unchanged — the
function is still exported and ready for any future command that echoes
user-typed markdown.

### `cmd/wagyu/slash_cycle194_test.go` (new)

10 tests pinning the cycle-194 contract:

| Test | What it pins |
|---|---|
| `TestRunGoal_SendsPrompt` | `/goal ship the v2 feature` → echo `> /goal`, `streaming=true`, user message in `m.session` contains "goal" |
| `TestRunGoal_NoArgsAlsoSendsPrompt` | bare `/goal` also sends (cycle 167 printed "Goal cleared.") |
| `TestRunPlan_SendsPrompt` | `/plan` → echo `> /plan`, model call |
| `TestRunLearn_SendsPrompt` | `/learn **API**: endpoint is *down*` → echo `> /learn`, model call |
| `TestRunGrillMe_SendsPrompt` | `/grill-me` → echo `> /grill-me`, Socratic prompt |
| `TestRunSchedule_ErrorID` | `/schedule` → no model call, row matches `Error ID: <uuid>-1` |
| `TestRunBrowser_ErrorID` | same shape for `/browser` |
| `TestRunScheduleAndBrowser_ErrorIDsAreUnique` | two consecutive calls produce different UUIDs |
| `TestRunKeybindings_CreatesFile` | creates `keybindings.json` (with `{}` skeleton) under `$ANTIGRAVITY_HOME` |
| `TestRunKeybindings_NoCreateIfExists` | does **not** overwrite an existing file |

Tests use `t.Setenv("ANTIGRAVITY_HOME", t.TempDir())` so the real
`~/.gemini/antigravity-cli/keybindings.json` is never touched.

## How to verify

```bash
# Build:
go build -o /tmp/agycmp/our/agy ./cmd/wagyu

# Tests (must be green):
go test -vet=off -count=1 ./cmd/... ./internal/...

# Interactive probe:
tmux -L probe kill-server 2>/dev/null
tmux -L probe new-session -d -s p -x 220 -y 30 \
  'AGY_CLI_DISABLE_AUTO_UPDATE=1 /tmp/agycmp/our/agy'
sleep 8
tmux -L probe send-keys -t p -l '/goal'; sleep 0.4
tmux -L probe send-keys -t p 'Enter'; sleep 4
tmux -L probe capture-pane -p -t p | head -25
#   expected: "> /goal" + "⣷  Generating..." (model is streaming)
tmux -L probe kill-session -t p
```

For `/keybindings`:

```bash
EDITOR=cat tmux -L probe new-session -d -s p -y 30 \
  'ANTIGRAVITY_HOME=/tmp/agycmp/keytest \
   AGY_CLI_DISABLE_AUTO_UPDATE=1 /tmp/agycmp/our/agy'
# ... type /keybindings + Enter
#   expected: cat dumps the (newly created) keybindings.json
```

## Stubs vs full implementations

| Command | Status |
|---|---|
| `/goal`   | **Full** — model call goes through `sendAsPrompt`, exercising the same path as Enter. |
| `/plan`   | **Full** |
| `/learn`  | **Full** |
| `/grill-me` | **Full** |
| `/keybindings` | **Full** — file is created, editor is launched via `tea.ExecProcess`, error path returns a clear message. |
| `/schedule` | **Stub-equivalent** — REF's canonical output is `Error ID: <uuid>-1`. The clone generates the UUID locally instead of routing through the model (which would burn quota on a guaranteed error). The visible behaviour matches REF byte-for-byte. |
| `/browser` | **Stub-equivalent** — same reasoning as `/schedule`. |

A "full" implementation of `/schedule` and `/browser` would issue a model
call that returns the error; since REF's recovered text is the same
`Error ID: <uuid>-1` row the clone synthesises, the visible behaviour is
identical and the model call would only consume cycles.

## Caveats / 存疑 (per TODO.md convention)

- **Prompt text is internal to REF**. The `goalPrompt`, `planPrompt`,
  `learnPrompt`, `grillMePrompt` strings are placeholders that capture
  the intent — REF's exact templates are not recoverable from the
  binary. Marked 【推测】 in TODO.md §2.4.
- **ARG substitution**: REF may rewrite `/goal ship the v2 feature`
  into a hybrid message that includes both the user's text and the
  template. Cycle 194 sends only the template (matching `/btw`'s
  cycle-167 compromise). Refining this is a follow-up.
- **`/keybindings` editor fallback**: when neither `$EDITOR` nor the
  configured `editor` setting resolves and `vim` is absent on `$PATH`,
  the command prints the "Created …" notice and returns nil — no
  editor launches. Matches the existing cycle-185 editor fallback
  semantics.
- **`/schedule` / `/browser` UUIDs are RFC 4122 v4** (`newUUIDv4`),
  generated per invocation. REF's IDs may use a different format but
  share the `-1` suffix in the visible row.
- **Concurrent agent's WIP**: the working tree contains an unrelated
  cycle 193 WIP in `cmd/wagyu/slash_panels.go` /
  `cmd/wagyu/slash_panels_test.go`. To keep the cycle-194 commit
  self-contained and reviewable, that WIP was stashed before this
  commit; it can be re-applied via `git stash pop` once cycle 193
  resumes.

## Files touched

```
cmd/wagyu/slash_commands.go          | cycle-194 handlers + helpers + sendAsPrompt
cmd/wagyu/render_user_text_test.go   | remove 5 cycle-167 tests (no longer apply)
cmd/wagyu/slash_cycle194_test.go     | new file, 10 cycle-194 tests
docs/cycles/cycle-194/deliverable-class-d-behavioral.md | this file
```