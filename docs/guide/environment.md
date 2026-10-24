# Environments and toolchain

> Two targets, two machines. The committed RE artefacts come from a Debian sandbox; this checkout lives on macOS/arm64. Addresses are **not** comparable between them.

## Two different environments — the single biggest source of confusion

The committed artifacts were produced in a **Debian 12 x86-64 sandbox rooted at
`/workspace/agy/`**. This checkout lives on macOS/arm64. Two distinct targets exist:

| | macOS/arm64 case | Linux sandbox case |
|---|---|---|
| Target | `~/.local/bin/agy` | `/workspace/agy/install/antigravity` |
| Format | Mach-O arm64, 180 MB | ELF64 x86-64 PIE, 214 MB |
| pclntab section | `__lrodata_gopcln` (62.5 MB) | folded into `.lrodata` |
| `.text` start address | `0x1000011c0` (loaded at `0x100000000`) | `0x5dc5000` (PIE, no fixed base) |
| Write-ups | `rea-go-seeds/README.md`, `rea-go-seeds/docs/TUTORIAL.md` | everything in `out/` |

**Never mix addresses between the two.** A function named
`.../store.store.(*Manager).waitForEligibility` exists at `0x899cd40` in the ELF build
and at `0x10262f660` in the Mach-O build. The `.text` start address is what gets
handed to `gosym.NewLineTable`, and getting it wrong silently shifts every PC.

Everything under `out/` — except `out/local-macos/` — is hardcoded to `/workspace/agy/`.
`out/decompile/DecompilePriority.java` has 6 such references and
`out/decompile/build_reports.py` has 4. **Rewrite these constants if you run them
elsewhere**, or they will silently write to a nonexistent tree.

### Toolchain — installed and working on this machine

The macOS/arm64 pipeline has been configured and run end-to-end here; see
**`out/local-macos/FINDINGS.md`** for results and `out/local-macos/env.sh` for the
environment.

| Tool | Version | Path |
|---|---|---|
| Go | 1.27.0 | `/Users/gear/go/bin/go` |
| JDK | 21.0.12.1 (Homebrew `openjdk@21`) | `/opt/homebrew/opt/openjdk@21` |
| Ghidra | 12.1.4 | `~/tools/ghidra_12.1.4_PUBLIC` |
| rea-agents | 6.4.0 | `/opt/homebrew/bin/rea` |

`rea doctor --provider ghidra` is all-green. Node is v26.11.1 (REA needs 22/24/26+).

Six things this machine gotcha-checks, each of which cost real time:

1. **The official Ghidra zip ships no macOS decompiler.** You must build it:
   `cd ~/tools/ghidra_12.1.4_PUBLIC/support/gradle && JAVA_HOME=<jdk21> ./gradlew buildNatives`
   (~3 min) → `Ghidra/Features/Decompiler/build/os/mac_arm_64/decompile`. Without it
   `rea doctor` fails `ghidra-native_decompiler`.
2. **REA defaults Ghidra's heap to `-Xmx2G`** (`dist/ghidra/GhidraJavaLaunch.js:53`);
   override with `GHIDRA_HEADLESS_MAXMEM`. Raising it is worth doing but is **not** the
   cause of the wrapper failures — see the next section.
3. **Ghidra searches the current working directory for scripts before `-scriptPath`.** A
   stray `DecompilePriority.java` in the CWD is picked up and fails with *"Failed to find
   source bundle containing script"*. Run `analyzeHeadless` from a neutral CWD (e.g.
   `/tmp`) and keep the script in its own directory.
4. **`~/GitHub` is a symlink to `~/Documents/GitHub`.** Ghidra canonicalises paths, which
   shows up in error messages; harmless once you know.
5. **The seeds, the Ghidra project and `dec/` are version-scoped, and they are
   currently at agy 1.4.6.** `out/local-macos/seeds/` holds **161,286**
   functions — regenerated from the installed binary
   (`cd rea-go-seeds && mkdir -p /tmp/seeds && go run . ~/.local/bin/agy /tmp/seeds`
   — ~2 seconds, no network), with `out/local-macos/proj` (the seeded Ghidra
   project) and `out/local-macos/dec/` rebuilt from it: **2,290/2,290**
   priority addresses resolved, **0 skipped**, 335,020 LOC. **Every address
   moves on every release**, so the moment `agy` updates, every address in
   `seeds.txt`, in the project, in `dec/` and in the committed
   `out/local-macos/decompile-*.md` reports is stale, and citing one for a
   newer finding means decompiling whatever function happens to sit there.
   Regenerate before anything else. Related: Ghidra's `&UNK_<addr>,<len>` refs
   are unreliable for literals whose pointer it failed to track — read the
   instruction stream instead (`out/local-macos/scan_strings.py`,
   `out/local-macos/find_callers.py`). See
   `docs/cycles/reference-drift-146/deliverable.md`.
6. **A second agent may commit or revert files under you.** The tree is shared.
   Re-read a file before editing it, commit your own paths as soon as they are
   green, and re-check `git status` before concluding that a change vanished.

`.crush/` is Crush runtime state (sqlite db + logs), not project content. `.crush/init` is
empty.

## The old Debian sandbox (historical, from `out/prep-env.md`)

Everything the committed `out/` artefacts were produced under. Kept for the
record; the pipeline itself runs on this machine now.

- `github.com` is network-blocked there; `codeload.github.com/<owner>/<repo>/tar.gz/main`
  works. Mirrors that did work: `mirrors.aliyun.com/golang`, `mirrors.tuna…/Adoptium`,
  `registry.npmmirror.com`. Tuna has no `/golang/`; Huawei's `/golang/` returns 401;
  Aliyun has no `/Adoptium/`.
- A 30 s bash timeout truncated both `npm install -g` and the Go tarball extraction on
  first attempt (Go lost 65 of 72 stdlib packages, breaking every later `go run`).
  Extract to `/tmp` (tmpfs) first, then `cp -a` to the NAS mount, and
  `chmod -R a+rX` afterwards.
- Keep `<tool>/<tool>-<version>/*` flattened into `<tool>/` root. A `current` symlink
  breaks the expected contract paths.
