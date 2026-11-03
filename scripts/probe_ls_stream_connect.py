#!/usr/bin/env python3
"""Read one server-streaming method's answer in the Connect dialect.

The gRPC-Web dialect and the Connect dialect answer the same failure
differently: gRPC-Web carries the status in the response headers
(`Grpc-Status` / `Grpc-Message`), Connect carries it in the HTTP
status and a JSON body (`{"code":"…","message":"…"}`).

The cloud's v2 mode asks for `application/proto` and the IDE's own
workbench asks for `application/grpc-web+json`, so the clone has to
answer both the way the reference answers them.

    probe_ls_stream_connect.py 127.0.0.1 8899 StreamCascadeReactiveUpdates [wait]
"""

import http.client
import json
import struct
import sys
import time

SERVICE = "exa.language_server_pb.LanguageServerService"


def envelope(payload):
    return b"\x00" + struct.pack(">I", len(payload)) + payload


def read_all(resp, wait):
    out = []
    deadline = time.time() + wait
    while time.time() < deadline:
        try:
            chunk = resp.read1(65536)
        except Exception:
            break
        if not chunk:
            break
        out.append(chunk)
    return b"".join(out)


def main():
    host, port, method = sys.argv[1], int(sys.argv[2]), sys.argv[3]
    wait = float(sys.argv[4]) if len(sys.argv) > 4 else 5.0
    ctype = sys.argv[5] if len(sys.argv) > 5 else "application/connect+json"

    body = envelope(b"{}")
    conn = http.client.HTTPConnection(host, port, timeout=wait)
    conn.request(
        "POST", f"/{SERVICE}/{method}", body=body,
        headers={
            "Content-Type": ctype,
            "Accept": ctype,
            "Connect-Protocol-Version": "1",
        })
    resp = conn.getresponse()
    print("--- STATUS ---")
    print(resp.status, resp.reason)
    print("--- HEADERS ---")
    for key, value in resp.getheaders():
        print(f"{key}: {value}")
    data = read_all(resp, wait)
    print("--- BODY (%d bytes) ---" % len(data))
    print(repr(data[:600]))
    conn.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
