# Cycle 1 — chroma/v2 for /diff syntax highlighting

**Branch:** `feature/package-simplification`
**Commit:** `29fe714`
**AGY alignment:** AGY 1.3.5 changelog line 3 (Aurora-Dark syntax
highlighting in /diff file panel — `strings ~/.local/bin/agy | grep
chroma` confirms chroma is the engine in the reference binary).

## What changed

`cmd/wagyu/diff_syntax.go` was rewritten on top of `chroma.Coalesce`
+ `lexers.Match`. The ~625-line hand-rolled tokenizer (5-language
keyword sets, comment walk, string scanner, identifier classifier,
language dispatch table) was replaced by ~205 lines that:

1. Look up a chroma lexer by filename (chroma matches ~270 lexers by
   Filenames / AliasFilenames glob)
2. Run `chroma.Coalesce(lex).Tokenise(nil, text)` to merge adjacent
   same-type tokens
3. Map each chroma `TokenType` to one of our 5 `diffTokenClass`
   values via `chromaToDiffClass` (uses `SubCategory()` for the
   bucket test, exact-type overrides for `type`)
4. Re-coalesce by mapped class so chroma's `KeywordType` (Go's
   `string`) and `NameBuiltin` (Rust's `Vec`) collapse into a single
   `diffType` token instead of two

The public surface (`diffTokenClass`, `diffToken`, `tokenizeDiffLine`,
`diffTokenOpen`, `renderDiffInlineTokenStream`, `renderDiffInlineLine`)
is unchanged — every consumer site (panelDiffInlineLines,
slash_diff_fold.go) keeps working.

`go.mod` now lists `github.com/alecthomas/chroma/v2 v2.24.1` as a
direct dep (was already present as indirect via the existing charm
TUI stack).

## Test counts

| Test | Status | Notes |
|---|---|---|
| `TestTokenizeDiffLine_Go_Keyword` | kept | regression gate |
| `TestTokenizeDiffLine_Go_Comment` | kept | regression gate |
| `TestTokenizeDiffLine_Python_Docstring` | kept | regression gate |
| `TestTokenizeDiffLine_PlusPrefix` | kept | diff-marker contract |
| `TestTokenizeDiffLine_UnknownExt_Plain` | updated | `.json` / `.md` cases removed — chroma now recognises them |
| `TestTokenizeDiffLine_DiffMetadataArePlain` | kept | framing-row contract |
| `TestTokenizeDiffLine_RustTypeAndKeyword` | kept | type-table regression gate |
| `TestTokenizeDiffLine_EmptyLine` | kept | empty-input contract |
| `TestRenderDiffInlineTokenStream` | kept | renderer wiring |
| `TestTokenizeDiffLine_JSON_Numbers` | **new** | JSON lexer lights up numbers |
| `TestTokenizeDiffLine_MarkdownHeading` | **new** | markdown lexer recognises headings |
| `TestTokenizeDiffLine_BashComment` | **new** | Bash lexer recognises `#` comments |
| `TestTokenizeDiffLine_YAMLKey` | **new** | YAML lexer round-trip |
| `TestTokenizeDiffLine_NoFilenameIsPlain` | **new** | commit-view empty-filename fast path |

13 tests in `diff_syntax_test.go` (was 9). All pass.

Full test suite (`./cmd/wagyu/ ./internal/...`): same two pre-existing
flakes as before the change — `TestDispatchPrintSlash_LocalCommands`
(model default) and `TestChangelog_MatchesReferenceBinary` (agy
binary drift). No new failures.

## Language count

Before: 5 (Go / Python / JavaScript / TypeScript / Rust).
After: **270+** — every lexer in chroma's embedded catalogue plus the
hand-written lexers (Go, Markdown, HTML, PHP, ...). For
free-languages relevant to a developer CLI:

`.json` · `.yaml` · `.toml` · `.md` · `.sh` / `.bash` · `.sql` ·
`.css` · `.html` · `.xml` · `.lua` · `.vim` · `.dockerfile` · `.tf` ·
`.proto` · `.ini` · `.cfg` · ...

## Chroma TokenType → diffTokenClass map (verbatim)

The exact-match overrides are evaluated first (they catch the cases
where chroma splits what would otherwise be a `keyword` or `plain`
token into our `type` bucket):

| chroma `TokenType` | `diffTokenClass` | Why |
|---|---|---|
| `KeywordType` | `diffType` | Go's `string` / `int` / `error` (and every language's primitive type names) are tagged `KeywordType` by chroma — promoting them to `type` makes Go's `func Foo() string { ... }` highlight `string` in yellow. |
| `NameClass` | `diffType` | User-defined class names (`type Foo struct { ... }`). |
| `NameBuiltin` | `diffType` | Rust's `Vec` / `Option` / `Result` / `Box` / `Arc` / `String` are `NameBuiltin` — the changelog-screenshot highlight. |
| `NameBuiltinPseudo` | `diffType` | `self` / `Self` / `crate` style builtin pseudo-ids; the changelog sometimes paints these the same colour as real builtins. |
| `NameVariableClass` | `diffType` | Generic type parameters like Rust's `T` / `U`. |
| `NameVariableMagic` | `diffType` | `__name__` / `__file__` style magic ids. |
| `LiteralStringSymbol` | `diffType` | Symbol literals (Ruby `:foo`) — same colour as types in the Aurora-Dark scheme. |

Then the `SubCategory()` switch covers everything else:

| chroma `TokenType.SubCategory()` | `diffTokenClass` | Covers |
|---|---|---|
| `Keyword` (1000) | `diffKeyword` | `Keyword`, `KeywordConstant`, `KeywordDeclaration`, `KeywordNamespace`, `KeywordPseudo`, `KeywordReserved` |
| `LiteralString` (3100) | `diffString` | `LiteralString`, `LiteralStringBacktick` (Go raw strings), `LiteralStringDouble`, `LiteralStringSingle`, `LiteralStringHeredoc`, `LiteralStringDoc` (Python docstrings), `LiteralStringRegex`, `LiteralStringInterpol`, `LiteralStringEscape`, ... |
| `LiteralNumber` (3200) | `diffNumber` | `LiteralNumber`, `LiteralNumberBin`, `LiteralNumberFloat`, `LiteralNumberHex`, `LiteralNumberInteger`, `LiteralNumberOct` |
| `Comment` (6000) | `diffComment` | `Comment`, `CommentSingle` (`// ...`), `CommentMultiline` (`/* ... */`), `CommentHashbang` (`#!`), `CommentPreproc`, `CommentSpecial` (PHP / Ruby `# ...`) |
| everything else | `diffPlain` | `Name`, `NameFunction`, `NameAttribute`, `NameDecorator`, `Operator`, `Punctuation`, `Text`, `TextWhitespace`, `Error`, `Generic`, ... |

The 5-class mapping is intentionally lossy on purpose — chroma
distinguishes 80+ token types but the Aurora-Dark palette the
reference ships has 5 syntax slots (`#a626a4` magenta, `#50a14f`
green, `#986801` amber, `#a0a1a7` grey, `#e5c07b` yellow — see
colors.go). Anything outside those 5 slots collapses to plain so the
visible output matches the AGY changelog screenshot's colour count.

## File / LoC delta

```
cmd/wagyu/diff_syntax.go      | 628 ++++++++---    (now 205 lines)
cmd/wagyu/diff_syntax_test.go | 169 ++++++++---    (now 297 lines, +4 tests)
go.mod                        |   1 +             (chroma promoted indirect -> direct)
go.sum                        |   6 +             (chroma + assert + repr hashes)
```

Net diff: 322 insertions, 482 deletions → **-160 LoC** (160 lines
of hand-rolled code removed from the file that already contained
the cycle diff-syntax-highlight deliverable).

## Binary size delta

| Build | Bytes | Note |
|---|---|---|
| baseline (`/tmp/wagyu-baseline`, pre-cycle) | 73,767,010 | same source as `main` |
| after cycle 1 (`/tmp/wagyu-pkg`) | 76,593,186 | +2,826,176 bytes (+3.83%) |

+2.7 MB is the size of chroma's 270 embedded XML lexer definitions
after Go's `embed.FS` packages them as a single data blob. No
non-lexer code growth (chroma's own .go sources are already small
compared to the embedded catalogue).

## AGY alignment wins

- chroma is what the reference binary uses — `strings ~/.local/bin/
  agy \| grep chroma` returns the chroma init symbol plus the five
  Aurora hex tokens (recovered in cycle diff-syntax-highlight's
  report). The clone now drives the same library for the same job.
- The 5 Aurora-Dark colours in `colors.go` are unchanged — they're
  AGY's palette, chroma doesn't dictate colour, we colour from our
  own `cSyntax*Open` constants.

## Hard constraints / limitations

- Chroma's lexer registry is loaded lazily on first `lexers.Match`
  call; first render of a /diff file view pays a one-time ~50 ms
  cost (measured by hand on this machine). Subsequent calls are
  amortised.
- The 5-class bucket is intentionally coarse — chroma distinguishes
  e.g. `NameFunction` vs `NameClass` vs `NameDecorator`. If a future
  cycle wants more colours, the change is in `chromaToDiffClass` and
  `colors.go` (new `cSyntax*Open` constant + render wiring), not in
  the token walk.
- `lexers.Fallback` (the catch-all "match any filename" lexer) returns
  nil from `lexers.Match` rather than itself; we treat nil as
  "unknown extension, render plain". This is identical visible output
  to using `chroma.Fallback` (which would emit one Text token covering
  the whole line) and avoids one allocation per call.
- JSON / YAML keys are tagged `NameTag` by chroma and we map that to
  `diffPlain` (not `diffKeyword`) — we don't want YAML `name: foo` to
  colour the `name` key in magenta. If a future cycle wants JSON
  keys lit up, the change is one line in `chromaToDiffClass` (add
  `chroma.NameTag: return diffKeyword`).