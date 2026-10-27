# Cycle 342 deliverable: `/browser` and `/schedule` do the work, and a configured model is selectable by its bare id

**Cycle:** `cycle-342-browser-slash-command`
**Subject:** a user report — "the CLI cannot reach any model, even though I
have quota" — and the two real bugs behind it.
**Method:** the report's own evidence (the reference's slash menu, the
cycle-194 captures) plus live runs of the clone's TUI under `tmux`.

---

## 0. The short answer

Two separate defects, both of which end in the same sentence on the user's
screen:

| # | defect | what the user saw | what happens now |
|---|---|---|---|
| 1 | `/browser` and `/schedule` printed a **hard-coded** quota block | `⚠ Individual quota reached. … Resets in 0h0m0s.` + `Error ID: <uuid>-1` | `/browser <task>` runs the browser agent; `/schedule <instruction>` sends the instruction to the model |
| 2 | the TUI could not resolve the **bare id** of a configured model that declares tiers, and fell back to the reference's default model | `⚠ "minimax-m3.1-flash" is no longer available. Using "Gemini 3.8 Flash (High)".` — and the request went to the CloudCode account, whose quota is the one that runs out | the bare id resolves to the model's default tier, and the request goes to the configured endpoint |

Defect 1 is the one the report is about: it is a **false statement about the
reader's own quota**, printed by the clone itself, with no request made and
no quota checked.

This cycle also closed the last **browser** gap against the reference: the
four flags the reference hides for its browser (`cdp_port`,
`local_chrome_headless`, `local_chrome_user_data_dir`, `use_local_chrome`)
now exist in the clone with the same names, defaults and hidden state, and
they decide what the browser does — including whether it gets a **window**
(§3.6).

---

## 1. What the clone actually printed

Cycle 194 replaced two stub bodies with a locally generated error. The
source (before this cycle, `internal/cliapp/slash_commands_handlers.go`):

```go
func (m *runMode) slashBrowser(args string) (bool, tea.Cmd) {
	errID, err := newUUIDv4()
	if err != nil {
		m.slashRespond("/browser", "Failed to generate error ID: "+err.Error())
		return true, nil
	}
	quota := panelGray166 +
		"⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 0h0m0s." +
		panelGray166C
	errLine := panelGray241 + "Error ID: " + errID + "-1" + panelGray241C
	m.slashRespond("/browser", quota, errLine)
	return true, nil
}
```

Nothing in that path talks to a quota, an account or a model. The UUID is
`convstore.NewUUIDv4()` — a fresh random value — and the "Resets in 0h0m0s"
is a literal. The row is **not** an error the server returned; it is a
sentence the clone wrote.

Why it was written that way is on the record: cycle 194's report says the
reference's capture of `/schedule` and `/browser` showed "model output that
ultimately surfaces an `Error ID: <uuid>-1` row", and the clone "generates it
locally so the clone doesn't burn model quota on a guaranteed error"
【一手 — the capture; the quote is cycle 194's own wording】.

That reasoning is sound and the conclusion is wrong. The capture was taken
while **the reference account's** quota was exhausted, so what it holds is
the *failure* of the command, not the command. Reproducing the failure
makes the clone assert, forever and for every user, that the reader's quota
is spent. The reader has no way to tell that apart from a real quota
error — the text is the reference's own text.

Cycle 167 had it right the first time: `/browser` answered "Browser agent:
not available in openagy." — an honest, if empty, statement. The agent
exists now (cycle 311), so the honest answer is the agent's answer.

## 2. What the reference's command is

The reference's own slash menu describes it:

> **/browser** — Invoke a browser agent for web tasks.

【一手 — `internal/tuilib/commands.go`, the catalogue recovered from the
v1.3.5 binary's slash picker (`/tmp/scan_*.bin` captures)】

So the command **starts a browser agent**. The clone has one:
`internal/browsersubagent`, the agent behind the `browser_subagent` tool
(cycle 311), whose tool set is the clone's twenty-three browser tools.

【推测】 that the reference's `/browser` starts *that* agent rather than
asking the parent model to call the tool: the capture shows model output
under the command, and a browser agent is a model — but the reference's own
dispatch is not recoverable from the captures this repository holds. The
clone's choice (call the agent directly) is the one the description names,
and it does not spend the parent model's quota to decide to spend it.

`/schedule` has the same shape in the reference's capture — model output
under the command — and the clone carries the `schedule` tool (one of the
reference's twelve wire declarations), so the command is a **prompt-sending**
one, like `/plan` and `/goal`.

## 3. What the clone does now

### 3.1 `/browser <task>`

`internal/cliapp/slash_browser.go` (new):

- the task is the command's argument. Without one the command says how it
  is used: `Usage: /browser <task>` — the same shape as `/btw` and `/open`;
- the command builds **its own** browser agent
  (`browsersubagent.New(...)` over a fresh `coding.CreateAgentSessionServices`),
  because the runner the `browser_subagent` *tool* uses is installed per
  turn and that turn's session services are closed when the turn ends
  (`internal/cliapp/tui_runmode_agent.go`);
- the run happens off the UI goroutine — a browsing task can take minutes —
  and the answer comes back as `browserAgentDoneMsg`, handled in
  `internal/cliapp/tui_update_stream.go`;
- the agent's final text is the answer, printed as plain rows under the
  command's echo, the way a model's answer is printed.

The failure path is honest too: no browser, no session, no model — each
produces its own reason, and the reason is the reason.

### 3.2 `/schedule <instruction>`

`internal/cliapp/slash_commands_handlers.go`:

- no instruction → `Usage: /schedule <instruction>`;
- with one → the instruction plus a synthesized prompt
  (`schedulePrompt()`) go to the model, which registers the schedule
  through the `schedule` tool — the same path `/plan` and `/goal` take.
  The prompt's wording is the clone's own, like the other prompt-sending
  commands' (the reference's own text is not recoverable).

### 3.3 The browser starts by itself

The clone used to refuse to touch a browser without an opt-in
(`PIGASUS_ENABLE_BROWSER`), and `/browser` printed that refusal. The
reference does not work that way — it says so in its own words, in the
`chrome-devtools-mcp` skill it ships:

> **Browser lifecycle**: Browser starts automatically on first tool call
> using a persistent Chrome profile.

【一手 — `out/ide-language-server/skills/chrome-devtools/SKILL.md`】

and the language server starts that MCP server itself
(`--use_ls_chrome_devtools_mcp`, default `true`;
`--use_local_chrome` / `--cdp_port` = `false` / `9222` are the *opt-in*
to a local Chrome) 【一手 — `out/ide-language-server/README.md`】.

So the reference's browser needs no opt-in: the first tool call starts
Chrome, with a persistent profile. The clone now does the same:

| | |
|---|---|
| no flag | the client starts its own Chrome — headless, with a persistent profile under the user's cache dir |
| `--use_local_chrome` | the Chrome that is already running on this machine is the one that gets driven |
| `--local_chrome_headless=false` | the Chrome the client starts gets a **window** |
| `--local_chrome_user_data_dir=<dir>` | the profile the started Chrome uses |
| `--cdp_port=<n>` | the port of the local Chrome |
| `CHROME_REMOTE_DEBUGGING_PORT` | the clone's own override: attach to that port |
| `CHROME_BIN` | the clone's own override: the binary to start |
| no Chrome binary at all | `no Chrome browser is available: install Google Chrome, or set CHROME_BIN to the browser binary, or CHROME_REMOTE_DEBUGGING_PORT to attach to a running one` |

`PIGASUS_ENABLE_BROWSER` is gone: the browser is not a switch any more.

The browser the clone starts stops with the clone — `cmd/pigasus` calls
`browsercdp.Shutdown()` before it exits, so no headless Chrome is left
behind holding the profile and the port. (Before this, the launched
Chrome *and* its temporary profile directory outlived every run.)

§3.6 has the four flags themselves, and where they come from.

### 3.4 The command checks the browser before it asks the model

Two things were found while running the command for real, and both are
about the *failure* path — the path nobody looks at until it happens.

**The agent retries tools that cannot work.** Without a browser every
browser tool fails, and the browser agent — which only knows that *a tool
failed* — tries another one, and another. The run does not end: it burns
the model's quota on a dozen calls that cannot succeed. Measured on this
machine: the command was still running after **two minutes**, calling
`open_browser_url`, `list_browser_pages`, `capture_browser_console_logs`
and `browser_get_dom` in turn.

The command therefore checks the browser **before** it starts the agent
(`browserProbe` → `browsercdp.CheckBrowser`): the check answers in a
second instead of after two minutes of doomed calls. It starts nothing —
the browser starts on the first tool call, the reference's rule — so the
check only finds out whether a browser *could* work:

```
> /browser
  ⎿  The browser agent is not available: no Chrome browser is available: install Google
Chrome, or set CHROME_BIN to the browser binary, or CHROME_REMOTE_DEBUGGING_PORT to attach
to a running one
```

**The browser tools wrote to stderr.** `browserStubBase.reject` printed
`[<tool>] <reason>` to `os.Stderr` before returning the error. In the
alt-screen TUI the terminal owns the screen, so those lines landed **on
top of the rendered frame** — the answer, the input box's border and the
status line were drawn over each other. The error was returned to the
caller anyway, so the write bought nothing and cost the screen.

The write is gone: the returned error is the only channel, and it names
the tool that failed (`fmt.Errorf("%s: %w", name, browserUnavailable)` —
the sentinel is still reachable through `errors.Is`).

### 3.5 A bound on the run

`/browser` is bounded at ten minutes (`browserAgentTimeout`). The
reference's own cap is not recoverable; the clone's rule is that a slash
command which never returns is a defect in the TUI. A run that hits the
bound says so.

### 3.6 The browser's four flags — the last 1:1 gap

The clone drove the browser with its own rules. The reference has four
flags for it, and they are **hidden**:

| # | flag | state |
|---|---|---|
| 4 | `cdp_port` | hidden |
| 5 | `local_chrome_user_data_dir` | hidden |
| 6 | `local_chrome_headless` | hidden |
| 7 | `use_local_chrome` | hidden |

【一手 + 反编译 — `out/local-macos/FINDINGS.md` §2: the reference's
`main.hideFlagsForExternal` sets the `Hidden` bit on exactly these
names. The report calls the cluster "local-Chrome CDP control"】

Their types, defaults and usage strings are the reference's too, and they
are in the 1.3.4 strings dump: *"Port for Chrome DevTools Protocol"*,
*"Chrome user data directory."*, *"Whether Chrome runs in headless
mode."*, *"Whether to use local chrome"* 【strings】. The language server
carries the same four, with the same defaults
(`out/langserver/flags.txt`: `-cdp_port=9222`, `-local_chrome_headless=true`,
`-use_local_chrome=false`) 【一手】.

So the clone registers them with the same names, types, defaults and usage
strings (`internal/cli/parser_flags.go`), hidden like the reference's, and
its browser client reads them (`internal/browsercdp.Config`, wired in
`internal/cliapp/browser_flags.go`):

| flag | what the clone does with it |
|---|---|
| `--cdp_port=9222` | the port of the local Chrome |
| `--local_chrome_headless=true` | the Chrome the clone starts runs headless; `=false` gives it a **window** |
| `--local_chrome_user_data_dir=""` | the profile the started Chrome uses; empty keeps the clone's own persistent profile |
| `--use_local_chrome=false` | `true` drives the Chrome that is already running on this machine |

The reference's visible window is the opt-in, and its own skill says so:

```bash
chrome-devtools start --headless=false # Start with visible browser window
```

【一手 — `out/ide-language-server/skills/chrome-devtools-cli/SKILL.md`】

so the clone's visible window is `pigasus --local_chrome_headless=false`.

**What the reference launches.** The 1.3.4 strings carry the launch
arguments and the error strings of that path: `--headless=new`,
`--no-first-run`, `--no-default-browser-check`, `--user-data-dir=`,
`--remote-debugging-port=0`, `about:blank`, `LaunchBrowser: %w`,
`Failed to open browser: %v`, and the profile name
`agy_encoder_chrome_profile` 【strings】. Port `0` means Chrome picks a
free one — and the reference then finds it, because Chrome writes the
port into the profile's `DevToolsActivePort` file, which is also what
the language server's `CheckDevToolsActivePort` method is about
【strings】.

The clone now launches the same way:

```
--no-first-run --no-default-browser-check --remote-debugging-port=0
--user-data-dir=<the profile> [--headless=new] about:blank
```

and reads the port out of the `DevToolsActivePort` file. Two consequences:
a Chrome that is already running on 9222 no longer collides with the
clone's own, and the launch arguments are the reference's, character for
character.

The clone's own name for the profile is `pigasus/browser-profile` under
the user's cache dir; the reference's is `agy_encoder_chrome_profile`
(the clone does not write into the reference's layout).

## 4. The second bug: a configured model was not selectable by its bare id

Found while verifying the first fix, and it is the same user-visible
sentence in a different place.

`modelcatalog.ResolveEffectiveModel` resolves the requested model against
the catalogue (`cli.ModelCatalogue()`), and a model the catalogue does not
know falls back to the reference's default model with the warning
`"<requested>" is no longer available. Using "Gemini 3.8 Flash (High)".`

A configured model that declares reasoning tiers is listed in the catalogue
**once per tier** — `minimax-m3.1-flash-low` … `minimax-m3.1-flash-max` — so
its bare id, `minimax-m3.1-flash`, is not a row of its own. The catalogue
lookup therefore missed it, the TUI fell back to `Gemini 3.8 Flash (High)`,
and — this is the part that matters — **the request went to the CloudCode
account instead of the configured endpoint**. The quota that then ran out was
the Google account's, not the reader's.

The guide already promised the right behaviour
(`docs/guide/models.md` → *The resolution*):

| The user writes | The catalogue id |
|---|---|
| `--model minimax-m3.1-flash` | `minimax-m3.1-flash-<the default tier>` |

so this was a gap between the documentation and the TUI, not a new rule.

The fix is `cli.ConfiguredModelDefaultLabel` (the bare id → the label of the
model's default tier) consulted by `modelcatalog.CanonicalModelLabel`
after the catalogue rows and before the fallback. The reference resolves a
bare id the same way — its own default model's id, `gemini-3.8-flash`,
answers `Gemini 3.8 Flash (High)` 【一手 — the clone's special case for the
reference's own catalogue, which the reference's `--model` accepts the same
way】.

The **print** path never saw the bug: it uses the raw `--model` value and
lets the registry's tier-aware lookup do the work, so
`pigasus --model minimax-m3.1-flash --print …` always talked to the
configured endpoint. The two paths disagreed about the same flag.

## 5. Live evidence (this machine, `tmux`)

The TUI under `tmux`, the user's own `providers.json` (`agw` →
`https://agw.k0s.io/v1/chat/completions`, model `minimax-m3.1-flash`):

**Before** — `--model minimax-m3.1-flash`:

```
⚠ Warning
  ⎿  "minimax-m3.1-flash" is no longer available. Using "Gemini 3.8 Flash (High)".
…
auto · Gemini 3.8 Flash · high
```

**After** — the same command:

```
…
auto · MiniMax M3.1 Flash · high
```

**After** — `/browser open https://example.com and report the page title`
(no opt-in, no environment variable — the browser starts itself):

```
> /browser
  ⎿  Running the browser agent…

I'll open the page and check its title.The page title of https://example.com is **"Example Domain"**.
```

**Before** (the same command, while the clone still demanded an opt-in):

```
> /browser
  ⎿  The browser agent is not available: browser tools require Chrome DevTools Protocol: set
PIGASUS_ENABLE_BROWSER=1 to launch a headless Chrome, CHROME_REMOTE_DEBUGGING_PORT to attach to
a running one, or CHROME_BIN to name the binary
```

**After** — `/browser` (no task):

```
> /browser
  ⎿  Usage: /browser <task>
```

**After** — the same command with the reference's own visible-window flag
(`--local_chrome_headless=false`), the Chrome the clone starts has a
window (its process carries no `--headless=new`):

```
$ ps aux | grep '[u]ser-data-dir=/Users/gear/Library/Caches/pigasus/browser-profile'
gear 8146 … /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --no-first-run
  --no-default-browser-check --remote-debugging-port=0
  --user-data-dir=/Users/gear/Library/Caches/pigasus/browser-profile about:blank

> /browser
  ⎿  Running the browser agent…

I'll open the page and check the title.The page title is **"Example Domain"**.
```

**After** — the same command with `--use_local_chrome` and a Chrome
already listening on 9222: the clone drives **that** Chrome and starts
none of its own (the count of the clone's own Chrome processes is 0):

```
> /browser
  ⎿  Running the browser agent…

I'll open the URL and read the page title.The page title is **"Example Domain"**.
```

**After** — the clone's exit leaves the Chrome it *attached* to alone
(it is not the clone's to kill), and kills the one it *started*:

```
$ ps aux | grep -c '[u]ser-data-dir=/Users/gear/Library/Caches/pigasus/browser-profile'
0
```

**After** — `/schedule remind me to check the build in 10 minutes`:

```
> /schedule

● Scheduled Reminder: check the build (run go build ./cmd/... and/or go … (ctrl+o to expand)

  Registered.

  • Type: one-time timer, 10 minutes
  • First run: 2026-10-27 13:59 local time
  • Action: reminder to check the build (build the Go commands / run the test suite in openagy)
```

## 6. What changed

| | |
|---|---|
| `internal/cliapp/slash_browser.go` | **new** — the `/browser` command, the browser agent it builds, `browserAgentDoneMsg` and its transcript handling |
| `internal/cliapp/slash_browser_test.go` | **new** — the command's tests, and the guard that no slash command claims a quota failure |
| `internal/cliapp/slash_commands_handlers.go` | `/browser` → the browser agent; `/schedule` → a prompt to the model |
| `internal/cliapp/slash_commands.go` | `schedulePrompt()`, and the file's header no longer claims the two commands answer with an "Error ID" row |
| `internal/cliapp/tui_update_stream.go` | handles `browserAgentDoneMsg` |
| `internal/cliapp/slash_helpers.go` | `newUUIDv4` deleted — the two stubs were its only callers |
| `internal/cliapp/slash_panels.go` | `panelGray166`'s comment now names what uses it (the failing `/browser` run's warning row) |
| `internal/cli/model_catalogue.go` | **new** `ConfiguredModelDefaultLabel` — a configured model's bare id → its default tier's label |
| `internal/modelcatalog/resolve.go` | `CanonicalModelLabel` consults it, so the bare id no longer falls through to the reference's default model |
| `internal/browsercdp/client.go` | the browser starts by itself (attach, or start one with a persistent profile); `CheckBrowser` answers "could a browser work?" without starting one; `Shutdown` stops the browser with the clone |
| `internal/browsercdp/config.go` | **new** — `Config`, the reference's four flags' shape, and the local-Chrome discovery (`DevToolsActivePort`) |
| `internal/browsercdp/config_test.go` | **new** — the config's normalisation, the discovery order, the attach rule |
| `internal/cli/parser_flags.go` | the reference's four hidden browser flags (`cdp_port`, `local_chrome_headless`, `local_chrome_user_data_dir`, `use_local_chrome`) |
| `internal/cli/parser_test.go` | the flag count grows by the four hidden ones; the visible half is unchanged |
| `internal/cliapp/browser_flags.go` | **new** — the four flags → the browser client's policy |
| `internal/cliapp/browser_flags_test.go` | **new** — the mapping, and the defaults |
| `internal/cliapp/run.go`, `internal/cliapp/tui_entry.go` | the policy is read on both paths into the TUI |
| `cmd/pigasus/main.go` | calls `browsercdp.Shutdown()` before it exits, so no headless Chrome is left behind |
| `internal/tools/browser.go` | the browser tools no longer write to stderr — the TUI owns the screen |
| `internal/tools/tools_test.go` | the browser assertion no longer touches the machine's Chrome and no longer assumes the machine has no browser |
| `devutils/browserdemo/` | **new** — drives one browsing task in the browser agent from the command line |

The obsolete cycle-194 tests (`TestRunSchedule_ErrorID`,
`TestRunBrowser_ErrorID`, `TestRunScheduleAndBrowser_ErrorIDsAreUnique`,
`TestRunSchedule_TwoLineError`, `TestRunBrowser_TwoLineError` and the
`quotaRe` pattern) are deleted: they pinned the fake quota text.

## 7. 硬约束 (hard constraints)

- **A model the user configured must never fall through to the reference's
  catalogue.** The fallback sends the request to the CloudCode account, and
  the account's quota is the one the user did not ask about. The catalogue
  is the merge of the reference's rows and the user's rows
  (`cli.ModelCatalogue()`); a configured model's bare id is now a row of the
  user's half.
- **The clone must not print a quota state it did not measure.** The quota
  block is gone from the code, and
  `TestSlashCommands_NeverClaimAQuota` fails if it comes back.
- **A slash command that cannot do its work says why.** "Usage: …", "The
  browser agent is not available: …" — never a foreign error.
- **The browser starts by itself.** The reference's browser surface starts
  Chrome on the first tool call, with a persistent profile
  ("Browser starts automatically on first tool call using a persistent
  Chrome profile" 【一手】), so the clone does the same and has no opt-in.
  `CHROME_BIN` and `CHROME_REMOTE_DEBUGGING_PORT` are overrides, not
  switches.
- **The browser stops with the clone.** `cmd/pigasus` calls
  `browsercdp.Shutdown()` before it exits; a headless Chrome left behind
  would hold the profile and the port. A Chrome the clone only *attached*
  to is not the clone's to kill, and it survives.
- **The reference's four hidden browser flags must keep their names, their
  types, their defaults and their hidden state.** `cdp_port`,
  `local_chrome_headless`, `local_chrome_user_data_dir` and
  `use_local_chrome` are the reference's own 【一手 + 反编译】. A visible
  `--help` column that grew by four rows would break the parse-parity
  contract with the reference's `--help`.
- **A visible browser window is opened by `--local_chrome_headless=false`
  and by nothing else.** The reference's visible mode is the opt-in
  ("chrome-devtools start --headless=false" 【一手】), and the headless
  default is the reference's own (`local_chrome_headless` defaults to
  true).
- **The clone's launch arguments are the reference's.** `--headless=new`
  (only when headless), `--no-first-run`, `--no-default-browser-check`,
  `--remote-debugging-port=0`, `--user-data-dir=<profile>`, `about:blank`
  【strings】 — the port is `0` on purpose: Chrome picks a free one and
  writes it into the profile's `DevToolsActivePort` file, which is how
  the reference finds it too.
- **The `/browser` agent builds its own runner.** The tool's runner belongs
  to the turn that installed it, and that turn's services are closed when
  the turn ends.
- **A tool must not write to the terminal.** The TUI owns the screen: a
  write to `os.Stderr` from a tool lands on top of the rendered frame.
  Errors travel through the return value.
- **A command checks its precondition before it spends the model's quota.**
  The browser check answers in a second; the agent's retry loop took
  minutes and never ended.

## 8. 存疑 (open questions)

- 【推测】 The reference's `/browser` starts the browser agent directly; the
  captures only show model output under the command. If the reference
  actually asks the parent model to call the `browser_subagent` tool, the
  clone's behaviour differs in one visible way: the reference's `/browser`
  would spend the parent model's quota, the clone's spends the same model's
  quota (the agent runs on the session's model) but skips the parent's
  *decision*. No capture settles this.
- 【推测】 The reference's `/schedule` reaches the scheduler through a model
  call rather than through a client-side registration. The clone's shape
  (a prompt to the model, which then calls the `schedule` tool) fits the
  capture, and the clone's `schedule` tool has no scheduler backend
  either way — it writes the log file the reference names.
- The reference's `/browser` **without** a task is unknown. The clone
  answers `Usage: /browser <task>`, the house shape for a command that
  needs an argument (`/open`, `/btw`).
- The reference's own behaviour for a model the user configured with tiers
  is outside the reference's world (the reference has no `providers.json`);
  the clone's rule — the bare id means the model's default tier — is the
  one the guide already documented.

## 9. Limitations

- The live evidence is **this machine**: macOS, Chrome 155, the user's own
  `agw` gateway, one configured model. The reference binary was not run in
  this cycle — the reference-side evidence is the slash-menu catalogue and
  cycle 194's captures.
- `/browser` was verified against `https://example.com`. A page that needs
  a click, a form or a login exercises more of the agent's twenty-three
  tools, and none of that was measured here.
- The browser agent's **answer** is the model's final text, and the model
  may prefix it with its own prose ("I'll open the page and check the
  title." above). The clone prints the text as it is; it does not trim the
  preamble.
- `/schedule` registers a schedule in the clone's own log file; the clone
  has no scheduler backend, so a registered timer does not fire (the same
  limit the `schedule` **tool** has had since it landed).
- The **ten-minute bound** on a `/browser` run and the **browser check**
  are the clone's own policy. The reference's own bounds are not
  recoverable from the captures this repository holds. The check starts
  nothing, but the agent's first tool call starts the clone's own Chrome
  when no local Chrome is being driven — the reference's rule, and the
  reason a `/browser` run leaves a browser behind on the machine (it
  stops with the clone, but it is there while the clone runs).
- The reference's own behaviour when a tool fails inside an agent is
  unknown. The clone's browser agent keeps trying new tools after a
  failure; the check keeps that path from being reached in the one case
  that is certain (no browser at all), and nothing else.
- **The exact mapping of the four flags onto the reference's browser
  startup is a reading, not a decompilation.** The reference's flags, their
  defaults and their usage strings are recovered; the function that turns
  them into launch arguments was not decompiled. The clone's reading: the
  reference *launches* its own Chrome by default (`LaunchBrowser: %w`,
  `--headless=new`, `--remote-debugging-port=0` 【strings】) and drives
  the **running** one when `--use_local_chrome` is set (the reference's
  discovery path — `[CDP Discovery] Failed: %v`, the language server's
  `CheckDevToolsActivePort` method 【strings】). If the reference turns out
  to launch the local Chrome itself as well, the clone's `--use_local_chrome`
  would have to launch it instead of attaching to it.
- `--cdp_port` is the port of the **local** Chrome in the clone's reading.
  It is not used for the Chrome the clone starts: that one gets an
  ephemeral port (`0`), so a Chrome already listening on 9222 cannot
  collide with it. The reference's own string is
  `--remote-debugging-port=0`, so the reference's launched Chrome is
  ephemeral too — but whether the reference's `cdp_port` also reaches that
  launch (as `--remote-debugging-port=9222`) was not recovered.
- The clone's `DevToolsActivePort` discovery reads the standard Chrome
  profile dirs (`~/Library/Application Support/Google/Chrome` on macOS).
  A Chrome started with a debugging port writes the file there; a Chrome
  started without one does not, and then the clone falls back to
  `--cdp_port`.
- `internal/tools`'s browser assertion points the client at a closed port
  with a binary that cannot be run and a scratch home directory, so the
  test starts no browser, does not drive the user's own Chrome and does
  not pick the user's `DevToolsActivePort` file up. It pins that every
  browser tool answers — a result or an error — and no longer pins
  *which* error.
- `devutils/browserdemo` spends the configured model's quota when it runs,
  and it starts a headless Chrome when no Chrome is listening.

## Files

| | |
|---|---|
| `internal/cliapp/slash_browser.go` | the `/browser` command and its browser agent |
| `internal/cliapp/slash_browser_test.go` | its tests, and the quota guard |
| `internal/cli/model_catalogue.go` | `ConfiguredModelDefaultLabel` |
| `internal/modelcatalog/resolve.go` | `CanonicalModelLabel` consults it |
| `internal/browsercdp/client.go` | the browser starts by itself; `CheckBrowser`, `Shutdown` |
| `internal/browsercdp/config.go` | **new** — `Config` (the reference's four flags), the `DevToolsActivePort` discovery |
| `internal/browsercdp/config_test.go` | **new** — the config's normalisation, the discovery order, the attach rule |
| `internal/cli/parser_flags.go` | the reference's four hidden browser flags |
| `internal/cli/parser_test.go` | the flag count grows by four; the visible half is unchanged |
| `internal/cliapp/browser_flags.go` | **new** — the four flags → the browser client's policy |
| `internal/cliapp/browser_flags_test.go` | **new** — the mapping, and the defaults |
| `internal/cliapp/run.go`, `internal/cliapp/tui_entry.go` | the policy is read on both paths into the TUI |
| `internal/tools/tools_test.go` | the browser tools run against a scratch home, so the discovery finds nothing |
| `cmd/pigasus/main.go` | stops the browser with the clone |
| `internal/tools/browser.go` | the browser tools no longer write to stderr |
| `devutils/browserdemo/main.go` | the browser agent from the command line |
| `docs/cycles/cycle-194/deliverable-class-d-behavioral.md` | the report that introduced the fake quota block — **superseded by this one** |
| `docs/guide/tools.md` | the browser section: the `/browser` command, the browser's own lifecycle |
| `docs/guide/models.md` | *The resolution*: the bare id means the default tier |
