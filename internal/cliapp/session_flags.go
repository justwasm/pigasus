package cliapp

import (
	"flag"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	browserflags "github.com/justwasm/pigasus/internal/core/cliflags/browser"
	logfileflags "github.com/justwasm/pigasus/internal/core/cliflags/logfile"
	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
)

// applySessionFlags applies the flags a session starts with: the browser's
// policy, the project the session runs in and the CLI's log file.
//
// The policies live below the host — internal/core/cliflags/browser,
// internal/core/cliflags/projects and internal/core/cliflags/logfile own
// them — and the host keeps the wiring: where they are applied, and what
// the session needs from them.
//
// A new project's id is a conversation id, so the generator comes from the
// conversation's own package.
func applySessionFlags(fs *flag.FlagSet) {
	browserflags.Apply(fs)
	projectflags.Apply(fs, projectflags.Options{NewID: agyprompt.NewConversationID})
	logfileflags.Apply(fs)
}
