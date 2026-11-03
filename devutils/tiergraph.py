#!/usr/bin/env python3
"""Measure the internal/ tier layering: upward edges and import cycles."""
import os
import re
import sys
from collections import defaultdict

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MOD = "github.com/justwasm/pigasus"

TIERS = [
    ("internal/core", 1),
    ("internal/features", 2),
    ("internal/infra", 3),
    ("internal/app", 4),
    ("internal/ui", 5),
]
HOSTS = {
    "internal/agent",
    "internal/pigrt",
    "internal/piagentbridge",
    "internal/modular",
    "internal/cli",
    "internal/cliapp",
    "internal/printmode",
}

IMPORT_BLOCK = re.compile(r"^import\s*\((.*?)^\)", re.S | re.M)
IMPORT_LINE = re.compile(r'^\s*(?:[\w.]+\s+)?"([^"]+)"', re.M)
SINGLE = re.compile(r'^import\s+(?:[\w.]+\s+)?"([^"]+)"', re.M)


def tier_of(pkg):
    for prefix, n in TIERS:
        if pkg == prefix or pkg.startswith(prefix + "/"):
            return n
    return None


def is_host(pkg):
    return pkg in HOSTS or any(pkg.startswith(h + "/") for h in HOSTS)


def collect(test_only):
    """Map package dir -> set of imported internal packages."""
    pkgs = {}
    for dirpath, dirnames, filenames in os.walk(os.path.join(ROOT, "internal")):
        dirnames[:] = [d for d in dirnames if d not in (".git", "scratch")]
        gofiles = [f for f in filenames if f.endswith(".go")]
        if not gofiles:
            continue
        if test_only:
            gofiles = [f for f in gofiles if f.endswith("_test.go")]
        else:
            gofiles = [f for f in gofiles if not f.endswith("_test.go")]
        if not gofiles:
            continue
        rel = os.path.relpath(dirpath, ROOT)
        if rel in ("internal",):
            continue
        imports = set()
        for f in gofiles:
            src = open(os.path.join(dirpath, f), encoding="utf-8", errors="replace").read()
            blocks = IMPORT_BLOCK.findall(src) + [m.group(0) for m in SINGLE.finditer(src)]
            for b in blocks:
                for imp in IMPORT_LINE.findall(b):
                    if not imp.startswith(MOD):
                        continue
                    imports.add(imp[len(MOD) + 1:])
        pkgs[rel] = imports
    return pkgs


def scc_count(graph):
    """Tarjan's SCC, iterative."""
    index = {}
    low = {}
    onstack = {}
    stack = []
    result = []
    counter = [0]
    for root in graph:
        if root in index:
            continue
        work = [(root, iter(graph.get(root, ())))]
        index[root] = low[root] = counter[0]
        counter[0] += 1
        stack.append(root)
        onstack[root] = True
        while work:
            node, it = work[-1]
            advanced = False
            for w in it:
                if w not in graph:
                    continue
                if w not in index:
                    index[w] = low[w] = counter[0]
                    counter[0] += 1
                    stack.append(w)
                    onstack[w] = True
                    work.append((w, iter(graph.get(w, ()))))
                    advanced = True
                    break
                if onstack.get(w):
                    low[node] = min(low[node], index[w])
            if advanced:
                continue
            work.pop()
            if work:
                parent = work[-1][0]
                low[parent] = min(low[parent], low[node])
            if low[node] == index[node]:
                comp = []
                while True:
                    w = stack.pop()
                    onstack[w] = False
                    comp.append(w)
                    if w == node:
                        break
                result.append(comp)
    return result


def main():
    prod = collect(test_only=False)
    counts = defaultdict(int)
    upward = []
    host_edges = []
    for pkg, imports in prod.items():
        t = tier_of(pkg)
        for imp in imports:
            it = tier_of(imp)
            if it is None:
                continue
            if t is None:
                continue
            if it > t:
                upward.append((pkg, imp))
            counts[(t, it)] += 1
            if it <= 4 and is_host(imp):
                host_edges.append((pkg, imp))

    names = {1: "core", 2: "features", 3: "infra", 4: "app", 5: "ui"}
    print("=== tier -> tier edge counts (production) ===")
    for t in sorted(names):
        for it in sorted(names):
            c = counts.get((t, it), 0)
            if c:
                same = "  same" if t == it else ""
                print(f"  {names[t]:8s} ({t}) -> {names[it]:8s} ({it}) : {c:4d}{same}")

    print("\n=== upward edges in detail (tier N importing tier M>N) ===")
    for a, b in sorted(upward):
        print(f"  {a} -> {b}")
    if not upward:
        print("  (none)")

    print("\n=== tier -> host edges ===")
    print("(host = agent, pigrt, piagentbridge, modular, cli, cliapp, printmode)")
    for a, b in sorted(host_edges):
        print(f"  {a} -> {b}")
    if not host_edges:
        print("  (none)")

    print("\n=== PRODUCTION graph ===")
    comps = scc_count(prod)
    print(f"packages {len(prod)}  sccs {len(comps)}  cycles {sum(1 for c in comps if len(c) > 1)}")

    print("\n=== TEST graph (test files only) ===")
    testpkgs = collect(test_only=True)
    tcomps = scc_count(testpkgs)
    print(f"packages {len(testpkgs)}  sccs {len(tcomps)}  cycles {sum(1 for c in tcomps if len(c) > 1)}")

    if upward or host_edges:
        sys.exit(1)


if __name__ == "__main__":
    main()
