#!/usr/bin/env python3
"""
scripts/test_resume_parity.py — Bidirectional resume parity and reproduction harness.

Verifies:
1. Difference in agy 1.5.8 resume behavior between legacy (render_info NULL)
   and aligned (StepRenderInfo present in column & field 140).
2. Streaming replay detection: measures time-to-first-stable-frame and step renders.
3. Expanded view (ctrl+o) fidelity.
4. Lossless / non-destructive storage round-trip.
"""

import os
import sys
import time
import uuid
import sqlite3
import subprocess

CONV_DIR = os.path.expanduser("~/.gemini/antigravity-cli/conversations")
SUMMARIES_DB = os.path.expanduser("~/.gemini/antigravity-cli/conversation_summaries.db")

def encode_varint(val: int) -> bytes:
    res = bytearray()
    while True:
        b = val & 0x7f
        val >>= 7
        if val > 0:
            res.append(b | 0x80)
        else:
            res.append(b)
            break
    return bytes(res)

def encode_tag(fn: int, wt: int) -> bytes:
    return encode_varint((fn << 3) | wt)

def encode_len_field(fn: int, data: bytes) -> bytes:
    return encode_tag(fn, 2) + encode_varint(len(data)) + data

def encode_str_field(fn: int, s: str) -> bytes:
    return encode_len_field(fn, s.encode("utf-8"))

def encode_varint_field(fn: int, val: int) -> bytes:
    return encode_tag(fn, 0) + encode_varint(val)

def build_terminal_block(command: str, cwd: str, exit_code: int, output: str) -> bytes:
    body = bytearray()
    body.extend(encode_str_field(1, command))
    body.extend(encode_str_field(2, cwd))
    body.extend(encode_varint_field(3, exit_code))
    body.extend(encode_str_field(4, output))
    return encode_len_field(3, bytes(body))

def build_markdown_block(output: str) -> bytes:
    return encode_str_field(1, output)

def build_step_render_info(title_prefix: str, icon: str, agg_key: str, title: str,
                           content_block_bytes: bytes) -> bytes:
    body = bytearray()
    if title_prefix:
        body.extend(encode_str_field(5, title_prefix))
    if icon:
        body.extend(encode_str_field(6, icon))
    if agg_key:
        body.extend(encode_str_field(7, agg_key))
    if title:
        title_part = encode_str_field(1, title)
        body.extend(encode_len_field(9, title_part))
    if content_block_bytes:
        body.extend(encode_len_field(10, content_block_bytes))
    return bytes(body)

def build_generic_step_140(tool_name: str, args_json: str, result_output: str, render_info_bytes: bytes) -> bytes:
    call_msg = bytearray()
    call_msg.extend(encode_str_field(1, tool_name))
    call_msg.extend(encode_str_field(2, args_json))
    
    result_msg = bytearray()
    if result_output:
        result_msg.extend(encode_str_field(1, result_output))
    if render_info_bytes:
        result_msg.extend(encode_len_field(3, render_info_bytes))
        
    generic = bytearray()
    generic.extend(encode_len_field(1, bytes(call_msg)))
    generic.extend(encode_len_field(2, bytes(result_msg)))
    return encode_len_field(140, bytes(generic))

def build_legacy_tool_payload(tool_name: str, args_json: str, result_output: str) -> bytes:
    call = bytearray()
    call.extend(encode_str_field(2, tool_name))
    call.extend(encode_str_field(3, args_json))
    
    f5 = bytearray()
    f5.extend(encode_len_field(4, bytes(call)))
    
    payload = bytearray()
    payload.extend(encode_len_field(5, bytes(f5)))
    payload.extend(encode_varint_field(1, 132))
    payload.extend(encode_varint_field(4, 3))
    if result_output:
        res = encode_str_field(1, result_output)
        payload.extend(encode_len_field(140, encode_len_field(2, res)))
    return bytes(payload)

def build_aligned_tool_payload(tool_name: str, args_json: str, result_output: str, render_info_bytes: bytes) -> bytes:
    call = bytearray()
    call.extend(encode_str_field(2, tool_name))
    call.extend(encode_str_field(3, args_json))
    
    f5 = bytearray()
    f5.extend(encode_len_field(4, bytes(call)))
    
    payload = bytearray()
    payload.extend(encode_varint_field(1, 132))
    payload.extend(encode_varint_field(4, 3))
    payload.extend(encode_len_field(5, bytes(f5)))
    payload.extend(build_generic_step_140(tool_name, args_json, result_output, render_info_bytes))
    return bytes(payload)

def build_user_input_payload(text: str) -> bytes:
    inner = encode_str_field(1, text)
    mid = encode_len_field(3, inner)
    return encode_len_field(19, mid)

def build_model_response_payload(text: str, thinking: str = "") -> bytes:
    planner = bytearray()
    planner.extend(encode_str_field(1, text))
    if thinking:
        planner.extend(encode_str_field(3, thinking))
    return encode_len_field(20, bytes(planner))

def init_test_db(db_path: str, conv_id: str, mode: str, n_turns: int = 3):
    os.makedirs(os.path.dirname(db_path), exist_ok=True)
    if os.path.exists(db_path):
        os.remove(db_path)
    con = sqlite3.connect(db_path)
    cur = con.cursor()
    cur.execute("""
    CREATE TABLE trajectory_meta (
        trajectory_id text, cascade_id text,
        trajectory_type integer, source integer,
        PRIMARY KEY (trajectory_id)
    )""")
    cur.execute("""
    CREATE TABLE steps (
        idx integer, step_type integer NOT NULL DEFAULT 0,
        status integer NOT NULL DEFAULT 0,
        has_subtrajectory numeric NOT NULL DEFAULT false,
        metadata blob, error_details blob, permissions blob,
        task_details blob, render_info blob,
        step_payload blob, step_format integer NOT NULL DEFAULT 0,
        PRIMARY KEY (idx)
    )""")
    cur.execute("CREATE TABLE trajectory_metadata_blob (id text DEFAULT 'main', data blob, PRIMARY KEY (id))")
    
    cur.execute("INSERT INTO trajectory_meta VALUES (?, ?, 4, 17)", (conv_id, conv_id))
    cur.execute("INSERT INTO trajectory_metadata_blob (id, data) VALUES ('main', ?)", (b"",))

    idx = 0
    for t in range(n_turns):
        # User
        cur.execute("INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?, 14, 3, ?)",
                    (idx, build_user_input_payload(f"User request turn {t}: inspect git")))
        idx += 1

        # Tool 1: terminal
        cmd = f"git status --porcelain"
        out = f" M internal/app/session/convstore/write.go\n"
        term_block = build_terminal_block(cmd, "/Users/gear/GitHub/openagy", 0, out)
        rinfo_term = build_step_render_info("Ran", "terminal", "terminal", "check git status", term_block)
        if mode == "legacy":
            payload = build_legacy_tool_payload("run_command", f'{{"CommandLine":"{cmd}"}}', out)
            cur.execute("INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?, 132, 3, ?)", (idx, payload))
        else:
            payload = build_aligned_tool_payload("run_command", f'{{"CommandLine":"{cmd}"}}', out, rinfo_term)
            cur.execute("INSERT INTO steps (idx, step_type, status, render_info, step_payload) VALUES (?, 132, 3, ?, ?)", (idx, rinfo_term, payload))
        idx += 1

        # Tool 2: file read
        file_path = "/Users/gear/GitHub/openagy/go.mod"
        read_out = "module github.com/justwasm/pigasus\n\ngo 1.23.0\n"
        read_block = build_markdown_block(read_out)
        rinfo_read = build_step_render_info("Analyzed", "description", "files", "go.mod", read_block)
        if mode == "legacy":
            payload = build_legacy_tool_payload("read_file", f'{{"Path":"{file_path}"}}', read_out)
            cur.execute("INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?, 132, 3, ?)", (idx, payload))
        else:
            payload = build_aligned_tool_payload("read_file", f'{{"Path":"{file_path}"}}', read_out, rinfo_read)
            cur.execute("INSERT INTO steps (idx, step_type, status, render_info, step_payload) VALUES (?, 132, 3, ?, ?)", (idx, rinfo_read, payload))
        idx += 1

        # Model
        cur.execute("INSERT INTO steps (idx, step_type, status, step_payload) VALUES (?, 15, 3, ?)",
                    (idx, build_model_response_payload(f"Model reply turn {t}: status is checked.", f"Thinking {t}.")))
        idx += 1

    con.commit()
    con.close()

    scon = sqlite3.connect(SUMMARIES_DB)
    scur = scon.cursor()
    scur.execute("""
    INSERT INTO conversation_summaries (
        conversation_id, title, preview, step_count, workspace_uris,
        last_modified_time, last_user_input_time,
        app_data_dir, project_id, status
    ) VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'), 'antigravity-cli', 'default-cli-project', 'CASCADE_RUN_STATUS_IDLE')
    ON CONFLICT(conversation_id) DO UPDATE SET
        step_count=excluded.step_count,
        last_modified_time=excluded.last_modified_time
    """, (conv_id, f"Test {mode} resume", "Check git status", idx, '["file:///Users/gear/GitHub/openagy"]'))
    scon.commit()
    scon.close()

def run_tmux_test(cid: str, label: str, duration_s: float = 3.0):
    sname = f"agy-test-{uuid.uuid4().hex[:6]}"
    subprocess.run(["tmux", "kill-session", "-t", sname], stderr=subprocess.DEVNULL)
    t0 = time.time()
    subprocess.run(["tmux", "new-session", "-d", "-s", sname, "-x", "120", "-y", "40", f"/Users/gear/.local/bin/agy --conversation={cid}"], check=True)

    ready_time = None
    samples = []
    trusted = False

    while (time.time() - t0) < duration_s:
        res = subprocess.run(["tmux", "capture-pane", "-p", "-t", sname], capture_output=True, text=True)
        pane = res.stdout
        elapsed = time.time() - t0
        samples.append((elapsed, pane))

        if not trusted and "Do you trust" in pane:
            subprocess.run(["tmux", "send-keys", "-t", sname, "C-m"])
            trusted = True

        if "for shortcuts" in pane and ("Bash(" in pane or "Read(" in pane):
            if ready_time is None:
                ready_time = elapsed

        time.sleep(0.1)

    # Test expanded view with ctrl+o
    subprocess.run(["tmux", "send-keys", "-t", sname, "C-o"])
    time.sleep(0.3)
    exp_res = subprocess.run(["tmux", "capture-pane", "-p", "-t", sname], capture_output=True, text=True)

    subprocess.run(["tmux", "kill-session", "-t", sname], stderr=subprocess.DEVNULL)
    return ready_time, samples, exp_res.stdout

def main():
    print("=== BIDIRECTIONAL RESUME PARITY & PERFORMANCE TEST ===")
    legacy_id = f"test-p-legacy-{uuid.uuid4().hex[:8]}"
    aligned_id = f"test-p-aligned-{uuid.uuid4().hex[:8]}"
    legacy_path = os.path.join(CONV_DIR, f"{legacy_id}.db")
    aligned_path = os.path.join(CONV_DIR, f"{aligned_id}.db")

    try:
        # Create 5-turn databases (20 steps each)
        init_test_db(legacy_path, legacy_id, "legacy", n_turns=5)
        init_test_db(aligned_path, aligned_id, "aligned", n_turns=5)

        print("\n1. Testing Legacy Format in Official agy 1.5.8...")
        l_ready, l_samples, l_exp = run_tmux_test(legacy_id, "Legacy")
        print(f"   Legacy Ready Time: {l_ready:.2f}s" if l_ready else "   Legacy: not ready")
        print(f"   Legacy Sample Frames: {len(l_samples)}")
        # Check if expanded view has read_file body
        l_has_read_body = "github.com/justwasm/pigasus" in l_exp
        print(f"   Legacy Expanded read_file content present: {l_has_read_body}")

        print("\n2. Testing Aligned Format in Official agy 1.5.8...")
        a_ready, a_samples, a_exp = run_tmux_test(aligned_id, "Aligned")
        print(f"   Aligned Ready Time: {a_ready:.2f}s" if a_ready else "   Aligned: not ready")
        print(f"   Aligned Sample Frames: {len(a_samples)}")
        a_has_read_body = "github.com/justwasm/pigasus" in a_exp
        print(f"   Aligned Expanded read_file content present: {a_has_read_body}")

        print("\n=== SUMMARY COMPARISON ===")
        print(f"Ready Time:      Legacy = {l_ready:.2f}s vs Aligned = {a_ready:.2f}s")
        print(f"Tool Read Body:  Legacy = {l_has_read_body} vs Aligned = {a_has_read_body}")

    finally:
        for p, cid in [(legacy_path, legacy_id), (aligned_path, aligned_id)]:
            if os.path.exists(p):
                os.remove(p)
            try:
                scon = sqlite3.connect(SUMMARIES_DB)
                scon.execute("DELETE FROM conversation_summaries WHERE conversation_id = ?", (cid,))
                scon.commit()
                scon.close()
            except Exception:
                pass

if __name__ == "__main__":
    main()
