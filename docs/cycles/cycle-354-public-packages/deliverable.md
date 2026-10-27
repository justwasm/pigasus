# Cycle 354 deliverable: the first public tranche

**Cycle:** `cycle-354-public-packages`
**Subject:** four packages that are **contracts**, not clone internals, moved
out of `internal/` to the top of the tree — and the ROI ranking that
decided the order.
**Evidence:** 【一手】 first-hand against the working tree (the moves, the
dependency closure, the tests) · 【反编译】 n/a — no binary was
disassembled this cycle · 【strings】 n/a · 【推测】 the ROI weights are a
judgement, and the guide says so

---

## 0. The short answer

| | before | after |
|---|---|---|
| the SDK's wire (`localharness`) | `internal/localharness` — importable by nobody outside this module | **`localharness/`** |
| the hook protocol | `internal/lifecyclehooks` | **`lifecyclehooks/`** |
| the limit manager | `internal/budget` | **`budget/`** |
| the harness prompt | `internal/harnessprompt` | **`harnessprompt/`** |
| the CloudCode dialect | `internal/backend` + `internal/geminiwire` | **`geminicode/`** (cycle 353) |
| the queue for the rest | — | **ranked**, in [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md) |

40,872 LOC of protocol and limits became importable by a third party, and
the next three tranches are written down with their dependency work
spelled out.

## 1. Why the SDK family went first

Three facts, all checkable:

1. **It is a contract, not an interior.** The `localharness` binary is what
   the `google-antigravity` PyPI SDKs spawn. Its transport, its hook
   protocol and its limit manager are the *client side of a published
   protocol* — the people who need them are the SDK implementors, and they
   are outside this repository.
2. **The guide already said so.** `docs/guide/localharness.md` has called
   them *"standalone packages on purpose"* since the layer was written.
   They were standalone in the *dependency* sense; they were not
   standalone in the *reachability* sense.
3. **The blast radius is one command.** `cmd/harness` is their only
   consumer. Twenty-eight files changed, all of them mechanical.

## 2. The rule that made it safe

`internal/` is Go's word for *not for you*. A package under it cannot be
imported from outside the module — so a public package that *depends* on
an internal one is unusable outside the module, whatever its location
says.

That is why the check is transitive, and why it is the first thing
verified:

```bash
go list -deps ./geminicode ./localharness/... ./lifecyclehooks ./budget ./harnessprompt \
  | grep pigasus/internal
# nothing
```

Before the move, `internal/localharness` depended only on its own `pb/`
subpackages; `internal/lifecyclehooks` only on `localharness/pb` (in
`proto.go` alone); `internal/budget` and `internal/harnessprompt` on
nothing. So the whole family was already a leaf-level closure — the move
needed no dependency surgery at all.

## 3. The generated protobuf moved with it

`localharness/pb/` is 33,980 LOC of generated protobuf in four packages
(`genai`, `localharness`, `genaiskills`, `gaosparsing`). It **had** to
move: a public package may not import an `internal/` one, so leaving the
bindings behind would have made `localharness` unusable outside the
module.

The worry was the descriptor. The embedded `rawDesc` of a generated file
normally carries the file's `go_package` option — and `go_package` is a
*Go import path*. Moving the package would have left the descriptor
pointing at a path that no longer exists.

It does not, and the reason is worth recording: **protoc-gen-go strips
`go_package` from the embedded descriptor.** Measured:

```bash
awk '/rawDesc = "" \+/,0' localharness/pb/localharness/localharness.pb.go \
  | grep -c justwasm
# 0
```

Every `justwasm` occurrence in those files is a Go **import statement**,
not descriptor bytes. The move was therefore a pure path rewrite — no
descriptor surgery, no regeneration, and no risk to the registry keys
(which come from the `.proto` file names and did not move).

## 4. What did not change

- **The wire.** The framing, the 4-byte little-endian prefix, the
  protojson dialect, the hook verdicts, the budget arithmetic — all
  identical. A move of the package, not of the behaviour.
- **The generated code.** The `.pb.go` files were moved, not edited apart
  from their import paths.
- **The `go:embed` assets.** `harnessprompt/sections/*.txt` and
  `harnessprompt/testdata/*.txt` are relative to the package, so they
  moved with it and the golden tests still compare byte for byte.

## 5. One thing the move forced: import order

Six files in `cmd/harness/` failed `gofmt` after the path rewrite, because
`github.com/justwasm/pigasus/budget` sorts before
`github.com/justwasm/pigasus/internal/...` where `internal/budget` sorted
after it. All six were clean at HEAD; the fix was `gofmt -w` on exactly
those six files, and nothing else in the tree was reformatted.

## 6. The ranking

The guide ranks the remaining candidates on three axes — **value** (how
many other programs need this contract), **breadth** (how little of the
clone's vocabulary it carries) and **effort** — and divides the product by
the effort. The queue, in order:

| tranche | packages | LOC |
|---|---|---|
| **T1** (done) | `localharness` + `pb/`, `lifecyclehooks`, `budget`, `harnessprompt` | 40,872 |
| T2 | `lsproto`, then `remotetransport` + `endpoints` + `originpolicy` + `rctoken` | 17,269 |
| T3 | `render`, `diffsyntax`, `palette`, `retry`, `retrybudget`, `atomicfile`, `endpoints` | 7,412 |
| T4 | `browsercdp`, `browserbridge`, `agentbrowser`, `mcp` + `envref`, `grpc` | 10,560 |

T2 is the second-largest block of contract in the tree: the Web UI's wire.
T3 is the library tranche — nothing to do with Antigravity, everything to
do with being reusable.

## 7. Two findings the ranking turned up

**`internal/mcpoauth` is dead.** 1,477 LOC plus 829 LOC of tests and
**zero references** anywhere in the tree — no import, no mention outside
its own directory. It was built for the AGY 1.3.4 changelog line *"Fixed
remote MCP servers that use OAuth signing you out after a temporary …"*
and never wired in. Publishing it would be advertising dead code with a
compatibility promise attached. **Deleting** it is worth more ROI than
moving it — but it is not done in this cycle, because the changelog line
describes a behaviour the clone may still owe a user.

**The `*_pb` descriptor packages are evidence, not code.**
`internal/codeium_common_pb` (4,205 LOC), `internal/extension_server_pb`
(2,746) and `internal/language_server_pb` (409) have zero importers:
`internal/grpc` keeps its own working copies of the same service
descriptors, and these are the 1:1 transcriptions of the reference's
decompiled sources. `TODO-rc-parity.md` marks them *"generated protobuf
code — do not refactor"*. They stay under `internal/` — publishing an
evidence directory would misrepresent it as a supported API.

## 8. Verification 【一手】

```
go build ./cmd/... ./internal/... ./geminicode/ ./localharness/... ./lifecyclehooks/ ./budget/ ./harnessprompt/   # clean
go test  -count=1 ./localharness/... ./lifecyclehooks/ ./budget/ ./harnessprompt/ ./cmd/harness/                 # all green
gofmt -l cmd/harness/ localharness/ lifecyclehooks/ budget/ harnessprompt/                                      # clean
go list -deps ./localharness/... ./lifecyclehooks ./budget ./harnessprompt | grep pigasus/internal               # empty
```

The last line is the load-bearing one: the four public packages depend on
nothing in `internal/`, so they are importable from outside this module.

`scripts/check_constraints.py` reports the same pre-existing violations
before and after the move (`cmd/harness/events.go` at 1,202 lines, the
generated `content.pb.go` at 5,043) — the move added none.

## 9. Open questions

- **The ROI index is a judgement.** The three axes are stated so the
  ranking can be argued with, but a different weighting would reorder the
  tiers. The top of each tier is stable.
- **`internal/mcpoauth` — dormant or dead?** The changelog line it
  answers is the evidence that it was once wanted. Answering it is the
  precondition for deleting it.
- **The other five RPCs on the CloudCode host** are described in the
  dialect guide but not specified field by field; a local service that
  wants to answer `loadCodeAssist` needs more than that.

## 10. Limitations

- **No live run against a real SDK.** The move was validated by the byte
  level tests and by the existing suite. The next live run is the real
  check.
- **The public surface is a one-way door in practice.** A package that is
  public and then reshaped breaks its consumers. The T2–T4 tranches should
  each be a single, deliberate commit.
- **The ranking covers the packages that exist today.** A package that has
  not been written cannot be ranked.
- **Third-party demand is estimated, not measured.** No package outside
  this module is known to import any of the public ones, so the value axis
  reads the contract's nature rather than a usage count.

---

Reports: [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md)
(the current state),
[`cycle-353-cloudcode-dialect`](../cycle-353-cloudcode-dialect/deliverable.md)
(the first public package),
[`cycle-299-localharness`](../cycle-299-localharness/deliverable.md) (the
protocol's own report).
