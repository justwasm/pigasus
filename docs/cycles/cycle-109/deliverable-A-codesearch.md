# Cycle 109 — `/codesearch` runs a real workspace search

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** a third cycle-77 stub replaced with a genuine feature. `/codesearch`
previously printed `Code search: <arg> (cycle 77: stub)`; it now searches the
workspace.

---

## 1. Recovered evidence 【一手】

| String | Role |
|---|---|
| `Usage: /codesearch [-F\|--literal] <query>` | no query, or a bad flag |
| `No results` | no match |
| `  \| %d more` | truncation tail |
| `Searching for %q...` | transient progress label |
| `CodeSearch function is not configured` | the language-server RPC is nil |
| `+ Match all (no filter)`, `Keep typing to filter files` | picker affordances |

The description is *“Search code in the workspace (usage: /codesearch
<query>)”*. The `-F` / `--literal` flag follows ripgrep's convention — the
default is a pattern, `-F` selects a fixed string — which the usage string makes
explicit.

## 2. Implementation

* `codeSearchMatcher(query, literal)` compiles the pattern; `literal` wraps the
  query in `regexp.QuoteMeta`.
* `searchWorkspace(dir, re)` walks the workspace and returns
  `relpath:line:col: text` for the first `codeSearchMaxHits` (200) matching
  lines, plus a count of further matches that were withheld for the truncation
  tail. It skips `.git`, `.hg`, `.jj`, `node_modules`, `.venv` and `vendor`,
  ignores files over 1 MiB, and treats a file as binary when its first 8 KiB
  contain a NUL. Unreadable entries are skipped rather than failing the search.
* `codeSearchFeedback(args, workspace)` parses the flags (`-F` / `--literal`,
  with `--` ending flag parsing), rejects anything else with the recovered usage
  line, and renders the hits followed by `  | %d more` when truncated.

The **result line format** is 【推测】. The reference searches through the
language server's `SearchCode` RPC and presents the hits in an interactive
picker; the clone has no language server, so it walks the filesystem itself and
appends plain `path:line:col: text` lines to the transcript. The messages, the
flag semantics and the empty case are not invented.

`Searching for %q...` is **not** emitted — like `/diff`'s `Loading diff`, it is
a transient progress label shown *before* the content, so reproducing it as a
permanent header would be wrong.

## 3. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* New tests: `TestCodeSearchFeedback` (usage for empty/flags-only/bad-flag,
  pattern and literal matching, `--literal`, `--` terminator, `.git` exclusion,
  empty workspace, invalid pattern), `TestCodeSearch_Truncation` (exactly 200
  rendered hits and a `  | 7 more` tail), `TestCodeSearch_BinarySkipped`, and
  `TestSlashCodesearch_Router`.
* Live pty probe in the cycle-107 scratch repository:

  ```
  > /codesearch two
  tracked.txt:1:1: two
  > /codesearch -F n.edle
  No results
  > /codesearch
  Usage: /codesearch [-F|--literal] <query>
  ```

* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff; the one
  remaining row is a `124/2` reference timeout with matching stdout).
* `harness137.sh` — **23/25**, both diffs the documented cycle-101 pair
  (`usage_slash`, `disable_slash`).

## 4. Divergences and limitations

* **The result format is 【推测】** — see §2. No picker, no filtering, no
  per-file grouping.
* **`CodeSearch function is not configured` is never emitted.** The reference
  prints it when its language-server `SearchCode` hook is nil; the clone always
  searches locally, so that state does not exist.
* **No `.gitignore` handling.** Only a fixed skip list is honoured, so ignored
  files (build output, caches) still appear. `rg` semantics would need a
  `.gitignore` reader.
* **Only the first match per line is reported.** Multi-match columns are not
  listed.
* **Binary detection is a heuristic** (NUL in the first 8 KiB, 1 MiB size cap).
* **`/codesearch` is still rejected in print mode**, byte-identically to the
  reference (harness136 `slash_codesearch`), because the reference treats it as
  an interactive-only command.
* **Still cycle-77 stubs (~8):** `/keybindings`, `/voice`, `/resume`, `/rewind`,
  `/fork`, `/feedback`, `/artifact`, `/btw`, `/context`, `/plugin`. `/context`
  (`Token usage by category`, `Agent responses`, `System overhead`,
  `System messages`, `Knowledge files`, `%s Free space: %s (%.1f%%)`) and
  `/keybindings` (`Set custom keybindings`, `No keybindings found.`,
  `keybindings.json`, sharing cycle 108's editor machinery) are the next
  cheapest.
