package livechat

import (
	"io"

	"github.com/justwasm/pigasus/internal/core/cliflags"
)

// SubcommandSpec returns the subcommand specification for `pigasus livechat`.
func SubcommandSpec() cliflags.SubcommandSpec {
	return cliflags.SubcommandSpec{
		Name:    "livechat",
		Aliases: []string{"live"},
		Short:   "Start proactive voice & multimodal LiveChat (TUI or Web UI)",
		HelpFn: func(prog string) string {
			return "Usage of livechat:\n\n" +
				"  " + prog + " livechat [flags]\n\n" +
				"Flags:\n" +
				"  -w, --web           Start embedded LiveChat Web UI server\n" +
				"      --port <port>   HTTP server port (default 8090)\n" +
				"      --no-browser    Do not automatically open browser on startup in web mode\n" +
				"      --voice <name>  Voice persona (Fola, Arlo, Cleo, Finn, Gero, Lora, Neno, Olin, Tari)\n" +
				"      --model <name>  Gemini Live model name (default: models/gemini-3.8-live)\n" +
				"      --workspace <dir> Workspace directory to monitor for changes\n" +
				"      --duplex        Enable full duplex mode (headphones only)\n" +
				"      --debug-wire    Show real-time WebSocket and audio frame diagnostics\n" +
				"      --resume <id>   Resume a saved LiveChat session\n" +
				"      --list-sessions List all saved LiveChat sessions and exit\n"
		},
		Run: func(w io.Writer, args []string) error {
			return Run(args)
		},
	}
}

func init() {
	cliflags.RegisterProvider("livechat", SubcommandSpec)
}
