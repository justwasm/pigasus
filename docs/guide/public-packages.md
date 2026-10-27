# The public packages

> Which packages a third party can import, the rule that keeps them
> importable, and the **ROI ranking** of what is queued next.

## What this is for

`internal/` is Go's own word for *"not for you"*: a package under
`internal/` cannot be imported from outside the module, whatever it
contains. That is the right default for a clone's internals.

But some of what lives under `internal/` is **not** the clone's interior.
It is a **contract** — a wire another implementation has to match byte for
byte, a protocol another program has to speak. Keeping those under
`internal/` says *"nobody else may use this"*, and for a contract that is
the wrong answer: the people who need it most are outside this repository.

This file is the inventory of that distinction, and the queue.

## The rule

A package is **public** when it sits at the top of the tree (not under
`internal/`) **and** has no dependency on `internal/` — transitively, not
just directly.

The transitive part is the load-bearing one. A public package that imports
`github.com/justwasm/pigasus/internal/foo` compiles fine here and is
**unusable outside the module**: Go refuses the import before the compiler
ever sees it. So "public" is a property of the package's whole dependency
closure, not of its location.

```bash
go list -deps ./<pkg> | grep pigasus/internal   # must print nothing
```

The second rule: **a public package's API is a compatibility promise.** A
private package can be reshaped freely; a public one cannot. That is why
the queue below is ranked by value and not by convenience — a public
package nobody uses is dead weight with a promise attached.

## The public surface today

| package | LOC | what it is | who needs it |
|---|---|---|---|
| [`geminicode`](cloudcode-dialect.md) | 1,801 | the CloudCode dialect — the request envelope, the SSE frames, the tool round trip | anyone standing in for Google's Code Assist |
| `localharness` | 2,801 + 33,980 generated | the SDKs' transport, **both halves** — the stdio handshake, the websocket, the frame codec | anyone implementing the `google-antigravity` SDK side |
| `lifecyclehooks` | 1,723 | the bidirectional hook protocol — nine types, request/response correlation, per-type verdicts | the same |
| `budget` | 1,680 | the limit manager — five limits, the scope window, subagent accounting | the same |
| `harnessprompt` | 688 | the harness's own system instruction, rebuilt byte for byte | the same |
| [`lsproto`](remote-control.md) | 1,433 | the LanguageServer proto-JSON dialect — the canonical wire the Web UI speaks | a local service that answers the Web UI |
| [`remotetransport`](remote-control.md) | 14,626 | the Connect and gRPC-Web transport, the error masking, the stream plumbing | the same |
| [`rctoken`](remote-control.md) | 477 | the remote-control token format | the same |
| [`originpolicy`](remote-control.md) | 733 | the browser-origin policy | the same |
| [`endpoints`](endpoints.md) | 835 | the named service endpoints and the `config.json` resolver | anyone pointing a Google service at a local one |
| `accountpool` | 463 | the account-routing policy — the config shape, the four strategies, the quota threshold, the decision | anyone routing a turn over a pool of credentials |
| `render` | 3,004 | the markdown renderer — fences, tables, hyperlinks, terminal capability detection | any Go program that prints markdown |
| `diffsyntax` | 798 | per-token diff highlighting and truncation | any Go program that shows a diff |
| `palette` | 1,160 | the recovered true-colour ANSI palette | any Go program that paints a terminal |
| `retry` | 742 | the retry / backoff helper | any Go program that talks to a flaky API |
| `retrybudget` | 752 | the per-turn cumulative retry-wait cap | the same |
| `atomicfile` | 123 | the atomic file write — temp file, fsync, rename | any Go program that writes a file |
| `browsercdp` | 2,405 | the Chrome DevTools Protocol client | any Go program that drives a browser |
| `browserbridge` | 1,482 | the Open Claude in Chrome bridge protocol | the same |
| `agentbrowser` | 2,147 | the browser tool's broker, wire shapes and declaration | a clone of the browser tool |
| `mcp` | 1,245 | the MCP client — the pool, the config, the header substitution | any Go program that speaks MCP |
| `envref` | 306 | the `${env:NAME}` reference resolver | the same |
| `grpc` | 2,980 | the recovered v4 gRPC service registry — the six services and the Code Assist client stub | a clone of the Antigravity gRPC side |
| `vim` | 3,816 | the modal editing engine — a pure state machine, no terminal in it | any Go program with a text editor |
| `imagery` | 621 | the image downscaler — format detection, the per-side ceiling | any Go program that feeds images to a model |

None of them touches `internal/`:

```bash
go list -deps ./geminicode ./localharness/... ./lifecyclehooks ./budget ./harnessprompt \
             ./lsproto/... ./rctoken ./originpolicy ./endpoints ./accountpool \
             ./remotetransport \
             ./render ./diffsyntax ./palette ./retry ./retrybudget ./atomicfile \
             ./browsercdp ./browserbridge ./agentbrowser ./mcp ./envref ./grpc \
             ./vim ./imagery \
  | grep pigasus/internal     # nothing
```

## The ROI model

The queue is ranked on three axes, because "reusable" alone does not say
what to do first.

| axis | question | 0 | 1 | 2 | 3 |
|---|---|---|---|---|---|
| **V — value** | how many *other* programs need this exact contract? | nothing | a building block only this clone needs | a general-purpose capability | a wire another implementation must match byte for byte |
| **B — breadth** | how little of the clone's own vocabulary does it carry? | tied to the clone's model | a clone-shaped asset | a generic format or algorithm | pure protocol or data |
| **E — effort** | what does the move cost? | a leaf with ≤2 importers | a leaf with more importers, or one small leaf alongside | several leaves together | dependency surgery |

**ROI index = V × B ÷ (E + 1).** A high index is worth doing first; a low
one is worth doing eventually, or not at all.

## The ranking

### Tier 1 — protocol (V=3)

A second implementation *cannot exist* without these. They are the highest
value in the tree and the highest ROI per line moved.

| rank | package | LOC | importers | V | B | E | ROI | state |
|---|---|---|---|---|---|---|---|---|
| 1 | `localharness` + `pb/` | 36,781 | 31 files | 3 | 3 | 2 | **4.5** | **done** |
| 2 | `lifecyclehooks` | 1,723 | 3 | 3 | 3 | 0 | **9.0** | **done** |
| 3 | `budget` | 1,680 | 1 | 3 | 2 | 0 | **6.0** | **done** |
| 4 | `harnessprompt` | 688 | 3 | 2 | 3 | 0 | **6.0** | **done** |
| 5 | `mcp` (+ `envref`) | 1,245 + 306 | 2 | 3 | 3 | 1 | **4.5** | **done** (T4) |
| 6 | `lsproto` | 1,433 | 12 | 3 | 3 | 1 | **4.5** | **done** (T2) |
| 7 | `remotetransport` (+ `lsproto`, `endpoints`, `originpolicy`, `rctoken`) | 14,626 | 10 | 2 | 3 | 2 | **2.0** | **done** (T2) |
| 8 | `browserbridge` | 1,482 | 2 | 2 | 3 | 0 | **6.0** | **done** (T4) |
| 9 | `agentbrowser` | 2,147 | 12 | 2 | 3 | 1 | **3.0** | **done** (T4) |
| 10 | `grpc` | 2,980 | 1 | 1 | 3 | 0 | **3.0** | **done** (T4) |
| 11 | `rctoken` | 477 | 7 | 2 | 3 | 1 | **3.0** | **done** (T2) |
| 12 | `browsercdp` | 2,405 | 9 | 3 | 2 | 1 | **3.0** | **done** (T4) |

`browsercdp` is the odd one out in Tier 1: it is not a Google contract at
all, it is a Chrome DevTools Protocol client — general-purpose, but so
reusable that it earns its place next to the wires.

### Tier 2 — capability (V=2)

Not tied to any Google contract, but a library anyone would want. Broad
demand, no protocol lock-in.

| rank | package | LOC | importers | V | B | E | ROI | note |
|---|---|---|---|---|---|---|---|---|
| 13 | `render` | 3,004 | 8 | 3 | 2 | 1 | **3.0** | the markdown renderer — **done** (T3) |
| 14 | `vim` | 3,816 | 3 | 2 | 2 | 0 | **4.0** | a modal editing layer; niche but self-contained — **done** (T5) |
| 15 | `retry` + `retrybudget` | 1,494 | 7 | 2 | 2 | 1 | **2.0** | **done** (T3) |
| 16 | `diffsyntax` | 798 | 5 | 2 | 2 | 1 | **2.0** | **done** (T3) |
| 17 | `palette` | 1,160 | 36 | 2 | 2 | 1 | **2.0** | the reference's true-colour palette — **done** (T3) |
| 18 | `endpoints` | 835 | 40 | 2 | 2 | 1 | **2.0** | the named-endpoint resolver — **done** (T2) |
| 19 | `atomicfile` | 123 | 16 | 2 | 2 | 1 | **2.0** | 123 lines, universally useful — **done** (T3) |
| 20 | `imagery` | 621 | 2 | 2 | 2 | 0 | **4.0** | **done** (T5) |

### Tier 3 — reference asset (V=1, B=3)

The clone's own answer key. Valuable to someone building their own
Antigravity-compatible agent, and to nobody else. `agytemplates`,
`agyprompt` and `agystep` stay internal for now: they are the reference's
*content*, not a protocol, and the content is already reachable through
`cmd/harness` and the guides.

### Tier 4 — application (V≤1)

Everything else: `cliapp`, `cli`, `remotecontrol`, `tools`, `convstore`,
`agent`, `pigrt`, … These are the clone's interior and stay in `internal/`
by definition.

## The tranches

**T1 — the SDK family (done).** `localharness`, `lifecyclehooks`, `budget`
and `harnessprompt` moved to the top of the tree. The whole family was
already leaf-level inside `internal/`, its only consumer is `cmd/harness`,
and the guide had called it "standalone packages on purpose" since it was
written. 40,872 LOC of protocol and limits became importable by anyone.

**T2 — the Web UI's wire (done).** `lsproto` (the proto-JSON dialect),
`remotetransport` (the Connect and gRPC-Web transport), and their three
leaves `endpoints`, `originpolicy` and `rctoken`. This was the
second-largest block of contract in the tree: 18,104 LOC, and the dialect
a local service has to answer to serve the Web UI. All five were already
leaf-level — `remotetransport`'s only in-tree dependencies were the other
four — so the move was a path rewrite with no dependency surgery.

**T3 — the capability tranche (done).** `render`, `diffsyntax`,
`palette`, `retry`, `retrybudget`, `atomicfile`. 6,579 LOC of library
that has nothing to do with Antigravity and everything to do with being
reusable. (`endpoints` moved with T2, so it is no longer part of this
tranche.) All six were already leaf-level; the only import the tranche
had to rewrite was `palette`'s, at 36 importers.

**T4 — the browser and MCP surface (done).** `browsercdp`,
`browserbridge`, `agentbrowser`, `mcp` + `envref`, `grpc`. 10,565 LOC.
All six were leaf-level (`mcp`'s only dependency was `envref`, which is
in the same tranche), so the move needed no dependency surgery — and
unlike T2 and T3 it touched **none** of the files another agent is
editing.

**T5 — the leftovers (done).** `vim` (3,816 LOC, the modal editing engine)
and `imagery` (621 LOC, the image downscaler). Both were already leaves,
so the move was a path rewrite. The interesting part was what it
uncovered: **a relative path in a test fixture**, and **thirty import
blocks** that this work's own path changes had left unsorted two and
three tranches earlier. Both are fixed, and the import-order sweep is
now empty across the whole tree.

**What is left.** The queue is empty: every package the ranking scored
is published. Two findings remain open — `internal/mcpoauth` is dead
code (a deletion candidate, not a publication candidate) and the three
`*_pb` descriptor packages are evidence, not code.

**A package that is born public.** `accountpool` did not move: it was
written at the top of the tree. The account-routing policy is a
*contract* — the shape of a configuration section and the rules that
decide which credential a turn runs on — and it is pure (no I/O, no
`internal/` dependency), so it can be imported by a program that is not
this clone. Its runtime (`internal/accountrouting`) stays internal: it
reads the registry, the keychain and the server's quota, all of which
belong to the clone's interior. The split is the rule: the *policy* is
public, the *plumbing* is not.

## Two findings that change the ranking

**`internal/mcpoauth` is dead.** 1,477 LOC plus 829 LOC of tests, and
**zero references** anywhere in the tree — not one import, not one mention
outside its own directory. It was built for the AGY 1.3.4 changelog line
*"Fixed remote MCP servers that use OAuth signing you out after a temporary
…"* and never wired in. It is not a public-package candidate; it is a
**deletion** candidate, and deleting it is worth more ROI than moving it.
(Not done in this cycle: the changelog line it answers may describe
behaviour the clone still owes.)

**The `*_pb` descriptor packages are evidence, not code.**
`internal/codeium_common_pb` (4,205 LOC), `internal/extension_server_pb`
(2,746) and `internal/language_server_pb` (409) have zero importers —
the public `grpc` package keeps its own working copies of the same
service descriptors, and these are the 1:1 transcriptions of the
reference's decompiled sources. `TODO-rc-parity.md` marks them
*"generated protobuf code — do not refactor"*. They stay under `internal/`:
they are the evidence, and publishing an evidence directory would
misrepresent it as a supported API.

## 硬约束 / 存疑 / limitations

- **硬约束**: a public package has **no** `internal/` dependency,
  transitively. `go list -deps ./<pkg> | grep pigasus/internal` must print
  nothing. A public package that violates this is unusable outside the
  module, whatever its location says.
- **硬约束**: the live wire did not change in any move. The T1 and T2
  moves were a path change plus a doc change; the bytes, the framing, the
  error masking and the policy are the same.
- **硬约束**: a path change can break a **relative** path. `internal/clipboard`
  read its image fixtures as `../imagery/testdata/…`, which stops being true
  once `imagery` moves up a level. A tree-wide scan for relative escapes
  into a moved package is part of the move, not an afterthought.
- **硬约束**: the generated protobuf under `localharness/pb/` was moved
  **with** its package, because a public package may not import an
  `internal/` one — and the generated descriptors do not embed the Go
  import path (the `go_package` option is absent from the embedded
  `rawDesc`), so the move needed no descriptor surgery.
- **硬约束**: each tranche is **one commit**. A public package's API is a
  compatibility promise, so a tranche that moves five packages is a single
  deliberate change, not five staged ones.
- **存疑**: the ROI index is a **judgement**, not a measurement. The axes
  are stated so the ranking can be argued with; a different weighting would
  reorder the tiers but not the top of each.
- **存疑**: `internal/mcpoauth` may be dormant rather than dead — the
  1.3.4 changelog line it answers suggests a behaviour the clone might
  still owe a user. Deleting it needs that answered first.
- **limitations**: the ranking covers the packages that exist **today**.
  A package that has not been written yet cannot be ranked.
- **limitations**: no package outside this module is known to import any
  of the public ones, so "third-party demand" is estimated from the
  contract's nature, not measured from usage.

---

Reports: [`cycle-358-public-tranche-5`](../cycles/cycle-358-public-tranche-5/deliverable.md)
(the T5 move, and the last of the queue),
[`cycle-357-public-tranche-4`](../cycles/cycle-357-public-tranche-4/deliverable.md)
(the T4 move), [`cycle-356-public-tranche-3`](../cycles/cycle-356-public-tranche-3/deliverable.md)
(the T3 move), [`cycle-355-public-tranche-2`](../cycles/cycle-355-public-tranche-2/deliverable.md)
(the T2 move), [`cycle-354-public-packages`](../cycles/cycle-354-public-packages/deliverable.md)
(the T1 move and the ranking), [`cycle-353-cloudcode-dialect`](../cycles/cycle-353-cloudcode-dialect/deliverable.md)
(the first public package).
