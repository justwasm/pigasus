#!/usr/bin/env python3
"""Render a recovered FileDescriptorProto text dump as proto3 source.

The descriptors in out/local-macos/protos/*.txt are `protoc --decode` dumps of
the FileDescriptorProto blobs the Go protobuf runtime links into the agy
binary. They are the evidence; the sibling .proto files are a reading aid
rendered from them.

    python3 devutils/render_proto.py out/local-macos/protos/prompt_1.4.7.txt

prints the proto3 rendering on stdout. The dump is parsed as the text format
it is — no binary parsing, no guessing — so the rendering can be checked
against the dump by eye, field by field.

What the rendering keeps: message and enum names, field names, numbers,
labels, types, type names, oneofs (including the synthetic ones proto3
`optional` creates), map fields, reserved ranges and names, and the file's
dependencies.

What it drops: field options (the datapol annotations, the custom field
options) and the file options. The .txt dump keeps everything.
"""
import re
import sys

TOKEN = re.compile(r'"(?:[^"\\]|\\.)*"|[A-Za-z0-9_.]+|[{}]|-?\d+')

SCALARS = {
    "TYPE_DOUBLE": "double",
    "TYPE_FLOAT": "float",
    "TYPE_INT64": "int64",
    "TYPE_UINT64": "uint64",
    "TYPE_INT32": "int32",
    "TYPE_FIXED64": "fixed64",
    "TYPE_FIXED32": "fixed32",
    "TYPE_BOOL": "bool",
    "TYPE_STRING": "string",
    "TYPE_BYTES": "bytes",
    "TYPE_UINT32": "uint32",
    "TYPE_SFIXED32": "sfixed32",
    "TYPE_SFIXED64": "sfixed64",
    "TYPE_SINT32": "sint32",
    "TYPE_SINT64": "sint64",
}


def tokenize(text):
    text = re.sub(r"#[^\n]*", "", text)
    return TOKEN.findall(text)


def parse(tokens, i):
    """Parse one message body; returns (dict of key -> list of values, index)."""
    out = {}
    while i < len(tokens):
        tok = tokens[i]
        if tok == "}":
            return out, i + 1
        if tok == "{":
            raise ValueError("unexpected '{'")
        key = tok.rstrip(":")
        i += 1
        if i < len(tokens) and tokens[i] == "{":
            val, i = parse(tokens, i + 1)
            out.setdefault(key, []).append(val)
        else:
            out.setdefault(key, []).append(tokens[i])
            i += 1
    return out, i


def unquote(value):
    if value.startswith('"') and value.endswith('"'):
        return value[1:-1].replace('\\"', '"').replace("\\\\", "\\")
    return value


def one_of(block, key, default=None):
    vals = block.get(key)
    if not vals:
        return default
    val = vals[0]
    if isinstance(val, dict):
        return val
    return unquote(val)


def type_name(field, local):
    """The type as proto3 spells it."""
    kind = one_of(field, "type", "TYPE_STRING")
    if kind in SCALARS:
        return SCALARS[kind]
    full = one_of(field, "type_name", "")
    full = full.lstrip(".")
    if full.startswith(local + "."):
        return full[len(local) + 1:]
    return full


def map_entries(message):
    """The nested messages that are map entries, by their full name."""
    out = {}
    for nested in message.get("nested_type", []):
        options = one_of(nested, "options")
        if options and "map_entry" in options:
            out[one_of(nested, "name")] = nested
    return out


def render_field(field, local, entries, indent):
    name = one_of(field, "name")
    number = one_of(field, "number")
    label = one_of(field, "label", "LABEL_OPTIONAL")
    kind = one_of(field, "type", "TYPE_STRING")
    full = one_of(field, "type_name", "").lstrip(".")
    if kind == "TYPE_MESSAGE" and full.rsplit(".", 1)[-1] in entries:
        entry = entries[full.rsplit(".", 1)[-1]]
        key = next((f for f in entry.get("field", []) if one_of(f, "name") == "key"), None)
        value = next((f for f in entry.get("field", []) if one_of(f, "name") == "value"), None)
        ktype = type_name(key, local) if key else "string"
        vtype = type_name(value, local) if value else "string"
        return f"{indent}map<{ktype}, {vtype}> {name} = {number};"
    prefix = ""
    if label == "LABEL_REPEATED":
        prefix = "repeated "
    elif label == "LABEL_REQUIRED":
        prefix = "required "
    return f"{indent}{prefix}{type_name(field, local)} {name} = {number};"


def render_message(message, local, indent="  "):
    name = one_of(message, "name")
    lines = [f"{indent}message {name} {{"]
    inner = indent + "  "
    entries = map_entries(message)
    oneofs = message.get("oneof_decl", [])
    synthetic = {one_of(o, "name") for o in oneofs}
    by_index = {}
    for field in message.get("field", []):
        idx = one_of(field, "oneof_index")
        if idx is not None:
            by_index.setdefault(int(idx), []).append(field)
    emitted = set()
    for field in message.get("field", []):
        idx = one_of(field, "oneof_index")
        fname = one_of(field, "name")
        if idx is not None:
            decl = oneofs[int(idx)]
            dname = one_of(decl, "name")
            # A synthetic oneof — proto3 `optional` — is spelled `optional`.
            if dname == "_" + fname:
                lines.append(
                    f"{inner}optional {type_name(field, local)} {fname} = {one_of(field, 'number')};"
                )
                emitted.add(id(field))
                continue
            continue
        lines.append(render_field(field, local, entries, inner))
    for idx, decl in enumerate(oneofs):
        dname = one_of(decl, "name")
        fields = by_index.get(idx, [])
        if not fields or dname in synthetic and all(
            dname == "_" + one_of(f, "name") for f in fields
        ):
            continue
        lines.append(f"{inner}oneof {dname} {{")
        for field in fields:
            lines.append(render_field(field, local, entries, inner + "  "))
        lines.append(f"{inner}}}")
    for nested in message.get("nested_type", []):
        if one_of(nested, "name") in entries:
            continue
        lines.extend(render_message(nested, local, inner))
    for enum in message.get("enum_type", []):
        lines.extend(render_enum(enum, inner))
    for rng in message.get("reserved_range", []):
        start = one_of(rng, "start")
        end = one_of(rng, "end")
        lines.append(f"{inner}reserved {start} to {end};")
    for reserved in message.get("reserved_name", []):
        lines.append(f'{inner}reserved "{unquote(reserved)}";')
    lines.append(f"{indent}}}")
    return lines


def render_enum(enum, indent="  "):
    name = one_of(enum, "name")
    lines = [f"{indent}enum {name} {{"]
    for value in enum.get("value", []):
        lines.append(
            f"{indent}  {one_of(value, 'name')} = {one_of(value, 'number')};"
        )
    lines.append(f"{indent}}}")
    return lines


def render(fd):
    package = one_of(fd, "package", "")
    lines = ['syntax = "proto3";', ""]
    if package:
        lines.append(f"package {package};")
    deps = [unquote(d) for d in fd.get("dependency", [])]
    if deps:
        lines.append("")
        for dep in deps:
            lines.append(f'import "{dep}";')
    for enum in fd.get("enum_type", []):
        lines.append("")
        lines.extend(render_enum(enum, ""))
    for message in fd.get("message_type", []):
        lines.append("")
        lines.extend(render_message(message, package, ""))
    return "\n".join(lines) + "\n"


def main(argv):
    if len(argv) != 2:
        print("usage: render_proto.py <descriptor.txt>", file=sys.stderr)
        return 2
    tokens = tokenize(open(argv[1], encoding="utf-8").read())
    fd, _ = parse(tokens, 0)
    sys.stdout.write(render(fd))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
