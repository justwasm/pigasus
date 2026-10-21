#!/usr/bin/env python3
"""Cycle 278: remove the conversations our own test runs created.

Dry-run by default; pass --apply to act. Everything removed is *moved*, not
deleted: the .db files land in <store>/trash-278/ and the summaries DB is
copied there first, so the operation is reversible.

Keep set:
  * every conversation with a non-empty title (146 at the time of writing);
  * untitled rows whose preview is real prose (anything not in PROBE below).

Delete set:
  * untitled + empty preview  (the bulk: sessions the clone opened and never
    wrote a preview for);
  * untitled + a preview that matches one of the known probe prompts.
"""
import json
import os
import re
import shutil
import sqlite3
import sys
import time

STORE = os.path.expanduser("~/.gemini/antigravity-cli")
DB = os.path.join(STORE, "conversation_summaries.db")
CACHE = os.path.join(STORE, "cache", "conversation_metadata.json")
LAST = os.path.join(STORE, "cache", "last_conversations.json")
CONV = os.path.join(STORE, "conversations")
TRASH = os.path.join(STORE, "trash-278")

# Exact probe prompts (case-insensitive, first 60 chars of the preview).
PROBE = {
    "x", "xx", "hi", "hello", "say hi", "test", "ping", "zzz", "abc", "abcd efgh",
    "iabcd efgh", "first", "first prompt", "second", "third turn", "first turn",
    "/clear", "/schedule", "clear chat history", "clear conversation history",
    "simple greeting", "reply with the single word: ping", "what is this",
    "what is 2+2?", "hello, what is 1+1?", "list files in current directory",
    "probe", "probe1", "probe!", "markdown test", "hi there", "foo", "bar",
}
PROBE_PREFIX = (
    "use run_command", "use view_file", "use the run_command",
    "reply with the single word", "**first bold**", "**bold opens",
)


def is_probe(preview: str) -> bool:
    p = " ".join(preview.split()).lower()
    if p in PROBE:
        return True
    return any(p.startswith(x) for x in PROBE_PREFIX)


def main() -> int:
    apply = "--apply" in sys.argv
    db = sqlite3.connect(f"file:{DB}?mode=ro", uri=True)
    rows = db.execute(
        "select conversation_id, title, preview, step_count, last_modified_time "
        "from conversation_summaries"
    ).fetchall()
    db.close()

    delete = []
    for cid, title, preview, steps, mtime in rows:
        if title:
            continue
        if preview.strip() == "" or is_probe(preview):
            delete.append((cid, preview, steps, mtime))

    reasons = {"empty-preview": 0, "probe-prompt": 0}
    for cid, preview, steps, mtime in delete:
        reasons["probe-prompt" if preview.strip() else "empty-preview"] += 1

    print(f"total conversations : {len(rows)}")
    print(f"keep (titled)       : {sum(1 for r in rows if r[1])}")
    print(f"delete              : {len(delete)}  {reasons}")
    print(f"keep (untitled, real preview): "
          f"{sum(1 for r in rows if not r[1] and r[2].strip() and not is_probe(r[2]))}")
    print("\nsample of previews being removed:")
    seen = set()
    for cid, preview, steps, mtime in delete:
        p = " ".join(preview.split())[:60] or "(empty)"
        if p in seen:
            continue
        seen.add(p)
        print(f"  {cid[:12]}  steps={steps:<4} {p}")
        if len(seen) >= 12:
            break
    print("\nsample of untitled rows being KEPT:")
    n = 0
    for cid, title, preview, steps, mtime in rows:
        if title or not preview.strip() or is_probe(preview):
            continue
        print(f"  {cid[:12]}  steps={steps:<4} {' '.join(preview.split())[:60]}")
        n += 1
        if n >= 8:
            break

    if not apply:
        print("\n(dry run — pass --apply to move these to trash-278/)")
        return 0

    os.makedirs(TRASH, exist_ok=True)
    stamp = time.strftime("%Y%m%d-%H%M%S")
    shutil.copy2(DB, os.path.join(TRASH, f"conversation_summaries.db.{stamp}"))
    if os.path.exists(CACHE):
        shutil.copy2(CACHE, os.path.join(TRASH, f"conversation_metadata.json.{stamp}"))
    moved = 0
    ids = {c[0] for c in delete}
    for cid in ids:
        src = os.path.join(CONV, cid + ".db")
        if os.path.exists(src):
            shutil.move(src, os.path.join(TRASH, os.path.basename(src)))
            moved += 1

    db = sqlite3.connect(DB)
    db.executemany("delete from conversation_summaries where conversation_id = ?",
                   [(c,) for c in ids])
    db.commit()
    db.close()

    if os.path.exists(CACHE):
        with open(CACHE) as f:
            data = json.load(f)
        convs = data.get("conversations", {})
        before = len(convs)
        data["conversations"] = {k: v for k, v in convs.items() if k not in ids}
        with open(CACHE, "w") as f:
            json.dump(data, f)
        print(f"cache entries: {before} -> {len(data['conversations'])}")

    if os.path.exists(LAST):
        with open(LAST) as f:
            last = json.load(f)
        changed = {k: v for k, v in last.items() if v in ids}
        if changed:
            for k in changed:
                del last[k]
            with open(LAST, "w") as f:
                json.dump(last, f, indent=2)
        print(f"last_conversations: dropped {len(changed)} stale entries")

    print(f"moved {moved} .db files to {TRASH}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
