# merge agents-json-in-plugins — plugin root agents.json validate / install / discover

## TL;DR

AGY 1.3.6 changelog line 13 + 35: a plugin's root `agents.json` declares
subagents to register. The clone now `validate`s the file during
`/plugin validate`, surfaces an `Agents:` row in both the installed
plugin detail block and the Discover details, and the install path copies
the source tree into `~/.gemini/config/plugins/<name>/` with the union
of any agents.json + AGENTS.md into the enabled plugin manifest.

| | |
|---|---|
| Branch | `feature/agents-json-in-plugins` |
| Commit | `cf4e419` |
| Merge | `7f23c6b` |
| Lines | +1382 / −9 (9 files) |
| New files | `cmd/wagyu/plugin_agents.go`, `cmd/wagyu/slash_cycle234_test.go`, `internal/cli/plugin_agents_test.go` |
| Cycle report | `docs/cycles/agents-json-in-plugins/deliverable.md` |

## Scope (vs AGY 1.3.6 line 13/35)

| Reference behaviour | Clone implementation | Status |
|---|---|---|
| Plugin root `agents.json` (nested) | `internal/cli/subcommand_parity.go::legacyLoadPluginAgents` reads `{"agents": [...]}` (1.3.6 nested) and falls back to the pre-1.3.3 flat `[...]` form | ✅ |
| `name` empty → drop; `path` preserved verbatim | `plugin_agents.go::normalizeAgentsJSON` | ✅ |
| Union dedupes by name (JSON read second → wins) | `plugin_agents.go::pluginAgents` | ✅ |
| `/plugin validate` shows `agents : N processed` | `runPluginValidate` agents line | ✅ |
| `/plugin install` copies tree into plugins dir | `subcommand_parity.go::copyTree` (`os.CopyFS`) | ✅ |
| `/plugin install` marks enabled=true | `subcommand_parity.go::legacyMarkEnabled` (local rather than `auth.SetPluginEnabled`; install runs before auth state is ready) | ✅ |
| Malformed JSON → exit 1 + `Error: invalid agents.json: ...` | yes | ✅ |
| `Agents:` row in `/plugin list` details | `cmd/wagyu/slash_panels.go::pluginInstalledDetails` (6th row, after Skills:) | ✅ |
| `Agents:` row in `/plugin discover` details | `cmd/wagyu/slash_plugin_search.go::pluginDiscoverDetails` (reads unpacked cache → `loading...` if not unpacked yet) | ✅ |

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `cmd/wagyu/plugin_agents.go` | new | +264 | TUI loader |
| `cmd/wagyu/slash_cycle234_test.go` | new | +338 | 11 TUI tests |
| `cmd/wagyu/slash_panels.go` | modified | +13 / −1 | pluginInstalledDetails + Agents row |
| `cmd/wagyu/slash_plugin_search.go` | modified | +14 / −1 | pluginDiscoverDetails + Agents row |
| `cmd/wagyu/slash_cycle198_test.go` | modified | +1 / −0 | Agents: - row expected |
| `cmd/wagyu/slash_cycle204_test.go` | modified | +4 / −1 | Agents: row assertion |
| `internal/cli/subcommand_parity.go` | modified | +206 / −2 | legacyListAgents, runPluginValidate agents line, runPluginInstall full install path, copyTree, legacyMarkEnabled |
| `internal/cli/plugin_agents_test.go` | new | +328 | 8 CLI tests |
| `docs/cycles/agents-json-in-plugins/deliverable.md` | new | +222 | TL;DR + scope + schema + gates + 8-item 存疑 |

## Gates

| Gate | Result |
|---|---|
| `git rebase main` | clean (no conflicts) |
| `go build -o /tmp/wagyu-agentsjson ./cmd/wagyu` | exit 0, 73 MB |
| `go test -vet=off -count=1 ./cmd/... ./internal/...` (×3) | only `TestDispatchPrintSlash_LocalCommands` fails — pre-existing environmental flake (REF's `/model` default `gemini-3.8-flash-medium` ≠ test expectation `minimax-m3`); confirmed identical on `main` (5a40576) |
| `gofmt -l` on every touched Go file | empty |
| `go vet ./cmd/wagyu/ ./internal/...` | only pre-existing cycle-224 `%` warnings |
| New tests (cycle code paths) | 19/19 pass (8 CLI + 11 TUI) |
| `internal/convstore/sqlite.go` | NOT TOUCHED ✓ |

## Smoke test (clone-only; REF is 1.4.0 here, AGY 1.3.6 line is older)

- `validate` json-only fixture → `agents : 4 processed`
- `install` json-only fixture → tree copied to
  `~/.gemini/config/plugins/agents-json-plugin/`, `Installed plugin "..."`
  summary, `config.json` gains `plugins.<name>.enabled=true`,
  `plugin list` shows `(enabled, 1.2.3)`
- `validate` malformed JSON → exit 1 + `Error: invalid agents.json: invalid character 'o' ...`

## 存疑

1. JSON wins on name collision (changelog silent; deliberate but unverified vs REF).
2. `include_only` recursion (changelog line 169) not implemented — out of scope.
3. `path` not gated on `.md` extension — matches REF schema.
4. Discover `loading...` driven by TUI's marketplace unpack handler.
5. `copyTree` uses `os.CopyFS` (Go 1.23+); 1.22.10 sandbox needs stdlib workaround.
6. `copyTree` + `legacyMarkEnabled` local rather than importing `auth.SetPluginEnabled`.
7. No `--plugins-dir` CLI flag — `legacyPluginsDir()` honours `GEMINI_CONFIG_HOME`.
8. Pre-existing `TestDispatchPrintSlash_LocalCommands` flake (REF's `/model`
   default drift) — unrelated to cycle 234.

## Cleanup

- Worktree removed.
- Branch deleted.
- main is now `7f23c6b`.
