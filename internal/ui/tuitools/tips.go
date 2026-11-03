package tuitools

import (
	"math/rand"
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// BuiltinTips contains the 14 tip strings from official agy 1.4.12
// (google3/third_party/jetski/cli/store/store.AllTipStrings, 0x1086b1920).
var BuiltinTips = []string{
	"Use /help to see all commands.",
	"Use /btw to ask a question without breaking context.",
	"You can switch conversations with /resume.",
	"Use /settings to configure your environment.",
	"Use /feedback to share your experience with the team.",
	"Use /model to switch between available models.",
	"Use /skills to browse and manage agent skills.",
	"Use /cs to search in your codebase.",
	"Use /voice (or press f5) to record your voice.",
	"To disable tips, toggle Show Tips in /settings.",
	"Press esc to interrupt generation.",
	"Press ctrl+o to expand or collapse tool steps.",
	"Press ctrl+g to open an external editor for the prompt.",
	"Press ? to see keyboard shortcuts.",
}

// RandomTip returns a randomly chosen tip from BuiltinTips.
func RandomTip() string {
	if len(BuiltinTips) == 0 {
		return ""
	}
	return BuiltinTips[rand.Intn(len(BuiltinTips))]
}

// FormatTip formats a tip line with the official "   └ Tip: " prefix and dim gray styling.
func FormatTip(tip string) string {
	tip = strings.TrimSpace(tip)
	if tip == "" {
		return ""
	}
	return palette.DimGrayOpen + "   └ Tip: " + tip + palette.DimGrayClose
}
