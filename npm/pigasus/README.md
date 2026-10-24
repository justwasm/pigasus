# Pigasus (`npx pigasus`)

Cross-platform runner and wrapper for **Pigasus** — a high-fidelity reimplementation of the Google Antigravity agent CLI (`agy`).

## Quick Start

Run instantly without manual installation:

```bash
npx pigasus --help
```

Or run interactively:

```bash
npx pigasus
```

You can also run via the `agy` alias:

```bash
npx pigasus agy --help
```

---

## Global Installation

Install globally for instant access in any terminal session:

```bash
npm install -g pigasus
```

Then run directly:

```bash
pigasus --version
pigasus
```

---

## How It Works

1. **Automatic Architecture Detection**: Detects your operating system (`Darwin`, `Linux`) and CPU architecture (`arm64`, `x86_64`).
2. **Automated Download**: Downloads the prebuilt native binary from [GitHub Releases](https://github.com/justwasm/pigasus/releases).
3. **SHA-256 Integrity Verification**: Verifies the archive against official release `checksums.txt`.
4. **Local Caching**: Caches the unpacked native binary under `~/.cache/pigasus/bin/` so subsequent runs launch with zero delay.
5. **Interactive TUI Support**: Transparently attaches stdin/stdout and handles `SIGWINCH` resize signals for seamless terminal Bubbletea TUI rendering.

---

## Environment Variables

- `PIGASUS_CACHE_DIR`: Directory where downloaded binaries are stored (default: `~/.cache/pigasus/bin`).
- `PIGASUS_BINARY_PATH`: Override to use a local or custom-built `pigasus` binary instead of downloading.
- `PIGASUS_DOWNLOAD_URL`: Custom base URL for release assets.
- `GITHUB_MIRROR`: Optional mirror prefix for GitHub releases (e.g. `https://ghproxy.com/`).

---

## License

Apache-2.0. See [LICENSE](./LICENSE) for details.
