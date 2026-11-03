#!/usr/bin/env python3
"""Read every server-streaming method's raw HTTP response off the wire.

The urllib probe (probe_ls_methods.py) loses two things a raw read
keeps: the gRPC status the reference carries in the *response headers*
(`Grpc-Status` + `Grpc-Message`), and the difference between a stream
the reference ends and one it holds open — both end with an empty body
to urllib, but only the first one carries a status.

This one classifies each stream into exactly one of:

  message    — the stream produced a frame
  trailers   — the stream ended, with a status (from the headers or
               from a trailers frame)
  held-open  — the reference holds the connection open and says nothing

    probe_ls_streams_raw.py 127.0.0.1 8899 [wait]
"""

import http.client
import json
import socket
import struct
import sys
import time

SERVICE = "exa.language_server_pb.LanguageServerService"

STREAMS = [
    "SubscribeToSidecars",
    "GetSidecarLogs",
    "HandleStreamingCommand",
    "WatchDirectory",
    "WatchVersionControlState",
    "StreamUserTrajectoryReactiveUpdates",
    "StreamCascadePanelReactiveUpdates",
    "StreamCascadeReactiveUpdates",
    "StreamCascadeSummariesReactiveUpdates",
    "StreamAgentStateUpdates",
    "SubscribeToMcpServerStates",
    "StreamTerminalOutput",
    "SubscribeToPlugins",
    "StreamAudioTranscription",
    "StartNarration",
    "JetboxSubscribeToState",
    "JetboxSubscribeToSummaries",
    "JetboxSubscribeToGcertState",
    "JetboxSubscribeToOAuthState",
    "StreamSearchCode",
    "ProjectUpdatesStream",
    "SetupJetskiChat",
    "SubscribeToAgentBrowserRequests",
]


def envelope(payload):
    return b"\x00" + struct.pack(">I", len(payload)) + payload


def read_frames(resp, wait):
    """Read the frames the stream produces within `wait` seconds.

    `read1` returns whatever has arrived, without waiting for a full
    buffer — `read(n)` would block until it has n bytes, and a stream
    the reference holds open never reaches that.

    The return value carries whether the connection *ended* (EOF) or
    merely ran out of time — the difference between "the stream ended"
    and "the stream is still open".
    """
    out = []
    ended = False
    deadline = time.time() + wait
    while time.time() < deadline:
        try:
            chunk = resp.read1(65536)
        except socket.timeout:
            # The socket went quiet: the stream is still open, it just
            # has nothing more to say. This is *not* an end.
            break
        except Exception:
            ended = True
            break
        if not chunk:
            # An empty read is the end of the HTTP response.
            ended = True
            break
        out.append(chunk)
    return b"".join(out), ended


def parse_frames(data):
    out = []
    i = 0
    while i + 5 <= len(data):
        flags = data[i]
        ln = struct.unpack(">I", data[i + 1:i + 5])[0]
        payload = data[i + 5:i + 5 + ln]
        kind = "end" if flags & 0x02 else ("trailers" if flags & 0x80 else "msg")
        out.append((kind, payload))
        i += 5 + ln
    return out


def probe(host, port, method, wait):
    conn = http.client.HTTPConnection(host, port, timeout=wait)
    try:
        conn.request(
            "POST", f"/{SERVICE}/{method}", body=envelope(b"{}"),
            headers={
                "Content-Type": "application/grpc-web+json",
                "Accept": "application/grpc-web+json",
            })
        resp = conn.getresponse()
    except Exception as e:
        return {"verdict": "held-open", "note": f"no response: {e}"}

    headers = {k.lower(): v for k, v in resp.getheaders()}
    data, ended = read_frames(resp, wait)
    frames = parse_frames(data)
    msgs = [p for k, p in frames if k == "msg"]
    trailers = [p for k, p in frames if k == "trailers"]

    status = headers.get("grpc-status")
    message = headers.get("grpc-message")
    if trailers:
        verdict = "trailers"
    elif status is not None and status != "0":
        # The reference carries a failure status in the *headers* and
        # ends the stream right away.
        verdict = "trailers"
    elif msgs:
        # A frame arrived. Whether the stream then ends or stays open
        # is a separate fact.
        verdict = "message-then-ended" if ended else "message-then-open"
    elif status == "0":
        verdict = "trailers"
    else:
        verdict = "held-open" if not ended else "ended-empty"

    return {
        "verdict": verdict,
        "http_status": resp.status,
        "grpc_status": status,
        "grpc_message": message,
        "ended": ended,
        "frames": [[k, p.decode("utf-8", "replace")[:400]] for k, p in frames],
    }


def main():
    host = sys.argv[1]
    port = int(sys.argv[2])
    wait = float(sys.argv[3]) if len(sys.argv) > 3 else 4.0
    out_path = sys.argv[4] if len(sys.argv) > 4 else "-"
    out = {}
    for method in STREAMS:
        out[method] = probe(host, port, method, wait)
        print(f"{method:42s} {out[method]['verdict']:12s} "
              f"grpc-status={out[method].get('grpc_status')} "
              f"{str(out[method].get('grpc_message'))[:70]}", file=sys.stderr)
    if out_path == "-":
        json.dump(out, sys.stdout, ensure_ascii=False, indent=1, sort_keys=True)
        print()
    else:
        json.dump(out, open(out_path, "w"), ensure_ascii=False, indent=1, sort_keys=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
