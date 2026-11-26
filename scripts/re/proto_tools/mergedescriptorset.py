#!/usr/bin/env python3
"""Merge the well-known-type descriptors into the antigravity descriptor set."""
import sys

from google.protobuf import descriptor_pb2

merged = descriptor_pb2.FileDescriptorSet()
merged.ParseFromString(open(sys.argv[1], "rb").read())
extra = descriptor_pb2.FileDescriptorSet()
extra.ParseFromString(open(sys.argv[2], "rb").read())

have = {f.name for f in merged.file}
for f in extra.file:
    if f.name not in have:
        merged.file.append(f)
        have.add(f.name)

with open(sys.argv[3], "wb") as fh:
    fh.write(merged.SerializeToString())
print(f"merged -> {sys.argv[3]}: {len(merged.file)} files")
for f in merged.file:
    print(" ", f.name)
