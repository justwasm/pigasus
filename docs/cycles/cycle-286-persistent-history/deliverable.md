# Cycle 286 — Prompt history: cross-session loading, slash commands, and streaming queues

Reported symptom: **`agy` allows cycling through previous prompt history with the Up/Down arrow keys in a brand new session, whereas `wagyu` shows no history on Up arrow.**

Investigation revealed three distinct parity gaps in history recording and recall:

1. **No startup loading into `m.history`**: While `pushHistory` correctly appended submitted prompts to `~/.gemini/antigravity-cli/history.jsonl`, `NewRunMode` and `runLiveTUI` never loaded existing entries at startup (`m.history` started as `nil`). On Up arrow in a fresh session, `historyUp` aborted immediately (`len(m.history) == 0`), leaving the user unable to recall any previous commands. Official `agy` loads cross-session history globally.
2. **Slash commands bypassed history**: In `submitPrompt`, handled slash commands (`handleSlashCommand`, `openSlashPanelArgs`, `handlePromptAtPrefix`) returned early before `m.pushHistory(prompt)` was called. Consequently, `/model`, `/clear`, `/help`, and `@subagent` commands were never added to `m.history` or `history.jsonl`, directly conflicting with the reference changelog requirement: *"Added support for capturing slash command history, allowing users to use the up arrow to replay previously entered slash commands."*
3. **Mid-stream queued prompts bypassed history**: When the model was generating (`m.streaming == true`), prompts submitted with Enter entered `m.streamQueue` and returned immediately without calling `m.pushHistory(prompt)`.

## Evidence method

Raw truecolour pty captures (200×40, `TERM=xterm-256color`, `COLORTERM=truecolor`), replayed via `pyte`:

| Capture | Subject | Result |
|---|---|---|
| `/tmp/ref_hist_new2.bin` | Official `agy` 1.4.3 started in fresh session, Up arrow pressed at t=4s | Prompt recalled immediately into input line (`> hello`) |
| `/tmp/test_hist_new.bin` | `wagyu` before this cycle, fresh session, Up arrow pressed at t=1s | Empty input line; Up arrow completely ignored |
| `/tmp/test_hist_work.bin` | `wagyu` after this cycle, fresh session, Up arrow pressed at t=1s | Prompt recalled immediately from `history.jsonl` into input line (`> hi`) |

## Changes made

- **`cmd/wagyu/history.go`**:
  - Added `(m *runMode) loadHistory()`: loads up to `HistoryCap` prompts from `convstore.LoadHistory("", HistoryCap)` into `m.history` if empty.
- **`cmd/wagyu/tui_run.go`**:
  - `runLiveTUI`: added `m.loadHistory()` right after `m.loadAnnouncements()`.
  - `submitPrompt`:
    - Moved `m.pushHistory(prompt)` up, immediately after picker autocompletion and the `isBlank` check, before slash panel, slash command, and `@subagent` routing.
    - Added `m.pushHistory(prompt)` in the `m.streaming` queue branch so prompts queued while the model is thinking/responding are recorded.
  - `submitOrQueuePrompt`: added `m.pushHistory(prompt)` for queue mode submissions.
- **`cmd/wagyu/history_persistent_test.go`**:
  - Added unit tests covering disk loading, Up/Down arrow navigation, slash command capture, streaming queue capture, and queue mode capture.

## Verification

- `go test -run 'TestLoadHistory|TestSubmitPrompt_Captures|TestSubmitOrQueuePrompt_Captures' ./cmd/wagyu`: **PASS**
- `go test ./cmd/wagyu`: **PASS**
- `go test ./cmd/... ./internal/...`: **PASS**
- Live pty verification (`/tmp/test_hist_work.bin`): **PASS**
