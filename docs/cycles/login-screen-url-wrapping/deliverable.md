# The login screen: the OAuth URL is no longer truncated

## Scope

The login screen cut the authorization URL off at the terminal width. On a
phone-sized terminal the visible link stopped after a few dozen characters
and the rest was gone — the link could not be copied, and the code prompt
below it was pushed off the screen entirely.

This cycle rebuilt the screen's layout: every line is wrapped to the
terminal width, the URL is chunked the way the reference chunks it (each
chunk a hyperlink to the *complete* URL), and the screen became scrollable
so the code prompt is reachable on a short terminal.

Two corrections followed in the same cycle:

- the **"click here" block** the reference shows under the URL was
  restored (the first draft of this report claimed the block was gone —
  it was wrong, see [The click-here block](#the-click-here-block));
- the **alternate screen** the session runs in is now respected by the
  login screen, both inside the TUI (`/accounts` → `a`) and at start-up.

**`internal/loginui` and `internal/cliapp` were touched.** No prompt, no
transport, no tool surface, no configuration file.

## The bug

The renderer draws the view into a cell buffer the size of the terminal
(`charm.land/bubbletea/v2`'s `cursedRenderer` → `content.Draw(s.cellbuf,
s.cellbuf.Bounds())`), so a line longer than the terminal is **clipped, not
soft wrapped**. The clone wrote the URL on a single line:

```
 https://accounts.google.com/o/oauth2/auth?access_type=offline&client_id=1071006
```

That is 80 cells of a ~700 character URL. Everything past the terminal's
right edge was dropped on the floor, silently, with no error.

The same clipping hit every other long line of the screen — the tail of
`Your browser should open automatically. If not:`, the tail of
`If you aren't automatically redirected, paste the authorization code below:`,
the horizontal rules, the footer.

## What the reference does

【一手】 The reference binary (`~/.local/bin/agy`) was run on this machine
under `tmux` at 40×30, 80×30 and 100×30, with a throwaway `HOME`
(`/tmp/agy-empty-home`) and a fake `open` on the `PATH` so the browser did
not launch. Each pane was captured with `tmux capture-pane -p` and measured
with Python (character counts, not byte counts).

The reference **wraps** the URL instead of clipping it, and every chunk is
its own OSC 8 hyperlink whose target is the complete URL:

```
 8 ' Your browser should open automatically.'
 9 ' If not:'
11 ' https://accounts.google.com/o/oauth2/a'
12 ' uth?access_type=offline&client_id=1071'
13 ' 006060591-tmhssin2h21lcre235vtolojh4g4'
...
27 '  (1–26 of 35 lines)'
29 '  shift+up/down Navigate'
```

The geometry, measured:

| element | reference | rule |
|---|---|---|
| prose | 1 column of indent, content wrapped at *width*−1 | the indent is repeated on the continuation lines |
| URL | 1 column of indent, *width*−2 characters per chunk | every chunk is *width*−1 cells wide |
| line counter | `  (1–26 of 35 lines)` | two columns of indent, an en dash |
| footer | `  shift+up/down Navigate` | the viewport's own key hint |

Two more findings from the same captures:

- **The screen is a viewport.** At 40 columns the reference's content is 35
  lines, the screen shows 26 of them, and the rest is reachable with
  shift+up / shift+down.
- **The start-up login screen runs in the alternate buffer.** `tmux
  display-message -p -t <session> '#{alternate_on}'` reports `1` for the
  reference's sign-in screen — the reference has no `--no-alt-screen`
  flag, so the alternate buffer is the only mode it has.

## The click-here block

The first draft of this report claimed that the block was gone from the
reference and that the per-chunk hyperlinks had replaced it:

> ~~**The "click here" block is gone in 1.4.6.** The clone carried
> `Copy and paste the URL or click on the link below:`, two rules and a
> `→ Click here to authenticate` link. The reference prints none of it.~~

**That claim was wrong** and is withdrawn. The user's reference screen
carries the block, and the block is restored in the clone:

```
 Copy and paste the URL or click on the link below:
 ────────────────────────────────────────────────────────────────────────────
 → Click here to authenticate
 ────────────────────────────────────────────────────────────────────────────
```

【一手】 What is measured from the user's screen (the reference's own
output, ≈120 columns):

| element | rule |
|---|---|
| the hint line | 1 column of indent, the same prose wrapping as the other text |
| the rules | 1 column of indent, spanning the terminal's full width |
| the link | 1 column of indent, `→ Click here to authenticate` |

【一手】 The link is an **OSC 8 hyperlink to the complete URL** — the same
target the URL chunks carry — so a cmd+click on either one opens the whole
URL. The clone emits
` \x1b]8;;<full url>\x07\x1b[34;4m→ Click here to authenticate\x1b[m\x1b]8;;\x07`.

【推测】 **The trigger for the block is still unresolved.** Re-measured
against the live binary (1.4.7) at 100×30 with a fake `open` exiting 1, no
block appeared at t=15 s, 40 s or 70 s; with the browser really opening
(the user's run) it does. The block is therefore restored unconditionally,
which is also what the clone's own screen did before this cycle.

The label is **clipped, never wrapped**: a wrapped label would split the
OSC 8 target away from the text it belongs to, and the link would leak
into the next line. The rules are drawn to the terminal's width — the
clone's original screen used a fixed 78-column rule.

## The alternate screen

【一手】 `/accounts` → `a` (add an account) opened the sign-in modal
*inside* the running TUI, and the terminal **left the alternate buffer**:

```
tmux display-message -p -t <session> '#{alternate_on}'
start of the session   1
after /accounts        1
after a (add)         0     ← the bug
```

The cause is not the login flow but the *view*: `runMode.View()` returned
the login model's own `tea.View` untouched, and in bubbletea v2 the
alternate screen is a property of the view (`View.AltScreen`), not of the
program. A view that forgets it makes the renderer leave the alternate
buffer in the middle of the session.

The fix applies the session's terminal settings to **every** view the
program renders:

```go
func (m *runMode) applyTerminalSettings(v tea.View) tea.View {
	v.AltScreen = m.altScreen
	if m.altScreen {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}
```

The start-up gate had the mirror-image defect: it ran the login screen in
its own program, in the *normal* buffer, while the reference runs it in
the alternate one. `loginui.Model` now carries an `AltScreen` field, and
the gate asks for the same setting the session uses — so `--no-alt-screen`
keeps the whole session (gate included) in the normal buffer, and the
default matches the reference.

【一手】 After the fix the session stays in the alternate buffer through
the whole flow:

```
start of the session   1
after /accounts        1
after a (add)         1
```

## The fix

`internal/loginui/wrap.go` (new) holds the layout rules:

- `wrapProse` wraps a line at the terminal width and repeats the line's
  indent on the continuation lines, the way the reference does.
- `urlChunks` hard-wraps the URL at *width*−2 characters. The URL is one
  unbroken run, so the break points are arbitrary character boundaries —
  the reference breaks it the same way (`.../oauth2/a` / `uth?access_...`).

`internal/loginui/view.go` builds the screen as a list of lines that are
already at most the terminal width wide, so the renderer cannot clip any of
them:

- the URL block writes one line per chunk, each wrapped in an OSC 8
  hyperlink pointing at the complete URL:
  ` \x1b]8;;<full url>\x07<chunk>\x1b]8;;\x07`;
- the click-here block: the hint line, a rule across the terminal, the
  OSC 8 link, another rule;
- the screen is rendered into a scrollable window: the content is shown in
  rows, the line counter reports the visible range, and shift+↑/↓ or the
  mouse wheel scroll it;
- the footer lists the key hints that fit the terminal width, the scroll
  hint first when the content overflows.

`internal/loginui/update.go` gained the scroll keys and the wheel handler.

One more defect surfaced while the keys were being handled: the code
prompt's fallback branch accepted any key code `>= ' '`, and the navigation
keys carry a special code (`0x100000` and up). Pressing ↑ or ↓ in the
prompt appended a private-use rune to the authorization code. The fallback
is now limited to printable ASCII, and the navigation keys are consumed
explicitly.

`internal/cliapp/tui_view.go` gained `applyTerminalSettings`, used by both
the main view and the login view; `internal/cliapp/tui_entry.go` gained
`resolveAltScreen`, shared by the session and the start-up gate.

## Result

The clone and the reference agree on the geometry, measured the same
way at 40 columns:

```
reference                                   clone
 8 ' Your browser should open automatically.'   8 ' Your browser should open automatically.'
 9 ' If not:'                                    9 ' If not:'
11 ' https://accounts.google.com/o/oauth2/a'    11 ' https://accounts.google.com/o/oauth2/a'
12 ' uth?access_type=offline&client_id=1071'    12 ' uth?access_type=offline&client_id=1071'
27 '  (1–26 of 35 lines)'                       27 '  (1–26 of 35 lines)'
29 '  shift+up/down Navigate'                   29 '  enter Submit · shift+↑/↓ Scroll'
```

and at 80 columns both produce ten URL chunks of 79 cells each, the same
prose lines, the same ` authorization code...` prompt.

The complete URL is on screen, the chunks are individually tappable (the
mobile path: tap any chunk, the browser opens the whole URL), the
click-here link opens the same URL, and the selection across the chunks
is the whole URL — a browser strips the newlines out of a pasted URL, so
the copy still pastes cleanly.

## Hard constraints

- **The renderer clips; it does not wrap.** Any line the login screen
  writes has to be at most the terminal width wide. The regression test
  `TestLoginView_NoLineExceedsTerminalWidth` enforces it at 20, 40, 60, 80
  and 120 columns.
- **Every chunk of the URL carries the complete URL as its link target.**
  A chunk that links to itself would break the mobile path. The click-here
  link carries the same target.
- **The screen stays scrollable.** The content is taller than a
  phone-sized terminal; the code prompt is at the bottom, so the viewport
  and its keys are load-bearing, not decoration.
- **The clone keeps its own key hints.** The reference's footer is the
  viewport's own help line; the clone's lists the keys it accepts. The
  hints that do not fit the terminal are dropped whole, never clipped.
- **Every view the program renders carries the session's terminal
  settings.** A view that forgets `AltScreen` drops the terminal out of
  the alternate buffer mid-session.

## Open questions

- **Why the reference shows the click-here block is still unknown.** The
  captures against the live 1.4.7 binary never showed it; the user's own
  run does. Until the trigger is identified the clone shows it
  unconditionally.
- **The reference's viewport is `bubbles`; the clone's is hand-rolled.**
  The reference's counter (`(1–26 of 35 lines)`) and its scroll keys come
  from that package. The clone reproduces the behaviour, not the code; a
  future `bubbles` migration would change the numbers.
- **The reference's URL block is one column narrower than its prose**
  (*width*−2 against *width*−1). The cause is not visible from the
  outside — a margin, a padding, a reserved cell. The clone copies the
  measurement, not the mechanism, so the two will drift apart if the
  reference's layout is rebuilt.
- **The clone's logo and its login-method screen still differ** from the
  reference (the reference's logo is `▄▀▀▄`, the clone's is `▀▀▄`; the
  reference's chooser has no spinner and no `Signing in...` prefix). That
  divergence is older than this cycle and was left alone.

## limitations

- The captures were taken on **macOS with tmux**. A terminal that
  implements OSC 8 differently (or not at all) shows the chunks and the
  click-here label as plain text — the link is then only reachable by
  selecting and copying.
- The **copy path was not verified end to end** in a real browser, and the
  cmd+click path was not exercised in a real terminal either. What was
  verified is that the visible text of the chunks concatenates back into
  the exact URL (`TestLoginView_URLIsNotTruncated`) and that every OSC 8
  target is the complete URL (`TestLoginView_URLChunksAreLinks`,
  `TestLoginView_ClickHereBlock`).
- The reference was measured at **40, 80 and 100 columns** only. Below 20
  columns the layout rules are extrapolated, and the clone clamps its
  width at 8 columns.
- The reference binary on this machine is **1.4.7**; most of the captures
  in this cycle were taken against it, but the clone is pinned to 1.4.6
  (`internal/cli.ReferenceVersion`). The click-here block is restored
  because the user's reference shows it — not because the 1.4.6 captures
  showed it.
