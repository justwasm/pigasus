# Cycle 361 deliverable: the reference's diff row model, in the transcript and in the `/diff` panel

**Cycle:** `cycle-361-diff-row-model`
**Subject:** the clone draws a code diff in two places — the **inline diff
preview** in the transcript, under an expanded edit or create step, and the
**`/diff` file view** in the panel. Neither drew the row the reference
draws. This cycle recovers the reference's row from the 1.4.8 binary and
renders both surfaces in it.
**Evidence:** 【一手】 first-hand against a truecolour pty capture of agy
1.4.3 · 【反编译】 decompiled output at confirmed agy 1.4.8 addresses ·
【strings】 string-table hits only · 【推测】 hypothesis

---

## 0. The short answer

The reference's diff row is **not** the git body with a `+` in front of it.
It is a three-column row:

```
<gutter><marker><text>
   │       │      └ the text, syntax-highlighted where the language is known
   │       └ four columns: " +  " / " -  " / "    "
   └ the line number, right-aligned in max(4, digits) columns
```

and the clone now draws exactly that, in both surfaces.

| | before this cycle | after |
|---|---|---|
| the gutter | a hardcoded `%4d` | `fmt.Sprintf("%*d", numWidth, num)` — the number widens with the file |
| the marker column | " -  " / " +  " / "    " | the same four columns, and the reference's own three body format strings |
| the changed body | padded to `width-13` | padded to the **content width** = `width-numWidth-2`, so a row is one column narrower than the width it was given |
| the summary | always `+N / -M lines`, for the whole diff | **three forms**, one per change: `Added N lines` / `Removed N lines` / `+N / -M lines` |
| the cap | none — every row was drawn | **20 rows a change**, then `… and N more lines` |
| the inline preview's colours | two variants: an untinted gutter + the add/remove tint, or (with no path) a striped gutter and no highlighting | **one** variant: the stripe on the gutter, the add/remove colour on the body, the syntax colours on the text |
| the `/diff` panel's rows | the raw git body — `+text`, `-text`, ` text` | the reference's row model, with the file view's background tint |

---

## 1. The row, from the 1.4.8 binary

【反编译】 `render.codeActionDetail` (`0x1028fff40`) builds the detail of an
edit step and `render.codeActionDetail.func2` (`0x102912630`) renders one
row of it. The literals are in the binary's own rodata:

| literal | address | what it is |
|---|---|---|
| `%*d` | `0x1033bf730` | the gutter — the line number, right-aligned |
| `+  %s` | `0x1033d1ad8` | an added body |
| `-  %s` | `0x1033d1add` | a removed body |
| `    %s` | `0x1033df8e9` | an unchanged body |
| ` ` | `0x103dd22f8` | the one space the added and removed bodies carry in front |
| `  ⎿  ` | `0x1033f174f` | the detail bullet |

The added and removed bodies carry **one space more** than the unchanged
body, so the text starts in the same column in all three — the marker
column is four wide, and the sign sits in its second column:

```
   3 -  gamma
   3 +  GAMMA
   3    delta
```

The body of an added or a removed row is padded with spaces to the content
width (`strings.Repeat(" ", …)`), and the body of an unchanged row is
**not** — it is the plain text behind the four-column marker.

【反编译】 The two widths come out of the same function: the gutter's width
is `max(4, digits)` and the content width is `max(1, width-numWidth-2)`. A
row is therefore `numWidth + 1 + contentWidth` wide — **one column
narrower** than the width the body was given, which is what keeps the
row's background from running into the next line.

## 2. The summary has three forms, one per change

【反编译】 `codeActionDetail` walks a list of *changes* — each one a start
line, an end line and the number of added and of removed lines — and prints
**one summary per change** before that change's rows:

| form | literal | when |
|---|---|---|
| `Added %d line%s` | `0x1034b43b4` | the change removes nothing |
| `Removed %d line%s` | `0x1034dcd2d` | the change adds nothing |
| `+%d / -%d lines` | `0x1033bf754`, `0x1033bf19c`, `0x1033bf757`, `0x1033df8e3` | the change does both |

The first two are plain text in the reference — only the two counts of the
third form are painted — so the clone's `diffrow.Summary` returns them
unstyled and the caller paints the pair itself.

## 3. Twenty rows a change, then a marker

【反编译】 The row loop breaks at `0x13` — **19** — so a change shows at
most **20** rows. What is left becomes one marker:

```
%*s    … and %d more lines      (0x10358a97d)
```

【推测】 The indent's own source is not recoverable — the reference reads
it from a state field this cycle did not pin — so the clone passes the
gutter's width, which puts the ellipsis in the column the rows' text
starts in.

## 4. The inline diff preview

【一手】 agy 1.4.3's captured diff (cycle 285) is the only first-hand
capture of this surface, and it settles the shape and the colours:

```
\x1b[6G\x1b[38;2;131;148;150m   1\x1b[m    alpha
\x1b[6G\x1b[38;2;131;148;150;48;2;238;232;213m   3\x1b[38;2;220;50;47m -  gamma\x1b[K\x1b[m
```

- `\x1b[6G` moves the cursor to **column 6** — the diff block is indented
  by five columns, in front of the `⎿` bullet's own text.
- The line number is the **line-number grey** (`#839496` in the solarized
  light scheme) on the changed row's **stripe** (`#EEE8D5`), and the body
  is the remove colour (`#DC322F`) or the add colour (`#859900`).
- `\x1b[K` erases to the end of the line, so the stripe spans the **whole**
  row.

`#EEE8D5` is the solarized light scheme's own **add background**, its
**remove background** *and* its **border** (`palette/scheme.go`), so the
capture's stripe and the reference's `AddBg`/`RemoveBg` are the same
colour — the reference paints the changed row's background from the
scheme's slots.

【strings】 The syntax highlighting is the 1.4.1 changelog's promise
("Improved the inline diff preview … with syntax highlighting for your
color scheme", `internal/cli/testdata/changelog_reference.txt:88`). The
1.4.8 row renderer does not highlight anything — `codeActionDetail.func2`
calls nothing but `fmt.Sprintf`, `strings.Repeat`,
`utf8.RuneCountInString`, `runtime.concatstring2` and
`lipgloss.Style.Render` — so the text arrives already styled and the row
renderer only has to measure the *plain* text to pad it. The clone
highlights the text when the file's language is known and measures the
plain text for the padding, which is the same arrangement.

## 5. The `/diff` file view

【strings】 The file view carries "syntax highlighting … and a background
tint on added and removed lines" (`changelog_reference.txt:127`), and the
1.4.1 changelog says the inline preview renders "like the `/diff` file
view … and an untinted line-number gutter" — so the file view has the
same gutter-and-marker row.

The clone's panel now draws its rows in that model: the line number in the
gutter, the four-column marker, the syntax-highlighted text, and the
**tint** across the whole row — which is what the row's padding is for.

【推测】 The reference's four tints live in `model.diffTerminalTints`, in
`__DATA`'s bss, and could not be read statically; the clone keeps its own
`palette.DiffAddTintOpen` / `DiffRemoveTintOpen` for them.

## 6. The files

| File | Status | Note |
|---|---|---|
| `internal/diffrow/diffrow.go` | new | the reference's row model: the layout, the gutter, the marker, the body, the summary, the truncation marker, the two styles |
| `internal/diffrow/git.go` | new | the `@@ -a,b +c,d @@` header parser the `/diff` panel needs for its line numbers |
| `internal/diffrow/diffrow_test.go` | new | pins the literals and the widths |
| `internal/diffaction/rows.go` | new | the `/diff` file view's row styler |
| `internal/diffaction/rows_test.go` | new | the styler's coverage |
| `internal/tuitools/diff.go` | modified | the inline preview: per-change summaries, the 20-row cap, the row model, the syntax colours |
| `internal/tuitools/tuitools_test.go` | modified | the reference shape, the summary forms and the cap |
| `internal/cliapp/slash_viewer_diff_fold.go` | modified | the panel's body goes through the new styler |
| `internal/cliapp/tui_tools_test.go` | modified | the 1.4.1-changelog test moves to the capture's shape; the create test moves to `Added N lines` |
| `internal/cliapp/slash_panels_test.go` | modified | three panel tests look for the git-style `+text` and now look for the marker column |

The colour **palette** was not touched: the row model assembles the
escapes from `palette.*`'s current values at render time, so the row model
follows the selected scheme the way the rest of the interface does.

---

## 硬约束

- The gutter is `fmt.Sprintf("%*d", numWidth, num)` — **not** a hardcoded
  `%4d`. A file with 1 000 lines needs a wider gutter, and the reference
  computes it.
- The marker column is **four** wide, and the added and removed bodies
  carry **one space more** than the unchanged body. The text must start in
  the same column in all three.
- An added or a removed body is padded to the **content width**
  (`width-numWidth-2`); an unchanged body is **not** padded. The padding is
  measured in runes against the **plain** text, never against the styled
  text — a styled text's escape sequences are not visible columns.
- The summary is **per change** and has **three** forms. A change that only
  adds lines reads `Added N lines`, not `+N / -0 lines`.
- A change shows **20 rows** at most, and the rest collapse into one
  `… and N more lines` marker.
- The inline preview's diff block is indented by **five** columns, so its
  text lines up under the `⎿` bullet's text.
- The `/diff` panel's rows must go through the same row model. The fold
  state (`internal/difffold`) is untouched by this change.

## 存疑

- **The changed gutter's foreground.** The 1.4.3 capture shows the line
  number in the **line-number grey** on the stripe. The 1.4.8 style table
  (cycle 323) says `DiffAddLineNum` is the **add** foreground on the add
  background and `DiffRemoveLineNum` the remove foreground on the remove
  background — the foregrounds disagree, the backgrounds agree
  (`#EEE8D5` is the solarized light scheme's `AddBg`, its `RemoveBg` and
  its `Border`). The clone keeps the capture's grey, because the capture
  is first-hand and the changelog's "untinted line-number gutter" agrees
  with it. If the 1.4.8 style table is the later truth, the changed
  rows' numbers should take the add and remove colours.
- **The stripe's extent in 1.4.8.** The 1.4.8 row renderer draws the
  gutter and the body as **two** `lipgloss` renders, and each ends with a
  full reset — so in 1.4.8 the changed row's background may cover **only
  the gutter**, where the 1.4.3 capture's background spans the whole row
  (`\x1b[K`). The clone keeps the capture's look, because the capture is
  the only rendering that was ever seen.
- **The body of an added row.** The capture shows the add colour
  (`#859900`); the 1.4.8 style table says `DiffAdd` is the *dim*
  foreground and `DiffRemove` is the tool foreground in bold. The clone
  keeps the capture's colours.
- **The truncation marker's indent.** 【推测】 — the source of the `%*s`
  width was not pinned.
- **The gutter's width.** The floor of `4` is pinned; the "digits" part
  is read as the largest line number the body prints.
- **The hint row.** The reference hangs the transcript's hint on the last
  row. The clone's changed rows are padded to the content width, so a
  changed last row would push the hint onto the next line; the clone keeps
  its own four-space spacer row for that case.
- **The create path.** The reference's create prints `Wrote N lines` (the
  1.4.3 capture) while the clone renders an addition-only diff preview.
  That difference predates this cycle and was left alone.

## Limitations

- No live agy session was available, so there is **no first-hand capture
  of the 1.4.8 rendering**. Every 1.4.8 statement above is 【反编译】 from
  the binary, and every first-hand statement is from the 1.4.3 capture.
- The reference's `/diff` file view has roughly **sixty** `DiffModel`
  methods; this cycle covers the *rows* only. The file view's own layout
  — the side-by-side arrangement, the file list, the header — is not
  reproduced.
- The four terminal tints the reference picks from the terminal's
  background brightness could not be read out of the binary's bss.
- The `palette` package was not modified, so the row model's colours come
  from the palette's current values rather than from the reference's
  named styles. A later cycle that maps the named styles
  (`DiffLineNum`, `DiffAddLineNum`, `DiffRemoveLineNum`, `DiffAdd`,
  `DiffRemove`) onto the palette's slots would let the row model ask for
  a style *by name*.
- `gofmt -l` is not clean for `internal/` and was not run over it; the
  files this cycle wrote are formatted.
