#!/usr/bin/env python3
"""Reconstruct the reference's preview card as a grid of styled cells.

The reference draws its card with cursor positioning, so the card is
rebuilt from the raw stream: every cell keeps the text and the style
that was in effect when the cell was written.  The result is compared
with the clone's card cell by cell.
"""
import json
import re
import sys

SGR_RE = re.compile(rb'\x1b\[([0-9;?]*)([A-Za-z])')
OSC_RE = re.compile(rb'\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)')


class Style:
    __slots__ = ('fg', 'bg', 'bold', 'underline', 'faint')

    def __init__(self):
        self.fg = None
        self.bg = None
        self.bold = False
        self.underline = False
        self.faint = False

    def copy(self):
        s = Style()
        s.fg, s.bg = self.fg, self.bg
        s.bold, s.underline, s.faint = self.bold, self.underline, self.faint
        return s

    def key(self):
        return (self.fg, self.bg, self.bold, self.underline, self.faint)

    def __eq__(self, other):
        return isinstance(other, Style) and self.key() == other.key()


def apply_sgr(style, params):
    if params == '':
        return Style()
    nums = [int(p) if p.isdigit() else 0 for p in params.split(';')]
    i = 0
    while i < len(nums):
        n = nums[i]
        if n == 0:
            style = Style()
        elif n == 1:
            style.bold = True
        elif n == 2:
            style.faint = True
        elif n == 4:
            style.underline = True
        elif n == 22:
            style.bold = False
            style.faint = False
        elif n == 39:
            style.fg = None
        elif n == 49:
            style.bg = None
        elif 30 <= n <= 37:
            style.fg = 'basic%d' % (n - 30)
        elif 90 <= n <= 97:
            style.fg = 'basic%d' % (n - 90 + 8)
        elif 40 <= n <= 47:
            style.bg = 'basic%d' % (n - 40)
        elif 100 <= n <= 107:
            style.bg = 'basic%d' % (n - 100 + 8)
        elif n in (38, 48):
            target = 'fg' if n == 38 else 'bg'
            if i + 1 < len(nums) and nums[i + 1] == 5 and i + 2 < len(nums):
                setattr(style, target, 'idx%d' % nums[i + 2])
                i += 2
            elif i + 1 < len(nums) and nums[i + 1] == 2 and i + 4 < len(nums):
                setattr(style, target, '#%02x%02x%02x' % (nums[i + 2], nums[i + 3], nums[i + 4]))
                i += 4
        i += 1
    return style


def build(data, cols=240, rows=60):
    grid = [[(' ', ('default', None, False, False, False)) for _ in range(cols)] for _ in range(rows)]
    style = Style()
    r = c = 0
    i = 0
    n = len(data)
    while i < n:
        b = data[i]
        if b == 0x1b:
            m = SGR_RE.match(data, i)
            if m:
                params, final = m.group(1), m.group(2)
                if final == b'H':
                    nums = [int(p) if p.isdigit() else 1 for p in params.split(b';')]
                    r = (nums[0] - 1) if len(nums) > 0 else 0
                    c = (nums[1] - 1) if len(nums) > 1 else 0
                elif final == b'm' and not params.startswith(b'?'):
                    style = apply_sgr(style, params.decode())
                elif final == b'X':
                    cnt = int(params) if params.isdigit() else 1
                    for k in range(c, min(cols, c + cnt)):
                        if 0 <= r < rows:
                            grid[r][k] = (' ', style.key())
                elif final == b'K':
                    for k in range(c, cols):
                        if 0 <= r < rows:
                            grid[r][k] = (' ', style.key())
                elif final == b'J':
                    mode = int(params) if params.isdigit() else 0
                    if mode == 2:
                        for rr in range(rows):
                            for k in range(cols):
                                grid[rr][k] = (' ', style.key())
                    elif mode == 1:
                        for rr in range(0, r + 1):
                            for k in range(0, cols if rr < r else c + 1):
                                grid[rr][k] = (' ', style.key())
                    else:
                        for rr in range(r, rows):
                            for k in range(0 if rr == r else 0, cols if rr > r else c):
                                grid[rr][k] = (' ', style.key())
                elif final == b'C':
                    cnt = int(params) if params.isdigit() else 1
                    c += cnt
                elif final == b'D':
                    cnt = int(params) if params.isdigit() else 1
                    r += cnt
                elif final == b'A':
                    cnt = int(params) if params.isdigit() else 1
                    r -= cnt
                elif final == b'B':
                    cnt = int(params) if params.isdigit() else 1
                    r += cnt
                i = m.end()
                continue
            m = OSC_RE.match(data, i)
            if m:
                i = m.end()
                continue
            i += 1
            continue
        if b == 0x0d:
            c = 0
            i += 1
            continue
        if b == 0x0a:
            r += 1
            i += 1
            continue
        if b < 0x20:
            i += 1
            continue
        # a run of printable text
        j = i
        while j < n and data[j] >= 0x20 and data[j] != 0x1b:
            j += 1
        text = data[i:j].decode('utf-8', 'replace')
        for ch in text:
            if 0 <= r < rows and 0 <= c < cols:
                grid[r][c] = (ch, style.key())
            c += 1
        i = j
    return grid


def card_region(grid):
    """The card's cells: the 63-column, 22-line block at column 34."""
    rows = []
    for r in range(18, 40):
        row = grid[r][33:96]
        rows.append(row)
    return rows


def dump(rows):
    out = []
    for row in rows:
        line = []
        for ch, style in row:
            line.append([ch, list(style)])
        out.append(line)
    return out


if __name__ == '__main__':
    # Usage: refgrid.py cards.json <scheme> [out.json]
    #
    # Prints the card's rows as text, and writes the cells (text and
    # style per cell) as JSON when an output path is given.
    if len(sys.argv) < 3:
        print(__doc__)
        sys.exit(2)
    cards = json.load(open(sys.argv[1]))
    name = sys.argv[2]
    data = bytes.fromhex(cards[name])
    grid = build(data)
    rows = card_region(grid)
    if len(sys.argv) > 3:
        with open(sys.argv[3], 'w') as fh:
            json.dump(dump(rows), fh)
    for row in rows:
        print(repr(''.join(ch for ch, _ in row).rstrip()))
