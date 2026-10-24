# Cycle 174 — deliverable B: mdrender wires into /learn and /goal echoes

## Scope

Cycle 174 wires the cycle-168 `internal/mdrender` primitive into the
TUI slash commands that echo user-typed text back into the
transcript (`/learn`, `/goal`). Before cycle 174 a markdown-flavoured
note showed the raw `**` / `*` markers; after cycle 174 the echo
goes through `mdrender.RenderANSI` so the TTY shows bold / italic
emphasis instead.

This deliverable is *B* because cycle 174 is also where the user
shipped deliverable *A* (`deliverable-A-in-app-text-selection.md`).
The two share a cycle number by coincidence — the user picked 174
for their in-app text selection work, and we kept that number for
the mdrender wire-in rather than renumbering.

## What changed

| File | What | Net |
|---|---|---|
| `cmd/wagyu/render_user_text.go` (new) | `renderUserText(s)` — thin wrapper over `mdrender.RenderANSI`, strips the trailing newline the renderer appends | +51 |
| `cmd/wagyu/render_user_text_test.go` (new) | 12 unit / integration tests for the helper and `/learn` / `/goal` | +237 |
| `cmd/wagyu/slash_commands.go` | `/learn` and `/goal` echoes routed through `renderUserText` | +9 / −7 |
| `internal/mdrender/mdrender.go` | Two bug fixes to the cycle-168 walker (see below) | +55 / −7 |

## The two mdrender bugs found while testing the wire-in

### Bug 1 — Emphasis walker put ANSI on the marker, not the content

The cycle-168 walker emitted:

```go
case *ast.Emphasis:
    if entering {
        if v.Level == 2 {
            wk.emit(wk.ansi(ansiBold, "*"))   // wraps the marker byte
        } else {
            wk.emit(wk.ansi(ansiItalic, "_"))
        }
    } else {
        if v.Level == 2 { wk.emit("*") } else { wk.emit("_") }
    }
```

So `**Important**` came out as `\033[1m*\033[22mImportant*` — the
bold span wrapped a single `*` byte, the emphasised text itself was
*not* bold. Visible to the user only as a faint dot before the word.

**Fix** — emit the open ANSI *before* the marker and the close
*after* the marker, so the span brackets the entire content:

```go
if entering {
    if v.Level == 2 {
        wk.emit(wk.ansiOpen(ansiBold))   // "\033[1m"
        wk.emit("*")
    } ...
} else {
    if v.Level == 2 {
        wk.emit("*")
        wk.emit(wk.ansiClose(ansiBold))  // "\033[22m"
    } ...
}
```

New helpers `ansiOpen(kind)` / `ansiClose(kind)` return just the
half-codes so the walker can bracket without `ansi(...)` having to
take an empty string and emit `[bs, ""]` (which would have produced
`\033[1m\033[22m` with nothing in between).

Output is now `\033[1m*Important*\033[22m` — the TTY renders
"Important" in bold (the marker byte also gets bolded, which is
invisible since it is just a delimiter).

### Bug 2 — `firstPara` defaulted to false

The walker has a field with a doc comment "true until the first
block-level emit happens" but the struct literal never initialised
it, so Go's zero value (`false`) took over. Consequence: every
output started with a stray `\n` because `emitLeadingBlank()`
emitted one (it returns early when `firstPara` is true).

```go
wk := &walker{
    out:    buf,
    colour: colourMode,
    source: source,
    // firstPara: true — added in cycle 174
}
```

Output before fix: `\nplain text\n` (leading newline).
Output after fix: `plain text\n` (only trailing newline).

The existing `TestRender_HasTrailingNewline` only checked the
*trailing* newline so the bug slipped through.

## Behaviour preserved (cycle-167 byte parity)

For plain text — i.e. any input without markdown markers — the
output is byte-identical to the pre-cycle-174 echo:

* `/learn api endpoint is down` →
  `"  Captured learning: api endpoint is down"` (unchanged)
* `/goal ship the v2 feature` →
  `"  Goal mode: ship the v2 feature"` (unchanged)

The cycle-167 snapshot tests that pin these echoes still pass, so
no downstream TUI consumer (the wrap cache, the screen reader, the
copy-to-clipboard action) sees a different byte stream.

## What's deliberately not in cycle 174

* **Tool-result detail rendering** — `stepDetailLines` in
  `cmd/wagyu/tui_tools.go` shows raw tool output (e.g. `cat
  README.md`). Routing it through `mdrender.RenderANSI` would
  change every snapshot that pins a tool-result panel, and the
  detection of "is this markdown" is non-trivial. Deferred.
* **`/help` overlay markdown** — the help panel is rendered
  through dedicated ANSI builders (`renderHelpGeneral`,
  `renderHelpCommands`, `renderHelpShortcuts`) whose output is
  pinned to the v1.3.6 reference at the byte level. Wire-in here
  would require either routing the *content* of those panels
  through mdrender (changing the captured bytes) or rendering a
  side panel (large UI work). Deferred.
* **Cycle-155 `renderMarkdown` in `cmd/wagyu/conversation.go`** —
  that function applies the v1.3.6 reference's three transforms
  (bullet conversion, bold styling, 4-space code-block indent) to
  model replies. It is *closer* to what mdrender does now but is
  still distinct (it converts `- ` to `• `, which mdrender does
  not). Replacing it with mdrender is a bigger cycle (cycle 175
  candidate).

## Tests added (12 cases)

* `TestRenderUserText_Empty`
* `TestRenderUserText_PlainText` (3 sub-cases)
* `TestRenderUserText_Bold`
* `TestRenderUserText_Italic`
* `TestRenderUserText_Heading`
* `TestRenderUserText_NoLeadingNewline` (cycle-174 fix)
* `TestRenderUserText_NoTrailingNewline`
* `TestRunLearn_RendersMarkdown`
* `TestRunLearn_PlainTextRoundTrip`
* `TestRunGoal_RendersMarkdown`
* `TestRunGoal_PlainTextRoundTrip`
* `TestRunGoal_ClearedUnchanged`

Plus the walker-fix tests inside `internal/mdrender` that pin the
new emphasis-span output shape (already covered by the package's
existing tests).

## Hard constraints / limitations

* `mdrender.RenderANSI` emits only the four unambiguous SGR codes
  the package documents (`\033[1m` / `\033[22m` bold open/close,
  `\033[3m` / `\033[23m` italic open/close). Anything more
  elaborate (24-bit colour, background colours, link styling) is
  deliberately left for a future cycle.
* The user's text is rendered *as Markdown* by the walker; there
  is no opt-out. A user who genuinely wants literal `**` in their
  /learn echo has to escape it (`\*\*`). This matches the
  reference's behaviour — model output also goes through Markdown
  rendering — but is worth flagging.
* The walker fix is technically a behaviour change for
  `mdrender.RenderANSI`'s emphasis output. All callers (just
  `login_prompt` in `internal/cli` and the new
  `renderUserText` in `cmd/wagyu`) handle the new shape
  correctly. No external API change.