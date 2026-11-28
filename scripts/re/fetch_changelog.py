#!/usr/bin/env python3
"""Fetch and extract a specific version section from the official Antigravity CLI CHANGELOG.md.

Usage:
    python3 scripts/re/fetch_changelog.py [--version 1.5.18] [--out internal/cli/testdata/changelog_upstream_1.5.18.md]
"""

import argparse
import pathlib
import re
import sys
import urllib.request

REPO = pathlib.Path(__file__).resolve().parents[2]
CHANGELOG_URL = "https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md"


def fetch_changelog_text(url: str = CHANGELOG_URL) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": "openagy-re-fetch/1.0"})
    with urllib.request.urlopen(req, timeout=20) as resp:
        return resp.read().decode("utf-8").replace("\r\n", "\n")


def extract_version_section(text: str, version: str | None = None) -> tuple[str, str]:
    if version:
        ver = version.strip().lstrip("v")
        pattern = rf"(^## {re.escape(ver)}\n.*?)(?=^## |\Z)"
        m = re.search(pattern, text, re.M | re.S)
        if not m:
            raise ValueError(f"Version section '## {ver}' not found in upstream CHANGELOG.md")
        return ver, m.group(1).strip() + "\n"

    m = re.search(r"^## ([0-9]+\.[0-9]+\.[0-9]+)\n(.*?)(?=^## |\Z)", text, re.M | re.S)
    if not m:
        raise ValueError("No version section found in upstream CHANGELOG.md")
    ver = m.group(1)
    return ver, f"## {ver}\n{m.group(2)}".strip() + "\n"


def main():
    parser = argparse.ArgumentParser(description="Fetch upstream Antigravity CLI CHANGELOG section.")
    parser.add_argument("--version", help="Target version (e.g. 1.5.18). Defaults to latest section.")
    parser.add_argument("--out", help="Output markdown path (defaults to internal/cli/testdata/changelog_upstream_<version>.md)")
    parser.add_argument("--url", default=CHANGELOG_URL, help="Override CHANGELOG.md raw URL")
    args = parser.parse_args()

    text = fetch_changelog_text(args.url)
    ver, section = extract_version_section(text, args.version)
    out_path = pathlib.Path(args.out) if args.out else (REPO / "internal" / "cli" / "testdata" / f"changelog_upstream_{ver}.md")
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(section, encoding="utf-8")
    bullets = sum(1 for line in section.splitlines() if line.startswith("- "))
    print(f"Saved {ver} changelog ({bullets} bullets, {len(section)} bytes) -> {out_path}")


if __name__ == "__main__":
    main()
