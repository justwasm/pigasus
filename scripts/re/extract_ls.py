#!/usr/bin/env python3
"""Standardized extraction SOP script for Antigravity IDE language_server.

Extracts seeds, //go:embed file tree, protobuf descriptor set, IDE JS,
and bundled skills from Antigravity.app into `out/ide-language-server-<version>/`.

Usage:
    python3 scripts/re/extract_ls.py [--app /Applications/Antigravity.app] [--out out/ide-language-server-<version>]
"""

import argparse
import hashlib
import json
import os
import pathlib
import plistlib
import re
import shutil
import struct
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]


def find_app_bundle(specified=None) -> pathlib.Path:
    if specified:
        p = pathlib.Path(specified).expanduser().resolve()
        if p.is_dir():
            return p
        raise FileNotFoundError(f"Specified Antigravity app not found: {specified}")

    env_app = os.environ.get("ANTIGRAVITY_APP")
    if env_app:
        p = pathlib.Path(env_app).expanduser().resolve()
        if p.is_dir():
            return p

    candidates = [
        pathlib.Path("/Applications/Antigravity.app"),
        pathlib.Path.home() / "Applications" / "Antigravity.app",
    ]
    for c in candidates:
        if c.is_dir():
            return c.resolve()

    raise FileNotFoundError("Could not find Antigravity.app. Please specify via --app or ANTIGRAVITY_APP env var.")


def get_app_version(app: pathlib.Path, override=None) -> str:
    if override:
        return override.strip().lstrip("v")
    plist_path = app / "Contents" / "Info.plist"
    if plist_path.is_file():
        try:
            with open(plist_path, "rb") as f:
                pl = plistlib.load(f)
                ver = pl.get("CFBundleShortVersionString") or pl.get("CFBundleVersion")
                if ver:
                    return str(ver).strip()
        except Exception:
            pass
    return "unknown"


def sha256_file(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def asar_unpack_dist(asar_path: pathlib.Path, out_js_dir: pathlib.Path):
    """Unpack non-vendor JS files (e.g. dist/) from Electron asar."""
    with open(asar_path, "rb") as f:
        f.seek(4)
        (hsize,) = struct.unpack("<I", f.read(4))
        f.seek(12)
        (json_size,) = struct.unpack("<I", f.read(4))
        header = json.loads(f.read(json_size).decode("utf-8"))
        data_offset = 8 + hsize

        files = []
        def walk(node, prefix=""):
            for name, entry in node.get("files", {}).items():
                p = f"{prefix}/{name}" if prefix else name
                if "files" in entry:
                    walk(entry, p)
                elif "size" in entry and "offset" in entry:
                    files.append((int(entry["offset"]), int(entry["size"]), p))
        walk(header)

        for offset, size, rel in files:
            top = rel.split("/", 1)[0]
            if top != "dist" and rel not in ("package.json", "icon.png", "trayTemplate.png", "trayTemplate@2x.png"):
                continue
            dst = out_js_dir / rel
            dst.parent.mkdir(parents=True, exist_ok=True)
            f.seek(data_offset + offset)
            dst.write_bytes(f.read(size))


def extract_language_server(app: pathlib.Path, out_dir: pathlib.Path, version: str):
    print(f"==> Extracting Antigravity IDE language_server assets...")
    print(f"    App:     {app}")
    print(f"    Version: {version}")
    print(f"    Out Dir: {out_dir}")

    ls_bin = app / "Contents" / "Resources" / "bin" / "language_server"
    if not ls_bin.is_file():
        raise FileNotFoundError(f"language_server binary not found at {ls_bin}")

    out_dir.mkdir(parents=True, exist_ok=True)
    seeds_dir = out_dir / "seeds"
    embed_dir = out_dir / "embed-fs"
    protos_dir = out_dir / "protos"
    js_dir = out_dir / "ide-js"
    skills_dir = out_dir / "skills"
    for d in (seeds_dir, embed_dir, protos_dir, js_dir, skills_dir):
        d.mkdir(parents=True, exist_ok=True)

    # 1. Identity & Stamp
    size_bytes = ls_bin.stat().st_size
    sha = sha256_file(ls_bin)
    with open(out_dir / "identity.txt", "w") as f:
        f.write(f"version: {version}\n")
        f.write(f"binary: {ls_bin}\n")
        f.write(f"size_bytes: {size_bytes}\n")
        f.write(f"sha256: {sha}\n")

    stamp_res = subprocess.run([str(ls_bin), "--stamp"], capture_output=True, text=True)
    if stamp_res.returncode == 0:
        (out_dir / "stamp.txt").write_text(stamp_res.stdout)
        cl_match = re.search(r"Built at CL:\s*(\d+)", stamp_res.stdout)
        cl = cl_match.group(1) if cl_match else "unknown"
        print(f"    [1/6] Recorded identity & stamp (CL: {cl}, {size_bytes:,} B, sha: {sha[:16]}...)")
    else:
        print(f"    [1/6] Recorded identity ({size_bytes:,} B)")

    # 2. Seeds (pclntab)
    print(f"    [2/6] Recovering pclntab function seeds via rea-go-seeds...")
    rea_dir = REPO / "rea-go-seeds"
    res_seeds = subprocess.run(
        ["go", "run", ".", str(ls_bin), str(seeds_dir)],
        cwd=str(rea_dir),
        capture_output=True,
        text=True,
    )
    if res_seeds.returncode != 0:
        print(f"Warning: rea-go-seeds failed: {res_seeds.stderr}", file=sys.stderr)
    else:
        print(f"          {res_seeds.stderr.strip() or 'Seeds extracted'}")

    # 3. //go:embed file tree
    print(f"    [3/6] Extracting //go:embed file tree...")
    res_embed = subprocess.run(
        ["go", "run", "./devutils/embedextract", "-bin", str(ls_bin), "-embed-out", str(embed_dir)],
        cwd=str(REPO),
        capture_output=True,
        text=True,
    )
    if res_embed.returncode != 0:
        print(f"Warning: embedextract failed: {res_embed.stderr}", file=sys.stderr)
    else:
        print(f"          Embed extracted successfully.")

    # 4. Protoset
    print(f"    [4/6] Extracting FileDescriptorSet (protoset)...")
    protoset_script = out_dir / "extract_ls_protoset.py"
    if not protoset_script.is_file():
        # Fallback to REPO / out / ide-language-server / extract_ls_protoset.py
        protoset_script = REPO / "out" / "ide-language-server" / "extract_ls_protoset.py"
    
    protoset_target = protos_dir / "language_server.protoset"
    if protoset_script.is_file():
        subprocess.run(
            [sys.executable, str(protoset_script), "--bin", str(ls_bin), str(protoset_target)],
            cwd=str(out_dir),
            capture_output=True,
            text=True,
        )
        if protoset_target.is_file():
            print(f"          Extracted protoset ({protoset_target.stat().st_size:,} B)")
    else:
        print(f"Warning: extract_ls_protoset.py not found, skipping protoset extraction", file=sys.stderr)

    # 5. IDE JS from app.asar
    asar_path = app / "Contents" / "Resources" / "app.asar"
    if asar_path.is_file():
        print(f"    [5/6] Unpacking dist JS from app.asar...")
        asar_unpack_dist(asar_path, js_dir)
        js_count = len(list(js_dir.rglob("*.js")))
        print(f"          Unpacked {js_count} JS files.")
    else:
        print(f"    [5/6] Skipping asar unpack (app.asar not found).")

    # 6. Bundled skills
    app_skills = app / "Contents" / "Resources" / "skills"
    if app_skills.is_dir():
        print(f"    [6/6] Syncing bundled skills...")
        for item in app_skills.iterdir():
            target = skills_dir / item.name
            if item.is_dir():
                shutil.copytree(item, target, dirs_exist_ok=True)
            elif item.is_file():
                shutil.copy2(item, target)
        print(f"          Skills synced.")

    # Update backward-compatible symlink `out/ide-language-server -> ide-language-server-<version>`
    if out_dir.parent == REPO / "out":
        symlink_path = REPO / "out" / "ide-language-server"
        if symlink_path.is_symlink() or not symlink_path.exists():
            try:
                symlink_path.unlink(missing_ok=True)
                symlink_path.symlink_to(out_dir.name)
                print(f"    Updated symlink: out/ide-language-server -> {out_dir.name}")
            except Exception as e:
                print(f"Warning: Could not update symlink: {e}")

    print(f"==> language_server extraction complete: {out_dir}\n")


def main():
    parser = argparse.ArgumentParser(description="Extract artifacts from Antigravity IDE language_server.")
    parser.add_argument("--app", help="Path to Antigravity.app")
    parser.add_argument("--version", help="Override IDE version string")
    parser.add_argument("--out", help="Output directory (default: out/ide-language-server-<version>)")
    args = parser.parse_args()

    app_path = find_app_bundle(args.app)
    version = get_app_version(app_path, args.version)
    out_dir = pathlib.Path(args.out).resolve() if args.out else REPO / "out" / f"ide-language-server-{version}"

    extract_language_server(app_path, out_dir, version)


if __name__ == "__main__":
    main()
