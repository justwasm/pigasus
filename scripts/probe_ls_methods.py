#!/usr/bin/env python3
"""Probe every LanguageServerService method of a language server.

The Antigravity language server exposes 351 methods on
`exa.language_server_pb.LanguageServerService`. This tool asks a live
instance for every one of them and records the answer, so the clone's
coverage can be measured against the reference's instead of guessed.

    scripts/probe_ls_methods.py http://127.0.0.1:8811 answers.json

The method list and the streaming flag come from the descriptor set in
`out/ide-language-server/protos/language_server.protoset`; the request
bodies are the *default instance* of each request message, so a method
that needs an argument is probed with that argument absent (the
reference's own error is then part of the answer).

Every method is called in the gRPC-Web dialect — the one the IDE's own
workbench speaks: the request carries its message inside an envelope
and the answer ends in a trailers envelope that carries the status. A
stream that produces nothing is recorded as an empty stream — that is
what the reference does for a cascade it has no state for.

Volatile values (uuids, tracing ids, timestamps, ports) are
normalised to placeholders so two runs can be compared byte for byte.
"""

import json
import os
import re
import struct
import sys
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor

PROTOSET = "out/ide-language-server/protos/language_server.protoset"
SERVICE = "exa.language_server_pb.LanguageServerService"


# --------------------------------------------------------------------------
# descriptor set


def _varint(b, i):
    r = s = 0
    while True:
        x = b[i]
        i += 1
        r |= (x & 0x7F) << s
        if not x & 0x80:
            return r, i
        s += 7


def _fields(b):
    i = 0
    while i < len(b):
        key, i = _varint(b, i)
        fn, wt = key >> 3, key & 7
        if wt == 0:
            v, i = _varint(b, i)
            yield fn, wt, v
        elif wt == 2:
            n, i = _varint(b, i)
            yield fn, wt, b[i:i + n]
            i += n
        elif wt == 5:
            yield fn, wt, b[i:i + 4]
            i += 4
        elif wt == 1:
            yield fn, wt, b[i:i + 8]
            i += 8
        else:
            return


def _s(b):
    return b.decode("utf-8", "replace")


def methods(path):
    """Return {method: streaming} for the language server service."""
    data = open(path, "rb").read()
    out = {}
    for fn, wt, val in _fields(data):
        if fn != 1 or wt != 2:
            continue
        pkg = ""
        svc_name = ""
        svc_methods = {}
        for f2, w2, v2 in _fields(val):
            if f2 == 2 and w2 == 2:
                pkg = _s(v2)
            elif f2 == 6 and w2 == 2:
                for f3, w3, v3 in _fields(v2):
                    if f3 == 1 and w3 == 2:
                        svc_name = _s(v3)
                    elif f3 == 2 and w3 == 2:
                        mname = ""
                        streaming = False
                        for f4, w4, v4 in _fields(v3):
                            if f4 == 1 and w4 == 2:
                                mname = _s(v4)
                            elif f4 == 5:
                                streaming = True
                        svc_methods[mname] = streaming
        if f"{pkg}.{svc_name}" == SERVICE:
            out = svc_methods
    return out


# --------------------------------------------------------------------------
# normalisation

NORMALISERS = [
    (re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"), "<uuid>"),
    (re.compile(r"error ID: [0-9a-f]{32}"), "error ID: <tracingId>"),
    (re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z"), "<timestamp>"),
    (re.compile(r"\b\d{10,13}\b"), "<epoch>"),
]


def normalise(text):
    for pattern, repl in NORMALISERS:
        text = pattern.sub(repl, text)
    return text


# --------------------------------------------------------------------------
# calls


def _envelope(payload):
    return b"\x00" + struct.pack(">I", len(payload)) + payload


def _frames(data):
    out = []
    i = 0
    while i + 5 <= len(data):
        flags = data[i]
        ln = struct.unpack(">I", data[i + 1:i + 5])[0]
        out.append((flags, data[i + 5:i + 5 + ln]))
        i += 5 + ln
    return out


def call(base, method, streaming, timeout=4):
    url = f"{base}/{SERVICE}/{method}"
    body = json.dumps({}).encode()
    # The IDE's own web bundle speaks gRPC-Web, not Connect: the
    # request carries its message inside an envelope and the answer
    # ends in a trailers envelope that carries the status. The plain
    # JSON dialect masks a failed call as an empty body, so it cannot
    # tell "answered" from "not implemented" — the gRPC-Web dialect
    # can.
    data = _envelope(body)
    ctype = "application/grpc-web+json"
    req = urllib.request.Request(url, data=data, headers={"Content-Type": ctype})
    headers = {}
    held = False
    try:
        resp = urllib.request.urlopen(req, timeout=timeout)
    except urllib.error.HTTPError as e:
        payload = e.read()
        ctype = e.headers.get("Content-Type", "")
        status = e.code
        headers = {k.lower(): v for k, v in e.headers.items()}
    except Exception as e:  # connection refused
        return {"status": 0, "error": str(e)}
    else:
        ctype = resp.headers.get("Content-Type", "")
        status = resp.status
        headers = {k.lower(): v for k, v in resp.headers.items()}
        try:
            payload = resp.read()
        except Exception:
            # The stream is open and has nothing to say: a
            # subscription that waits for an event. The reference
            # holds the connection open for the streams it has
            # nothing to report on.
            payload = b""
            held = True
        finally:
            resp.close()
    if "grpc-web" in ctype:
        frames = []
        for flags, payload_frame in _frames(payload):
            kind = "end" if flags & 0x02 else ("trailers" if flags & 0x80 else "msg")
            frames.append([kind, payload_frame.decode("utf-8", "replace")])
        answer = {"status": status, "frames": frames}
        if held:
            answer["held_open"] = True
        if "grpc-status" in headers:
            answer["grpc_status"] = headers["grpc-status"]
        if "grpc-message" in headers:
            answer["grpc_message"] = normalise(headers["grpc-message"])
        return answer
    return {"status": status, "body": payload.decode("utf-8", "replace")}


def main():
    base = sys.argv[1].rstrip("/")
    out_path = sys.argv[2] if len(sys.argv) > 2 else "-"
    table = methods(PROTOSET)
    if not table:
        print("no methods found in", PROTOSET, file=sys.stderr)
        return 1
    out = {}
    if out_path != "-" and os.path.exists(out_path):
        out = json.load(open(out_path))
    todo = [m for m in sorted(table) if m not in out]
    with ThreadPoolExecutor(max_workers=4) as pool:
        futures = {
            pool.submit(call, base, method, table[method]): method
            for method in todo
        }
        for future, method in futures.items():
            answer = future.result()
            answer["streaming"] = table[method]
            if "body" in answer:
                answer["body"] = normalise(answer["body"])
            if "frames" in answer:
                answer["frames"] = [[k, normalise(v)] for k, v in answer["frames"]]
            out[method] = answer
    if out_path == "-":
        json.dump(out, sys.stdout, ensure_ascii=False, indent=1, sort_keys=True)
        print()
    else:
        json.dump(out, open(out_path, "w"), ensure_ascii=False, indent=1, sort_keys=True)
        print(f"{len(out)}/{len(table)} methods recorded in {out_path}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
