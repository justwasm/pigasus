#!/usr/bin/env python3
"""Compare the clone's preview card with the reference's, cell by cell.

The reference's card is rebuilt from the raw pty stream by refgrid.py;
the clone's card is parsed from the lines `carddump` writes.  Both are
grids of (text, style) cells, and a difference in either is reported.

Usage:

    capture_card.py cards.json          # capture the reference's cards
    carddump <dir>                      # write the clone's cards
    compare_card.py cards.json <dir>    # diff them

The card sits at rows 18..39 and columns 34..96 of the captured screen
(one-based, as the reference addresses it).
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from refgrid import build, apply_sgr, Style, SGR_RE, OSC_RE  # noqa: E402

SCHEMES = [
    "terminal", "light", "solarized light", "colorblind-friendly light",
    "dark", "solarized dark", "colorblind-friendly dark", "tokyo night",
]

CARD_ROWS = range(17, 39)   # the card's 22 rows, 0-based
CARD_COLS = range(33, 96)   # the card's 63 columns, 0-based


def clone_grid(path):
    """The clone's card as a grid of (text, style) cells."""
    rows = []
    with open(path, 'rb') as fh:
        for raw in fh:
            line = raw.rstrip(b'\n')
            style = Style()
            cells = []
            i = 0
            n = len(line)
            while i < n:
                m = SGR_RE.match(line, i)
                if m:
                    params = m.group(1)
                    if m.group(2) == b'm' and not params.startswith(b'?'):
                        style = apply_sgr(style, params.decode())
                    i = m.end()
                    continue
                j = i
                while j < n and line[j] >= 0x20 and line[j] != 0x1b:
                    j += 1
                for ch in line[i:j].decode('utf-8', 'replace'):
                    cells.append((ch, style.key()))
                i = j
            rows.append(cells)
    return rows


def ref_cells(cards, name):
    data = bytes.fromhex(cards[name])
    grid = build(data)
    return [grid[r][CARD_COLS[0]:CARD_COLS[-1] + 1] for r in CARD_ROWS]


def style_name(st):
    fg, bg, bold, under, faint = st
    parts = []
    if fg:
        parts.append('fg=' + fg)
    if bg:
        parts.append('bg=' + bg)
    if bold:
        parts.append('bold')
    if under:
        parts.append('under')
    if faint:
        parts.append('faint')
    return ','.join(parts) or 'default'


def compare(cards, clone_dir, name):
    ref = ref_cells(cards, name)
    path = os.path.join(clone_dir, '%s.txt' % name.replace(' ', '_'))
    clone = clone_grid(path)
    diffs = []
    for r, (rr, cr) in enumerate(zip(ref, clone)):
        for c, (rc, cc) in enumerate(zip(rr, cr)):
            if rc[0] != cc[0] or rc[1] != cc[1]:
                diffs.append((r, c, rc, cc))
    return diffs


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    cards = json.load(open(sys.argv[1]))
    clone_dir = sys.argv[2]
    total = 0
    for name in SCHEMES:
        diffs = compare(cards, clone_dir, name)
        print('--- %s: %d differing cells' % (name, len(diffs)))
        total += len(diffs)
        for (r, c, rc, cc) in diffs[:12]:
            print('   row %2d col %2d  ref %r %-40s clone %r %s' % (
                r + 1, c + 1, rc[0], style_name(rc[1]), cc[0], style_name(cc[1])))
        if len(diffs) > 12:
            print('   ... %d more' % (len(diffs) - 12))
    print('total differing cells:', total)
    return 0 if total == 0 else 1


if __name__ == '__main__':
    sys.exit(main())
