# Cycle 362 deliverable: the Color Scheme preview card in the reference's own colours, and the interface that follows the selection

**Cycle:** `cycle-362-theme-live-parity`
**Subject:** `/config` → **Color Scheme**. Two complaints: the preview card's
code highlighting did not match the reference's, and the rest of the
interface did not follow the scheme the preview showed. This cycle recovers
the reference's card **cell by cell** from a true-colour capture and makes
the clone's palette a *live* value: the scheme the preview shows is the
scheme the whole interface is painted in.
**Evidence:** 【一手】 first-hand, against a raw-pty capture of agy 1.4.8 in a
true-colour terminal · 【反编译】 n/a this cycle · 【strings】 n/a this cycle ·
【推测】 the hypotheses are marked where they appear

---

## 0. The short answer

| | before | after |
|---|---|---|
| the card's code colours | a 256-index palette (`palette.Syntax`), guessed back from a quantised capture | the reference's own **24-bit** escapes, read off the wire, for all eight schemes |
| the card's escape forms | colour and attribute in separate escapes | colour **and** attribute in **one** escape, as the reference writes them (`38;2;25;103;210;1`) |
| the interface when a scheme is previewed | the palette's colours were frozen at the default | the palette follows the **hovered** scheme: every frame is painted in it |
| the interface at session start | the configured scheme was ignored | the session wears the configured scheme from its first frame |
| the exit banner | the default scheme | the scheme the session wore |

The card is now **byte-identical to the reference's** on seven of the eight
schemes, and differs by **one cell** on the eighth — the reference's own
first paint, which is explained in [§6](#6-the-one-cell-difference).

---

## 1. What was wrong, exactly

The clone's card was painted from `palette.Syntax`, a table of **terminal
indices** (`Keyword: 129`, `String: 36`, …). That table came from a capture
taken in a 256-colour terminal, so the values were quantised: the reference's
`#1967d2` was recovered as index 26, and `index 26 → #1963d2` was inferred
from the xterm cube. The inference is wrong by one step in the green channel
— and the same inference, applied to every scheme, produced a palette that
*looked* right and was wrong everywhere.

The second complaint was structural. `palette` held its colours in package
variables, initialised once at init time from the solarized-light scheme.
Selecting a scheme in `/config` recorded the name and repainted the preview
card, but nothing else in the interface consulted the name — the transcript,
the panels, the prompt and the banner all kept the colours they were
initialised with.

---

## 2. The capture

【一手】 The reference's card is drawn with cursor positioning
(`ESC[19;34H`, `ESC[30X`, `ESC[19;96H`), so the card cannot be read from a
tmux screen dump: a 256-colour terminal quantises the 24-bit escapes before
the dump sees them, and a tmux capture loses the escape bytes entirely.

The capture is therefore a **raw pty** with `COLORTERM=truecolor`:

* `capture_card.py` forks a pty, runs `~/.local/bin/agy`, sends
  `/config`, `color`, `⏎`, and then the number of `↑` that lands on the
  wanted scheme;
* it then **kills** the reference with `SIGKILL`. The last key the reference
  would take is `Esc`, which closes the panel and repaints the screen — the
  card would be erased before it could be read. The card is on screen when
  the process dies, and the bytes it wrote are in the pty buffer.

One run per scheme: the reference repaints the card **in place** when the
selection moves, so a single run's buffer holds every scheme's card, one
over the other, and only the last one is legible.

`refgrid.py` is a small terminal emulator: it replays the raw bytes into a
grid of `(text, style)` cells, honouring `CUP`, `ECH`, `EL`, `ED`, `CUF`,
`CUD`, `CUU`, `CUB` and the SGR set. The card's cells are then compared
with the clone's, cell by cell.

```
docs/cycles/cycle-362-theme-live-parity/
  capture_card.py   the raw-pty capture, one run per scheme
  refgrid.py        the mini terminal emulator
  compare_card.py   the cell-level diff
```

The clone's side of the comparison is written by `devutils/carddump`:

```
go run ./devutils/carddump /tmp/themecap/clone
python3 docs/cycles/cycle-362-theme-live-parity/compare_card.py \
        /tmp/themecap/cards2.json /tmp/themecap/clone
```

---

## 3. The recovered palette

【一手】 Every scheme but the terminal one writes **24-bit** escapes for the
code roles — the reference never falls back to a 256-index for them. The
terminal scheme is the exception: its roles are the basic sixteen-colour
codes, and its backgrounds are `48;5;131` / `48;5;65`.

| scheme | keyword | string | function | type | variable | keyword attr |
|---|---|---|---|---|---|---|
| terminal | `94` | `33` | `34` | `35` | — | `1` (bold) |
| light | `#770088` | `#008800` | `#0000ff` | `#008855` | `#0055aa` | — |
| solarized light | `#cb4b16` | `#859900` | `#2aa198` | `#6c71c4` | `#657b83` | `22` |
| colorblind-friendly light | `#e8710a` | `#7b1fa2` | `#1967d2` | `#00838f` | `#3c4043` | — |
| dark | `#d8884b` | `#97c67b` | `#75b3db` | `#eede7b` | `#b5bcc5` | — |
| solarized dark | `#cb4b16` | `#859900` | `#2aa198` | `#6c71c4` | `#839496` | `22` |
| colorblind-friendly dark | `#fb8c00` | `#b388ff` | `#ffe57f` | `#00e5ff` | `#e0e0e0` | — |
| tokyo night | `#bb9af7` | `#9ece6a` | `#7aa2f7` | `#2ac3de` | `#c0caf5` | — |

Three of these values are the ones the quantised capture got wrong, and the
raw pty is what corrected them:

| role | quantised capture | raw pty |
|---|---|---|
| colorblind-friendly light, function | `#1963d2` | `#1967d2` |
| colorblind-friendly light, type | `#00878f` | `#00838f` |
| dark, type | `#eedd7b` | `#eede7b` |

The table lives in `palette.SyntaxColorsFor` (`palette/theme.go`); the
256-index `palette.Syntax` stays, because `internal/onboarding` — the
pre-TUI wizard — paints with it.

---

## 4. The escape forms

【一手】 The reference does not write a colour and an attribute as two
escapes. It writes them as one:

| what | the reference's bytes |
|---|---|
| the user's line | `ESC[38;2;25;103;210;1m` — the user colour **and** bold |
| the body after it | `ESC[38;2;32;33;36;22m` — the body colour **and** normal intensity |
| the link | `ESC[38;2;26;115;232;4m` — the link colour **and** underline |
| the diff rows | `ESC[39;48;2;r;g;bm` — the background, with the syntax colours inside |

So the clone's card writes the same forms, and `palette.SeqWith(c, extra)`
is the helper that produces them. Two details follow from the capture and
are now pinned:

* an **empty** extra leaves the escape alone — the reference never writes a
  trailing `;` before the `m`;
* a body that follows a bold run is closed with the *body colour's* escape
  (plus `;22`), or with a full reset when the body colour is unset.

The clone's card also paints the *link label* (`  → link: `) in the body
colour and only the URL in the link colour — as the reference does.

---

## 5. The cell-level comparison

The reference's card and the clone's card are compared cell by cell: the
text **and** the style (foreground, background, bold, underline, faint) of
each of the 63 × 22 cells.

```
--- terminal: 0 differing cells
--- light: 0 differing cells
--- solarized light: 0 differing cells
--- colorblind-friendly light: 0 differing cells
--- dark: 0 differing cells
--- solarized dark: 0 differing cells
--- colorblind-friendly dark: 0 differing cells
--- tokyo night: 1 differing cells
   row  5 col  1  ref ' ' default    clone '│' fg=#414868
total differing cells: 1
```

The card's bytes are pinned in the repo as well:
`internal/themepreview/testdata/card_solarized_light.golden` holds the whole
card, escape for escape, and `TestCardGoldenSolarizedLight` fails on any
change.

---

## 6. The one-cell difference

The differing cell is the **left border of the blank line** under the
heading, in the tokyo-night scheme: the reference's cell is a space with
the default style, the clone's is the border glyph in the border colour.

It is not a colour difference, and it is not a rendering difference either.
It is the reference's **first paint**.

The capture's raw bytes show that the reference draws the card in two
ways:

* the **first** paint (the card as it appears when the panel opens) skips
  the left border of the blank row — and of the two diff rows, whose
  borders it paints as part of the rows themselves;
* a **hover** paint (the card after the selection moves to another scheme)
  draws every row's left border, including the blank row's.

The tokyo-night capture is the *first* paint: the cursor starts on the last
row of the list, so no hover ever happens in that run, and the capture
holds the initial paint. Every other scheme's capture ends on a hover
paint — the cursor moved to reach it — and those captures agree with the
clone on every cell, the blank row's border included.

The clone always renders the card as a whole, so it always paints the
border: it matches the reference's *hovered* card for all eight schemes,
and differs from the reference's *initial* card by one cell in one scheme.
A second capture of tokyo night reproduced the same artefact, which is
what a deterministic partial repaint looks like.

---

## 7. The live scheme

The palette is now a live value rather than a frozen table.

* `palette.SetScheme(name)` resolves the name against the eight schemes and
  recomputes the package's colours from that scheme's slots.
* `palette.PushScheme(name)` does the same and returns a restore function.
  The TUI pushes the session's scheme around the two places that paint a
  frame — the `Update` that handles the keys and the `View` that renders —
  so a frame is painted in the scheme the preview shows, and the palette
  keeps its default colours outside a frame.
* `palette.OnSchemeChange(fn)` lets a package that *copies* the palette's
  colours refresh its copies. Without it an alias copied once would keep
  the previous scheme's escape forever.

The wiring:

| where | what |
|---|---|
| `internal/cliapp/slash_config_preview.go` | `applyThemeLive` records the hovered scheme on `m.colorScheme` and re-renders the preview; it is called on every `↑`/`↓` in the Color Scheme submenu |
| `internal/cliapp/tui_run.go` | `palette.PushScheme(m.colorScheme)` around `Update` |
| `internal/cliapp/tui_view.go` | `palette.PushScheme(m.colorScheme)` around `View` |
| `internal/cliapp/tui_runmode_agent.go` | the session's scheme is read from the settings at startup and recorded, so the *configured* scheme paints the first frame |
| `internal/cliapp/tui_entry.go` | the exit banner is painted under the session's scheme |

`palette`'s twenty slots are the reference's `types.SchemeColors` — the
table was already in `palette/scheme.go`; what was missing was the
*switching*. Four slots had no alias at all and were hard-coded in the
clone's own tables; `UserFgOpen`, `AgentFgOpen`, `TaskFgOpen` and
`ToolRemoveBodyOpen` now follow the scheme.

### The hard-coded sweep

Every literal colour in the interface was re-examined against the scheme
table. A literal whose value equals a scheme slot — **in the scheme it came
from** — became the scheme-driven alias; the others stayed literal:

* the effort slider's `172` / `166`, the inline-code green, the panel blue
  `69` and the tool glyph `172` have no slot in any scheme, so they stay;
* `internal/loginui` is untouched: it is a pre-TUI surface, painted before
  a session exists.

The packages that keep their own copies of the palette's colours
(`internal/tuilib`, `internal/contextpanel`, `internal/modelpicker`,
`internal/usagepanel`, `internal/pluginsui`, `internal/skillsui`,
`internal/cliapp/renderer.go`) register a `palette.OnSchemeChange` listener
and refresh their aliases when the scheme changes.

---

## 8. What is left

The colours that remain hard-coded, and why:

| where | colour | why |
|---|---|---|
| `internal/contextpanel` | `38;5;172`, `38;5;26` | the tool glyph and the user-message bullet; no scheme slot carries them |
| `internal/codesearch` | `38;5;32`, `38;5;166` | the match highlight and the error |
| `internal/modelpicker` | `38;5;60`, `38;5;172`, `38;5;166`, `38;2;133;153;0` | the note, the glyph, the error, the effort slider |
| `internal/permissions` | `38;5;69` | the rule name |
| `internal/pluginsui` | `38;5;244`, `38;5;238`, `38;5;166` | the status column |
| `internal/skillsui` | `38;5;249`, `38;5;235`, `38;5;69`, `38;5;36`, `38;5;27`, `38;5;228` | the markdown theme (`detail_md.go`) — a separate theme, not a scheme slot |
| `internal/tuilib` | `38;5;235` | the changelog-like surfaces |
| `internal/changelog` | the whole palette | the `changelog` command's own theme |
| `internal/featdiff` | the whole palette | the `feat-diff` command's own theme |
| `internal/cliapp` | `38;5;69`, `38;5;166`, `38;2;101;123;131`, `38;2;88;110;117`, `38;2;133;153;0`, `38;2;255;117;24` | the panel chrome, the seasonal banner, the verification marks |

`internal/changelog` and `internal/featdiff` are *commands*, not the
session's interface: they print their own theme, as the reference's own
`changelog` surface does.

---

## 硬约束

* **The card's colours are the reference's, byte for byte.** The eight
  schemes' syntax palettes are read off the wire, not inferred: the
  quantised capture's three wrong values are corrected, and the corrected
  ones are pinned by `palette/theme_test.go`.
* **The palette is live, not a table.** A frame is painted in the scheme
  the preview shows. The palette's *default* (solarized light) is only what
  a frame sees outside a pushed scheme — the exit banner pushes the
  session's scheme before it prints.
* **The reference's terminal scheme is the exception.** Its code roles are
  the basic sixteen-colour codes and its keyword is bold; every other
  scheme's roles are 24-bit.
* **`palette.Syntax` (256 indices) stays** for `internal/onboarding`, the
  pre-TUI wizard.
* **The card's bytes are pinned**: the golden file plus the cell-level
  comparison harness in this directory.

## 存疑

* **The reference's default scheme is not established by these captures.**
  The reference's own settings (`~/.gemini/antigravity-cli/settings.json`)
  carry `"colorScheme": "dark"` on this machine, and the panel's
  `(current)` marker follows the *cursor*, not the active scheme — the
  marker was on tokyo night in every capture, including the ones where the
  cursor had been moved to another scheme. The clone's palette default is
  solarized light; the reference's default, with no setting at all, is
  still unknown. 【推测】 the reference's default is the last scheme in the
  list (the cursor starts there), but the capture does not prove it.
* **The clone follows the *hovered* scheme; the reference does not.** In
  the reference's captures the panel's own chrome — the menu, the hints —
  keeps the previous scheme's colours while the card shows the hovered
  one; only the card repaints. The clone repaints the whole interface,
  which is what the user asked for and what the reference does when the
  scheme is *committed* (Esc), but it is a deliberate divergence from the
  reference's hover behaviour. 【推测】 the reference's hover repaint is
  deliberately narrow so the panel does not flicker.
* **`palette.WarningOpen` keeps its plain slot colour.**
  `38;2;203;75;22m` — the `;22` there is the *green channel* of
  `#cb4b16`, not an attribute. The reference's own transcript warning bytes
  are `38;2;203;75;22;22m` (the colour **and** the normal intensity). The
  clone keeps the plain form, because an earlier attempt to add the `;22`
  broke `TestCaptureIsSolarizedLight`, which pins the reference's own
  capture of that colour.
* **The first-paint artefact (§6) is explained but not reproduced.** The
  clone always paints the whole card. If byte-parity with the reference's
  *initial* paint matters, the blank row's left border would have to be
  dropped — which would make the clone differ from the reference's
  *hovered* card instead.

## Limitations

* **The comparison covers the card, not the whole panel.** The eight
  captures prove the card's 63 × 22 cells; the panel's chrome around it
  (the menu, the hints, the borders) was compared by eye, not cell by
  cell.
* **The transcript's history is baked at stream time.** The clone colours
  the transcript as it is written, so a scheme change does not repaint the
  lines already on screen — only the lines drawn afterwards. The
  reference has the same property (it does not rewrite the scrollback), but
  the two differ in *when* the colour is resolved: the reference resolves
  it when the line is painted, the clone when the stream event arrives.
* **The capture is one machine, one terminal.** The reference's colours
  were read in a 240 × 50 true-colour pty on macOS. A terminal that does
  not advertise `COLORTERM=truecolor` would make the reference fall back
  to the 256-index codes the clone's `palette.Syntax` still carries.
* **The remaining hard-coded colours (§8) are not compared with the
  reference's own bytes.** They are the values the clone already used, and
  the sweep kept the ones that have no scheme slot. Whether the reference
  paints them in a scheme-independent colour is not established.
* **The other agent's in-flight work is not covered.** The `/pool` work in
  `internal/cliapp/slash_panel_accounts.go`, `internal/accountrouting/` and
  `endpoints/config.go` was in the tree while this cycle ran; the accounts
  panel's own colours were left to that work.
