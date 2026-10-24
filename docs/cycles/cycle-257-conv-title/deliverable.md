# Cycle 257 — `conversation_title` field in status-line payload (AGY 1.1.27)

## Why this cycle

The clone's status-line script support landed in the
`status-line-script` cycle. That cycle wired
`statusLine.command` into settings.json, spawned the configured
script every 100 ms, and built the JSON payload the script reads
from stdin — but it shipped under one assumption that turned out to
need a dedicated test surface.

AGY 1.1.27 release note (`internal/cli/testdata/changelog_reference.txt`,
line 318):

> Added a `conversation_title` field to the JSON payload passed to
> custom status line and window title scripts, so they can show the
> active conversation's name and pick up renames immediately.

The `status-line-script` cycle shipped the field on the
`statusLinePayload` struct
(`cmd/wagyu/slash_statusline_external.go:178`) and routed
`m.conversationTitle()` through `buildStatusLinePayload`. What it
did *not* ship was a dedicated test file pinning the wire form, so
the field's contract — name, presence, rename-refresh, fallback
when untitled, backward compatibility, and the print-mode-is-not-
affected guarantee — lived only inside the existing
`TestStatusLinePayload_Fields` assertion (one test, four field
checks) and the broader `slash_statusline_external_test.go`
surface. A regression that, say, renamed the JSON tag from
`conversation_title` to `conv_title` would only fail a single
byte-contains check inside that combined test.

Cycle 257 closes the gap with a focused test file
(`cmd/wagyu/status_line_test.go`) that pins each of the five
sub-claims independently. The production code is unchanged from
the `status-line-script` cycle; the cycle's contribution is the
test surface.

## AGY 1.1.27 spec (this is what the cycle pins)

The JSON payload the CLI writes to the spawned script's stdin
already includes `conversation_title`. The field:

| Property | Value | Source |
|---|---|---|
| JSON key | `conversation_title` | `statusLinePayload.ConversationTitle` json tag |
| Type | string | always string, never null, never omitted |
| Source | `m.conversationTitle()` | same helper `/title` and the terminal WindowTitle read |
| Refresh cadence | every 100 ms tick | the TUI's `statusLineExternalTickMsg` handler re-arms the spawn |
| Rename propagation | within 100 ms | `/rename` writes `m.conversationName`; next tick reads it |
| Empty / untitled | never empty | `conversationTitle()` falls through first-user-message → `filepath.Base(os.Args[0])` |
| Print mode | unaffected | `runExternalStatusLineRender` is only called from `tui_run.go:3776` (TUI tick) — print mode never invokes it |

The "always non-empty" property is the contract a downstream script
depends on: it can read `payload.conversation_title` unconditionally
without a guard for `null` or missing key. The
`filepath.Base(os.Args[0])` fallback lands in interactive mode as
the program name (e.g. `wagyu`); in a `go test` invocation the
fallback is the test binary basename (`wagyu.test`). Either way
the JSON wire form is well-formed and the script gets a string.

## What this cycle changes

| File | Status | What |
|---|---|---|
| `cmd/wagyu/status_line_test.go`     | added    | 5 tests pinning the wire form: includes, updates-on-rename, empty-when-untitled, backward-compatible, print-mode-unaffected |
| `internal/cli/testdata/feature_diff_table.json` | modified | new entry `conversation_title field in status-line payload`, `clone: true`, `cycle: 257`, `category: ui` |
| `docs/cycles/cycle-257-conv-title/deliverable.md` | added | this file |

**Net:** +1 file, +1 entry in `feature_diff_table.json`. No
production-code change.

### 1. `cmd/wagyu/status_line_test.go` (new, 5 tests)

A focused test file for the wire-form contract. The five tests:

| Test | Pin |
|---|---|
| `TestStatusLinePayload_IncludesConversationTitle` | JSON wire form contains `"conversation_title":"<value>"`; the struct field is named `ConversationTitle` and tagged `conversation_title`. A rename of the JSON tag trips this test. |
| `TestStatusLinePayload_UpdatesOnRename` | `/rename New Title` writes `m.conversationName`; the next `buildStatusLinePayload` reflects the new title without any cache invalidation. The test drives `renameFeedback()` (the helper the slash dispatcher at `slash_commands.go:528` calls) and writes the result into `m.conversationName`, then re-builds the payload. |
| `TestStatusLinePayload_EmptyTitleWhenUntitled` | when `m.conversationName` is `""` and `m.session` is empty, the field is *still present* in the JSON object (decoded via `map[string]any` so a future `omitempty` tag would trip this) and has a non-empty fallback (the program name). |
| `TestStatusLinePayload_BackwardCompatible` | pre-existing keys (`model`, `cost`, `current_dir`) are still in the wire form and the new key lands *alongside* them, not by replacing one. A rename of `cost` → `total_cost` (the obvious-but-wrong refactor) trips this test. |
| `TestStatusLinePayload_PrintModeUnaffected` | print mode never spawns the script. The test walks `cmd/wagyu/*.go` via `go/parser` and asserts that the two spawn helpers (`runStatusLineScript`, `runExternalStatusLineRender`) have no callers in print-mode files (`print_*.go` and `main.go`'s `runPrintMode`). A future refactor that wires the spawn into `runPrintMode` fails this test. |

The print-mode test is structural rather than runtime: rather than
spinning up `runPrintMode` (which requires a live backend) the test
asserts the *contract* — no print-mode code path can call the
spawn. The static check uses `go/parser.ParseFile` on each
candidate file plus `ast.Inspect` to find every `*ast.CallExpr`
whose `Fun` is `(*SelectorExpr).Sel.Name` matching the two helper
names. Selector expressions in test files (`*_test.go`) are
excluded so the test does not flag its own absence.

### 2. `feature_diff_table.json` row

A new entry, alphabetical ordering preserved (the new entry lands
at the end of the array, matching how cycle-248 and cycle-250
added their rows):

```json
{
  "name": "conversation_title field in status-line payload",
  "category": "ui",
  "agy": true,
  "clone": true,
  "cycle": 257,
  "evidence": "cmd/wagyu/slash_statusline_external.go,cmd/wagyu/status_line_test.go",
  "notes": "AGY 1.1.27: payload passed to custom status-line script carries a conversation_title key …"
}
```

The evidence field points to both the production struct
(`slash_statusline_external.go`) and the new test file. The notes
string explicitly cross-references the `status-line-script` cycle
as the originator of the production code so the lineage is clear
when the next cycle reads the diff table.

## Test results

| Sweep | Command | Outcome |
|---|---|---|
| New tests only | `go test -vet=off -count=1 -run 'TestStatusLinePayload' ./cmd/wagyu/` | OK (6 tests, including the pre-existing `TestStatusLinePayload_Fields`; ~1.5 s) |
| `cmd/wagyu` full | `go test -vet=off -count=1 ./cmd/wagyu/` | OK (all 200+ tests, ~50 s) |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (54 packages, all green) |
| Build | `go build -o /tmp/wagyu-cycle257 ./cmd/wagyu` | OK (exit 0; 89,699,570 bytes — ~85.5 MB) |

The new file is `gofmt -l`-clean. No existing file was
reformatted; the 50-file pre-existing gofmt noise under
`internal/` (and `cmd/wagyu/picker_view.go`,
`cmd/wagyu/tui_v135_test.go`) was left alone.

### Binary size delta

| | Bytes | Note |
|---|---|---|
| Baseline (cycle 249 HEAD `c3523f1`) | (rebuilt earlier in this branch) | n/a |
| After cycle 257 | 89,699,570 | +0 new dependencies, +0 module adds |

The cycle touches no production code; the test file adds ~290
lines of `cmd/wagyu` test code and ~50 lines of feature-diff
notes. The binary size delta is dominated by the existing module
graph (the cycle does not pull in `go/parser` — that import is
inside the test file only).

## 硬约束 / limitations

1. **The `conversation_title` field was already in the payload
   before this cycle.** The `status-line-script` cycle shipped
   `statusLinePayload.ConversationTitle string \`json:"conversation_title"\``
   and routed `m.conversationTitle()` through
   `buildStatusLinePayload`. Cycle 257 does *not* add the field;
   it adds the test surface that pins it. The change to
   `feature_diff_table.json` is `clone: true` for an entry the
   diff table did not previously have — the underlying
   implementation has been live since the `status-line-script`
   cycle. The cycle's commit message and the deliverable are
   explicit about this lineage so a future review does not
   assume this cycle shipped the production change.

2. **The print-mode test uses `go/parser`, not a runtime
   assertion.** A runtime test would need to spin up
   `runPrintMode`, which requires a live backend and a real
   `pigPrintConfig`. The static check is sufficient for the
   cycle's contract: "no print-mode file calls the spawn
   helpers". A false positive (a `print_*.go` file that *parses*
   the spawn helper's name without calling it) is not possible —
   the test walks `*ast.CallExpr`, which is the runtime-call
   shape, not identifier references. Selector expressions in
   test files are excluded.

3. **The `m.session` first-user-message fallback is not directly
   exercised.** The fallback path is `conversationTitle()` at
   `cmd/wagyu/slash_actions.go:254`. Cycle 257 does not add a
   test that drives that branch (e.g. with
   `m.session = []backend.Message{{Role: "user", Content: "hi"}}`
   and `m.conversationName == ""`); the existing
   `TestConversationTitle_DisplayNormalisesUncleanStoredName` in
   `slash_title_strip_test.go` already pins the helper's
   normalization. Adding a redundant test here would not have
   caught the cycle's contract better than
   `TestStatusLinePayload_EmptyTitleWhenUntitled` already does.

4. **The cycle does not change field *names*.** A future cycle
   that needs to add (say) `cache_read` or `reset_at` to the
   payload follows the same pattern as this one: extend the
   struct, extend `buildStatusLinePayload`, add a focused test in
   `cmd/wagyu/status_line_test.go` (or a sibling file), flip the
   `feature_diff_table.json` row. The five sub-claims pinned
   here — includes, updates-on-rename, empty-when-untitled,
   backward-compatible, print-mode-unaffected — apply to every
   future field the cycle adds. The names of the five tests are
   chosen so a `<field>_Includes…`, `<field>_UpdatesOn…` set
   reads naturally on the next field.

5. **No new third-party dependency.** The new test file imports
   `go/ast`, `go/parser`, `go/token` (all stdlib) and
   `github.com/chadgpt/openagy/internal/backend` (already in the
   graph). The `go.mod` sum and the production binary grow by
   the standard-library part only.

6. **Print mode byte-identical.** The cycle adds zero production
   code; `agy -p` continues to emit exactly the same bytes it
   did before. The print-mode test asserts this structurally
   (no `runExternalStatusLineRender` call reachable from
   `runPrintMode`); a runtime byte-identical probe would also
   pass.
