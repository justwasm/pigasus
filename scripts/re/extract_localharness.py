#!/usr/bin/env python3
"""Standardized extraction SOP script for Google Antigravity localharness.

Downloads the specified `google-antigravity` wheel from PyPI (or uses a local wheel/binary),
extracts the `localharness` binary, recovers function seeds, and reconstructs
the protobuf schema into `out/localharness/`.

Usage:
    python3 scripts/re/extract_localharness.py [--version 0.1.23] [--wheel path/to.whl] [--bin path/to/localharness] [--out out/localharness]
"""

import argparse
import hashlib
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import zipfile

REPO = pathlib.Path(__file__).resolve().parents[2]


def sha256_file(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def download_wheel(version: str, dest_dir: pathlib.Path) -> pathlib.Path:
    pkg = f"google-antigravity=={version}" if version else "google-antigravity"
    print(f"    Downloading {pkg} wheel for macOS arm64 from PyPI...")
    cmd = [
        "pip", "download", pkg,
        "--no-deps",
        "--platform", "macosx_11_0_arm64",
        "--only-binary=:all:",
        "--python-version", "3.12",
        "--implementation", "cp",
        "--dest", str(dest_dir),
    ]
    res = subprocess.run(cmd, capture_output=True, text=True)
    if res.returncode != 0:
        raise RuntimeError(f"pip download failed: {res.stderr}\n{res.stdout}")

    whls = list(dest_dir.glob("google_antigravity*.whl"))
    if not whls:
        raise FileNotFoundError(f"No google_antigravity .whl found in {dest_dir}")
    return whls[0]


def extract_localharness(binary_path: pathlib.Path, proto_dir: pathlib.Path, out_dir: pathlib.Path, version: str):
    print(f"==> Extracting localharness assets...")
    print(f"    Binary:  {binary_path}")
    print(f"    Version: {version}")
    print(f"    Out Dir: {out_dir}")

    out_dir.mkdir(parents=True, exist_ok=True)
    seeds_dir = out_dir / "seeds"
    seeds_dir.mkdir(parents=True, exist_ok=True)
    target_proto_dir = out_dir / "proto"
    target_proto_dir.mkdir(parents=True, exist_ok=True)

    # 1. Identity
    size_bytes = binary_path.stat().st_size
    sha = sha256_file(binary_path)
    with open(out_dir / "identity.txt", "w") as f:
        f.write(f"version: {version}\n")
        f.write(f"binary: {binary_path.name}\n")
        f.write(f"size_bytes: {size_bytes}\n")
        f.write(f"sha256: {sha}\n")
    print(f"    [1/3] Recorded identity ({size_bytes:,} B, sha: {sha[:16]}...)")

    # 2. Seeds (pclntab)
    print(f"    [2/3] Recovering pclntab function seeds via rea-go-seeds...")
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

    # 3. Protocol dump from python stubs
    if proto_dir and proto_dir.is_dir():
        print(f"    [3/3] Reconstructing .proto schemas from Python stubs...")
        protodump_script = REPO / "out" / "localharness" / "proto_tools" / "protodump.py"
        pb2_files = list(proto_dir.glob("*_pb2.py"))
        if protodump_script.is_file() and pb2_files:
            cmd = [sys.executable, str(protodump_script), str(target_proto_dir)] + [str(p) for p in pb2_files]
            res_proto = subprocess.run(cmd, capture_output=True, text=True)
            if res_proto.returncode == 0:
                proto_count = len(list(target_proto_dir.glob("*.proto")))
                print(f"          Reconstructed {proto_count} .proto schemas.")
            else:
                print(f"Warning: protodump.py failed: {res_proto.stderr}", file=sys.stderr)
        else:
            print(f"Warning: protodump script or pb2 files missing.", file=sys.stderr)
    else:
        print(f"    [3/3] Skipped proto reconstruction (proto stubs not supplied).")

    print(f"==> localharness extraction complete: {out_dir}\n")


def main():
    parser = argparse.ArgumentParser(description="Extract artifacts from localharness PyPI wheel or binary.")
    parser.add_argument("--version", default="0.1.23", help="PyPI version of google-antigravity (default: 0.1.23)")
    parser.add_argument("--wheel", help="Path to local google_antigravity .whl")
    parser.add_argument("--bin", help="Path to extracted localharness binary")
    parser.add_argument("--out", help="Output directory (default: out/localharness)")
    args = parser.parse_args()

    out_dir = pathlib.Path(args.out).resolve() if args.out else REPO / "out" / "localharness"

    if args.bin:
        bin_path = pathlib.Path(args.bin).resolve()
        extract_localharness(bin_path, None, out_dir, args.version)
        return

    with tempfile.TemporaryDirectory() as td:
        temp_dir = pathlib.Path(td)
        if args.wheel:
            whl_path = pathlib.Path(args.wheel).resolve()
        else:
            whl_path = download_wheel(args.version, temp_dir)

        print(f"    Unpacking wheel: {whl_path.name}")
        unpack_dir = temp_dir / "unpacked"
        with zipfile.ZipFile(whl_path, "r") as z:
            z.extractall(unpack_dir)

        bin_path = unpack_dir / "google" / "antigravity" / "bin" / "localharness"
        if not bin_path.is_file():
            raise FileNotFoundError(f"localharness binary not found in wheel: {bin_path}")
        bin_path.chmod(0o755)

        proto_dir = unpack_dir / "google" / "antigravity" / "proto"
        extract_localharness(bin_path, proto_dir, out_dir, args.version)


if __name__ == "__main__":
    main()
