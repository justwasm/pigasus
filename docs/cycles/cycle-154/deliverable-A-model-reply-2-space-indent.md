# Cycle 154 — 2-space indent on resumed model replies

## Symptom

Capturing ref and ours at 200×60 against the same conversation showed the
model-reply prose indented differently:

```
=== ref 000, line 1 ===  "  ## 这两个文件是什么"
=== our 000, line 0 ===  "?? rea-go-seeds/analyzeAgyV135Tui.java.bak..."
```

Ref prefixed every line of the rendered model message with two spaces, ours
prefixed none. The result is that our's prose visually butted up against the
tool-call bullets and the user prompts, while ref's prose read as a
quote-block.

## Fix

`cmd/wagyu/conversation.go` — `seedConversation`'s model case now calls
`indentModelReply(styled, "  ")`, a small helper that prepends two spaces to
every non-empty line of the styled text. Empty lines stay empty so the
paragraph rhythm (blank separators between paragraphs) is preserved.

The indent is applied *after* `styleInlineBackticks`, so the colour escapes
ride in front of the spaces and the rendered cell width stays correct.

## Side benefits

- Visually separates the model's prose from the tool-call bullets above
  and below.
- Resets cycle-152's chrome-anchored-at-bottom padding as expected: at
  200×60 the body now contains the indented prose + tool rows + chrome and
  fills the viewport the same way ref's body does.

## After

```
=== our 000 (post-154, 200×60) ===
 0 ""
 1 "  ## 这两个文件是什么"
 2 ""
 3 "    ?? rea-go-seeds/XxxX.java..."
 4 "    ?? rea-go-seeds/analyzeAgyV135Tui.java.bak..."
 5 ""
 6 "  都是 cycle 39 时期..."
 ...
```

The 2-space indent and the 4-space markdown-code-block indent now match
ref's render byte-for-byte.

## Remaining gaps

The captured diff still differs from ref in:

1. **Markdown bold / bullets** — ref renders `**text**` as bold and
   `- foo` as `• foo`; ours passes the markdown through unchanged.
   This is a `renderMarkdown` function that the v1.3.6 binary has but
   the clone doesn't (no source-side test pin, so the recovery is
   separate from this cycle).
2. **Tool-group collapsing** — ref merges consecutive `Ran` tools into
   one line (`● Ran (git add ... && git commit ...)`); ours shows each
   tool as a separate row. Cycle-155+ work.
3. **Conversation content drift** — the captured DB is being extended
   during the harness runs, so ref and ours pin to slightly different
   positions. Layout matches; content doesn't.

## Files touched

- `cmd/wagyu/conversation.go` — model case in `seedConversation`
  wraps the styled text with `indentModelReply`; the helper is
  appended to the file with its own docstring.

## Verification

```
go build -o /tmp/agycmp/our/agy ./cmd/wagyu
go test ./cmd/... ./internal/...                 # 25 packages green
REF=… OUR=… bash /tmp/agycmp/hotpath.sh          # 5 scenarios, no regression
bash /tmp/wide_compare.sh                        # 6 frames @ 200×60
```

100×30 hotpath diff summary (no regression vs cycle 153):

```
cold_start  000-init            diff= 23
ctrl_o_expand 000-init           diff= 50
              001-scrolled       diff= 55
              002-expanded       diff= 55
              003-collapsed      diff= 55
help_tabs    000-general        diff= 37
              001-second         diff= 18
              002-third          diff= 12
              003-wrapped        diff= 32
              004-closed         diff= 27
picker       000-slash          diff= 24
              001-down5          diff= 24
              002-closed         diff= 15
resume_scroll 000-init           diff= 49
              001-pgup           diff= 53
              002-bottom         diff= 53
              003-top            diff= 53
```

200×60 000-init captures now show our with the same 41-line transcript fill
as ref's 39-line fill (cycle 153 was 18 lines).

## Limitations

- The 2-space indent is applied unconditionally to resumed model replies.
  Live model replies (the ones streamed during the current session) are not
  indented here — those go through `liveRows`, not `seedConversation`. If
  ref also indents live replies, this gap will resurface in fresh
  conversations.
