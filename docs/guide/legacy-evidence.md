# Legacy evidence — what was retired, and how to rebuild it

> The reference moves. Every address, every captured byte and every
> decompiled function has a shelf life, and a stale artefact that *looks*
> current is worse than no artefact at all: it sends the next reader to
> look up an address in the wrong binary.
>
> **The rule this page records:** material whose provenance is a
> **superseded release** is retired in favour of a **recipe** and a
> **pointer**, not kept in bulk.

| I want to… | Read |
|---|---|
| know what was retired and what survived | [`docs/legacy-re/README.md`](../legacy-re/README.md) — the ledger, one section per retired tree |
| rebuild a retired artefact | [`docs/legacy-re/recipes.md`](../legacy-re/recipes.md) — the verbatim commands |
| look up a retired artefact programmatically | [`docs/legacy-re/index.json`](../legacy-re/index.json) — the machine-readable index and the redirect table |
| read the preserved fragments | [`docs/legacy-re/extracts/`](../legacy-re/extracts/) — the cited windows, the cited `.c` files, the PTY logs, the stubs |
| run the RE pipeline **today** | [`reverse-engineering.md`](reverse-engineering.md) — the live recipe, and `scripts/re/` — the scripts |
| know whether the current evidence is stale | [`reference-drift.md`](reference-drift.md) — the gate |

---

## What was retired (2026-11-02)

Two sweeps, both local commits, both verified against the evidence gate.

| sweep | retired | bytes |
|---|---|---|
| **S** — `docs/` | ten 1.3.x-era trees under `docs/` (a 293 MiB Ghidra project database, a 79 MiB decompile tree, the 1.3.4/1.3.5 seed and string dumps, the 1.3.5 decompiled C for 50 packages, the v135 PTY captures, the cycle-20-to-23 binaries) plus the root-level duplicate `re-decompiled-1.3.3/c-1.3.4/` | 561 501 053 B (535.5 MiB) |
| **U** — `out/` | the rebuildable bulk: the 1.4.11 raw `strings` dump, the 1.3.3 ELF seed set, the duplicated IDE frontend bundle, the duplicated `.tmpl` templates, the duplicated `decompile-*.md` reports, the redundant changelog copy, the unreferenced wasm probe | 92 345 977 B (88.1 MiB) |

**The pipeline's scripts moved.** `out/local-macos/{build_priority.py,
build_plan.py, build_reports.py, plan-local.jsonl, ghidra-scripts/**}` are
now **`scripts/re/`** — outside `out/`, which does not ship in the release
snapshot. The guide's invocations and `scripts/macos-re.sh` were repointed
at the new paths.

---

## The three rules that make the retirement safe

1. **A citation must name the string, not the deleted line.** A line
   number into a retired file is a dead pointer. The comment or the
   document quotes the *fact* and names the **surviving copy**.
2. **A retired artefact is not registered in the drift gate.**
   `internal/infra/referencedrift/reference.lock.json` is for artefacts the
   clone *depends on*; a digest mismatch there turns the gate permanently
   red, and a 1.3.x artefact is stale by construction. The gate's own
   exemption (`out/`-prefixed paths are exempt only when the whole `out/`
   directory is absent) is why the sweep preserved the seven registered
   `out/` paths byte for byte.
3. **The recipe must outlive the artefact.** Every retired tree's
   regeneration command is quoted **verbatim** in
   `docs/legacy-re/recipes.md`, next to the gotchas that made it work.

---

## What survived, and why

- **The cited windows.** Five live code comments cited the 1.3.4 string
  dumps for *changelog-line provenance* — "the reference added X in
  release Y". That history is not recoverable by re-dumping the current
  binary. The windows around each cited offset were kept
  (`docs/legacy-re/extracts/strings-v1.3.*-cited.txt`).
- **The pruned strings index.** Thirteen live code comments cited the
  1.4.11 `strings` dump by line number. `out/local-macos-1.4.11/strings-index.txt`
  carries every cited block with its context, the binary's digest and the
  command that regenerates the whole dump.
- **The cited decompiled C.** 217 of the 1 660 `.c` files of the 1.3.5
  run survive under `docs/legacy-re/extracts/decompiled-v1.3.5/c/`, plus
  the 48-package index that says what the run covered.
- **The re-anchored call graph.**
  `docs/legacy-re/extracts/analysis-v1.3.5-official/backend-call-graph.md`
  is cited by live production code (`internal/core/backend/lifecycle.go`).
  Its six addresses are 1.3.5-era and were marked as such; the *shape*
  (the emit FSM and the `ServerBackend` method set) is re-derivable from
  the current seed set.
- **The 1.3.5 PTY logs.** Six tmux screen captures of the reference's
  onboarding screen, approval card and eligibility error — the only
  record of those bytes.
- **The 1.3.4 provenance.** `README.md` and `deliverable.md` of the
  1.3.4 decompile — the binary identity, the OAuth client IDs, the
  S2Av2/mTLS finding, and the run's own recipe.

Everything else was either a duplicate, a re-partition, or rebuildable in
seconds.

---

## The redirect table

The full table is `docs/legacy-re/index.json`'s `redirects` block; each
entry names a retired path, its surviving copy and the kind of redirect
(`windows`, `verbatim`, `cited-subset`, `re-anchored`, `stub`,
`dropped`). `docs/legacy-re/README.md` carries the same table in prose.

---

## Hard constraints

- **硬约束**: a retired artefact is **never** registered in
  `internal/infra/referencedrift/reference.lock.json`. The gate's
  semantics make a superseded release a permanent failure.
- **硬约束**: the seven registered `out/` paths keep their **exact
  bytes**. The gate compares digests.
- **硬约束**: the RE pipeline's scripts live in **`scripts/re/`**, not in
  `out/` — `out/` does not ship in the release snapshot.
- **硬约束**: a citation into retired material names the **surviving
  copy**, and quotes the string rather than the line number.

## Open questions

- **存疑**: the 1.4.11 `strings` dump's **hex offsets** are
  unrecoverable — the committed dump had its `strings -t x` offset column
  stripped. The index therefore carries **line numbers**, not offsets.
- **存疑**: the 1.4.11 dump cannot be regenerated byte-for-byte on this
  machine any more (`~/.local/bin/agy` is 1.4.12). The index was cut from
  the retired dump.
- **存疑**: `out/local-macos/` still holds a large amount of off-list
  intermediate output (`dec/`, `proj/`, `gosrc/`, `xref/`). It is
  gitignored and was left alone; a further sweep needs each lock entry
  re-pointed at the ledger first.

## limitations

- **limitations**: the retirement is a **storage** decision, not a
  correctness claim. Nothing about the reference changed; the artefacts
  simply stopped being current enough to be worth their bytes.
- **limitations**: the decompiled C for 42 of the 1.3.5 run's 50 packages
  is **gone**. It is mechanically rebuildable — the names are in the
  current seed set and the recipe survives — but nobody has run the
  pipeline for those packages at 1.4.12, and the current committed
  reports cover eight packages only. If a future alignment needs those
  bodies, **re-run the pipeline at 1.4.12** rather than reaching for a
  nine-release-old archive.
- **limitations**: the ledger records what each artefact *was* and how to
  rebuild it; it does not certify that a rebuild would be byte-identical
  (the binary moved, so it would not be).
