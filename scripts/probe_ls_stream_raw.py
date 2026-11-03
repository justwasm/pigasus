#!/usr/bin/env python3
"""Read one server-streaming method's raw HTTP response off the wire.

The gRPC-Web dialect's end-of-stream is a trailers frame carrying
`grpc-status: 0`; a stream the reference holds open sends *neither* a
message frame nor a trailers frame — the connection simply stays open.
A urllib probe cannot tell those two apart, because both end with an
empty body, and it also loses the gRPC status the reference carries in
the *response headers* (a `Grpc-Status: 2` + `Grpc-Message` pair).

This one reads the raw response with http.client (which de-chunks the
body), prints the headers, and says which of the three happened:

  - a message frame        — the stream produced something
  - a trailers frame       — the stream ended, with a status
  - nothing, still open    — the reference holds the connection open

    probe_ls_stream_raw.py 127.0.0.1 8899 StreamAgentStateUpdates [wait]
"""

import http.client
import struct
import sys
import time

SERVICE = "exa.language_server_pb.LanguageServerService"


def envelope(payload):
    return b"\x00" + struct.pack(">I", len(payload)) + payload


def read_frames(resp, wait):
    """Read the frames the stream produces within `wait` seconds."""
    out = []
    deadline = time.time() + wait
    while time.time() < deadline:
        try:
            chunk = resp.read(65536)
        except Exception:
            break
        if not chunk:
            break
        out.append(chunk)
    return b"".join(out)


def main():
    host, port, method = sys.argv[1], int(sys.argv[2]), sys.argv[3]
    wait = float(sys.argv[4]) if len(sys.argv) > 4 else 5.0
    body = envelope(b"{}")

    conn = http.client.HTTPConnection(host, port, timeout=wait)
    conn.request(
        "POST", f"/{SERVICE}/{method}", body=body,
        headers={
            "Content-Type": "application/grpc-web+json",
            "Accept": "application/grpc-web+json",
        })
    resp = conn.getresponse()
    print("--- STATUS ---")
    print(resp.status, resp.reason)
    print("--- HEADERS ---")
    for key, value in resp.getheaders():
        print(f"{key}: {value}")

    data = read_frames(resp, wait)
    print("--- BODY (%d bytes) ---" % len(data))
    i = 0
    while i + 5 <= len(data):
        flags = data[i]
        ln = struct.unpack(">I", data[i + 1:i + 5])[0]
        payload = data[i + 5:i + 5 + ln]
        kind = "end" if flags & 0x02 else ("trailers" if flags & 0x80 else "msg")
        print(f"frame flags=0x{flags:02x} kind={kind} len={ln} payload={payload[:400]!r}")
        i += 5 + ln
    if i < len(data):
        print("trailing bytes:", data[i:][:200])
    if not data:
        print("(no body bytes — the reference held the connection open)")
    conn.close()
    return 0


if __name__ == "__main__":
    sys.exit(main())
