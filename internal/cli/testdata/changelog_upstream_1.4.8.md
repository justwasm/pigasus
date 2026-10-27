# The upstream 1.4.8 release notes (【一手】)
#
# Source: the vendor's own repository —
# https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md
# (raw: https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md),
# fetched 2026-10-27. The section below is copied verbatim; the whole file
# hashed to
# 0b911c6fb5187c9286ac2a318a432c8ae1bfdd3f9820edc720a4f13c5ed38d56
# and this section alone to
# 2d2a18bca7fc92bc4cd9da6a4d3ae2e7a51abd8d1678b106be7623394737f22d.
#
# WHY THIS FILE EXISTS: the *served* changelog feed — the one `agy changelog`
# prints and the one internal/cli/testdata/changelog_reference.txt captures —
# does NOT carry the 1.4.8 section yet. Re-captured against the 1.4.8 binary
# on 2026-10-27, the feed is 177,232 bytes / 1,023 lines / 78 sections / 868
# bullets and its newest section is still 1.4.7. The notes below are the
# vendor's own statement of what 1.4.8 changed; they are evidence about the
# release, not a capture of the feed. The clone's changelog therefore still
# prints exactly what the reference prints.
#
# The clone-relevant item is the ninth bullet: a failed tool call is now
# passed back to the agent as an ERROR rather than as an ordinary result.
# The clone's implementation of it is in internal/tools/failure.go and
# internal/backend; the first-hand capture of the new wire shape is
# internal/tools/testdata/tool_errors_1.4.8.txt.

## 1.4.8

- Added `[` and `]` to the `Per Turn` tab in `/diff` to jump to the first file of the previous or next turn, wrapping around at either end, in both the file list and the file view; the `All Changes` and `Per Turn` key-hint footers now wrap onto a second row on narrow terminals instead of being cut off.
- Improved how quickly `/diff` opens in large repositories and long conversations: `All Changes` and `Commit Tree` appear as soon as tracked changes are ready, showing `checking for untracked files…` while untracked files load in the background, and `Per Turn` shows the newest turns first and loads older ones in the background with a `checking N older turn(s)…` indicator, caching finished turns so reopening `/diff` only recomputes the latest one.
- Improved `/diff` in checkouts with large untracked files: binary files and untracked files over 1 MB are listed with a note such as `binary file` or `7.3 MB, over the 1 MB limit` instead of being read into memory, untracked files containing lines that start with `++` followed by a space are no longer shown incorrectly, and a file deleted while `/diff` is loading no longer makes the whole file list fail.
- Improved Auto mode's review of commands that would leave the terminal sandbox: it now has more time to decide and stops any single slow check after 10 seconds, so it times out less often and asks you to approve safe commands less often.
- Improved `agy plugin install` output to name each installed skill, agent, rule, command, MCP server, and hook, to stop listing component types the plugin doesn't contain as skipped, and to print the folder the plugin was installed to, adding `(replaced existing installation)` when it overwrote an earlier copy.
- Improved `agy plugin validate` to check the `author`, `homepage`, and `repository` fields in `plugin.json`, warning when `author` is neither a name string nor a `{"name", "url"}` object or when a URL isn't a valid `https://` link, and plugin listings now show the author's name when `author` is written as an object instead of leaving it blank.
- Improved resuming a conversation to use about 10% less CPU by not rendering already finished tool calls a second time.
- Fixed failed tool calls, including calls to a tool that doesn't exist and canceled or interrupted calls, being passed back to the agent as ordinary results; the agent now sees them as errors and can recover on its next step.
- Fixed conversations forked from one with an active `/goal`, including `/btw` side questions and forked subagents, inheriting that goal and being unable to stop; the inherited goal is now canceled at the fork point.
- Fixed subagents failing to run when their custom agent's `name` contains a colon.
- Fixed deleting a conversation leaving its running subagents, their saved history, and their shared or branched workspaces behind; they are now stopped and cleaned up with it.
- Fixed a memory leak in long sessions with plugins that run sidecars, where every sidecar run left a background task running for the rest of the session.
- Fixed pasting images on Windows when they were copied from a browser, Snipping Tool, or another app that puts PNG or V5 bitmap data on the clipboard, and pasting now retries when another app briefly holds the clipboard.
