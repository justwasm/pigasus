#!/usr/bin/env python3
"""merge_go_files.py — merge two Go files in the same package into one.

Both files must be package main (or the same package). The merged file
keeps the first file's package clause and doc comment, unions the import
blocks, and concatenates the bodies.

Usage:
    merge_go_files.py <target> <source> [--out NAME]
"""

import re
import sys


def split_file(path):
    """Return (header, body) where header is everything up to and
    including the import block, and body is the rest."""
    src = open(path).read()
    lines = src.split("\n")
    # find the import block
    imp_start = imp_end = None
    for i, l in enumerate(lines):
        if l.startswith("import ("):
            imp_start = i
            for j in range(i + 1, len(lines)):
                if lines[j] == ")":
                    imp_end = j
                    break
            break
        if l.startswith("import ") and not l.startswith("import ("):
            imp_start = imp_end = i
            break
    if imp_start is None:
        return lines, []
    header = lines[: imp_start + 1]
    imports = []
    if imp_start == imp_end:
        # single-line import
        m = re.match(r'import\s+(.*)', lines[imp_start])
        imports = [m.group(1).strip()]
        body = lines[imp_end + 1 :]
    else:
        for l in lines[imp_start + 1 : imp_end]:
            s = l.strip()
            if not s or s.startswith("//"):
                continue
            imports.append(s)
        body = lines[imp_end + 1 :]
    return header, (imports, body)


def parse_import(entry):
    """Return (alias, path) for one import entry."""
    entry = entry.strip()
    if entry.startswith('"'):
        return "", entry.strip('"')
    m = re.match(r'(?:(\w+|\.|_)\s+)?"(.*)"', entry)
    if not m:
        return "", entry
    return m.group(1) or "", m.group(2)


def main():
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    out_name = None
    for a in sys.argv[1:]:
        if a.startswith("--out="):
            out_name = a.split("=", 1)[1]
    target, source = args[0], args[1]

    t_header, (t_imports, t_body) = split_file(target)
    s_header, (s_imports, s_body) = split_file(source)

    # Union the imports, keeping the target's order then the source's new ones.
    seen = {}
    order = []
    for e in t_imports + s_imports:
        alias, path = parse_import(e)
        key = (alias, path)
        if key in seen:
            continue
        seen[key] = e
        order.append(e)

    # The header is everything before the import block in the target.
    pre = []
    for l in t_header:
        if l.startswith("import ("):
            break
        pre.append(l)

    out = list(pre)
    if len(order) == 1 and parse_import(order[0])[1] and not order[0].startswith("("):
        out.append("import " + order[0])
    else:
        out.append("import (")
        # Keep the stdlib group first, then the third-party group, matching
        # the convention the tree already uses.
        std = [e for e in order if "." not in parse_import(e)[1].split("/")[0]]
        ext = [e for e in order if e not in std]
        for group, sep in ((std, True), (ext, False)):
            if not group:
                continue
            for e in group:
                alias, path = parse_import(e)
                if alias:
                    out.append(f'\t{alias} "{path}"')
                else:
                    out.append(f'\t"{path}"')
            if sep:
                out.append("")
        out.append(")")
    out.append("")

    def clean(body):
        # Drop leading blank lines, keep the rest verbatim.
        i = 0
        while i < len(body) and body[i].strip() == "":
            i += 1
        return body[i:]

    out.extend(clean(t_body))
    out.append("")
    out.extend(clean(s_body))

    result = "\n".join(out)
    if not result.endswith("\n"):
        result += "\n"

    dest = out_name or target
    open(dest, "w").write(result)
    print(f"wrote {dest} ({result.count(chr(10))} lines)")


if __name__ == "__main__":
    main()
