# The reference's chroma — deliverable

**Cycle:** `cycle-300-chroma-parity`
**Subject:** the clone's syntax highlighting is chroma-based and was already
ported; what was *not* matched was the **chroma version itself**. The
reference vendors upstream **v2.27.0** byte for byte; the clone pinned
**v2.24.1**, which ships four fewer XML lexers.
**Evidence:** 【一手】 the reference's own binary — the 353 chroma files
embedded in **agy 1.4.7** (`~/.local/bin/agy`, SHA-256
`b7a13e53c9f49921692676ef8edc2e60ceb67064c00bc732d9a364ec3ad75b32`),
recovered with `devutils/embedextract`, plus the Go module cache of the
upstream releases, compared file by file with sha256.

---

## 0. Summary

The user's question — *is syntax highlighting ported too?* — is **yes, and it
has been since cycle 233**, on two surfaces:

| Surface | Clone | Reference |
|---|---|---|
| fenced code blocks in the chat transcript | `internal/mdrender/chroma_inline.go` (cycle 233) | chroma, `terminal256`, `solarized-light` |
| syntax-coloured diff hunks | `internal/diffsyntax/syntax.go` | chroma token types |

What was *not* matched was the dependency's **version**. The reference ships
chroma's own XML assets inside its binary; comparing those files against
upstream releases identifies the version exactly:

| | Reference (agy 1.4.7) | Clone (before this cycle) |
|---|---|---|
| chroma version | **v2.27.0** (upstream, unmodified) | v2.24.1 |
| XML lexers embedded | 279 | 275 (the same 275, plus 4 more) |
| XML styles embedded | 74 | 74 (identical) |
| languages the clone cannot highlight | — | **Arturo, Gemfile.lock, LilyPond, Templ** |

Cycle 300 bumps the dependency to v2.27.0 and adds a fence so it stays there.

The bump is **invisible on the wire**: 30 common languages render
byte-identically under v2.24.1 and v2.27.0 (sha256 per language, §4). That
is exactly why it needs a fence of its own — a rendering regression test
cannot see it.

---

## 1. What the reference embeds

`devutils/embedextract -bin ~/.local/bin/agy -list` reports 472 embed-FS
records. 353 of them are chroma's:

```
   dir  embedded//
   315  google3/third_party/jetski/prompt/template_provider/templates/helpers/errors.tmpl
  4868  google3/third_party/jetski/prompt/template_provider/templates/step_strings/run_command.tmpl
  ...
```

The chroma files are split across two roots:

- **279 XML lexers** under the FS root `embedded/` — each one is a chroma
  lexer definition (`<lexer><config><name>ABAP</name>…`), i.e. the upstream
  `lexers/embedded/*.xml` tree.
- **74 XML styles** at the FS root — each one is a chroma style
  (`<style name="abap">…`), i.e. the upstream `styles/*.xml` tree.

The Go function names recovered from the binary (`rea-go-seeds`) confirm the
package is stock chroma v2, vendored under Google's third-party tree:

```
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/chroma            245
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/lexers/lexers    66
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/formatters/html  28
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/formatters/formatters  11
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/formatters/svg    8
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/styles/styles      1
google3/third_party/golang/github_com/alecthomas/chroma/v/v2/quick/quick        1
```

## 2. The version, identified by file set **and** by content

Comparing the reference's embedded files against the upstream module cache
(chroma v2.24.1, v2.25.0, v2.26.1, v2.27.0):

| Release | XML lexers | `arturo` | `gemfile_lock` | `lilypond` | `templ` |
|---|---|---|---|---|---|
| v2.24.1 | 275 | – | – | – | – |
| v2.25.0 | 278 | ✔ | ✔ | ✔ | – |
| v2.26.1 | 278 | ✔ | ✔ | ✔ | – |
| **v2.27.0** | **279** | ✔ | ✔ | ✔ | ✔ |

The reference has 279 → **v2.27.0**. The style set is identical to upstream's
in every release, so the styles alone do not discriminate; the four extra
lexers do.

The version claim is not a file-set guess. All 353 files were hashed and
compared against the upstream v2.27.0 tree:

```
checked: 353 mismatches: 0
```

**The reference vendors upstream chroma v2.27.0, unmodified.**

The reference's **IDE** binary (`language_server`) embeds **none** of these
(0 XML entries) — the chroma bundle is a CLI-only asset. The two binaries
must not be mixed here, per `docs/guide/environment.md`.

## 3. What the clone changes

`go.mod`: `github.com/alecthomas/chroma/v2 v2.24.1` → `v2.27.0`, which also
pulls in `github.com/dlclark/regexp2/v2 v2.2.1` (indirect — chroma v2.27.0
moved to `regexp2/v2`).

The clone's chroma surfaces do not change behaviour:

- `internal/mdrender/chroma_inline.go` still renders `terminal256` with
  `solarized-light`, the style byte-matched against the reference in cycle
  271.
- `internal/diffsyntax/syntax.go` still maps chroma token types onto the
  TUI's diff token classes.

What changes is **coverage**: `lexers.Get("templ")` and its three siblings
return a lexer instead of nil, so those fences stop falling back to
plaintext.

## 4. The rendering is unchanged — measured

30 language samples (Go, Python, TypeScript, Rust, JavaScript, C, C++, Java,
Ruby, Bash, JSON, YAML, HTML, CSS, SQL, Markdown, diff, Dockerfile,
Makefile, Terraform, Kotlin, Swift, PHP, Lua, XML, TOML, Go-template, …)
rendered with `quick.Highlight(…, "terminal256", "solarized-light")` under
both versions, one sha256 per language:

```
diff m24.txt m27.txt  →  ALL IDENTICAL
```

So the bump adds four languages and changes no existing byte. The clone's
own golden-style pins (`internal/mdrender/chroma_inline_test.go`,
`internal/diffsyntax/syntax_test.go`) still pass unchanged.

## 5. The fence

`internal/mdrender/chroma_reference_test.go` + `internal/mdrender/testdata/reference_lexers.txt`:

- the testdata file is the reference's 279 lexer file bases with the alias
  each one registers, recovered from the binary;
- `TestChromaReference_LexerCoverage` asserts every one of the 279 resolves
  in the clone's chroma, and that the list still has 279 entries (a shorter
  list means the reference moved — re-run the recovery, do not silently
  re-baseline);
- `TestChromaReference_NewlyAvailableLanguages` pins the four that the bump
  brought in;
- `TestChromaReference_AliasResolves` spot-checks four aliases.

The fence is load-bearing: with the old pin (v2.24.1) all four of the
"newly available" languages are `nil`, so the test fails.

## 6. 硬约束 (hard constraints)

- **The clone must keep chroma at v2.27.0.** The reference's binary contains
  upstream v2.27.0's exact bytes; a different release is a different
  highlight surface, and the four extra lexers are the visible part of it.
- **The style stays `solarized-light` with the `terminal256` formatter.**
  Cycle 271 pinned those bytes against a live 1.4.2 PTY capture
  (`ESC[38;5;166m` for `package`, `ESC[38;5;100m` for `func`). A chroma
  upgrade that changed the 256-colour quantisation would break that pin —
  v2.27.0 does not (measured, §4).
- **The clone does not need chroma's HTML or SVG formatters.** The reference
  links both (`formatters/html` 28 functions, `formatters/svg` 8), but the
  clone has no surface that renders code to HTML or to an SVG image. Until
  such a surface exists (a web UI, a markdown export, a "copy as image"
  feature), those formatters have no consumer.
- **`go.mod` / `go.sum` are shared with a second agent.** The chroma bump
  was staged as an isolated hunk; the concurrent protobuf promotion was left
  untouched.

## 7. 存疑 (open questions)

- **Why does the reference link the HTML and SVG formatters?** No
  user-visible string ("copy as image", "export as svg", …) was found in the
  binary, and the call graph was not walked, so the consumer of
  `formatters/svg` is unidentified. It may be a web-UI code renderer, a
  share-image feature, or a dependency pulled in by a service path the CLI
  never takes.
- **Which chroma style does the reference's *diff* view use?** The transcript
  fence is pinned to `solarized-light` by a PTY capture; the diff view was
  not captured. `internal/diffsyntax` maps token *types* to the TUI's diff
  token classes, so the colour source is the TUI's own palette, not chroma
  styles — but whether the reference does the same is unmeasured.
- **The 4 extra languages are the whole file-set difference today.** A
  future chroma release may change Go/Python lexer definitions; §4 shows the
  recent releases did not, but the fence in §5 only watches the XML lexer
  set, not the Go-defined lexers' behaviour.
- **`internal/diffsyntax` and `internal/mdrender` could drift apart.** Both
  consume chroma; a lexer change affects the diff colours and the fence
  colours at once, and only the fence side has a golden.

## 8. Limitations

- The version identification rests on the **CLI** binary's embed FS. The
  IDE binary was checked and carries no chroma bundle, so no cross-check
  against it was possible.
- The 30-language rendering matrix covers the *common* languages; the four
  new ones were checked for *resolution*, not for a byte comparison against
  the reference (the reference produces no artifact on this machine that
  contains a highlighted Arturo/Gemfile.lock/LilyPond/Templ block).
- No live TUI session was driven for this cycle; the change is a dependency
  bump plus tests, and the existing TUI test suite passes unchanged.
- The clone's chroma surface is the transcript fence and the diff view.
  The reference may highlight code in surfaces the clone does not have at
  all (the web UI, for instance), which this cycle does not address.
