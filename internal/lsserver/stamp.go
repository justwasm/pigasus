// Package lsserver — stamp.go: the `-stamp` answer.
//
// The IDE asks the language server for its build identity before it
// spawns it — `execFile(LS_BINARY, ['--stamp'])` and a regex over
// stdout (out/ide-language-server/ide-js/dist/languageServer.js).
// The regex is `/Built at CL: (\d+)/`, so the clone has to print a
// numeric changelist on stdout or the IDE's crash report loses the
// field.
//
// The reference's four lines are:
//
//	Built on: 2026-10-23 04:48:42 +0800 CST
//	Built at CL: 1005016104
//	Build label:
//	Built info: changelist 1005016104 with baseline 1002541031 in a mint client based on //depot/branches/antigravity_web_release_branch/1002541031.1/google3
//
// The clone reports the same identity, the way `cli.ReferenceVersion`
// reports the reference CLI's version: the replica stands in for the
// reference release it replicates. The four values are package
// variables, so a native build can stamp itself with -ldflags.
//
// Evidence grade: 一手 — captured from the reference binary
// (out/langserver/stamp.txt).
package lsserver

import (
	"fmt"
	"io"
)

// The reference release the clone stands in for. -ldflags may
// override each of them.
var (
	// StampBuiltOn is the reference binary's build time.
	StampBuiltOn = "2026-10-28 03:41:13 +0800 CST"
	// StampCL is the reference binary's changelist number.
	StampCL = "1008300195"
	// StampBaseline is the reference binary's baseline changelist.
	StampBaseline = "1006933201"
	// StampLabel is the reference binary's build label. The
	// reference prints an empty one.
	StampLabel = ""
	// StampBranch is the branch the reference binary was cut from.
	StampBranch = "//depot/branches/antigravity_web_release_branch/1006933201.1/google3"
)

// WriteStamp writes the reference's stamp to w.
//
// The reference writes it to stdout — the IDE reads stdout, and the
// reference's own logging goes to stderr.
func WriteStamp(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Built on: %s\n", StampBuiltOn)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "Built at CL: %s\n", StampCL); err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "Build label: %s\n", StampLabel); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "Built info: changelist %s with baseline %s in a mint client based on %s\n",
		StampCL, StampBaseline, StampBranch)
	return err
}
