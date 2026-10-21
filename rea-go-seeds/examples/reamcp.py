#!/usr/bin/env python3
"""Drive REA's MCP stdio server: run a plan of tool calls in one session.

Usage: reamcp.py <plan.jsonl>
Each plan line: {"label": "name", "tool": "open_binary", "args": {...}}
Results land in ./mcp/<seq>-<label>.json ; progress to stderr.
"""
import json, os, subprocess, sys, threading, time

PLAN = sys.argv[1]
OUTDIR = os.path.abspath("./mcp")
os.makedirs(OUTDIR, exist_ok=True)

env = dict(os.environ)
env.setdefault("REA_GHIDRA_SEED_FILE", os.path.abspath("./out/seeds.txt"))
env.setdefault("REA_GHIDRA_STARTUP_TIMEOUT_MS", "21600000")
# GHIDRA_INSTALL_DIR and JAVA_HOME must point at your Ghidra + JDK 21:
# export GHIDRA_INSTALL_DIR=~/tools/ghidra_12.1.4_PUBLIC
# export JAVA_HOME=$(/usr/libexec/java_home -v 21)

proc = subprocess.Popen(["rea", "--mcp"], stdin=subprocess.PIPE,
                        stdout=subprocess.PIPE, stderr=open(f"{OUTDIR}/server.err", "wb"),
                        env=env, bufsize=0)

buf_lock = threading.Lock()
responses = {}
next_id = [0]
read_done = threading.Event()

def reader():
    buf = b""
    while True:
        chunk = proc.stdout.read1(65536) if hasattr(proc.stdout, "read1") else proc.stdout.read(65536)
        if not chunk:
            read_done.set()
            return
        buf += chunk
        while b"\n" in buf:
            line, buf = buf.split(b"\n", 1)
            line = line.strip()
            if not line:
                continue
            try:
                msg = json.loads(line)
            except Exception:
                continue
            if "id" in msg:
                with buf_lock:
                    responses[msg["id"]] = msg

t = threading.Thread(target=reader, daemon=True)
t.start()

def call(method, params, timeout=21600):
    next_id[0] += 1
    rid = next_id[0]
    proc.stdin.write((json.dumps({"jsonrpc": "2.0", "id": rid, "method": method, "params": params}) + "\n").encode())
    proc.stdin.flush()
    deadline = time.time() + timeout
    while time.time() < deadline:
        with buf_lock:
            if rid in responses:
                return responses.pop(rid)
        if proc.poll() is not None:
            raise RuntimeError(f"server exited rc={proc.returncode}")
        time.sleep(0.5)
    raise TimeoutError(method)

def notify(method, params=None):
    proc.stdin.write((json.dumps({"jsonrpc": "2.0", "method": method, "params": params or {}}) + "\n").encode())
    proc.stdin.flush()

init = call("initialize", {"protocolVersion": "2024-11-05",
                           "capabilities": {},
                           "clientInfo": {"name": "agy-re-driver", "version": "0.1"}})
print("init ok:", json.dumps(init.get("result", {}))[:300], file=sys.stderr)
notify("notifications/initialized")

tools = call("tools/list", {})
tool_names = [x["name"] for x in tools["result"]["tools"]]
open(f"{OUTDIR}/tools.json", "w").write(json.dumps(tools["result"]["tools"], indent=1))
print(f"tools: {len(tool_names)}", file=sys.stderr)

for seq, line in enumerate(open(PLAN)):
    line = line.strip()
    if not line:
        continue
    step = json.loads(line)
    label, tool, args = step["label"], step["tool"], step.get("args", {})
    print(f"[{seq}] {tool} {label} ...", file=sys.stderr, flush=True)
    t0 = time.time()
    try:
        res = call("tools/call", {"name": tool, "arguments": args})
    except Exception as e:
        res = {"driver_error": str(e)}
    dt = time.time() - t0
    out = json.dumps(res, indent=1)
    with open(f"{OUTDIR}/{seq:02d}-{label}.json", "w") as f:
        f.write(out)
    err = res.get("result", {}).get("isError") if isinstance(res.get("result"), dict) else None
    print(f"[{seq}] done in {dt:.0f}s bytes={len(out)} isError={err}", file=sys.stderr, flush=True)

print("plan complete", file=sys.stderr)
proc.terminate()
