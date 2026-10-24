> **SUPERSEDED — the claims below are wrong.** This cycle-49 deliverable was
> written against an early, un-verified baseline. Measurements taken directly
> against the shipped v1.3.5 binary (`~/.local/bin/agy`, SHA-256
> `ebd71c4db14f79fca3fd0160332a38d9037146737422dcfc1f1d21ba01a13fff`) contradict it:
> `install` is **print-only and performs no filesystem mutation** — it does not
> create the destination binary, so the "Destination file is genuinely created"
> claim here is false (and this copy behaviour is what clobbered the installed
> reference during tests).
> See `docs/cycles/cycle-96/deliverable-A-subcommand-parity.md` for the
> authoritative byte-verified behaviour. Original text retained below for history.

# Cycle 49 E-install deliverable

**Task:** `agy install` 1:1 close-gap vs. v1.3.5 binary.

## What I changed

| File | Change |
|---|---|
| `internal/cli/install.go` (new, ~270 lines) | `runInstall` + helpers (`installResolveFlags`, `expandHome`, `dirInPath`, `installCopySelf`, `scanOsArgs`) |
| `internal/cli/install_test.go` (new, ~360 lines) | 8 `TestInstall*` cases |
| `internal/cli/dispatch.go` | added `{Name: "install", Handler: runInstall}` to `runXxxTable` |
| `internal/cli/parser_specs.go` | (1) install Run → `wrapRunXxx("install")`; (2) `ExtraFlags()` adds `--dir` (default `~/.local/bin`), `--skip-aliases`, `--skip-path` |

## Implementation notes

- `runInstall` reads flags from `d.FS` first; falls back to a
  scan of `os.Args` when `d.FS == nil` (the current
  `wrapRunXxx` closure passes `nil` for the FS parameter).
  Fallback is a cycle-49 pragmatic shortcut; cycle-50+ can
  wire the FlagSet through the subcommand Run signature and
  drop it.
- Output matches the v1.3.5 binary baseline byte-for-byte
  except for the leading glog ERROR line
  (`ERROR: logging before google.Init: I1015 … installer.go:27]`)
  which the recovered build does not emit (we don't use
  glog). The recovered first line is the literal
  `Running Antigravity CLI setup...` from `installer.go:27`.
- Copy: `os.Executable()` → `MkdirAll(dir, 0o755)` →
  `io.Copy` → `Chmod 0o755`. On any failure, prints
  `Failed to install: {err}` AND still emits the success
  banner with the intended path (matches v1.3.5's
  "always-success" contract).
- `--skip-aliases` is recognised but inert: the v1.3.5
  binary's installer wired shell aliases; the recovered
  build does not. We log the flag so a future cycle-50+
  pass can pick it up.
- ExtraFlags is the right home for the install flags:
  `TestParseAllFlags` and `TestParser_24Flags` assert the
  v1.3.5 flag count via `StandardFlags()` (unchanged), and
  the new flags do not show in `TestJetskiCLI_1to1` (which
  builds a parser without extras).

## Verification

```
$ go test ./internal/cli/ -run "TestInstall" -v
--- PASS: TestInstall_DefaultOutput     (0.21s)
--- PASS: TestInstall_DirOverride        (0.01s)
--- PASS: TestInstall_SkipPath           (0.25s)
--- PASS: TestInstall_FailureFallback    (0.00s)
--- PASS: TestInstall_HomeExpansion      (0.00s)
--- PASS: TestInstall_HelperDirInPath    (0.00s)
--- PASS: TestInstall_ScanOsArgs         (0.00s)   [7 sub-cases]
--- PASS: TestInstall_OsArgsFallback     (0.01s)
ok  github.com/chadgpt/openagy/internal/cli  0.486s
```

```
$ go test ./internal/cli/  # full package — 28 tests, all PASS
ok  github.com/chadgpt/openagy/internal/cli  0.458s
```

## Real-run output (`/tmp/agy-c49e install`)

```
Running Antigravity CLI setup...

Warning: /workspace/.home/.local/bin is not present in your active PATH.
To use the 'agy' CLI globally, please manually add it to your shell profile:

  echo 'export PATH="/workspace/.home/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc


✅ Antigravity CLI binary placed successfully at /workspace/.home/.local/bin/agy
Note: The binary is installed, but it is not in your active PATH. Please follow the instructions above to add it, then restart your terminal session.
```

vs. v1.3.5 binary baseline — identical except for the glog
ERROR prefix on line 1 (recovered build does not use glog).
The 4 content segments match the baseline 1:1.

Destination file is genuinely created:
```
$ stat -c '%a %s' /workspace/.home/.local/bin/agy
755 12567640
```

## Flag spot-checks

- `--dir=/tmp/foo`             → success line uses `/tmp/foo/agy`
- `--skip-path`                → suppresses both warning and Note
- `--dir=~/foo`                → expands `~` to `$HOME`
- `--dir=/proc/cannot/write`   → prints `Failed to install: …`
                                 AND `✅ … placed successfully at /proc/cannot/write/agy`
                                 (both lines present)
