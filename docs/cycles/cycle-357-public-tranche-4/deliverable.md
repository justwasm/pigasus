# Cycle 357 deliverable: the browser and MCP surface becomes public

**Cycle:** `cycle-357-public-tranche-4`
**Subject:** the fourth public tranche — the **browser and MCP surface**
and the recovered **gRPC service registry**, moved out of `internal/` to
the top of the tree.
**Evidence:** 【一手】 first-hand against the working tree (the moves, the
dependency closure, the tests, a token-level equivalence proof) ·
【反编译】 n/a — no binary was disassembled this cycle · 【strings】 n/a ·
【推测】 the ROI weights are a judgement, and the guide says so

---

## 0. The short answer

| | before | after |
|---|---|---|
| the Chrome DevTools Protocol client | `internal/browsercdp` | **`browsercdp/`** |
| the in-Chrome bridge protocol | `internal/browserbridge` | **`browserbridge/`** |
| the browser tool's broker | `internal/agentbrowser` | **`agentbrowser/`** |
| the MCP client | `internal/mcp` | **`mcp/`** |
| the `${env:NAME}` resolver | `internal/envref` | **`envref/`** |
| the recovered v4 gRPC registry | `internal/grpc` | **`grpc/`** |

10,560 LOC. Six packages, one commit, and — for the first time since T1 —
**no file shared with the other agent working in this tree**.

## 1. Why this tranche

Tier 1 of the ROI ranking, in the guide's own words: *a second
implementation cannot exist without these*.

- `browsercdp` is a Chrome DevTools Protocol client. It is not a Google
  contract at all, and it is the most broadly reusable package in the
  tree — but it is reusable for the same reason a wire is: another
  program has to speak it exactly.
- `browserbridge` is the Open Claude in Chrome extension's protocol — one
  TCP socket, a promotion handshake, argument coercion.
- `agentbrowser` is the browser tool's broker: the wire shapes, the tool
  declaration, the parameter builder. Its own guide entry says it has
  *"no PiG, no `internal/tools`, nothing of the clone's"* — which is
  exactly the profile of a package that can leave `internal/`.
- `mcp` (+ `envref`) is the Model Context Protocol client.
- `grpc` is the recovered v4 service registry — the six gRPC services the
  reference advertises, with their exact proto names, plus the Code
  Assist client stub.

## 2. The rule, measured

Every one of the six was a leaf. `mcp`'s only in-tree dependency was
`envref`, and `envref` moved in the same tranche, so the closure moved as
one piece with no dependency surgery:

```bash
go list -deps ./browsercdp ./browserbridge ./agentbrowser ./mcp ./envref ./grpc \
  | grep pigasus/internal
# nothing
```

Across the whole public surface — twenty-two packages now:

```bash
go list -deps ./geminicode ./localharness/... ./lifecyclehooks ./budget ./harnessprompt \
             ./lsproto/... ./rctoken ./originpolicy ./endpoints ./remotetransport \
             ./render ./diffsyntax ./palette ./retry ./retrybudget ./atomicfile \
             ./browsercdp ./browserbridge ./agentbrowser ./mcp ./envref ./grpc \
  | grep pigasus/internal
# nothing
```

## 3. The move

| | count |
|---|---|
| packages moved | 6 |
| LOC moved | 10,560 |
| files renamed | 42 |
| paths in the commit | 125 |
| files whose import path was rewritten | 36 |
| files whose **stale comment paths** were fixed | 43 |

That last row is the finding worth recording. The import rewrite in T1–T3
matched on the **full import path** (`pigasus/internal/lsproto`), so it
never touched a *comment* that named a package by its bare path. A scan
of the moved packages for stale references found 43 files still saying
`internal/lsproto`, `internal/remotetransport` or `internal/rctoken` —
including, in the public packages themselves, comments that pointed at
their own now-moved siblings. Those were fixed with a bare-path rewrite
and the scan repeated until it printed nothing.

A stale path in a comment is a small thing, but a public package is a
promise and its comments are part of what it says about itself.

## 4. A token-level proof that nothing changed

A move is easy to assert and easy to get wrong. So the whole public
surface was checked mechanically: for every moved file, take the version
from before its tranche, apply the import-path substitution that the move
implies, strip all whitespace, and compare.

**137 of the 138 files are token-identical.** The one file with a visible
difference is instructive:

```
$ diff <(base) <(moved)
8d7
< "github.com/justwasm/pigasus/originpolicy"
9a9
> "github.com/justwasm/pigasus/originpolicy"
```

Two lines, same two imports, in a different order — gofmt's doing, not
mine. The other differences the check reports are whitespace inside doc
comments and struct-field alignment, and nothing else.

## 5. Formatting

Ten files needed `gofmt`:

| file | why | diff |
|---|---|---|
| `devutils/browserdemo/main.go` | the import path changed, so its sort position did | 2 lines |
| 9 files in `grpc/` | doc-comment re-indentation and struct-field alignment that gofmt had wanted at HEAD | 4–36 lines each |

The `grpc` files were unformatted at HEAD. Applying gofmt to them changed
**only whitespace**: the token-level check above reports `IDENTICAL (code)`
for every one of the eleven files in the package, including the two that
were already clean. The doc-comment change is gofmt 1.19's list
formatting, which turns

```
//   - exa.api_server_pb.ApiServerService
//       .FetchLicensesFromAICode  →  license tier list
```

into a proper godoc list.

A fourth file needed no reformat but needed the same treatment for a
different reason: `internal/printmode/mcp.go` was clean at HEAD and its
`mcp` import moved position, so the path rewrite alone left it
unformatted. It was formatted. Three more files — two of them the other
agent's — had the same problem from the earlier tranches and were
formatted for the same reason: an import line I moved, nothing else.

**No file was reformatted for a reason other than an import path this
work changed.**

## 6. The shared tree

For the first time, this tranche touched **none** of the files the other
agent is editing. All twelve of their in-flight files were checked for a
T4 import (`browsercdp`, `browserbridge`, `agentbrowser`, `mcp`,
`envref`, `grpc`): zero hits. No filtered patch was needed, and the
commit is a plain index commit with no shared paths in it.

## 7. What did not change

- **The browser.** The CDP client's session lifecycle, the actions, the
  profile handling, the real-Chrome end-to-end test — unchanged.
- **The bridge.** The promotion handshake, the two-second failover, the
  argument coercion (a `tabId` that arrives as a string becomes a
  number) — unchanged.
- **The MCP client.** The pool, the `mcp.json` config reader, the
  header substitution — unchanged.
- **The gRPC registry.** The six service names, the `Service` interface,
  the `AllServices` list, the Code Assist client stub and the `Tier`
  enum — unchanged, character for character.

## 8. Verification 【一手】

```
go build ./cmd/... ./internal/... ./devutils/... (and the 22 public packages)   # clean
go test  -count=1 ./cmd/... ./internal/... (and the 22)                       # all green
gofmt -l <the moved paths>                                                   # clean
go list -deps <the 22 public packages> | grep pigasus/internal                # empty
```

Plus the token-level equivalence check over all four tranches: **138 of
138 files identical**.

`scripts/check_constraints.py` reports the same pre-existing violations
before and after the move — the move added none.

## 9. Open questions

- **`grpc` is a generic name for a public package.** A consumer that
  imports both `github.com/justwasm/pigasus/grpc` and
  `google.golang.org/grpc` has to alias one of them. The name was kept
  because it matches the reference's own vocabulary and the guide's
  ranking, but a more specific name (`agygrpc`, `apiservice`) would be
  friendlier. It is a rename before anyone depends on it.
- **The `*_pb` descriptor packages stay internal** while their working
  copies are now public. The split is deliberate — one is the supported
  surface, the others are the evidence — but it is worth remembering that
  `internal/codeium_common_pb`, `internal/extension_server_pb` and
  `internal/language_server_pb` still have zero importers.
- **`internal/mcpoauth` is still dead** and still not deleted: 1,477 LOC
  and 829 LOC of tests, no references anywhere. The AGY 1.3.4 changelog
  line it answers may describe behaviour the clone still owes.
- **T5 remains queued**: `vim` (3,816 LOC) and `imagery` (621 LOC).

## 10. Limitations

- **No external consumer has imported any of these.** The value axis of
  the ranking is estimated from the nature of each contract, not measured
  from usage.
- **The public surface is a one-way door in practice.** Four tranches are
  committed; the next one should be a single, deliberate commit too.
- **The token-level proof covers the moved files, not their callers.**
  The callers were checked by the test suite, which is green, but a
  caller's behaviour change would need its own evidence.
- **`gofmt` was applied only to files whose import path this work
  changed.** The `grpc` package's eleven files are token-identical to
  their pre-move versions; the others are import reordering.

---

Reports: [`docs/guide/public-packages.md`](../../docs/guide/public-packages.md)
(the current state and the ranking),
[`cycle-356-public-tranche-3`](../cycle-356-public-tranche-3/deliverable.md)
(the T3 move),
[`cycle-355-public-tranche-2`](../cycle-355-public-tranche-2/deliverable.md)
(the T2 move),
[`cycle-354-public-packages`](../cycle-354-public-packages/deliverable.md)
(the T1 move and the ranking),
[`cycle-353-cloudcode-dialect`](../cycle-353-cloudcode-dialect/deliverable.md)
(the first public package).
