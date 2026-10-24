// Package palette provides the recovered reference true-colour ANSI palette.
//
// Cycle 126 / 283 / 285: the reference binary emits 24-bit RGB colour escapes
// (\x1b[38;2;R;G;Bm) directly to the terminal, recovered from truecolour pty
// captures of agy 1.4.3 (COLORTERM=truecolor).
package palette

// ANSI true-colour opens for the recovered reference palette.
const (
	// Chrome & status
	BlueOpen        = "\x1b[38;2;38;139;210m"
	BlueBoldOpen    = "\x1b[38;2;38;139;210;1m"
	DimGrayOpen     = "\x1b[38;2;101;123;131m"
	WarningOpen     = "\x1b[38;2;203;75;22m"
	WarningBlueOpen = "\x1b[38;2;38;139;210;1m"
	WarningBodyOpen = "\x1b[38;2;88;110;117;22m"
	SeparatorOpen   = "\x1b[38;2;238;232;213m"
	SubtleOpen      = "\x1b[38;2;88;110;117m"

	// Tool step palette
	ToolAddOpen            = "\x1b[38;2;133;153;0m" // Solarized green #859900
	ToolRemoveOpen         = "\x1b[38;2;220;50;47m" // Solarized red   #DC322F
	ToolBulletOpen         = "\x1b[38;2;133;153;0m"
	ToolBulletClose        = "\x1b[39m"
	ToolBulletRunningOpen  = "\x1b[38;2;181;137;0m"
	ToolBulletErrOpen      = "\x1b[38;2;220;50;47m"
	ToolVerbOpen           = "\x1b[38;2;181;137;0;1m"
	ToolVerbClose          = "\x1b[0m"
	ToolArgsOpen           = "\x1b[38;2;88;110;117m"
	ToolArgsClose          = "\x1b[39m"
	ToolArgsOuterOpen      = "\x1b[38;2;88;110;117m"
	ToolArgsOuterClose     = "\x1b[39m"
	ToolHintOpen           = "\x1b[38;2;88;110;117m"
	ToolHintClose          = "\x1b[39m"
	ToolFailedSuffixOpen   = "\x1b[38;2;220;50;47;1m"
	ToolFailedSuffixClose  = "\x1b[0m"

	// Step detail & diff
	DetailBulletOpen     = "\x1b[38;2;88;110;117m"
	DiffNumOpen          = "\x1b[38;2;131;148;150m"
	DiffStripeOpen       = "\x1b[38;2;131;148;150;48;2;238;232;213m"
	DiffAddTintDark      = "\x1b[48;2;25;50;25m"
	DiffRemoveTintDark   = "\x1b[48;2;60;25;25m"
	DiffAddTintLight     = "\x1b[48;2;230;245;230m"
	DiffRemoveTintLight  = "\x1b[48;2;250;230;230m"
	DiffAddTintOpen      = DiffAddTintDark
	DiffRemoveTintOpen   = DiffRemoveTintDark

	// Thought & body
	ThoughtBulletOpen  = "\x1b[38;2;88;110;117m"
	ThoughtBulletClose = "\x1b[39m"
	BodyOpen           = "\x1b[38;2;7;54;66m"
	BodyClose          = "\x1b[39m"

	// Spinner
	SpinnerBaseOpen = "\x1b[38;2;101;123;131m"
	SpinnerHiOpen   = "\x1b[38;2;38;139;210m"

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
