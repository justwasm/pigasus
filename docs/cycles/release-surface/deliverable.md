# Cycle `release-surface` — the CI, the release workflow and the publish script, after the flattening

**Status**: implemented; `go build ./cmd/... ./internal/...` and
`go test ./cmd/... ./internal/...` green.
**Why**: cycle `wrapper-flatten` deleted `cmd/agy`, renamed the `wrapper`
command group to `proxy`, and cycle `d9907dca` renamed `cmd/wagyu` to
`cmd/pigasus`. Everything *around* the clone — the GitHub workflows, the
publish script, the README that ships inside every release archive — still
described the old surface. This pass closes that gap.

**Evidence grade**: 【一手】for everything below that is marked *verified* —
the claims were read off the built binary, the committed workflows and the
published repository itself, not inferred.

---

## 1. What was breaking

### 1.1 The shipped README documented commands that no longer exist

`release.yml` copies `README.md` into every release archive
(`cp README.md "$raw_staging/README.md"`), so the README *is* the user
documentation of a release. It said:

```bash
agy wrapper providers add gateway --type openai …
agy wrapper models add minimax-m3 --provider gateway \
    --external-model MiniMax-M3 --display-name 'MiniMax M3'
agy wrapper providers list
agy wrapper models list
```

Three things wrong with that block:

| what | why it is wrong |
|---|---|
| `agy wrapper …` | the `wrapper` group and its legacy spelling were deleted by `wrapper-flatten`; the group is `proxy` now, and the registry CLI is `provider` / `model` |
| `--external-model` / `--display-name` | those flags never existed in the registry CLI — the real ones are `--model` (upstream id) and `--name` (display name), see `internal/cli/provider_cmds.go` |
| `agy …` as the clone's spelling | `agy` is the **vendor** binary; the clone's binary is `pigasus`. Typing `agy provider list` runs the vendor and fails |

The block also claimed the registry had **three** front-ends ("clone 的 CLI, TUI
的 `/provider` 面板, wrapper 的 CLI"). After the flattening the proxy only
*reads* the registry (`internal/agyproxy` must not import `internal/cli`), so
there are **two**.

**Fixed**: the block now shows the clone's own CLI (`pigasus provider …` /
`pigasus model …`), the front-end count is two, and every `agy …` spelling
that meant *the clone* became `pigasus …`. The reference's own `agy` spelling
(the vendor binary, `~/.local/bin/agy`) is untouched — the two are different
programs and the README now keeps them apart.

### 1.2 The shipped README documented two flags that are no longer registered

`--tool <name> --tool-args <json>` and `--services` were the clone's own
recovered-snapshot flags. The parse-parity surface dropped them (the
reference `agy` has no such flags), so today:

```
$ pigasus --tool status --tool-args '{}'
flags provided but not defined: -tool
```

The README still had two sections showing them as working commands. Both
sections are replaced by one note that says the flags are gone and why. The
tools themselves are unaffected — they live in `internal/tools/`, they just
have no CLI entry point.

### 1.3 The shipped README's tables and file tree were stale

- "二进制入口 | 8 (`wagyu` / `agy` / …)" — `cmd/agy` was deleted by the
  flattening, `cmd/wagyu` was renamed. The real count is **7**.
- the file tree still showed `cmd/wagyu/  # Antigravity CLI 克隆主程序`.
- the tree header said the module root is `github.com/chadgpt/openagy`; the
  module path is `github.com/justwasm/pigasus` (that is what `go install`
  needs).

### 1.4 `.gitignore` did not ignore the new build output

The file ignored `/wagyu` (the old name) but not `/pigasus`, so
`go build ./cmd/pigasus` in the repo root produced a 102 MB binary that
`git status` reported as untracked. Both names are ignored now, and the
stale `cmd/wagyu/inspect_hometest_test.go` rule points at `cmd/pigasus/`.

### 1.5 The publish script watched the wrong GitHub Actions run

```bash
RUN_ID=$(gh run list --repo "$REPO" --limit 1 --json databaseId -q '.[0].databaseId')
```

`--limit 1` with no `--workflow` returns the newest run of *any* workflow.
The script pushes `main` first (which queues the **CI** run) and only then
pushes the tag (which queues the **release** run), so `--watch` could easily
attach to the CI run — or to a run from a previous release. The lookup now
filters by workflow *and* by head branch (the tag), and polls for up to a
minute for the run to appear. A tag that was *not* pushed (the "already
exists, use `--force-tag`" path) no longer pretends to watch anything.

### 1.6 A failed release could not be re-run

`gh release create "$VERSION"` fails when a release for that tag already
exists. The published repository currently has **both** a published and a
draft release for `v1.4.4` — the exact state a re-run leaves behind. The
publish job now checks `gh release view` first and replaces the assets with
`gh release upload --clobber` instead of dying.

### 1.7 The published snapshot shipped no cycle reports

`scripts/publish-snapshot.sh` syncs an allowlist into the published
repository (`justwasm/pigasus`). The allowlist did not include `docs/`, so
the published tree's own `README.md` and `AGENTS.md` link to
`docs/cycles/…` paths that do not exist there. `docs/cycles` (6.9 MB) and
`docs/research` (56 KB) are now in the allowlist; the rest of `docs/`
(535 MB of Ghidra projects, decompiled dumps and seed chunks) stays out.

### 1.8 The CI never compiled the other entry points

`test.yml` built `./cmd/pigasus` only. `cmd/openagy-models`,
`cmd/openai-shim` and `cmd/anthropic-shim` are compiled by `go test
./cmd/…` but a *build* break in them would only show up in a release build.
There is now an explicit `go build ./cmd/...` step.

### 1.9 Two user-visible strings pointed at a directory that does not exist

`internal/cli/logo_bridge.go` and `internal/cli/dispatch.go` printed
"… (build with cmd/wagyu to enable)" — an instruction the reader cannot
follow, since `cmd/wagyu` is gone. Both say `cmd/pigasus` now.

---

## 2. What was *not* breaking (verified)

| claim | how it was verified |
|---|---|
| The release archive's **layout is unchanged**: the published `v1.4.4` archive already carried a `pigasus` binary *and* a `wagyu` alias (that workflow built `./cmd/wagyu` into a file named `pigasus`) | read `v1.4.4:.github/workflows/release.yml` in the published clone, and rebuilt the archive locally: `pigasus`, `wagyu`, `README.md`, `LICENSE` |
| `pigasus --help` lists exactly **12** visible subcommands; `proxy` stays hidden | ran the built binary |
| `pigasus agy --help` prints the **vendor's** help ("Usage of agy:") | ran the built binary against the installed `~/.local/bin/agy` (1.4.6) |
| The published tree's stale `cmd/agy` and `cmd/wagyu` are removed by the next snapshot | `rsync -av --delete` on the allowlist's directories; verified rsync creates the missing `docs/` parents |
| The proxy's env vars and on-disk paths are unchanged (`OPENAGY_WRAPPER_*`, `~/.openagy/wrapper.{json,log}`) | untouched by this cycle — on purpose: renaming them would move an installed capture directory (11 GB on this machine) |

## 3. What changed in the automation

| file | change |
|---|---|
| `.github/workflows/test.yml` | new `go build ./cmd/...` step; three smoke steps for the surfaces the clone adds on top of the reference's twelve visible subcommands (`proxy version`, `provider list`, `model list`) — all offline-safe: they exit 0 with no vendor `agy` and no registry file |
| `.github/workflows/release.yml` | the `wagyu` alias copy is branch-correct on Windows (no extension-less duplicate next to `wagyu.exe`); the publish step is re-runnable (`gh release view` → `gh release upload --clobber`) |
| `scripts/publish-snapshot.sh` | `--watch` picks the release run *for the tag*; the allowlist ships `docs/cycles` + `docs/research`; the "tag already exists" path no longer claims to watch a run |
| `.gitignore` | ignores `/pigasus` (the new build output) and points the scratch-test rule at `cmd/pigasus/` |
| `README.md` | §1.1–§1.3 above, plus a new section documenting the flattened proxy surface (`pigasus agy`, `pigasus proxy doctor\|log\|capture\|version`) and the `go install` line for the published module |
| `internal/cli/{logo_bridge,dispatch}.go` | the two "build with …" strings |

## 4. 硬约束 / hard constraints

- **`pigasus --help` still lists exactly twelve visible subcommands.** The
  new smoke steps assert the hidden surfaces work; they do not add visible
  ones.
- **The release archive's contents did not change** (`pigasus` + the `wagyu`
  alias + `README.md` + `LICENSE`). No downloader breaks; an existing script
  that calls `wagyu` keeps working.
- **The proxy's environment variables and file paths did not change.** The
  command *surface* was flattened, the *files* deliberately were not.
- **The CI does not reach the network.** Every new smoke step exits 0 with no
  vendor `agy` installed and no registry file present.

## 5. 存疑 / open questions

- **The next release's notes should mention that the drop-in `agy` binary is
  gone.** It was never shipped as a release *artifact* (the workflow only
  ever built `./cmd/wagyu`), so no downloader breaks — but a user who cloned
  the published repository and built `./cmd/agy` loses that path. The
  replacement is `pigasus agy` (or the `wagyu` alias).
- **The published repository's release notes are auto-generated**
  (`--generate-notes`), so the flattening will not be described in prose
  anywhere except this report.
- **`internal/tuilib` still defaults the program name to "wagyu"** in two
  places (`logo_cmd.go`, `exitbanner.go`) — cosmetic branding leftovers from
  the rename, left alone here because they are pinned by tests and are not
  part of the release surface.
- **Whether `docs/cycles` belongs in the published snapshot at all** is a
  judgement call: it makes the published tree self-consistent (its own docs
  resolve) at the cost of ~7 MB per snapshot.

## 6. Limitations

- The workflows were validated by **parsing** them (YAML → step list) and by
  running every command they invoke locally, but they were **not** executed
  on a GitHub runner.
- The publish script's `--watch` path was exercised only in dry-run mode;
  the tag-push path was not run against the live repository (that would
  publish a release).
- The release archive was rebuilt **locally** to confirm its layout; the
  published `v1.4.4` archive was not downloaded and unpacked for
  comparison.
