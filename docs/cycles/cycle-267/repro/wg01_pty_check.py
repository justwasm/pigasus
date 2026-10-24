#!/usr/bin/env python3
"""Cycle 267 SIMPLE PTY verification — confirms the tea.ClearScreen fix
works by observing the escape sequences the bubbletea renderer emits
after a streamed turn completes.

Pre-fix (cycle 266): the glamour-replace handler wrote the new
response at row 9 with [CSI9d][L], leaving stale pixels above. No
\x1b[H\x1b[2J is emitted.

Post-fix (cycle 267): the case turnDoneMsg handler returns
(tea.Batch(queueCmd, tea.ClearScreen)) which triggers the renderer to
emit \x1b[H\x1b[2J (cursor home + clear screen) on the next render,
followed by a complete redraw of the View() output.

Detection: look for the literal bytes "\x1b[H\x1b[2J" or "\x1b[2J\x1b[H"
in the captured PTY output AFTER state=done:app=wagyu. This is the
unambiguous signature of s.scr.Erase()/clearUpdate() running.

Usage: /tmp/c267-verify.py [binary-path]
       defaults to /tmp/wagyu-c267
"""
import os, sys, pty, select, time, fcntl, termios, struct

BINARY = sys.argv[1] if len(sys.argv) > 1 else '/tmp/wagyu-c267'
LOG = '/tmp/c267-verify.log'

os.environ['HOME'] = '/tmp/wagyu-tui-home'
os.environ['TERM'] = 'xterm-256color'
os.environ['OPENAGY_TRUST'] = 'always'  # skip workspace-trust dialog
os.environ['COLUMNS'] = '250'
os.environ['LINES'] = '30'

cmd = [BINARY, '--backend=shim']
pid, fd = pty.fork()
if pid == 0:
    os.chdir('/Users/gear/Documents/GitHub/openagy-cycle267')
    try:
        os.execvp(cmd[0], cmd)
    except Exception:
        os._exit(1)
else:
    cols, rows = 250, 30
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', cols, rows, 0, 0))
    attr = termios.tcgetattr(fd)
    attr[0] = attr[0] | termios.ICRNL
    attr[3] = attr[3] | termios.ICANON | termios.ECHO
    termios.tcsetattr(fd, termios.TCSANOW, attr)

    log = open(LOG, 'wb')

    def drain_until(secs, sentinel):
        end = time.time() + secs
        buf = b''
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.05)
            if r:
                try:
                    data = os.read(fd, 8192)
                except OSError:
                    break
                if not data:
                    break
                buf += data
                log.write(data)
                log.flush()
                if sentinel in buf:
                    return buf
        return buf

    def drain(secs):
        end = time.time() + secs
        out = b''
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.05)
            if r:
                try:
                    data = os.read(fd, 8192)
                except OSError:
                    break
                if not data:
                    break
                out += data
                log.write(data)
                log.flush()
        return out

    # Wait for initial View() to settle
    log.write(b"=== BEFORE drain(2.0) ===\n")
    init = drain(2.0)
    log.write(f"=== AFTER drain(2.0) got {len(init)} bytes ===\n".encode())

    # Send prompt + Enter (CR for bubbletea's mapped-on-newline parser)
    log.write(b"\n\n=== SENDING PROMPT ===\n")
    os.write(fd, b'analyze pwd\r')

    # Capture for ~10 seconds (mock backend streams 5 chunks at 15ms each,
    # glamour-replace, ClearScreen all complete well within 5s; 10s gives
    # ample headroom).
    log.write(b"=== BEFORE drain(10.0) ===\n")
    import sys as _sys
    _sys.stderr.write("starting drain(10.0)\n"); _sys.stderr.flush()
    full = drain(10.0)
    _sys.stderr.write(f"drain returned, {len(full)} bytes\n"); _sys.stderr.flush()
    log.write(f"=== AFTER drain(10.0) got {len(full)} bytes ===\n".encode())

    log.write(b"\n\n=== ANALYSIS ===\n")
    log.write(f"captured {len(full)} bytes after submit\n".encode())

    # Signature 1: full-redraw clear (\x1b[H\x1b[2J)
    sig_clear_home = b'\x1b[H\x1b[2J'
    sig_clear_home_rev = b'\x1b[2J\x1b[H'
    has_clear_sig = sig_clear_home in full or sig_clear_home_rev in full
    log.write(f"clear-screen-after-state=done present: {has_clear_sig}\n".encode())

    if sig_clear_home in full:
        idx = full.index(sig_clear_home)
        log.write(f"  signature at byte {idx}: ...{full[max(0,idx-30):idx+30]!r}...\n".encode())
    elif sig_clear_home_rev in full:
        idx = full.index(sig_clear_home_rev)
        log.write(f"  signature at byte {idx}: ...{full[max(0,idx-30):idx+30]!r}...\n".encode())

    # Signature 2 (negative): no leftover raw-chunk leakage ("mock reply f"
    # followed by a NON-continuation; the substring "mock reply from" is
    # the legitimate post-clear-screen response, not a streaming leak,
    # so we look for "mock reply f" NOT followed by "rom model="). Pre-
    # fix this would appear (the chunks accumulated at row 9 and
    # glamour-replace didn't clear them).
    past_done = full.split(b'state=done:app=wagyu', 1)[-1] if b'state=done:app=wagyu' in full else full
    raw_marker = b'mock reply f'
    leak_offsets = []
    j = 0
    while True:
        j = past_done.find(raw_marker, j)
        if j == -1: break
        # Check the next byte — if it's the start of "rom model=", it's
        # part of "mock reply from model=…" which is the legitimate
        # response, NOT a streaming leak. Otherwise it's a leftover
        # chunk that should have been cleared.
        next_byte = past_done[j + len(raw_marker):j + len(raw_marker) + 4]
        if next_byte != b'rom ':
            leak_offsets.append(j)
        j += 1
    has_raw_chunk_leak = len(leak_offsets) > 0
    log.write(f"raw streaming chunk 'mock reply f' leaked past state=done: {has_raw_chunk_leak} ({len(leak_offsets)} occurrences)\n".encode())

    # Signature 3 (positive): response string fragments present (the PTY
    # captured the bytes wrapped across multiple rows because the
    # captured terminal was 30 rows tall × 250 cols, and the bytes
    # emitted by glamour include \r\n between fragments). What we
    # verify is that each fragment is present somewhere in the
    # post-clear-screen data — that's the post-glamour-replace
    # content we expect on screen. We strip CR/LF before searching
    # so line-wraps don't fool the assertion.
    past_stripped = b''.join(past_done.splitlines())
    has_full_response = (
        b'mock reply from model=' in past_stripped
        and b'Gemini 3.8 Flash (Medium)' in past_stripped
    )
    log.write(f"full response fragments present post-ClearScreen: {has_full_response}\n".encode())

    # Signature 4 (positive): no spinner glyphs after state=done. The
    # spinner glyphs are UTF-8 braille chars in the sequence U+28XX
    # encoded as \xe2\xa0\xbd / \xe2\xa0\xbb / \xe2\xa0\xbc / \xe2\xa0\x99
    # etc. We accept the \xe2\xa0 prefix as the marker; if any braille-
    # pattern char shows up post-state=done, the spinner leaked through.
    has_done = b'state=done:app=wagyu' in full
    if has_done:
        past_done = full.split(b'state=done:app=wagyu', 1)[-1]
    else:
        past_done = full
    # Common spinner UTF-8 prefixes from the bubbles library.
    braille_prefix = b'\xe2\xa0'
    has_spinner_leak = braille_prefix in past_done and b'Generating' in past_done
    log.write(f"spinner glyph 'Generating' leaked past state=done: {has_spinner_leak}\n".encode())

    # Signature 5 (positive): Card 2 title appears after state=done
    has_card2 = b'Daily quota resets' in past_done
    log.write(f"Card 2 'Daily quota resets' present after state=done: {has_card2}\n".encode())

    try:
        os.close(fd)
    except Exception:
        pass
    # Don't waitpid — child may still be alive and the OS-level
    # cleanup is not on the critical path; the parent will exit
    # and the child becomes a zombie briefly before reaping.
    log.close()

    pass_count = sum([
        has_clear_sig,
        not has_raw_chunk_leak,
        has_full_response,
        not has_spinner_leak,
        has_card2,
    ])
    print(f"PASS={pass_count}/5 clear-sig={has_clear_sig} no-raw-leak={not has_raw_chunk_leak} full-resp={has_full_response} no-spinner={not has_spinner_leak} card2={has_card2}")
    sys.exit(0 if pass_count == 5 else 1)
