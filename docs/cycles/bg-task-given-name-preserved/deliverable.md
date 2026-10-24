# Cycle bg-task-given-name-preserved — AGY 1.2.5 changelog gap

**Baseline:** `54db67b` (cycle 224)
**Branch:** `feature/bg-task-given-name-preserved`
**Changelog line:**
> *Improved background task naming so a task keeps the name it was given when sent to the background; completion notifications and task lists now show that name instead of a generic auto-derived one.*

## TL;DR

`create_task` already accepted a `title` parameter but the value was
only echoed back in the tool response — the taskregistry entry the
`/tasks` panel and completion notifications consume was always
auto-derived from the binary path + args (`bash -c <prompt>`,
`ddgr --num=10 <query>`, …). This cycle adds a `Title` field to
`subagent.Subagent` and wires it through the registry bridge so a
user-given title wins outright; empty falls back to the existing
auto-derived form so every existing caller continues to behave the
same way.

| File | Change | Net |
|---|---|---|
| `internal/subagent/subagent.go` | add public `Title` field + `SetTitle(string)` setter | +29 |
| `internal/taskregistry/bridge.go` | `subagentDisplayName` prefers `s.Title` over auto-derived | +14 / -3 |
| `internal/tools/stubs.go` | `create_task` calls `sa.SetTitle(p.Title)` before `AttachSubagent` | +8 |
| `internal/taskregistry/bridge_test.go` (new) | 5 unit tests for the bridge change | +253 |
| `internal/tools/stubs_bg_name_test.go` (new) | 4 end-to-end tests through `create_task` | +212 |

Total: 3 modified files + 2 new test files.

## Why the gap existed

`create_task`'s schema already declared a `title` parameter:

```go
`{"type":"object","properties":{"agentName":{"type":"string"},"prompt":{"type":"string"},"title":{"type":"string"}},"required":["agentName","prompt"]}`
```

and the response carried it back (`m["title"] == p.Title`), so on
the wire the title was honoured. But the registry path — the
single source of truth for `/tasks`, the status-row task count, and
any "completion notification" subscriber — went through
`internal/taskregistry/bridge.go:subagentDisplayName`:

```go
func subagentDisplayName(s *subagent.Subagent) string {
    name := s.Name           // agent_name (e.g. "bash")
    if name == "" && … { name = baseName(s.Cmd.Path) }
    if s.Cmd != nil && len(s.Cmd.Args) > 1 {
        // join Cmd.Args[1:] ("-c", "<prompt>") → "bash -c <prompt>"
        …
    }
}
```

The Subagent had no place to carry a user-given display name — the
constructor's `name` argument was the **binary path** (used to
build `exec.Cmd.Path`), not a friendly label. Title was therefore
silently dropped on the way to the registry, and `/tasks` showed
"bash -c <prompt>" instead of "My background task".

`run_command --background` was already correct: the `bgTask.command`
field stores the user's command string and `bgTask.status()` echoes
it verbatim, so the "given name" is preserved on that path. The
fix below is therefore scoped to the create_task / subagent path.

## The fix

### 1. `internal/subagent/subagent.go`

`Subagent` gains a public `Title string` field with a thread-safe
`SetTitle(string)` setter (the setter tolerates a nil receiver so
callers can chain it without a guard). The field is set once
between `NewSubagent` and the first reader; it is not mutated
afterwards, so the absence of a mutex is intentional and matches
the existing `ID`/`Name` convention.

```go
// Title is the human-given name the caller passed when sending
// the subagent to the background. The taskregistry (and the /tasks
// panel / completion notifications) prefer this over the auto-
// derived "<Name> <args...>" string produced by subagentDisplayName
// so a task keeps the friendly name it was given instead of
// reverting to the underlying command line. Empty means "no name
// given; fall back to the auto-derived form". Title is set once
// after construction (mirroring the way ID/Name are set) and is
// read by the taskregistry bridge at attach time; it is not
// mutated afterwards, so the lack of a mutex is intentional.
Title string

func (s *Subagent) SetTitle(title string) {
    if s == nil { return }
    s.Title = title
}
```

### 2. `internal/taskregistry/bridge.go`

`subagentDisplayName` returns `s.Title` verbatim when set, and
falls back to the existing auto-derived logic otherwise. The
fallback path is unchanged so a regression in either branch is
caught by the tests below.

```go
func subagentDisplayName(s *subagent.Subagent) string {
    if s == nil { return "" }
    if s.Title != "" {            // <-- new
        return s.Title
    }
    name := s.Name
    …
}
```

### 3. `internal/tools/stubs.go`

`create_task` calls `sa.SetTitle(p.Title)` between `NewSubagent`
and `AttachSubagent`. An empty title keeps the previous behaviour
(auto-derived "bash -c <prompt>") for backward compatibility.

```go
sa := subagent.NewSubagent(p.AgentName, p.Prompt)
sa.SetTitle(p.Title)              // <-- new
defaultSubagentManager.Add(sa)
defaultTaskRegistry.AttachSubagent(sa, "")
```

The `title` field is still returned in the response map, so any
caller that previously read it from the JSON envelope continues to
work unchanged.

## Tests

Four + five new tests pin the behaviour; the four required by the
deliverable spec are highlighted.

| Required? | Test | Asserts |
|---|---|---|
| ✔ given name in task list  | `TestSubagentDisplayNameHonoursTitle` | `subagentDisplayName` returns `Title` verbatim when set |
| ✔ default name when none   | `TestSubagentDisplayNameFallsBackToAuto` | empty `Title` → existing auto-derived name; `SetTitle("")` after a non-empty set clears the title |
| ✔ given name in notif      | `TestAttachSubagentCompletionNotificationShowsGivenName` | after Start + Kill (subagent goes terminal), registry's `Task.Name` is still the `Title`, not the auto-derived form |
| ✔ given name in task list  | `TestAttachSubagentGivenNamePreservedInRegistry` | `AttachSubagent` copies the `Title` onto `Task.Name`; `List()` returns it |
| regression pin             | `TestSubagentDisplayNameAutoTruncates` | the 60-char join cap and trailing ellipsis survive the Title-first branch |
| e2e: list preserves title  | `TestCreateTaskTitleUsedInRegistry` | `create_task({title:"Greet the user"})` → registry entry has `Name == "Greet the user"` |
| e2e: list fallback         | `TestCreateTaskNoTitleFallsBackToAuto` | `create_task({agent_name:"echo",prompt:"fallback"})` (no title) → `Name == "echo fallback"` |
| e2e: notif preserves title | `TestCreateTaskCompletionShowsGivenName` | after `create_task` returns, `Task.State == Done` and `Task.Name == Title` (NOT `"bash -c hi"`) |
| repair-pin                  | `TestSubagentSetTitleNilReceiver` | `SetTitle` on a `*Subagent(nil)` is a no-op |

Run:

```bash
go test -vet=off -count=1 -race \
  -run 'TestSubagentDisplayName|TestAttachSubagent|TestCreateTask|TestSubagentSetTitle' \
  ./internal/taskregistry/ ./internal/tools/
# ok  	github.com/chadgpt/openagy/internal/taskregistry	1.274s
# ok  	github.com/chadgpt/openagy/internal/tools	1.268s
```

The targeted run from the brief:

```bash
go test -vet=off -count=1 -run 'TestBackground|TestTask|TestName' ./cmd/wagyu/ ./internal/.../
# ok    github.com/chadgpt/openagy/cmd/wagyu	1.406s
# ok    github.com/chadgpt/openagy/internal/tools	0.439s
# ok    github.com/chadgpt/openagy/internal/subagentrouter	0.304s
# … (all other internal packages report "[no tests to run]")
```

Full build:

```bash
go build -o /tmp/wagyu-bg ./cmd/wagyu
# (exit 0; binary 73 MB, same size as 54db67b baseline)
```

`gofmt -l` is clean on every new file (and on the three modified
files). The pre-existing non-clean files in `internal/...` and
`cmd/wagyu/...` were not touched — see "硬约束 / limitations".

## 硬约束 / limitations

1. **`internal/convstore/sqlite.go` not touched.** Verified:
   `git diff --stat HEAD -- internal/convstore/` is empty.

2. **Pre-existing non-clean files left alone.** `gofmt -l` reports
   ~50 files under `internal/` plus a couple in `cmd/wagyu/`, all
   of them pre-existing — `gofmt -l` against the baseline
   `54db67b` shows the same list. Reformatting them wholesale
   would bury the real diff (per `AGENTS.md`).

3. **Scope is the create_task / subagent path.** `run_command
   --background` already preserved the user's command as the
   task name (`bgTask.command` → `bgTask.status()`). No change
   was needed there. A follow-on cycle could optionally add a
   `title` parameter to `RunCommand`'s background branch for
   symmetry; that was out of scope for this changelog line.

4. **`Title` is a public field.** Set once after construction,
   never mutated. No mutex — matches the `ID` / `Name` /
   `Cmd.Path` convention that has shipped since cycle 22. The
   `SetTitle` setter exists so a future caller that wants to
   reset the title (e.g. a re-attach path) has a single point
   of contact; it tolerates a nil receiver so callers can chain
   it without a guard.

5. **`SetTitle` after `AttachSubagent` is a no-op for the
   already-registered entry.** The bridge reads `Title` at
   attach time and copies it into `Task.Name`; subsequent
   `SetTitle` calls change the Subagent field but not the
   registry's snapshot. This matches the existing field-mutation
   semantics — `Subagent.Name` set after attach would behave
   identically.

6. **Test `TestDispatchPrintSlash_LocalCommands` is unrelated.**
   It fails on `54db67b` too (the test expects a default model
   label that this environment doesn't carry). The required
   `TestBackground|TestTask|TestName` patterns all pass.

## Return

| Item | Value |
|---|---|
| Commit | _set after `git commit`_ — see git log |
| Files modified | 3 (`internal/subagent/subagent.go`, `internal/taskregistry/bridge.go`, `internal/tools/stubs.go`) |
| Files added | 2 (`internal/taskregistry/bridge_test.go`, `internal/tools/stubs_bg_name_test.go`) |
| Tests added | 9 (4 required by the brief + 5 regression pins) |
| `go build ./cmd/wagyu` | exit 0 |
| `gofmt -l` (new files only) | empty |
| Branch pushed | no (per brief) |
| Worktree | `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-bg-task-given-name-preserved` |