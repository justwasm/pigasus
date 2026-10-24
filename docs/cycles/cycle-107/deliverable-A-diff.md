# Cycle 107 — `/diff` runs the real working-copy diff

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the first cycle-77 stub replaced with a genuine feature rather than a
recovered string. `/diff` previously printed
`Pending diff: working tree changes (cycle 77: stub)`; it now runs the
workspace's real diff.

---

## 1. Recovered evidence 【一手】

| String | Role |
|---|---|
| `No uncommitted changes` | empty working copy |
| `no workspace directory` | no workspace to diff |
| `git diff: %w`, `hg diff: %w`, `jj diff: %w` | per-VCS error wrapper |
| `  [%d-%d of %d]  (end of diff)` | viewer footer |
| `Loading diff` | transient progress label |
| `No file changes in conversation`, `No commit history available` | the other two `/diff` views |
| `VCS_TYPE_GIT`, `VCS_TYPE_JJ` (+ internal `VCS_TYPE_PIPER` / `VCS_TYPE_FIG`) | the VCS enum |
| 1.3.4 changelog: *“…repositories configured to always color output…”*; *“agent commands now set `JJ_PAGER=cat` alongside `PAGER` and `GIT_PAGER`”* | why colour and pagers must be disabled |

The command description is *“View uncommitted changes and per-turn diffs”* — the
clone implements the first view.

## 2. Implementation

* `detectVCS(dir)` walks up from the workspace looking for `.git`, `.hg` or
  `.jj`, mirroring the reference's Git / Mercurial / Jujutsu support.
* `vcsDiff(vcs, dir)` runs the working-copy diff with pagers and colour off:

  | VCS | Command | Extra env |
  |---|---|---|
  | git | `git --no-pager diff --no-color` | `PAGER=cat`, `GIT_PAGER=cat`, `NO_COLOR=1` |
  | hg | `hg diff` | `HGPLAIN=1`, `PAGER=cat` |
  | jj | `jj --color=never diff` | `JJ_PAGER=cat`, `NO_COLOR=1` |

* `diffFeedback(workspace)` renders the result: the recovered empty-state line,
  the per-VCS error wrapper (`git diff: <err>`), or the diff body followed by the
  recovered footer `  [1-N of N]  (end of diff)`.

The **presentation** is 【推测】. The reference renders the diff in an
interactive, syntax-highlighted viewer (1.3.5 changelog: *“Added syntax
highlighting to the `/diff` file view”*); the clone appends plain lines to the
transcript. The diff body itself and every message above are not invented.

`Loading diff` is **not** emitted: it is a transient progress label shown
*before* the content, so reproducing it as a permanent header would be wrong.

## 3. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `TestDiffFeedback_Git` builds a real repository, checks the clean case, a
  modified tracked file (`-one` / `+two`, footer present) and nested-directory
  detection; `TestDiffFeedback_Edges` covers `""` and a directory with no VCS.
* Print-mode `/diff` is untouched and still byte-identical to the reference:

  ```
  error: /diff is not available in print mode (it opens the interactive diff
  viewer); pass --disable-slash-commands to send /diff to the model as literal text
  ```
  (rc 2, both binaries).
* Live pty probe in a scratch repository with one modified file:

  ```
  > /diff
  diff --git a/tracked.txt b/tracked.txt
  index 5626abf..f719efd 100644
  --- a/tracked.txt
  +++ b/tracked.txt
  @@ -1 +1 @@
  -one
  +two
    [1-7 of 7]  (end of diff)
  ```

> **Reference flakiness is now severe.** Under its exhausted quota the reference
> timed out (`124`) on `slash_resume`, `slash_open`, `slash_codesearch` and
> `slash_plugin` in the same harness136 run, and on five harness137 rows
> (`usage_slash`, `model_effort_only`, `add_dir_flag`, …). Every one of those is
> `124/…` — a *reference* timeout against our correct rc — and all four
> harness136 rows show `=` for stdout and stderr, i.e. they match once the
> missing output is normalised. Run-to-run scores swung from 87/87 down to
> 83/87 and 18/25 purely on reference timeouts. Treat `124/…` rows as harness
> noise.

## 4. Divergences and limitations

* **The viewer is not interactive and not syntax-highlighted.** No file list, no
  `next/prev diff`, no comment editing — those need `KeyDiffView*` UI the clone
  does not have. Only the working-copy view is implemented.
* **`/diff` ignores its argument.** The reference distinguishes the
  working-copy, per-turn and commit views (`No file changes in conversation`,
  `No commit history available`, `only works in the working-copy diff, not
  per-turn or commit diffs`, `specific revision diff not supported for %s`); the
  clone always shows the working copy.
* **The footer is 【推测】.** `[1-N of N]` is the pager's viewport indicator; in a
  non-paged transcript it always spans the whole diff.
* **Mercurial and Jujutsu are untested.** Neither is installed on this machine;
  the invocations follow the recovered `hg diff: %w` / `jj diff: %w` wrappers and
  the changelog's pager note, but only the Git path has been exercised.
* **Still cycle-77 stubs (~9):** `/keybindings`, `/voice`, `/resume`, `/rewind`,
  `/fork`, `/feedback`, `/artifact`, `/btw`, `/codesearch`, `/open`, `/context`,
  `/plugin`. `/context` (`Token usage by category`, `Agent responses`,
  `System overhead`, `System messages`, `Knowledge files`,
  `%s Free space: %s (%.1f%%)`) and `/codesearch`
  (`Usage: /codesearch [-F|--literal] <query>`, `Searching for %q...`,
  `CodeSearch function is not configured`) are the next cheapest.
