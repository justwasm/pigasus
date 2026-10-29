#!/usr/bin/env python3
"""Build out/langserver/coverage-351.json from two probe captures.

    scripts/probe_ls_methods.py http://127.0.0.1:8811 /tmp/ref.json
    scripts/probe_ls_methods.py http://127.0.0.1:8814 /tmp/clone.json
    scripts/build_ls_coverage.py /tmp/ref.json /tmp/clone.json

The two captures are the answers of the reference and of the clone for
every method of `exa.language_server_pb.LanguageServerService`, taken
in the dialect the IDE's own workbench speaks (gRPC-Web). The merged
file is the evidence: it says, per method, what each instance answers
and whether the two answers are the same bytes.

The five methods the reference *closes the connection* on are listed
here: they cannot be captured, because the answer is the hang-up.
"""

import datetime
import json
import pathlib
import sys

# The reference closes the connection without answering on these
# (measured 2026-10-27, standalone + mocked). The answer is the
# hang-up, so it cannot be captured as a body.
CLOSES = [
    "CheckoutCommit",
    "ProvideCompletionFeedback",
    "SubscribeToAgentBrowserRequests",
    "UpdateCustomizationPathsFile",
    "WatchDirectory",
]

# The failure kinds the reference's refusals fall into.
ERROR_KINDS = [
    ("deprecated", "deprecated"),
    ("not implemented", "not-implemented"),
    ("unimplemented", "not-implemented"),
    ("not initialized", "not-initialised"),
    ("not found", "not-found"),
    ("required", "missing-argument"),
    ("missing", "missing-argument"),
    ("Method not found", "not-implemented"),
]


def error_kind(message):
    """The kind of failure an answer carries."""
    text = message.split(" (error ID")[0]
    for needle, kind in ERROR_KINDS:
        if needle in text:
            return kind
    return "other"


def classify(answer):
    """What an answer *is*, independent of the dialect."""
    if answer.get("status") == 0:
        return "no-answer", ""
    frames = answer.get("frames") or []
    trailers = [f[1] for f in frames if f[0] == "trailers"]
    if trailers:
        body = trailers[-1]
        status = body.split("grpc-status:")[1].split()[0] if "grpc-status:" in body else "0"
        if status != "0":
            return "error", body.strip()
    messages = [f[1] for f in frames if f[0] == "msg"]
    if messages:
        return "answer", messages[0]
    status = answer.get("grpc_status")
    if status not in (None, "0"):
        return "error", f"grpc-status {status}: {answer.get('grpc_message', '')}"
    return "empty-stream", ""


def main():
    if len(sys.argv) < 3:
        print(__doc__, file=sys.stderr)
        return 2
    reference = json.load(open(sys.argv[1]))
    clone = json.load(open(sys.argv[2]))
    out = {
        "_说明": {
            "证据等级": f"一手 —— 参考实现和克隆都在跑，{len(reference)} 个方法逐个实测",
            "抓取时间_utc": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "方法路径": "exa.language_server_pb.LanguageServerService",
            "方法数": len(reference),
            "方言": "application/grpc-web+json（IDE 工作台自己说的那一种；plain JSON 方言会把失败掩盖成 200 {}）",
            "请求体": "每个方法的请求消息的默认实例（{}）",
            "归一化": ["uuid → <uuid>", "error ID → <tracingId>", "时间戳 → <timestamp>"],
            "抓取工具": "scripts/probe_ls_methods.py",
        },
        "方法": {},
    }
    kinds = {"reference": {}, "clone": {}}
    for method in sorted(reference):
        if method in CLOSES:
            rk = "reference-closes"
            rb = "the reference closes the connection without answering"
        else:
            rk, rb = classify(reference[method])
        ck, cb = classify(clone.get(method, {"status": 0}))
        kinds["reference"][rk] = kinds["reference"].get(rk, 0) + 1
        kinds["clone"][ck] = kinds["clone"].get(ck, 0) + 1
        entry = {
            "reference": {"类型": rk, "答案": rb},
            "clone": {"类型": ck, "答案": cb},
            "逐字节相同": bool(rb) and rb == cb,
        }
        if rk == "error":
            entry["reference"]["错误类别"] = error_kind(rb)
        if ck == "error":
            entry["clone"]["错误类别"] = error_kind(cb)
        out["方法"][method] = entry
    error_kinds = {}
    for entry in out["方法"].values():
        kind = entry["reference"].get("错误类别")
        if kind:
            error_kinds[kind] = error_kinds.get(kind, 0) + 1
    out["_说明"]["汇总"] = {
        "reference": kinds["reference"],
        "reference_错误类别": error_kinds,
        "clone": kinds["clone"],
        "clone_未实现": sorted(
            m for m in reference
            if "Method not found" in (clone.get(m, {}).get("grpc_message") or "")
        ),
    }
    path = pathlib.Path(sys.argv[3]) if len(sys.argv) > 3 else pathlib.Path(f"out/langserver/coverage-{len(reference)}.json")
    path.write_text(json.dumps(out, ensure_ascii=False, indent=1) + "\n")
    print("wrote", path, path.stat().st_size, "bytes")
    print("reference:", kinds["reference"])
    print("clone:    ", kinds["clone"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
