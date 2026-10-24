# Cycle 111 — recovered tool-step count vocabulary and the `/codesearch` header

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the changelog is the roadmap. Entry 1.3.3 records a counting bug that
names the exact strings to use, and the 1.3.6 string table contains every one of
them as a literal. The clone was rendering `⏺ 2 steps`; the reference renders
`⏺ 1 file, 1 command`.

---

## 1. Recovered evidence 【一手】

> Fixed counts in tool steps and the `/codesearch` panel, which now read
> "Found 1 result", "1 match", "1 file", and "Wrote 1 line" instead of
> "1 results", and the file-creation step reporting one line too many.

Every singular/plural pair below is a **separate literal** in the 1.3.6 string
table (verified with `strings`), which is why the singular forms matter — the
reference does not build them by trimming an `s`:

| Category | Singular | Plural |
|---|---|---|
| file | `1 file` | `%d files` |
| search | `1 search` | `%d searches` |
| command | `1 command` | `%d commands` |
| tool | `1 tool` | `%d tools` |
| task | `1 task` | `%d tasks` |
| cron | `1 cron` | `%d crons` |
| page | `1 page` | `%d pages` |
| resource | `1 resource` | `%d resources` |
| question | `1 question` | `%d questions` |
| timer | `1 timer` | `%d timers` |
| image | `1 image` | `%d images` |
| step | — | `%d steps` |
| result | `1 result` | `%d results` |

`Found %d results` is also a literal, and `1 result` is a literal, so the panel
header is `Found ` plus the count — matching the changelog's `Found 1 result`.

Related literals that confirm the same family: `Removed %d line%s`,
`Wrote %d lines` / `Wrote 1 line`, `%d lines` / `1 line`, `1 permission`,
`%d bytes`, `%d failed`.

## 2. Implementation

`cmd/antigravity/tui_tools.go`:

* `categoryWords` — the table above.
* `toolCategories` — tool name → category. **【推测】**: the reference derives
  the category from its own step model, so the mapping is reconstructed
  (`run_command`/`command_status`/`send_command_input` → command,
  `view_file`/`write_to_file`/`replace_file_content`/`multi_replace_file_content`/
  `sed_file`/`list_dir`/`notebook_edit` → file, `grep_search`/`find_by_name`/
  `codebase_search`/`search_web` → search, `read_url_content`/`view_content_chunk`
  → page, `manage_task`/`send_message`/`wait` → task, `schedule` → cron,
  `list_resources`/`read_resource` → resource, `ask_question` → question,
  `generate_image` → image, everything else → tool).
* `countLabel(n, word)` — `1 file` when `n == 1`, `%d files` otherwise.
* `groupLabel(steps)` — one category reads `3 files`; a mixed group lists every
  non-empty category in `categoryOrder` joined by `, `, e.g.
  `1 file, 2 searches`. The join is 【推测】; the counts are not.
* `toolBlockLines` now emits `⏺ <groupLabel>  (<hint>)` instead of
  `⏺ <n> steps`. `pluralSteps` was deleted.

`cmd/antigravity/slash_actions.go`:

* `codeSearchFeedback` now prefixes the recovered `Found <n> result(s)` header
  (counting withheld matches too, so the header and the `  | %d more` tail
  agree).

## 3. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* New tests: `TestGroupLabel` (10 cases: single-category singular and plural,
  each recovered category, unknown tool → `tool`, mixed order, repeated buckets),
  `TestCountLabel` (the singular/plural rule, including `0 files`),
  `TestToolCategory` (every mapped category has a recovered word pair).
  `TestTUITools_GroupHeaderForMultipleSteps` now pins `1 file, 1 command` in both
  the collapsed and expanded states.
* `/codesearch` tests updated for the header:
  `Found 1 result\na.go:3:6: func needle() {}`.
* Live pty probe (`/codesearch`):

  ```
  > /codesearch two
  Found 1 result
  tracked.txt:1:1: two
  > /codesearch -F n.edle
  No results
  ```

* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented cycle-101 diffs).

> **Reference noise observed and dismissed.** One `harness137` run reported
> `skills_slash 0/0 D =` — the reference listed three builtin skills
> (`automation`, `migrate-workflows`, `run-workflow`) that our clone's empirical
> builtin allowlist omits. A direct
> `agy -p /skills` immediately afterwards produced **68 lines byte-identical to
> the clone's** (empty `diff`), and the next `harness137` run was back to 23/25.
> The reference's builtin-skill listing is therefore state-dependent: it lists
> 2 or 5 of the 9 bundled skills depending on the extraction state of
> `~/.gemini/antigravity-cli/builtin/skills`. The clone reproduces the stable
> (2-skill) state. Not a regression, and not caused by this cycle's changes.

## 4. Divergences and limitations

* **The tool-name → category mapping is 【推测】.** Only the vocabulary is
  recovered. A tool the reference counts as a `file` but the clone counts as a
  `tool` would show a different header.
* **The mixed-group join (`, `) and its order are 【推测】.** No capture of a
  mixed group exists (the reference's TUI cannot be driven — quota).
* **A single step still renders its tool title**, not `1 file`, matching the
  changelog's requirement that a collapsed group keep the tool name visible. The
  recovered singular forms therefore appear in the clone when a *bucket* inside a
  mixed group holds exactly one step.
* **`1 step` is not a literal in the string table**, so a one-step group cannot
  use it; `categoryWords["step"]` singular is never rendered by `groupLabel`
  (which is only called for groups of two or more) but is kept for
  completeness and pinned by `TestCountLabel`.
* **`1 match` (the picker's filter count) is unused** — the clone has no
  interactive `/codesearch` picker.
* **`Found %d results` is used as a plain header.** The reference shows it in a
  picker panel; the clone prints it as the first transcript line. The header
  text is recovered, its placement is 【推测】.
* **Still cycle-77 stubs (~8):** `/keybindings`, `/voice`, `/resume`, `/rewind`,
  `/fork`, `/feedback`, `/artifact`, `/btw`, `/context`, `/plugin`.
