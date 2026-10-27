#!/usr/bin/env python3
"""macho_strrefs.py — find every code reference to a string literal.

Usage:
    python3 devutils/macho_strrefs.py 0x103555f98 [0x10343ac6b …]
    python3 devutils/macho_strrefs.py --binary ~/.local/bin/agy 0x103555f98

A Go binary materialises the address of a string literal with two
instructions:

    ADRP Xn, <page>      ; the 4 KiB page the literal lives in
    ADD  Xn, Xn, #<off>  ; the offset inside that page

Scanning the executable segment for that pair finds *every* use of a
literal — including the ones Ghidra's reference manager does not
record, because the decompiler folds the pair into a single address
constant and the reference manager then has nothing to attach a
cross-reference to.

The tool prints one line per target address: the address, how many
references were found and the addresses themselves. Pair a target with
the address of a function's entry point and the difference is the
offset *inside* that function, which is how the section order of
`model.SubagentDetailModel.buildContentLines` was recovered — see
docs/cycles/cycle-317-tui-subagent-trajectory/deliverable.md.

【反编译】 / 【strings】 the references are read out of the reference
binary's executable segment; the tool itself reads no symbols.

Limitations:
    * Only the `ADRP`+`ADD` form is matched. A literal reached through
      a pointer table, or through `ADR` (the 21-bit form, used for
      small offsets), is not found.
    * The scan assumes the file is a Mach-O 64-bit image; the Linux
      ELF reference binary needs the `out/local-macos` environment's
      own tooling instead.
"""

import os
import struct
import sys

DEFAULT_BINARY = os.path.expanduser("~/.local/bin/agy")

# The ADRP + ADD pair may sit up to three instructions apart (the
# compiler sometimes materialises the page in one register and adds the
# offset in another a couple of instructions later).
MAX_ADD_DISTANCE = 3


def load_segments(data):
    """Return the Mach-O 64-bit segment table as a list of dicts."""
    magic = struct.unpack_from("<I", data, 0)[0]
    if magic not in (0xFEEDFACE, 0xFEEDFACF):
        raise SystemExit("not a Mach-O 64-bit image (magic %s)" % hex(magic))
    ncmds = struct.unpack_from("<I", data, 16)[0]
    off = 32
    segs = []
    for _ in range(ncmds):
        cmd, cmdsize = struct.unpack_from("<II", data, off)
        if cmd == 0x19:  # LC_SEGMENT_64
            name = data[off + 8 : off + 24].rstrip(b"\x00").decode()
            vmaddr, vmsize, fileoff, filesize = struct.unpack_from("<QQQQ", data, off + 24)
            _, initprot = struct.unpack_from("<II", data, off + 56)
            segs.append(
                {
                    "name": name,
                    "vmaddr": vmaddr,
                    "vmsize": vmsize,
                    "fileoff": fileoff,
                    "filesize": filesize,
                    "initprot": initprot,
                }
            )
        off += cmdsize
    return segs


def executable_segment(segs):
    """Return the segment holding the executable text."""
    for seg in segs:
        if seg["name"] == "__TEXT" and seg["initprot"] & 4:  # PROT_EXEC
            return seg
    raise SystemExit("no executable segment")


def sign_extend(value, bits):
    if value & (1 << (bits - 1)):
        value -= 1 << bits
    return value


def scan(data, segs):
    """Yield (code address, referenced address) for every ADRP+ADD pair."""
    seg = executable_segment(segs)
    base = seg["fileoff"]
    end = base + seg["filesize"]
    words = struct.unpack_from("<%dI" % ((end - base) // 4), data, base)
    vm = seg["vmaddr"]
    for i, word in enumerate(words):
        # ADRP: op=1, immlo, 10000, immhi, Rd
        if (word & 0x9F000000) != 0x90000000:
            continue
        immlo = (word >> 29) & 0x3
        immhi = (word >> 5) & 0x7FFFF
        imm = sign_extend((immhi << 2) | immlo, 21) << 12
        rd = word & 0x1F
        pc = vm + i * 4
        page = (pc & ~0xFFF) + imm
        for j in range(1, MAX_ADD_DISTANCE + 1):
            if i + j >= len(words):
                break
            add = words[i + j]
            # ADD (immediate): sf 1 0 0 010 0 0 imm12 Rn Rd
            if (add & 0xFF800000) != 0x91000000:
                continue
            if ((add >> 5) & 0x1F) != rd:
                continue
            yield pc, page + ((add >> 10) & 0xFFF)


def main(argv):
    binary = DEFAULT_BINARY
    args = list(argv)
    if args and args[0] == "--binary":
        binary = os.path.expanduser(args[1])
        args = args[2:]
    if not args:
        raise SystemExit(__doc__)
    with open(binary, "rb") as fh:
        data = fh.read()
    segs = load_segments(data)
    targets = {}
    for arg in args:
        target = int(arg, 16)
        targets[target] = []
    for pc, target in scan(data, segs):
        if target in targets:
            targets[target].append(pc)
    for target in sorted(targets):
        addrs = targets[target]
        print(hex(target), len(addrs), [hex(a) for a in addrs[:64]])


if __name__ == "__main__":
    main(sys.argv[1:])
