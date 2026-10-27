# Cycle 358 deliverable: the last two leaves, and the queue is empty

**Cycle:** `cycle-358-public-tranche-5`
**Subject:** the fifth and last public tranche — `vim` and `imagery` moved
out of `internal/` to the top of the tree — plus the cleanup the move
uncovered.
**Evidence:** 【一手】 first-hand against the working tree (the moves, the
dependency closure, the tests, a token-level equivalence proof over all
five tranches) · 【反编译】 n/a — no binary was disassembled this cycle ·
【strings】 n/a · 【推测】 the ROI weights are a judgement, and the guide
says so

---

## 0. The short answer

| | before | after |
|---|---|---|
| the modal editing engine | `internal/vim` | **`vim/`** |
| the image downscaler | `internal/imagery` | **`imagery/`** |

4,437 LOC. Two packages, one commit, and with it the **queue is empty**:
every package the ROI ranking scored is now published.

Two things came out of the move that the earlier tranches did not
anticipate, and both are worth a paragraph each — a **relative path**
that stopped being true, and **thirty import blocks** this work had left
unsorted two and three tranches earlier.

## 1. Why these two, and why they were cheap

Both were already leaves — `go list -deps` showed no `internal/`
dependency for either — so the move needed no dependency surgery:

```bash
go list -deps ./vim ./imagery | grep pigasus/internal
# nothing
```

They were ranked in Tier 2 (capability, V=2, B=2, E=0, ROI 4.0) because
both are **pure functions with no clone vocabulary in them**:

- `vim` is the modal editing engine behind the clone's prompt editor
  (*"Editor Mode › Vim"*). Its own doc comment says it: *"a pure,
  dependency-light state machine: the host owns the terminal widget and
  hands the engine one normalized key at a time … Nothing here knows about
  bubbletea, lipgloss or the run loop."* A vim-mode engine for a terminal
  editor is a thing any Go program with a text field would want.
- `imagery` scales raster images down to a per-side ceiling so that large
  images fit a model's image-size limits. It detects PNG / JPEG / WebP by
  magic bytes and re-encodes. It is a capability, not a contract — the
  ceiling (2,576 px) is the clone's answer to the reference's 1.2.4
  changelog line, not a spec.

## 2. The finding: a path change can break a *relative* path

`internal/clipboard` read its WebP fixtures through a relative path:

```go
data, err := os.ReadFile(filepath.Join("..", "imagery", "testdata", name))
```

From `internal/clipboard/`, `..` is `internal/`, so `../imagery/testdata`
resolved. The moment `imagery` moved to the top of the tree, that path
resolved to `internal/imagery/testdata` — which no longer exists. The
test caught it:

```
--- FAIL: TestPasteWebPFromClipboardAccepted
    read fixture ../imagery/testdata/small.webp: no such file or directory
```

The fix is two more `..` levels, and the comment above it now says why.

**The lesson is worth keeping:** an import-path rewrite is not the whole
of a move. A tree-wide scan for relative escapes into a moved package is
part of the move. The scan found exactly one, and it was in a *test*,
which is where a relative path is most likely to hide.

## 3. The finding: thirty import blocks this work had left unsorted

A path change moves a line in an import block, and the block has to be
re-sorted — `github.com/justwasm/pigasus/render` sorts *before*
`github.com/justwasm/pigasus/internal/…`, where `internal/render` sorted
after it.

Earlier tranches formatted the files that were gofmt-clean at HEAD and
left the ones that were already dirty. That was a mistake of scope: the
files that were *already* dirty at HEAD were often dirty **because of an
earlier tranche of this same work** — the import block had been moved
and never re-sorted, and the file's own doc-comment noise made it look
pre-existing.

A tree-wide scan for files whose *only* gofmt complaint is an import
line found **30** of them, spread over T2's, T3's and T4's changes. All
30 were re-sorted; the scan now prints nothing:

```
$ for f in $(git ls-files '*.go'); do
    gofmt -d "$f" | grep '^[+-]' | grep -v '^[+-][+-]' | grep -v '^[+-][[:space:]]*$' \
      | grep -qvE '^[+-][[:space:]]*(_ |[a-zA-Z0-9_]+ )?"[^"]*"$' && echo "$f"
  done
$ # nothing
```

Four of the thirty were files another agent is editing. Their index
entries carry the re-sort and nothing else; their in-flight work stays
in the worktree.

## 4. A mistake, and its correction

The rewrite rule for this tranche was written with a **double** backslash
in the replacement:

```sed
s|internal/vim([^a-zA-Z0-9_])|vim\\1|g
```

BSD sed took `\\1` as a literal backslash and a `1`, so every match became
the text `vim\1` and the captured character was lost. Nine lines across
seven files were mangled; the compiler caught it at once:

```
internal/clipboard/paste.go:38:2: string literal not terminated
```

The repair was surgical — each of the nine sites had a known original
trailing character (a quote, a slash, a space) and was put back
explicitly — and the result was verified line by line against HEAD before
anything was staged. The lesson is the same one the staging mistake in
T3 taught: **verify the output, not the intent.** A one-character
difference between `\1` and `\\1` is invisible in a command line and
fatal in a file.

## 5. The move

| | count |
|---|---|
| packages moved | 2 |
| LOC moved | 4,437 |
| files renamed | 22 (17 `vim`, 2 `imagery`, 3 fixtures) |
| paths in the commit | 59 |
| files shared with another agent, staged by filtered patch | 7 |
| files re-sorted by the import sweep | 30 |

`imagery`'s `testdata/` moved with it — the WebP fixtures and the golden
file are the package's own evidence, exactly as `lsproto/descriptor/`
moved with `lsproto` in T2.

## 6. The shared tree

Seven files in this tranche are among the ones another agent is editing:

```
internal/builtinsubagent/image_generator_test.go   (a comment)
internal/cliapp/tui_active_items.go                (an import, re-sorted)
internal/cliapp/tui_run.go                         (an import, re-sorted)
internal/cliapp/tui_run_test.go                    (an import, re-sorted)
internal/cliapp/vim_mode.go                        (an import + a comment)
internal/clipboard/paste.go                        (an import)
internal/clipboard/paste_test.go                   (an import + the fixture path)
```

Each index entry was built from HEAD rather than from the worktree, so
the commit carries only this work's hunks:

```bash
git show HEAD:<file> > base.go
# apply this work's edit to the copy
gofmt -w base.go
diff -u base.go mine.go | git apply --cached
```

For `internal/clipboard/paste_test.go` the base was unformatted at HEAD,
so the import block was spliced in rather than the whole file being
formatted — a filter that keeps a formatting fix from dragging a
doc-comment rewrite into the commit.

The other agent's 63 in-flight files were verified to contain none of
this work's hunks and left unstaged. Their two reference-drift artifacts
(`internal/tools/testdata/tool_variants_1.4.7.json` and
`internal/tools/variants_test.go`, a re-derivation of the tool-variant
addresses against agy 1.4.8) were also found staged during this cycle
and **unstaged**: they are not this work, and committing them without
regenerating `internal/referencedrift/reference.lock.json` would have
broken the drift lock in the repository's own history.

## 7. The token-level proof, across all five tranches

The same mechanical check that proved T4 now covers everything: for each
moved file, take the version from before its tranche, apply the
import-path substitution the move implies, strip the whitespace, and
compare against the **index**.

**156 of 157 files are token-identical.** The one exception is
`remotetransport/tcp_connectstream_test.go`, whose difference is the
*order* of two import lines — gofmt's doing, because their sort position
changed with the path.

| tranche | packages | LOC | .go files checked | identical |
|---|---|---|---|---|
| T1 | 4 | 40,872 | 21 | 21 |
| T2 | 5 | 18,104 | 39 | 38 (+1 import order) |
| T3 | 6 | 6,579 | 36 | 36 |
| T4 | 6 | 10,565 | 42 | 42 |
| T5 | 2 | 4,437 | 19 | 19 |
| **total** | **23** | **80,557** | **157** | **156 + 1 import order** |

The LOC column counts the files **as committed**, not as they were before
their move: `gofmt` re-wrapped two expressions in `render` and re-indented
nine doc comments in `grpc`, which is seven lines more than the ranking's
pre-move figures. The token check below proves that none of those seven
lines is a behaviour change.

The seven generated `localharness/pb/` files are **inside** the 21, not
excluded from it: they moved verbatim, and cycle 354 already proved
their descriptors unchanged. The non-Go files that moved alongside the
code — `lsproto/descriptor/` (a README and a `.protoset`) and
`imagery/testdata/` (two WebP fixtures and a golden file) — are five
files that are not Go and are not token-compared; they moved with the
package that describes them.

## 8. What did not change

- **The editing engine.** The parser, the motions, the text objects, the
  operator ring, the undo stack, the count multipliers from AGY 1.2.9 —
  all byte-identical. The only formatting change in the package is one
  line of operator spacing in `motion_word.go` (`s.lineLen(from.row) - 1`
  → `s.lineLen(from.row)-1`), which gofmt wanted and which the token
  check confirms is the only difference.
- **The downscaler.** The magic-byte detection, the per-side ceiling, the
  PNG / JPEG / WebP re-encode paths, the sentinel errors — unchanged.
- **The clipboard.** Its behaviour is unchanged; only the *path* it reads
  its fixtures from moved.

## 9. Verification 【一手】

```
go build ./cmd/... ./internal/... ./devutils/... (and the 24 public packages)  # clean
go test  -count=1 ./cmd/... ./internal/... (and the 24)                        # all green
gofmt -l vim/ imagery/                                                        # clean
go list -deps <the 24 public packages> | grep pigasus/internal                 # empty
```

`internal/referencedrift` is excluded from the run above: its lock
expects the sha256 of the *committed* `tool_variants_1.4.7.json`
(`ea46a029…`), and the worktree carries another agent's uncommitted
re-derivation of that fixture (`b68ad4ea…`) whose lock has not been
regenerated. The committed tree passes.

`internal/daemon`'s `TestShutdownSIGKILLsSurvivors` failed once in a
full-suite run and passed on three consecutive re-runs: a timing-sensitive
test about SIGTERM escalation, with no dependency on any moved package.

## 10. Open questions

- **`internal/mcpoauth` is still dead** — 1,477 LOC plus 829 LOC of
  tests, zero references anywhere in the tree. It is a **deletion**
  candidate, and deleting it is worth more than any move. It was left
  alone because the AGY 1.3.4 changelog line it answers
  (*"Fixed remote MCP servers that use OAuth signing you out after a
  temporary …"*) may describe behaviour the clone still owes a user.
- **The three `*_pb` descriptor packages stay internal** while their
  working copies are now public. The split is deliberate — one is the
  supported surface, the others are the evidence — but
  `internal/codeium_common_pb` (4,205 LOC), `internal/extension_server_pb`
  (2,746) and `internal/language_server_pb` (409) still have **zero**
  importers.
- **`grpc` is a generic name for a public package.** A consumer importing
  both `github.com/justwasm/pigasus/grpc` and `google.golang.org/grpc`
  has to alias one. The name was kept because it matches the reference's
  own vocabulary, but a rename is still cheap — it is one before anyone
  depends on it.
- **The public surface is now 24 packages.** Every one of them is a
  compatibility promise from here on.

## 11. Limitations

- **No external consumer has imported any of them.** The value axis of
  the ranking is estimated from the nature of each contract, not measured
  from usage. Five tranches of work have produced 80,557 LOC that no
  outsider has yet compiled against.
- **The token-level proof covers the moved files, not their callers.**
  The callers are covered by the test suite, which is green, but a
  caller's behaviour change would need its own evidence.
- **The import-order sweep touched thirty files** across three earlier
  tranches' worth of paths. Each is a two-to-four-line reorder of an
  import block, and each was applied because this work moved the line —
  but the diff is wider than a pure move.
- **Seven files in this tranche are shared with another agent.** Their
  index entries carry this work's hunks only; the trees are consistent
  only once both agents have committed.
- **The historical cycle reports still name the old paths**
  (`internal/vim/…`, `internal/imagery/…`). That is deliberate: a cycle
  report is the evidence of what was true at the time, and the guide is
  the current state.

---

Reports: [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md)
(the current state and the ranking),
[`cycle-357-public-tranche-4`](../cycle-357-public-tranche-4/deliverable.md)
(the T4 move),
[`cycle-356-public-tranche-3`](../cycle-356-public-tranche-3/deliverable.md)
(the T3 move),
[`cycle-355-public-tranche-2`](../cycle-355-public-tranche-2/deliverable.md)
(the T2 move),
[`cycle-354-public-packages`](../cycle-354-public-packages/deliverable.md)
(the T1 move and the ranking),
[`cycle-353-cloudcode-dialect`](../cycle-353-cloudcode-dialect/deliverable.md)
(the first public package).
