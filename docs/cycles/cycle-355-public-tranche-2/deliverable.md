# Cycle 355 deliverable: the Web UI's wire becomes public

**Cycle:** `cycle-355-public-tranche-2`
**Subject:** the second public tranche — five packages that make up the
**Web UI's wire**, moved out of `internal/` to the top of the tree.
**Evidence:** 【一手】 first-hand against the working tree (the moves, the
dependency closure, the tests, the staged patches) · 【反编译】 n/a — no
binary was disassembled this cycle · 【strings】 n/a · 【推测】 the ROI
weights are a judgement, and the guide says so

---

## 0. The short answer

| | before | after |
|---|---|---|
| the LanguageServer proto-JSON dialect | `internal/lsproto` | **`lsproto/`** |
| the Connect / gRPC-Web transport | `internal/remotetransport` | **`remotetransport/`** |
| the remote-control token | `internal/rctoken` | **`rctoken/`** |
| the browser-origin policy | `internal/originpolicy` | **`originpolicy/`** |
| the named-endpoint resolver | `internal/endpoints` | **`endpoints/`** |

18,104 LOC of contract — the second-largest block in the tree after the
SDK family — became importable by a third party. A local service that
answers the Web UI can now import the dialect it has to speak, instead of
copying it.

## 1. Why this tranche was second

The ROI ranking in [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md)
puts the Web UI's wire in Tier 1 (protocol) behind only the SDK family,
and its score is the highest of the queued Tier 1 items: a second
implementation of the Web UI's client **cannot exist** without these
files, and the people who would write it are outside this repository.

Three facts made it tractable:

1. **The closure was already a leaf set.** `remotetransport` depended on
   `lsproto`, `endpoints`, `originpolicy` and `rctoken` — and nothing
   else. `originpolicy` depended on `endpoints` and nothing else. Every
   one of the five had no dependency outside the five.
2. **Nothing in the set was generated.** No protobuf descriptors, no
   embedded `rawDesc`, no `go_package` to keep honest. A path rewrite was
   the whole of it.
3. **The set had one entry point.** `remotetransport` is the transport;
   the other four are its leaves. There was no ordering problem — the
   tranche moved as one piece.

## 2. The rule, again, measured

The load-bearing invariant is that a public package has **no** `internal/`
dependency, *transitively*. Go refuses an `internal/` import from
outside the module before the compiler ever sees it, so a public package
that reaches into `internal/` is unusable outside the module whatever its
location says.

```bash
go list -deps ./lsproto/... ./rctoken ./originpolicy ./endpoints ./remotetransport \
  | grep pigasus/internal
# nothing
```

Measured after the move, across the whole public surface:

```bash
go list -deps ./geminicode ./localharness/... ./lifecyclehooks ./budget ./harnessprompt \
             ./lsproto/... ./rctoken ./originpolicy ./endpoints ./remotetransport \
  | grep pigasus/internal
# nothing
```

## 3. The move

| | count |
|---|---|
| packages moved | 5 |
| LOC moved | 18,104 |
| files whose import path was rewritten | 81 |
| renames recorded by git | 41 |
| paths in the commit | 109 |

The rewrite was a single `sed` over the tree, plus the comment references
that name the old paths. The import path sorts differently after the move
(`github.com/justwasm/pigasus/endpoints` now sorts *before*
`github.com/justwasm/pigasus/internal/…`, where `internal/endpoints`
sorted after it), so four files needed `gofmt -w`:

- `cmd/anthropic-shim/main.go`, `cmd/openai-shim/main.go` and
  `remotetransport/tcp_connectstream_test.go` were gofmt-clean at HEAD —
  the fix was import reordering only.
- `lsproto/order_test.go` was **already** unformatted at HEAD (a map
  literal whose alignment gofmt disagrees with). It was formatted as part
  of making the newly-public package clean: a three-line change, no
  behaviour.

No other file in the tree was reformatted.

## 4. The shared tree: staging only my own change

Another agent is working in `internal/cliapp` and `internal/tuilib`. Three
of their in-flight files import `endpoints`:

```
internal/cliapp/slash_actions_test.go
internal/cliapp/slash_commands_handlers.go
internal/tuilib/feedback.go
```

Their changes are in the `/statusline` area — unrelated to the import
block — but a commit is a commit: staging the whole file would have
swept their work into mine.

The index was therefore built from HEAD rather than from the worktree.
For each of the three:

```bash
git show HEAD:<file>  > base.go     # HEAD's version, no in-flight work
sed -i '' 's|…/internal/endpoints|…/endpoints|' base.go
gofmt -w base.go                   # the reorder the move forces
diff -u base.go mine.go | git apply --cached
```

The result: the index holds **only** the import-path hunk, while the
worktree keeps both agents' changes side by side. `git status` shows
those three as `MM` — staged and modified — which is exactly the
intended state.

The other seven of their files were verified to contain none of my
change (`git diff HEAD -- <file> | grep -c 'pigasus/endpoints'` → 0) and
were left unstaged.

## 5. What did not change

- **The wire.** The proto-JSON canonical member names, the Connect and
  gRPC-Web codecs, the error masking (`-32018` / `-32019` → `not_found`,
  `-32602` → `invalid_argument`), the origin policy, the token format —
  all identical. A move of the package, not of the behaviour.
- **The endpoint resolver.** `endpoints.Current()` still reads
  `~/.config/openagy/config.json` with the same precedence, and the
  compiled-in defaults are unchanged.
- **The `lsproto/descriptor/` bundle.** It is a `.protoset` file plus a
  README, not a Go package — it moved with `lsproto` because it describes
  the same wire.

## 6. What the move makes possible

Before this cycle, a service that wanted to answer the Web UI had two
options: vendor the clone's `internal/` packages (impossible — Go's
`internal/` rule forbids it from outside the module) or re-implement the
dialect from the reference bundle. Now it can import `lsproto` and
`remotetransport` directly.

That is the point of the exercise. The Web UI's contract is the second
biggest piece of **published** behaviour this repository reproduces after
the SDK protocol, and it was the piece with the least reach.

## 7. Verification 【一手】

```
go build ./cmd/... ./internal/... ./geminicode/ ./localharness/... ./lifecyclehooks/ \
        ./budget ./harnessprompt ./lsproto/... ./rctoken/ ./originpolicy/ ./endpoints/ \
        ./remotetransport                                              # clean
go test  -count=1 ./cmd/... ./internal/... (and the ten public packages)  # all green
gofmt -l cmd/ lsproto/ rctoken/ originpolicy/ endpoints/ remotetransport/ # clean
go list -deps <the ten public packages> | grep pigasus/internal           # empty
```

`scripts/check_constraints.py` reports the same pre-existing violations
before and after the move (`internal/remotecontrol`'s long functions, the
generated files) — the move added none.

## 8. Open questions

- **The dialect's completeness.** The five RPCs the Web UI actually calls
  are implemented; the rest of `LanguageServerService` is not. A public
  `lsproto` advertises more than the clone serves, and that gap is now
  visible to third parties.
- **`remotetransport` is 14,626 LOC in one package.** It moved as one
  piece because it is one contract, but it is large enough that a future
  split (codec / error policy / stream plumbing) would be worth
  considering — as a breaking change, once.
- **`internal/mcpoauth`** is still dead and still not deleted. The AGY
  1.3.4 changelog line it answers may describe behaviour the clone still
  owes.

## 9. Limitations

- **No live run against a real Web UI.** The move was validated by the
  byte-level tests and by the existing suite; the next live run is the
  real check.
- **The public surface is a one-way door in practice.** A package that is
  public and then reshaped breaks its consumers. T3 and T4 should each be
  a single, deliberate commit, as this one was.
- **Third-party demand is estimated, not measured.** No package outside
  this module is known to import any of the public ones, so the value axis
  reads the contract's nature rather than a usage count.
- **The other agent's three files carry an uncommitted import hunk.** The
  hunk is staged in the index, so it survives, but the working tree is
  only consistent once that agent commits.

---

Reports: [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md)
(the current state and the ranking),
[`cycle-354-public-packages`](../cycle-354-public-packages/deliverable.md)
(the T1 move),
[`cycle-353-cloudcode-dialect`](../cycle-353-cloudcode-dialect/deliverable.md)
(the first public package).
