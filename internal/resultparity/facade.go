// Package resultparity is a backward-compatible facade over internal/infra/proxy/resultparity.
package resultparity

import (
	"github.com/justwasm/pigasus/internal/infra/proxy/resultparity"
)

type (
	Section = resultparity.Section
)

var (
	Load = resultparity.Load
)
