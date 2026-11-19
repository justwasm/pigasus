#!/usr/bin/env python3
"""Render tmux capture pane output (with ANSI 24-bit color + half-block + box-drawing chars) to PNG.

v2 changes vs v1:
- Box-drawing chars (─ │ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼ ╭ ╮ ╰ ╯) now render via thin rectangle strokes,
  not the FreeMono font (which has no glyphs and fell back to 'm').
- Cell width is strictly `font.getbbox('M')[2..0]` so every cell is the same width and
  characters align to a clean grid.
- Stroke rectangles use the fg color; bg is filled first.
- Miscellaneous symbol chars (● ○ ◉ ▸ ▾ ■ □ ◆ ◇ ★ ⚠ ⚙ ✗) still use the font — these are
  rare in TUI output and have font glyphs.
- All half-block paths (▀ ▄ █) and color parse paths (24-bit RGB, 256-palette) unchanged.
"""
import re
import sys
from PIL import Image, ImageDraw, ImageFont

# ---------- palette / SGR ---------------------------------------------------

def palette_256(n):
    if n < 16:
        base = [(0,0,0),(128,0,0),(0,128,0),(128,128,0),(0,0,128),(128,0,128),(0,128,128),(192,192,192),(128,128,128),(255,0,0),(0,255,0),(255,255,0),(0,0,255),(255,0,255),(0,255,255),(255,255,255)]
        return base[n]
    if n < 232:
        n -= 16
        r = (n // 36) * 51
        g = ((n % 36) // 6) * 51
        b = (n % 6) * 51
        return (r,g,b)
    g = (n - 232) * 10 + 8
    return (g,g,g)

ANSI_RE = re.compile(r'\x1b\[((?:\d+;?)+)m')
CSI_RE = re.compile(r'\x1b\[[\?>0-9;]*[a-zA-Z]')
OSC_RE = re.compile(r'\x1b\][^\x07]*\x07')
CUP_RE = re.compile(r'\x1b\[(\d+);(\d+)H')
CHA_RE = re.compile(r'\x1b\[(\d+)G')
SGR_RE = re.compile(r'\x1b\[(\d*(?:;\d+)*)m')

DEFAULT_FG = (204, 204, 204)
DEFAULT_BG = (12, 12, 12)

def parse_sgr(params, fg, bg, bold):
    parts = params.split(';') if params else ['0']
    i = 0
    while i < len(parts):
        p = parts[i]
        if p == '' or p == '0':
            fg, bg, bold = DEFAULT_FG, DEFAULT_BG, False
        elif p == '1':
            bold = True
        elif p == '22':
            bold = False
        elif p == '39':
            fg = DEFAULT_FG
        elif p == '49':
            bg = DEFAULT_BG
        elif p == '38' and i + 4 < len(parts) and parts[i+1] == '2':
            fg = (int(parts[i+2]), int(parts[i+3]), int(parts[i+4]))
            i += 4
        elif p == '48' and i + 4 < len(parts) and parts[i+1] == '2':
            bg = (int(parts[i+2]), int(parts[i+3]), int(parts[i+4]))
            i += 4
        elif p == '38' and i + 3 < len(parts) and parts[i+1] == '5':
            i += 2
        elif p == '48' and i + 3 < len(parts) and parts[i+1] == '5':
            i += 2
        i += 1
    return fg, bg, bold

# ---------- box-drawing stroke table ---------------------------------------
# Each entry is a list of (x_frac, y_frac, x2_frac, y2_frac) relative to the cell.
# x_frac in [0,1], y_frac in [0,1] (where 1 = full char height = 2*cell_h).
# Stroke width = STROKE_PX (so a 0.5 frac line of width STROKE_PX reads as a thin line).
# Cell rect is (x0, y0, x0+char_w, y0+2*cell_h). Center is (x0+char_w/2, y0+cell_h).
# Quarter marks: x at 0 (left edge), 0.5 (middle), 1 (right edge);
#                y at 0 (top), 0.5 (middle), 1 (bottom).

# Bitmask convention:
#   1 = top    horizontal stroke (─ at top row)
#   2 = right  vertical stroke  (│ at right col)
#   4 = bottom horizontal stroke (─ at bottom row)
#   8 = left   vertical stroke  (│ at left col)
# We just hand-author each glyph's segments because Unicode box-drawing has many variants.

# Each segment: ((x0_frac, y0_frac), (x1_frac, y1_frac))
# where frac is 0..1 of (char_w, 2*cell_h).

BOX_GLYPHS = {
    # Light horizontal & vertical
    '─': [((0.0, 0.5), (1.0, 0.5))],                                 # U+2500 BOX DRAWINGS LIGHT HORIZONTAL
    '━': [((0.0, 0.5), (1.0, 0.5))],                                 # U+2501 HEAVY
    '│': [((0.5, 0.0), (0.5, 1.0))],                                 # U+2502 LIGHT VERTICAL
    '┃': [((0.5, 0.0), (0.5, 1.0))],                                 # U+2503 HEAVY VERTICAL
    # Light triples & quadruples
    '┄': [((0.0, 0.5), (0.2, 0.5)), ((0.4, 0.5), (0.6, 0.5)), ((0.8, 0.5), (1.0, 0.5))],  # U+2504
    '┅': [((0.0, 0.5), (0.2, 0.5)), ((0.4, 0.5), (0.6, 0.5)), ((0.8, 0.5), (1.0, 0.5))],
    '┆': [((0.5, 0.0), (0.5, 0.33)), ((0.5, 0.5), (0.5, 0.83))],    # U+2506
    '┇': [((0.5, 0.0), (0.5, 0.33)), ((0.5, 0.5), (0.5, 0.83))],
    '┈': [((0.0, 0.5), (0.3, 0.5)), ((0.6, 0.5), (1.0, 0.5))],
    '┉': [((0.0, 0.5), (0.3, 0.5)), ((0.6, 0.5), (1.0, 0.5))],
    '┊': [((0.5, 0.0), (0.5, 0.2)), ((0.5, 0.4), (0.5, 0.6)), ((0.5, 0.8), (0.5, 1.0))],
    '┋': [((0.5, 0.0), (0.5, 0.2)), ((0.5, 0.4), (0.5, 0.6)), ((0.5, 0.8), (0.5, 1.0))],
    # Light corners (down+right, down+left, up+right, up+left)
    '┌': [((0.5, 0.0), (0.5, 0.5)), ((0.5, 0.5), (1.0, 0.5))],     # U+250C
    '┍': [((0.5, 0.0), (0.5, 0.5)), ((0.5, 0.5), (1.0, 0.5))],
    '┎': [((0.5, 0.0), (0.5, 0.5)), ((0.5, 0.5), (1.0, 0.5))],
    '┏': [((0.5, 0.0), (0.5, 0.5)), ((0.5, 0.5), (1.0, 0.5))],
    '┐': [((0.5, 0.0), (0.5, 0.5)), ((0.0, 0.5), (0.5, 0.5))],     # U+2510
    '┑': [((0.5, 0.0), (0.5, 0.5)), ((0.0, 0.5), (0.5, 0.5))],
    '┒': [((0.5, 0.0), (0.5, 0.5)), ((0.0, 0.5), (0.5, 0.5))],
    '┓': [((0.5, 0.0), (0.5, 0.5)), ((0.0, 0.5), (0.5, 0.5))],
    '└': [((0.5, 0.5), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],     # U+2514
    '┕': [((0.5, 0.5), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┖': [((0.5, 0.5), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┗': [((0.5, 0.5), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┘': [((0.5, 0.5), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],     # U+2518
    '┙': [((0.5, 0.5), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┚': [((0.5, 0.5), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┛': [((0.5, 0.5), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    # Light T-junctions
    '├': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],     # U+251C right T
    '┝': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┞': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┟': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┠': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┡': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┢': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┣': [((0.5, 0.0), (0.5, 1.0)), ((0.5, 0.5), (1.0, 0.5))],
    '┤': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],     # U+2524 left T
    '┥': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┦': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┧': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┨': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┩': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┪': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┫': [((0.5, 0.0), (0.5, 1.0)), ((0.0, 0.5), (0.5, 0.5))],
    '┬': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],     # U+252C bottom T
    '┭': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┮': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┯': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┰': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┱': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┲': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┳': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.5), (0.5, 1.0))],
    '┴': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],     # U+2534 top T
    '┵': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┶': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┷': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┸': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┹': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┺': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┻': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 0.5))],
    '┼': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],     # U+253C cross
    '┽': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '┾': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '┿': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╀': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╁': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╂': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╃': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╄': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╅': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╆': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╇': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╈': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╉': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╊': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    '╋': [((0.0, 0.5), (1.0, 0.5)), ((0.5, 0.0), (0.5, 1.0))],
    # Rounded corners (╭╮╰╯)
    '╭': [((0.5, 0.25), (0.5, 0.5)), ((0.5, 0.5), (0.75, 0.5))],  # U+256D top-left rounded
    '╮': [((0.5, 0.25), (0.5, 0.5)), ((0.25, 0.5), (0.5, 0.5))],  # U+256E top-right rounded
    '╯': [((0.5, 0.5), (0.5, 0.75)), ((0.25, 0.5), (0.5, 0.5))],  # U+256F bottom-right rounded
    '╰': [((0.5, 0.5), (0.5, 0.75)), ((0.5, 0.5), (0.75, 0.5))],  # U+2570 bottom-left rounded
    # Heavy & double — only need a few common ones
    '═': [((0.0, 0.4), (1.0, 0.4)), ((0.0, 0.6), (1.0, 0.6))],     # U+2550 double horizontal
    '║': [((0.4, 0.0), (0.4, 1.0)), ((0.6, 0.0), (0.6, 1.0))],     # U+2551 double vertical
    '╔': [((0.4, 0.0), (0.4, 0.5)), ((0.4, 0.4), (0.5, 0.4)), ((0.4, 0.6), (0.5, 0.6)), ((0.5, 0.4), (0.6, 0.4)), ((0.5, 0.6), (0.6, 0.6)), ((0.6, 0.4), (0.6, 0.5))],  # U+2554
    '╗': [((0.6, 0.0), (0.6, 0.5)), ((0.4, 0.4), (0.5, 0.4)), ((0.4, 0.6), (0.5, 0.6)), ((0.5, 0.4), (0.6, 0.4)), ((0.5, 0.6), (0.6, 0.6)), ((0.4, 0.4), (0.4, 0.5))],
    '╚': [((0.4, 0.5), (0.4, 1.0)), ((0.4, 0.4), (0.5, 0.4)), ((0.4, 0.6), (0.5, 0.6)), ((0.5, 0.4), (0.6, 0.4)), ((0.5, 0.6), (0.6, 0.6)), ((0.6, 0.5), (0.6, 0.6))],
    '╝': [((0.6, 0.5), (0.6, 1.0)), ((0.4, 0.4), (0.5, 0.4)), ((0.4, 0.6), (0.5, 0.6)), ((0.5, 0.4), (0.6, 0.4)), ((0.5, 0.6), (0.6, 0.6)), ((0.4, 0.5), (0.4, 0.6))],
}

STROKE_PX = 2  # thickness of box-drawing strokes in PNG pixels

# ---------- main render ----------------------------------------------------

def render(input_path, output_path, font_path='/usr/share/fonts/truetype/freefont/FreeMono.ttf',
           bold_font_path='/usr/share/fonts/truetype/freefont/FreeMonoBold.ttf', font_size=18):
    with open(input_path) as f:
        text = f.read()
    # Strip script(1) header/footer
    text = re.sub(r'^Script started on[^\n]*\n', '', text)
    text = re.sub(r'Script (done|started)[\s\S]*?(?=\n\n|\Z)', '', text)
    text = OSC_RE.sub('', text)
    text = re.sub(r'\x1b\[\?[0-9;]*[hl]', '', text)
    text = re.sub(r'\x1b\[[\?>][0-9;]*[a-zA-Z]', '', text)
    text = re.sub(r'\x1b\[[\?>0-9;]*\$[a-zA-Z]', '', text)
    text = re.sub(r'\x1b\[[0-9;]*\$p', '', text)
    text = re.sub(r'\x1b\[[0-9;]*\$P', '', text)
    # NOTE: do NOT strip SGR (m) sequences here — the parser handles them.
    # The earlier v1 stripped `\x1b\[\d*[a-zA-Z]` which also matched short
    # 24-bit fg/bg resets like `[49m` and `[39m`, causing bg/fg to "stick" at
    # the last 24-bit color seen. We rely on the parser loop below to consume
    # SGRs (it uses SGR_RE which correctly handles semicolon-separated params).
    # Other one-letter CSI terminators we still strip: H/J/K only — no digits,
    # so the regex `\x1b\[[a-zA-Z]` is safe (it skips sequences with params).
    text = re.sub(r'\x1b\[H', '', text)  # cursor home
    text = re.sub(r'\x1b\[[a-zA-Z]', '', text)  # single-letter CSI (J, K, etc.)
    font = ImageFont.truetype(font_path, font_size)
    bold_font = ImageFont.truetype(bold_font_path, font_size)
    # Strict cell width from getbbox('M') — every cell is exactly char_w wide.
    bbox = font.getbbox('M')
    char_w = bbox[2] - bbox[0]
    # Cell height is half the line height (so two cells stack in a char row → half-block resolution)
    line_h = int(font.size * 1.4)
    cell_h = line_h // 2
    x, y = 0, 0
    fg, bg, bold = DEFAULT_FG, DEFAULT_BG, False
    max_x, max_y = 0, 0
    grid = {}
    pos = 0
    last_x = 0
    while pos < len(text):
        c = text[pos]
        if c == '\x1b':
            sgr_m = SGR_RE.match(text, pos)
            if sgr_m:
                fg, bg, bold = parse_sgr(sgr_m.group(1), fg, bg, bold)
                pos = sgr_m.end()
                continue
            m = CSI_RE.match(text, pos)
            if m:
                seq = m.group(0)
                if seq.startswith('\x1b[?') or seq.startswith('\x1b[>'):
                    pos = m.end()
                    continue
                if seq.endswith('m'):
                    pos = m.end()
                    continue
                if re.match(r'\x1b\[[\?>0-9;]*\$[a-zA-Z]', seq):
                    pos = m.end()
                    continue
                if re.match(r'\x1b\[H', seq):
                    x, y = 0, 0
                    pos = m.end()
                    last_x = 0
                    continue
                if re.match(r'\x1b\[\d*J', seq):
                    pos = m.end()
                    grid.clear()
                    max_x, max_y = 0, 0
                    continue
                if re.match(r'\x1b\[\d*K', seq):
                    pos = m.end()
                    continue
                cup = CUP_RE.match(seq)
                cha = CHA_RE.match(seq)
                if cup:
                    y = int(cup.group(1)) - 1
                    x = int(cup.group(2)) - 1
                elif cha:
                    new_x = int(cha.group(1)) - 1
                    if new_x < last_x and x > 0:
                        y += 1
                    x = new_x
                pos = m.end()
                last_x = x
                continue
            pos += 1
            continue
        elif c == '\r':
            x = 0
            last_x = 0
        elif c == '\n':
            y += 1
            x = 0
            last_x = 0
        elif c == '\b':
            x = max(0, x - 1)
        elif c == ' ' or (ord(c) >= 0x20 and ord(c) < 0x7f) or ord(c) >= 0xa0:
            grid[(x, y)] = (c, fg, bg, bold)
            x += 1
            max_x = max(max_x, x)
            max_y = max(max_y, y + 1)
            last_x = x
        pos += 1
    if not grid:
        print("Empty grid!")
        return
    img_w = (max_x + 2) * char_w
    img_h = (max_y + 1) * cell_h
    img = Image.new('RGB', (img_w, img_h), DEFAULT_BG)
    d = ImageDraw.Draw(img)
    for (cx, cy), (ch, fg_c, bg_c, bld) in grid.items():
        x0 = cx * char_w
        y0 = cy * cell_h
        cell_x1 = x0 + char_w
        cell_y1 = y0 + 2 * cell_h
        if ch == '\u2580':  # upper half block
            d.rectangle([x0, y0, cell_x1, y0 + cell_h], fill=fg_c)
            d.rectangle([x0, y0 + cell_h, cell_x1, cell_y1], fill=bg_c)
        elif ch == '\u2584':  # lower half block
            d.rectangle([x0, y0, cell_x1, y0 + cell_h], fill=bg_c)
            d.rectangle([x0, y0 + cell_h, cell_x1, cell_y1], fill=fg_c)
        elif ch == '\u2588':  # full block
            d.rectangle([x0, y0, cell_x1, cell_y1], fill=fg_c)
        elif ch in BOX_GLYPHS:
            # Draw cell background
            d.rectangle([x0, y0, cell_x1, cell_y1], fill=bg_c)
            # Draw stroke segments
            for (fx0, fy0), (fx1, fy1) in BOX_GLYPHS[ch]:
                # Convert fractions to pixel positions; y_frac of 1.0 = 2*cell_h (full char height)
                px0 = x0 + fx0 * char_w
                py0 = y0 + fy0 * (2 * cell_h)
                px1 = x0 + fx1 * char_w
                py1 = y0 + fy1 * (2 * cell_h)
                if abs(py1 - py0) < 1:  # horizontal line
                    d.rectangle([min(px0, px1), py0 - STROKE_PX // 2,
                                 max(px0, px1) + 1, py0 + STROKE_PX // 2 + (STROKE_PX % 2)],
                                fill=fg_c)
                elif abs(px1 - px0) < 1:  # vertical line
                    d.rectangle([px0 - STROKE_PX // 2, min(py0, py1),
                                 px0 + STROKE_PX // 2 + (STROKE_PX % 2), max(py0, py1) + 1],
                                fill=fg_c)
                else:  # diagonal — fall back to PIL line
                    d.line([(px0, py0), (px1, py1)], fill=fg_c, width=STROKE_PX)
        else:
            # Background + character
            d.rectangle([x0, y0, cell_x1, cell_y1], fill=bg_c)
            f = bold_font if bld else font
            # Center horizontally using getbbox for tight alignment
            cbbox = f.getbbox(ch)
            if cbbox:
                cw = cbbox[2] - cbbox[0]
                ch_h = cbbox[3] - cbbox[1]
                # Use (char_w - cw) / 2 to center the glyph in the cell horizontally
                tx = x0 + (char_w - cw) // 2 - cbbox[0]
                # Vertically center within the 2*cell_h box
                ty = y0 + (2 * cell_h - ch_h) // 2 - cbbox[1]
                d.text((tx, ty), ch, font=f, fill=fg_c)
            else:
                d.text((x0, y0 - int(font.size * 0.15)), ch, font=f, fill=fg_c)
    img.save(output_path, 'PNG')
    print(f"PNG saved: {img_w}x{img_h}px, grid {max_x}x{max_y}")

if __name__ == '__main__':
    if len(sys.argv) < 3:
        print("Usage: render_agy_v2.py <input.log> <output.png> [font.ttf] [bold.ttf] [size]")
        sys.exit(1)
    args = [sys.argv[1], sys.argv[2]]
    kwargs = {}
    if len(sys.argv) > 3:
        kwargs['font_path'] = sys.argv[3]
    if len(sys.argv) > 4:
        kwargs['bold_font_path'] = sys.argv[4]
    if len(sys.argv) > 5:
        kwargs['font_size'] = int(sys.argv[5])
    render(*args, **kwargs)
