// colors.go — v1.3.6 true-color ANSI palette.
//
// Cycle 126: the reference binary emits 24-bit RGB colour escapes
// (`\x1b[38;2;R;G;Bm`) directly to the terminal — confirmed by
// running `script -q /tmp/agycmp/ref-color.txt /tmp/agycmp/ref136/agy`
// over `~/.local/bin/agy` 1.3.6 on macOS, then grepping the resulting
// raw bytes. The clone prior to this cycle used the 256-color fallback
// form (`\x1b[38;5;Nm`) for *every* drawn glyph except the banner's
// true-color gradient (the gradient block was already RGB because the
// ref binary bytes happened to fall on it; the surrounding text and
// chrome all used 256-color because that was what lipgloss returned at
// the time). The visual difference in tmux is subtle — 256 colour 69
// is xterm's "darker blue", whereas 24-bit 66,133,244 is the
// Antigravity brand blue. From a normal Mac Terminal window (which
// does honour true-colour) the difference is obvious.
//
// Reference values recovered from /tmp/agycmp/ref-color.txt:
//
//	foreground blue   (Antigravity brand)        38;2; 66;133;244
//	foreground blue bold                          38;2; 66;133;244 + ESC[1m
//	foreground dim gray (account, model, cwd)    38;2; 95; 99;104
//	foreground warning orange                    38;2;227;116;  0
//	foreground warning blue (bold ⎿)            38;2; 25;103;210 + ESC[1m
//	foreground warning body (no-bold dim gray)   38;2; 60; 64; 67  + ESC[22m
//	foreground separator gray (─)                38;2;218;220;224
//
// Each constant is the *opening* escape (the closing is `\x1b[0m` for
// blue/orange/separator/full-reset, or `\x1b[39m` to restore the default
// fg without affecting bold/underline). The dimensioned constants here
// only encode open + close — the matching close is chosen at the call
// site to keep style composition local.
//
// The trailing green-palette 256-colour code (`38;5;70`, etc.) the
// /help and picker lists still used was retained as a separate value;
// cycle 126 normalises those too. The brand blue is the same as the
// prompt blue (`>`) — the clone did already use the same 256 colour
// for both, so swapping both to the same true-colour is the right move.
package main

import "strings"

// ANSI true-colour opens for the recovered reference palette.
// The "Open" constants do not include a final reset; use the
// corresponding "Close" constant after the styled segment.
const (
	// Cycle 283: the 1.4.x reference renders the chrome from the
	// Solarized palette selected by the store's settings.json
	// ("colorScheme": "solarized light"). The pre-283 values were the
	// 1.3.x Google-dark RGB set; they quantise to visibly different
	// 256-colour indices (69/241/253 instead of 32/66/254), which is
	// exactly what the live A/B showed. Recovered from a truecolour
	// pty capture of agy 1.4.3 (COLORTERM=truecolor):
	//
	//	prompt ">", keys, active tab, plan mode  #268BD2  38;2;38;139;210
	//	status, model, cwd, dim labels            #657B83  38;2;101;123;131
	//	secondary text, tool args, hints          #586E75  38;2;88;110;117
	//	separator, inactive tab                   #EEE8D5  38;2;238;232;213
	//	body text                                 #073642  38;2;7;54;66
	//	auto mode                                 #CB4B16  38;2;203;75;22
	//	accept-edits mode                         #859900  38;2;133;153;0
	cBlueOpen        = "\x1b[38;2;38;139;210m"
	cBlueBoldOpen    = "\x1b[38;2;38;139;210;1m"
	cDimGrayOpen     = "\x1b[38;2;101;123;131m"
	cWarningOpen     = "\x1b[38;2;203;75;22m"
	cWarningBlueOpen = "\x1b[38;2;38;139;210;1m"
	cWarningBodyOpen = "\x1b[38;2;88;110;117;22m"
	cSeparatorOpen   = "\x1b[38;2;238;232;213m"

	// cSubtleOpen is the reference's secondary grey (#586E75) — the
	// picker's search placeholder / row metadata and the tool-step
	// args and hint. One step dimmer than cDimGrayOpen.
	cSubtleOpen = "\x1b[38;2;88;110;117m"

	// Cycle 127: tool-step palette. The brand blue is reused for the
	// ⏺ step bullet (the convention in Aider/Cursor is cyan, but the
	// cycle-126 evidence for v1.3.6 only confirmed the brand palette
	// on the chrome itself; the bullet colour is interpreted from the
	// tool-step renderer's escape sequence boundaries). The diff-add
	// and diff-remove colours are the standard chroma Aurora-Dark
	// tokens (those hex values are present in the reference's string
	// table, see `strings ~/.local/bin/agy | grep '#55aa22'`).
	cToolAddOpen    = "\x1b[38;2;133;153;0m" // Solarized green #859900
	cToolRemoveOpen = "\x1b[38;2;220;50;47m" // Solarized red   #DC322F

	// Cycle 156: tool-bullet colour as recovered from the v1.3.6
	// binary. The 256-colour index 29 is a deep teal/green the
	// reference uses for both the bullet and the "tool title" prefix
	// (e.g. "● Edited 1 file", "● Ran (git add ...)"). The bullet
	// character itself is U+25CF BLACK CIRCLE — ref uses that glyph,
	// not the U+23FA ⏺ "record" glyph that the cycle-127 clone picked
	// from a different source.
	cToolBulletOpen  = "\x1b[38;2;133;153;0m"
	cToolBulletClose = "\x1b[39m"

	// Cycle 285: step-detail palette, measured raw on agy 1.4.3:
	//
	//	\x1b[38;2;88;110;117m  ⎿  \x1b[mCommand exited with code 0.
	//	\x1b[6G\x1b[38;2;131;148;150m   1\x1b[m    alpha
	//	\x1b[6G\x1b[38;2;131;148;150;48;2;238;232;213m   3\x1b[38;2;220;50;47m -  gamma\x1b[K\x1b[m
	//
	// The detail chevron is the secondary gray (#586E75), not the warning
	// blue the clone used before; diff context lines print their line
	// number in #839496 with no background, while changed lines carry a
	// base3 (#EEE8D5) stripe that runs to the end of the row.
	cDetailBulletOpen = "\x1b[38;2;88;110;117m"
	cDiffNumOpen      = "\x1b[38;2;131;148;150m"
	cDiffStripeOpen   = "\x1b[38;2;131;148;150;48;2;238;232;213m"

	// AGY 1.4.1: inline diff preview background tints on added and removed lines
	// (picked from terminal background). Line-number gutter remains untinted (cDiffNumOpen).
	cDiffAddTintDark     = "\x1b[48;2;25;50;25m"
	cDiffRemoveTintDark  = "\x1b[48;2;60;25;25m"
	cDiffAddTintLight    = "\x1b[48;2;230;245;230m"
	cDiffRemoveTintLight = "\x1b[48;2;250;230;230m"
	cDiffAddTintOpen     = cDiffAddTintDark
	cDiffRemoveTintOpen  = cDiffRemoveTintDark

	// Cycle 156: thought-bullet palette. U+25B8 BLACK RIGHT-POINTING
	// SMALL TRIANGLE in a dim gray (256-colour 238 = #5d6168, the
	// brand-dim gray from cycle 126).
	cThoughtBulletOpen  = "\x1b[38;2;88;110;117m"
	cThoughtBulletClose = "\x1b[39m"

	// Cycle 157: tool-step verb palette. The v1.3.6 reference wraps
	// the verb (the first word — "Ran", "Edited", "Read", "Wrote",
	// …) in BOLD + 256-colour 172 (orange) so the verb pops out of
	// the dim-gray args that follow. The args themselves render in
	// 256-colour 235 (darker gray); the parenthesised file
	// annotation renders in 256-colour 238 (dim gray) again.
	cToolVerbOpen  = "\x1b[38;2;181;137;0;1m"
	cToolVerbClose = "\x1b[0m"

	// Cycle 162: tool-step args palette. The args after the verb
	// render in 256-colour 238 — the same dim gray as the bullet
	// and the hint. Cycle 157 had used 235 (a darker gray); the
	// recovered ref sequence verifies 238.
	cToolArgsOpen  = "\x1b[38;2;88;110;117m"
	cToolArgsClose = "\x1b[39m"

	// Cycle 162: tool-step args *outer* palette (the part of the
	// args before the first "("). 256-colour 235 is a darker gray
	// than 238, used for the bare count / noun tokens (e.g. "1 file"
	// in "Edited 1 file (build-release.yml)"). The parenthesised
	// portion goes in cToolArgsOpen (238). The two-colour split
	// matches the recovered ref sequence exactly.
	cToolArgsOuterOpen  = "\x1b[38;2;88;110;117m"
	cToolArgsOuterClose = "\x1b[39m"

	// Cycle 159: tool-step hint palette. The "(ctrl+o to expand)"
	// parenthetical that follows a tool title is slightly *lighter*
	// than the args (256-colour 238 vs 235) — the hint is a UI cue,
	// not a content detail, so it reads as a slightly less-prominent
	// annotation. The leading "(" and trailing ")" are wrapped in
	// the same colour as the inside of the parens, so the whole
	// hint renders as one highlighted parenthetical.
	cToolHintOpen  = "\x1b[38;2;88;110;117m"
	cToolHintClose = "\x1b[39m"

	// Cycle 157: tool-step failure palette. When the tool exited
	// non-zero, ref uses a red bullet (256-colour 166) and a red
	// "(Failed)" suffix. The verb stays orange — only the bullet
	// and the suffix change.
	cToolBulletErrOpen = "\x1b[38;2;220;50;47m"

	// Cycle 285: spinner-label palette. The reference paints the activity
	// label ("Generating", "Working", "Running command") in base0
	// (#657B83) and animates a #268BD2 highlight across its trailing
	// characters. Measured raw:
	//
	//	\x1b[38;2;101;123;131mRunning comma\x1b[38;2;38;139;210mnd...\x1b[m
	cSpinnerBaseOpen = "\x1b[38;2;101;123;131m"
	cSpinnerHiOpen   = "\x1b[38;2;38;139;210m"

	// Cycle 285: running-tool bullet. While a call is in flight the
	// reference paints the same U+25CF in the verb orange (#B58900)
	// without the bold attribute, and flips to the success green
	// (#859900) when it finishes. Verified byte-for-byte against
	// agy 1.4.3 (\\x1b[38;2;181;137;0m● on the "Running (…)" row).
	cToolBulletRunningOpen = "\x1b[38;2;181;137;0m"

	// Cycle 285: group-header count palette. The reference renders the
	// "<n> command(s)" token of a collapsed group in the body colour,
	// not in the verb orange: "\\x1b[38;2;7;54;66m2 commands\\x1b[m".
	cBodyOpen  = "\x1b[38;2;7;54;66m"
	cBodyClose = "\x1b[39m"

	// Cycle 157: failed-suffix palette. Bold + 256-colour 166 (red)
	// wraps the "(Failed)" trailing word so the failure is the
	// first thing the eye lands on.
	cToolFailedSuffixOpen  = "\x1b[38;2;220;50;47;1m"
	cToolFailedSuffixClose = "\x1b[0m"

	// Cycle 158: inline-code span palette. The v1.3.6 binary
	// recovers the byte sequence \x1b[48;5;254m\x1b[38;5;69m
	// (256-colour 254 background + 256-colour 69 foreground) —
	// a light pastel-gray background with a bright blue text.
	//
	// Cycle 128 had used 24-bit RGB (#1f2430 bg + #a8b2c5 fg) as a
	// guess; the recovered sequence above is what ref actually emits.
	cInlineCodeOpen  = "\x1b[38;5;32m\x1b[48;5;254m"
	cInlineCodeClose = "\x1b[0m"

	// Cycle 155: markdown bold palette. The v1.3.6 reference wraps
	// every ``**text**`` run in ESC[1m / ESC[22m so the body of a
	// resumed model reply reads with the same weight as the user's
	// terminal bold. The closing escape is `[22m` rather than `[0m`
	// because we only want to clear the bold attribute, not every
	// style — the surrounding line keeps its colour and (if any)
	// background.
	cBoldOpen  = "\x1b[1m"
	cBoldClose = "\x1b[22m"

	// Closes. ESC[0m clears every style; ESC[39m only clears the
	// foreground so bold/underline are kept if they were set.
	cBlueClose        = "\x1b[39m" // brand blue + prompt don't carry extra attrs
	cBlueBoldClose    = "\x1b[0m"  // brand-bolded (in tab active) needs full reset
	cDimGrayClose     = "\x1b[39m"
	cWarningClose     = "\x1b[39m"
	cWarningBlueClose = "\x1b[0m"
	cWarningBodyClose = "\x1b[39m" // bold is already off via ESC[22m in open
	cSeparatorClose   = "\x1b[0m"
	cToolAddClose     = "\x1b[39m"
	cToolRemoveClose  = "\x1b[39m"

	// Cycle diff-syntax-highlight: per-token syntax colours for the
	// /diff file view. The Aurora-Dark hex tokens the reference binary
	// ships (recoverable as `#a626a4`, `#50a14f`, `#986801`, `#a0a1a7`,
	// `#e5c07b` from `strings ~/.local/bin/agy | grep chroma`) are
	// emitted as 24-bit RGB so the colour survives a truecolour
	// terminal and degrades gracefully on the 256-colour fallback that
	// the cycle-126 palette normalises to.
	//
	// keyword  #a626a4  magenta              — Go/Py/JS/TS/Rs reserved words
	// string   #50a14f  green                — "..." / `...` / '...' literals
	// number   #986801  amber                — 0..9 digits and 0x.. hex literals
	// comment  #a0a1a7  light gray + italic  — // ..., # ..., /* ... */ blocks
	// type     #e5c07b  yellow               — Vec<T>, Option<T>, MyStruct, etc.
	cSyntaxKeywordOpen = "\x1b[38;2;166;38;164m"
	cSyntaxStringOpen  = "\x1b[38;2;80;161;79m"
	cSyntaxNumberOpen  = "\x1b[38;2;152;104;1m"
	cSyntaxCommentOpen = "\x1b[38;2;160;161;167m\x1b[3m"
	cSyntaxTypeOpen    = "\x1b[38;2;229;192;123m"
	cSyntaxTokenClose  = "\x1b[23m\x1b[39m" // clears italic and foreground
)

// cRule returns `<open>─<close>` — one cell of the horizontal rule the
// reference draws between the transcript and the prompt and again
// between the prompt and the status row. width is in *runes* (─ is 1
// rune / 3 bytes UTF-8); the helper pads with literal ─ characters so
// the byte budget only depends on width, not the number of cells.
func cRule(width int) string {
	var b strings.Builder
	b.Grow(len(cSeparatorOpen) + 3*width + len(cSeparatorClose))
	b.WriteString(cSeparatorOpen)
	for i := 0; i < width; i++ {
		b.WriteString("─")
	}
	b.WriteString(cSeparatorClose)
	return b.String()
}

// statusModeColor is the reference's per-mode colour on the status row's
// mode prefix (agy 1.4.3, truecolour capture): auto is Solarized orange,
// plan the blue, accept-edits the green. The default mode has no prefix.
func statusModeColor(mode string) string {
	switch mode {
	case "auto":
		return "\x1b[38;2;203;75;22m" // #CB4B16
	case "plan":
		return cBlueOpen // #268BD2
	case "accept-edits":
		return "\x1b[38;2;133;153;0m" // #859900
	}
	return cDimGrayOpen
}
