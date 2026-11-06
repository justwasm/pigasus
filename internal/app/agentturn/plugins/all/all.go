// Package all imports all built-in agentturn backend plugins for side effects.
//
// Importing this package registers all engine drivers ("agy", "pigasus",
// "claude", "codex", "grok", "opencode", "pi") into agentturn's driver registry.
package all

import (
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/agy"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/claude"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/codex"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/crush"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/grok"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/opencode"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/pi"
)
