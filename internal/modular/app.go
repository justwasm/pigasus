package modular

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/cliflags"
)

// AppOptions configures the modular CLI entry point.
type AppOptions struct {
	Name                string
	Description         string
	LongDesc            string
	Version             string
	DefaultEngine       string
	Subcommands         []cli.SubcommandSpec
	InteractiveRunner   func(w io.Writer, fs *flag.FlagSet) error
	TUISnapshotRenderer func(w io.Writer) error
}

// App is the modular CLI application runner.
type App struct {
	opts   AppOptions
	parser *cli.Parser
}

// NewApp creates a modular CLI application.
func NewApp(opts AppOptions) *App {
	if opts.Name == "" {
		opts.Name = "pigga"
	}
	if opts.Version == "" {
		opts.Version = cli.ReferenceVersion + "-wasm"
	}
	cli.SetProgramVersion(opts.Version)
	if opts.Description == "" {
		opts.Description = "Modular Antigravity CLI (WASM edition)"
	}
	if opts.LongDesc == "" {
		opts.LongDesc = "pigga is a modular, streamlined shell of Google Antigravity CLI.\n" +
			"It selectively includes WASM-compatible toolsets and engines while\n" +
			"excluding OS-dependent features like PTYs and local daemon sockets."
	}
	if opts.DefaultEngine != "" {
		SetDefaultEngine(opts.DefaultEngine)
	}

	app := &App{opts: opts}
	app.setupParser()
	return app
}

func (a *App) setupParser() {
	var subs []cli.SubcommandSpec
	if len(a.opts.Subcommands) > 0 {
		subs = a.opts.Subcommands
	} else {
		registered := GetSubcommands()
		if len(registered) > 0 {
			subs = registered
			hasHelp := false
			for _, s := range subs {
				if s.Name == "help" {
					hasHelp = true
					break
				}
			}
			if !hasHelp {
				subs = append(subs, cli.HelpSubcommand())
			}
		} else {
			subs = cli.WASMSubcommands()
		}
	}

	p := cli.NewParser(
		cli.StandardFlags(),
		subs,
		a.opts.Name,
		a.opts.Description,
		a.opts.LongDesc,
		cli.ExtraFlags()...,
	)

	p.Root().Version = a.opts.Version + "\n"
	p.Root().RootRun = func(args []string) error {
		return a.runRoot(args, p)
	}

	a.parser = p
}

func (a *App) runRoot(args []string, p *cli.Parser) error {
	out := p.Root().Out
	fs := p.Root().FlagSet()
	if fs == nil {
		return nil
	}

	interactiveRunner := a.opts.InteractiveRunner
	if interactiveRunner == nil {
		interactiveRunner = GetTUIRunner()
	}

	printMode := cliflags.WasSet(fs, "print") || cliflags.WasSet(fs, "prompt") || cliflags.WasSet(fs, "p")
	promptInter := cliflags.WasSet(fs, "prompt-interactive") || cliflags.WasSet(fs, "i")
	if printMode && promptInter {
		fmt.Fprintln(os.Stderr, "error: --print and --prompt-interactive are mutually exclusive")
		return fmt.Errorf("error: --print and --prompt-interactive are mutually exclusive")
	}

	prompt := ""
	for _, n := range []string{"print", "prompt", "p"} {
		if cliflags.WasSet(fs, n) {
			prompt = cliflags.StringValue(fs, n)
			break
		}
	}

	if !printMode && len(args) > 0 {
		prompt = strings.Join(args, " ")
		printMode = true
	}

	if printMode {
		if prompt == "" {
			fmt.Fprintln(os.Stderr, a.opts.Name+": --print requires a prompt argument")
			return fmt.Errorf("missing prompt")
		}
		return a.executeTurn(fs, prompt, out)
	}

	if promptInter || cliflags.ResolveBoolAlias(fs, "continue", "c") {
		if interactiveRunner != nil {
			return interactiveRunner(out, fs)
		}
		fmt.Fprintf(os.Stderr, "%s: interactive TUI is not available in this build. Run with a prompt or --print \"<prompt>\".\n", a.opts.Name)
		return nil
	}

	// No flags, no args: in native with TUI runner, start interactive TUI; in WASM or non-TUI, print help.
	if len(args) == 0 {
		if interactiveRunner != nil {
			return interactiveRunner(out, fs)
		}
		p.Root().PrintHelp()
		return nil
	}

	p.Root().PrintHelp()
	return nil
}

func (a *App) executeTurn(fs *flag.FlagSet, prompt string, out io.Writer) error {
	eng, err := DefaultEngine()
	if err != nil {
		return err
	}

	model := cliflags.StringValue(fs, "model")
	effort := cliflags.StringValue(fs, "effort")
	outFormat := cliflags.StringValue(fs, "output-format")

	var buf strings.Builder
	var targetOut io.Writer = out
	if outFormat == "json" {
		targetOut = &buf
	}

	input := TurnInput{
		Prompt:  prompt,
		Model:   model,
		Effort:  effort,
		FlagSet: fs,
		Out:     targetOut,
		ErrOut:  os.Stderr,
	}

	ctx := context.Background()
	res, err := eng.Send(ctx, input)
	if err != nil {
		if outFormat == "json" {
			errResp := map[string]any{
				"conversation_id": "",
				"status":          "ERROR",
				"response":        "",
				"error":           err.Error(),
				"num_turns":       1,
			}
			data, _ := json.Marshal(errResp)
			fmt.Fprintln(out, string(data))
			return err
		}
		return err
	}

	if outFormat == "json" {
		jsonResp := map[string]any{
			"conversation_id":  "",
			"status":           "SUCCESS",
			"response":         res.Response,
			"duration_seconds": 0,
			"num_turns":        res.TurnCount,
			"usage": map[string]any{
				"input_tokens":  res.InputTokens,
				"output_tokens": res.OutputTokens,
			},
		}
		data, _ := json.Marshal(jsonResp)
		fmt.Fprintln(out, string(data))
	} else {
		fmt.Fprintln(out)
	}
	return nil
}


// Run executes the CLI.
func (a *App) Run(args []string) int {
	if a.parser == nil {
		a.setupParser()
	}
	var execArgs []string
	if args == nil {
		execArgs = os.Args[1:]
	} else if len(args) > 1 {
		execArgs = args[1:]
	} else {
		execArgs = []string{}
	}
	if err := a.parser.ExecuteArgs(execArgs); err != nil {
		return a.parser.ExitCode()
	}
	return 0
}
