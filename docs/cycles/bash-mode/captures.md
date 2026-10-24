# bash mode — raw captures

All snippets are verbatim bytes from truecolour pty sessions (`TERM=xterm-256color`,
120×40) on 2026-10-22. `agy` is the reference 1.4.4; `wagyu` is the clone.

The probe driver writes keystrokes into a pty, drains the output with timeouts, and
splits the stream at send marks. `\x1bM` (reverse index) is the scrolled repaint;
`\x08` is the reference's own cursor nudge between output lines — neither is
reproduced by the clone (the clone redraws whole rows).

## §1 success block — `! printf 'x\ny\n'`

```
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! printf 'x\ny\n'\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mx\x1b[m\n\x08
\x1b[38;2;88;110;117my\x1b[m\r\n
```

Final screen (blank row between blocks):

```
● ! true
<blank>
● ! sh -c 'echo out; echo err >&2; exit 3'
  ⎿  out
err
<blank>
● ! printf 'x\ny\n'
  ⎿  x
y
<blank>
```

## §2 composer + hint row — typing `!`

```
\r\x1b[38;2;38;139;210;1m!\x1b[m \r\n\n
\x1b[38;2;101;123;131m activated bash mode · esc to cancel\x1b[m
```

The `!` is bold blue and the space after it is default-coloured. The hint row
replaces `? for shortcuts`, keeps a leading space and uses U+00B7.

The command text after the prompt is the *default* foreground, not blue:

```
\r\x1b[38;2;38;139;210;1m!\x1b[m sh -c 'ech
```

## §3 empty command — `!` + Enter

```
\r\x1b[38;2;38;139;210m>\x1b[m \r\n\n
\x1b[38;2;101;123;131m? for shortcuts\x1b[m
```

No transcript row, no history entry.

## §4 failure with output — `! sh -c 'echo out; echo err >&2; exit 3'`

```
\x1b[38;2;220;50;47m●\x1b[m \x1b[38;2;38;139;210;1m! sh -c 'echo out; echo err >&2; exit 3'\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;220;50;47;22mout\x1b[m\n\x08
\x1b[38;2;220;50;47merr\x1b[m\r\n
```

There is no `exit status 3` line: the status is only printed when the command
produced no output.

## §5 failure without output — `! sh -c 'exit 3'`

```
● ! sh -c 'exit 3'
  ⎿  exit status 3
```

(the body is red in the capture; the text is Go's `*exec.ExitError` string)

## §6 Esc while composing — `!echo xyz` + Esc

```
> 
```

The buffer is cleared and the `? for shortcuts` hint returns. Esc is the same
gesture that leaves the mode when the command is already empty.

## §7 leading space — ` !echo lead`

```
> !echo lead
⣷  Generating...
```

It went to the model, which then called `run_command`:

```
● Ran (echo lead) (ctrl+o to expand)
```

So the mode only arms when `!` is the first key of an empty composer.

## §8 cancel — `! sleep 5` + Esc

```
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! sleep 5\x1b[m
```

No body, no `Interrupted · …` row, and the bullet is **green** (not red). While it
runs, the row is:

```
\x1b[38;2;181;137;0m○\x1b[m \x1b[38;2;38;139;210;1m! sleep 5\x1b[m
```

with the ordinary spinner — `⣷  Generating...` and the status row `esc to cancel`.

## §9 queued while a turn streams

```
\x1b[38;2;101;123;131m▸ echo hist\x1b[m
```

No `(alt+m to send immediately)` suffix (queued *prompts* do get it). The status row
below reads `  Press up to edit queued messages`.

## §10 history recall

After `!echo hist`, Up restores the composer in bash mode:

```
! echo hist
```

i.e. the mode is re-armed rather than the literal `!echo hist` text being inserted.

## §11 help panel does not describe the mode

`?` opens the shortcuts tab with `[1-26 of 34 items]`; scrolling to item 29 shows
`pgup  Page up` and the list continues alphabetically (right, shift+down, …). The
general tab's Quick Reference lists only `/`. The string
`Start your message with ! to run a shell command.` exists in the binary but does
not appear in either tab.

## Clone comparison

`wagyu`, same 120×40 pty, `!echo hello`:

```
\x1b[38;2;133;153;0m●\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[38;5;235;22m\x1b[K\x1b[m\r\n
\x1b[38;2;38;139;210;1m  ⎿  \x1b[38;2;88;110;117;22mhello\x1b[38;5;235m\x1b[K\x1b[m\r\n
```

`\x1b[38;5;235;22m\x1b[K` / `\x1b[38;5;235m\x1b[K` is the clone renderer's own
row-fill at the end of every line (present on all rows, not just this block); the
block's own sequences are identical to §1. The cloned failure block is identical to
§4, and the cancelled block identical to §8.
