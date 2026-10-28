// Package palette provides the recovered reference true-colour ANSI palette.
//
// Cycle 126 / 283 / 285: the reference binary emits 24-bit RGB colour escapes
// (\x1b[38;2;R;G;Bm) directly to the terminal, recovered from truecolour pty
// captures of agy 1.4.3 (COLORTERM=truecolor).
//
// The colours follow the *selected* colour scheme: SetScheme recomputes
// them from the scheme's slots, so picking a scheme in /config repaints
// the whole interface the way the reference's does. They start at the
// reference's "solarized light" scheme — the scheme the capture they
// were recovered from was taken under.
//
// The colours the reference hard-codes — the agent status trio, the
// inline code pair, the panel blue, the syntax tokens and the diff
// tints — keep their recovered values; they are not scheme slots.
package palette

// The colours that follow the selected colour scheme.
//
// Every value here is the recovered true colour of the slot the
// reference reads it from, and SetScheme recomputes it when the
// selected scheme changes.
var (
	// Chrome & status
	BlueOpen        = "\x1b[38;2;38;139;210m"
	BlueBoldOpen    = "\x1b[38;2;38;139;210;1m"
	DimGrayOpen     = "\x1b[38;2;101;123;131m"
	WarningOpen     = "\x1b[38;2;203;75;22m"
	WarningBlueOpen = "\x1b[38;2;38;139;210;1m"
	WarningBodyOpen = "\x1b[38;2;88;110;117;22m"
	SeparatorOpen   = "\x1b[38;2;238;232;213m"
	SubtleOpen      = "\x1b[38;2;88;110;117m"

	// The user's and the agent's own colours — the two glyph roles the
	// transcript and the panels paint.
	UserFgOpen     = "\x1b[38;2;38;139;210m"
	UserFgBoldOpen = "\x1b[38;2;38;139;210;1m"
	AgentFgOpen    = "\x1b[38;2;133;153;0m"
	TaskFgOpen     = "\x1b[38;2;211;54;130m"

	// Tool step palette
	ToolAddOpen           = "\x1b[38;2;133;153;0m" // Solarized green #859900
	ToolRemoveOpen        = "\x1b[38;2;220;50;47m" // Solarized red   #DC322F
	ToolRemoveBodyOpen    = "\x1b[38;2;220;50;47;22m"
	ToolBulletOpen        = "\x1b[38;2;133;153;0m"
	ToolBulletRunningOpen = "\x1b[38;2;181;137;0m"
	ToolBulletErrOpen     = "\x1b[38;2;220;50;47m"
	ToolVerbOpen          = "\x1b[38;2;181;137;0;1m"
	ToolArgsOpen          = "\x1b[38;2;88;110;117m"
	ToolArgsOuterOpen     = "\x1b[38;2;88;110;117m"
	ToolHintOpen          = "\x1b[38;2;88;110;117m"
	ToolFailedSuffixOpen  = "\x1b[38;2;220;50;47;1m"

	// Step detail & diff
	DetailBulletOpen = "\x1b[38;2;88;110;117m"
	DiffNumOpen      = "\x1b[38;2;131;148;150m"
	DiffStripeOpen   = "\x1b[38;2;131;148;150;48;2;238;232;213m"

	// Thought & body
	ThoughtBulletOpen = "\x1b[38;2;88;110;117m"
	BodyOpen          = "\x1b[38;2;7;54;66m"

	// Spinner
	SpinnerBaseOpen = "\x1b[38;2;101;123;131m"
	SpinnerHiOpen   = "\x1b[38;2;38;139;210m"
)

// The colours the reference hard-codes. They are not scheme slots, so
// they do not move when the scheme changes.
const (
	// Tool step palette
	ToolBulletClose       = "\x1b[39m"
	ToolVerbClose         = "\x1b[0m"
	ToolArgsClose         = "\x1b[39m"
	ToolArgsOuterClose    = "\x1b[39m"
	ToolHintClose         = "\x1b[39m"
	ToolFailedSuffixClose = "\x1b[0m"

	// Step detail & diff
	DiffNumClose        = "\x1b[39m"
	DiffAddTintDark     = "\x1b[48;2;25;50;25m"
	DiffRemoveTintDark  = "\x1b[48;2;60;25;25m"
	DiffAddTintLight    = "\x1b[48;2;230;245;230m"
	DiffRemoveTintLight = "\x1b[48;2;250;230;230m"
	DiffAddTintOpen     = DiffAddTintDark
	DiffRemoveTintOpen  = DiffRemoveTintDark

	// Thought & body
	ThoughtBulletClose = "\x1b[39m"
	BodyClose          = "\x1b[39m"

	// Agent status — cycle 316.
	//
	// 【反编译】 model.SubagentDetailModel.buildHeaderLines picks the
	// status word's colour with lipgloss.Color("196") / Color("34") /
	// Color("220") — the ANSI-256 palette, not the true-colour one
	// the rest of this file carries. 196 is the red, 34 the blue and
	// 220 the amber of that palette.
	AgentErrorOpen  = "\x1b[38;5;196m"
	AgentDoneOpen   = "\x1b[38;5;34m"
	AgentActiveOpen = "\x1b[38;5;220m"

	// Inline code & formatting
	InlineCodeOpen  = "\x1b[38;5;32m\x1b[48;5;254m"
	InlineCodeClose = "\x1b[0m"
	BoldOpen        = "\x1b[1m"
	BoldClose       = "\x1b[22m"

	// Panel hint blue
	PanelBoldBlueOpen  = "\x1b[1m\x1b[38;5;69m"
	PanelBoldBlueClose = "\x1b[0m"

	// Standard closes
	BlueClose        = "\x1b[39m"
	BlueBoldClose    = "\x1b[0m"
	DimGrayClose     = "\x1b[39m"
	WarningClose     = "\x1b[39m"
	WarningBlueClose = "\x1b[0m"
	WarningBodyClose = "\x1b[39m"
	SeparatorClose   = "\x1b[0m"
	ToolAddClose     = "\x1b[39m"
	ToolRemoveClose  = "\x1b[39m"

	// Diff syntax highlight tokens
	SyntaxKeywordOpen = "\x1b[38;2;166;38;164m"
	SyntaxStringOpen  = "\x1b[38;2;80;161;79m"
	SyntaxNumberOpen  = "\x1b[38;2;152;104;1m"
	SyntaxCommentOpen = "\x1b[38;2;160;161;167m\x1b[3m"
	SyntaxTypeOpen    = "\x1b[38;2;229;192;123m"
	SyntaxTokenClose  = "\x1b[23m\x1b[39m"
)

func init() {
	applyScheme(SolarizedLight)
}

// applyScheme recomputes the package's colours from a scheme's slots.
//
// 【一手】 every colour above whose recovered value is one of the
// scheme's slots — the accent, the dim foreground, the warning, the
// muted foreground, the border, the add and remove foregrounds, the
// tool foreground, the error foreground, the line-number foreground and
// the body foreground — is that slot. The remaining ones are the
// colours the reference hard-codes, and they do not move.
func applyScheme(sc SchemeColors) {
	// Chrome & status
	BlueOpen = sc.Accent.Seq(false)
	BlueBoldOpen = seqWith(sc.Accent, false, "1")
	DimGrayOpen = sc.DimFg.Seq(false)
	WarningOpen = sc.WarningFg.Seq(false)
	WarningBlueOpen = seqWith(sc.Accent, false, "1")
	WarningBodyOpen = seqWith(sc.MutedFg, false, "22")
	SeparatorOpen = sc.Border.Seq(false)
	SubtleOpen = sc.MutedFg.Seq(false)

	// The user's and the agent's own colours
	UserFgOpen = sc.UserFg.Seq(false)
	UserFgBoldOpen = seqWith(sc.UserFg, false, "1")
	AgentFgOpen = sc.AgentFg.Seq(false)
	TaskFgOpen = sc.TaskFg.Seq(false)

	// Tool step palette
	ToolAddOpen = sc.AddFg.Seq(false)
	ToolRemoveOpen = sc.RemoveFg.Seq(false)
	ToolRemoveBodyOpen = seqWith(sc.RemoveFg, false, "22")
	ToolBulletOpen = sc.AddFg.Seq(false)
	ToolBulletRunningOpen = sc.ToolFg.Seq(false)
	ToolBulletErrOpen = sc.ErrorFg.Seq(false)
	ToolVerbOpen = seqWith(sc.ToolFg, false, "1")
	ToolArgsOpen = sc.MutedFg.Seq(false)
	ToolArgsOuterOpen = sc.MutedFg.Seq(false)
	ToolHintOpen = sc.MutedFg.Seq(false)
	ToolFailedSuffixOpen = seqWith(sc.ErrorFg, false, "1")

	// Step detail & diff
	DetailBulletOpen = sc.MutedFg.Seq(false)
	DiffNumOpen = sc.LineNumFg.Seq(false)
	DiffStripeOpen = pairSeq(sc.LineNumFg, sc.Border)

	// Thought & body
	ThoughtBulletOpen = sc.MutedFg.Seq(false)
	BodyOpen = sc.Fg.Seq(false)

	// Spinner
	SpinnerBaseOpen = sc.DimFg.Seq(false)
	SpinnerHiOpen = sc.Accent.Seq(false)
}
