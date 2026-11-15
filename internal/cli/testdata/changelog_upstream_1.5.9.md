# The upstream 1.5.9 release notes (【一手】)
#
# Source: the vendor's own repository —
# https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#159
# (raw: https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md),
# fetched 2026-11-14. The section below is copied verbatim.
#
# WHY THIS FILE EXISTS: the *served* changelog feed — the one `agy changelog`
# prints and the one internal/cli/testdata/changelog_reference.txt captures —
# lags the vendor's own repository, so the 1.5.9 section does not reach the
# clone through the feed. The notes below are the vendor's own statement of
# what 1.5.9 changed; they are evidence about the release, not a capture of
# the feed.

## 1.5.9

- Added the `run_workflow` tool for everyone: the agent can run a Python workflow script that fans work out to many subagents in parallel or in stages and then combines their results. It uses the Python bundled with the CLI, so you don't need Python installed.
- Added a `pluginControls` section to managed admin settings, so organizations that don't use Gemini Enterprise can control plugins too: `pluginsDisabled` stops all plugins from loading, including built-in ones, and blocks installing or enabling them, while `marketplaceDisabled` turns off the plugin marketplace but keeps installed plugins working. Gemini Enterprise users keep following their organization's Cloud Console policy.
- Improved Auto mode's review of commands that would leave the terminal sandbox, so it decides faster and times out less often: the reviewer can now search the conversation, list directories, and search files directly instead of starting a sandboxed command for each check.
- Improved `/resume`: the conversation picker no longer fetches the full history of every open conversation, so it no longer stalls on "Loading conversations..." in long sessions, and `/resume <id>` now accepts any prefix that matches exactly one conversation, such as the short ID shown in the status line, and reports `conversation <id> not found` for an unknown ID instead of silently opening the picker.
- Improved `/browser` subagent session recordings, which are now `.mp4` files made with Chrome's built-in screen recording instead of `.webm`.
- Fixed a security issue where custom headers configured for an HTTP MCP server, such as API keys, could be forwarded to a different host when the server responded with a redirect; they are now sent only to the configured server's own origin.
- Fixed the terminal UI accepting malformed or implausible window-size reports, such as negative or extremely large dimensions from stray escape sequences, which could make the CLI freeze or use excessive memory; these reports are now ignored.
- Fixed several `agy acp` issues: a session no longer fails with "Authentication required" after `agy acp --login` when `settings.json` names an auth method that has no credential (such as `gemini-api-key` without `GEMINI_API_KEY`), because the CLI now falls back to your saved login; prompts no longer fail when the ACP client passes an MCP server whose name contains spaces or other special characters; a failed turn now reports its specific error, such as a model or quota error, instead of "Agent execution terminated due to error."; and the diffs sent for completed edits no longer carry an extra trailing newline in `oldText` and `newText`.
- Fixed more cases of files saved with a UTF-8 byte order mark (BOM) being mishandled: `"modelProvider"` in `settings.json` is now honored on the first run, `agy acp` now loads your settings and authentication options instead of falling back to defaults, and `agy plugin validate` now runs all of its checks on such a `hooks.json` and reports the real syntax error and line instead of blaming the BOM.
- Fixed `-i`/`--prompt-interactive` together with `--remote-agent` and `--conversation` or `--continue`: the prompt is now sent to the resumed conversation after its history loads, instead of starting a new conversation, and it is handled like typed input, so it shows right away, waits behind a running turn, and reports a failed send.
- Fixed `!` shell commands on Windows: PowerShell no longer drops double quotes, so commands like `! git commit -m "two words"` run exactly as typed, and quitting the CLI or killing the task in the tasks panel now also ends the programs the command started instead of leaving them running and the task marked as running.
- Fixed `/copy` and other clipboard actions on Windows over SSH: the copy now reaches your local clipboard, and the copied text is no longer written to the CLI log file.
- Fixed scheduled automations on Windows receiving only the first line of a multi-line prompt; each scheduled or manually triggered run now gets the full prompt.
- Fixed the macOS "Keychain Not Found" dialog reappearing about once an hour: after a keychain write fails, the CLI now saves your sign-in token to its file-based storage for the rest of the session instead of retrying the keychain on every token refresh.
- Fixed a race where reloading MCP servers while several subagents were starting could launch duplicate MCP server processes, leaving some subagents with broken MCP tools or failed sign-ins.
- Fixed MCP server and plugin OAuth sign-in on machines without an IPv4 loopback address, such as IPv6-only VMs; the sign-in callback now falls back to `[::1]` when `127.0.0.1` is unavailable instead of failing silently.
- Fixed signing in to a plugin from `/plugin` sometimes reporting that the sign-in didn't finish when it had just succeeded.
- Fixed tool groups jumping around when expanded with `Ctrl+O`: the collapse hint now stays on the same line as the expand hint, and in low verbosity the token summary line no longer disappears from the expanded view.
- Fixed the model selector in the Remote Control browser view on phones, where the list could not be scrolled by touch past the first screen, so models near the end of the list could not be selected.
