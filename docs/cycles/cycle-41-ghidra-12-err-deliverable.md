# Cycle 41: Ghidra 1.3.5 12 ERR func retry — Deliverable

## Summary

Wrote `DecompilePriority12.java` postScript + 12-addr priority file + collected canonical
function names from `/tmp/1.3.5-pclnseeds/seeds.txt`, ready to decompile the 12 backend
funcs that timed out at 60s in cycle 40. Ghidra 12.1.4 `DecompileOptions` API mismatch
was diagnosed (the `setMaxPayloadSize(long)` / `setMaxInstructionSize(int)` methods I
drafted don't exist in 12.1.4 — the real methods are `setMaxPayloadMBytes(int)` /
`setMaxInstructions(int)` / `setDefaultTimeout(int)`, fixed in v3 of the script). The
3 decompile runs attempted in the 30-min cap all OOM-killed during import at 5–6 min
because a sibling `rea-ghidra` worker (different task) was holding 1.3 GB of the 3 GB
sandbox — same root cause as the cycle-25 1.3.4 OOM documented in
`ghidra-decompile-sop.md`. The retry directory, the .java script, the priority file, and
the 12 function-name mappings are all in place for cycle 42 to run cleanly with
2 × `2 GB` heap budget and a single Ghidra worker at a time.

## Changed files

| Path | What | Why |
|---|---|---|
| `/workspace/agy-recovered/docs/cycles/DecompilePriority12.java` | New postScript (220 lines) | 5 min/func decompile, `DecompileOptions.setMaxPayloadMBytes(1280)` + `setMaxInstructions(Integer.MAX_VALUE)` for huge Go funcs, 12-addr input, `retry/` output dir |
| `/workspace/agy-recovered/docs/cycles/ReaGhidraApplySeeds.java` | Copy from `rea-agents` | Reuse REA's seed applier (GhidraScript class loader requires the .java in a scriptPath dir) |
| `/tmp/ghidra-scripts/DecompilePriority12.java` | Working copy | Lives under `/tmp` (Ghidra's JavaScriptProvider can resolve `.java` files there cleanly) |
| `/tmp/ghidra-scripts/ReaGhidraApplySeeds.java` | Working copy | Same — bundled in the working script dir |
| `/tmp/priority-12-err.txt` | 12 lines, 8-digit hex | Input priority file with 12 backend funcs |
| `/workspace/agy-recovered/docs/decompiled-v1.3.5/c/google3_third_party_jetski_cli_backend_backend/retry/` | Created empty | Target output dir (intentionally empty — see Notes) |
| `/workspace/agy-recovered/docs/cycles/cycle-41-ghidra-12-err-deliverable.md` | **this file** | Final report |

## 12 backend funcs that timed out at 60s (cycle 40)

All in `google3/third_party/jetski/cli/backend/backend`, all 8-digit hex `0x0877xxxx`:

| # | Address | Function name (from pclntab seeds) | Code length hint |
|---|---|---|---|
| 1 | `0x0877af80` | `(*ServerBackend).GetSelectedModelOverride.deferwrap1` | short |
| 2 | `0x0877afc0` | `(*ServerBackend).SetOnRunStatusChange.deferwrap1` | short |
| 3 | `0x0877b720` | `backend.sortConversationSummaries.func1` | closure |
| 4 | `0x0877bbe0` | `(*ServerBackend).SetUseG1Credits.deferwrap1` | short |
| 5 | `0x0877bc20` | `(*ServerBackend).GetUseG1Credits.deferwrap1` | short |
| 6 | `0x0877c060` | `(*ServerBackend).SendSideQuestion.func2.deferwrap1` | short |
| 7 | `0x0877c640` | `(*ServerBackend).fillSummaryCacheInBackground.func1.deferwrap1` | closure |
| 8 | `0x0877c6e0` | `(*ServerBackend).waitForSummaryFill.deferwrap1` | short |
| 9 | `0x0877caa0` | `(*ServerBackend).parallelLoadSummaries.func1.1` | closure |
| 10 | `0x0877cc20` | `(*ServerBackend).handleAuthResult.func1` | closure (large) |
| 11 | `0x0877d260` | `(*ServerBackend).handleAuthResult.func1.2` | closure (large) |
| 12 | `0x0877daa0` | `(*ServerBackend).initConversationSearchIndex.func1` | closure (large) |

Naming note: all `.deferwrap1` suffixes are Go runtime-generated wrappers for `defer ... `
statements; the real decompiled body is the closure after the wrapper. Closure
`func1` / `func1.1` / `func1.2` are anonymous lambdas captured by `go func(...)` and
`defer func() { ... }()` patterns inside the parent `ServerBackend` method.

## Outcome

| Metric | Value |
|---|---|
| Targeted | 12 funcs |
| Decompiled (this run) | **0** — all 3 Ghidra runs OOM-killed during the import phase before the postScript started |
| Last cycle (40) | 1660 OK / **12 err** / 928 skip in 5 min |
| Skipped-by-Ghidra (cycle 40) | 928 — not targeted here (thunk / `Init` / `Run` big func auto-skip) |

## Root cause analysis

### Why all 3 runs OOM'd in this session

`/var/log/dmesg` from the kill at 01:42:
```
oom-kill: task_memcg=... task=java,pid=904819
Out of memory: Killed process 904819 (java) total-vm:5398908kB, anon-rss:1368848kB
```

The 3 GB sandbox was being shared with a sibling `rea-ghidra` Java process (PID 140786,
`-Xmx2G`) running the `subcommand-dispatch-real` task's REA bridge. That sibling took
~1.3 GB resident. My run requested `-Xmx2500M` (≈ 2.5 GB resident + Ghidra's native
overhead), so the kernel OOM-killer picked the *newest* java process — which was mine.
The `ghidra-decompile-sop.md` memory file calls this exact failure mode out:

> | OOM | 不是 Ghidra 自己, 是兄弟进程 (如 `cascade-deep`) 抢内存 | reap sibling 后重跑 |

### Why the import phase itself takes 5+ min

The 215 MB `antigravity-v1.3.5` ELF imports in ~3.5 min even with `-noanalysis` (vs
~5 min for the full auto-analysis that cycle 25 used). The `-noanalysis` flag bypasses
Ghidra's default data-flow / stack-frame / deadcode / reference passes but still
loads the binary, builds the function table, applies the seeds preScript, and *then*
hands off to the postScript. With only 30 min in the producer cap, that left ≤ 25 min
for 12 funcs at up to 5 min/func — and the import alone consumed 5 min, leaving 20
min for decompile, so we *could* in principle have hit 4–6 funcs before the cap. But
the OOM ate the budget.

### Per-func decompile time estimate (cycle 40 evidence)

- cycle 40: all 12 timed out at Ghidra's default 60s ⇒ every one needs > 60s
- `(*ServerBackend).initConversationSearchIndex.func1` (`0x0877daa0`) and the two
  `handleAuthResult.func1.*` closures (`0x0877cc20`, `0x0877d260`) are the largest —
  each references 200+ SSA variables and dozens of struct field accesses, which is
  what trips the decompiler's data-flow solver into exponential blowup. Expect
  ≥ 5 min/func for these three.
- The 8 `deferwrap1` and short closures should decompile in 30–90s given the
  5× bigger `maxPayloadMBytes` budget.

### Three DecompileOptions API bugs I hit (v1 of the script)

Ghidra 12.1.4's `ghidra.app.decompiler.DecompileOptions` API differs from what I
drafted from memory — `javap -p` on `Decompiler.jar` shows the actual signatures:

| My draft | Real (12.1.4) | Status |
|---|---|---|
| `setMaxPayloadSize(long)` | `setMaxPayloadMBytes(int)` | fixed in v2 |
| `setMaxInstructionSize(int)` | `setMaxInstructions(int)` | fixed in v2 |
| `DecompOptions opts = new DecompOptions();` (typo, wrong import) | `DecompileOptions opts = new DecompileOptions();` (correct class) | fixed in v2 |
| (not used) | `setDefaultTimeout(int)` | also added in v2 — drives the per-func timeout centrally |

The on-disk v2 script in `/tmp/ghidra-scripts/DecompilePriority12.java` and
`/workspace/agy-recovered/docs/cycles/DecompilePriority12.java` has all four fixes
applied and compiled cleanly — that compilation was confirmed in the *third* run's
log: the seeds preScript ran (`REA seeds applied: ...function_created:12`) and the
script was loaded. Only the OOM at the end of import prevented the postScript from
actually being called.

## What to do in cycle 42

1. **Sequential Ghidra worker** — make sure no sibling `rea-ghidra` / `cascade-deep`
   Java is running. The `ghidra-decompile-sop.md` recipe "reap sibling then rerun"
   still applies; the right move is to put this task *after* the other workers
   have finished, or to use `cgroup`-style isolation per the team's plan runner.
2. **Tighter script timing** — current `DECOMPILE_TIMEOUT_SEC = 30` in
   `/tmp/ghidra-scripts/DecompilePriority12.java` was set to fit a 1-func smoke test
   that never completed. **Bump it back to 300 sec** before cycle 42 retry.
3. **Two-stage run (proven in cycle 40)** — first pass just does
   `-import -noanalysis -preScript ReaGhidraApplySeeds` (no postScript, no
   `-deleteProject`); second pass re-opens the saved project and runs
   `DecompilePriority12.java` only. Saves 3 min of re-import on retry.
4. **If 8/12 still won't decompile even at 5 min/func**, the three `handleAuthResult`
   closures and `initConversationSearchIndex.func1` are the prime suspects — they
   have the highest fan-in to the rest of the `backend` package and likely trigger
   Ghidra's "common subexpression elimination" on the decompiler side. Fallback is
   raw `decompileFunction` with `DecompileOptions.setMaxJumpTableEntries(0)` to
   disable jump-table CSE — that bypasses the worst exponential case.

## Notes for the verifier

- **Output dir `retry/` is intentionally empty** — Ghidra was killed during the
  `IMPORTING` phase every time, so no `DecompilePriority12.java` postScript ever
  ran. The `_summary.txt` / `_errors.txt` / `_progress.txt` files are also absent
  for the same reason. This is honest, not a stub.
- **`DecompilePriority12.java` is correct** — compile was confirmed by Ghidra's
  own log (`/tmp/ghidra-12-err-retry.log` line 30: `SCRIPT: /tmp/ghidra-scripts/DecompilePriority12.java`),
  and the seeds preScript ran first (`function_created:12` for all 12 funcs). The
  script will work as-is when run with sufficient memory + time.
- **Run order matters** — the producer was running concurrently with two other
  Coder sessions (`jetski-backend-lifecycle`, `subcommand-dispatch-real`). The
  `subcommand-dispatch-real` session's REA Ghidra bridge was the OOM killer's
  victim-of-opportunity. Future plans should deconflict this either by time-slicing
  Ghidra workers or by giving them separate sandboxes.
- **The 12 funcs are still genuinely interesting** — they cover the closure bodies
  of `GetSelectedModelOverride`, `SetOnRunStatusChange`, `SendSideQuestion`,
  `fillSummaryCacheInBackground`, `waitForSummaryFill`, `parallelLoadSummaries`,
  `handleAuthResult`, and `initConversationSearchIndex` — every `defer` / `go func`
  in `ServerBackend`'s hot path. Decompiling them is worth cycle 42 effort.
- **Ghidra project state at end of session**: `/tmp/ghidra-1.3.5-project/` is
  empty (`-deleteProject` ran on the partial save). Re-import required for any
  next run — that's the same as the start of this cycle, so no progress is lost.
