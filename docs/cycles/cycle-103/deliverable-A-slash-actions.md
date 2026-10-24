# Cycle 103 — TUI slash commands that act: `/add-dir`, `/copy`, `/logout`

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** cycle 102 gave the *content* commands real data; this cycle gives
three commands a real **effect**. All three previously printed a cycle-77
placeholder (`Added directory … (cycle 77: stub)`, `Logged out (cycle 77: stub)`,
`Copy the last assistant message … (cycle 77: stub)`).

---

## 1. `/add-dir <path>` — validate + persist 【一手】

Recovered strings (1.3.6 string table):

| Situation | Message |
|---|---|
| no argument | `error: no directory specified` |
| `stat` fails | `error: directory not found: <path>` |
| regular file | `error: not a directory: <path>` |
| success | `Workspace added: %s` |

This is the fix the **1.3.3 changelog** describes: *“Fixed `/add-dir` claiming
it added a path that doesn't exist or isn't a directory; it now reports
`directory not found` or `not a directory`.”* The clone was still making the
claim — it printed `Added directory /whatever (cycle 77: stub)` unconditionally.

The success path now appends the directory to `trustedWorkspaces` in
`~/.gemini/antigravity-cli/settings.json` via the new
`auth.AddTrustedWorkspace`, which preserves every other key and is idempotent.

**Why `trustedWorkspaces`** — the reference demonstrably keeps its session
working directories there: driving the reference over a pty adds each session
`cwd` to that array (visible in the live `settings.json`). The key's struct tag
is also recovered verbatim:
`"TrustedWorkspaces"json:"trustedWorkspaces,omitempty"`. It is the only
directory list in `settings.json`. That `/add-dir` and the cwd-trust path share
this one array is **【推测】**.

Verified live over a pty (40×120) with a throwaway `ANTIGRAVITY_HOME`:

```
> /add-dir /tmp
Workspace added: /tmp
```

```json
{
  "colorScheme": "dark",
  "trustedWorkspaces": [
    "/private/var/folders/…/T/agytui-b7c94fsd",
    "/tmp"
  ]
}
```

With no argument the TUI opens the existing directory **picker** (`Add a
directory to the workspace` / `↑/↓ Navigate · enter Select · tab Complete`), so
`error: no directory specified` is reachable only when a handler is invoked
directly.

## 2. `/copy` — real clipboard write 【一手】

Recovered:

* `Copied %s to clipboard.` with the literal **`last response`** sitting in the
  same string blob — i.e. `Copied last response to clipboard.`
* `Failed to copy to clipboard: ` for the error path.
* The clipboard implementation is the vendored
  `github.com/atotto/clipboard` — recovered symbols
  `google3/third_party/golang/github_com/atotto/clipboard/v/v0/clipboard.readAll`
  and `…clipboard.getPasteCommand`.

`copyToClipboard` therefore shells out the way atotto/clipboard does:
`pbcopy` on darwin, `wl-copy` / `xclip` / `xsel` on Linux, and an explicit error
elsewhere. `/copy` copies the most recent non-empty **assistant** message.

## 3. `/logout` — credential removal 【一手 effect / 推测 string】

`logoutFeedback` deletes `~/.gemini/antigravity-cli/antigravity-oauth-token`
(tolerating absence — logout is idempotent) and prints the recovered

```
Signed out from server: %s
```

The `%s` is **unrecovered**. It sits in a blob of auth-flow strings
(`Select available license:`, `Authentication successful!`,
`[Auth State Transition] %s`, `Continue with Google Cloud`,
`Continue with Advanced SSO`), and the token file carries no server field, so
the clone interpolates the Cloud Code endpoint host
(`daily-cloudcode-pa.googleapis.com`). The argument choice is **【推测】**; the
format string is not.

## 4. Files changed

| File | Change |
|---|---|
| `cmd/antigravity/slash_actions.go` | **new** — `addDirFeedback`, `unquotePath`, `lastResponseText`, `copyToClipboard`, `haveExec`, `authServerName`, `logoutFeedback`, injectable `clipboardWrite` |
| `cmd/antigravity/slash_commands.go` | `/add-dir`, `/copy`, `/logout` wired to the real actions |
| `cmd/antigravity/slash_actions_test.go` | **new** — 6 tests |
| `internal/auth/v135_compat.go` | `AddTrustedWorkspace` |

## 5. Verification

* `go build ./cmd/antigravity` — clean; `go vet` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* `harness136.sh` — **86/87** unchanged (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** unchanged (both diffs documented in cycle 101).
* pty probe — `/add-dir /tmp` renders `Workspace added: /tmp` and updates
  `settings.json`; `/add-dir` with no argument opens the picker.
* `/copy` and `/logout` are tested with `t.Setenv("ANTIGRAVITY_HOME", …)` and an
  injected clipboard writer.

## 6. Divergences and limitations

* **`/copy` is not tested against the real system clipboard.** The handler is
  exercised through the injectable `clipboardWrite`; a real `pbcopy` write is
  deliberately avoided in tests so the developer's clipboard is not clobbered.
  `pbcopy`/`pbpaste` exist on this machine (`/usr/bin/…`).
* **`/logout`'s `%s` is 【推测】** — see §3.
* **`/add-dir` path normalisation is 【推测】.** The argument is resolved with
  `filepath.Abs` before `stat` and before being recorded, so a relative
  argument lands in `settings.json` as an absolute path. `~` is **not**
  expanded. One layer of matching quotes is stripped.
* **`/add-dir` persistence target is 【推测】** — see §1.
* **A settings write failure reports `error: <err>`**, a shape the recovered set
  does not cover (the three recovered `error:` lines are the validation ones).
* **`/copy` with nothing to copy prints nothing** — the reference has no
  recovered message for that case.
* **Discovered gap, not fixed here:** the reference adds the *session working
  directory* to `trustedWorkspaces` on startup; the clone does not. The probe
  above shows the entry arriving from `WriteV135CompatDefaults` only on first
  run, not per session. Worth a later cycle.
* **Still cycle-77 stubs (~18):** `/diff`, `/mcp`, `/tasks`, `/keybindings`,
  `/statusline`, `/voice`, `/remote-control`, `/resume`, `/rewind`, `/fork`,
  `/rename`, `/title`, `/feedback`, `/artifact`, `/btw`, `/codesearch`,
  `/plugin`, `/open`. `/diff` has recovered vocabulary
  (`Loading diff`, `[%d-%d of %d]  (end of diff)`, `git diff: %w`,
  `No diff was generated due to an internal error. Try again.`) but needs a real
  viewer.
