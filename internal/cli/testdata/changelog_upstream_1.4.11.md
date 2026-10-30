# The upstream 1.4.11 release notes (【一手】)
#
# Source: the vendor's own repository —
# https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md
# (raw: https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md),
# fetched 2026-10-30. The section below is copied verbatim.
#
# WHY THIS FILE EXISTS: the *served* changelog feed — the one `agy changelog`
# prints and the one internal/cli/testdata/changelog_reference.txt captures —
# lags the vendor's own repository, so the 1.4.11 section does not reach the
# clone through the feed. The notes below are the vendor's own statement of
# what 1.4.11 changed; they are evidence about the release, not a capture of
# the feed. The clone's changelog therefore still prints exactly what the
# reference prints.
#
# The clone-relevant items are the ones this cycle implemented; see
# docs/cycles/cycle-364-agy-1.4.11-alignment/deliverable.md for the per-item
# mapping and for the ones the clone deliberately does not model.

## 1.4.11

- Added a billing model choice after you sign in with Google Cloud, so you can pick either a Gemini subscription or Pay As You Go.
- Added `terminationBehavior` to `PreInvocation` hooks: returning `"terminate"` stops the turn before the model is called, for example to block a prompt that contains sensitive data, and hooks can now inject an `errorMessage` whose `userMessage` is shown to you without being sent to the model, which also makes a `-p` run exit with a non-zero code.
- Added a `finished_reason` field to the result of `--output-format json` and `stream-json` runs that says why a turn ended early, `print_timeout` when `--print-timeout` expired and the response is partial, `interrupted` when the run was canceled such as with `Ctrl+C`, or `error` for any other failure, so scripts can tell a cut-off answer from a complete one; the field is omitted when the turn finishes normally.
- Improved the `/plugin` Discover tab for long shelves: each shelf now shows its first 6 plugins followed by a `⋯ N more — enter to show` row, `Ctrl+E` expands or collapses the shelf under the cursor, and `[` and `]` jump to the previous or next shelf.
- Fixed `agy plugin import claude` printing `No claude extensions found.` for plugins installed from a Claude Code marketplace: it now reads Claude Code's list of installed plugins, skipping plugins disabled in Claude Code's settings and plugins installed for a single project only, and imported plugins now keep their remote MCP servers (`"type": "http"` with `url` and `headers`) and servers from `.mcp.json` files without an `mcpServers` wrapper, expand `${CLAUDE_PLUGIN_ROOT}` to the installed plugin's directory, and list servers that can't be converted, such as ones using the legacy SSE transport, with a warning instead of reporting them as installed.
