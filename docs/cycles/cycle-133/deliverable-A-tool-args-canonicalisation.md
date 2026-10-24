# Cycle 133 — case-insensitive tool argument decoding

## Why

> 还有一个问题是，明明优先命令的工具调用成功了，但是它显示 failed, fix it

The user reported that **successful tool calls were being marked as
`(Failed)` in the TUI transcript**. The root cause: the v1.3.6
reference's wire format uses **capitalised JSON keys**
(`AbsolutePath`, `TargetFile`, `CommandLine`, `Query`, `OldString`,
`NewString`, etc.), but the cycle-72 clone's tool catalog decodes
**lowercase** keys (`path`, `command`, `query`, `oldString`,
`newString`).

So a perfectly valid `view_file({"AbsolutePath": "secret.txt"})`
would unmarshal with `Path == ""`, fall back to the workspace
directory root, fail at `os.ReadFile(<directory>)`, and the
transcript would dutifully report `Read <empty> (Failed)`.

## What changes

### `internal/tools/nonnetwork.go`

- New helper `argsOf(raw, v)` (already existed; cycle 133 swaps in a
  canonicalisation pass before `json.Unmarshal`).
- New `canonicaliseJSONKeys(raw)` walks every JSON object key
  through `canonicaliseAny` and re-marshals. Non-object payloads
  pass through unchanged.
- New `canonicaliseAny(v)` recursively rewrites object keys via
  `camelToSnake`.
- New `camelToSnake(s)` converts CamelCase to snake_case but
  consults a **known-aliases map** (`toolKeyAliases`) first. The
  aliases map wire-format keys like `AbsolutePath` to the exact
  struct tag the clone uses (e.g. `"path"`, `"oldString"`,
  `"start_line"`). Unknown keys fall through to the snake_case
  fallback so any future struct tag is matched.

The aliases table covers every wire key the cycle-99 / cycle-100
recovery surfaced (and the catalog now uses):

| Wire key                | Struct tag       |
|---|---|
| `AbsolutePath` / `FilePath` / `TargetFile` / `DirectoryPath` / `SearchPath` / `File` / `directory` / `target_file` / `file_path` / `search_path` / `absolute_path` | `path` |
| `CommandLine` / `command_line` / `Command` / `Cmd` | `command` |
| `OldString` / `oldString` | `oldString` |
| `NewString` / `newString` | `newString` |
| `StartLine` / `startLine` | `start_line` |
| `EndLine` / `endLine`     | `end_line` |
| `MaxBytes` / `MaxLines`   | `maxBytes` / `maxLines` |
| `Query` / `Pattern` / `SearchTerm` | `query` / `pattern` |
| `Url` / `URL` / `Prompt` / `Goal`   | `url` / `prompt` / `goal` |
| `Global` / `Recurse` / `Includes`  | `global` / `recursive` / `includes` |
| `ReplaceAll` / `AllowMultiple`     | `replaceAll` / `allowMultiple` |

If the canonicalisation pass fails (malformed JSON), `argsOf`
falls back to the raw bytes so the existing error path still
fires.

### Tests

| Test | Asserts |
|---|---|
| `TestArgsOf_CaseInsensitiveKeys` | All-capitalised keys decode to the struct fields; lowercase keys still work; nested objects canonicalise recursively; nil payload still produces zero-value struct |
| `TestGrepSearchTool_CapitalisedArgs` | `GrepSearchTool` accepts `{"Pattern":"...","Path":"..."}` (capitalised) and returns matches against a temp-file fixture |

All 24 pre-existing tool tests in `internal/tools/` still pass.

## What does NOT change in cycle 133

- **`toolStep.err`** (the field on `toolStep`) — the rendering
  logic in `stepTitle` is unchanged. The bug was *upstream* of the
  display: the tool implementations saw a non-nil `os.ReadFile`
  error because the path was empty. Cycle 133 fixes the *input*
  to the tool runner, so the runner returns `(out, nil)` and
  `s.err` stays false.
- **Display of `(Failed)`** — still appended only when the Go
  error from the runner is non-nil. Cycle 133 doesn't broaden or
  narrow this — it's a faithful projection of the runner's
  verdict.
- **JSON-in-string canonicalisation** — the pre-pass walks the
  decoded `any` tree, so it ignores literal JSON inside string
  values. The recovered wire format has no such cases.

## Hard constraints / 存疑 / limitations

- 【推测】The exact wire-format casing was reconstructed from the
  recovered arg-shape strings in `tui_tools.go::toolStepTitle`
  (cycle 99) and from the reference's `strings ~/.local/bin/agy`
  scan. The alias map covers every form that appeared in those
  recoveries; new variants would need new entries.
- 【推测】The aliases list assumes struct tags stay where they are.
  If cycle 134+ renames a tag (e.g. `path` → `filePath`), the
  alias needs updating too. Both directions of the contract are
  in the same file, so a single diff keeps them in sync.
- The `camelToSnake` fallback is a defensive net for any wire key
  we haven't seen. It's not expected to fire in practice; if it
  does, the right move is to add the alias and re-test.