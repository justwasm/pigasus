#!/usr/bin/env python3
"""Build decompile reports from the local macOS run.

Trimmed derivative of out/decompile/build_reports.py: same package-splitting
heuristic and same per-package table format, but the hardcoded `/workspace/agy`
paths and the hand-written findings block (which described the Linux/ELF pass)
are replaced with what this run actually produced.
"""
import hashlib
import os
import re
import subprocess
import sys
from collections import defaultdict

BASE = os.path.dirname(os.path.abspath(__file__))
PRIORITY_FILE = os.path.join(BASE, "priority-funcs.txt")
DEC_DIR = os.path.join(BASE, "dec")
SEEDS_FILE = os.path.join(BASE, "seeds", "seeds.txt")
SUMMARY_FILE = os.path.join(DEC_DIR, "_summary.txt")
OUT_DIR = BASE
BINARY = os.path.expanduser("~/.local/bin/agy")


def seed_count() -> int:
    """How many functions the seed tool recovered from the binary's pclntab."""
    try:
        with open(SEEDS_FILE, errors="replace") as fh:
            return sum(1 for ln in fh if ln.strip())
    except OSError:
        return 0


def decompile_summary() -> dict:
    """What the Ghidra pass recorded, so the report quotes the run it describes.

    The numbers used to be hardcoded, which is how a report ended up claiming
    "56 s" for a run that had not happened yet. They are read back out of the
    decompile pass's own summary instead.
    """
    out = {"processed": 0, "skipped": 0, "errors": 0, "elapsed": ""}
    try:
        with open(SUMMARY_FILE, errors="replace") as fh:
            for ln in fh:
                ln = ln.strip()
                if ln.startswith("# Total processed:"):
                    out["processed"] = int(ln.rsplit(":", 1)[1])
                elif ln.startswith("# Total skipped:"):
                    out["skipped"] = int(ln.rsplit(":", 1)[1])
                elif ln.startswith("# Total errors:"):
                    out["errors"] = int(ln.rsplit(":", 1)[1])
                elif ln.startswith("# Elapsed:"):
                    out["elapsed"] = ln.rsplit(":", 1)[1].strip()
    except OSError:
        pass
    return out


def package_of(name: str) -> str:
    """pkg.(*Type).Method -> pkg ; pkg.Func -> pkg."""
    if "(" in name:
        return name[: name.find("(")]
    return name.rsplit(".", 1)[0]


def type_of(name: str) -> str:
    if "(" in name and ")" in name:
        idx = name.find(")")
        if idx > 0:
            return name[: idx + 1]
    return "<package>"


def main():
    binary_path = sys.argv[1] if len(sys.argv) > 1 else BINARY

    priority, seen = [], set()
    for ln in open(PRIORITY_FILE):
        ln = ln.strip()
        if not ln or ln.startswith("#"):
            continue
        parts = ln.split("\t")
        if len(parts) < 3 or parts[1] != "function":
            continue
        addr, name = parts[0], parts[2]
        hex_addr = addr[2:] if addr.startswith("0x") else addr
        if hex_addr in seen:
            continue
        seen.add(hex_addr)
        priority.append((hex_addr, name, package_of(name), type_of(name),
                         name.rsplit(".", 1)[-1] if "." in name else name))

    by_pkg = defaultdict(list)
    for h, n, p, t, s in priority:
        by_pkg[p].append((h, n, t, s))

    dec_files = {}
    if os.path.isdir(DEC_DIR):
        for fn in os.listdir(DEC_DIR):
            if not fn.endswith(".c"):
                continue
            base = fn[:-2]
            idx = base.find("_")
            if idx > 0:
                dec_files[base[:idx]] = fn

    total_funcs = len(priority)
    decompiled = sum(1 for h, *_ in priority if h in dec_files)
    summary = decompile_summary()
    total_loc = 0
    for h, name, pkg, tp, short in priority:
        if h in dec_files:
            total_loc += open(os.path.join(DEC_DIR, dec_files[h]),
                              errors="ignore").read().count("\n")

    try:
        sha = subprocess.run(["shasum", "-a", "256", binary_path],
                             capture_output=True, text=True).stdout.split()[0]
        ver = subprocess.run([binary_path, "--version"],
                             capture_output=True, text=True).stdout.strip()
    except Exception:
        sha, ver = "?", "?"

    with open(os.path.join(OUT_DIR, "decompile-summary.md"), "w") as f:
        f.write("# Decompile summary — agy (macOS/arm64, local run)\n\n")
        f.write(f"> Target: `{binary_path}` (Mach-O arm64, agy {ver})\n")
        f.write(f"> SHA-256: `{sha}`\n")
        f.write(f"> Go toolchain: `go1.28-20260921-RC02` (Google internal, "
                "tags `fieldtrack,boringcrypto,simd`)\n")
        f.write(f"> Decompiler: Ghidra 12.1.4, imported with `-noanalysis`, "
                "native decompiler built for mac_arm_64\n")
        f.write(f"> Priority list: `{PRIORITY_FILE}` "
                f"({total_funcs} funcs across {len(by_pkg)} Go packages)\n")
        f.write(f"> Output: `{DEC_DIR}/<HEX>_<shortname>.c`\n\n")

        f.write("## Headline numbers\n\n")
        f.write("| Metric | Value |\n|---|---|\n")
        f.write(f"| Functions recovered from pclntab | {seed_count():,} |\n")
        f.write(f"| Priority addresses resolved in the project | "
                f"{summary['processed'] - summary['skipped']:,}/{total_funcs:,} "
                f"(**{summary['skipped']} unresolved**) |\n")
        f.write(f"| Priority funcs decompiled | {decompiled}/{total_funcs} |\n")
        f.write(f"| Decompiled LOC | {total_loc:,} |\n")
        f.write(f"| Decompile wall time | {summary['elapsed'] or 'n/a'} |\n\n")

        f.write("## All packages\n\n")
        f.write("| Functions | Decompiled | LOC | Package |\n|---:|---:|---:|---|\n")
        rows = []
        for pkg, fns in by_pkg.items():
            pkg_dec = sum(1 for h, *_ in fns if h in dec_files)
            pkg_loc = sum(
                open(os.path.join(DEC_DIR, dec_files[h]), errors="ignore").read().count("\n")
                for h, *_ in fns if h in dec_files
            )
            rows.append((len(fns), pkg_dec, pkg_loc, pkg))
        for n, d, loc, pkg in sorted(rows, key=lambda x: -x[0]):
            f.write(f"| {n} | {d} | {loc:,} | `{pkg}` |\n")
        f.write("\n")

    written = {"decompile-summary.md"}
    for pkg, fns in sorted(by_pkg.items()):
        slug = re.sub(r"[^A-Za-z0-9._-]+", "_", pkg).strip("_") or "root"
        if len(slug) > 70:
            # Truncating alone silently collides: `...codeassistclient.yield...`
            # packages share a 70-char prefix. Disambiguate with a digest of the full name.
            digest = hashlib.sha1(pkg.encode()).hexdigest()[:8]
            slug = f"{slug[:60]}_{digest}"
        report_name = f"decompile-{slug}.md"
        written.add(report_name)
        with open(os.path.join(OUT_DIR, report_name), "w") as f:
            dec = [(h, n, t, s) for h, n, t, s in fns if h in dec_files]
            pend = [(h, n, t, s) for h, n, t, s in fns if h not in dec_files]
            f.write(f"# Decompile report — `{pkg}`\n\n")
            f.write(f"> {len(fns)} priority functions\n\n")
            f.write(f"**Decompiled:** {len(dec)}/{len(fns)}\n\n")
            by_type = defaultdict(list)
            for h, n, t, s in dec:
                by_type[t].append((h, n, s))
            for t, tf in sorted(by_type.items()):
                f.write(f"## `{t}`\n\n_{len(tf)} methods decompiled._\n\n")
                f.write("| Addr | Function | Decompile file |\n|---|---|---|\n")
                for h, n, s in sorted(tf, key=lambda x: x[0]):
                    fn = dec_files.get(h, "?")
                    f.write(f"| `0x{h}` | `{n}` | [`{fn}`](dec/{fn}) |\n")
                f.write("\n")
            if pend:
                f.write(f"## Pending ({len(pend)})\n\n")
                for h, n, _, _ in pend[:100]:
                    f.write(f"- `0x{h}` — `{n}`\n")

    # Prune reports this run did not write. A package that disappeared from the
    # release (main.deleteAgentMessageFn is gone in 1.4.5) would otherwise leave
    # its old report behind, citing addresses that no longer exist.
    pruned = []
    for fn in sorted(os.listdir(OUT_DIR)):
        if fn.startswith("decompile-") and fn.endswith(".md") and fn not in written:
            os.remove(os.path.join(OUT_DIR, fn))
            pruned.append(fn)

    print(f"summary + {len(by_pkg)} per-package reports -> {OUT_DIR}")
    print(f"{decompiled}/{total_funcs} decompiled, {total_loc:,} LOC")
    if pruned:
        print(f"pruned {len(pruned)} stale report(s): {', '.join(pruned)}")


if __name__ == "__main__":
    main()
