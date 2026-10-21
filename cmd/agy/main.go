// Command agy is a transparent launcher for the official Antigravity CLI that
// adds custom models (MiniMax M3 first) and captures CloudCode traffic.
//
//	agy [agy args …]                 # proxy on, exec the real agy
//	agy wrapper models               # list configured models
//	agy wrapper doctor               # environment + upstream diagnosis
//
// It works by exporting CLOUD_CODE_URL=http://127.0.0.1:<port> before exec —
// agy 1.4.2 honours that variable, so the vendor binary is never modified.
// See cmd/agy/README.md and docs/cycles/cycle-274/.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the whole argument contract in one function:
//
//   - a literal first argument "wrapper" selects the admin subcommands;
//   - everything else is passed to the real agy untouched.
//
// Keeping the branch here (rather than in main) is what makes
// wrapperDispatch testable without exec'ing anything.
// Test seams: the two paths are swapped by main_test.go to observe dispatch
// without exec'ing a real agy.
var (
	launcherFunc = runLauncher
	wrapperFunc  = runWrapper
)

func run(args []string) int {
	if len(args) > 0 && args[0] == "wrapper" {
		return wrapperFunc(args[1:])
	}
	return launcherFunc(args)
}
