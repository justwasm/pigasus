# Voice preview samples

The recorded preview clips the Antigravity web UI plays in its voice picker,
embedded into the binary so `livechat` and the TUI can audition a persona
before switching to it.

- **Source**: `out/extracted-1.5.10/web-assets/voices/` (agy 1.5.10, macOS arm64).
- **Layout**: `<persona>_<variant>.mp3` — nine personas (`arlo`, `cleo`,
  `finn`, `fola`, `gero`, `lora`, `neno`, `olin`, `tari`), three variants each,
  27 files, 680 KiB total.
- **Copy, not link**: `out/` is reverse-engineering evidence and is excluded
  from the published snapshot (`scripts/publish-snapshot.sh` allowlists
  `scripts/` only), so the clips are duplicated here to keep `/voice preview`
  working in the published tree.

Do not regenerate these by hand — re-extract from the target build and copy
them over, then run `go test ./internal/voice/`.
