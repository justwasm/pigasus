# Cycle 371 deliverable: the documents, realigned with the source

**Cycle:** `cycle-371-doc-realignment`
**Subject:** the tree had moved three times since the documents were last
written — the **five-tier refactor** (`c00afe34` → `9ff6d833`, 2026-10-30),
the **1.4.11 RE regeneration** (`cc4690f8`), and the **config-home
consolidation** (cycle 370) — and the documents had not followed. A reader
who followed `AGENTS.md` and `README.md` landed on paths that no longer
existed, on numbers that had moved, and on a plan that had already been
executed. This cycle re-aligns them with the source, and removes the dead
shells the audit turned up on the way.
**Evidence:** 【一手】 — every number below was read out of the tree on this
machine (the seed set, the decompile summary, the facade map, the parser's
own subcommand table, the built binary's size), not copied from an older
document.

---

## 0. The short answer

| # | what | the answer |
|---|---|---|
| 1 | the defect | `README.md` and `AGENTS.md` described a tree that had been refactored under them; **104** path references pointed at facade shells, and 14 numeric claims were stale |
| 2 | the fix | every path now names the package that **holds the implementation**; every number was re-measured against the tree |
| 3 | the new document | `docs/guide/architecture-tiers.md` — the five tiers, the 58-facade map, what did *not* move, and the rule for new packages |
| 4 | the retired document | `docs/guide/architecture-refactoring-plan.md` — a *plan*, for a refactor that finished nine commits later. A plan is not a description; it was deleted and replaced by the record |
| 5 | the dead code | eight top-level `internal/` shells with **zero** importers were deleted (see §2) |
| 6 | the golden that drifted | `internal/cli/testdata/help_1.4.6.txt` is no longer the reference's untouched output — it carries the clone's own rows. Its doc comment said otherwise for several cycles |
| 7 | the flagged, not deleted | five legacy evidence directories (~57 MB of 1.3.3-era output) are referenced only by historical cycle reports; they are **flagged** for a decision, not removed |

---

## 1. What had drifted, and how it was found

The audit was mechanical first and editorial second.

**Mechanical.** Every `internal/…` path in every document under
`docs/guide/`, plus `README.md`, `AGENTS.md` and
`internal/agyproxy/README.md`, was extracted and resolved against the tree.
A path that did not exist was either (a) a facade path — a shell that exists
but holds no implementation — or (b) a path that had moved into a tier.

**The facade rule.** A top-level `internal/` package that has a `facade.go`
is a *name*, not a location: the implementation lives at the tier path the
facade re-exports. Fifty-eight packages are in that state, and **219 files**
still import one. Any document that named a facade path as "where the code
is" was wrong twice over — the file it pointed at does not exist, and even if
it did, it would be a re-export.

**Editorial.** Numbers that had moved were re-measured rather than
remembered: the seed count, the decompile table, the binary's size, the
cycle count, the PiG version, the subcommand table.

---

## 2. The dead shells that were deleted

Eight top-level `internal/` packages had **no importer anywhere in the
tree** — not one file outside their own directory referenced them. All
eight were `facade.go` shells left over from the five-tier refactor or from
the public-package extraction, and all eight re-exported a package that
lives elsewhere:

| deleted shell | re-exported | importers |
|---|---|---|
| `internal/celpolicy/` | `celpolicy/` (public) | 0 |
| `internal/dns/` | `dns/` (public) | 0 |
| `internal/urlchunk/` | `urlchunk/` (public) | 0 |
| `internal/procgroup/` | `procgroup/` (public) | 0 |
| `internal/mediagen/` | `internal/features/tools/media` | 0 |
| `internal/mcpoauth/` | `internal/features/auth/mcpoauth` | 0 |
| `internal/parityverify/` | `internal/infra/proxy/parity` | 0 |
| `internal/rea/` | `internal/infra/proxy/rea` | 0 |

`internal/celpolicy/eval.go` was the odd one out — a *typed* re-export with
a `Deprecated:` marker, 22 LOC plus 277 LOC of test. The other seven were
symbol aliases of 7–26 LOC each.

**What was deliberately *not* deleted:**

- **`internal/features/auth/mcpoauth` (1,477 LOC + 829 LOC of tests).** Its
  implementation has zero importers too, but it was built for the AGY 1.3.4
  changelog line *"Fixed remote MCP servers that use OAuth signing you out
  after a temporary …"* — a behaviour the clone may still owe a user. Only
  the re-export shell went; the implementation stays until that changelog
  line is settled.
- **The 1.3.3-era RE skeletons** — `internal/cortex`,
  `internal/language_server`, `internal/language_server_pb`,
  `internal/codeassistclient`, `internal/codeium_common_pb`,
  `internal/extension_server_pb`, `internal/planrender`. They have no
  importers either, but they are **evidence**, not program: they are the
  recovered shapes the newer packages were written against. `docs/guide/`
  cites them by name.

---

## 3. The plan that became a record

`docs/guide/architecture-refactoring-plan.md` was a forward-looking plan:
*"Approved architectural plan for refactoring OpenAGY / Pigasus into a
clean, 5-tier modular hierarchy with zero-breaking progressive migration."*

The refactor it described was **executed** on 2026-10-30, in nine commits
(`c00afe34` → `9ff6d833`), and the plan's own "migration complete"
acceptance criteria are met. A plan describes a future that has already
happened; keeping it invites a reader to check whether it is *still* a plan
and to plan against it.

It was replaced by **`docs/guide/architecture-tiers.md`**, which records:

- the five tiers and what lives in each (`core` → `app` → `features` →
  `infra` → `ui`);
- the **58-facade map** — every old path beside its tier path, in one
  table;
- the packages that **did not move**, and why (the two turn engines, the
  two transports, the prompt fixture, the Web UI's own surface, the proto
  skeletons that are evidence);
- `internal/modular`'s actual role — the *registration* surface, not a tier;
- the four rules for new code, the fourth of which is the one this cycle
  executed: **a facade whose importers are gone is deleted, not kept**.

`docs/guide/architecture.md` now says explicitly that it is the *wiring*
and that the *directory layout* lives in the new file.

---

## 4. The path rewrites

**104** references were rewritten from facade paths to tier paths, and
the substitution was mechanical: each facade's own `facade.go` names its
target, and the rewrite was verified by re-resolving every rewritten path
against the filesystem.

| old (facade) | new (tier) |
|---|---|
| `internal/accounts` | `internal/features/accounts/core` |
| `internal/accountrouting` | `internal/features/accounts/routing` |
| `internal/agystep` | `internal/app/prompt/step` |
| `internal/implicit` | `internal/app/prompt/implicit` |
| `internal/cumulativeprompt` | `internal/app/prompt/cumulative` |
| `internal/builtinskills` | `internal/features/tools/skills` |
| `internal/clipboard` | `internal/features/tools/clipboard` |
| `internal/toolrunner` | `internal/features/tools/toolrunner` |
| `internal/modelcatalog` | `internal/features/models/catalog` |
| `internal/modelctx` | `internal/features/models/ctx` |
| `internal/agybin` | `internal/infra/proxy/bin` |
| `internal/rclink` | `internal/infra/remote/link` |
| `internal/persistentrc` | `internal/infra/remote/persistent` |
| `internal/browsersubagent` | `internal/app/subagent/browser` |
| `internal/builtinsubagent` | `internal/app/subagent/builtin` |
| `internal/rcterminal` | `rcterminal/` (public) |
| `internal/urlchunk` | `urlchunk/` (public) |
| `internal/dns` | `dns/` (public) |
| `internal/geminiwire` | `geminicode/` (public) |

Two rewrites needed care:

- **`internal/browsersubagent`**'s `facade.go` imports `internal/agyprompt`
  *first* (for a type in a helper), so a naive "first import = target" rule
  picks the wrong package. Its real target is
  `internal/app/subagent/browser`, and that is what the documents now say.
- **`internal/rcterminal` does not exist at all** — it was never a facade;
  the terminal became the public `rcterminal/` package directly. Four
  documents still pointed at the old path.

`AGENTS.md` carries the same rule as a new invariant block, **"The tree"**,
placed first among the load-bearing invariants, because it is the one that
every other path in the file depends on.

---

## 5. The numbers that had moved

Every one of these was re-measured against the tree, not against an older
document.

| claim | was | now | where it was measured |
|---|---|---|---|
| pclntab seeds | 161,286 (1.4.6) | **168,548** (1.4.11) | `out/local-macos/seeds/seeds.txt` |
| priority functions decompiled | 2,290 (335,020 LOC) | **2,397 / 2,397** (350,577 LOC) | `out/local-macos/decompile-summary.md` |
| per-package reports | 102 | **112** | `out/local-macos/decompile-*.md` |
| binary size | ~110 MB | **~139 MB / 132 MiB** | the built `cmd/pigasus` (138,651,026 B) |
| PiG | v0.5.2 | **v0.5.3** | `go.mod` (commit `450f19a8`) |
| visible subcommands | 12 | **18** (reference 12 + clone 6) | `pigasus --help` |
| cycle directories | 414 | **523** | `docs/cycles/` |
| top-level facades | (unstated) | **58**, with **219** files importing one | the tree |
| public packages | (unstated) | **31** | `docs/guide/public-packages.md` |
| capture ceiling | (unstated) | **1 GiB** (`DefaultCaptureMaxBytes`) | `internal/agyproxy` |

The RE numbers moved because **the pipeline was regenerated** at 1.4.11
(commit `cc4690f8`) — the previous documents still said "the pipeline was
*not* regenerated", which was true for 1.4.7 and 1.4.8 and stopped being
true at 1.4.11. `out/local-macos/FINDINGS.md` now leads with a 1.4.11
"current tree" section and keeps the 1.4.6 run below it as the 1.4.6
record.

`out/ide-language-server/README.md` was **not** rewritten: it carries its
own "不要手工改" (*do not edit by hand*) header, and its 164,772-name diff
table belongs to a dated IDE-side run. It is flagged in §7 instead.

---

## 6. The golden that had drifted

`internal/cli/testdata/help_1.4.6.txt` is the reference binary's captured
`--help` output — it was, at 1.4.6, byte for byte. It no longer is:

- commit `78f4a3b9` added the clone's `accounts`, `model` and `provider`
  rows;
- commit `6bd0850c` added `halloween`;
- the binary adds `proxy` and `web` outside the parser entirely.

So the file now pins the **clone's sixteen parser-level rows**, of which
the reference's own twelve are a subset — and the reference's own twelve
are no longer pinned by any test. The doc comment above the golden claimed
the file was "the reference binary's own output" and that "the golden keeps
its name and its bytes". That was true when written and false now; the
comment now says so, and says what the file actually pins.

Two stale comments next to it were corrected in the same pass:
`internal/cli/parser.go` claimed "10 visible subcommands (8 spec + agents
alias + changelog)" and `internal/cli/parser_specs.go` claimed "10 hidden
subcommands" — both numbers were from v1.3.5.

---

## 7. What was flagged, not changed

| item | why it was left |
|---|---|
| `re-decompiled-1.3.3/` (2,152 files, 43 MB) | the evidence backing the 1.3.3-era cycle reports; the reports cite it by path. The `c-1.3.4/` duplicate inside it was retired on 2026-11-02 — see `docs/legacy-re/README.md` |
| `go-recovered/` (861 files, 14 MB) | same — the 1.3.3 recovered Go tree, cited by `docs/legacy-re/extracts/decompiled-v1.3.4/README.md` (the surviving copy of the retired `docs/decompiled-v1.3.4/README.md`) and `README-recovered.md` |
| `re-docs/` (31 files) | the 1.3.3-era RE documents |
| `v4-captures/` (16 files) | the v4 TUI capture cited by `docs/architecture/v1.3.5-tui-panels-real.md` |
| `memory/` (5 files) | the working notes cited by the old cycle reports |
| `out/ide-language-server/README.md` | carries its own "do not edit by hand" rule; its numbers belong to a dated IDE-side run |
| `npm/pigasus/` | **live** — it is in `scripts/publish-snapshot.sh`'s allowlist |

The first five are the open decision: ~57 MB of evidence that only the
historical cycle reports reference. Deleting them would orphan those
reports' evidence; keeping them keeps a large, old tree in the checkout.
**This cycle did not decide it** — see 存疑.

---

## 8. What changed, file by file

| file | what changed |
|---|---|
| `README.md` | seeds 161,286 → 168,548; reports 102 → 112; cycles 414 → 522; PiG v0.5.2 → v0.5.3; the subcommand table rewritten from the reference's 12 to the clone's 18 (with the source of each row); `wrapper.{json,log}` → `wrapper.log` + the `"wrapper"` section of `config.json`; the 1 GiB capture ceiling; the 31 public packages; the `devutils/` list completed; the guide table completed; the badge/module-name mismatch reconciled; the test command extended to `./endpoints/... ./configdir/...`; the five-tier layout paragraph added |
| `AGENTS.md` | a missing `>` fixed in the guide index; nine facade paths corrected; `internal/rcterminal` → `rcterminal/`; the binary size → 132.2 MiB; the entry-point list extended (`cmd/agy-update`, `cmd/langserver`, `cmd/pigga`); the test command extended; **the new "The tree" invariant block**; a guide-index row for `architecture-tiers.md` |
| `docs/guide/architecture-tiers.md` | **new** — the five tiers, the 58-facade map, what did not move, `internal/modular`'s role, the rules for new code |
| `docs/guide/architecture-refactoring-plan.md` | **deleted** — a completed plan, replaced by the record above |
| `docs/guide/README.md` | the index completed (20 guide files, every one of them listed; `architecture-tiers.md`, `accounts.md`, `endpoints.md`, `plugins.md`, `cloudcode-dialect.md`, `remote-control.md`, `proxy.md`, `compaction.md`, `localharness.md`, `public-packages.md`, `environment.md`, `ide-language-server.md` all now listed) |
| `docs/guide/{accounts,architecture,agent-loop,cloudcode-dialect,compaction,environment,ide-language-server,localharness,models,prompt,proxy,public-packages,reference-drift,remote-control,reverse-engineering,tools,tui}.md` | 104 path rewrites; the stale numeric claims; the "hidden subcommand" claims corrected (`provider` / `model` / `accounts` / `proxy` became visible in `78f4a3b9`, `halloween` in `6bd0850c`) |
| `internal/cli/help_golden_test.go` | the doc comment corrected — the golden is no longer the reference's untouched output |
| `internal/cli/parser.go`, `internal/cli/parser_specs.go` | stale subcommand counts corrected |
| `out/local-macos/FINDINGS.md` | a 1.4.11 "current tree" section added; the 1.4.6 run kept below as the 1.4.6 record |

---

## 硬约束 / Hard constraints

- **A facade path is a name, not a location.** A top-level `internal/`
  package with a `facade.go` holds no implementation. Documents name the
  tier path; if the old name matters to a reader, they say it is a facade.
- **The five tiers are `core` → `app` → `features` → `infra` → `ui`**, and
  the package's own directory is the unit of placement — the tier is chosen
  by what the package depends on, not by where it was born.
- **A new package gets no facade.** A facade only makes sense for a
  package that already had callers at the old path.
- **A facade whose importers are gone is deleted.** Eight such shells went
  in this cycle; the rule is in `docs/guide/architecture-tiers.md`.
- **`internal/modular` is the registration surface, not a tier.** Its
  `subcommands/*` and `subsystems/*` directories are blank-import shims
  over the tier packages, and `cmd/pigga` (the WASM build) depends on
  that.
- **The 1.3.3-era RE skeletons are evidence, not program.** They have no
  importers and stay.
- **The `--help` golden pins the clone's rows, not the reference's.** The
  reference's own twelve are a subset of the table and are no longer pinned
  by any test.
- **`out/ide-language-server/README.md` is not hand-edited.** Its own rule
  says so.

## Open questions / 存疑

- **The ~57 MB of legacy evidence directories is an open decision.**
  `re-decompiled-1.3.3/`, `go-recovered/`, `re-docs/`, `v4-captures/` and
  `memory/` are referenced only by historical cycle reports and the
  1.3.3-era documents. Deleting them orphans that evidence; keeping them
  keeps a large old tree in every checkout. They are *flagged* here, not
  deleted — the call belongs to the owner of the historical reports.
  **Update (2026-11-02):** the call was taken the other way for the
  rebuildable bulk; see `docs/legacy-re/README.md`.
- **`internal/features/auth/mcpoauth` is still dead code** (1,477 LOC +
  829 LOC of tests, zero importers). Its shell was removed; the
  implementation waits on the 1.3.4 changelog line it answers.
- **The reference's own twelve `--help` rows are no longer pinned.** The
  golden carries the clone's sixteen. If the reference's table must stay
  pinned, it needs a second golden — the reference's original capture —
  which was overwritten in `78f4a3b9`.
- **The 219 files that still import a facade** are the open end of the
  refactor. Each retirement is mechanical, but 219 files is a real cost and
  the value is only in the eventual removal of the shells.
- **`docs/guide/public-packages.md`'s ROI queue is empty** — every package
  the ranking scored is published. The next public-package candidate has to
  come from a new ranking.

## Limitations

- **The audit covered `docs/guide/`, `README.md`, `AGENTS.md` and
  `internal/agyproxy/README.md`.** The 522 `docs/cycles/*` reports were
  *not* rewritten — they are the historical record and describe the tree as
  it was when they were written. Path references inside them may name
  packages that have since moved.
- **`out/*.md` was not swept.** Only `out/local-macos/FINDINGS.md` was
  updated (it is the RE run's own headline file and it carries the "current
  tree" claim). The per-package decompile reports are regenerated artefacts.
- **The numbers are a snapshot.** The seed count, the binary size and the
  cycle count change with every release and every cycle; this cycle's
  values are correct as of this commit.
- **The facade map is derived from `facade.go` files.** A package that was
  moved *without* leaving a shell would not appear in it — none is known,
  but the map is only as complete as the shells are.

---

Report language note: this file is English because `docs/cycles/*` is English.
The evidence files under `out/` are Chinese, per the repository's convention.
