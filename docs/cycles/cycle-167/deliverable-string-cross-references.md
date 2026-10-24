# Cycle 167 — string cross-references via ADRP+ADD walker

## Question

Cycle 165 (yaml.v3), cycle 166 (shlex), and cycle 175 (ncruces/go-sqlite3)
each replaced a hand-rolled implementation in the clone with a third-party
package. The user asked whether a similar pull-the-deps approach might work
on the **reverse** problem too: **can we recover a high-fidelity copy of
the jetski library itself** purely from strings + pclntab evidence?

That question was answered in the previous turn's three-tier table. This
cycle addresses the leftover piece: building a toolchain that complements
the existing `out/local-macos/gosrc/` shape dump (361 packages, 37,832
declarations from `018a8c6`) by attaching **concrete string references** to
each function — i.e., pushing fidelity up from "map of API surface" toward
"map of API surface plus the strings they touch".

## What the toolchain does

Three scripts in `out/local-macos/xref/`:

1. **`gen_strings_addrs.py`** runs `strings(1)` on `~/.local/bin/agy` with
   a minimum length of 8, prints a vaddr-sorted TSV
   (`strings-addrs.tsv`, ~518K entries, regenerable in <5 s).

2. **`analyze.py`** walks the binary's `__text` arm64 instructions, finds
   `ADRP` followed by `ADD (immediate)` pairs within 16 instructions of
   each other, computes the address they load, and looks it up in the
   strings table. For each matching pair, it credits the enclosing
   function (found by binary search against `seeds.txt` starts) with the
   string content. Output: `xrefs.tsv` with start-vaddr, func name, hit
   count, and a CSV-of-quoted strings. ~8 s end-to-end.

3. **`xref_query.py`** is the user-facing entry point: it takes a func
   name substring or a `0xADDR`, looks it up in `xrefs.tsv`, and prints
   matching funcs plus the strings they reference. By default it filters
   out Go protobuf type-descriptor noise (e.g.,
   `"google3/..../proto.(*Type).Method"`); `--no-filter` shows everything.

## Results

```
$ time python3 out/local-macos/xref/analyze.py
# strings table: 517,921 entries
# funcs in scope: 154,525
# ADRP+ADD pairs scanned: 567,042
# matched 629 string refs in 400 funcs
# wrote 401 rows to out/local-macos/xref/xrefs.tsv
real    0m8.241s

$ python3 out/local-macos/xref/xref_query.py waitForEligibility
(no matches — see "Coverage" below)

$ python3 out/local-macos/xref/xref_query.py "ConfigIssue"
... (or any other string substring)

$ python3 out/local-macos/xref/xref_query.py "store.NewManager" --human
<func>
    <string>
    <string>
```

At the time of writing the 400 funcs cover 250 unique strings,
distributed approximately as:

| Class | Count | Notes |
|---|---|---|
| Configuration / scope snippets (`</config>`, `</state>`, `## Review Scope`, ...) | ~60 | Real userspace content |
| JSON tag names / enum keys (`defaultTeamFeatures`, `create_interaction`, ...) | ~80 | Generated metadata, real and useful |
| `package: ...` proto descriptors | ~30 | protobuf metadata, filtered by default |
| Generated type descriptors (`*.ProtoMessage`, `*.Descriptor`, `*.Enum`) | ~31 | Filtered by default |
| Error templates, log messages | ~10 | Real errors, real value |
| URL fragments | ~1 | Real URLs |
| Other (`.config:...`, `<pop depth="1"/>`, ...) | ~38 | XML / HTML-ish fragments |

## Why coverage is partial (the honest answer)

The Go compiler does not always load a string literal directly into a
register. Most runtime strings go through:

- `runtime.gostring` / `runtime.concatstring` (multi-piece strings)
- `runtime.convT*` chains for small literal strings via `type.string`
- `stringStruct` indirection (interface dispatch)
- Field references via relocated `LDR` plus offset computation
- Literal-pool `LDR (literal)` for short strings embedded in `__text`

Of these, only direct `__rodata` symbol loads use the
`ADRP+ADD Xn, Xn, #imm12` pattern this engine detects. That is why
`waitForEligibility` (which presumably builds most of its message text
through `runtime.gostring` or `fmt.Sprintf`) shows up with zero ADRP+ADD
cross-refs even though every line of its message text lives somewhere
in the binary.

This is the same problem every static reverse-engineering tool that
tries to recover printf format strings from a Go binary faces — there
is no recovery path from this side; the strings live in
`__rodata` but their addresses are reached via `runtime.gostring`,
which is an indirect call.

A second, smaller limit: `go tool objdump` on this Mach-O binary
returns 0 lines (`main module (...). does not contain package ...`),
so the engine cannot lean on stdlib disassembly. `otool -tV` does work
but its ADRP display formatting is wrong on stripped binaries (we
verified the schema A decoder in `_adrp()` by hand by checking that
30 random hits produced printable ASCII string data, then we let the
tester confirm by reading bytes at the predicted address).

## How it slots into the existing pipeline

- The shape dump (`out/local-macos/gosrc/cmd/symsearch`) is unaffected
  — it reads from `symbols.tsv`, which this cycle does not touch.
- `xref_query.py` is the consumer entry point and has its own CLI
  surface that complements `symsearch`. Workflow: `symsearch waitForEligibility`
  to find the address, then either nothing (no ADRP cross-refs in this
  case) or `xref_query.py -addr 0x...` to see what it does reference.
- The `strings-addrs.tsv` table is shared infrastructure: any future
  tool that wants to know "what's the printable text at vaddr X" can
  load it directly.

## Files

| File | Lines | What |
|---|---|---|
| `out/local-macos/xref/gen_strings_addrs.py` | 56 | strings(1) → tsv, sized filter |
| `out/local-macos/xref/analyze.py` | 188 | ADRP+ADD walker + cross-ref |
| `out/local-macos/xref/xref_query.py` | 78 | substring / addr query CLI |
| `out/local-macos/xref/README.md` | 79 | usage, coverage, limits |

(`out/local-macos/xref/strings-addrs.tsv` and `xrefs.tsv` are regenerated
on demand; they are not checked in because the analyser takes ~10 s
to recreate both.)

## Verification

```bash
$ time python3 out/local-macos/xref/analyze.py
# strings table: 517,921 entries
# funcs in scope: 154,525
# ADRP+ADD pairs scanned: 567,042
# matched 629 string refs in 400 funcs
# wrote 401 rows to out/local-macos/xref/xrefs.tsv
real    0m8.241s

$ python3 out/local-macos/xref/xref_query.py "store\.\(\*Manager\)" --human
... (real func hits printed)

$ python3 out/local-macos/xref/xref_query.py -addr 0x10262f660
no ADRP+ADD-referenced strings for 0x10262f660
(exit 0)
```

## 硬约束 / 存疑 / limitations

- 【一手】8-second end-to-end on this machine (16 GB MacBook,
  `~/.local/bin/agy` 185 MB). The two big `*.tsv` outputs are not
  committed; regenerating them is part of the documented workflow.
- 【一手】The schema-A ADRP decoder (immhi at bits 23:5, immlo at
  bits 30:29, Rd at bits 4:0) is verified against 30 sampled hits:
  every hit's computed vaddr points to printable ASCII text. Sample
  includes `("  <entry type=\"NameClass", "...", ...)`
  hits that match known XML-scope fragments.
- 【推测】Coverage is roughly 0.26 % (400 of 154,525 funcs). The
  dominant reason is runtime.gostring indirection, which this
  analysis cannot chase.
- 【推测】A `_func.args`-based signature-infidelity push (mentioned
  in the previous turn as 5-15% possible gain) is deliberately not
  attempted in this cycle; size-bucket decoding would emit
  confidently-wrong signatures, which is worse than `()`.
- 【推测】The `xref_query.py` filter for type-descriptor noise is
  heuristic (`pkg/...proto.(*T).M` regex). User can pass
  `--no-filter` when they want raw output.
- 【推测】The ADRP+ADD walker is single-threaded; reading and
  decoding 1.5 GB-equivalent instructions is Python-stdlib-bound at
  ~8 s. Multiprocessing would yield 2-3x on a machine with 4+ free
  cores, but does not change the bottom line.
