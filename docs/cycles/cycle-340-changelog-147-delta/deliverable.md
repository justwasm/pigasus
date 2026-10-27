# cycle-340-changelog-147-delta — closing the 1.4.7 changelog gap

The clone emulates `agy` 1.4.7. This cycle takes the upstream
CHANGELOG's 1.4.7 section item by item, measures each item against the
reference binary (`~/.local/bin/agy`, 1.4.7) and closes the two gaps
that are visible in the clone.

---

## TL;DR

- **Two of the fourteen 1.4.7 items were clone-visible gaps. Both are
  closed.**
  1. **`/help`'s SHORTCUTS tab** no longer lists the basic navigation
     keys — 27 rows, row for row with the reference.
  2. **The seasonal (Halloween) theme** is implemented end to end: the
     `/config` row, the themed startup banner, the themed prompt-box
     rules and the moon-phase spinner.
- **The themed banner is not one picture.** The reference draws one of
  **five** scenes per launch — a pumpkin, a moon, a ghost, a witch's hat
  and a bat — each with its own colour set, next to one of **four**
  seasonal sentences. The clone now picks a scene the same way (one per
  process), and every one of the five is **cell for cell identical** to
  the reference's own settled banner, colours included.
- **The five scenes are printable.** `pigasus halloween` prints them all,
  `pigasus halloween <scene>` prints one, and the clone's own hidden
  `logo` command grew `--seasonal` / `--scene`. The reference has no
  such surface — `agy logo` answers *"Error: unexpected argument
  \"logo\""* — so this is a pigasus addition.
- **The other twelve items are parity, server-side, or not applicable**
  to the clone. Each one is listed in the checklist below with its
  verdict and the evidence behind it.
- **Two open differences are documented and left open**: the code
  block's soft wrap (the reference keeps the indentation and marks the
  continuation, the clone does neither) and the MCP OAuth grants (the
  clone's store is keyed by server URL, not by the signed-in user).
- `go test ./cmd/... ./internal/...` green except for
  `internal/convstore` (another agent's in-flight edit).

---

## The 1.4.7 checklist

The upstream section
(<https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md>,
fetched 2026-10-27) has fourteen bullets. The reference binary wins
over the changelog wherever the two disagree.

| # | 1.4.7 item | Verdict | Evidence |
|---|---|---|---|
| 1 | `lastUserInput` in the JSON hook commands receive on stdin | **N/A** | The clone has no hook-command runner — `internal/hooks/hooks.go:19` says so explicitly ("we do not actually invoke shell commands here"). There is no stdin JSON to add a field to. |
| 2 | Seasonal theme (opt-in `/config` row, banner, moon spinner, themed borders) | **CLOSED** | See "The seasonal theme" below. `internal/seasonal/`, `internal/cliapp/seasonal*.go`. The banner draws **five** scenes, one per launch, exactly as the reference does. |
| 3 | Auto mode asks for approval less often on routine commands | **server-side** | The clone's local policy auto-approves *every* command in auto mode (`internal/permissions/policy.go:235`), so it never asks; the reference's prompt *frequency* is the agent's decision, made on the server. Nothing in the CLI layer reproduces it. |
| 4 | Headless `-p` uses much less CPU (no search index, no transcript rendering, no status-line / window-title commands) | **parity** | `internal/printmode` has no conversation-search-index code at all, and print mode refuses the two settings outright: `internal/printmode/print_slash.go:366` (`"title": reject("it configures the terminal window title")`) and `:385` (`"statusline"`). |
| 5 | Improved rendering speed while streaming / on a permission prompt / on resize | **N/A** | A performance change with no behavioural surface to compare. |
| 6 | MCP tools loaded on demand: results up to 10 KB returned directly | **parity** | The clone's MCP client returns the tool result inline (`internal/mcp/client.go`) — it never writes an MCP result to a file, which *is* the 1.4.7 behaviour (the old behaviour was ">4 KB saved to a file"). The 10 KB cap the clone does implement is the run-command body cap: `internal/tools/command.go:527` (`maxCommandBodyBytes = 10240`). |
| 7 | `/help` shortcuts tab leaves out the basic navigation keys | **CLOSED** | `internal/tuilib/help.go` — the seven rows (`down`, `left`, `pgdown`, `pgup`, `right`, `tab`, `up`) are gone; 27 rows, matching `out/changelog-147/ref/help-shortcuts.txt`. |
| 8 | ADC sign-in loads skills, rules, agents, plugins and hooks | **parity in effect** | The clone loads skills and rules from the home directories regardless of the sign-in method; ADC is only a business-account marker (`internal/auth/business.go:23`). The clone never withheld them from an ADC session. |
| 9 | Conversations compact over and over when a message / tool output / document is too large | **server-side** | Compaction is the server's job; the CLI only relays the compacted transcript. |
| 10 | Long link wrapping in a Markdown table cell linked only on the first line; tables in the Terminal scheme use box-drawing borders | **parity** | Measured live against the reference: `out/changelog-147/ref/markdown-table-and-code-wrap.txt`. The reference renders the table with `│` and `─` and turns the bare URL into `example.com[1]` with a footnote `[1]: example.com https://…`. The clone renders the same markdown to the same shape (`mdrender.ANSI(src, 150)`). |
| 11 | Copying a long command that wraps in a code block copies as one line; wrapped code-block lines break between words and keep the indentation | **PARTIAL — open** | The clone breaks long code lines between words (glamour's word wrap) but does **not** repeat the indentation on the continuation row and has no soft-wrap marker. The reference's continuation row is `    ￹echo "done now"` — indentation kept, `￹` (U+FFF9) marking the continuation. See "Open items". |
| 12 | Vim mode's `r` replace leaving part of a collapsed `[Pasted text #N]` placeholder | **N/A** | The clone has no `[Pasted text #N]` placeholder to collapse. |
| 13 | Plugin / MCP server sign-ins too large for the keychain are stored in the CLI's private secrets file | **parity** | The clone's keyring falls back to the file store when the keychain refuses the payload. |
| 14 | Plugin MCP servers with OAuth get tokens that belong to the signed-in user and reconnect on an account switch | **OPEN** | The clone's saved grants live in one store keyed by server URL (`internal/mcpoauth/signin.go`), so two accounts share a grant and nothing reconnects on a switch. See "Open items". |

---

## What landed

| File | Change |
|---|---|
| `internal/seasonal/seasonal.go` | The event catalogue: the `halloween-<year>` event, its window, its opt-in key, its four banner sentences and its **five** banner scenes (`Event.Arts`, `Event.Art(pick)`). |
| `internal/seasonal/art.go` | The banner-art renderer (half blocks). **Fixed**: a cell whose *lower* sub-pixel alone carries a colour was painted with the *background*, which puts the colour in the wrong half; it now uses the foreground, like the reference. |
| `internal/seasonal/art_halloween.go` | The five scenes: five palettes and five 46 × 14 rasters, decoded from the reference's own truecolor captures of the settled banner. |
| `internal/seasonal/print.go` | The printer: `Art.Print`, `Event.ArtNames`, `Event.ArtByName`, `Event.PrintScenes`. |
| `internal/seasonal/print_test.go` | The scene names, the lookup, the caption rule and the unknown-scene error. |
| `internal/cli/halloween_cmd.go` | The `halloween` subcommand: it prints the scenes. |
| `internal/cli/halloween_cmd_test.go` | The command's five routes: all scenes, one scene, a random scene, an unknown name, the usage block. |
| `internal/tuilib/logo_cmd.go` | The `--seasonal` and `--scene` flags on the clone's hidden `logo` command. |
| `internal/tuilib/logo_seasonal_test.go` | The two flags: the named scene, the random scene, all scenes, the unknown-scene error. |
| `internal/seasonal/border.go` | The themed prompt-box rule and the moon-phase spinner frames. |
| `internal/seasonal/seasonal_test.go` | Golden tests against the reference's captures (all five scenes, cell for cell), the scene-distinctness test, plus the half-block, spinner and greeting tests. |
| `internal/cliapp/seasonal.go` | The theme resolution (event × opt-in), the sentence pick, the **scene pick**, the themed spinner swap. |
| `internal/cliapp/seasonal_banner.go` | The themed banner renderer. |
| `internal/cliapp/slash_panel_config.go` | The `Seasonal Themes` row and its position in the catalogue. |
| `internal/cliapp/slash_config_preview.go` | The refresh hook: switching the row re-reads the theme. |
| `internal/cliapp/tui_help.go` | `renderBanner` → `renderBannerWithTheme`. |
| `internal/cliapp/tui_run.go`, `tui_runmode_agent.go`, `tui_turn_blocks.go`, `tui_view.go`, `tui_viewport_scroll.go` | The themed prompt-box rules, the themed spinner, the themed banner in the scroll header. |
| `internal/printmode/print_slash_config.go`, `print_format_config.go` | The two print-mode settings keys (`seasonalThemesOff`, `seasonalThemesOptIn`), 34 → 36 keys. |
| `internal/tuilib/help.go` | The seven navigation rows removed from the SHORTCUTS tab (34 → 27). |
| `out/changelog-147/ref/` | The reference captures this cycle was measured against. |

---

## The seasonal theme

Everything below was measured live against `agy` 1.4.7 in a 200 × 50
tmux session, with the captures kept under `out/changelog-147/ref/`.

### The setting and the row

- The opt-in lives in `settings.json` as **`seasonalThemesOptIn`** and
  carries the *event's own id* — `"halloween-2026"`. An absent key means
  off; there is no `seasonalThemesOff` key in the file (the print-mode
  dump lists one, but the file never carries it).
- The `/config` row is named **`Seasonal Themes`**, its two stops read
  `off` and `on`, and its help line is the reference's own sentence:

  > Show seasonal touches, such as a themed banner and spinner, while a
  > seasonal event is on. Your choice lasts until the event ends.

- The row sits between **`Sandbox Mode`** and **`Show Active Tasks`**.
- The row is drawn **only while the event is in season**. The clone's
  catalogue is therefore dynamic: `configRows()` inserts the row when
  `seasonal.Current(now)` is non-nil.
- Switching it **on** writes `{"seasonalThemesOptIn": "halloween-2026"}`;
  switching it **off deletes the key** — the reference's own "a setting
  at its default is absent" convention, which the clone follows
  everywhere else.

### The banner

Seven art rows, 46 cells wide; the information column starts at
**column 46** (the art carries the gutter the plain banner's two spaces
would have made). Rows 0–3 carry the session's information — CLI name
and version, account, model, cwd — and the sentence follows the art:

```
      ▀        ▄▄▀▀▀▀▀▀▄▄      ▀▄▄▄▀    ▄     Antigravity CLI 1.4.7
      ▀      ▄▀▀▀▀▀▀▀▀▀▀▀▀▄    ▄      ▀▄▀     naivgaid@gmail.com (Google AI Pro)
    ▀▄▀▄▀    ▀▀▀▀▀▀▀▀▀▀▀▀▀▀       ▄▄▄▄▄       Gemini 3.8 Flash (High)
    ▀▀▀▀▀  ▄▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▄    ▀▀▀▀▀▀▀      ~/Documents/GitHub/openagy
   ▀ ▀ ▀   ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀    ▀▀▀▀▀▀▀
  ▀▀▀        ▀▀▀▀▀▀▀▀▀▀▀▀▀▀      ▀▀▀▀▀▀▀
  ▀▀▀▄▀▄▄▄▀▄▄▀▀▄▄▀▄▄▀▄▄▀▄▀▀▄▀▄▄▀▄▀▀▀▀▀▀▀▄▀
  Spooky season, steady builds, naivgaid.
```

- The sentence is bold, 24-bit orange (`38;2;255;117;24`), closed by a
  full reset, and is addressed to the **local part of the session's
  email** (`naivgaid`).
- The reference has **four** sentences and picks one **per launch**:

  > Spooky season, steady builds · No bugs tonight, only boos ·
  > Happy haunting · Something is brewing in the build

  The clone picks one per process (`math/rand/v2`) and keeps it for the
  session.

### The banner is not one picture (five scenes)

【一手】 Measured live on 2026-10-27 in a pty
(`COLORTERM=truecolor`, `TERM=xterm-256color`, 200 × 50), so the capture
carries the reference's own 24-bit output:

- The reference draws **one of five scenes per launch** — a **pumpkin**,
  a **moon**, a **ghost**, a **witch's hat** and a **bat**, each standing
  on the same spider, web and ground, and **each with its own colour
  set**. Fifteen launches produced all five (1 / 6 / 1 / 3 / 4), and a
  scene is drawn **identically every time** — twenty-two launches gave
  exactly five rasters, one per scene.
- The scene pick and the sentence pick are **independent**: every
  sentence was seen next to every scene.
- The clone stores the five scenes and picks one per process
  (`math/rand/v2`), next to the sentence pick.
- **All five scenes are cell for cell identical to the reference's own
  settled banner** — glyph, foreground colour and background colour
  compared cell by cell over all seven rows: **zero differing cells**.

The reference paints the banner **twice**: once on its own while the
session signs in (one column to the left), and once inside the signed-in
banner. The two drawings are identical for the pumpkin, the moon and the
ghost, and **differ for the witch's hat and the bat** — the reference
re-places their elements. The clone stores the **settled** banner, the
one the user is left looking at.

Two measurement traps cost real time and are worth writing down:

- **A 256-colour capture is not the truth.** `tmux capture-pane`
  quantises the picture's shading, and the quantised result reads as a
  *different* picture — that is where an earlier "ten variants"
  measurement came from. The scenes have to be captured over a pty that
  reports true colour.
- **The reference's own first paint is not the banner.** Only the
  settled banner is what the user sees; the standalone pass differs for
  two of the five scenes.

### Printing the scenes

The five pictures are now printable, on two routes that share one
printer (`internal/seasonal/print.go`).

| Command | What it prints |
|---|---|
| `pigasus halloween` | every scene, each under its name |
| `pigasus halloween <scene>` | that scene alone |
| `pigasus halloween random` | one scene, picked the way the reference picks it at startup |
| `pigasus logo --seasonal` | one scene, the same way |
| `pigasus logo --scene=<scene>` | that scene alone |
| `pigasus logo --scene=all` | every scene |

- 【一手】 **The reference has no such surface.** Its art only ever
  appears in the startup banner, and the binary has **no `logo`
  command** at all: `agy logo` answers *"Error: unexpected argument
  \"logo\". Prompts are read only from -p/--print, -i/--prompt-interactive,
  or stdin, so this argument would have been ignored."* Both routes
  are therefore the clone's own additions.
- `halloween` is a **visible** subcommand, next to the clone's other
  openagy commands (`accounts`, `model`, `provider`); the help golden
  (`internal/cli/testdata/help_1.4.6.txt`) carries its row, and the
  reference's own help list is untouched.
- `logo` is the clone's own **hidden** command, so the two new flags
  cost the reference's help nothing.
- **One scene is printed as the bare picture; several are printed with
  their names above them**, so the pictures can be told apart. The
  output is the reference's own 24-bit escape stream, so it needs a
  truecolour terminal to look right.
- **The art is not calendar-gated.** The *themed startup* is
  (`seasonal.Current` returns the event only in season), the *pictures*
  are not (`seasonal.Halloween` returns them any time) — the printing
  command has to work in July.
- 【推测】 **The five names are the clone's own labels**, read off the
  pictures: the reference carries no scene table and names its scenes
  nowhere. The names are the clone's contract (`Art.Name`), not the
  reference's.

### The spinner

The streaming row shows a four-frame moon phase. The binary's string
table holds the four frames as four four-byte literals — `◐ `, `◓ `,
`◑ `, `◒ ` — each with its own trailing space, which is why the row
reads `◐  Generating…` (glyph, space, space, text). The clone's
themed spinner uses the same frames.

### The prompt box

The prompt box's two rules become the event's own: a gradient rule with
face motifs in it. The top and the bottom rule are two phases of the
same cycle, and the motif placement does not depend on the width (the
reference's 120-column and 200-column captures agree). The rule that
separates the queued messages from the transcript stays plain.

### Half-block semantics (a real bug, fixed)

A half block paints its *filled* half with the **foreground**: `▄` with
`fg = <colour>` fills the lower half. The clone's new art renderer
painted a lower-only cell with the *background*, which puts the colour
in the upper half instead. The reference's own capture reads
`\x1b[38;2;195;166;255m▄▄` for such a cell — the colour is in the
foreground. Fixed, and pinned by a test that parses the rendered cells.

---

## Open items

### The code block's soft wrap (1.4.7 item 11)

The reference wraps a long code line at a word boundary, **keeps the
indentation** on the continuation row and marks it with `￹`
(U+FFF9):

```
    cd /some/very/long/path/… && cat /tmp/out.txt &&
    ￹echo "done now"
```

The clone breaks at a word boundary (glamour's word wrap) but the
continuation row starts at column 0 — the indentation is lost — and
there is no marker. Closing this needs a code-block wrap in
`internal/mdrender` that knows the render width (glamour's own wrap is
width-agnostic), which is a change in the transcript's rendering path
and was left for a cycle of its own.

### The MCP OAuth grants (1.4.7 item 14)

The clone's saved sign-ins live in one store keyed by the server URL
(`internal/mcpoauth/signin.go`), so a grant survives an account switch
and is shared between two accounts. The reference now scopes the grant
to the signed-in user and reconnects the plugin's MCP servers when the
account changes.

---

## Hard constraints

- **The reference binary wins over the changelog.** Where the two
  disagree, the changelog line is read as a summary of a behaviour the
  1.4.7 binary actually has.
- **The clone's `/config` keeps the reference's storage convention**: a
  setting at its default is *absent* from `settings.json`. The seasonal
  opt-in therefore writes `seasonalThemesOptIn: "halloween-2026"` and
  deletes the key again — it never writes `seasonalThemesOff`.
- **The seasonal row is calendar-gated.** The clone's clock is an
  injectable variable (`seasonalNow`); the package's tests pin it
  outside the season so the panel's row count stays deterministic, and
  the seasonal tests pin it inside.
- **The banner scenes are captured, not reconstructed.** The reference
  draws its art procedurally (`cli/theme/halloween`), so the clone
  stores the five decoded rasters. They were captured over a pty that
  reports true colour, because a 256-colour capture loses the picture's
  shading and reads as a different scene.
- **The other agent's tree is untouched.** `internal/convstore`,
  `internal/remotecontrol/`, `internal/remotetransport/`, `cmd/harness/`
  and friends were not staged. `internal/agyproxy/passthrough.go` was
  already modified by that agent when this cycle started and was left
  alone.
- **No `go test ./...`** — `scratch/` has never compiled. The cycle's
  surface is `go build ./cmd/... ./internal/...` and
  `go test ./cmd/... ./internal/...`.

---

## Open questions

- **The season's window is a guess.** The reference ships a *schedule*
  and a *periods* setting whose values live in a server-side schedule
  the binary only reads; the clone uses the calendar month of the event
  (1 October – 1 November). The event id carries the year
  (`halloween-2026`), so the event is regenerated per year.
- **The themed spinner's frame rate is a guess.** The capture cadence
  is consistent with the plain spinner's 10 fps, but the binary does
  not carry the rate in a readable form.
- **The banner's sentence is random in the reference.** The reference
  picks one of four sentences per launch, and the clone picks one per
  process; the reference's own selection rule (random, seeded by the
  launch time, or something else) is not recoverable from the captures.
- **The scene's own selection rule is likewise unknown.** Five scenes,
  drawn uniformly at random as far as the captures show (15 launches:
  1 / 6 / 1 / 3 / 4 — a chi-square of 8 against 4 degrees of freedom,
  p ≈ 0.16), but the binary keeps no scene table in readable form: the
  recovered symbols of `cli/theme/halloween` are `halloween.paint`,
  `halloween.renderLogo`, `halloween.renderLogoWith`, `halloween.window`,
  `halloween.greeting` and `halloween.fg` — the picture is **composed in
  code**, and a static read recovers no RGB palette table and no image
  asset for the theme. The clone therefore stores the decoded captures.
- **The reference paints the banner twice, and the two paintings differ
  for two of the five scenes.** The clone stores the settled banner only;
  the standalone pass (the one the user sees for a moment while signing
  in) is not reproduced.
- **`dismissedAnnouncements` is still missing from the print-mode
  `/config` dump.** The reference lists 37 keys, the clone 36 (after
  this cycle's two seasonal keys). The key is not a 1.4.7 item, so it
  was left alone.
- **The reference's colour depth varies with the terminal.** The
  captures in `out/changelog-147/ref/` are 24-bit in one session and
  256-colour in another; the clone emits 24-bit, which is what the
  reference emits when the terminal supports true colour.

---

## Limitations

- The reference's markdown and code-block rendering was compared on
  one table and one long command line, in one terminal width. The
  reference's renderer is not glamour's; a full differential of the
  two renderers would need a corpus.
- The seasonal measurements were taken on 2026-10-27 with the Halloween
  event in season. Another event (the reference's binary carries a
  *halloween* theme package) would have its own art, rules and
  sentences; the clone only knows the Halloween one.
- The five scenes were captured over one pty geometry (200 × 50, true
  colour). The reference's art does not depend on the terminal width —
  the banner's art block is a fixed 46 cells — but the scenes were not
  re-measured at another width or with another account.
- The clone's themed art emits one escape pair per cell, while the
  reference merges equal runs into a single escape. The rendering is
  identical on screen; the bytes differ.
- **The printing surface is the clone's own, and it is not parity
  work.** The reference has no `halloween` command and no `logo`
  command, so there is nothing to compare the two routes against; they
  were shaped by what the pictures needed — one picture bare, several
  pictures named — and by the clone's existing command conventions.

---

## Reproduction

```bash
go build -o /tmp/pigasus ./cmd/pigasus
go test ./cmd/... ./internal/...        # internal/convstore fails: another
                                        # agent's in-flight edit

# The seasonal package's golden tests.
go test ./internal/seasonal/...

# The clone's seasonal tests (the package's clock is pinned to
# January; the seasonal tests pin it to October).
go test ./internal/cliapp/ -run TestSeasonal -v

# Live: the reference and the clone side by side, 200x50.
tmux new-session -d -s agyref -x 200 -y 50 '~/.local/bin/agy'
tmux new-session -d -s pigy   -x 200 -y 50 '/tmp/pigasus'
tmux capture-pane -p -t agyref    # the themed banner
tmux capture-pane -p -t pigy

# The five banner scenes: the reference's own truecolor output has to be
# captured over a pty — a 256-colour capture quantises the picture's
# shading and reads as a different scene.  The five raw streams this
# cycle measured against live under
# out/changelog-147/ref/seasonal-scenes/.

# The scenes themselves (the clone's own printing surface).
pigasus halloween                 # every scene, each under its name
pigasus halloween pumpkin         # one scene
pigasus halloween random          # one scene, the reference's own pick
pigasus logo --seasonal           # the same, through the logo command
pigasus logo --scene=witch-hat    # one scene, by name
```

The reference captures this cycle was measured against live in
`out/changelog-147/ref/`:

| File | What it shows |
|---|---|
| `config-seasonal-on.txt` | The themed banner, 24-bit, with the information column at 46. |
| `seasonal-prompt.txt` | The themed prompt-box rules. |
| `border-200.txt`, `border-120.txt` | The same rules at two widths. |
| `config.txt`, `config.plain.txt` | The `/config` panel with the `Seasonal Themes` row. |
| `help-shortcuts.txt`, `help-shortcuts.plain.txt` | The `/help` SHORTCUTS tab without the navigation keys. |
| `turn/f01..f24.txt` | The streaming row with the moon-phase spinner. |
| `markdown-table-and-code-wrap.txt` | The Markdown table (box borders, footnote link) and the wrapped code block (indentation + `￹`). |
| `seasonal-scenes/scene-pumpkin.txt` | One of the five banner scenes — the reference's raw pty stream (24-bit), the settled banner. |
| `seasonal-scenes/scene-moon.txt` | The moon scene, same format. |
| `seasonal-scenes/scene-ghost.txt` | The ghost scene, same format. |
| `seasonal-scenes/scene-witch-hat.txt` | The witch's hat scene, same format. |
| `seasonal-scenes/scene-bat.txt` | The bat scene, same format. |
| `seasonal-scenes/manifest.json` | Which scene each of the fifteen launches drew. |
| `seasonal-scenes/README.md` | The measurement, and the two traps (256-colour capture, the reference's first paint). |
