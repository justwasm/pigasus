// Package keybind is a backward-compatible facade over internal/ui/editor/keybind.
package keybind

import (
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
)

const (
	EscReleaseWindow = keybind.EscReleaseWindow
)

type (
	Keybindings   = keybind.Keybindings
	EscTimeoutMsg = keybind.EscTimeoutMsg
	EscBuffer     = keybind.EscBuffer
)

var (
	TermNeedsOptionMeta = keybind.TermNeedsOptionMeta
	DefaultPath         = keybind.DefaultPath
	Load                = keybind.Load
	ParseBinding        = keybind.ParseBinding
	IsAltLetter         = keybind.IsAltLetter
	IsTypedText         = keybind.IsTypedText
)
