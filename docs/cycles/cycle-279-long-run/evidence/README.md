# cycle 279 — raw evidence

Model: MiniMax M3 via `agw.k0s.io`. Reference = official `agy` 1.4.2 launched
through `cmd/agy`'s CloudCode proxy (`/tmp/agy-wrapper`); clone = this worktree.

| file | what it is |
|---|---|
| `postfix-frames-sleep70.txt` | **post-fix confirmation, re-captured live on 2026-10-20 14:52–14:54** with `/tmp/wagyu-main` built from `main` @ `2391ae4`. Same H1 prompt (`sleep 70`). The tool is still running at 14:53:53 — 75 s after the prompt, 15 s past the old deadline — the turn reports `Thinking... (1m13.3s)` at 14:54:04, and the answer lands at 14:54:06. Pre-fix the identical prompt died at `1m0.017s text=0`. |

## The pre-fix artefacts were lost

The first run of this cycle captured, under this directory:

- `prefix-turn-sleep70.log` — pre-fix clone, H1, env-gated turn log:
  turn start 05:39:52.588 → turn end 05:40:52.606, `elapsed=1m0.017s text=0`.
- `prefix-frames-around-stop.txt` — tmux 200×40 frames either side of that stop.
- `prefix-wire-clone-chain.jsonl`, `reference-wire-chain.jsonl`,
  `reference-wire-h3.jsonl`, `reference-h3-notes-head.txt` — the H2/H3 wire
  captures and the reference's `NOTES.md` head.

They lived in the session's `TMPDIR` scratch tree and were removed by a TMPDIR
reset at 14:19 on 2026-10-20, together with the throwaway worktrees that held
the captures. The numbers quoted in `../deliverable.md` are from those files;
they were not re-derived, and they are marked accordingly in the deliverable's
limitations. Only the post-fix confirmation above is committed as raw evidence.

The code changes themselves were recovered from the session record (every
`edit`/`write`/instrumentation step was replayed onto `a5af64b`) and are what
`997b0b2` contains; the replay builds, formats, and passes the same tests as the
original tree.
