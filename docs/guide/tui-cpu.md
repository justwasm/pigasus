# TUI CPU hot-path safeguards

This guide records the bounded work guarantees added after sampling two
long-lived TUI processes at high CPU.

## Task refresh

Registry notifications use `tasksChangedMsg`; the periodic refresh is
`tasksTickMsg`. A single `TasksModel.tickArmed` timer may be outstanding, and
each tick carries a generation so a late message cannot revive an obsolete
chain. The subscription goroutine coalesces pending wakeups atomically; it does
not touch the Bubble Tea-owned timer state.

Task updates invalidate frame/layout geometry, not the transcript's wrapped
rows. The transcript remains unchanged as task durations and status rows move.

## View and transcript caches

`viewportHeight` measures prompt geometry before the final view body is
assembled. `RootModel.View` memoizes the formatted composer only for that one
call; it is discarded on return, so cursor, selection, placeholder, and style
changes cannot reuse output from a previous frame.

Composer-only keys invalidate frame/layout geometry but preserve the wrapped
transcript. Vim, modal, escape-buffered, modified, and custom action keys use
the conservative full invalidation path. New message kinds should default to
that same conservative path until their transcript effects are understood.

## Configuration snapshots

The ordinary settings and provider APIs continue to detect external edits
immediately. Render-heavy callers can use `settings.CachedSnapshot()` and
`models.CachedRegistrySnapshot()`: they check file metadata at most once per
250 ms, retain the last valid parse for a temporarily invalid same-path file,
and are invalidated immediately by local settings/provider writes. Explicit
cache invalidation also bypasses the interval. The status row caches its
resolved context window by provider registry generation.

## Bounded filesystem search

`WalkFilesContext` uses `filepath.WalkDir`, does not descend into symlinks, and checks
cancellation between entries. Explicit regular-file roots retain the legacy
symlink-to-file behavior. Grep and codebase search stream text files with
an 8 MiB per-file / 64 MiB total scan budget, skip individual lines larger
than 256 KiB, limit results to 200, and bound rendered output to 512 KiB.
Output indicates when a budget truncated the scan. No broad new ignore list is
applied, so an explicitly selected generated directory remains searchable.

## Regression checks

```sh
go test ./internal/tui -run 'TestTasksRefresh|TestComposerEdits|TestViewFormatsComposer|TestStatusContextSnapshot'
go test ./internal/core/settings ./internal/features/models ./internal/features/models/catalog ./internal/features/models/modelctx ./internal/ui/views/modelpicker ./internal/app/tools/filesystem
go test -race ./internal/core/settings ./internal/features/models ./internal/features/models/catalog ./internal/features/models/modelctx ./internal/ui/views/modelpicker ./internal/app/tools/filesystem
go test ./internal/tui -run '^$' -bench 'BenchmarkPromptFrameFormatting|BenchmarkCPUComposerFrame|BenchmarkTasksRefreshNotificationBurst' -benchmem
```

`BenchmarkPromptFrameFormatting` compares the per-frame composer memo against
an uncached double-format path. `BenchmarkCPUComposerFrame` includes 100,
1,000, and 5,000 transcript steps to catch work that grows with history.
