package storybook

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/palette"
)

const (
	ansiReset        = "\x1b[0m"
	ansiBoldOpen     = "\x1b[1m"
	ansiBoldClose    = "\x1b[22m"
	ansiDimOpen      = "\x1b[2m"
	ansiDimClose     = "\x1b[22m"
	ansiReverseOpen  = "\x1b[7m"
	ansiReverseClose = "\x1b[27m"

	ansiBlueOpen     = "\x1b[38;2;38;139;210m"
	ansiBlueClose    = "\x1b[39m"
	ansiDimGrayOpen  = "\x1b[38;2;101;123;131m"
	ansiDimGrayClose = "\x1b[39m"
	ansiYellowOpen   = "\x1b[33m"
	ansiYellowClose  = "\x1b[39m"
	ansiGreenOpen    = "\x1b[32m"
	ansiGreenClose   = "\x1b[39m"
	ansiRedOpen      = "\x1b[31m"
	ansiRedClose     = "\x1b[39m"
	ansiCyanOpen     = "\x1b[36m"
	ansiCyanClose    = "\x1b[39m"
	ansiWhiteOpen    = "\x1b[97m"
	ansiWhiteClose   = "\x1b[39m"
)

func dim(s string) string {
	return ansiDimGrayOpen + s + ansiDimGrayClose
}

func bold(s string) string {
	return ansiBoldOpen + s + ansiBoldClose
}

func blue(s string) string {
	return ansiBlueOpen + s + ansiBlueClose
}

func reverse(s string) string {
	return ansiReverseOpen + s + ansiReverseClose
}

func yellow(s string) string {
	return ansiYellowOpen + s + ansiYellowClose
}

func green(s string) string {
	return ansiGreenOpen + s + ansiGreenClose
}

func red(s string) string {
	return ansiRedOpen + s + ansiRedClose
}

func cyan(s string) string {
	return ansiCyanOpen + s + ansiCyanClose
}

// isCtrlO checks whether a key event represents the global Ctrl+O trajectory toggle chord.
func isCtrlO(msg tea.KeyMsg) bool {
	k := msg.Key()
	return (k.Code == 'o' || k.Code == 'O') && (k.Mod.Contains(tea.ModCtrl) || k.Mod&tea.ModCtrl != 0)
}

// ensure palette is referenced to satisfy imports
var _ = palette.BlueOpen
