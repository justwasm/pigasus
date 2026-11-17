#!/usr/bin/env python3
"""Extract embedded FileDescriptorProto blobs from a Go binary (macOS/arm64).

The Go protobuf runtime links each descriptor in as a serialised
`FileDescriptorProto`, so a descriptor can be recovered without running the
binary: find the descriptor's `name` field, walk backwards over the length
varint, then walk top-level fields forwards to find the end.

Usage:
    python3 scripts/re/extract_descriptors.py <binary> <proto-file-name>
"""
import sys
from google.protobuf import descriptor_pb2


def read_varint(buf, i):
    shift = 0
    val = 0
    while True:
        b = buf[i]
        val |= (b & 0x7F) << shift
        if not (b & 0x80):
            return val, i + 1
        shift += 7
        i += 1
        if shift > 70:
            raise ValueError("varint too long")


def descriptor_starts(data, name):
    """Offsets where a FileDescriptorProto with this `name` begins."""
    nb = name.encode()
    out = []
    k = data.find(nb)
    while k >= 0:
        i = k - 1
        while i >= 0 and (data[i] & 0x80):
            i -= 1
        try:
            ln, after = read_varint(data, i)
        except Exception:
            k = data.find(nb, k + 1)
            continue
        if after == k and ln == len(nb) and i >= 1 and data[i - 1] == 0x0A:
            out.append(i - 1)
        k = data.find(nb, k + 1)
    return out


def walk_end(buf, start, limit=16_000_000):
    """End offset of the message starting at `start`, by walking top-level fields."""
    n = len(buf)
    i = start
    while i < n and i - start < limit:
        try:
            tag, j = read_varint(buf, i)
        except Exception:
            return i
        field, wt = tag >> 3, tag & 7
        if field == 0:
            return i
        if wt == 0:
            try:
                _, j = read_varint(buf, j)
            except Exception:
                return i
        elif wt == 1:
            j += 8
        elif wt == 2:
            try:
                ln, k = read_varint(buf, j)
            except Exception:
                return i
            j = k + ln
        elif wt == 5:
            j += 4
        else:
            return i
        if j <= i or j > n:
            return i
        i = j
    return i


def extract(data, name):
    for s in descriptor_starts(data, name):
        end = walk_end(data, s)
        fd = descriptor_pb2.FileDescriptorProto()
        try:
            fd.ParseFromString(data[s:end])
        except Exception:
            continue
        if fd.name == name:
            return fd, s, end
    return None, None, None


def render_fd(fd, start, end):
    lines = []
    lines.append(f"# {fd.name}")
    lines.append(f"# package {fd.package}  offset {start:#x}  bytes {end - start}  "
                 f"messages {len(fd.message_type)}  enums {len(fd.enum_type)}")
    for m in fd.message_type:
        lines.append("")
        lines.append("=" * 72)
        lines.append(f"message {m.name} {{")
        for f in m.field:
            t = f.type_name or descriptor_pb2.FieldDescriptorProto.Type.Name(f.type)
            label = "repeated " if f.label == 3 else ""
            lines.append(f"  {f.number:>3} {label}{t} {f.name};")
        lines.append("}")
    for en in fd.enum_type:
        lines.append("")
        lines.append(f"enum {en.name} {{")
        for v in en.value:
            lines.append(f"  {v.name} = {v.number};")
        lines.append("}")
    return "\n".join(lines) + "\n"


def main():
    if len(sys.argv) < 3:
        print("Usage: extract_descriptors.py <binary> <proto-path>", file=sys.stderr)
        sys.exit(1)
    binary = sys.argv[1]
    name = sys.argv[2]
    data = open(binary, "rb").read()
    fd, start, end = extract(data, name)
    if fd is None:
        print(f"Descriptor not found for {name}", file=sys.stderr)
        sys.exit(1)
    sys.stdout.write(render_fd(fd, start, end))


if __name__ == "__main__":
    main()
