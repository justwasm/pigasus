#!/usr/bin/env python3
"""Probe one unary method's answer in the Connect dialect.

The reference requires the Connect envelope even for a unary call — a
bare JSON body is answered with "415 Unsupported Media Type" (Connect)
or a protocol error (gRPC-Web).

    probe_ls_unary_connect.py 127.0.0.1 8899 SendTerminalInput '{"terminalId":"nope"}'
"""

import http.client
import json
import struct
import sys

SERVICE = "exa.language_server_pb.LanguageServerService"


def envelope(payload):
    return b"\x00" + struct.pack(">I", len(payload)) + payload


def main():
    host, port, method = sys.argv[1], int(sys.argv[2]), sys.argv[3]
    payload = sys.argv[4] if len(sys.argv) > 4 else "{}"
    ctype = sys.argv[5] if len(sys.argv) > 5 else "application/connect+json"

    conn = http.client.HTTPConnection(host, port, timeout=10)
    conn.request(
        "POST", f"/{SERVICE}/{method}", body=envelope(payload.encode()),
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
    body = resp.read()
    print("--- BODY ---")
    print(repr(body[:800]))
    conn.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
