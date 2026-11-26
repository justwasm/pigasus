#!/usr/bin/env python3
"""Build a FileDescriptorSet from the google-antigravity wheel's *_pb2.py modules.

The generated modules embed a serialized FileDescriptorProto. We read the
bytes literal straight out of the source (no import needed, so the wheel's
``google.antigravity`` namespace package can never shadow the real
``google.protobuf``) and stitch every file into one FileDescriptorSet that
``protoc --descriptor_set_in=`` can consume.
"""
import ast
import os
import re
import sys

from google.protobuf import descriptor_pb2

WHEEL_PROTO = sys.argv[1]
OUT = sys.argv[2]

SERIALIZED_RE = re.compile(r"AddSerializedFile\((b'(?:[^'\\]|\\.)*')\)")


def load(path):
    src = open(path).read()
    m = SERIALIZED_RE.search(src)
    if not m:
        raise ValueError(f"no serialized descriptor in {path}")
    fdp = descriptor_pb2.FileDescriptorProto()
    fdp.ParseFromString(ast.literal_eval(m.group(1)))
    return fdp


def main():
    fds = descriptor_pb2.FileDescriptorSet()
    for name in sorted(os.listdir(WHEEL_PROTO)):
        if not name.endswith("_pb2.py"):
            continue
        fdp = load(os.path.join(WHEEL_PROTO, name))
        print(f"{fdp.name}\n  package={fdp.package}\n  deps={list(fdp.dependency)}")
        fds.file.append(fdp)
    with open(OUT, "wb") as fh:
        fh.write(fds.SerializeToString())
    print(f"\nwrote {OUT}: {len(fds.file)} files")


if __name__ == "__main__":
    main()
