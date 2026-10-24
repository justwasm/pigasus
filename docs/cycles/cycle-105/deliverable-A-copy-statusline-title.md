# Cycle 105 — `/copy` argument forms, `/statusline` and `/title` toggles

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** three more cycle-77 stubs replaced with the recovered behaviour — and
one correction to cycle 103, whose `/copy` was a simplification.

---

## 1. `/copy` — the full argument forms 【一手】

Cycle 103 implemented `/copy` as "copy the last assistant message", silently
doing nothing when there was no message. The recovered command description shows
that was wrong on two counts:

> `/copy` — *Copy the last planner response to the clipboard, the n-th with
> '/copy n', or the /btw answer with '/copy btw'*

and the recovered strings supply the messages cycle 103 lacked:

| Situation | Message |
|---|---|
| nothing to copy | `No response to copy.` |
| `/copy btw`, no answer | `No /btw response to copy.` |
| `/copy btw`, answer in flight | `The /btw answer is still being generated.` |
| success | `Copied %s to clipboard.` with **`last response`** as the filler |

`resolveCopy(args, responses)` is now a pure function returning
(payload, message):

| Argument | Payload | Message |
|---|---|---|
| *(none)* | last planner response | `Copied last response to clipboard.` |
| `n` (1 = most recent) | n-th planner response | `Copied response n to clipboard.` |
| `n` out of range | — | `No response to copy.` |
| `btw` (any case) | — | `No /btw response to copy.` |
| non-numeric | last planner response | `Copied last response to clipboard.` |

The clone has no `/btw`, so the in-flight string is documented but unreachable.
"Planner responses" are the assistant messages in `m.session`
(`plannerResponses()`), which is what cycle 103 already copied.

## 2. `/statusline` — toggle with a visible effect 【一手 message / 推测 value】

Recovered: `Statusline on.` and `Statusline off.` (plus a second context,
`Statusline off. Run /statusline to re-enable.`, which belongs to the
auto-disable path, not the command). The reference's own description is
*“Toggle the statusline”*.

The command now:

* flips the state (on by default, so the first `/statusline` turns it **off**),
* hides the bottom status row in `View()` when off — the real effect, not just a
  message,
* persists it to `settings.json`'s `statusLine` key.

The persisted value (`"on"` / `"off"`) is **【推测】**: the live `settings.json`
has no `statusLine` key and `agy -p /config` reports it as empty, so the
reference's encoding is unrecovered.

## 3. `/title [on|off]` — custom terminal window title 【一手 message / 推测 text】

Recovered: `Usage: /title [on|off]`, `Custom title enabled. (%s)` and
`Custom title disabled.` The description is *“Toggle custom terminal window
title”*.

`titleFeedback(arg, current, titleText)`:

| Argument | Message |
|---|---|
| *(none)* | toggles; enabling prints `Custom title enabled. (%s)` |
| `on` / `off` (any case) | sets explicitly |
| anything else | `Usage: /title [on|off]`, state unchanged |

Turning it on sets `View().WindowTitle` (bubbletea v2's replacement for
`tea.SetWindowTitle`), so the terminal window title actually changes, and the
state persists to `settings.json`'s `title` key.

The `%s` is the title text. It is **unrecovered**; the clone passes the
conversation title — the first user message, or the program name (`agy`) when
there is none. The persisted value (`"on"` / `"off"`) is **【推测】** for the same
reason as §2.

## 4. Settings plumbing

Cycle 103's `AddTrustedWorkspace` was refactored onto a shared
`updateSettings(mutate)`, and two new helpers were added:

* `auth.SetSettingsString(key, value)` — the write half of both toggles;
* `auth.SettingsString(key)` — the read half, used by `runLiveTUI` to seed
  `v135.statusLine` / `v135.title` from disk at startup.

The rewrite keeps the reference's formatting: 2-space indent, HTML escaping
(`\u003c`), and alphabetical keys — what `json.MarshalIndent` on a map produces,
matching the live file.

## 5. Files changed

| File | Change |
|---|---|
| `cmd/antigravity/slash_actions.go` | `plannerResponses`, `resolveCopy`, `statuslineFeedback`, `titleFeedback`, `conversationTitle`; `lastResponseText` rebuilt on top |
| `cmd/antigravity/slash_commands.go` | `/copy` rewired; `/statusline` and `/title` implemented |
| `cmd/antigravity/tui_v135.go` | `statusLine` / `title` fields; status row hidden when off; `View().WindowTitle`; seeded in `runLiveTUI` |
| `cmd/antigravity/slash_actions_test.go` | 8 new/updated tests |
| `internal/auth/v135_compat.go` | `updateSettings`, `SetSettingsString`, `SettingsString`; `AddTrustedWorkspace` refactored |

## 6. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean on every changed
  file except the pre-existing header-comment misalignment in
  `internal/auth/v135_compat.go` (unclean at `HEAD` before this cycle too).
* `go test ./cmd/... ./internal/...` — all packages pass.
* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both diffs documented in cycle 101).
* Live pty probe (throwaway `ANTIGRAVITY_HOME`):

  ```
  > /statusline
  Statusline off.
  ```
  the status row disappears, and `settings.json` gains `"statusLine": "off"`;

  ```
  > /title on
  Custom title enabled. (agy)
  ```
  and `settings.json` gains `"title": "on"` — with `colorScheme` and
  `trustedWorkspaces` preserved.

> An intermediate harness136 run reported 85/87 because the **reference** timed
> out under its exhausted quota; a re-run returned 86/87. As in cycle 102, treat
> single-run swings on the `124/…` rows as harness noise.

## 7. Divergences and limitations

* **`/copy n`'s success filler is 【推测】.** Only the default form's
  `last response` is recovered; `Copied response n to clipboard.` is the clone's
  wording.
* **`/copy` with a non-numeric argument falls back to the default form.** The
  reference's behaviour for e.g. `/copy zzz` is unrecovered.
* **`/copy btw` can never succeed.** The clone has no `/btw`, so it always
  answers `No /btw response to copy.`
* **`statusLine` / `title` persisted values are 【推测】** — see §2 and §3.
* **`/title`'s `%s` is 【推测】** — see §3.
* **No live reference comparison.** The reference account's quota is exhausted,
  so none of the three commands could be driven in the reference TUI; the
  strings, the command descriptions and the `/config` output are the evidence.
* **Still cycle-77 stubs (~14):** `/diff`, `/mcp`, `/tasks`, `/keybindings`,
  `/voice`, `/remote-control`, `/resume`, `/rewind`, `/fork`, `/rename`,
  `/feedback`, `/artifact`, `/btw`, `/codesearch`, `/open`, `/plugin`, `/context`.
  `/mcp` (`No MCP servers configured.`) and `/tasks` (`%d running, %d done`) have
  recovered strings and are the cheapest next targets.
