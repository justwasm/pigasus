#!/usr/bin/env python3
"""Capture agy's /config → Color Scheme preview card, one scheme per run.

Each run drives the reference to one scheme value and keeps the raw
bytes it wrote, so the card's 24-bit escapes can be read off directly.

Usage:

    capture_card.py cards.json

The capture is a raw pty in a true-colour terminal (COLORTERM=truecolor),
one run per scheme — the reference repaints the card in place when the
selection moves, so a single run cannot hold more than one scheme's
card.  The last key the reference would take is Esc, which closes the
panel and repaints the screen; the run is killed with the card on
screen instead.
"""
import fcntl
import json
import os
import pty
import select
import struct
import sys
import termios
import time

AGY = os.path.expanduser("~/.local/bin/agy")
SCHEMES = [
    "terminal",
    "light",
    "solarized light",
    "colorblind-friendly light",
    "dark",
    "solarized dark",
    "colorblind-friendly dark",
    "tokyo night",
]
COLS, ROWS = 240, 50


def run(index):
    buf = bytearray()

    def pump(seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([fd], [], [], 0.2)
            if not r:
                continue
            try:
                data = os.read(fd, 65536)
            except OSError:
                return
            if not data:
                return
            buf.extend(data)

    def send(keys, wait=1.0):
        os.write(fd, keys)
        pump(wait)

    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm-256color"
        os.environ["COLORTERM"] = "truecolor"
        os.execv(AGY, ["agy"])
        os._exit(1)

    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    deadline = time.time() + 60
    while time.time() < deadline and b"ready for input" not in buf:
        pump(1)
    pump(2)
    send(b"/config\r", 4)
    send(b"color", 3)
    send(b"\r", 4)
    ups = len(SCHEMES) - 1 - index
    for _ in range(ups):
        send(b"\x1b[A", 0.35)
    pump(3)
    # The card is on screen: kill the reference here.  Closing the panel
    # would repaint the screen and take the card away.
    try:
        os.kill(pid, 9)
    except OSError:
        pass
    return bytes(buf)


def main():
    out = {}
    for i, name in enumerate(SCHEMES):
        out[name] = run(i).hex()
        print("captured", name, len(out[name]) // 2, "bytes", flush=True)
    with open(sys.argv[1], "w") as fh:
        json.dump(out, fh)


if __name__ == '__main__':
    main()
