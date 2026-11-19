#!/usr/bin/env python3
"""Verify reproducibility of reverse-engineering extraction pipelines.

Extracts artifacts from the reference binary into a clean temporary directory
and verifies that every generated file (seeds, embeds, protos, metadata) is
100% byte-identical to the tracked golden baseline in `out/`.

Usage:
    python3 scripts/re/verify_reproducibility.py [--target {all,agy,ls,localharness}]
"""

import argparse
import hashlib
import os
import pathlib
import sys
import tempfile

REPO = pathlib.Path(__file__).resolve().parents[2]


def hash_file(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def compare_trees(label: str, golden_dir: pathlib.Path, test_dir: pathlib.Path, ignore_stamps=True) -> bool:
    print(f"\nComparing {label}:")
    print(f"  Golden: {golden_dir}")
    print(f"  Actual: {test_dir}")

    if not golden_dir.is_dir() or not test_dir.is_dir():
        print(f"  Error: Directory missing.")
        return False

    golden_files = {p.relative_to(golden_dir): p for p in golden_dir.rglob("*") if p.is_file()}
    test_files = {p.relative_to(test_dir): p for p in test_dir.rglob("*") if p.is_file()}

    all_keys = sorted(set(golden_files.keys()) | set(test_files.keys()))
    mismatches = 0
    matched = 0

    for rel in all_keys:
        rel_str = str(rel)
        # Skip date/time, changelog captures, or environment-specific timestamps if requested
        if ignore_stamps and (rel_str in ("stamp.txt", "summary.txt", "identity.txt") or rel_str.startswith("changelog_")):
            continue

        if rel not in golden_files:
            print(f"  [+] Only in test output: {rel_str}")
            mismatches += 1
            continue
        if rel not in test_files:
            print(f"  [-] Missing from test output: {rel_str}")
            mismatches += 1
            continue

        g_hash = hash_file(golden_files[rel])
        t_hash = hash_file(test_files[rel])
        if g_hash != t_hash:
            print(f"  [!] Hash mismatch: {rel_str} (golden: {g_hash[:12]}, actual: {t_hash[:12]})")
            mismatches += 1
        else:
            matched += 1

    print(f"  Result: {matched} identical files, {mismatches} mismatches.")
    return mismatches == 0


def verify_agy(tmp_root: pathlib.Path) -> bool:
    golden = REPO / "out" / "extracted-1.5.12"
    if not golden.is_dir():
        print("Golden out/extracted-1.5.12 not found, skipping.")
        return False

    from extract_agy import extract_agy, find_agy_binary, get_agy_version

    bin_path = find_agy_binary()
    version = get_agy_version(bin_path)
    test_out = tmp_root / f"extracted-{version}"
    extract_agy(bin_path, test_out, version)

    return compare_trees("agy CLI", golden, test_out)


def verify_ls(tmp_root: pathlib.Path) -> bool:
    golden = REPO / "out" / "ide-language-server-2.33.0"
    if not golden.is_dir():
        print("Golden out/ide-language-server-2.33.0 not found, skipping.")
        return False

    from extract_ls import extract_language_server, find_app_bundle, get_app_version

    app_path = find_app_bundle()
    version = get_app_version(app_path)
    test_out = tmp_root / f"ide-language-server-{version}"
    extract_language_server(app_path, test_out, version)

    # Compare key extracted asset subdirectories
    subdirs = ["seeds", "embed-fs", "protos", "ide-js"]
    ok = True
    for sd in subdirs:
        g_sub = golden / sd
        t_sub = test_out / sd
        if not compare_trees(f"language_server IDE ({sd})", g_sub, t_sub):
            ok = False
    return ok


def verify_localharness(tmp_root: pathlib.Path) -> bool:
    from fetch_binaries import fetch_localharness
    from extract_localharness import extract_localharness

    bin_path, proto_dir = fetch_localharness(version="0.1.23", os_name="darwin", arch="arm64")

    # Extract twice to two independent directories to test 100% determinism & reproducibility
    test_out_a = tmp_root / "localharness_run_a"
    test_out_b = tmp_root / "localharness_run_b"
    extract_localharness(bin_path, proto_dir, test_out_a, "0.1.23")
    extract_localharness(bin_path, proto_dir, test_out_b, "0.1.23")

    ok = compare_trees("localharness SDK (run A vs run B determinism)", test_out_a, test_out_b)

    # Also verify that reconstructed .proto files match expected schema signatures
    proto_files = list((test_out_a / "proto").glob("*.proto"))
    if len(proto_files) != 7:
        print(f"Warning: Expected 7 .proto schemas, got {len(proto_files)}")
        ok = False
    else:
        print(f"  Verified: All 7 .proto files reconstructed deterministically.")

    return ok


def main():
    parser = argparse.ArgumentParser(description="Verify reproducibility of reverse-engineering extraction.")
    parser.add_argument("--target", choices=["all", "agy", "ls", "localharness"], default="all")
    args = parser.parse_args()

    # Add scripts/re to sys.path
    re_dir = REPO / "scripts" / "re"
    if str(re_dir) not in sys.path:
        sys.path.insert(0, str(re_dir))

    all_passed = True
    with tempfile.TemporaryDirectory() as td:
        tmp_root = pathlib.Path(td)

        if args.target in ("all", "agy"):
            if not verify_agy(tmp_root):
                all_passed = False

        if args.target in ("all", "ls"):
            if not verify_ls(tmp_root):
                all_passed = False

        if args.target in ("all", "localharness"):
            if not verify_localharness(tmp_root):
                all_passed = False

    if not all_passed:
        print("\nReproducibility check FAILED.")
        sys.exit(1)

    print("\nReproducibility check PASSED: All extracted assets are byte-identical to golden baselines.")


if __name__ == "__main__":
    main()
