package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func printUsage() {
	fmt.Print(`tuialign — Pixel-perfect TUI alignment harness between agy and pigasus

Usage:
  tuialign run <scenario.align>       Run scripted parity test against agy & pigasus
  tuialign live [scenario.align]      Launch interactive side-by-side tmux session (sync typing)
  tuialign server [-addr ADDR]        Run standalone mock CloudCode server
  tuialign diff <left.txt> <right.txt> Compare two captured terminal screens
  tuialign version                    Print version

Environment variables:
  AGY_REAL_BIN   Path to reference agy binary (default: ~/.local/bin/agy)
  PIGASUS_BIN    Path to openagy binary (default: /tmp/pigasus)
`)
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "run":
		runScenarioCmd(args)
	case "live":
		runLiveCmd(args)
	case "server":
		runServerCmd(args)
	case "diff":
		runDiffCmd(args)
	case "version", "--version", "-v":
		fmt.Println("tuialign 1.0.0 (openagy testing harness)")
	default:
		// If first argument is an .align or .scenario file, default to "run"
		if strings.HasSuffix(cmd, ".align") || strings.HasSuffix(cmd, ".scenario") {
			runScenarioCmd(os.Args[1:])
			return
		}
		printUsage()
		os.Exit(1)
	}
}

func runScenarioCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	verbose := fs.Bool("v", false, "Verbose mock logging")
	fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "error: please provide a scenario file (*.align)")
		os.Exit(1)
	}

	scenarioPath := files[0]
	sc, err := ParseScenario(scenarioPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing scenario: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[tuialign] Running scenario: %q (window: %dx%d, target: %s)\n",
		sc.Name, sc.Width, sc.Height, sc.Target)

	// 1. Start Mock Server
	srv, err := NewMockServer("127.0.0.1:0", *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting mock server: %v\n", err)
		os.Exit(1)
	}
	srv.Start()
	defer srv.Close()

	fmt.Printf("[tuialign] Mock CloudCode proxy running at %s\n", srv.URL())

	// 2. Setup Tmux Driver
	driver, err := NewTmuxDriver(TmuxConfig{
		Width:        sc.Width,
		Height:       sc.Height,
		CloudCodeURL: srv.URL(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing tmux driver: %v\n", err)
		os.Exit(1)
	}
	defer driver.Close()

	// 3. Execute Scenario
	if err := sc.Run(srv, driver); err != nil {
		fmt.Fprintf(os.Stderr, "\n[tuialign:FAILED] %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n[tuialign:SUCCESS] Scenario completed successfully!")
}

func runLiveCmd(args []string) {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	width := fs.Int("w", 100, "Pane width")
	height := fs.Int("h", 30, "Pane height")
	fs.Parse(args)

	// 1. Start Mock Server
	srv, err := NewMockServer("127.0.0.1:58999", true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting mock server: %v\n", err)
		os.Exit(1)
	}
	srv.Start()
	defer srv.Close()

	// If a scenario file was given, load its mock turns
	if len(fs.Args()) > 0 {
		sc, err := ParseScenario(fs.Args()[0])
		if err == nil {
			for _, t := range sc.Turns {
				srv.AddTurn(t)
			}
			fmt.Printf("[tuialign:live] Loaded %d programmed turns from %s\n", len(sc.Turns), fs.Args()[0])
		}
	} else {
		// Provide a default welcoming conversation turn
		srv.AddTurn(MockTurn{
			Chunks: []MockChunk{
				{
					Thought:       "Inspecting interactive alignment session.",
					ThoughtTokens: 130,
					Delay:         100 * time.Millisecond,
				},
				{
					Text:            "Hello from deterministic Mock LLM! Type anything to test.",
					PromptTokens:    18200,
					CandidateTokens: 20,
					ThoughtTokens:   130,
					FinishReason:    "STOP",
					Delay:           200 * time.Millisecond,
				},
			},
		})
	}

	fmt.Printf("[tuialign:live] Mock Server running at %s\n", srv.URL())

	// 2. Setup Tmux Driver with synchronize-panes enabled
	sessionName := "tuialign_live"
	driver, err := NewTmuxDriver(TmuxConfig{
		SessionName:  sessionName,
		Width:        *width,
		Height:       *height,
		CloudCodeURL: srv.URL(),
		Synchronize:  true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing tmux: %v\n", err)
		os.Exit(1)
	}
	defer driver.Close()

	if err := driver.Launch(TargetBoth); err != nil {
		fmt.Fprintf(os.Stderr, "error launching panes: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n==================================================================")
	fmt.Println("  TUI LIVE ALIGNMENT SESSION STARTED")
	fmt.Println("  Left Pane:  agy (reference)")
	fmt.Println("  Right Pane: pigasus (target)")
	fmt.Println("  Keystrokes are synchronized across both panes in real-time!")
	fmt.Println("  To attach now, run:")
	fmt.Printf("      tmux -L tuialign attach-session -t %s\n", sessionName)
	fmt.Println("==================================================================")

	// Automatically attach if running in an interactive terminal
	if isTerminal() {
		attachCmd := exec.Command("tmux", "-L", driver.cfg.SocketName, "attach-session", "-t", sessionName)
		attachCmd.Stdin = os.Stdin
		attachCmd.Stdout = os.Stdout
		attachCmd.Stderr = os.Stderr
		_ = attachCmd.Run()
	} else {
		// Wait for signal in non-interactive shell
		select {}
	}
}

func runServerCmd(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:58999", "Listen address")
	fs.Parse(args)

	srv, err := NewMockServer(*addr, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	srv.Start()
	fmt.Printf("tuialign mock server listening on %s (export CLOUD_CODE_URL=%s)\n", *addr, srv.URL())
	select {}
}

func runDiffCmd(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tuialign diff <reference.txt> <target.txt>")
		os.Exit(1)
	}
	leftBytes, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", args[0], err)
		os.Exit(1)
	}
	rightBytes, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", args[1], err)
		os.Exit(1)
	}

	label := fmt.Sprintf("%s vs %s", filepath.Base(args[0]), filepath.Base(args[1]))
	res := CompareSnapshots(string(leftBytes), string(rightBytes), label)
	fmt.Print(res.Report)
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
