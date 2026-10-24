# Cycle 108 — `/open` opens a file in the configured editor

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** a second cycle-77 stub replaced with a genuine feature. `/open`
previously printed `Open: <arg> (cycle 77: stub)`; it now resolves the
configured editor and actually opens the file.

---

## 1. Recovered evidence 【一手】

| String | Role |
|---|---|
| `Usage: /open <file_path>` | no argument |
| `File not found: %s` | the path does not exist |
| `Opening file: %s` | about to hand the file to the editor |
| `No editor configured; using %s` | neither `editor` nor `$EDITOR` is set |
| `Failed to resolve editor: %v` | the editor could not be found |
| `Editor exited with error: %v` | the editor returned non-zero |
| `auto ($EDITOR)` | the settings label for the default `editor` value |
| `editor` (default `"auto"`) | the settings key, from `agy -p /config` |

The description is *“Open a file or view opened/edited files”*.

## 2. Implementation

* `resolveEditor(configured)`:
  * `""` or `auto` → `$EDITOR`, split into name + arguments;
  * `""` or `auto` with no `$EDITOR` → `vi`, **plus** the recovered notice
    `No editor configured; using vi`;
  * anything else → that command, split the same way.
* `openFeedback(arg, configuredEditor, workspace)`:
  * empty argument → `Usage: /open <file_path>`;
  * relative paths resolve against the workspace;
  * a missing path → `File not found: <abs path>`;
  * an editor that is not on `PATH` → `Failed to resolve editor: <err>`;
  * otherwise `Opening file: <abs path>` (prefixed by the notice when there is
    one) and an `*exec.Cmd` with `Dir` set to the workspace.
* The router hands the command to `tea.ExecProcess`, so the TUI suspends, the
  editor owns the terminal, and on return a new `editorFinishedMsg` produces
  `Editor exited with error: <err>` when the process failed.

Quoting is handled by cycle 103's `unquotePath`, so `"/a path/with spaces"` works.

## 3. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* New tests: `TestResolveEditor` (fallback, `$EDITOR` with arguments, explicit
  command), `TestOpenFeedback` (usage, missing file, success, quoted path, bad
  editor), `TestSlashOpen_Router` (router wiring and the scheduled command) and
  `TestEditorFinishedMsg` (the post-editor report). The editor process is never
  actually spawned in tests.
* Live pty probe with `EDITOR=true` (so the editor exits immediately):

  ```
  > /open
  Usage: /open <file_path>
  > /open nope.txt
  File not found: /private/var/folders/…/agytui-btlnwwim/nope.txt
  > /open secret.txt
  Opening file: /private/var/folders/…/agytui-btlnwwim/secret.txt
  ```

* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **21/25**, of which two are the documented cycle-101 diffs
  (`usage_slash`, `disable_slash`) and two are **reference timeouts**
  (`effort_bogus` and `skills_slash` both `124/…`, i.e. the reference did not
  answer within 25 s). Effectively 23/25, as in cycles 105–107.

## 4. Divergences and limitations

* **`/open` with no argument prints the usage line**, but the reference *views
  the list of opened/edited files* (per the description). The clone has no such
  list, so the recovered usage string is the honest substitute.
* **The `vi` fallback is 【推测】.** The reference's fallback when neither
  `editor` nor `$EDITOR` is set is unrecovered.
* **The `editor` setting is read but never written.** `/config` renders the key;
  there is still no settings editor (cycle 104's limitation).
* **`editorMode` is ignored.** The recovered settings key `editorMode` (default
  `default`) presumably selects a vim/emacs keybinding profile *inside* the
  built-in editors; `/open` shells out to an external editor either way.
* **`Opening file:` shows an absolute path.** Whether the reference prints the
  argument as typed or the resolved path is unrecovered.
* **Still cycle-77 stubs (~8):** `/keybindings`, `/voice`, `/resume`, `/rewind`,
  `/fork`, `/feedback`, `/artifact`, `/btw`, `/codesearch`, `/context`,
  `/plugin`. `/context` (`Token usage by category`, `Agent responses`,
  `System overhead`, `System messages`, `Knowledge files`,
  `%s Free space: %s (%.1f%%)`) and `/codesearch`
  (`Usage: /codesearch [-F|--literal] <query>`, `Searching for %q...`) are the
  next cheapest; `/keybindings` shares `/open`'s editor machinery
  (`Set custom keybindings`, `No keybindings found.`, `keybindings.json`).
