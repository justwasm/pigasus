#!/usr/bin/env python3
"""Unified runner for reverse-engineering extraction SOP across all three upstream components:
  1. agy (CLI)
  2. language_server (IDE)
  3. localharness (SDK)

Usage:
    python3 scripts/re/extract_all.py [--target {all,agy,ls,localharness}]
"""

import argparse
import pathlib
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]


def run_extractor(script_name: str, extra_args=None):
    script = REPO / "scripts" / "re" / script_name
    if not script.is_file():
        raise FileNotFoundError(f"Extractor script not found: {script}")
    cmd = [sys.executable, str(script)] + (extra_args or [])
    print(f"\n========================================================")
    print(f"Running {script_name}...")
    print(f"========================================================")
    res = subprocess.run(cmd, cwd=str(REPO))
    if res.returncode != 0:
        print(f"Error: {script_name} exited with return code {res.returncode}", file=sys.stderr)
        return False
    return True


def main():
    parser = argparse.ArgumentParser(description="Unified extraction runner for agy, language_server, and localharness.")
    parser.add_argument("--target", choices=["all", "agy", "ls", "localharness"], default="all",
                        help="Extraction target to run (default: all)")
    parser.add_argument("--agy-bin", help="Explicit path to agy binary")
    parser.add_argument("--app", help="Explicit path to Antigravity.app")
    parser.add_argument("--lh-version", help="Explicit localharness version from PyPI")
    args = parser.parse_args()

    success = True
    if args.target in ("all", "agy"):
        extra = ["--bin", args.agy_bin] if args.agy_bin else []
        if not run_extractor("extract_agy.py", extra):
            success = False

    if args.target in ("all", "ls"):
        extra = ["--app", args.app] if args.app else []
        if not run_extractor("extract_ls.py", extra):
            success = False

    if args.target in ("all", "localharness"):
        extra = ["--version", args.lh_version] if args.lh_version else []
        if not run_extractor("extract_localharness.py", extra):
            success = False

    if not success:
        sys.exit(1)
    print("\nAll requested extraction tasks completed successfully.")


if __name__ == "__main__":
    main()
