// Package workspace answers "where am I?".
//
// The clone asks for the process's working directory in several
// places: the conversation store scopes a conversation to the
// workspace it was started in, the `-c` / `--continue` pick is
// workspace-scoped, and the trust list of the compatibility settings
// names the current directory. The answer is the same every time, so
// it has one owner — a package below every tier, because the
// conversation store (the app tier) and the hosts (above every tier)
// both need it.
//
// The directory is read through the operating system, so a process
// that changed its own directory sees the change.
package workspace

import "os"

// Current returns the process's working directory.
//
// It is the empty string when the directory cannot be read, which is
// the answer every caller gave before this package existed.
func Current() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}
