// Command browserdemo runs one browsing task in the clone's browser agent.
//
// It is the end-to-end check for the /browser slash command: the same
// runner the command builds, driven from the command line.
//
//	go run ./devutils/browserdemo "open https://example.com and report the page title"
//
// With --live the agent drives the browser the *user* already runs, through
// the Open Claude in Chrome extension (browserbridge) — the same
// surface the reference's --use_local_chrome asks for:
//
//	go run ./devutils/browserdemo --live "open https://example.com and report the page title"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backendselect"
	"github.com/justwasm/pigasus/internal/browsersubagent"
	"github.com/justwasm/pigasus/internal/modelpicker"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

func main() {
	model := flag.String("model", "minimax-m3.1-flash", "the model the browser agent runs on")
	task := flag.String("task", "open https://example.com and report the page title", "the browsing task")
	live := flag.Bool("live", false, "drive the browser the user already runs, through the Open Claude in Chrome extension")
	flag.Parse()

	fs := flag.NewFlagSet("browserdemo", flag.ContinueOnError)
	be := backendselect.Pick(fs, *model, modelpicker.ResolveCatalogueID)
	if be == nil {
		fmt.Fprintln(os.Stderr, "no backend for", *model)
		os.Exit(1)
	}
	piModel := printmode.PigPrintModel(be, *model)
	if piModel == nil {
		fmt.Fprintln(os.Stderr, "no model for", *model)
		os.Exit(1)
	}
	ws, _ := os.Getwd()
	agentDir := auth.AntigravityConfigHome()

	// The live surface is the reference's --use_local_chrome: the browser
	// the user already runs, not one the clone starts.
	if *live {
		cfg := browsercdp.CurrentConfig()
		cfg.UseLocalChrome = true
		browsercdp.Configure(cfg)
	}

	// The browser first: without one every tool fails, and the agent
	// retries new tools instead of giving up (it only knows that a tool
	// failed). The probe starts nothing — the browser starts on the
	// first tool call, the reference's rule.
	surface, err := browsersubagent.Probe(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "browser:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "surface: %s\n", surface)

	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      ws,
		AgentDir: agentDir,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "services:", err)
		os.Exit(1)
	}
	defer services.Close()

	catalog := tools.RuntimeCatalog(ws)
	runner := browsersubagent.New(subagenttools.Deps{
		Services: services,
		Model:    piModel,
		CWD:      ws,
		AgentDir: agentDir,
	}, browsersubagent.Tools(catalog))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	answer, err := runner.RunBrowserTask(ctx, *task)
	// The demo starts the browser itself; it stops with the demo.
	// (os.Exit below skips the deferred calls, so it is done here.)
	browsercdp.Shutdown()
	if err != nil {
		fmt.Fprintln(os.Stderr, "browser agent:", err)
		os.Exit(1)
	}
	fmt.Println(answer)
}
