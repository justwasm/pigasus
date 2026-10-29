# The upstream 1.4.10 release notes (【一手】)
#
# Source: the vendor's own repository —
# https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md
# (raw: https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md),
# fetched 2026-10-29. The section below is copied verbatim.
#
# WHY THIS FILE EXISTS: the *served* changelog feed — the one `agy changelog`
# prints and the one internal/cli/testdata/changelog_reference.txt captures —
# does NOT carry the 1.4.10 section yet. Re-captured against the 1.4.10 binary
# on 2026-10-29, the feed is 184,882 bytes / 1,054 lines / 80 sections / 895
# bullets and its newest section is still 1.4.9. The notes below are the
# vendor's own statement of what 1.4.10 changed; they are evidence about the
# release, not a capture of the feed. The clone's changelog therefore still
# prints exactly what the reference prints.
#
# The clone-relevant items are the ones this cycle implemented; see
# docs/cycles/cycle-363-agy-1.4.10-alignment/deliverable.md for the per-item
# mapping and for the ones the clone deliberately does not model.

## 1.4.10

- Improved streaming of agent replies: lines that scroll off the top of the screen are now scrolled into your terminal's scrollback instead of repainting the whole screen, and text wrapping writes whole runs of text at a time, so streaming writes about 70% fewer bytes to the terminal and stays much smoother over slow SSH connections.
- Improved memory use shortly after startup: the CLI now returns about 100 MB of memory left over from startup to the operating system 15 seconds after the interactive UI opens; print and headless modes are unaffected.
- Improved the after-compaction size shown in the compaction marker and in `/context` to also count what the summary carries into the next prompt, such as your earlier requests and artifact, subagent, and running-task snapshots, so it is closer to the real size; the `(N summary)` label still shows the summary alone.
- Fixed subagents losing the task their parent agent gave them, and the parent's ID, after their context was compacted, including with `/compact`; a subagent's summary now keeps the parent agent's 10 most recent messages.
- Fixed conversations with many or large images, such as screenshots or pasted images, being rejected for exceeding the model's request size limit: images are now capped at 2000 pixels per side and large PNGs are sent as JPEGs when that is much smaller, and an image saved as JPEG but labeled PNG, such as a pasted image or browser screenshot, is no longer rejected for a mismatched type.
- Fixed the `/browser` subagent not receiving the current page's snapshot when the selected page belongs to an isolated browser context, such as a page opened with `isolatedContext` or attached from an Incognito window.
