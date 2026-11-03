#!/usr/bin/env python3
"""Remove top-level Go test functions (and their doc comments) by name."""
import re
import sys

path = sys.argv[1]
names = set(sys.argv[2:])

src = open(path).read().split("\n")
out = []
i = 0
removed = []
while i < len(src):
    line = src[i]
    m = re.match(r"^func (\w+)\(", line)
    if m and m.group(1) in names:
        name = m.group(1)
        # walk back over the doc comment / comment lines already emitted
        j = len(out)
        while j > 0 and out[j - 1].startswith("//"):
            j -= 1
        # drop a trailing blank line before the comment block
        while j > 0 and out[j - 1].strip() == "":
            j -= 1
        del out[j:]
        # skip the function body
        depth = 0
        started = False
        while i < len(src):
            depth += src[i].count("{") - src[i].count("}")
            if "{" in src[i]:
                started = True
            i += 1
            if started and depth <= 0:
                break
        # skip the blank lines that followed
        while i < len(src) and src[i].strip() == "":
            i += 1
        removed.append(name)
        continue
    out.append(line)
    i += 1

open(path, "w").write("\n".join(out))
print("removed:", ", ".join(removed))
