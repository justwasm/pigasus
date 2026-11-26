#!/usr/bin/env python3
"""Decode protobuf generated *_pb2.py modules back into .proto source.

This is the inverse of protoc: the generated modules embed a serialized
FileDescriptorProto, which we print back out as .proto text.
"""
import sys
import importlib.util
import os

from google.protobuf import descriptor_pb2

FD = descriptor_pb2.FieldDescriptorProto


def load_pb2(path):
    """Extract the serialized FileDescriptorProto without importing.

    The generated modules import each other by absolute package path
    (``google.antigravity.proto.*``), which only works when the wheel root is
    on ``sys.path`` *and* the ``google`` namespace package is not already
    bound to something else. Reading the bytes literal straight out of the
    source avoids both problems.
    """
    src = open(path).read()
    import ast
    import re
    m = re.search(r"AddSerializedFile\((b'(?:[^'\\]|\\.)*')\)", src)
    if not m:
        raise ValueError(f"no serialized descriptor in {path}")
    data = ast.literal_eval(m.group(1))
    fdp = descriptor_pb2.FileDescriptorProto()
    fdp.ParseFromString(data)
    return fdp


def type_name(field):
    if field.type in (FD.TYPE_DOUBLE, FD.TYPE_FLOAT, FD.TYPE_INT64, FD.TYPE_UINT64,
                      FD.TYPE_INT32, FD.TYPE_FIXED64, FD.TYPE_FIXED32, FD.TYPE_UINT32,
                      FD.TYPE_SFIXED32, FD.TYPE_SFIXED64, FD.TYPE_SINT32, FD.TYPE_SINT64):
        return field.type_name.split(".")[-1]
    if field.type == FD.TYPE_BOOL:
        return "bool"
    if field.type == FD.TYPE_STRING:
        return "string"
    if field.type == FD.TYPE_BYTES:
        return "bytes"
    if field.type == FD.TYPE_MESSAGE or field.type == FD.TYPE_ENUM:
        return field.type_name
    return "unknown"


def is_map(field, msg):
    return (field.type == FD.TYPE_MESSAGE
            and field.type_name.endswith(".google.protobuf.Struct")
            and False) or field.label == FD.LABEL_REPEATED and _is_map_entry(msg, field)


def _is_map_entry(msg, field):
    for nested in msg.nested_type:
        if nested.options.map_entry:
            if nested.name == field.type_name.split(".")[-1]:
                return True
    return False


def print_enum(enum, indent=""):
    out = []
    out.append(f"{indent}enum {enum.name} {{")
    for v in enum.value:
        out.append(f"{indent}  {v.name} = {v.number};")
    out.append(f"{indent}}}")
    return out


def print_message(msg, indent=""):
    out = []
    out.append(f"{indent}message {msg.name} {{")
    inner = indent + "  "
    for nested in msg.nested_type:
        if nested.options.map_entry:
            continue
        out.extend(print_message(nested, inner))
    for enum in msg.enum_type:
        out.extend(print_enum(enum, inner))
    map_entries = {n.name: n for n in msg.nested_type if n.options.map_entry}

    # group fields by oneof
    oneof_fields = {}
    for f in msg.field:
        if f.HasField("oneof_index"):
            oneof_fields.setdefault(f.oneof_index, []).append(f)

    emitted_oneofs = set()
    for f in msg.field:
        if f.HasField("oneof_index"):
            idx = f.oneof_index
            if idx in emitted_oneofs:
                continue
            emitted_oneofs.add(idx)
            out.append(f"{inner}oneof {msg.oneof_decl[idx].name} {{")
            for of in oneof_fields[idx]:
                out.append(f"{inner}  {type_name(of)} {of.name} = {of.number};")
            out.append(f"{inner}}}")
            continue
        if f.type == FD.TYPE_MESSAGE and f.type_name.split(".")[-1] in map_entries:
            entry = map_entries[f.type_name.split(".")[-1]]
            k = entry.field[0]
            v = entry.field[1]
            out.append(f"{inner}map<{type_name(k)}, {type_name(v)}> {f.name} = {f.number};")
            continue
        label = ""
        if f.label == FD.LABEL_REPEATED:
            label = "repeated "
        elif f.label == FD.LABEL_OPTIONAL and f.proto3_optional:
            label = "optional "
        out.append(f"{inner}{label}{type_name(f)} {f.name} = {f.number};")
    out.append(f"{indent}}}")
    return out


def print_file(fdp):
    out = []
    out.append(f"// file: {fdp.name}")
    out.append("syntax = \"" + fdp.syntax + "\";")
    out.append("")
    out.append(f"package {fdp.package};")
    out.append("")
    if fdp.HasField("options") and fdp.options.go_package:
        out.append(f'option go_package = "{fdp.options.go_package}";')
        out.append("")
    for dep in fdp.dependency:
        out.append(f'import "{dep}";')
    if fdp.dependency:
        out.append("")
    for enum in fdp.enum_type:
        out.extend(print_enum(enum))
        out.append("")
    for msg in fdp.message_type:
        out.extend(print_message(msg))
        out.append("")
    for svc in fdp.service:
        out.append(f"service {svc.name} {{")
        for m in svc.method:
            out.append(f"  rpc {m.name} ({m.input_type}) returns ({m.output_type});")
        out.append("}")
        out.append("")
    return "\n".join(out)


def main():
    outdir = sys.argv[1]
    os.makedirs(outdir, exist_ok=True)
    # The generated modules import each other by package path, so the wheel
    # root has to be importable.
    for path in sys.argv[2:]:
        fdp = load_pb2(path)
        dest = os.path.join(outdir, os.path.basename(path).replace("_pb2.py", ".proto"))
        with open(dest, "w") as fh:
            fh.write(print_file(fdp))
        print(f"{os.path.basename(path)} -> {dest} "
              f"({len(fdp.message_type)} msgs, {len(fdp.enum_type)} enums)")


if __name__ == "__main__":
    main()
