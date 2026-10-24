# Cycle 168 — Drop `v135` from filenames, identifiers, comments

## Scope

User feedback: "原版...根本就没... `v135` 这个完全没有意义了... 你可以 refactor/rename 一下".
The `_v135` suffix landed in filenames during cycle 46 (v1.3.5 binary 1:1
TUI). It has never reflected the actual code contents since: cycles
62+ added v1.3.6 work, cycles 80+ added tooling-state work, etc. The
suffix became misleading (the project target is now `~/.local/bin/agy`
1.4.0).

This commit renames the v135-suffixed identifiers fully and brings
the package/import paths up to date.

## Files renamed

| Old | New | Why |
|---|---|---|
| `cmd/wagyu/tui_v135.go` | `cmd/wagyu/tui_run.go` | File name no longer describes the v135 sub-state; the file is the run mode. |
| `cmd/wagyu/tui_v135_test.go` | `cmd/wagyu/tui_run_test.go` | Same. |
| `internal/auth/v135_compat.go` | `internal/auth/compat.go` | Cycle 48 v1.3.5 compat work; doesn't carry v135 specifics on its own. |
| `internal/auth/v135_compat_helpers.go` | `internal/auth/compat_helpers.go` | UUID helper. |
| `internal/auth/v135_compat_test.go` | `internal/auth/compat_test.go` | Tests for compat.go. |
| `internal/cli/v135_parity.go` | `internal/cli/subcommand_parity.go` | Cycle 96 subcommand surface parity. |
| `internal/cli/testdata/changelog_v135.txt` | `internal/cli/testdata/changelog_reference.txt` | Filename was misleading; comments now explain it's a v1.3.5 capture of `agy changelog`. |
| `internal/tui_v135/{commands,picker,picker_test}.go` | `internal/tuilib/{commands,picker,picker_test}.go` | Whole package rename (3 files); package declaration also flipped `package tui135` → `package tuilib`. |

`internal/cli/changelog_v135.txt` is the only `v135` left in any
*filename* in the repo, and only because the embedded file content
itself is the v1.3.5 binary's literal `changelog` output — keeping
the descriptive name is honest about what's in the file.

## Identifiers renamed

| Old | New | Count touched |
|---|---|---|
| `v135RunMode` | `runMode` | ~270 (every receiver, every method body) |
| `NewV135RunMode` | `NewRunMode` | ~25 |
| `v135Changelog` | `referenceChangelog` | 5 |
| `v135Changelog*` constants | `referenceChangelog*` | 9 |
| `renderV135Help{,Commands,General,Shortcuts}` | `renderHelp{,Commands,General,Shortcuts}` | 4 |
| `buildV135CommandRow` | `buildCommandRow` | 1 |
| `v135Logo{,Lines,Block}` | `logo{,Lines,Block}` | 12 |
| `v135Separator{,Width}` | `separator{,Width}` | 5 |
| `v135Input{,Prompt,Width}` | `input{,Prompt,Width}` | 4 |
| `v135Status{,Width,Right,RightPrefix}` | `status{,Width,Right,RightPrefix}` | 30 |
| `v135TabBar*` | `tabBar*` | 14 |
| `v135Help{,Header,DimGray,Blue}*` | `help{,Header,DimGray,Blue}*` | 30 |
| `v135Step{,Bullet,BulletOpen,BulletPlain,BulletErrOpen}` | `step{,Bullet,BulletOpen,BulletPlain,BulletErrOpen}` | 60 |
| `v135Thought{,BulletOpen,BulletPlain}` | `thought{,BulletOpen,BulletPlain}` | 3 |
| `v135Detail{,BulletOpen,BulletPlain}` | `detail{,BulletOpen,BulletPlain}` | 3 |
| `tui_v135` (import path) | `tuilib` | 3 sites |
| `tui135.` (qualified package) | `tuilib.` | ~12 |
| `WriteV135CompatDefaults` | `WriteCompatDefaults` | 7 |
| `ConfigPathV135{,Auth}` | `LegacyTokenPath{,Auth}` | 5 |
| `newV135ConversationID{,2}` | `newConversationID` / `newUUIDv4` | 6 |
| `isTerminalV135` | `isTerminalInteractive` | 2 |
| All `TestV135_*` test names | `Test_*` | ~40 |

Plus all the `v135*`-prefixed cli's `legacy{PluginsDir,Plugin,LoadPlugins,LoadMCPConfig,MCPListConfigPath,RenderMCPList*,ListSkills,MCPConfigPath,MCPDoc,LoadMCPDoc,SaveMCPDoc,MCPType,MCPStatus,MCPTarget,SignedIn,Plugin}` etc. — caught by an `perl` pass over the wider package.

## Comments cleaned up

Everywhere a comment said "v135 picker", "v135 key dispatch",
"v135_compat schema", "v135_conv_id", etc., the comment now reads
"tuilib picker", "tuilib key dispatch", "legacy compat schema",
"newConversationID" respectively. Older cycle deliverables (under
`docs/cycles/cycle-*`) still reference the old names because they
describe what was true at that cycle and shouldn't be falsified.

## go.mod / go.sum

`go mod tidy` picked up `mvdan.cc/sh/v3` (was a stray indirect used by
`internal/tools/command.go` from cycle 169; the require line was
missing). Verified by `go build ./cmd/... ./internal/auth ./internal/cli ./internal/convstore ./internal/tuilib ./internal/subagent ./internal/tools` succeeding.

## Tests

```
go test ./cmd/wagyu -count=1
ok      github.com/chadgpt/openagy/cmd/wagyu       0.632s

go test ./internal/auth -count=1
ok      github.com/chadgpt/openagy/internal/auth   1.435s

go test ./internal/cli -run "TestChangelog_"
TestChangelog_ByteLen   PASS
TestChangelog_Lines     PASS
TestChangelog_SHA256    PASS
TestChangelog_Shape     PASS

go test ./internal/cli  (full)
TestChangelog_MatchesReferenceBinary  FAIL  (pre-existing 3232-byte drift, not introduced here)
```

The pre-existing `TestChangelog_MatchesReferenceBinary` failure remains
and is unchanged by this commit; it's a main-branch baseline drift
recorded long before cycle 168.

## What isn't renamed

- `testdata/changelog_v135.txt` is replaced (renamed to `changelog_reference.txt`), not lost.
- `internal/cli/testdata/changelog_v135.txt` is the same file path — now `changelog_reference.txt` only.
- All `docs/cycles/cycle-*` markdown deliverables keep the old names — they describe historical work.

## Open / Limitations

- The `m.mouseEnabled` flag and `/mouse` slash command from cycle 166 are still in the codebase. The next cycle (169) closes them — the reference agy has no `/mouse` and supports wheel + select-and-copy simultaneously via in-app text selection.
- The `v135_compat*` functions still internally write **v1.3.5-compatible** config files (`~/.config/antigravity/...`); the rename to `compat.go`/`WriteCompatDefaults` doesn't change behaviour. The functions are still documenting "what the v1.3.5 binary does on first launch"; their docstrings say so explicitly.
