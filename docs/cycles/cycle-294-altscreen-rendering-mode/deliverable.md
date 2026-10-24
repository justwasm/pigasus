# Rendering mode — alternate screen vs inline — deliverable

**Cycle:** `cycle-294-altscreen-rendering-mode`
**Subject:** the `/config` "Rendering Mode" row, the startup decision
`resolveAltScreen`, and the two stale UI strings that advertised a
Ctrl+L toggle
**Evidence:** every measurement below was taken against **agy 1.4.7**
(`~/.local/bin/agy`) on 2026-10-25 and is reproducible on this machine

---

## 0. Summary

The report was:

> 怎么你的 altscreen mode 和 inline mode 没有区别，我切换成 inline mode 可是
> UI 还是以 altscreen 模式显示的
>
> ("Why is there no difference between your altscreen mode and inline mode?
> I switched to inline mode and the UI still renders in altscreen mode.")

The complaint was correct, and the cause was **not** one bug but three
independent ones stacked on top of each other:

1. **The setting was never read.** `/config`'s "Rendering Mode" row wrote
   `altScreenMode` into `settings.json`, and *nothing in the clone ever read
   it back*. `resolveAltScreen` looked at the clone's own `--alt-screen` /
   `--no-alt-screen` flags and at nothing else. The row was decorative.
2. **The inline mode was inexpressible.** Even if the setting had been read,
   the clone's inline stop **deleted the key** (`V("native terminal
   (inline)", "")` stores `nil`), and an absent `altScreenMode` means *the
   alternate screen*. The clone therefore could not write "inline" at all.
3. **The row did not apply live.** The reference switches the running session
   in the same keystroke; the clone only wrote the file.

Two further strings were wrong and are now fixed: the `/keybindings` panel and
the `--alt-screen` usage both advertised a **Ctrl+L toggle that the reference
does not have** — the reference's Ctrl+L *clears the screen*.

---

## 1. What the reference actually does

### 1.1 The vocabulary — 【一手】

`agy -p /config` prints the effective value of every setting, and it
**rejects** a value it does not know — that one key then falls back to its
default. Each candidate was written into `~/.gemini/antigravity-cli/settings.json`
and the reference was run:

| written value | `agy -p /config` prints | accepted? |
|---|---|---|
| `"always"` | `always` | yes |
| `"never"` | `never` | **yes** |
| `"off"` | `default` | no — rejected |
| `"inline"` | `default` | no — rejected |
| `"default"` | `default` | no — rejected |
| `"auto"` | `default` | no — rejected |
| `"none"` | `default` | no — rejected |
| `"false"` | `default` | no — rejected |

So the setting has exactly **two** meaningful values: `"always"` and
`"never"`.

A rejected value is **per key**, not per file: with `altScreenMode: "off"`
the dump still shows `agentMode auto`, `colorScheme light` and the rest of
the profile, and only `altScreenMode` reads `default`. (The whole-file
rejection the catalogue documents for `agentMode: "default"` in 1.4.0 does
not happen here.)

### 1.2 What each value *renders* — 【一手】

The reference is a TUI, so the mode shows up in the terminal bytes: the
alternate screen is entered with `ESC[?1049h`. Each state was run under a pty
and the raw bytes counted:

```
$ timeout 12 script -q /dev/null ~/.local/bin/agy > raw.txt
$ grep -c $'\x1b\[?1049h' raw.txt
```

| `altScreenMode` | `?1049h` | rendering |
|---|---|---|
| `"always"` | 1 | alternate screen |
| `"never"` | 0 | **inline** |
| absent | 1 | alternate screen — **the default** |

Reproduced twice each, identical counts. Two side findings from the same
capture: the reference does **not** write its effective mode back to
`settings.json` (the `altScreenMode` key was unchanged after every run), and
the reference's settings validation is what turns `"off"`/`"inline"`/… into
`default`.

### 1.3 The reference's own panel — 【一手】

The reference's settings panel is reached with `/` → `config` → Enter (the
picker's query does **not** include the leading slash: typing `/config`
literally yields "No matches"). Driving the panel with `Rendering` in the
search box and then ←:

```
before:  altScreenMode = "always"     panel: "Rendering Mode  no flickering (altscreen)"
after ←:  altScreenMode = <ABSENT>    panel: "Rendering Mode  native terminal (inline)"
```

Two facts fall out of this:

* The reference **applies the change live** — the capture shows `ESC[?1049l`
  (leaving the alternate buffer) *after* the switch, in the same session.
* The reference writes an **empty** value for the inline stop, which its
  `json:"altScreenMode,omitempty"` writer drops from the file. The key is
  deleted.

The same is true of the reference's **submenu** path — `enter` on a row
opens an option list (`enter Save  esc Cancel`, the current value marked
`(current)`), and saving "native terminal (inline)" from there deletes the
key as well:

```
after the submenu Save:  altScreenMode = <ABSENT>
terminal bytes:           ESC[?1049l   (the session left the alternate buffer)
```

So both of the reference's paths apply the switch live and persist it
lossily.

### 1.4 …and the reference's inline option does not survive a restart — 【一手】

That last bullet is a **bug in the reference**, and it is worth measuring
rather than assuming. After the panel switch above (key deleted), the
reference was started again with the same file:

```
fresh start: ?1049h=1   ← back in the alternate screen
```

The reference's "native terminal (inline)" option works *for the rest of the
session* and is then forgotten, because the empty value it writes is the same
value its own reader treats as the default — and the default is the alternate
screen.

---

## 2. The clone's three bugs

### 2.1 The setting was never read

`internal/cliapp/tui_entry.go`:

```go
func resolveAltScreen(fs *flag.FlagSet) bool {
	if flagBoolValue(fs, "no-alt-screen") || !flagBoolValue(fs, "alt-screen") {
		return false
	}
	return true
}
```

The only inputs were the clone's own flags. `altScreenMode` appeared in
exactly three places in the tree — the catalogue, the print-mode dump and the
tests — and in none of them on the *read* path of a running session.

### 2.2 The inline mode was inexpressible

`internal/configpanel/configpanel.go`:

```go
{Name: "Rendering Mode", SettingsKey: "altScreenMode",
	Values: []Val{
		V("native terminal (inline)", ""), V("no flickering (altscreen)", "always"),
	},
```

`V(display, "")` stores `nil`, and `nil` means *delete the key* — the
catalogue's absence convention. So the clone's inline stop deleted
`altScreenMode`, which means "the default", which is the alternate screen.

### 2.3 The row did not apply live

`internal/cliapp/slash_config_preview.go`'s `applyConfigValue` had exactly one
live hook, `applyThemeLive` for the "Color Scheme" row. The Rendering Mode row
had none, so even a correctly persisted value would have changed nothing
until the next start — and the next start did not read the value either.

### 2.4 Two stale strings

* `internal/cliapp/slash_helpers.go` — the `/keybindings` panel advertised
  `Ctrl+L  Toggle alternate screen`.
* `internal/cli/parser_flags.go` — `--alt-screen` said "toggle at runtime via
  Ctrl+L", `--no-alt-screen` said "runtime toggle still works".

The reference has **no** such toggle: its string table has "Clear CLI
screen" and the command id `cli.clear_screen`, and the 1.4.7 changelog says
"Fixed full-screen blank flashes in inline mode … when clearing the screen
with `Ctrl+L`". The clone's Ctrl+L has cleared the screen since `c6793ef2`,
which is the correct behaviour — the two strings were the stale part.

---

## 3. The changes

| file | change |
|---|---|
| `internal/configpanel/configpanel.go` | the Rendering Mode row's values are now `V("no flickering (altscreen)", "always")` and `V("native terminal (inline)", "never")` — the alternate screen **first** |
| `internal/cliapp/tui_entry.go` | `resolveAltScreen` reads the setting (flags → setting → default); new helpers `altScreenModeValue` / `altScreenModeFromSettings` |
| `internal/cliapp/slash_config_preview.go` | `applyConfigValue` calls the new `applyAltScreenLive`, so the row switches the running session |
| `internal/cliapp/slash_helpers.go` | `/keybindings` says `Ctrl+L  Clear CLI screen` |
| `internal/cli/parser_flags.go` | the two flag usage strings no longer mention a Ctrl+L toggle |
| `internal/cliapp/tui_altscreen_test.go` | new: the precedence table, the live switch, the default state, the value mapping |
| `internal/cliapp/model_test.go` | `TestCycle197_ConfigRenderingModeWrap` now expects `"never"` instead of a deleted key |
| `internal/configpanel/configpanel_test.go` | `TestDefaultIsAbsent` exempts the Rendering Mode row and pins its two forms |

### 3.1 Why the row's order changed

This is the heart of the user's report: **the panel said "inline" while the
session rendered the alternate screen.** The reference's row lists the inline
stop first, and its display falls back to the first entry when the key is
absent — but an absent key means the *alternate screen* (§1.2). So the
default state is labelled "native terminal (inline)" while the CLI is in
the alternate screen, and ← / → from that state walk to the *same* next
entry in both directions ("always"), so neither key changes anything.

The catalogue's convention is "the first entry is the default state". For
this row the default state is the alternate screen — but its stored form
cannot be `nil`, because `nil` is what the inline stop would have to write
and the reference reads an absent key as the alternate screen.

Putting the alternate screen first fixes both halves: the row now *displays*
the mode the session is actually in, and ← / → reach the inline stop from
the default state.

This is the clone's deliberate deviation from the reference's row order. The
values themselves are the reference's own two words; what changes is which
one comes first — and therefore what the row *displays* for a key that was
never written, and which stop ← and → reach.

### 3.2 Why the clone writes `"never"` and not an empty value

`"never"` is the reference's own inline value: agy 1.4.7 accepts it (no
settings error), prints it, and renders inline with it. So the clone's
`settings.json` stays **valid for the reference** — the file the reference
reads says "inline", and the reference honours it.

Writing an empty value instead (what the reference's panel does) would keep
the file byte-identical to the reference's, but would inherit the reference's
bug: the choice would be forgotten at the next start. The clone's own default
is unchanged — an absent `altScreenMode` still means the alternate screen,
exactly as in the reference.

---

## 4. Verification

### 4.1 Unit tests

```
go build ./cmd/... ./internal/...          # exit 0
go vet   ./cmd/... ./internal/...          # no findings
go test  ./cmd/... ./internal/...          # all packages ok
```

### 4.2 The clone against the same pty probe

The clone was run under the same pty capture as the reference, with an
isolated `ANTIGRAVITY_HOME`:

| `altScreenMode` | reference | clone before | clone after |
|---|---|---|---|
| `"always"` | alternate screen | alternate screen | alternate screen |
| `"never"` | **inline** | alternate screen ✗ | **inline** ✓ |
| absent | alternate screen | alternate screen | alternate screen |

### 4.3 The live switch, end to end

The clone's own `/config` panel was driven with the same keystrokes as the
reference's (`/`, `config`, Enter, `Rendering`, ←):

```
settings.json after:  { "altScreenMode": "never" }
terminal bytes:       ESC[?1049h (entered) … ESC[?1049l (left)
panel row after:      Rendering Mode  native terminal (inline)
```

The session left the alternate buffer in the same keystroke — the same
sequence the reference produces.

---

## 5. 硬约束 — hard constraints

* **`altScreenMode` has exactly two values.** `"never"` is inline; everything
  else — `"always"`, an absent key, and any value the reference rejects — is
  the alternate screen. Do not invent a third one: the reference's settings
  validation silently turns an unknown value into `default`, so the setting
  would read as "the alternate screen" without a word of warning.
* **The clone's `settings.json` must stay valid for the reference.** Writing
  `"never"` is safe (agy 1.4.7 accepts and honours it); writing an empty
  value is safe but lossy; writing `"off"`/`"inline"`/`"auto"` is **not** —
  the reference silently reads that key as `default`, i.e. the alternate
  screen, so the setting would look saved and do nothing.
* **The row's order is load-bearing.** The alternate screen must stay the
  first entry: an absent key means the alternate screen, and a two-entry row
  walks to the same next entry in both directions, so the inline stop is
  unreachable from any other order.
* **The live switch must go through `m.altScreen`.** bubbletea v2 has no
  `tea.WithAltScreen`; the renderer reads `v.AltScreen` off the frame and
  enters/leaves the alternate buffer when the flag flips. `applyTerminalSettings`
  sets it on every frame — a view that forgets it drops the session out of
  the alternate screen mid-session.
* **Do not re-introduce a Ctrl+L toggle.** The reference's Ctrl+L clears the
  screen. The clone's `/keybindings` panel and flag usage now say so.
* **`resolveAltScreen` is shared by the session and the login gate.** Both
  must agree, or the terminal ends up in a mode the user did not ask for.

## 6. 存疑 — open questions

* **Is `"never"` the reference's *intended* inline value, or a legacy
  leftover?** It is accepted and honoured by 1.4.7, and the reference's own
  panel never writes it — so the reference's writer and reader disagree. The
  clone follows the reader (the behaviour that is observable) rather than the
  writer (the behaviour that is lost). If a later reference release makes the
  panel write `"never"` too, the clone's file becomes byte-identical to the
  reference's without any further change.
* **The reference's "default" for an absent key was not read from the
  binary's code**, only from its observable behaviour (the pty capture). The
  clone's rule — absent means the alternate screen — matches that behaviour,
  and is consistent with the `altScreenMode: "always"` in the profile that
  was measured, but the internal enum that produces the printed `default` is
  still unknown.
* **The reference's panel was driven blind**, through a pty, by typing. The
  values read back from `settings.json` and the terminal byte counts are
  direct measurements; the *panel layout* around them was not compared
  frame by frame with the clone.

## 7. Limitations

* **One reference version was measured**: agy 1.4.7. An older release may not
  know `"never"` at all — the value was not bisected across releases, and the
  1.3.x string table in this repository does not settle it.
* **The reference was measured on one profile** (the user's own
  `~/.gemini/antigravity-cli/settings.json`, with `altScreenMode: "always"`).
  The profile was backed up and restored after every experiment; the final
  state is byte-identical to the backup.
* **The clone's live switch was verified by terminal bytes, not by a human
  looking at the screen.** The `ESC[?1049l` sequence is the renderer leaving
  the alternate buffer, which is what "the UI is now inline" means at the
  terminal level, but no screenshot-equivalent diff was produced.
* **The clone's inline layout was not compared with the reference's** beyond
  the mode switch itself. The reference's inline rendering is a different
  surface with its own open questions (the "blank flash" fixes in its 1.3.x
  changelog).
* **Print mode is unaffected** — `agy -p /config` dumps the raw setting and
  never renders a TUI.
