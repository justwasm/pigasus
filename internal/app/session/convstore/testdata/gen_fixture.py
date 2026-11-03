#!/usr/bin/env python3
"""Regenerate transcript.db, the fixture used by agy_test.go.

The fixture mimics a real reference conversation: the same table schema, and
step_payload blobs built with the same field numbers the reference uses
(recovered from ~/.gemini/antigravity-cli/conversations/*.db). Keeping the
generator in the repository means the fixture can be rebuilt and audited
instead of being an opaque binary.

    python3 gen_fixture.py

Requires the sqlite3 CLI (any version).
"""
import os
import sqlite3
import subprocess
import sys
import tempfile


def varint(n: int) -> bytes:
    out = bytearray()
    while True:
        b = n & 0x7F
        n >>= 7
        if n:
            out.append(b | 0x80)
        else:
            out.append(b)
            return bytes(out)


def tag(num: int, wire: int) -> bytes:
    return varint(num << 3 | wire)


def ld(num: int, payload: bytes) -> bytes:
    return tag(num, 2) + varint(len(payload)) + payload


def vi(num: int, value: int) -> bytes:
    return tag(num, 0) + varint(value)


def user_payload(text: str) -> bytes:
    """14: 19 -> 3 -> 1 (text)."""
    return vi(1, 14) + vi(4, 3) + ld(19, ld(3, ld(1, text.encode())))


def model_payload(text: str) -> bytes:
    """15: 20 -> 1 (text)."""
    return vi(1, 15) + vi(4, 3) + ld(20, ld(1, text.encode()))


def tool_payload(name: str, args: str) -> bytes:
    """132: 5 -> 4 -> {2: name, 3: arguments}."""
    return vi(1, 132) + vi(4, 5) + ld(5, ld(4, ld(2, name.encode()) + ld(3, args.encode())))


def error_payload(short: str, long_msg: str) -> bytes:
    """17: 24 -> 3 -> {1: short, 2: long}."""
    return vi(1, 17) + vi(4, 5) + ld(24, ld(3, ld(1, short.encode()) + ld(2, long_msg.encode())))


def system_payload(text: str) -> bytes:
    """101: 24 -> 3 -> {1: message}."""
    return vi(1, 101) + vi(4, 5) + ld(24, ld(3, ld(1, text.encode())))


def empty_model_payload() -> bytes:
    """A generation that produced no text: 20 present but empty."""
    return vi(1, 15) + vi(4, 3) + ld(20, b"")


def main() -> int:
    here = os.path.dirname(os.path.abspath(__file__))
    out = os.path.join(here, "transcript.db")
    if os.path.exists(out):
        os.remove(out)

    steps = [
        (0, 14, 3, user_payload("summarise the repository layout")),
        (1, 15, 3, model_payload("I will start by listing the top level.")),
        (2, 132, 5, tool_payload("run_command", '{"CommandLine":"ls","Cwd":"/tmp"}')),
        (3, 15, 3, empty_model_payload()),
        (4, 17, 5, error_payload("quota reached", "RESOURCE_EXHAUSTED (code 429): quota reached")),
        (5, 101, 5, system_payload("[Notice] background tasks were stopped.")),
        (6, 14, 3, user_payload("now do the same for internal/")),
        (7, 23, 3, b""),
        (8, 15, 3, model_payload("Done: internal/ has these packages.")),
    ]

    tmp = tempfile.mkdtemp()
    path = os.path.join(tmp, "transcript.db")
    con = sqlite3.connect(path)
    con.executescript(
        """
        CREATE TABLE trajectory_meta (trajectory_id text, cascade_id text,
            trajectory_type integer, source integer, PRIMARY KEY (trajectory_id));
        CREATE TABLE steps (idx integer, step_type integer NOT NULL DEFAULT 0,
            status integer NOT NULL DEFAULT 0, has_subtrajectory numeric NOT NULL DEFAULT false,
            metadata blob, error_details blob, permissions blob, task_details blob,
            render_info blob, step_payload blob, step_format integer NOT NULL DEFAULT 0,
            PRIMARY KEY (idx));
        """
    )
    con.execute(
        "INSERT INTO trajectory_meta VALUES (?,?,?,?)",
        ("traj-fixture", "cafe0000-0000-4000-8000-000000000001", 4, 17),
    )
    for idx, stype, status, payload in steps:
        con.execute(
            "INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?,?,?,?)",
            (idx, stype, status, payload if payload else None),
        )
    con.commit()
    con.close()

    # VACUUM rewrites the file so the page layout is deterministic.
    subprocess.run(["sqlite3", path, "VACUUM;"], check=True)
    with open(path, "rb") as f:
        data = f.read()
    with open(out, "wb") as f:
        f.write(data)
    print(f"wrote {out} ({len(data)} bytes, {len(steps)} steps)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
