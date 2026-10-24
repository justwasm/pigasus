# Prompt-history paging and the composer overlays — deliverable

**Cycle:** `cycle-295-history-recall-popup`
**Subject:** ↑/↓ stopped paging the prompt history at the first slash
command in the list (`/accounts`, `/agents`, …)
**Evidence:** every measurement below was taken against **agy 1.4.7**
(`~/.local/bin/agy`) on 2026-10-25, driven through tmux at 220×40, and is
reproducible on this machine

---

## 0. Summary

The report was:

> 每次 pigasus 历史翻页到 /accounts 这种命令的时候，翻页就会失效，我的理解是
> 它的按键输入被类似 /model 的 TUI 捕获了，因此我希望你在 /model 后面有空格
> 的时候才激活捕获，否则单纯历史中出现 /slash_command 之类不要影响上下翻页
>
> ("Every time pigasus pages the history to a command like /accounts, the
> paging stops working. My reading is that its key input is captured by a TUI
> like /model's, so I want the capture to be armed only when there is a space
> after /model; otherwise a mere /slash_command in the history must not affect
> up/down paging.")

The reading was right about the mechanism and the fix landed on the right
axis, with one correction: the reference does **not** arm the popup on
`/model ` alone. It arms the composer overlays from the **typed text**, and a
history recall is not typing — so the reference never opens the popup for a
recalled entry at all.

The clone re-derived both composer overlays (the `/` command popup and the
`@` file-mention dropdown) from the recalled text. The popup therefore became
visible on the recall, and `handleKeyUp` / `handleKeyDown` hand the arrow keys
to a visible popup **before** the history — so the next ↑ moved the popup's
highlight instead of paging, and the history was stuck at the first slash
command in the list.

A second, unrelated defect surfaced while measuring: the clone's **test suite
was writing into the user's real agy home**, so every `go test` run injected
fixture strings into the very `history.jsonl` the reference pages through. That
is fixed too (§5).

---

## 1. What the reference actually does — 【一手】

### 1.1 The reference's history *contains* slash commands

`~/.gemini/antigravity-cli/history.jsonl` stores the prompt history, and a
slash command is stored with an explicit type:

```json
{"display":"/agents","timestamp":1792867887627,"workspace":"/Users/gear","type":"slash_command"}
{"display":"/model MiniMax M3.1 Flash","timestamp":1792766423799,"workspace":"/Users/gear/GitHub/openagy","type":"slash_command"}
```

(The clone writes the same shape — `internal/convstore/write.go` sets
`Type = "slash_command"` for a display that starts with `/`.)

So paging up the history **lands on** a buffer that starts with `/`.

### 1.2 …and the popup stays closed for it

Measured live, in a fresh `agy` in `/Users/gear`, with the history's newest
entry being the `/agents` slash command:

| keystroke | the composer | the command popup |
|---|---|---|
| `Up` | `/agents` | **closed** |
| `Up` | `真不错!` (the next-older entry) | **closed** |
| `Up` | `h` | **closed** |
| `Down` | `/agents` again | **closed** |

The capture is unambiguous — with the popup closed the composer is the only
thing on screen:

```
────────────────────────────────────────────────────────────
> /agents
────────────────────────────────────────────────────────────
```

and the next `Up` walks on to the next-older entry. **The reference pages the
history straight across slash commands.**

### 1.3 The popup is re-derived by the *next keystroke*

Recalling `/agents` and then typing one character opens the popup again:

| keystroke | the composer | the command popup |
|---|---|---|
| `Up` | `/agents` | closed |
| `Up`, then `x` | `/agentsx` | **open** — the `No matches` row |

So the reference's rule is: **the overlays are a function of the typed text**,
not of the buffer content. A recall replaces the buffer without touching the
overlay state; the next inserted character re-derives it.

Two more measurements that pin the rule down:

* With the popup open but empty (`No matches`), `Up` does **not** page the
  history — the popup swallows the key. The clone already behaves this way.
* `Enter` on a recalled `/agents` **runs the command** (the reference opens its
  "Create New Agents / Available Agents" panel). A recalled slash command
  behaves exactly like a typed one — because, as far as the reference is
  concerned, nothing about the buffer changed.

### 1.4 The `@` file-mention dropdown obeys the same rule

Probed with a synthetic history entry (`{"display":"@zzzprobe", …}` appended to
the real `history.jsonl`, removed again afterwards):

| keystroke | the composer | the `@` dropdown |
|---|---|---|
| `Up` | `@zzzprobe` | **closed** |
| `Up` | `/agents` | **closed** |
| `Down` | `@zzzprobe` | **closed** |

---

## 2. The clone's bug

### 2.1 The recall armed the popup

`internal/cliapp/history.go` (before this cycle):

```go
func (m *runMode) historyUp() {
	…
	m.historyIdx++
	m.applyHistoryEntry(m.history[len(m.history)-m.historyIdx])
	m.picker.Update([]rune(firstLine(m.input.Value())))   // ← arms the popup
	m.atFileDropdown.Update(m.input.Value(), m.cwd)        // ← arms the dropdown
}
```

`PickerState.Update` derives the popup from the buffer: a buffer that starts
with `/` makes it **visible** (`internal/tuilib/picker.go`). A recalled
`/accounts` therefore opened the command popup.

### 2.2 …and the popup took the arrow keys

`internal/cliapp/tui_update_keys_edit.go`:

```go
func (m *runMode) handleKeyUp() (tea.Model, tea.Cmd, bool) {
	if m.picker.Visible() {
		m.picker.MoveUp()
		return m, nil, true          // ← the history is never reached
	}
	…
	if isOnFirstLine(m.input.Value(), inputCursorByteOffset(m.input)) {
		m.historyUp()
		return m, nil, true
	}
	return m, nil, false
}
```

The popup is consulted first, so once it was visible the history stopped
paging — the symptom the user reported.

The bug is a *single* mechanism with two entry points: the same `historyUp` /
`historyDown` pair arms the popup and the dropdown, and both overlays are
consulted before the history in `handleKeyUp` / `handleKeyDown`.

---

## 3. The change

| file | change |
|---|---|
| `internal/cliapp/history.go` | `historyUp` / `historyDown` no longer re-derive the overlays; they call the new `closeComposerOverlays()` (closes the command popup and the `@` dropdown) |
| `internal/cliapp/tui_history_popup_test.go` | new: the four regression tests of §4 |
| `internal/cliapp/main_test.go` | new: the package's `TestMain` — the test suite may no longer write into the user's real agy home (§5) |

```go
// closeComposerOverlays hides the composer overlays after a history recall.
func (m *runMode) closeComposerOverlays() {
	m.picker.Reset()
	m.atFileDropdown.Reset()
}
```

The overlays are re-derived by the next *typed* keystroke, exactly as in the
reference: `updateTextareaAndDropdown` calls `m.picker.Update(firstLine(value))`
and `m.atFileDropdown.Update(value, cwd)` on every key press, and that path is
untouched.

### 3.1 Why `Reset()` and not "leave the state alone"

The overlays can only be *reached* through `historyUp` when they are hidden —
`handleKeyUp` returns early on a visible popup — so their state is already
reset at that point. `Reset()` says so explicitly and keeps the invariant
local: **a recall never leaves an overlay open**.

### 3.2 What did **not** change

* A popup the user opened by typing still owns the arrow keys. Typing `/`
  opens the command popup, and `Up` / `Down` move its highlight — that is the
  reference's behaviour and the clone's existing test
  (`TestPickerVisible_OverridesUpHistory`) still pins it.
* `Enter` on a recalled slash command runs the command: the popup is closed,
  so `resolveSubmittedPrompt` hands the text to `dispatchSlashLine` — the same
  path a typed command takes (§1.3).
* The `/model <query>` **argument** popup is untouched. It is a different
  widget: the reference enters that mode when the command is followed by a
  space (`internal/tuilib/picker.go`, `modelQuery`), and the clone mirrors it.

### 3.3 One clone-side behaviour that this cycle reverts

The `@` dropdown's derivation from a recalled buffer came from the
`feature/at-file-dropdown-newline` cycle ("a recalled history entry containing
`@src` mid-line still triggers the dropdown"). That was a clone-side
assumption: the reference does **not** open the dropdown for a recalled entry
(§1.4), it opens it on the next typed character. The behaviour is therefore
gone — the parser still understands a mention *anywhere* in the buffer (that
part of the cycle was about the parser, not about the recall), but a recall
does not arm the dropdown any more.


---

## 4. Verification

### 4.1 The regression tests

`internal/cliapp/tui_history_popup_test.go`:

| test | pins |
|---|---|
| `TestHistoryUp_SlashCommandEntryKeepsTheCommandPopupClosed` | a recalled `/accounts` leaves the popup closed, and the next `Up` pages the history |
| `TestHistoryDown_SlashCommandEntryKeepsTheCommandPopupClosed` | the same in the other direction |
| `TestHistoryUp_AtMentionEntryKeepsTheFileDropdownClosed` | a recalled `@` mention leaves the dropdown closed |
| `TestHistoryUp_TypedKeyAfterRecallArmsTheCommandPopup` | the next *typed* character re-derives the popup (the reference's rule) |

All four **fail** on the pre-fix tree and pass after it:

```
$ git stash push -- internal/cliapp/history.go
$ go test ./internal/cliapp/ -run 'TestHistory(Up|Down)_'
--- FAIL: TestHistoryUp_SlashCommandEntryKeepsTheCommandPopupClosed
    tui_history_popup_test.go:57: the recalled slash command armed the command popup; it must stay closed
    tui_history_popup_test.go:64: Up #2: input = "/accounts", want "older prompt" — the history must page past a slash command
--- FAIL: TestHistoryDown_SlashCommandEntryKeepsTheCommandPopupClosed
--- FAIL: TestHistoryUp_AtMentionEntryKeepsTheFileDropdownClosed
--- FAIL: TestHistoryUp_TypedKeyAfterRecallArmsTheCommandPopup
```

### 4.2 The whole tree

```
go build ./cmd/... ./internal/...     # exit 0
go vet   ./cmd/... ./internal/...     # no findings
go test  ./cmd/... ./internal/...     # all packages ok
```

---

## 5. The opportunistic fix: the test suite was polluting the user's history

### 5.1 The measurement — 【一手】

`internal/cliapp`'s fixtures wrote to the **real** `~/.gemini/antigravity-cli`:
`runModeForTest()` and a handful of tests call `NewRunMode(...)` without
pointing `$ANTIGRAVITY_HOME` at a temp dir, and `pushHistory` appends to
`history.jsonl` on every submit.

One `go test ./cmd/... ./internal/...` run appended **15** entries to the real
file:

```
{"display":"first prompt","workspace":"/", …}
{"display":"older prompt","workspace":"/", …}
{"display":"would be recalled if picker were hidden","workspace":"/", …}
{"display":"must not be recalled","workspace":"/", …}
{"display":"hi","workspace":"/", …}
{"display":"yo","workspace":"/", …}
{"display":"h","workspace":"/", …}
{"display":"/changelog","workspace":"/","type":"slash_command", …}
```

The whole `history.jsonl` held **2000** entries, of which **1723** were
fixtures. The reference binary reads the same file — which is why the
reference's own history recall walked through `newer prompt`,
`would be recalled if picker were hidden` and `must not be recalled` during
the measurements of §1.

An instrumented `convstore.AppendHistoryEntry` (a stack trace per write,
temporarily, reverted afterwards) put the number at **8 writes to the real
home** and **0 writes to the real home from any other package** — the whole
problem is `internal/cliapp`'s fixtures.

### 5.2 The fix

`internal/cliapp/main_test.go` — a package `TestMain` that points
`$ANTIGRAVITY_HOME` at a scratch tree:

```
<tmp>/.gemini/antigravity-cli          ← the AGY home (writes land here)
<tmp>/.gemini/skills                   → symlink to the real ~/.gemini/skills
<tmp>/.gemini/antigravity-cli/builtin  → symlink to the real builtin bundle
```

The scratch tree mirrors the shape of the real one because the code walks the
*parent* of the AGY home (`filepath.Dir(AntigravityHome())` is `~/.gemini`) to
find `skills/` and the plugin roots. The two read-only roots are linked in, so
the `/skills` catalogue — and `TestPanelSkillsDetailView`, which asserts on
it — is unchanged; everything the tests *write* lands in the scratch tree.

Tests that need their own home still call `t.Setenv("ANTIGRAVITY_HOME", …)`
and are unaffected: `t.Setenv` restores the value `TestMain` installed.

### 5.3 The measurement after the fix

```
$ go test -count=1 ./cmd/... ./internal/...
$ diff history.jsonl.before history.jsonl.after   # → identical
$ md5 settings.json                               # → unchanged
```

Zero writes to the real home, and the real `history.jsonl` and
`settings.json` are byte-identical before and after a full test run.

---

## 6. 硬约束 — hard constraints

* **A history recall must never leave a composer overlay open.** The overlays
  are armed by the *typed* text; `historyUp` / `historyDown` close them
  (`closeComposerOverlays`). Re-deriving them from the recalled buffer is the
  bug this cycle fixed — do not "helpfully" derive them again.
* **The reference's rule is about typing, not about `/model `.** The
  `/model <query>` argument popup is a *different* widget from the command
  popup; the command popup opens on a bare `/` and the reference's own
  captures show it (`/a` → `/add-dir` highlighted). Do not gate the command
  popup on a trailing space.
* **The popup still owns the arrow keys while it is open** — including the
  empty (`No matches`) case, where the reference swallows the key rather than
  paging the history. `handleKeyUp` / `handleKeyDown` must keep consulting the
  overlays first.
* **The test suite must not write into the user's real agy home.** The
  `internal/cliapp` `TestMain` owns that guarantee for the package; a new
  package that writes to `$ANTIGRAVITY_HOME` needs the same treatment.
  `history.jsonl` and `settings.json` are shared with the **reference
  binary** — junk written there is junk the user pages through in `agy`.
* **The clone's `history.jsonl` stays byte-compatible with the reference's**,
  slash commands included (`"type":"slash_command"`). The reference's history
  recall walks those entries, so the clone must keep producing them.

## 7. 存疑 — open questions

* **Why does the reference's history navigation die after the user edits a
  recalled entry?** Measured: recall `/agents`, type `x`, and then `Up` and
  `Down` both do nothing — the reference's arrow navigation stays suspended
  until the buffer is cleared. The clone does not reproduce that (its `Up` /
  `Down` keep paging after an edit). The reference's rule for it was not
  identified, so the clone's behaviour here is unchanged and unpinned.
* **Is the reference's overlay state a *derived* view that is simply not
  recomputed on a programmatic replacement, or an explicit "armed" flag?** The
  observable behaviour is the same in both readings, and both fit the
  measurements of §1. The clone implements the observable rule.
* **The reference's `@` dropdown was probed with a synthetic history entry**
  (an entry containing `@zzzprobe`), because the real history held no `@`
  mention. A mention in the middle of a word (`openagy@users.noreply…`) was
  not probed — the clone's parser does not treat it as a mention either.
* **The reference's `/model ` argument popup was not re-measured** in this
  cycle; the clone's existing behaviour (the popup opens on `/model ` + a
  space, the filter is a case-insensitive substring of the label or the id)
  was left untouched.

## 8. Limitations

* **One reference version was measured**: agy 1.4.7. The overlay behaviour was
  not bisected across releases; an older release may arm its popup from the
  buffer content instead.
* **The reference was measured through tmux**, not by a human reading the
  screen: the evidence is the captured pane (the composer line and the
  presence or absence of the popup rows), not a rendered image.
* **The user's `~/.gemini/antigravity-cli/history.jsonl` was modified during
  the probes** (a synthetic `@zzzprobe` entry was appended and a synthetic
  `/agents` entry was appended for the `Enter` measurement). Both were
  removed by restoring the pre-probe copy; the file is back to its pre-probe
  state. The *pre-existing* fixture pollution (1723 entries, §5.1) was left
  alone — it is the user's data.
* **The clone's fix was verified by unit tests, not by driving the clone's
  TUI through a pty.** The reference's side of the comparison is a live
  capture; the clone's side is the state machine plus the four regression
  tests.
* **Print mode is unaffected** — the overlays are a TUI surface.
