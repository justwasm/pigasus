# The upstream 1.4.9 release notes (【一手】)
#
# Source: the vendor's own repository —
# https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md
# (raw: https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md),
# fetched 2026-10-28. The section below is copied verbatim; the whole file
# hashed to
# a8b3e21e3fd0bd454f45c7b9e51c5990db246f67960fb9f867e76893311da293
# and this section alone to
# cdba835b4e50c310f45f07b84a538986acf85545a8cc2a22a87b2129383ea37b.
#
# WHY THIS FILE EXISTS: the *served* changelog feed — the one `agy changelog`
# prints and the one internal/cli/testdata/changelog_reference.txt captures —
# does NOT carry the 1.4.9 section yet. Re-captured against the 1.4.9 binary
# on 2026-10-28, the feed is 180,524 bytes / 1,038 lines / 79 sections / 881
# bullets and its newest section is still 1.4.8. The notes below are the
# vendor's own statement of what 1.4.9 changed; they are evidence about the
# release, not a capture of the feed. The clone's changelog therefore still
# prints exactly what the reference prints.
#
# The clone-relevant items are the ones this cycle implemented; see
# docs/cycles/cycle-362-agy-1.4.9-alignment/deliverable.md for the per-item
# mapping and for the ones the clone deliberately does not model.

## 1.4.9

- Added image pasting through your terminal's own paste shortcut, such as `Cmd+V` or `Ctrl+Shift+V` with only an image copied, and by dragging image files into the prompt, which attaches them instead of inserting their paths; image paste now also works over SSH with X11 or Wayland forwarding, and the CLI's paste key now says `No image or video found in the clipboard` when there is nothing to attach and names the clipboard tool to install, such as `wl-clipboard` on Wayland, when one is missing.
- Improved the `/plugin` Discover tab to group marketplace plugins into the same sections as the Antigravity plugin marketplace instead of one flat list, listing a plugin under every section it belongs to.
- Improved `agy plugin validate` to warn when a plugin's `name` isn't lowercase kebab-case, such as `Neo4j` or `foo_bar`, which the marketplace can't publish, and to suggest a valid name, failing under `--strict`; it now also checks the plugin's `logo`, noting when `plugin.json` has none and warning when the logo path is blank, uses backslashes, is absolute or points outside the plugin, sits in a folder the marketplace bundle leaves out such as `.github/`, or is a URL that won't be shown, with the same checks for context provider `iconUri` values.
- Improved the position indicator under long `/diff` lists to say what it counts, such as `[1-44 of 195 files]` in `All Changes` and `[1-44 of 195 rows]` in `Per Turn`, where turn headers count as rows, so it no longer appears to disagree with the file count.
- Improved Auto mode's review of commands that would leave the terminal sandbox: the reviewer now knows whether the working directory is inside a Git or other repository, so it no longer spends its time budget running `git status` in folders that aren't repositories and times out less often.
- Changed `execute_url` permissions so approving a Google site such as `www.google.com` or `cloud.google.com` no longer also allows sensitive Google apps such as `mail.google.com`, `docs.google.com`, `drive.google.com`, `accounts.google.com`, or `console.cloud.google.com`; each of these now needs its own approval.
- Fixed always-allow rules for multi-line commands, such as a `python -c` or `node -e` script spanning several lines, never taking effect, which made the CLI ask again every time and pile up unusable entries in your settings; existing multi-line rules now apply.
- Fixed onboarding screens being cut off in small terminals and hiding their buttons: text and links now wrap to the terminal width, the color preview is cropped or hidden in very narrow terminals, the banner only shows when the whole step fits, and a step that is still too tall scrolls with `Shift+↑`/`Shift+↓` or `PgUp`/`PgDn` while the buttons stay visible.
- Fixed Vim mode's `r`, `f`, `F`, `t`, and `T` canceling when the target character is a space, so `r<space>`, `f<space>`, and `df<space>` now work, commands on an empty last line acting on the line above, and a half-typed operator such as `d` carrying over after you submit a prompt.
- Fixed invisible U+FFF9 and U+FFFA characters reaching the terminal around wrapped code in agent replies, which made some terminals and multiplexers draw boxes or leave stale characters on screen.
- Fixed conversations failing every later model request with `INVALID_MEDIA` after the agent viewed a corrupt image or one with text in front of its header, such as a screenshot captured with `adb exec-out screencap -p > screenshot.png` that also captured a warning; such images are now rejected with an error that says where the real image header starts, so the agent can fix the command.
- Fixed conversations getting stuck failing on every turn after a tool result was left without its matching tool call, such as after older history was trimmed; unmatched tool calls and results are now left out of the request.
- Fixed conversations with certain models on a Google Cloud project failing for the rest of the conversation once the system prompt, tools, or an earlier message changed after a thinking block; outdated thinking blocks are now dropped instead of rejected.
- Fixed the `/browser` subagent not receiving the current page's snapshot at the start of each turn with Chrome DevTools MCP 1.11 and later, because the snapshot request left out the now-required page ID and failed silently.

