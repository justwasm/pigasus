# Cycle 102 — TUI slash commands: recovered content instead of stubs

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** the most basic remaining gap after cycle 101 — the interactive TUI still
answered its slash commands with cycle-77 placeholder text
(`Settings panel (cycle 77: stub)`, `Skills (cycle 77: stub)`, …), and `/new`
was not routed at all.

---

## 1. What was wrong

`cmd/antigravity/slash_commands.go` (cycle 77) routed ~35 commands, but every
handler that needed *data* printed a placeholder string. Two concrete defects:

* **`/new` was never in the router.** The reference lists it, but the clone fell
  through to `Unknown command: /new`.
* **Content commands printed nothing real.** `/config`, `/skills`,
  `/permissions`, `/changelog`, `/usage`, `/credits` all rendered invented text
  even though the *same* command has a recovered, byte-exact print-mode handler
  (`print_slash.go`, cycles 99/101) that emits the real data.

## 2. What the reference shows 【一手 content / 推测 layout】

The reference renders these commands as **interactive panels**. The panel layout
cannot be captured: the reference account's quota is exhausted
(`⚠ Individual quota reached … Resets in ~93h`), so no TUI transcript with a
slash command can be obtained. (Same blocker as the tool-step layout in cycle
99.)

What *is* recoverable is the **content**: `agy -p /<cmd>` emits the same data the
panel is built from, and those strings were captured in cycles 99–101.

So the reconstruction is: render the print-mode content, laid out as columns.

| Command | Content source | Notes |
|---|---|---|
| `/model` | `printSlashModels(flags)` | `<id>\t<label>`, e.g. `gemini-3.8-flash-high  Gemini 3.8 Flash (High)` |
| `/effort` | `printSlashEffort(flags)` | e.g. `high` |
| `/config` | `printSlashConfig()` | 35 keys × value, read from `settings.json` |
| `/permissions` | `printSlashPermissions()` | `scope\tdecision\trule` rows |
| `/skills` | `printSlashSkills()` | the recovered skill roots + frontmatter descriptions |
| `/changelog` | `printSlashChangelog` | the release-notes blob |
| `/usage` | recovered failure text | see §4 |
| `/credits` | recovered failure text | `error: /credits failed: retrieving credits: no credits info found` |
| `/hooks`, `/agents` | nothing | nothing is configured, so the reference prints nothing |

`/model` and `/effort` read their values from the parsed flag set, which the TUI
did not previously have — a `flags *flag.FlagSet` field was added to
`v135RunMode` and populated in `runLiveTUI`.

## 3. `/new`

`/new` now starts a fresh conversation exactly like `/clear`: it drops
`m.response`, `m.session`, `m.blocks`, `m.liveSteps` and `m.activeTool`. Cycle 77
never routed it, so it reached the transcript as `Unknown command: /new`.

## 4. `/usage` and `/credits`

Both are **live service calls** in the reference (`/usage` runs an eligibility
check and then a quota fetch). The clone cannot reach that service, so it renders
the recovered failure vocabulary rather than the invented text cycle 99 used:

```
error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account
error: /credits failed: retrieving credits: no credits info found
```

Every fragment is 【strings】-verified present in the 1.3.6 binary
(`/%s failed: %w`, `retrieving quota summary: %w`,
`failed to retrieve user quota summary: %w`,
`no quota summary is available for this account`, `retrieving credits: %w`,
`no credits info found`). The **nesting order is 【推测】**.

The previous text (`retrieving usage: … no quota info found`) was invented —
`grep -Fc` finds **0** occurrences of either fragment in the binary.

## 5. Column layout

`printSlashConfig` and friends emit **tab-separated** rows. A raw `\t` collapses
in the TUI renderer (verified over a pty: `allowNonWorkspaceAccessfalse`), so
`slashContent` measures each column with `utf8.RuneCountInString` and pads with
spaces before appending the rows to the transcript.

The column widths are a presentation choice; the cell values are the recovered
ones, unmodified.

## 6. Files changed

| File | Change |
|---|---|
| `cmd/antigravity/slash_commands.go` | new `flags`-driven handlers for the 10 commands above; `slashContent` column formatter; `/new` route; header comment rewritten |
| `cmd/antigravity/tui_v135.go` | `v135RunMode.flags *flag.FlagSet`, set in `runLiveTUI` |
| `cmd/antigravity/slash_content_test.go` | **new** — 5 tests pinning the rendered content, the `/new` reset, and the absence of `stub` text |

## 7. Verification

* `go build ./cmd/antigravity` — clean; `gofmt` clean on both changed files.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `harness136.sh` — **86/87** unchanged (one diff: the documented `/plugin`
  pre-auth divergence).
* `harness137.sh` — **23/25** unchanged (both diffs documented in cycle 101).
* pty probe (`tui_probe.py`, 40×120) — `/config` and `/skills` render the real
  data as aligned columns; the secret-file check still passes.

> Note: an intermediate harness136 run reported 82/87 because the **reference**
> timed out at 25 s on several cases (quota/network). A re-run returned 86/87.
> The reference is currently flaky under its exhausted quota; treat single-run
> swings of ±4 on the `124/…` rows as harness noise, not regressions.

## 8. Divergences and limitations

* **Panel layout is 【推测】.** The reference opens an interactive panel (cursor,
  selection, key handling) for `/config`, `/model`, `/skills`, `/permissions`,
  `/effort`, `/changelog`; we append read-only lines to the transcript. The
  *content* matches the recovered print-mode output; the *chrome* does not.
* **~20 commands are still cycle-77 stubs** — `/diff`, `/copy`, `/logout`,
  `/add-dir`, `/mcp`, `/tasks`, `/keybindings`, `/statusline`, `/voice`,
  `/remote-control`, `/resume`, `/rewind`, `/fork`, `/rename`, `/title`,
  `/feedback`, `/artifact`, `/btw`, `/codesearch`, `/plugin`. They remain
  one-line placeholders. `/add-dir` in particular needs real persistence:
  inspecting `~/.gemini/antigravity-cli` shows the reference records the session
  working directory in `settings.json`'s `trustedWorkspaces` array (the pty
  probe's temp dir appears there), so a faithful `/add-dir` must append to that
  key — deferred to a later cycle rather than shipping a message that claims an
  add that did not happen (the exact bug the 1.3.3 changelog fixed).
* **`/usage` cannot succeed.** It is a live quota call; see §4.
* **`/hooks` and `/agents` render nothing** because nothing is configured in this
  build. The populated-panel rendering is untested.
* **No TUI transcript comparison is possible** while the reference quota is
  exhausted, so the panel behaviour of every command above is unverified against
  a live reference.
