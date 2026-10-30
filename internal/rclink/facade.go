// Package rclink is a backward-compatible facade over internal/infra/remote/link.
package rclink

import (
	"github.com/justwasm/pigasus/internal/infra/remote/link"
)

var (
	DeepLink            = link.DeepLink
	BrowserURL          = link.BrowserURL
	BrowserURLForAccount = link.BrowserURLForAccount
	ActiveAccount       = link.ActiveAccount
)
