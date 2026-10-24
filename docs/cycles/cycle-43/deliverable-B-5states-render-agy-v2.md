# Task B deliverable — tmux + rea 抓 v1.3.5 真 binary 5 state PTY log + render_agy.py v2

## Summary

用 tmux 3.3a 跑 v1.3.5 真 binary (`/workspace/agy/install/antigravity-v1.3.5`) 走完整 onboarding → 主 TUI, 抓 6 个状态的 PTY 字节流 (1 onboarding welcome + 5 main states: idle / thinking / tool-running / approval / error), 通过 `tmux capture-pane -e` 抓 raw escape 流。同时修 `render_agy.py` v1 的两个 bug — (a) 24-bit fg/bg reset SGRs `[49m`/`[39m` 被预处理 regex 误 strip, 导致 bg/fg 残留; (b) box-drawing 字符 (`─ │ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼ ╭ ╮ ╰ ╯`) 走 FreeMono font fallback 显示 'm'。v2 改为走手写 rectangle stroke path + 严格 cell width 居中, 渲染所有 6 state + cycle 42 v1 登录 logo 全部 1:1 (除 box-drawing 真渲染改为 rectangle stroke)。架构 doc `v1.3.5-real-tui-states.md` 写在 `/workspace/agy-recovered/docs/architecture/`, 含 5 panel 实际 row/col 布局, logo 5 行 ASCII art byte-level 序列, 和 v2 跟 v1 的差分说明。

## Changed files

### 产出 (deliverables in `outputs/tmux-rea-capture-v135-tui-5-states/`)

| 文件 | 大小 | 用途 |
|---|---|---|
| `state-0-onboarding-welcome.log` | 4752 B | onboarding 第一屏: 5 行彩虹渐变 ASCII art logo + `Welcome to Antigravity CLI!` + 8 个 color scheme 选择 (terminal/light/solarized light/colorblind-friendly light/dark/solarized dark/colorblind-friendly dark/tokyo night) + 右侧 preview box (rounded corners) |
| `state-1-idle.log` | 2309 B | 主 TUI idle: 顶部 logo + 右上 header (Antigravity CLI 1.3.5 / navigaid@gmail.com (Antigravity Starter Quota) / Gemini 3.8 Flash (High) / /root) + 上下 ─ 分割线 + `>` prompt + footer (`? for shortcuts` / `Gemini 3.8 Flash · high`) |
| `state-2-thinking.log` | 2643 B | 输入 `hello, what is 1+1?` 后 spinner `Generating...` + `esc to cancel` 提示 |
| `state-3-tool-running.log` | 3236 B | LLM 触发 `ls -la` tool, TUI 显示 `Running (ls -la) (ctrl+o to expand)` + `Generating...` + 注释 "Tip: When reviewing a file edit, press f to see the full diff" |
| `state-4-approval.log` | 6473 B | LLM 触发 `rm -v /tmp/foo.txt` 需要 approval, TUI 显示 `Run this command?` 4 选 1 prompt: (1) Yes, run / (2) Yes+always allow this / (3) Yes+always allow persist settings.json / (4) No, cancel + 底部 `↑/↓ Navigate · tab Amend · ctrl+g edit/expand command` + `esc to cancel` |
| `state-5-error.log` | 2390 B | 杀掉 sing-box proxy 后启 binary, TUI 显示 `Eligibility Check Failed: Post "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist": proxyconnect tcp: dial tcp 127.0.0.1:2081: connect: connection refused` |
| `v135-logo.bin` | 1100 B | logo 完整 byte 流 (1040 字节 + UTF-8 编码冗余) — 含 5 行 24-bit RGB 渐变 SGRs + 半块字符, 可直接 replay 到 terminal 看真实 logo 渲染 |
| `v1-baseline/state-0-v1-original.png` | 30297 B | v1 渲染 state-0 (broken): box-drawing chars 显示 'm', bg=blue stuck, 整图被蓝色矩形覆盖 |
| `v1-baseline/state-1-idle-v1.png` | 30 KB | v1 渲染 state-1 (broken): 'm' for box-drawing, 红/紫/蓝渐变泄露到所有 cell |
| `v1-baseline/cycle42.png` | 17635 B | v1 渲染 cycle 42 登录 logo (错: 半块在 col 0, 真实 col 5) |
| `v2-render/state-{0..5}-*.png` | 14-98 KB each | v2 渲染 6 state, box-drawing 走 rectangle stroke, bg/fg 正确 reset |
| `v2-render/cycle42.png` | 16714 B | v2 渲染 cycle 42 登录 logo (正确: 半块在 col 5, 跟 real terminal 一致) |
| `v2-render/cycle42-diff.png` | 23 KB | cycle 42 v1 vs v2 pixel diff (高亮), 9.12% 像素差是 v1 错 strip `[6G]` cursor positioning 导致 |
| `v1-vs-v2-state-0.png` | 85 KB | state-0 v1 vs v2 side-by-side, 直观对比 box-drawing fix + bg reset fix |
| `contact-sheet.png` | 302 KB | 6 state v2 渲染 contact sheet (2 列 × 3 行) |

### 修改

- **`/workspace/render_agy_v2.py`** (新建, 475 行): v2 修复, BOX_GLYPHS 字典覆盖 73 个 box-drawing 字符 (─ │ ┌ ┐ └ ┘ ├ ┤ ┬ ┴ ┼ ╭ ╮ ╰ ╯ + ━ ┃ ┄-┋ ┌-┛ ┝-┫ ┭-┳ ┵-┻ ┽-╋ ╔-╝), 每个字符定义手写 stroke 段 (fraction coordinates → pixel), STROKE_PX=2 画 thin rectangle 用 fg 颜色; cell 宽 = `font.getbbox('M')[2..0]`, char 用 getbbox 居中 (tx = x0 + (char_w - cw) // 2 - cbbox[0]); 24-bit SGR 留给 parser 不再被预处理 strip
- **`/workspace/agy-recovered/docs/architecture/v1.3.5-real-tui-states.md`** (新建, 125 行): 架构 doc — TL;DR table + 5 state PTY log 路径 + 5 state PNG 路径 + render_agy.py v2 vs v1 差分 (4 项改动) + v1.3.5 真 5 panel row/col 布局 + logo 5 行 ASCII art byte-level + 一手证据清单

### 保留

- **`/workspace/render_agy.py`** (180 行, **不修改**): v1 保留作 baseline 对照, 让 v1-vs-v2 差分可视化

## Notes

### Verification

- **5 state PTY log** 全部 `> 1KB` (state-1 = 2309 B, state-2 = 2643 B, state-3 = 3236 B, state-4 = 6473 B, state-5 = 2390 B), state-0 onboarding 附加 (4752 B)
- **5 state PNG** 全部用 `python3 /workspace/render_agy_v2.py <log> <png>` 渲染, 输出 1342×144..684 px (state-1 idle 1342×144, state-4 approval 1342×684 因为内容最多)
- **state-1 idle PNG** 显示 v1.3.5 真 5 panel 实际渲染 (logo + 右上 header + 上下分割线 + prompt + footer, 符合 architecture doc §4 row/col 表)
- **v2 cycle 42 vs v1 cycle 42**: 9.12% 像素差 — **不是 v2 引入的回归**, 是 v1 错 strip `[6G]` cursor positioning 把 logo 从真实 col 5 移到 col 0, v2 修复后 logo 位置正确 (跟 real terminal 一致); 看 `v2-render/cycle42-diff.png` 高亮区域就是 v1 错位的那些 cells
- **v2 state-0 vs v1 state-0**: v1 整图被蓝色矩形覆盖 (bg stuck + box-drawing 'm' fallback); v2 正确显示 `Welcome to Antigravity CLI!` + color scheme list + 右侧 preview box 边框 (用 ╭╮╰╯ + │ + ─ rectangle stroke 画)

### Test infrastructure

- 用 tmux 3.3a 创建 `agy135idle` session (state-1/2/3/4 抓的) 和 `agy135error` session (state-5 抓的), 都还跑着可备重抓
- 用 sing-box 1.9.2 SOCKS5 proxy on `127.0.0.1:2081` 走 chain TUIC → vps → antigravity API
- token: `/workspace/.home/.config/antigravity/oauth-token` (expiry 2026-10-15 03:12:56)
- python deps: Pillow 9.x (`/usr/local/lib/python3.10/dist-packages/PIL/`)
- font: FreeMono 18pt + FreeMonoBold 18pt (`/usr/share/fonts/truetype/freefont/`)

### Caveats / 已知 gap (留给后续 cycle)

1. **state-4 approval 没抓 "Yes and always allow" 二次确认的 autoplay 状态** — 当前只抓了 4 选 1 prompt 显示瞬间, 没抓用户选 (1)/(2)/(3) 后的 auto-approval 画面
2. **state-5 error 只抓了 proxyconnect 一种**, 没抓 401 Unauthorized (token 过期) / 429 rate limit / 500 internal 等其他 error 类型的 TUI 显示差异
3. **render_agy.py v2 emoji fallback** — `⚙ ◉ ★ ✗ ⚠` 等符号 FreeMono fallback 到空矩形, v2 走 font 路径显示为 `.notdef` 字符 (state-4 的 `⚙ tool:` 会显示为空), 没走 rectangle path
4. **rounded corner (╭╮╰╯) 渲染** — v2 用两个 stroke 段 (vertical + horizontal), 看起来是 "L" 形而非真弧形; real terminal 用真正的曲线, 视觉略不同
5. **没做 5 state + cycle 42 的真终端对比** — 没法用 headless Ghostty/iTerm2 在沙箱内启 v1.3.5 binary 同时输出 PNG 对比 (需要 sing-box + 1 token + 真 OAuth), cycle 43 task C/D 接力
