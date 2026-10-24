# Bash-mode interruption — raw captures

Method: `pty.fork()`, `TIOCSWINSZ` 120×40, `TERM=xterm-256color`, drain for 9 s at
startup, then scripted keystrokes with timestamps. No terminal emulation is applied to
the recordings — excerpts below are raw bytes, with `\x1b[…]` shown verbatim where the
escape matters.

Environment while capturing: `agy` **1.4.5** (auto-updated during this session; the
1.4.4-era `!sleep 5` capture is quoted from `docs/cycles/bash-mode/`), clone at
`1bd2eeb` / the working tree after it.

## Probe 1 — preemption (`timed.py`)

```python
drain(9)
send("!sh -c 'echo first; sleep 30'\r"); drain(4)
send("!echo queued2\r");                 drain(4)
send("\x1b");   drain(4)
send("\x03"); drain(1)
send("\x03"); drain(2)
```

### Reference (agy 1.4.5)

Timeline (seconds since start; `OUT` is bytes received):

```text
SEND   9.10 b"!sh -c 'echo first; sleep 30'\r"
OUT    9.10   38   \r!
OUT    9.12  106   sh -c 'echo first; sle\n\n activated bash mode · esc to cancel
OUT    9.14   89   \r>\n\n? for shortcuts
OUT    9.15  106   \n○ ! sh -c 'echo first; sleep 30'
SEND  13.11 b'!echo queued2\r'
OUT   13.11   40   \r!
OUT   13.13  104   \r>\n\n activated bash mode · esc to cancel
OUT   13.15  255   \r  ⎿  first\n\n● ! echo queued2\n  ⎿  queued2\r? for shortcuts
SEND  17.21 b'\x1b'      ← nothing to cancel any more: no output at all
```

The 13.15 chunk in raw bytes:

```text
\r\x1b[9d\x1b[4L
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mfirst\x1b[m\r\n
\n
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo queued2\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mqueued2\x1b[m…
```

Exit re-render (after two Ctrl+C) shows the interrupted head was never repainted:

```text
\x1b[38;2;181;137;0m○\x1b[m \x1b[38;2;38;139;210;1m! sh -c 'echo first; sleep 30'\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mfirst\x1b[m\r\n\n
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo queued2\x1b[m\r\n
```

### Clone, before the fix

```text
"○ ! sh -c 'echo first; sleep 30'"
'▸ echo queued2'                       ← queued, never ran before the Esc
```

and after Esc the spinner kept ticking for the full 30 s of the orphaned `sleep`
(no `first` anywhere in the recording).

### Clone, after the fix

Final screen, byte-identical structure to the reference:

```text
"○ ! sh -c 'echo first; sleep 30'"
'  ⎿  first'
'● ! echo queued2'
'  ⎿  queued2'
```

Raw head + body (the clone adds its usual `\x1b[38;5;235;22m\x1b[K` row fill, which is
the existing renderer convention for every transcript row):

```text
\x1b[38;2;181;137;0m○\x1b[m \x1b[38;2;38;139;210;1m! sh -c 'echo first; sleep 30'\x1b[38;5;235;22m\x1b[K\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mfirst\x1b[38;5;235m\x1b[K\x1b[m\r\n
\x1b[38;5;235m\x1b[K\x1b[m\n
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo queued2\x1b[m…
```

## Probe 2 — Esc / Ctrl+C (`kill.py`)

```python
drain(9)
send("!sh -c 'echo first; sleep 30'\r"); drain(3)
send("\x1b")      # or "\x03" for the Ctrl+C run
drain(4)
```

Reference, both keys: the block ends as the head row alone — `first` is **not** shown.

```text
\x1bM\x1b[38;2;181;137;0m●\x1b[m \x1b[38;2;38;139;210;1m! sh -c 'echo first; sleep 30'\x1b[m
```

amber `●` on 1.4.5 (the 1.4.4 capture in `docs/cycles/bash-mode/` shows
`\x1b[38;2;133;153;0m●`, green). The Ctrl+C run additionally prints:

```text
\n\npress ctrl+c again to exit
```

Clone after the fix: identical rows with the 1.4.4 green bullet, no body, and the same
`press ctrl+c again to exit` on Ctrl+C. `pgrep -fl "sleep 30"` after the run: empty —
the process group is gone.

## Probe 3 — the reported case (`topesc.py`)

```python
drain(9)
send("!top\r");   drain(2)
send("!pwd\r");   drain(1)
send("\x1b");     drain(4)
send("\x03");     drain(1)
send("\x03");     drain(2)
```

Reference: `top`'s dump is rendered as the interrupted block's body (117 non-blank
rows), the head stays `○ ! top`, then `● ! pwd` + `  ⎿  /private/var/folders/…`.

Clone after the fix: same shape and same body length (117 non-blank rows), i.e. the
`top` output is no longer swallowed. The clone prints the body in full where the
reference truncates (see Probe 4).

## Probe 4 — long output (`seqprobe.py`)

```python
drain(9)
send("!seq 1 200\r"); drain(3)
send("\x03"); drain(1)
send("\x03"); drain(2)
```

Reference final block:

```text
! seq 1 200
  ⎿  <output +180 lines>
181
182
…
200 (ctrl+o to expand)
```

Raw:

```text
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22m<output +180 lines>\x1b[m\n
\x1b[5G\x1b[1K \x1b[38;2;88;110;117m181\x1b[m\x1b[K\n
…
\x1b[38;2;88;110;117m200 (ctrl+o to expand)\x1b[m
```

So: elided count = total − cap, then the **last cap** lines, hint appended to the last
row.

### The cap is half the terminal height

Same `!seq 1 N` probe at three pty heights (agy 1.4.5):

| pty rows | command | marker | rows kept |
|---|---|---|---|
| 40 | `seq 1 20` | — | 20 (no hint) |
| 40 | `seq 1 21` | `<output +1 lines>` | 20 |
| 40 | `seq 1 200` | `<output +180 lines>` | 20 |
| 50 | `seq 1 40` | `<output +15 lines>` | 25 |
| 60 | `seq 1 40` | `<output +10 lines>` | 30 |

i.e. `cap = rows / 2`. The kept rows are on continuation style — only the marker row
carries `  ⎿  `:

```text
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22m<output +180 lines>\x1b[m\n
\x1b[38;2;88;110;117m181\x1b[m\n
…\x1b[38;2;88;110;117m200\x1b[m \x1b[38;2;88;110;117m(ctrl+o to expand)\x1b[m
```

### ctrl+o expands it

`expand.py` with `EXPAND=1` sends `\x0f` after the command. Reference, `!seq 1 200`:

- the `<output +180 lines>` marker disappears;
- the full body is rendered (the visible window is the tail because the viewport
  scrolls to the bottom — rows 1..165 are simply never painted);
- the last row becomes `200 (ctrl+o to collapse)`:
  `\x1b[38;2;88;110;117m200\x1b[m \x1b[38;2;88;110;117m(ctrl+o to collapse)\x1b[m`.

### The same rules apply to a preempted block (`prem.py`)

`!sh -c 'seq 1 100; sleep 30'`, then `!echo x` one second later:

```text
● ! sh -c 'seq 1 100; sleep 30'
  ⎿  <output +80 lines>
81
…
100 (ctrl+o to expand)

● ! echo x
  ⎿  x
```

ctrl+o then expands it (`100 (ctrl+o to collapse)`), same as a completed block.

### Clone after the fix

Identical rows and hint spans for all of the above (`w200.raw`, `w200exp.raw`,
`prem_w.raw`, `prem_w_exp2.raw`). The only byte-level difference on these rows is the
clone's usual `\x1b[38;5;235m\x1b[K` row fill, which every transcript row already
carries.
