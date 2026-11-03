#!/usr/bin/env python3
"""Probe the server-streaming methods and record what the reference answers.

The gRPC-Web dialect is the one the IDE's own workbench speaks: the
request carries its message inside an envelope, and the answer's frames
say what the stream produced. A stream that produces nothing is recorded
as an empty stream — that is what the reference does for a cascade it
has no state for.

    probe_streams.py http://127.0.0.1:8899 /tmp/ref-streams.json
"""

import json
import struct
import sys
import urllib.error
import urllib.request

from concurrent.futures import ThreadPoolExecutor

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


def frames(data):
    out = []
    i = 0
    while i + 5 <= len(data):
        flags = data[i]
        ln = struct.unpack(">I", data[i + 1:i + 5])[0]
        out.append((flags, data[i + 5:i + 5 + ln]))
        i += 5 + ln
    return out


def call(base, method, timeout=6):
    url = f"{base}/{SERVICE}/{method}"
    body = json.dumps({}).encode()
    req = urllib.request.Request(
        url, data=envelope(body),
        headers={"Content-Type": "application/grpc-web+json"})
    try:
        resp = urllib.request.urlopen(req, timeout=timeout)
    except urllib.error.HTTPError as e:
        payload = e.read()
        ctype = e.headers.get("Content-Type", "")
        status = e.code
    except Exception as e:
        return {"status": 0, "error": str(e)}
    else:
        ctype = resp.headers.get("Content-Type", "")
        status = resp.status
        held = False
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
        out = []
        for flags, payload_frame in frames(payload):
            kind = "end" if flags & 0x02 else ("trailers" if flags & 0x80 else "msg")
            out.append([kind, payload_frame.decode("utf-8", "replace")])
        answer = {"status": status, "frames": out}
        if held:
            answer["held_open"] = True
        return answer
    return {"status": status, "body": payload.decode("utf-8", "replace")}


def main():
    base = sys.argv[1].rstrip("/")
    out_path = sys.argv[2] if len(sys.argv) > 2 else "-"
    out = {}
    with ThreadPoolExecutor(max_workers=6) as pool:
        futures = {pool.submit(call, base, m): m for m in STREAMS}
        for future, method in futures.items():
            out[method] = future.result()
    if out_path == "-":
        json.dump(out, sys.stdout, ensure_ascii=False, indent=1, sort_keys=True)
        print()
    else:
        json.dump(out, open(out_path, "w"), ensure_ascii=False, indent=1, sort_keys=True)
        print(f"{len(out)} streams recorded in {out_path}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
