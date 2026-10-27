#!/usr/bin/env python3
"""Capture the conversation batch of the language server, side by side.

    scripts/probe_ls_conversation.py http://127.0.0.1:8811 http://127.0.0.1:8814 out/langserver/conversation.json

The 351-method probe (`scripts/probe_ls_methods.py`) calls every method
with the *default instance* of its request message — no arguments at
all. That is the right measure of the surface, but it cannot show what
a method answers for a conversation that exists: the reference's own
mocked store holds no conversation, and the clone's holds none either
until a message goes in.

This probe therefore *makes* a conversation on each instance — the same
one, with the same client-minted id — and then asks the conversation
batch about it:

    StartCascade            the conversation the client minted
    SendUserCascadeMessage  the message that goes into it
    GetCascadeTrajectory     the trajectory behind the conversation id
    GetCascadeTrajectorySteps  its steps, from an offset
    GetUserTrajectory        the same trajectory, by trajectory id
    GetUserTrajectoryDebug   the mainline entry, with its scope
    GetUserTrajectoryDescriptions  the trajectory list
    GetConversationMetadata  the conversation's metadata
    GetTrajectoryTurns       its turns
    GetAllCascadeTrajectories  the summary of every conversation
    SearchConversations      the conversation list
    DeleteCascadeTrajectory  the delete

Every answer is recorded with the frames it arrived in, and the
volatile values (uuids, timestamps, tracing ids, pids) are normalised
to placeholders so the two instances can be compared.

The output is the evidence for the findings in
`out/langserver/README.md` §10 — the trajectory id rule above all:
the reference's trajectory id is the cascade id with the prefix
"traj-".
"""

import json
import os
import re
import struct
import sys
import urllib.error
import urllib.request

SERVICE = "exa.language_server_pb.LanguageServerService"

# The conversation both instances are asked about. The id is minted by
# the client — the Web UI mints one uuid per conversation and sends it
# as the cascade id.
CONVERSATION_ID = "conversation-probe-1"
MESSAGE = "conversation probe"


def call(base, method, body, timeout=20):
    """Call one method in the gRPC-Web dialect and return its frames."""
    raw = json.dumps(body).encode()
    data = b"\x00" + struct.pack(">I", len(raw)) + raw
    req = urllib.request.Request(
        f"{base}/{SERVICE}/{method}",
        data=data,
        headers={"Content-Type": "application/grpc-web+json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            payload = resp.read()
            headers = {k.lower(): v for k, v in resp.headers.items()}
    except urllib.error.HTTPError as e:
        return {"status": e.code, "error": e.read()[:200].decode("utf-8", "replace")}
    except Exception as e:  # the stream held the connection open
        return {"error": str(e)}
    frames = []
    i = 0
    while i + 5 <= len(payload):
        flags = payload[i]
        length = int.from_bytes(payload[i + 1:i + 5], "big")
        kind = "end" if flags & 0x02 else ("trailers" if flags & 0x80 else "msg")
        frames.append([kind, payload[i + 5:i + 5 + length].decode("utf-8", "replace")])
        i += 5 + length
    answer = {"status": 200, "frames": frames}
    if "grpc-status" in headers:
        answer["grpc_status"] = headers["grpc-status"]
    if "grpc-message" in headers:
        answer["grpc_message"] = headers["grpc-message"]
    return answer


# The batch, in the order the workbench asks for it.
BATCH = [
    ("StartCascade", {"cascadeId": CONVERSATION_ID,
                      "source": "CORTEX_TRAJECTORY_SOURCE_CASCADE_CLIENT",
                      "requestedModel": "MODEL_PLACEHOLDER_M0"}),
    ("SendUserCascadeMessage", {"cascadeId": CONVERSATION_ID,
                                "items": [{"text": MESSAGE}]}),
    ("GetCascadeTrajectory", {"cascadeId": CONVERSATION_ID}),
    ("GetCascadeTrajectorySteps", {"cascadeId": CONVERSATION_ID, "stepOffset": 0}),
    ("GetUserTrajectory", {"trajectoryId": "traj-" + CONVERSATION_ID}),
    ("GetUserTrajectory", {"trajectoryId": CONVERSATION_ID}),
    ("GetUserTrajectoryDebug", {}),
    ("GetUserTrajectoryDescriptions", {}),
    ("GetConversationMetadata", {"conversationId": CONVERSATION_ID}),
    ("GetTrajectoryTurns", {"cascadeId": CONVERSATION_ID}),
    ("GetAllCascadeTrajectories", {}),
    ("SearchConversations", {"query": ""}),
    ("DeleteCascadeTrajectory", {"cascadeId": CONVERSATION_ID}),
]


def normalise(text):
    """Replace the volatile values with placeholders.

    The conversation's own id is *not* normalised: the finding this
    capture exists for is the trajectory id the server mints out of it
    ("traj-" plus the cascade id), and a placeholder would hide it.
    """
    text = re.sub(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}",
                  "<uuid>", text)
    text = re.sub(r"\b[0-9a-f]{32}\b", "<tracingId>", text)
    text = re.sub(r"\d{4}-\d{2}-\d{2}T[\d:.]+Z", "<timestamp>", text)
    text = re.sub(r'"pid":\d+', '"pid":<pid>', text)
    return text


def capture(base):
    """The answers of one instance, method by method."""
    out = {}
    for method, body in BATCH:
        answer = call(base, method, body)
        frames = []
        for kind, text in answer.get("frames", []):
            frames.append([kind, normalise(text)])
        entry = {"request": body, "frames": frames}
        if "grpc_status" in answer:
            entry["grpc_status"] = answer["grpc_status"]
            entry["grpc_message"] = normalise(answer["grpc_message"])
        if "error" in answer:
            entry["error"] = answer["error"]
        out[method] = entry
    return out


def main():
    if len(sys.argv) < 4:
        print(__doc__)
        return 1
    ref_base, clone_base, out_path = sys.argv[1], sys.argv[2], sys.argv[3]
    document = {
        "_说明": {
            "证据等级": "一手 —— 参考实现和克隆都在跑，同一个对话逐个方法实测",
            "抓取时间_utc": None,
            "方法路径": SERVICE,
            "方言": "application/grpc-web+json",
            "对话": "两个实例上各自建立同一个对话（客户端自己铸的 id）",
            "归一化": ["uuid → <uuid>", "error ID → <tracingId>",
                       "时间戳 → <timestamp>", "pid → <pid>"],
            "抓取工具": "scripts/probe_ls_conversation.py",
        },
        "reference": capture(ref_base),
        "clone": capture(clone_base),
    }
    os.makedirs(os.path.dirname(out_path) or ".", exist_ok=True)
    with open(out_path, "w") as fh:
        json.dump(document, fh, indent=1, ensure_ascii=False)
        fh.write("\n")
    print(f"wrote {out_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
