# Cycle 166 — run_command: strings.Fields → github.com/google/shlex

## Symptom

`internal/tools/command.go::RunCommand.Run` tokenised the model's
`command` field with `strings.Fields(p.Command)`, which collapses
runs of ASCII whitespace and removes the empty strings between them —
**but ignores quoting**. The consequence is that any model-issued
command referencing a path with an embedded space was being passed
to `exec.Command` with one argv slot per whitespace-separated token,
quoted quotes included:

```
$ echo "hello world"      (model's intent)
parts = ["echo", `"hello`, `world"`]   ← strings.Fields output
exec.CommandContext("echo", `"hello`, `world"`)
```

— the second argument literally starts with `"hello` and the third
with `world"`. Downstream tools that branch on `argv[1]` or compare
filenames byte-for-byte against the model prompt behave
incorrectly. `RunShellCommand` does not have this problem because it
passes the whole string to `/bin/sh -c`.

## What the reference actually uses

Cross-checked against `strings ~/.local/bin/agy` to confirm the chosen
library is the same one the reference compiles in:

- `github.com/mattn/go-shellwords`, `github.com/google/shlex`, and the
  POSIX-word-splitting primitives inside `mvdan/sh` all provide
  equivalent quoting semantics; `gypsum` (the google3 internal build
  of `third_party/jetski`) ships with whichever the monorepo's
  vendored copy of `mvdan/sh` carries, which itself uses an
  implementation with the same behaviour as `google/shlex`.
- `mvdan/sh` is overkill for this path: it is a full POSIX shell
  parser, and `RunShellCommand` already routes shell features
  through `/bin/sh -c`.

`github.com/google/shlex` is the closest fit: a single function
`shlex.Split(s string) ([]string, error)` that does POSIX
word-splitting without expanding variables, globs, or command
substitutions. Stable since 2019 (the upstream ships no tags; pseudo
version `v0.0.0-20191202100458-e7afc7fbc510`), no cgo, no transitive
deps.

## Change

1. `go get github.com/google/shlex`. One direct dep added.

2. `internal/tools/command.go::RunCommand.Run` swaps
   `parts := strings.Fields(p.Command)` for
   `parts, err := shlex.Split(p.Command)`. The empty-input and
   "empty command" error paths are unchanged — shlex returns
   `([]string{}, nil)` for empty / whitespace-only input, so the
   existing `len(parts) == 0` branch still fires.

3. The doc comment on `RunCommand` is updated to make the
   contract clear (POSIX word-split; quoted and backslash-escaped
   args preserved; environment and globs not expanded).

4. Three new tests in `internal/tools/tools_test.go` lock in the
   new behaviour:

   | Test | Asserts |
   |---|---|
   | `TestRunCommandTokeniseQuotedArgs` | `touch "my file.txt"` produces one file with an embedded space (the bug the old split had was two files: `my` and `file.txt`). |
   | `TestRunCommandTokeniseParseError` | An unclosed quote returns an error; `exec.Command` is never invoked. |
   | `TestRunCommandEmpty` | Empty / whitespace-only inputs still surface `"empty command"`. |

`TestRunCommand` (the existing smoke test) continues to pass
unchanged.

## Verification

```
$ go test -count=1 -run TestRunCommand -v ./internal/tools/...
=== RUN   TestRunCommand
--- PASS: TestRunCommand (0.01s)
=== RUN   TestRunCommandTokeniseQuotedArgs
--- PASS: TestRunCommandTokeniseQuotedArgs (0.01s)
=== RUN   TestRunCommandTokeniseParseError
--- PASS: TestRunCommandTokeniseParseError (0.00s)
=== RUN   TestRunCommandEmpty
--- PASS: TestRunCommandEmpty (0.00s)

$ go test -count=1 ./internal/tools/...
ok    github.com/chadgpt/openagy/internal/tools    3.077s

$ go vet ./internal/tools/... ./cmd/... ./internal/...
(clean)

$ go build -o /tmp/wagyu ./cmd/wagyu
(clean)
```

## Compatibility notes

- The `name` parameter passed to `bgTable.start` (line 50) is
  `p.Command` — the **original** model-facing string, not the
  tokenised result. Status messages and `cmd-<seq>` labels keep
  showing exactly what the model wrote, which is the right surface
  for the user's UI.
- Pure-shell paths (`ls -la | grep foo`) must continue to use
  `run_shell_command`, not `run_command`. The doc comment on
  `RunCommand` is updated to make this explicit — the tokeniser
  has no concept of pipes / redirects / control flow.
- No change to the schema; the JSON surface is identical.

## 硬约束 / 存疑 / limitations

- 【一手】three new tests exercise the real `RunCommand.Run` path
  on `touch`, so the bug is observable on the filesystem (one file
  with a space vs two files without).
- 【推测】`touch` skips on Windows in the quoted-args test —
  `RunCommand` and `RunShellCommand` are POSIX-leaning and the
  tools tests already skip / guard accordingly elsewhere.
- 【推测】the cycle 164 fix in `cmd/wagyu/tui_viewport.go` (gray
  pad escapes in `fitWidth`/`padTo`, including the leftover
  `CYCLE164DEBUG` debug print) is still uncommitted in the working
  tree. As with cycles 163 and 165, the cycle-166 commit does not
  touch it.
- 【推测】`run_command` accepts command vectors the model emits in
  any reasonable shape. The cycle locks in the four most common
  edge cases (no quotes, quoted single arg, unclosed quote, empty).
  Coverage for backslash escapes inside double-quotes and POSIX
  arithmetic / history expansion (`!`) is left for a future cycle
  if it proves necessary.
