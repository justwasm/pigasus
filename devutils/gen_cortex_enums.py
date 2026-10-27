#!/usr/bin/env python3
"""Rewrite internal/cortex_pb's enum blocks from the recovered .proto.

The clone's enums were an iota reconstruction: the member names were partly
invented and the numbers were consecutive. The reference's own numbering lives
in out/local-macos/protos/cortex_1.4.6.proto.

Run from the repository root:

    python3 devutils/gen_cortex_enums.py

It rewrites every enum the package already carries, inserts the two enums the
package was missing (the ones the implicit trajectory writes), and emits the
machine-readable EnumMembers table the .proto comparison test reads.
"""
import re
import collections
import sys

PROTO = "out/local-macos/protos/cortex_1.4.6.proto"
GO = "internal/cortex_pb/cortex.pb.go"

# Enums the package does not carry yet, inserted after the named anchor. The
# implicit trajectory writes both of them.
ADDITIONS = {
    "CortexStepSource": "CortexStepStatus",
    "CortexTrajectoryType": "CortexStepStatus",
}

proto = open(PROTO).read()
enums = collections.OrderedDict()
for m in re.finditer(r"enum\s+(\w+)\s*\{([^}]*)\}", proto, re.S):
    name = m.group(1)
    vals = []
    for line in m.group(2).splitlines():
        line = line.split("//")[0].strip()
        mm = re.match(r"([A-Z0-9_]+)\s*=\s*(-?\d+)\s*;", line)
        if mm:
            vals.append((mm.group(1), int(mm.group(2))))
    enums[name] = vals


def camel(name):
    return "".join(p[:1].upper() + p[1:].lower() for p in name.split("_"))


def screaming(name):
    """AgentMode -> AGENT_MODE, McpServerStatus -> MCP_SERVER_STATUS."""
    return re.sub(r"(?<!^)(?=[A-Z])", "_", name).upper()


def members(tname, vals):
    """[(go const name, value, proto member name)] for one enum."""
    pref = screaming(tname) + "_"
    out = []
    for pn, pv in vals:
        sfx = pn[len(pref):] if pn.startswith(pref) else pn
        out.append((f"{tname}{camel(sfx)}", pv, pn))
    return out


def gen(tname, vals):
    consts = members(tname, vals)
    width = max(len(c[0]) for c in consts)
    out = []
    out.append(f"// {tname} is the reference's {tname} enum.")
    out.append("//")
    out.append("// The values are the reference's own numbering and member names,")
    out.append("// taken from the recovered .proto")
    out.append("// (out/local-macos/protos/cortex_1.4.6.proto). The clone's earlier")
    out.append("// reconstruction used consecutive iota values and member names the")
    out.append("// reference does not have; cycle 307 replaced both.")
    out.append(f"type {tname} int32")
    out.append("")
    out.append("const (")
    for cname, pv, _ in consts:
        out.append(f"\t{cname.ljust(width)} {tname} = {pv}")
    out.append(")")
    out.append("")
    out.append(f"func (x {tname}) String() string {{")
    out.append("\tswitch x {")
    for cname, pv, pn in consts:
        if pv == 0 and pn.endswith("UNSPECIFIED"):
            continue
        out.append(f"\tcase {cname}:")
        out.append(f'\t\treturn "{pn}"')
    out.append("\t}")
    zero = next((pn for _, pv, pn in consts if pv == 0), f"{screaming(tname)}_UNSPECIFIED")
    out.append(f'\treturn "{zero}"')
    out.append("}")
    return "\n".join(out) + "\n"


def block_end(src, m, tname):
    """The end offset of the enum block that starts at the type declaration m."""
    cstart = src.find("const (\n", m.end())
    if cstart < 0 or cstart - m.end() > 200:
        print("no const block", file=sys.stderr)
        sys.exit(1)
    cend = src.find(")\n", cstart) + len(")\n")
    end = cend
    tail = src[cend:cend + 400]
    sm = re.search(r"func \([a-z]+ " + re.escape(tname) + r"\) String\(\) string \{\n", tail)
    if sm:
        s = cend + sm.end()
        e = src.find("\n}\n", s)
        end = e + len("\n}\n")
    return cend, end


def doc_start(src, start):
    """The start of the doc comment that precedes the type at start."""
    lines = src[:start].splitlines(keepends=True)
    i = len(lines) - 1
    while i >= 0 and lines[i].startswith("//"):
        i -= 1
    if i + 1 >= len(lines):
        return start
    return sum(len(l) for l in lines[: i + 1])


src = open(GO).read()
changed = []
for tname in list(enums):
    m = re.search(r"type " + re.escape(tname) + r" int32\n", src)
    if not m:
        continue
    _, end = block_end(src, m, tname)
    start = doc_start(src, m.start())
    src = src[:start] + gen(tname, enums[tname]) + src[end:]
    changed.append(tname)

# insert the missing enums after their anchor
for tname, anchor in ADDITIONS.items():
    if re.search(r"type " + re.escape(tname) + r" int32\n", src):
        continue
    m = re.search(r"type " + re.escape(anchor) + r" int32\n", src)
    if not m:
        print(f"anchor {anchor} not found", file=sys.stderr)
        sys.exit(1)
    _, end = block_end(src, m, anchor)
    src = src[:end] + "\n" + gen(tname, enums[tname]) + src[end:]
    changed.append(tname)

# the machine-readable table
written = [t for t in changed]
rows = []
for tname in written:
    rows.append(f'\t"{tname}": {{')
    entries = [(pn, pv) for _, pv, pn in members(tname, enums[tname])]
    width = max(len(f'"{pn}":') for pn, _ in entries)
    for pn, pv in entries:
        key = f'"{pn}":'
        rows.append(f'\t\t{key.ljust(width)} {pv},')
    rows.append("\t},")
table = [
    "// EnumMembers maps each recovered enum type to the reference's own member",
    "// names and their numbers, in the .proto's declaration order. It is the",
    "// machine-readable form of the tables above: the value is the number the",
    "// reference puts on the wire, the key is the member's proto name.",
    "//",
    "// GoalState is absent on purpose — the recovered .proto has no such enum,",
    "// so its members are the clone's own and not the reference's.",
    "var EnumMembers = map[string]map[string]int32{",
    *rows,
    "}",
    "",
]
marker = "// ---------------------------------------------------------------------------\n// File descriptor (recovered).\n"
if marker not in src:
    print("the file-descriptor marker moved; update the generator", file=sys.stderr)
    sys.exit(1)
src = src.replace(marker, "\n".join(table) + "\n" + marker, 1)

open(GO, "w").write(src)
print("rewrote:", ", ".join(changed))
