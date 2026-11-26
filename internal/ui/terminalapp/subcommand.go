package terminalapp

import (
	"io"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// SubcommandSpec returns the subcommand specification for `pigasus terminal`.
func SubcommandSpec() cliflags.SubcommandSpec {
	return cliflags.SubcommandSpec{
		Name:    "terminal",
		Aliases: []string{"term"},
		Short:   "Start standalone interactive multi-tab terminal multiplexer",
		HelpFn: func(prog string) string {
			return "Usage of terminal:\n\n" +
				"  " + prog + " terminal [flags] [-- command...]\n" +
				"  " + prog + " terminal mcp [--socket <path>]\n\n" +
				"Flags:\n" +
				"  -C, --cwd <dir>       Initial working directory (default: current directory)\n" +
				"  -c, --command <cmd>   Initial command to run in the first terminal tab\n" +
				"  -t, --title <title>   Initial title for the first terminal tab\n" +
				"      --socket <path>   Unix socket path for IPC/MCP control\n" +
				"      --remain-on-exit  Keep exited tabs open instead of closing automatically\n\n" +
				"Shortcuts:\n" +
				"  Esc                   Passed directly to the running subprocess\n" +
				"  Alt+N                 Open a new terminal tab\n" +
				"  Alt+W                 Close the active terminal tab\n" +
				"  Alt+Left / Alt+[      Switch to previous tab\n" +
				"  Alt+Right / Alt+]     Switch to next tab\n" +
				"  Alt+1..9              Switch to tab 1..9\n" +
				"  Alt+C / Ctrl+Space    Toggle vi-style Copy Mode\n" +
				"  Alt+M / F10           Open Terminal Menu\n" +
				"  Alt+Q                 Quit terminal multiplexer\n"
		},
		Run: func(w io.Writer, args []string) error {
			return Run(w, args)
		},
	}
}

func init() {
	cliflags.RegisterProvider("terminal", SubcommandSpec)
}
