// Package logfile owns the CLI's --log-file flag: the file the CLI's log
// goes to.
//
// The flag is the reference's own:
//
//	--log-file    string    Override CLI log file path
//
// The reference routes the log through its termio redirect
// (internal/cli's RedirectStderr), so that the terminal stays clean and
// every log line goes to the file. The redirect's only global effect is
// log.SetOutput — it does not swap the process's own streams — so the
// package does the same: a tier-1 package may not import the host that
// owns the recovered termio surface.
package logfile

import (
	"flag"
	"log"
	"os"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// FlagLogFile is the reference's flag name.
const FlagLogFile = "log-file"

// Apply routes the CLI's log to the file the --log-file flag names.
//
// The file is opened for append and created when it is not there yet. A
// path that cannot be opened leaves the log where it was.
func Apply(fs *flag.FlagSet) {
	if fs == nil {
		return
	}
	path := cliflags.StringValue(fs, FlagLogFile)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	log.SetOutput(f)
}
