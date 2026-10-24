# cycle 271 — AGY 1.4.2 markdown palette, width, blank rows, resumed transcript

## TL;DR

The clone's transcript was structurally close to `agy` 1.4.2 but not
byte-aligned. Four independent gaps, all closed here, each pinned to a PTY
capture of `agy --conversation=<id>` at 120×60:

1. **Palette.** `buildANSIConfig` used glamour defaults. The reference is
   glamour's `LightStyleConfig` with four overrides.
2. **Width.** The reply block spans `terminal width − 3` cells. The clone
   rendered at `width` and then wrapped it in an external 2-space indent.
3. **Blank rows.** Cycle 160 collapsed consecutive blank lines; the reference
   keeps them.
4. **Resumed transcript.** `agy` shows only model replies on resume; the
   clone echoed `> ` user rows.

After the change the 120×60 fixture render has **0 mismatched visible rows**
against `agy` (SGR palette matches apart from one cosmetic chroma-indent
grouping).

## 1. Palette

`buildANSIConfig` is now the recovered style verbatim: glamour's
`LightStyleConfig` plus four pinned overrides —

| Element | Value | Reference bytes |
|---|---|---|
| Document | colour `23`, margin `2` | prose/table/indent `38;5;23` |
| HorizontalRule | `"──────"` colour `249`, no surrounding newlines | six box-drawing horizontals |
| Code (inline) | `32` on `254`, no prefix/suffix | blue-on-grey chip |
| (everything else) | `LightStyleConfig` verbatim | H1 `228`-on-`63` chip, Heading `27` bold, H2–H6 `## ` prefixes, `• ` bullets, `│ ` quotes, `36`/`29` links |

The legacy marker-preserving shape (cycle 175: `# ` headings, `> ` quotes,
U+2421 bullet, `---` rule, 4-space fence) was split out into
`buildLegacyShapeConfig`, which `buildPlainConfig` now derives from. ANSI no
longer derives from it.

Fenced code is chroma **solarized-light** via the **terminal256** formatter
(was monokai/terminal16m, cycle 233's visual guess). The reference's Go bytes
— `package` = `ESC[1m ESC[38;5;166m`, `func` = `ESC[38;5;100m`, `main` =
`ESC[38;5;32m`, punctuation `242` — are exactly `solarized-light` under
terminal256 and no other embedded style matches.

`cInlineCodeOpen` in `cmd/wagyu/colors.go` was reordered to
`38;5;32` then `48;5;254` to match the reference's open order.

### OSC 8

glamour emits OSC 8 hyperlink tokens for every link; the 1.4.2 capture has
none (the URL is printed as underlined `36` text after the label). The ANSI
variant now strips OSC 8 (`osc8Re`) before returning. Explicit clickable
surfaces (`elig_inline.go`) still build OSC 8 through
`internal/render.Hyperlink`, so this only affects the transcript.

### Preserved newlines

The ANSI variant now sets `glamour.WithPreservedNewLines()`, previously
User-only. The reference does not collapse a lone `\n` between prose lines.

## 2. Width

`renderMarkdown` renders one column narrower than the caller's terminal
width, because `Document.Margin = 2` already supplies the two leading cells.
The old external `indentModelReply(renderMarkdown(...), "  ")` in
`seedConversation` and the turn-end path is gone; the margin owns the
indent. Net visible span: `width − 3` (one cell of glamour word-wrap slack
plus the two-cell margin).

A width sweep on a synthetic wrap fixture confirms paragraphs wrap at exactly
`width − 3` at 80 / 100 / 140, and code blocks are **not** pre-wrapped —
matching `agy`.

## 3. Blank rows

Cycle 160's "collapse consecutive blank lines" was written against the legacy
walker and ref 1.3.5. glamour emits two blank rows between a bullet list and
an ordered list that follows a single source blank, and the reference keeps
both. The collapse is removed. Blank rows are still normalised to a truly
empty string by `lineIsBlankANSI` (which skips SGR sequences and whitespace),
so a visually-blank row such as `"  \x1b[38;5;23m    \x1b[m"` does not paint
a full-width colour band.

## 4. Resumed transcript

`seedConversation` no longer appends a `> ` row for `user` steps. The message
is still pushed into `m.session` so the model sees the prior turn, but nothing
is rendered, and the separator blank is skipped too (the banner already ends
with one). This matches `agy`, which shows only the model reply steps when
resuming by id.

## Files

| File | Change |
|---|---|
| `internal/mdrender/glamour_adapter.go` | `buildANSIConfig` (new palette), `buildLegacyShapeConfig` split, OSC 8 strip, ANSI `WithPreservedNewLines` |
| `internal/mdrender/chroma_inline.go` | solarized-light, terminal256, 2-space block indent |
| `cmd/wagyu/conversation.go` | width − 1, blank-row preservation + `lineIsBlankANSI`, drop `indentModelReply`, hide user rows on resume |
| `cmd/wagyu/colors.go` | inline-code open order |
| `internal/mdrender/margin_restore_test.go` | margin propagation regressions |
| `internal/mdrender/cycle268_width_test.go`, `glamour_test.go`, `chroma_inline_test.go` | updated pins |
| `cmd/wagyu/conversation_test.go`, `continue_test.go` | updated pins |

## Verification

```
$ go test -count=1 ./cmd/... ./internal/...
RC=0   (63 packages)
```

Fixture parity at 120×60: **0 mismatched visible rows**. SGR palette matches;
the only diff is a cosmetic chroma-indent grouping (the clone emits the two
indent spaces outside the document-colour run, `agy` emits them inside).
Visually identical.

## 硬约束 / 存疑 / limitations

- **Status-bar colour/label** still differs from `agy` and is deferred.
  `cDimGrayOpen` (`\x1b[38;2;95;99;104m`) is used in 97 places; changing it
  globally for the status-bar `66` was judged too broad for this cycle.
- The cosmetic chroma-indent SGR grouping diff is not fixed. It has no
  visible effect, and forcing byte equality there means threading the
  document-colour run through chroma output, which the strip/restore
  architecture deliberately keeps separate.
- `renderMarkdown`'s `width − 1` assumes `Document.Margin = 2`. If the margin
  ever changes, this constant must move with it. There is no assertion tying
  the two together.
- The palette was recovered from **one** capture (120×60, light terminal,
  `agy` 1.4.2). A dark-terminal or different-`COLORTERM` capture could reveal
  a variant; none was attempted.
- `lineIsBlankANSI` treats a row of only spaces/tabs/CR and SGR as blank. A
  row carrying an OSC 8 or other non-SGR zero-width sequence would not be
  recognised as blank; none was observed in practice.
