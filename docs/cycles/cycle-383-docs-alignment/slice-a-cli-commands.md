# Slice A — CLI command surface

A read-only divergence survey: the official Antigravity CLI docs against the Go
clone in this tree, scoped to the CLI's command surface.

## Method

Every doc in the slice was read in full (16 files, 1,787 lines):
`official-docs/cli/reference.md`, `official-docs/slash-commands.md`, and
`official-docs/cli/commands/{agents,btw,changelog,codesearch,context,credits,diff,effort,permissions,resume,statusline,title,usage,voice}.md`.

Each concrete, checkable claim was traced to the clone's implementation and to
the reference binary's string table (`out/local-macos-1.4.11/strings.txt`),
which is the byte-parity ground truth where doc and binary disagree.

Counts below use two units: *claims* = individual assertions examined;
*findings* = the reportable blocks (matches, divergences, doc-only,
clone-only). A finding may cover several claims.

## Caveats

(a) `go build ./cmd/pigasus` currently fails in-tree at
`internal/ui/views/providerpanel/panel_render.go:42` (`p.searchContent
undefined`) — a pre-existing, unrelated break, so no finding below rests on
running the binary; all rest on source reading plus the reference string table.

(b) `strings.txt` is a concatenated blob with no Unicode glyphs (LC_ALL=C shows
ASCII + `\f` only), so glyph-level claims (`◉`, `⛁`, `└`, `●`) are checked
against clone source only, not against the binary.

(c) Where the docs describe the **Antigravity App** rather than the CLI, the
claim is marked *App-only* and excluded from the CLI verdict.

---

## Match

1. **`/btw` with no argument and no history** → `Usage: /btw <question>` —
   byte-identical to the reference (`strings.txt` carries `Usage: /btw
   <question>`). `internal/cliapp/slash_btw.go:102` (`runBtwSideQuestion`).

2. **`/btw` while one is in flight** → `A /btw question is already in
   progress. Please wait for it to finish.` `internal/cliapp/slash_btw.go:106`.

3. **`/btw` with no argument but with history** reopens the answer card instead
   of printing usage. `internal/cliapp/slash_btw.go:99-101`.

4. **`/btw` is refused in headless mode** — `printSlashRejections["btw"]`,
   `internal/printmode/print_slash.go:458`.

5. **`/title` bare toggles**; `on`/`enable`/`off`/`disable` accepted; any other
   verb → `Usage: /title [on|off]` (byte-identical to the reference string).
   `internal/cliapp/slash_commands_handlers.go:603-613`,
   `internal/ui/tuilib/feedback.go:119`.

6. **`/statusline` bare toggles** the status line on/off.
   `internal/cliapp/slash_commands_handlers.go:492-499`.

7. **`/statusline on|enable|off|disable`** applied explicitly; **`/statusline
   delete|reset`** clears the custom command; **`/statusline help`** prints the
   usage rows; **any other argument** is persisted as the custom rendering
   command. `internal/cliapp/slash_commands_handlers.go:501-534`.

8. **`/usage` and its alias `/quota`** open the same panel; the panel's title
   row is `└ Models & Quota`.
   `internal/ui/views/usagepanel/usagepanel.go:57-59`.

9. **`/usage` refreshes quota on open** (a fresh backend/disk check when the
   panel opens). `internal/cliapp/tui_prompt_submit.go:60-63`
   (`fetchQuotaSummaryCmd`).

10. **`/usage` navigation** — `↑/↓` (or `j/k`), `PgUp/PgDn`, `g`/`Shift+G`,
    `Esc`/`q`. `internal/ui/tuilib/nav.go:29-53` (`ClassifyViewerNavKey`).

11. **`/credits` panel rows** — `Model AI Credits`, `Remaining AI Credits`,
    `Actions`; the actions are `Get More AI Credits` and `See Activity` (all
    three byte-identical to the reference string table).
    `internal/ui/views/creditspanel/panel.go:114-121`.

12. **`/permissions` rule viewer** — allow/deny/ask tabs, `↑/↓` (or `j/k`),
    `A` to add, `E` to edit, `D`/Backspace to delete, `Esc` back to the scope
    picker. `internal/ui/views/permissionspanel/panel_keys.go:104-140`.

13. **`/voice` recording line** — `● Recording 0:07  enter or f5 to insert ·
    esc to discard` (byte-identical to `voice.md:29`).
    `internal/ui/views/voice/banner.go:47-54`.

14. **`F5` starts dictation** from the composer and toggles the recording.
    `internal/cliapp/tui_update_keys.go:104-106`.

15. **`ANTIGRAVITY_MIC`** is honoured as the remote-mic address, and **`agy
    mic-serve --addr <host:port>`** exists with the documented default
    `127.0.0.1:4713`. `internal/cliapp/voice_host.go:50`,
    `internal/cli/subcommand_misc.go:49-67`.

16. **`/voice` is refused in headless mode** (`--print`).
    `internal/printmode/print_slash.go:446-447`.

17. **`/diff` view tabs** are `All Changes`, `Per Turn`, `Commit Tree`
    (reference order), and the empty state is `No uncommitted changes`.
    `internal/ui/views/diff/action/render.go:13-15,77`.

18. **`/codesearch` usage line** — `Usage: /codesearch [-F|--literal]
    <query>`, byte-identical to the reference string.
    `internal/ui/views/codesearchaction/codesearchaction.go:43`.

19. **`/codesearch` aliases** `/cs` and `/search` resolve to the same panel.
    `internal/ui/tuilib/commands.go:64`,
    `internal/cliapp/slash_panels.go:437` (`canonicalPanelCommand`).

20. **`/context` panel header** is `└ Context Usage`; the free-space row is
    present. `internal/ui/views/contextpanel/breakdown.go:15`,
    `internal/ui/views/contextpanel/contextpanel.go:174,191`.

21. **`/agents` panel header** is `Create New Agents`, with the Workspace and
    Global template paths (`<cwd>/.agents/agents/{agent_name}/agent.md`,
    `~/.gemini/config/agents/{agent_name}/agent.md`) and the `Available Agents`
    section. `internal/ui/views/agentspanel/panel_render.go:50-68`.

22. **`/agents` fork notice** — `Create New Agents  [ Switch will fork the
    current conversation on exit ]`.
    `internal/ui/views/agentspanel/panel_render.go:52`.

23. **`/effort` panel title** is `Set Effort`.
    `internal/ui/views/effortpanel/slider.go:317`.

24. **`/changelog` in headless mode** prints the release notes to stdout
    without a model turn. `internal/printmode/print_slash.go:163-164`.

25. **`agy changelog`** exists as a standalone subcommand.
    `internal/cli/parser_specs.go:209-213`.

26. **`/changelog` viewer navigation** — `↑/↓` (or `k/j`), `PgUp/PgDn`,
    `Ctrl+U`/`Ctrl+D`, `g`/`Shift+G`, `Esc`. `internal/ui/tuilib/nav.go:29-53`.

27. **`/plugin` inline verbs** — `list`, `install`, `validate`, `enable`,
    `disable`, `uninstall`, each case-insensitively.
    `internal/cliapp/slash_commands_handlers.go:326-339`.

28. **Headless rejection format** — `/%s is not available in print mode (%s);
    …`. `internal/printmode/print_slash.go:137-138`; the rejection table
    (`internal/printmode/print_slash.go:433-469`) covers `clear, rewind, fork,
    resume, title, rename, diff, copy, open, voice, record, add-dir, tasks,
    artifact, exit, quit, new, context, btw, codesearch, statusline, mcp,
    logout, keybindings, remote-control, feedback` plus `/plugin`'s bespoke
    message (`printSlashPluginRejection`, `print_slash.go:471-472`).

29. **All 20 CLI flags** in `reference.md:120-139` exist, and `--effort`'s
    usage string is byte-identical to the reference's `Reasoning effort for the
    current CLI session (low|medium|high|xhigh|max)`.
    `internal/cli/parser_flags.go:33-88`.

30. **All 33 `settings.json` keys** in `reference.md:162-194` exist with the
    documented names. `internal/core/settings/settings.go:47-84`.

31. **Documented aliases that work** — `/clear`→`/new`, `/exit`→`/quit`,
    `/fork`→`/branch`, `/rewind`→`/undo`, `/config`→`/settings`,
    `/usage`→`/quota`, `/codesearch`→`/cs`,`/search`.
    `internal/ui/tuilib/commands.go:63-92` +
    `internal/cliapp/slash_panels.go:437`.

32. **`Ctrl+A` / `Ctrl+E`** move the insertion cursor to line start / line end
    (bubbles textarea v2.1.1 `LineStart`/`LineEnd` bindings,
    `charm.land/bubbles/v2@v2.1.1/textarea/textarea.go:93-94`); the clone's own
    Select-All path only claims the Super/Meta and `Ctrl+Shift+A` variants
    (`internal/ui/editor/composer/keys.go:34-36`), so plain `Ctrl+A` falls
    through.

33. **`Ctrl+D`** deletes the selection when there is one, exits when the prompt
    is empty, and otherwise forward-deletes — matching both `reference.md:67`
    and `reference.md:89`. `internal/cliapp/tui_update_keys.go:238-251`.

34. **`Ctrl+G`** opens the prompt in `$EDITOR`; **`Ctrl+O`** toggles the
    trajectory view; **`Ctrl+L`** clears the screen; **`Esc`/`Ctrl+C`** are
    global. `internal/cliapp/tui_update_keys.go:42-67,222-236`.

---

## Divergences

### A-1 — `/effort` accepts only 3 of the 5 documented levels — **Closed**

Closed in `3b75838c` / deliverable §7. `/effort` accepts `xhigh` and `max` alongside `low`, `medium`, `high`.

### A-2 — the invalid-level message is not the reference's — **Closed**

Closed in `3b75838c` / deliverable §7. Invalid effort returns reference's own error format `Invalid effort %q (valid: %s)`.

### A-3 — headless `/effort <level>` must be rejected, not applied — **Closed**

Closed in `3b75838c` / deliverable §7. Headless `/effort <level>` exits with code 2.

### A-4 — no "effort isn't adjustable" message — **Closed**

- **Doc claim:** "If you run `/effort` while using a model with a fixed
  reasoning budget, the CLI reports that effort isn't adjustable for the
  current model." (`official-docs/cli/commands/effort.md:25`). The binary
  carries both `Effort isn't adjustable for the current model` and `Effort
  isn't adjustable for %s.`
- **Clone:** Closed in `fix/a4` (deliverable §27). Dynamic model reasoning tiers and `Effort isn't adjustable for the current model` implemented.
- **Verdict:** closed.
- **Severity:** missing-feature.

### A-5 — the effort blurbs are joined with an em dash, not a newline — **REFUTED**

- **Doc claim:** none — this is a **binary-only** claim (no doc renders the
  blurbs).
- **Original claim (refuted):** the reference's blurbs were read as
  `Faster responses, lighter reasoning \n great for simpler tasks` and
  `Deepest reasoning for complex problems \n slower but strongest`, and the
  clone's em dash was called a divergence.
- **Refutation:** the reading came from the **string dump**, and `strings`
  splits a printable run at every non-ASCII byte. The separator is the
  reference's own **em dash** (U+2014, `e2 80 94`) — agy 1.4.12 offset
  `0x3740275`: `Faster responses, lighter reasoning \xe2\x80\x94 great for
  simpler tasks`. The clone's em dash was **byte-identical** to the
  reference's all along.
- **Clone:** `Faster responses, lighter reasoning — great for simpler tasks`
  and `Deepest reasoning for complex problems — slower but strongest` —
  `internal/ui/views/effortpanel/slider.go:66-71`.
- **Verdict:** **not a divergence.** The evidence is recorded in
  `internal/ui/views/effortpanel/slider.go` and its test.
- **Severity:** none — the finding was withdrawn.
- **Minimal fix:** none. The blurbs are unchanged.

> **Correction note.** This entry was **wrong on the first reading** and was
> caught by fix 7a, which went back to the binary's bytes rather than to the
> dump. A string dump is not a byte dump: `strings` breaks a run at every
> non-ASCII byte, so a `
` in a dump is often the *end of a line*, not a
> newline in the string. The other findings in this ledger that lean on a
> dump's line breaks deserve the same second look.

### A-6 — the `/plugins` alias does not exist — **Closed**

Closed in deliverable §10. `/plugin` aliased to `/plugins` and dispatches properly.

### A-7 — the `/resume` aliases `/switch` and `/conversation` are unreachable — **Closed**

Resolved in tranche 88 / `internal/cliapp/slash_alias_test.go:87`. `slashcommands.Canonical` resolves both `/switch` and `/conversation` to `/resume` and dispatches to `slashResume`.

### A-8 — the command is `/teamwork`, not `/teamwork-preview` — **Closed**

Closed in deliverable §10. Catalogue lists `/teamwork-preview` as canonical and `/teamwork` as alias.

### A-9 — the `/context` panel is missing 9 of the documented rows — **Closed**

Closed in deliverable §11. The `/context` panel draws all 13 breakdown rows with documented markers.

### A-10 — the `/context` "Related:" hint is one line, not three — **Closed**

Closed in deliverable §8. The panel renders three dim lines splitting on literal `\n`.

### A-11 — the `/agents` subagent group header is not the reference's — **Closed**

Closed in deliverable §8. The header uses `▸ Subagents (%d running, %d done)`.

### A-12 — `K` (Kill Active Subagent) is advertised but not implemented — **Closed**

Closed in `fix/a1213` (deliverable §20). Pressing `K` terminates active subagent via manager.

### A-13 — inline `A`/`D` tool approvals inside `/agents` are absent — **Closed**

Closed in `fix/a1213` (deliverable §20). Pressing `A` approves and `D` denies pending tool approvals inline from `/agents`.

### A-14 — `/resume` F4 deletes without a confirmation — **Closed**

Closed in `internal/ui/resumepicker/keys.go:128-175` / `picker_test.go:171`. F4 arms `[Delete? (y/n)]` confirmation stage next to title and deletes only on Enter/Y.

### A-15 — the `/resume` "Other" tab is empty and there is no import — **Closed**

- **Doc claim:** Tab switches to the **Other** tab, and Enter raises
  `[Import this conversation? (y/n)]` before cloning the desktop thread
  (`official-docs/cli/commands/resume.md:66-69`). The binary carries
  `[Import this conversation? (%s/%s)]`.
- **Clone:** Closed in `fix/a15` (deliverable §29). Discovers desktop app threads from `~/.gemini/antigravity`, supports full text preview matching, and implements `[Import this conversation? (y/n)]`.
- **Verdict:** closed.
- **Severity:** missing-feature.

### A-16 — `/resume` search matches the title only — **Closed**

Closed in `internal/ui/resumepicker/picker.go:342-361` (`matchesQuery`). Search matches title, conversation ID, and first line of the first prompt.

### A-17 — `/codesearch` file filters have inverted semantics and no `path:` alias — **Closed**

Closed in deliverable §12. `f:`, `file:`, `path:` are include globs; leading `-` denotes exclusion.

### A-18 — `/codesearch` cannot open a result — **Closed**

Closed in deliverable §12. Enter opens result in viewer; Ctrl+G opens external editor.

### A-19 — the `/codesearch` file-viewer comment flow does not exist — **Closed**

Closed in `fix/a19` (deliverable §21). Line comments, 💬 gutter markers, multi-line editor, and `Unsent Comments` screen implemented.

### A-20 — `/diff` `F` does not toggle "changes only ↔ full file" — **Closed**

Closed in deliverable §13. `F` toggles changes-only vs full file; `Ctrl+O` folds unchanged lines.

### A-21 — the `/diff` viewer is missing the reference's other bindings — **Closed**

Closed in deliverable §13. Full file view keys: `n`/`N`, `/`, `c`/`d`, `←`/`→`, and Unsent Comments screen.

### A-22 — `Ctrl+R` (`view.review_artifact`) is not implemented — **Closed**

Closed in deliverable §8. `Ctrl+R` opens the Artifact Review panel.

### A-23 — undo / redo do not exist — **Closed**

Closed in deliverable §8. Help table rows for undo/redo dropped as the editor tier owns undo.

### A-24 — `Alt+J` and `Ctrl+K` have different meanings — **Closed**

Closed in deliverable §8. Help table descriptions aligned with reference bytes.

### A-25 — `/changelog` neither fetches nor caches — **Closed**

Closed in deliverable §8. Changelog parsed once with `sync.Once`.

### A-26 — `/keybindings` launches `$EDITOR` instead of reporting the bindings — **Closed**

- **Doc claim:** "Open the interactive Keyboard Shortcut Editor."
  (`official-docs/cli/reference.md:33`); the F5 remapping hint points at
  `/keybindings` as the place to rebind a key
  (`official-docs/cli/commands/voice.md:46`).
- **Clone:** Closed in `fix/a26` (deliverable §28). `/keybindings` reports active keybindings, and `/keybindings edit` launches the editor.
- **Verdict:** closed.
- **Severity:** behaviour.

### A-27 — `/skills reload` is ignored — **Closed**

- **Doc claim:** "Run `/skills reload` to reload skills from disk."
  (`official-docs/cli/reference.md:47`); the binary carries `Reload skills from
  disk` next to `/skills reload`.
- **Clone:** Closed in `ec5056b6` / deliverable §15. `/skills reload` re-scans skill directories and prints `Reload skills from disk`.
- **Verdict:** closed.
- **Severity:** missing-feature.

### A-28 — `/plugin`'s unknown-subcommand message is not the reference's — **Closed**

Closed in deliverable §10. Reference format string `Unknown subcommand %q.` adopted.

### A-29 — the `/credits` header is the clone's brand — **By-Design**

- **Doc claim:** the panel shows the CLI's identity and credits
  (`official-docs/cli/commands/credits.md:24-28`).
- **Clone:** the header row is `Pigasus CLI  credits`
  (`internal/ui/views/creditspanel/panel.go:114`); the reference's string is
  `Antigravity CLI  ` (two trailing spaces, `strings.txt`). The value comes
  from `internal/core/branding/brand.go` (default `BrandPigasus`).
- **Verdict:** divergence — **by design**: the clone is a rebrand of the
  reference, so this string is expected to differ. Recorded so the byte-parity
  target is not mistaken for a bug.
- **Severity:** exact-string.
- **Minimal fix:** none required for the clone's identity; do not treat
  `Pigasus CLI` as a byte-parity defect.

### A-30 — `/permissions` has no `Ctrl+G` edit alias — **Closed**

Closed in deliverable §8. `Ctrl+G` accepted as alias of `e` in permissions panel.

### A-31 — the panel exit notice echoes the *typed* alias — **Closed**

Closed in deliverable §10. Exit notices echo the canonical command name.

### A-32 — `/statusline` does not open a "Status Bar customization overlay" — **Doc-Wrong**

- **Doc claim:** "Open the Status Bar customization overlay."
  (`official-docs/cli/reference.md:48`).
- **Clone:** implements the argument form the per-command page describes
  (`official-docs/cli/commands/statusline.md:19-40`): bare toggle,
  `on`/`enable`/`off`/`disable`, `delete`/`reset`, `help`, and any other
  argument persisted as the custom rendering command
  (`internal/cliapp/slash_commands_handlers.go:487-535`).
- **Verdict:** **doc-wrong.** The binary contains no `Status Bar` string at
  all, and the dedicated `statusline.md` page describes exactly what the
  clone implements. `reference.md:48` contradicts `statusline.md` and the
  binary.
- **Severity:** doc-wrong.
- **Minimal fix:** correct `reference.md:48` to "Toggle the status line, or
  set a custom rendering command." (a docs-only edit; the clone needs no
  change).

### A-33 — the `/credits` panel sections are named differently — **Doc-Wrong**

- **Doc claim:** the panel shows **Active balance**, **Usage summary** and
  **Quick links** (`official-docs/cli/commands/credits.md:26-28`).
- **Clone:** shows `Model AI Credits`, `Remaining AI Credits` and `Actions`
  (`internal/ui/views/creditspanel/panel.go:114-121`) — byte-identical to the
  binary's `Model AI Credits`, `Remaining AI Credits`, `Get More AI Credits`,
  `See Activity`.
- **Verdict:** **doc-wrong** (a paraphrase of the reference's own wording).
- **Severity:** doc-wrong.
- **Minimal fix:** correct `credits.md:26-28` to the reference's four strings;
  no clone change.

---

## Clone-only

Commands and surfaces the clone implements that the slice's docs never
mention:

1. `/accounts` — multi-account management
   (`internal/ui/tuilib/commands.go:94`).
2. `/pool` — the account-pool routing policy
   (`internal/ui/tuilib/commands.go:95`).
3. `/provider` (+ alias `/providers`) — the custom-provider registry
   (`internal/ui/tuilib/commands.go:96`).
4. `/feature-diff` (+ aliases `/featdiff`, `/diff-agy`) — the parity report
   (`internal/ui/tuilib/commands.go:72`).
5. `/compact` — manual conversation compaction
   (`internal/ui/tuilib/commands.go:98`).
6. `/quote`, `/narration`, `/share` — handlers with no doc page
   (`internal/cliapp/slash_commands.go:155,166,167`).
7. The `provider`, `model`, `proxy`, `accounts` CLI subcommands
   (`internal/cli/parser_specs.go:144,203,260`) — the reference's `--help`
   block lists 12 subcommands; `pigasus` lists 20.
8. The `/plugin validate` verb — the docs name only `install`, `uninstall`,
   `enable`, `disable`, `list`
   (`official-docs/cli/reference.md:41`); the clone adds `validate`
   (`internal/cliapp/slash_commands_handlers.go:331-332`).

These are the documented "superset" of the reference; they are listed so a
later slice does not re-report them as divergences.

---

## Doc-only

1. **`/generative_ui`** — "Builds interactive widgets, such as diagrams,
   charts, and explainers, as HTML artifacts or inline in the conversation.
   **Antigravity App only**."
   (`official-docs/slash-commands.md:43,216,246,273`). The doc marks it
   App-only, so the CLI is *not* required to implement it; the clone has no
   such command. Listed as doc-only, **not** a missing-feature.

   *Ambiguity flagged:* `official-docs/slash-commands.md` is a
   **cross-surface** catalogue (CLI + App + IDE). Every claim in it that is not
   marked App-only was checked against the CLI; the `/teamwork-preview` and
   `/plugins` rows are CLI-side (A-6, A-8), while `/generative_ui` is the only
   row that is explicitly out of the CLI's scope.

---

## Numbers

| Metric | Count |
|---|---|
| Docs read | 16 (1 reference + 1 cross-surface catalogue + 14 per-command pages; 1,787 lines) |
| Claims checked | 278 (individual assertions: 135 in `reference.md`, 8 in `slash-commands.md`, 135 across the 14 command pages) |
| Matches | 34 findings |
| Divergences | 33 findings (31 clone-side, 2 `doc-wrong`) — **A-5 withdrawn**, so 32 stand |
| Doc-only | 1 finding |
| Clone-only | 8 findings |

Severity split of the 33 divergences: **exact-string** 7 (A-2, A-5, A-10,
A-11, A-28, A-29, A-31) · **behaviour** 8 (A-1, A-3, A-8, A-14, A-16, A-17,
A-20, A-24) · **missing-feature** 16 (A-4, A-6, A-7, A-9, A-12, A-13, A-15,
A-18, A-19, A-21, A-22, A-23, A-25, A-26, A-27, A-30) · **doc-wrong** 2 (A-32,
A-33). **A-5 was withdrawn** by fix 7a (the separator is the reference's own
em dash, not a newline), so the split is 6 exact-string and 32 divergences
that stand.

Highest-value closings, in order: **A-1+A-2** (one-line widening of
`ParseEffortWord` plus a message change — the clone's catalogue already has
the five levels, so only the gate is wrong), **A-3** (a repository already
documented as pre-existing), **A-6+A-7+A-8** (three alias/name entries in the
catalogue plus two `slashHandlers` keys), **A-31** (one line: canonicalise the
echoed command name), **A-23** (drop the two `/help` rows, or add the undo
stack).

---

## limitations

- The slice read **16 docs** in full — `official-docs/cli/reference.md`,
  `official-docs/slash-commands.md` and the 14 files under
  `official-docs/cli/commands/` — totalling 1,787 lines. Every concrete claim
  in them was traced to a clone file, a symbol and a line.
- The build was **broken in-tree** at
  `internal/ui/views/providerpanel/panel_render.go:42` (`p.searchContent
  undefined`), so **no finding rests on running the binary**. Every verdict
  comes from source reading plus the reference string table
  (`out/local-macos-1.4.11/strings.txt`). The break is pre-existing and
  unrelated to this slice; it was not fixed, under the read-only mandate.
- `strings.txt` carries **no Unicode glyphs** (LC_ALL=C shows only ASCII and
  `\f`), so glyph-level claims (`◉`, `⛁`, `└`, `●`, `▸`, `⊠`) were checked
  against **clone source only**, not against the binary. Where a divergence
  rests on a glyph, it is stated as a clone-side observation.
- Claims that describe the **Antigravity App** rather than the CLI were
  **excluded** from the CLI verdict — `/generative_ui` is the only such row in
  the slice, and it is listed under Doc-only.
- Two findings are explicitly **ambiguous** and are flagged as such in their
  block: A-26 (`/keybindings` — the doc's "interactive Keyboard Shortcut
  Editor" wording is not supported by the binary's strings either, so the
  fix direction needs a live capture) and A-29 (the `/credits` header
  difference is the clone's own brand, recorded so it is not mistaken for a
  byte-parity defect).
- A-3 is anchored on the reference binary and on the repository's own
  first-hand captures (`docs/cycles/model-argument-completion/captures.md`),
  not on a doc line alone; it was still a divergence from the clone when the
  survey was written, and fix 7a closed it.
- **A-5 was withdrawn.** It was read off the **string dump**, and `strings`
  splits a printable run at every non-ASCII byte — the separator is the
  reference's own em dash (`e2 80 94`), not a newline. The clone's bytes were
  identical from the start. Fix 7a caught it by going back to the binary's
  bytes; the entry above is corrected in place.
- This file is a **survey**. Nothing in it has been implemented, and no other
  file was edited — except the **A-5 correction** above, which fix 7a
  required and which is recorded in the cycle's report.
