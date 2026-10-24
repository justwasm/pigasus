# Cycle 121 — `cmd/antigravity` → `cmd/wagyu`

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** user request — *"顺便考虑把 `./cmd/antigravity` 重命名为 `./cmd/wagyu`
并更新文档"*. The clone's entry point shared its name with the binary it studies, so
`go build ./cmd/antigravity` produced a command called `antigravity` that is *not*
the reference — and the reference's own argv0-basename behaviour means the clone
prints `antigravity` in every usage preamble while behaving differently.

---

## 1. What moved 【一手】

```
git mv cmd/antigravity cmd/wagyu
```

A directory move only. The package is `main` and **nothing imports it** — the
clone's entry point is a leaf, so no import path changed and `go.mod` is
untouched (module `github.com/chadgpt/openagy`).

`cmd/wagyu/` now holds 30 files (29 `.go`). The command is built with:

```bash
go build -o /tmp/wagyu ./cmd/wagyu
go test -count=1 ./cmd/... ./internal/...
```

## 2. References updated 【一手】

`cmd/antigravity` appeared in 15 `.go` files, all comments or test-run
instructions — the paths a reader is told to look at, which is a present-tense
fact and not history:

```
cmd/wagyu/main.go              internal/cli/{parser.go,parser_test.go,dispatch.go}
cmd/wagyu/tui_viewport_test.go internal/cli/{jetski.go,changelog_data.go,mode_warning_test.go}
internal/auth/oauth.go         internal/cortex/lifecycle.go
internal/backend/{lifecycle.go,mock.go}
internal/cascade/real_lifecycle.go
internal/onboarding/wizard.go  internal/tui_v135/picker.go
```

The only `cmd/antigravity` left in Go source is the deliberate one in the new
rename note at the top of `cmd/wagyu/main.go`.

Non-Go references updated:

| File | Change |
|---|---|
| `.github/workflows/test.yml` | builds `./cmd/wagyu`, tests `./cmd/... ./internal/...` |
| `README.md` | build block, mermaid label, layout tree, the `go build` row |
| `AGENTS.md` | the repository table now lists the clone and records the rename |

## 3. What was deliberately *not* rewritten 【一手】

The rename lands in cycle 121, so everything written before it refers to
`cmd/antigravity` **as a fact about the time it was written**:

* `docs/cycles/cycle-*/` — 53 cycle reports, including cycle 120's;
* `re-docs/`, `STATUS.md`, `cycle-40-*-deliverable.md`, the root `cycle-41/42`
  deliverables;
* `.mavis/plans/`, `.antigravity/plans/` — planning records;
* `CHANGELOG-recovered.md`, `README-recovered.md` — artifacts recovered *from the
  reference*, i.e. evidence, not project documentation.

Rewriting them would falsify the record. Both `AGENTS.md` and `README.md` carry a
one-line note pointing at the rename instead.

## 4. The CI workflow was already broken 【一手】

`.github/workflows/test.yml` ran every step with `working-directory: go-recovered`
and `cache-dependency-path: go-recovered/go.sum`. `go-recovered/` contains exactly
one untracked directory (`c2go-1.3.3`) and **zero tracked files**, so the workflow
could never have built anything; it was dead configuration before this cycle.

It now builds and tests the real module at the repository root. The test step is
`./cmd/... ./internal/...` rather than `./...`, because `./...` also sweeps in
`scratch/`, which has never compiled — the same surface `AGENTS.md` documents.

**Its smoke steps were invalid too.** They ran `--services` and
`--tool status --tool-args '{}'`, and neither flag is registered:

```
$ wagyu --services
flags provided but not defined: -services
Usage of agy: …
```

`--tools`, `--services`, `--tool` were dropped from the parse-parity surface in a
later cycle, so the handlers that still read them in `cmd/wagyu/main.go`
(lines 218, 224, 244) are unreachable, and the doc comment at the top of that file
("`--tools`, `--services`, `--oauth`, `--tui`: print static catalog snapshots") plus
`README.md`'s `--tool` sample block describe a binary that no longer exists. The
smoke steps now run `--version` and `--help`, which do work. Cleaning up the dead
handlers and the stale prose is backlog, not part of a rename.

## 5. What the rename does **not** change 【一手】

The clone derives its program name from `filepath.Base(os.Args[0])`
(`cmd/wagyu/main.go`, `print_format.go:223`, `slash_actions.go:196`), deliberately
mirroring the reference, which does the same (verified by renaming the reference
binary). Building to `/tmp/wagyu` therefore changes what the usage preamble says,
but the parity harness builds to `/tmp/agycmp/our/agy` and is unaffected:

| harness | before | after |
|---|---|---|
| `harness137.sh` | 23/25 | **23/25** (same two known diffs: `/usage`, `disable_slash`) |

`go vet` is clean, the full suite (`go test ./cmd/... ./internal/...`) passes, and
`gofmt -l` reports the same set as before the rename — verified by diffing against
a `git worktree` of the pre-rename HEAD: **50 unclean files under `internal/` at
HEAD, 50 now, no new entries**, plus the two known `cmd/wagyu/` files
(`picker_view.go`, `tui_v135_test.go`).

## 6. 硬约束 / limitations

* **The clone's own storage is still unwritten.** This cycle is a rename; cycle
  120's read-only limitation is unchanged.
* **The gofmt-unclean set is 50 files, not the handful the earlier notes listed.**
  They are pre-existing and left alone — reformatting them wholesale would bury
  real diffs. The count is now recorded in `AGENTS.md`.
* **Historical reports keep the old name.** A reader grepping for
  `cmd/antigravity` will still find ~40 hits under `docs/cycles/`, `re-docs/` and
  the root deliverables. That is intentional; the `AGENTS.md` note is the index.
* **The CI workflow has not been run.** It is a configuration fix verified only by
  reading — GitHub Actions is not reachable from here, and the two smoke steps
  (`--version`, `--help`) were checked locally.
* **`bin/antigravity`** (a local build artifact) and the `antigravity-tmux`
  helper directory are gitignored / unrelated and were not touched.

## 7. Backlog

Unchanged from cycle 120, plus:

* Remove the dead `--tools` / `--services` / `--tool` handlers in
  `cmd/wagyu/main.go` (218, 224, 244) and the stale prose that advertises them
  (`cmd/wagyu/main.go`'s doc comment, `README.md`'s `--tool` sample block).
* Reconcile the 50 pre-existing `gofmt`-unclean files in a dedicated cycle if the
  project wants a clean `gofmt -l`.
* Write the conversation store (append `history.jsonl`, update `cache/*.json`,
  create `conversations/<uuid>.db`).
* Decode tool results so `ctrl+o` shows output for loaded steps.
* `/resume` for real; seed the TUI up-arrow history from `history.jsonl`.
* Refresh `out/local-macos/` evidence to 1.3.6 (user-deferred).
