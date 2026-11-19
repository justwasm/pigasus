#!/usr/bin/env python3
"""Standardized extraction SOP script for upstream Antigravity CLI (agy).

Pulls seeds, //go:embed file tree, web assets ZIP, and proto descriptors
from the reference `agy` binary into `out/extracted-<version>/`.

Usage:
    python3 scripts/re/extract_agy.py [--bin /path/to/agy] [--out out/extracted-<version>]
"""

import argparse
import hashlib
import os
import pathlib
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]

PROTO_FILES = [
    ("third_party/jetski/cortex_pb/cortex.proto", "cortex"),
    ("google/gca/aicode/v1main/remote_agent_proxy.proto", "remote_agent_proxy"),
    ("third_party/jetski/remote_agent_proxy_pb/remote_agent_proxy.proto", "remote_agent_proxy_pb"),
    ("third_party/jetski/cascade_plugins_pb/cascade_plugins.proto", "cascade_plugins"),
    ("third_party/jetski/prompt_pb/prompt.proto", "prompt"),
    ("third_party/gemini_coder/proto/trajectory.proto", "trajectory"),
    ("third_party/gemini_coder/cider/proto/trajectory_steps.proto", "trajectory_steps"),
]


def find_agy_binary(specified=None):
    if specified:
        p = pathlib.Path(specified).expanduser().resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p
        raise FileNotFoundError(f"Specified agy binary not found or not executable: {specified}")

    env_bin = os.environ.get("AGY_BIN")
    if env_bin:
        p = pathlib.Path(env_bin).expanduser().resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p

    candidates = []
    # 1. Primary active symlinks
    for primary in [
        pathlib.Path.home() / ".openagy" / "bin" / "agy",
        pathlib.Path.home() / ".local" / "bin" / "agy",
    ]:
        if primary.is_file() and os.access(primary, os.X_OK):
            candidates.append(primary)

    # 2. Versioned binaries in ~/.openagy/bin sorted by semver descending
    openagy_bin = pathlib.Path.home() / ".openagy" / "bin"
    if openagy_bin.is_dir():
        def ver_key(p):
            parts = p.name.replace("agy-", "").split(".")
            nums = []
            for part in parts:
                try:
                    nums.append(int(part))
                except ValueError:
                    nums.append(0)
            return nums
        versioned = [p for p in openagy_bin.glob("agy-*") if p.is_file() and os.access(p, os.X_OK)]
        versioned.sort(key=ver_key, reverse=True)
        candidates.extend(versioned)

    for c in candidates:
        if c.is_file() and os.access(c, os.X_OK):
            return c.resolve()

    which_agy = subprocess.run(["which", "agy"], capture_output=True, text=True)
    if which_agy.returncode == 0 and which_agy.stdout.strip():
        p = pathlib.Path(which_agy.stdout.strip()).resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p

    raise FileNotFoundError("Could not find agy binary. Please specify via --bin or AGY_BIN env var.")


def get_agy_version(binary: pathlib.Path, override=None) -> str:
    if override:
        return override.strip().lstrip("v")
    res = subprocess.run([str(binary), "--version"], capture_output=True, text=True)
    if res.returncode == 0 and res.stdout.strip():
        return res.stdout.strip().splitlines()[0].strip().lstrip("v")
    # Fallback to file name if versioned
    stem = binary.name
    if "-" in stem:
        ver = stem.split("-", 1)[1]
        if ver.replace(".", "").isalnum():
            return ver
    return "unknown"


def sha256_file(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def extract_agy(binary_path: pathlib.Path, out_dir: pathlib.Path, version: str):
    print(f"==> Extracting agy CLI assets...")
    print(f"    Binary:  {binary_path}")
    print(f"    Version: {version}")
    print(f"    Out Dir: {out_dir}")

    out_dir.mkdir(parents=True, exist_ok=True)
    seeds_dir = out_dir / "seeds"
    embed_dir = out_dir / "embed"
    web_dir = out_dir / "web-assets"
    protos_dir = out_dir / "protos"
    for d in (seeds_dir, embed_dir, web_dir, protos_dir):
        d.mkdir(parents=True, exist_ok=True)

    # 1. Identity
    size_bytes = binary_path.stat().st_size
    sha = sha256_file(binary_path)
    identity_file = out_dir / "identity.txt"
    with open(identity_file, "w") as f:
        f.write(f"version: {version}\n")
        f.write(f"binary: {binary_path}\n")
        f.write(f"size_bytes: {size_bytes}\n")
        f.write(f"sha256: {sha}\n")
    print(f"    [1/4] Recorded identity ({size_bytes:,} B, sha256: {sha[:16]}...)")

    # 2. Seeds (pclntab)
    print(f"    [2/4] Recovering pclntab function seeds via rea-go-seeds...")
    rea_dir = REPO / "rea-go-seeds"
    res_seeds = subprocess.run(
        ["go", "run", ".", str(binary_path), str(seeds_dir)],
        cwd=str(rea_dir),
        capture_output=True,
        text=True,
    )
    if res_seeds.returncode != 0:
        print(f"Warning: rea-go-seeds failed: {res_seeds.stderr}", file=sys.stderr)
    else:
        print(f"          {res_seeds.stderr.strip() or 'Seeds extracted'}")

    # 3. //go:embed and web frontend ZIP
    print(f"    [3/4] Extracting //go:embed file tree and web assets...")
    res_embed = subprocess.run(
        [
            "go", "run", "./devutils/embedextract",
            "-bin", str(binary_path),
            "-embed-out", str(embed_dir),
            "-zip-out", str(web_dir),
            "-zip-index", "1",
        ],
        cwd=str(REPO),
        capture_output=True,
        text=True,
    )
    if res_embed.returncode != 0:
        print(f"Warning: embedextract failed: {res_embed.stderr}", file=sys.stderr)
    else:
        print(f"          Embed extracted successfully.")

    # 4. Protos
    print(f"    [4/4] Extracting descriptor protos...")
    extract_script = REPO / "scripts" / "re" / "extract_descriptors.py"
    for proto_path, out_stem in PROTO_FILES:
        out_target = protos_dir / f"{out_stem}_{version}.txt"
        with open(out_target, "w") as out_f:
            subprocess.run(
                [sys.executable, str(extract_script), str(binary_path), proto_path],
                cwd=str(REPO),
                stdout=out_f,
                stderr=subprocess.PIPE,
                text=True,
            )
        if out_target.stat().st_size > 0:
            print(f"          Extracted {out_stem}_{version}.txt ({out_target.stat().st_size:,} B)")

    # 5. Changelog (optional)
    res_ch = subprocess.run([str(binary_path), "changelog"], capture_output=True, text=True)
    if res_ch.returncode == 0 and res_ch.stdout.strip():
        ch_file = out_dir / f"changelog_{version}.txt"
        ch_file.write_text(res_ch.stdout)
        print(f"          Captured changelog ({len(res_ch.stdout.splitlines())} lines)")

    print(f"==> agy CLI extraction complete: {out_dir}\n")


def main():
    parser = argparse.ArgumentParser(description="Extract artifacts from upstream agy CLI.")
    parser.add_argument("--bin", help="Path to agy binary")
    parser.add_argument("--version", help="Override version string")
    parser.add_argument("--out", help="Output directory (default: out/extracted-<version>)")
    args = parser.parse_args()

    bin_path = find_agy_binary(args.bin)
    version = get_agy_version(bin_path, args.version)
    out_dir = pathlib.Path(args.out).resolve() if args.out else REPO / "out" / f"extracted-{version}"

    extract_agy(bin_path, out_dir, version)


if __name__ == "__main__":
    main()
