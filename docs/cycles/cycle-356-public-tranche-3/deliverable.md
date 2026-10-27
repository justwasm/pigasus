# Cycle 356 deliverable: the library tranche becomes public

**Cycle:** `cycle-356-public-tranche-3`
**Subject:** the third public tranche — six **general-purpose libraries**
moved out of `internal/` to the top of the tree.
**Evidence:** 【一手】 first-hand against the working tree (the moves, the
dependency closure, the tests, the staged patches) · 【反编译】 n/a — no
binary was disassembled this cycle · 【strings】 n/a · 【推测】 the ROI
weights are a judgement, and the guide says so

---

## 0. The short answer

| | before | after |
|---|---|---|
| the markdown renderer | `internal/render` | **`render/`** |
| the diff highlighter | `internal/diffsyntax` | **`diffsyntax/`** |
| the true-colour palette | `internal/palette` | **`palette/`** |
| the retry helper | `internal/retry` | **`retry/`** |
| the per-turn retry cap | `internal/retrybudget` | **`retrybudget/`** |
| the atomic file write | `internal/atomicfile` | **`atomicfile/`** |

6,577 LOC of library that has nothing to do with Antigravity and
everything to do with being reusable.

## 1. Why this tranche, and why it is the cheapest one so far

The first two tranches moved **contracts** — things another implementation
has to match byte for byte. This one moves **capabilities**: a markdown
renderer, a diff highlighter, a palette, two retry helpers and an atomic
file write. They are reusable by anyone, and their value does not depend
on this repository reproducing the reference.

It was also the cheapest move of the three:

| | T1 (SDK) | T2 (Web UI) | T3 (library) |
|---|---|---|---|
| packages | 4 | 5 | 6 |
| LOC | 40,872 | 18,104 | 6,577 |
| files rewritten | 28 | 81 | 79 |
| generated code in the set | 33,980 LOC | none | none |
| shared files staged by patch | 0 | 3 | 1 |

Every one of the six was already a **leaf** — `go list -deps` showed no
`internal/` dependency for any of them — so the move needed no
dependency surgery, and the only import the tranche had to rewrite
anywhere was `palette`'s, at 38 importers.

## 2. The rule, measured

```bash
go list -deps ./render ./diffsyntax ./palette ./retry ./retrybudget ./atomicfile \
  | grep pigasus/internal
# nothing
```

Across the whole public surface, sixteen packages:

```bash
go list -deps ./geminicode ./localharness/... ./lifecyclehooks ./budget ./harnessprompt \
             ./lsproto/... ./rctoken ./originpolicy ./endpoints ./remotetransport \
             ./render ./diffsyntax ./palette ./retry ./retrybudget ./atomicfile \
  | grep pigasus/internal
# nothing
```

## 3. The move

| | count |
|---|---|
| packages moved | 6 |
| LOC moved | 6,577 |
| files whose import path was rewritten | 79 |
| renames recorded by git | 36 |
| paths in the commit | 110 |

The rewrite was one `sed` over the tree with a **word-boundary** guard,
because `internal/retry` is a prefix of `internal/retrybudget`:

```sed
s|internal/retrybudget([^a-zA-Z0-9_])|retrybudget\1|g
s|internal/retry([^a-zA-Z0-9_])|retry\1|g
```

The `retrybudget` rule has to come first, or the shorter one eats the
longer package's import path. Two comment references ended a line with
`internal/render` and had no trailing character for the boundary to
capture; those were rewritten by a separate end-of-line rule.

## 4. Two files were already unformatted — one pair fixed, one pair left

`gofmt -l` flagged four files in the tranche. All four were unformatted
at HEAD, and the two answers were different because of how big the diff
would be:

| file | what gofmt wanted | diff size | decision |
|---|---|---|---|
| `render/url_osc8_encode.go` | a missing trailing newline | 4 lines | **fixed** |
| `render/url_osc8_encode_test.go` | a missing trailing newline | 4 lines | **fixed** |
| `palette/palette.go` | const-block alignment (two blocks) | 52 of 98 lines | left as it was |
| `render/hyperlink_caps_test.go` | a whole 221-line table re-indented | 432 lines on a 402-line file | left as it was |

The ground rules say `gofmt -l` is not clean for `internal/` and that
wholesale reformatting must be avoided because *it buries real diffs*.
That is the test applied: a fix that touches a handful of lines is noise
and is worth taking in a package that has just become public; a fix that
rewrites half a file — or all of it — is exactly the noise the rule
warns about, and it stays.

So `render` and `palette` are public with two files still unformatted,
in the same state they were in at HEAD. The rest of the tree was not
touched, and `internal/` was not reformatted.

### 4a. The verification is honest about it

```
gofmt -l cmd/ render/ diffsyntax/ palette/ retry/ retrybudget/ atomicfile/
# palette/palette.go
# render/hyperlink_caps_test.go
```

Two lines, both pre-existing, both named above.

## 5. The shared tree

Another agent is working in `internal/cliapp` and `internal/tuilib`. One
of their in-flight files imports `palette`:

```
internal/cliapp/slash_panels.go
```

Their change is in the panel rendering, unrelated to the import block,
but a commit is a commit: staging the whole file would have swept their
work into mine. The index for that file was therefore built from HEAD
rather than from the worktree:

```bash
git show HEAD:internal/cliapp/slash_panels.go > base.go
sed -E -i '' 's|internal/palette([^a-zA-Z0-9_])|palette\1|g' base.go
gofmt -w base.go
diff -u base.go mine.go | git apply --cached
```

The index carries the single import line and nothing else; the worktree
keeps both agents' changes side by side. The other nine of their files
were verified to contain none of my change and were left unstaged.

**A staging mistake was made and corrected.** The first attempt to
exclude their files used `grep -v -x -F "$theirs"` with a
newline-separated list in one variable. With `-F`, grep treats that as a
single multi-line literal rather than a list of alternatives, so nothing
matched and all ten of their files were staged with their in-flight
work. It was caught by inspecting `git diff --cached --numstat` for those
paths, undone with `git restore --staged`, and redone with
`grep -v -x -F -f <file>` (one pattern per line). The rule worth
remembering: **check the index, not the intent**, before committing.

## 6. What did not change

- **The renderer.** The markdown renderer still emits the same fences,
  tables, hyperlinks and OSC 8 sequences; the terminal capability
  detection still refuses hyperlinks on Apple Terminal, GNU screen and
  tmux without passthrough.
- **The highlighter.** `diffsyntax` still uses chroma v2.27.0 — pinned,
  because the reference embeds upstream v2.27.0's 353 files byte for byte.
  The move did not touch the pin.
- **The palette.** The recovered true-colour values are unchanged; only
  the const-block alignment moved.
- **The retry arithmetic.** The AGY 1.3.4 backoff cap (4 minutes → 30
  seconds) and the AGY 1.2.13 per-turn 30-second wait cap are the same
  numbers in the same place.
- **The atomic write.** Temp file, fsync, rename over the target, and the
  file mode the wrapper exists to preserve.

## 7. Verification 【一手】

```
go build ./cmd/... ./internal/... (and the sixteen public packages)   # clean
go test  -count=1 ./cmd/... ./internal/... (and the sixteen)           # all green
gofmt -l cmd/ render/ diffsyntax/ palette/ retry/ retrybudget/ atomicfile/
    # palette/palette.go, render/hyperlink_caps_test.go — both as at HEAD
go list -deps <the sixteen public packages> | grep pigasus/internal    # empty
```

`scripts/check_constraints.py` reports the same pre-existing violations
before and after the move — the move added none.

## 8. Open questions

- **`palette` is the reference's palette.** Its *content* is Antigravity's
  (recovered from truecolour pty captures of agy 1.4.3). It is published
  as a capability, not as a contract: a third party may use the values,
  but they are the clone's rendition, not a spec.
- **`render` is the largest single package in the tranche** (3,002 LOC)
  and the one with the widest reach outside this repository. Its API is
  now a promise; the next change to it should be deliberate.
- **`retrybudget` names PiG.** Its observer wires the budget into PiG's
  `AutoRetryStartEvent`, so it carries a dependency on the turn engine's
  vocabulary in its *comment* and its event names. The Go-level
  dependency is nil — that is why it could move alone — but the API
  should be read as "PiG-shaped".
- **`internal/mcpoauth`** is still dead and still not deleted.

## 9. Limitations

- **No external consumer has imported any of these.** The value axis of
  the ranking is estimated from the nature of each contract, not measured
  from usage.
- **The public surface is a one-way door in practice.** T4 should be a
  single, deliberate commit, as T1, T2 and T3 were.
- **Formatting was changed in two files** (a missing trailing newline
  each). Two more were left unformatted, as they were at HEAD, because
  fixing them would have rewritten most of each file.
- **The other agent's `slash_panels.go` carries an uncommitted import
  hunk** in the index. It survives, but the tree is only consistent once
  that agent commits.

---

Reports: [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md)
(the current state and the ranking),
[`cycle-355-public-tranche-2`](../cycle-355-public-tranche-2/deliverable.md)
(the T2 move),
[`cycle-354-public-packages`](../cycle-354-public-packages/deliverable.md)
(the T1 move and the ranking),
[`cycle-353-cloudcode-dialect`](../cycle-353-cloudcode-dialect/deliverable.md)
(the first public package).
