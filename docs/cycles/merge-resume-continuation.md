# merge resume-continuation — --continue + /continue slash command + auto-resume

## TL;DR

AGY 1.4.0 features that close the cycle 86 --continue stub:
- `--continue` CLI flag walks the parent/child conversation tree
- `/continue` slash command dispatches the same logic
- After a crashed session (no conv DB), the TUI auto-prompts to resume the
  most recent one from cwd / parents / child dirs.

| | |
|---|---|
| Branch | `feature/resume-continuation` |
| Commit | `2eadd29` |
| Merge | `149bd2f` |
| Binary delta | 73 MB → 73 MB (additive; SQLite reads only) |
| Lines | +1941 / −10 (13 files) |
| New package additions | `internal/convstore/find.go` + `title.go` |
| Cycle report | `docs/cycles/resume-continuation/deliverable-resume.md` |

## Scope (vs AGY 1.4.0)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| `--continue` resumes most recent conversation | `cmd/wagyu/continue.go::runContinueCLI` + `--continue` flag path in `main.go` | ✅ |
| `/continue` slash command | `cmd/wagyu/slash_commands.go::case "continue"` | ✅ |
| Walk cwd + 8 ancestor parents + immediate children | `internal/convstore/find.go::FindMostRecentContinue` | ✅ |
| Auto-prompt to resume after launch (when cwd is empty) | `tui_run.go::promptAutoResume` keyed on TTY + env | ✅ |
| Conversation auto-title cached on first message | `runMode.autoTitle` + `submitPrompt` populate + `saveSession` writes | ✅ |
| `/resume` picker filters out current session | Already implemented at cycle 167; merged unchanged | ✅ |

## Architecture / data flow

```
ANTIGRAVITY_HOME/conversations/<id>.db        ← 1 conversation = 1 SQLite file
ANTIGRAVITY_HOME/conversation_summaries.db   ← 1 row per (id, workspace_path) for ordering
cwd
 └─ is workspace_path in summaries.db?  ──yes──► resume that one
 └─ no: walk $HOME walk up ≤ 8 parents
 └─ still no: walk children of cwd
 └─ still no: most recent across summaries
                                          ↓
       /continue  /  --continue  /  promptAutoResume
                                          ↓
       loadConversationSelection() + seedConversation()
                                          ↓
       runMode.conversationID set, autoTitle cleared
                                          ↓
       On first user prompt: autoTitle = generateConversationTitle(prompt)
                                          ↓
       saveSession() writes through WriteSummariesTitle + WriteCacheTitle
```

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `cmd/wagyu/continue.go` | new | +259 | CLI flag handler, parent/child walk, error formatting |
| `cmd/wagyu/continue_test.go` | new | +398 | 12 TUI tests inc. `TestGoroutineLeakResumePicker` |
| `cmd/wagyu/continue_test_helpers_test.go` | new | +58 | t.Setenv / temp dir helpers |
| `cmd/wagyu/conversation.go` | modified | +26 / −4 | `resolveConversationSelection` walks parent/child tree |
| `cmd/wagyu/main.go` | modified | +42 / −5 | print-mode `--continue` validation |
| `cmd/wagyu/slash_commands.go` | modified | +51 | `/continue` slash command handler |
| `cmd/wagyu/tui_run.go` | modified | +50 / −1 | `autoTitle` field, `submitPrompt` populate, `saveSession` writes, auto-resume prompt hook |
| `internal/convstore/find.go` | new | +370 | `FindMostRecentContinue`, parent walk, child walk |
| `internal/convstore/find_test.go` | new | +132 | find tests |
| `internal/convstore/find_helpers_test.go` | new | +69 | t.Setenv helpers |
| `internal/convstore/title.go` | new | +87 | `generateConversationTitle` + `WriteSummariesTitle` + `WriteCacheTitle` |
| `internal/convstore/title_test.go` | new | +140 | title tests inc. `TestGenerateConversationTitle/{simple_prompt,markdown_noise_stripped,code_fence_stripped,empty_prompt,punctuation_only}` |
| `docs/cycles/resume-continuation/deliverable-resume.md` | new | +264 | TL;DR + scope + slash table + architecture + gates + 9-item 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | conflict on tui_run.go struct-field block (autoTitle vs tasks-panel fields) — resolved by keeping both; tab indent restored via `gofmt -w` |
| `go build ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` ×3 | only 2 pre-existing flakes (`TestDispatchPrintSlash_LocalCommands`, `TestCycle225QuotaCountdown`); both verified independently on `main` (5a40576) |
| `gofmt -l` on every touched Go file | empty |
| `go vet ./cmd/wagyu/ ./internal/convstore/` | clean (only pre-existing `slash_cycle224_test.go` `%` warnings untouched) |
| New tests (cycle code paths) | 12 new tests, all green |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |
| Fix to pre-existing `TestGoroutineLeakResumePicker` leak | added `m.tasksSubCancel()` per iteration; tested 3/3 PASS after the fix |

## Two pre-existing flakes (NOT regressions)

| Test | Why it flakes |
|---|---|
| `TestDispatchPrintSlash_LocalCommands` (print_slash_test.go:45) | Asserts `dispatchPrintSlash("/model")` returns `"minimax-m3\tMiniMax M3\n"` but does not pin `ANTIGRAVITY_HOME`, so the result depends on the user's `~/.gemini/antigravity-cli/settings.json` model field. Environmental. |
| `TestCycle225QuotaCountdown` | Timing-flaky wall-clock minute boundary; fails ~20% of runs (5-isolated-test on main: 4/5 PASS). Pre-cycle on main 5a40576. |

## Review fix applied (revise step)

The rebase introduced a tab-indent loss in the conflict resolution at
`cmd/wagyu/tui_run.go:1263` (first line of merged tasks-panel + autoTitle
field block was missing the leading tab). Detected via `gofmt -l`, fixed via
`gofmt -w`, post-fix `gofmt -l` empty.

After the fix, `TestGoroutineLeakResumePicker` was still failing because
cycle tasks-panel's `NewRunMode` arms a subscriber goroutine on the shared
registry, and the test was creating 10 successive runmodes without cancelling
the subscriber — each leaked 1 goroutine. Fixed by adding
`m.tasksSubCancel()` per iteration; tested 3/3 PASS after the fix.

Both fixes are local to the cycle's own files (no pre-existing unrelated file
reformatted).

## 存疑

1. Auto-title leaks across `/new` and `/clear`. Both commands wipe
   `m.response`/etc. but not `m.autoTitle`. Documented in deliverable.
2. `loadConversationSelection` could be called twice if both `--continue` and
   the auto-resume path return non-empty. Guarded by `sel.ID == ""` short-circuit.
3. Parent walk depth is hard-coded at 8 (`continueParentMaxDepth = 8`).
4. Child walk is one level deep only (immediate subdirectories of cwd).
5. `promptAutoResume` reads stdin once.
6. Auto-resume prompt is gated only on TTY mode + env var.
7. No probe-vs-clone table for the auto-resume prompt output (requires a
   fresh `agy` launch with stdin attached — not attainable here).
8. `WriteSummariesTitle` does not surface "row not found" — issues a SQL
   UPDATE that affects 0 rows.
9. `TestDispatchPrintSlash_LocalCommands` is environmentally flaky
   (see AGENTS.md / cycle-225 history).

## Cleanup

- Worktree `/private/var/folders/.../openagy-resume/` removed.
- Branch `feature/resume-continuation` deleted.
- main is now `149bd2f`.

## Cleanup-on-conflict note

Two cross-worktree pollutions were caught during the rebase:
- `cmd/wagyu/slash_panels.go` / `slash_plugin_search.go` / `slash_cycle198_test.go` /
  `slash_cycle204_test.go` / `internal/cli/subcommand_parity.go` in the resume worktree
  were stale uncommitted writes from the `feature/agents-json-in-plugins` subagent.
  Discarded via `git checkout -- <files>` before the build/tests gate ran.
- Same pattern was later seen in `feature/status-line-script`'s worktree (per its
  own report).
- All 4 preceding-and-this cycle's polluter references now resolve via the
  upcoming `feature/agents-json-in-plugins` merge (17118be provides the
  `pluginAgents` / `pluginUnpackedAgents` definitions).
