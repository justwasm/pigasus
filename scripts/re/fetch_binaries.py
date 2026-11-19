#!/usr/bin/env python3
"""Fetch upstream reference binaries and packages from official distribution endpoints.

Supports downloading and caching binaries across platforms (darwin, linux, windows; arm64, amd64):
  1. agy (CLI)              - from Google Cloud Run auto-updater & GCS bucket
  2. language_server (IDE)  - from Hub auto-updater releases / GCS bucket & IDE updater
  3. localharness (SDK)     - from PyPI wheel distribution (google-antigravity)

Usage:
    python3 scripts/re/fetch_binaries.py --target {all,agy,ls,localharness} [--os darwin|linux] [--arch arm64|amd64]
"""

import argparse
import hashlib
import json
import os
import pathlib
import platform
import shutil
import subprocess
import sys
import tarfile
import urllib.request
import zipfile

REPO = pathlib.Path(__file__).resolve().parents[2]
DEFAULT_CACHE = REPO / ".cache" / "upstream"

# Official Upstream Service Endpoints
CLI_UPDATER_BASE = "https://antigravity-cli-auto-updater-974169037036.us-central1.run.app"
HUB_UPDATER_BASE = "https://antigravity-hub-auto-updater-974169037036.us-central1.run.app"
IDE_UPDATER_BASE = "https://antigravity-ide-auto-updater-974169037036.us-central1.run.app"
PUBLIC_GCS_BASE = "https://storage.googleapis.com/antigravity-public"
PYPI_API_BASE = "https://pypi.org/pypi/google-antigravity/json"


def normalize_os(val: str) -> str:
    v = (val or platform.system()).lower()
    if "darwin" in v or "mac" in v:
        return "darwin"
    if "linux" in v:
        return "linux"
    if "windows" in v or "win" in v:
        return "windows"
    return v


def normalize_arch(val: str) -> str:
    v = (val or platform.machine()).lower()
    if "arm64" in v or "aarch64" in v:
        return "arm64"
    if "x86_64" in v or "amd64" in v or "x64" in v:
        return "amd64"
    return v


def download_file(url: str, dest_path: pathlib.Path, expected_sha512=None, expected_sha256=None) -> pathlib.Path:
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    temp_path = dest_path.with_suffix(dest_path.suffix + ".partial")

    print(f"      Downloading: {url}")
    curl = shutil.which("curl")
    if curl:
        cmd = [curl, "-s", "-L", "--fail", "--retry", "3", "-o", str(temp_path), url]
        res = subprocess.run(cmd)
        if res.returncode != 0:
            temp_path.unlink(missing_ok=True)
            raise RuntimeError(f"curl download failed with exit code {res.returncode}")
    else:
        req = urllib.request.Request(url, headers={"User-Agent": "openagy-re-fetch/1.0"})
        with urllib.request.urlopen(req, timeout=120) as resp, open(temp_path, "wb") as f:
            while chunk := resp.read(1024 * 1024):
                f.write(chunk)

    # Verify checksums if provided
    if expected_sha512 or expected_sha256:
        with open(temp_path, "rb") as f:
            data = f.read()
            if expected_sha512:
                actual = hashlib.sha512(data).hexdigest()
                if actual.lower() != expected_sha512.lower():
                    temp_path.unlink(missing_ok=True)
                    raise ValueError(f"SHA-512 mismatch: got {actual}, want {expected_sha512}")
            if expected_sha256:
                actual = hashlib.sha256(data).hexdigest()
                if actual.lower() != expected_sha256.lower():
                    temp_path.unlink(missing_ok=True)
                    raise ValueError(f"SHA-256 mismatch: got {actual}, want {expected_sha256}")

    temp_path.replace(dest_path)
    return dest_path


def fetch_agy(version: str = None, os_name: str = None, arch: str = None, cache_dir: pathlib.Path = None) -> pathlib.Path:
    """Fetch agy CLI binary for the given platform from the official updater service."""
    os_name = normalize_os(os_name)
    arch = normalize_arch(arch)
    cache_dir = cache_dir or DEFAULT_CACHE

    platform_key = f"{os_name}_{arch}"
    manifest_url = f"{CLI_UPDATER_BASE}/manifests/{platform_key}.json"
    print(f"    [agy] Fetching manifest: {manifest_url}")

    req = urllib.request.Request(manifest_url, headers={"User-Agent": "openagy-re-fetch/1.0"})
    with urllib.request.urlopen(req, timeout=10) as resp:
        manifest = json.loads(resp.read().decode())

    remote_ver = manifest.get("version", "unknown")
    tar_url = manifest.get("url") or manifest.get("package_url")
    if not tar_url:
        raise ValueError(f"No package URL in agy manifest: {manifest}")

    use_ver = version or remote_ver
    target_dir = cache_dir / "agy" / use_ver / platform_key
    target_bin = target_dir / ("agy.exe" if os_name == "windows" else "agy")
    if target_bin.is_file() and target_bin.stat().st_size > 0:
        print(f"    [agy] Found cached binary ({target_bin.stat().st_size:,} B) at {target_bin}")
        return target_bin

    target_dir.mkdir(parents=True, exist_ok=True)
    tar_path = target_dir / "cli_pkg.tar.gz"
    download_file(tar_url, tar_path, expected_sha512=manifest.get("sha512"), expected_sha256=manifest.get("sha256"))

    print(f"    [agy] Extracting package: {tar_path.name}")
    if tar_url.endswith(".tar.gz") or tar_url.endswith(".tgz"):
        with tarfile.open(tar_path, "r:gz") as tar:
            for member in tar.getmembers():
                name = os.path.basename(member.name)
                if name in ("agy", "antigravity", "cli", "cli.exe", "agy.exe"):
                    f = tar.extractfile(member)
                    target_bin.write_bytes(f.read())
                    target_bin.chmod(0o755)
                    break
    elif tar_url.endswith(".exe"):
        tar_path.replace(target_bin)
        target_bin.chmod(0o755)

    if not target_bin.is_file():
        raise FileNotFoundError(f"Failed to extract agy binary from {tar_path}")

    # Write manifest snapshot
    (target_dir / "manifest.json").write_text(json.dumps(manifest, indent=2))
    print(f"    [agy] Extracted binary ({target_bin.stat().st_size:,} B) to {target_bin}")
    return target_bin


def fetch_language_server(version: str = None, os_name: str = None, arch: str = None, cache_dir: pathlib.Path = None) -> pathlib.Path:
    """Fetch language_server binary for linux or IDE package for darwin."""
    os_name = normalize_os(os_name)
    arch = normalize_arch(arch)
    cache_dir = cache_dir or DEFAULT_CACHE

    # Query hub updater releases list
    releases_url = f"{HUB_UPDATER_BASE}/releases"
    print(f"    [language_server] Querying Hub releases: {releases_url}")
    req = urllib.request.Request(releases_url, headers={"User-Agent": "openagy-re-fetch/1.0"})
    with urllib.request.urlopen(req, timeout=10) as resp:
        releases = json.loads(resp.read().decode())

    target_release = None
    if version:
        for r in releases:
            if r.get("version") == version:
                target_release = r
                break
        if not target_release:
            print(f"Warning: version {version} not in Hub releases, using latest {releases[0].get('version')}")
            target_release = releases[0]
    else:
        target_release = releases[0]

    rel_version = target_release["version"]
    execution_id = target_release["execution_id"]

    target_dir = cache_dir / "language_server" / rel_version / f"{os_name}_{arch}"
    target_dir.mkdir(parents=True, exist_ok=True)
    target_bin = target_dir / ("language_server.exe" if os_name == "windows" else "language_server")

    if target_bin.is_file() and target_bin.stat().st_size > 0:
        print(f"    [language_server] Found cached binary ({target_bin.stat().st_size:,} B) at {target_bin}")
        return target_bin

    # If OS is Linux, download the server directly from the public release bucket
    if os_name == "linux":
        ls_arch = "arm64" if arch == "arm64" else "x64"
        raw_url = f"{PUBLIC_GCS_BASE}/antigravity-hub/{rel_version}-{execution_id}/unsigned/language_server-linux-{ls_arch}"
        download_file(raw_url, target_bin)
        target_bin.chmod(0o755)
        print(f"    [language_server] Downloaded Linux server ({target_bin.stat().st_size:,} B) to {target_bin}")
        return target_bin

    # For Darwin / Windows: download IDE zip package from IDE updater
    platform_key = "darwin-arm64" if (os_name == "darwin" and arch == "arm64") else ("darwin" if os_name == "darwin" else "win32-x64-user")
    ide_url = f"{IDE_UPDATER_BASE}/api/update/{platform_key}/stable/latest"
    print(f"    [language_server] Querying IDE download URL: {ide_url}")
    req = urllib.request.Request(ide_url, headers={"User-Agent": "openagy-re-fetch/1.0"})
    with urllib.request.urlopen(req, timeout=10) as resp:
        ide_data = json.loads(resp.read().decode())

    pkg_url = ide_data.get("url")
    if not pkg_url:
        raise ValueError(f"No IDE package URL found: {ide_data}")

    zip_path = target_dir / "Antigravity_IDE.zip"
    download_file(pkg_url, zip_path)

    print(f"    [language_server] Extracting language_server from IDE archive...")
    with zipfile.ZipFile(zip_path, "r") as z:
        for name in z.namelist():
            if name.endswith("bin/language_server") or name.endswith("language_server.exe"):
                with z.open(name) as src:
                    target_bin.write_bytes(src.read())
                    target_bin.chmod(0o755)
                break

    if not target_bin.is_file():
        raise FileNotFoundError(f"language_server binary not found inside {zip_path}")

    print(f"    [language_server] Extracted binary ({target_bin.stat().st_size:,} B) to {target_bin}")
    return target_bin


def match_wheel_for_platform(filename: str, os_name: str, arch: str) -> bool:
    fn = filename.lower()
    if os_name == "darwin":
        if "macosx" not in fn:
            return False
        return ("arm64" in fn) if arch == "arm64" else ("x86_64" in fn)
    if os_name == "linux":
        if "manylinux" not in fn and "linux" not in fn:
            return False
        return ("aarch64" in fn or "arm64" in fn) if arch == "arm64" else ("x86_64" in fn or "amd64" in fn)
    if os_name == "windows":
        if "win" not in fn:
            return False
        return ("arm64" in fn) if arch == "arm64" else ("amd64" in fn or "x64" in fn)
    return False


def fetch_localharness(version: str = None, os_name: str = None, arch: str = None, cache_dir: pathlib.Path = None) -> tuple[pathlib.Path, pathlib.Path]:
    """Fetch localharness binary and proto definitions from google-antigravity wheel on PyPI."""
    os_name = normalize_os(os_name)
    arch = normalize_arch(arch)
    cache_dir = cache_dir or DEFAULT_CACHE

    target_version = version or "0.1.23"
    target_dir = cache_dir / "localharness" / target_version / f"{os_name}_{arch}"
    target_dir.mkdir(parents=True, exist_ok=True)
    target_bin = target_dir / ("localharness.exe" if os_name == "windows" else "localharness")
    proto_dir = target_dir / "proto"

    if target_bin.is_file() and target_bin.stat().st_size > 0 and proto_dir.is_dir():
        print(f"    [localharness] Found cached binary ({target_bin.stat().st_size:,} B) at {target_bin}")
        return target_bin, proto_dir

    print(f"    [localharness] Querying PyPI for google-antigravity release metadata...")
    req = urllib.request.Request(PYPI_API_BASE, headers={"User-Agent": "openagy-re-fetch/1.0"})
    with urllib.request.urlopen(req, timeout=10) as resp:
        pypi_data = json.loads(resp.read().decode())

    releases = pypi_data.get("releases", {}).get(target_version)
    if not releases:
        raise ValueError(f"Version {target_version} not found on PyPI")

    matched_release = None
    for r in releases:
        if match_wheel_for_platform(r["filename"], os_name, arch):
            matched_release = r
            break

    if not matched_release:
        available = [r["filename"] for r in releases]
        raise FileNotFoundError(f"No matching wheel for {os_name}_{arch}. Available: {available}")

    whl_name = matched_release["filename"]
    whl_url = matched_release["url"]
    whl_sha256 = matched_release.get("digests", {}).get("sha256")

    whl_path = target_dir / whl_name
    download_file(whl_url, whl_path, expected_sha256=whl_sha256)

    print(f"    [localharness] Unpacking wheel: {whl_name}")
    proto_dir.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(whl_path, "r") as z:
        for name in z.namelist():
            if name.endswith("bin/localharness") or name.endswith("bin/localharness.exe"):
                with z.open(name) as src:
                    target_bin.write_bytes(src.read())
                    target_bin.chmod(0o755)
            elif name.startswith("google/antigravity/proto/") and name.endswith("_pb2.py"):
                base = os.path.basename(name)
                with z.open(name) as src:
                    (proto_dir / base).write_bytes(src.read())

    if not target_bin.is_file():
        raise FileNotFoundError(f"localharness binary not found in {whl_path}")

    print(f"    [localharness] Extracted binary ({target_bin.stat().st_size:,} B) and proto definitions.")
    return target_bin, proto_dir


def main():
    parser = argparse.ArgumentParser(description="Fetch upstream reference binaries for agy, language_server, and localharness.")
    parser.add_argument("--target", choices=["all", "agy", "ls", "localharness"], default="all",
                        help="Target component to fetch (default: all)")
    parser.add_argument("--os", help="Operating system (darwin, linux, windows)")
    parser.add_argument("--arch", help="Architecture (arm64, amd64)")
    parser.add_argument("--version", help="Specific version to fetch")
    parser.add_argument("--cache-dir", help="Custom cache directory")
    args = parser.parse_args()

    cache_dir = pathlib.Path(args.cache_dir).resolve() if args.cache_dir else DEFAULT_CACHE
    print(f"==> Upstream Binary Fetcher initialized. Cache: {cache_dir}")

    if args.target in ("all", "agy"):
        fetch_agy(version=args.version, os_name=args.os, arch=args.arch, cache_dir=cache_dir)

    if args.target in ("all", "ls"):
        fetch_language_server(version=args.version, os_name=args.os, arch=args.arch, cache_dir=cache_dir)

    if args.target in ("all", "localharness"):
        fetch_localharness(version=args.version, os_name=args.os, arch=args.arch, cache_dir=cache_dir)

    print(f"==> All requested downloads completed successfully.")


if __name__ == "__main__":
    main()
