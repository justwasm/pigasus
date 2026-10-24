# Cycle 134 — remove the speculative scrollbar; fix input-field misalignment

## Why

The user reported two related symptoms on long-output scrolling:

> 长输出滚动的时候，底部的 input field 经常会错位显示，而原版 agy 不会
> 右侧的 scrollbar 在内容显示时经常会有空白断开的问题

Investigation:

| File | `grep -c '│'` | `grep -c '█'` |
|---|---:|---:|
| `/tmp/agycmp/hotpath/resume_scroll/ref.*.txt` (all 4) | **0** | **0** |
| `/tmp/agycmp/hotpath/ctrl_o_expand/ref.*.txt` (all 4) | **0** | **0** |
| `/tmp/agycmp/hotpath/help_tabs/ref.*.txt` (all 4) | **0** | **0** |
| `/tmp/agycmp/hotpath/picker/ref.*.txt` (all 3) | **0** | **0** |
| `/tmp/agycmp/hotpath/cold_start/ref.*.txt` | **0** | **0** |
| `/tmp/agycmp/hotpath/*/our.*.txt` | **7–25 per file** | mixed |

**The reference has zero scrollbar characters in any capture.** The
cycle-122→133 clone added a one-column scrollbar with a
[`【推测】`](https://github.com/chadgpt/openagy/blob/main/cmd/wagyu/tui_viewport.go#L60-L62)
"no capture of it exists" comment — speculation confirmed wrong by every
capture collected since then.

The two user-visible symptoms came from that scrollbar:

1. **The "broken / gapped scrollbar in the middle" symptom** — the
   `scrollbarCell` thumb formula `thumb = viewport * viewport / total`
   produces a 1-row thumb on any transcript with `total > viewport²`,
   which is *every* long conversation on a 30-row terminal. A 1-row
   thumb in a 19-row track looks like a gap.
2. **The "input field misaligned" symptom** — `transcriptWindow` reserved
   the last cell for the scrollbar and re-wrapped the transcript one
   cell narrower (`resp = m.wrappedResponse(m.width - 1)`). With the
   scrollbar removed, the prompt-row's separator matches the transcript
   rows' width and aligns with the status row below.

Both bugs share a single root cause and a single fix.

## What changes

### `cmd/wagyu/tui_viewport.go`

- Deleted `v135ScrollbarTrack` and `v135ScrollbarThumb` constants (the
  only references in the codebase were inside `tui_viewport.go` itself
  and one test).
- Deleted `scrollbarCell(row, total, viewport)` method (~22 lines).
- Dropped the `+ m.scrollbarCell(...)` suffix from `transcriptWindow`
  — the loop is now `out[i] = padTo(row, effective)`.
- Dropped the `effective = m.width - 1` branch: the transcript always
  wraps to `m.width` regardless of overflow. (`overflow` is still
  computed and used to drive auto-scroll / clamp behaviour, so the
  scroll geometry is intact; only the column reservation is gone.)

### `cmd/wagyu/tui_viewport_test.go`

- Replaced `TestV135_ViewportScrollbarOnlyOnOverflow` (which pinned the
  scrollbar's row count and column width) with
  `TestV135_NoScrollbarEverywhere`, which asserts **no** `│` or `█`
  appears in either a fitting or an overflowing transcript. The new
  test is a stricter pin on the reference's actual behaviour and will
  fail loudly if anyone re-introduces the scrollbar without a fresh
  capture justifying it.

## What does NOT change in cycle 134

- The wrap cache (`wrappedResponse`, `rowsCacheDirty`, etc.) — cycle 122
  performance characteristic is preserved.
- The auto-scroll behaviour at the bottom of the transcript — the
  `if m.scroll >= maxScroll { m.autoScroll = true }` block is
  unchanged.
- `viewportHeight()` — chrome row count is unchanged.
- The picker / help / status overlays — none of them render a right
  column scrollbar in the reference, so they never needed one in the
  clone either.
- `padTo` — still clamps every row to `effective` columns so the
  transcript stays a rectangle regardless of wrap.

## Hard constraints / 存疑 / limitations

- 【推测】The scrollbar could reappear in a future capture. The harness
  re-runs `bash /tmp/agycmp/hotpath.sh` on every change, and a `│` /
  `█` in `ref.*.txt` will be visible immediately. If one shows up,
  reintroduce the scrollbar in `tui_viewport.go` and assert against it
  in the test — don't quietly let the build regress.
- The diff against `ref.001-pgup.txt` (54 chars) is now dominated by
  conversation-content differences: the reference tmux is showing a
  recent "MiniMax-M3 not available" thread, while the clone tmux is
  showing an older "picker byte structure" thread. Both are valid
  conversation IDs from `4638a7b0-e5f4-4961-80c2-d61499f06050`'s
  store; the discrepancy is in the local convstore cache, not the
  layout. Cycle 134 doesn't touch it; cycle 135+ can fix it by pointing
  both tmux panes at the same convstore root.