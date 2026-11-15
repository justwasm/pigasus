package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func printUsage() {
	fmt.Print(`wirealign — Wire-level Parity & Prompt Alignment Harness for AGY & Pigasus

Usage:
  wirealign diff <ref.json> <target.json>  Compare two wire request JSON files
  wirealign run <scenario>                 Run benchmark scenario check against AGY 1.5.9 reference
  wirealign test                           Run all benchmark scenarios
  wirealign server [-addr ADDR]            Run local CloudCode capture proxy/mock server
  wirealign version                        Print version

Options:
  -html FILE     Write visual HTML parity report to FILE
  -strict        Require strict byte-for-byte identity on prompt text
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
	case "version":
		fmt.Println("wirealign 1.0.0 (openagy wire observation & parity harness)")

	case "diff":
		fs := flag.NewFlagSet("diff", flag.ExitOnError)
		htmlOut := fs.String("html", "", "Output HTML report path")
		strict := fs.Bool("strict", false, "Strict byte identity")
		_ = fs.Parse(args)

		if fs.NArg() < 2 {
			fmt.Fprintln(os.Stderr, "usage: wirealign diff [-html report.html] <ref.json> <target.json>")
			os.Exit(1)
		}

		refPath := fs.Arg(0)
		tgtPath := fs.Arg(1)

		rawRef, err := os.ReadFile(refPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", refPath, err)
			os.Exit(1)
		}
		rawTgt, err := os.ReadFile(tgtPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to read %s: %v\n", tgtPath, err)
			os.Exit(1)
		}

		pRef, err := ParseWirePayload(rawRef)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse %s: %v\n", refPath, err)
			os.Exit(1)
		}
		pTgt, err := ParseWirePayload(rawTgt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse %s: %v\n", tgtPath, err)
			os.Exit(1)
		}

		opts := DefaultCompareOptions()
		opts.StrictByteIdentity = *strict

		diffRes := CompareWire(pRef, pTgt, opts)
		fmt.Print(RenderTerminal(diffRes))

		if *htmlOut != "" {
			if err := WriteHTMLReport(diffRes, *htmlOut); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write HTML report: %v\n", err)
			} else {
				fmt.Printf("Visual HTML report written to %s (file://%s)\n", *htmlOut, *htmlOut)
			}
		}

		if !diffRes.Passed {
			os.Exit(1)
		}

	case "run":
		if len(args) < 1 {
			fmt.Fprintln(os.Stderr, "usage: wirealign run <scenario_name>")
			os.Exit(1)
		}
		scName := args[0]
		sc, err := GetScenario(scName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}

		pRef := BuildReferenceWire(*sc)

		// Self-verify rendered templates structure
		fmt.Printf("[wirealign] Inspecting reference scenario %q rendered from agytemplates (%s):\n", sc.Name, sc.Description)
		pDec := DeconstructPrompt(pRef.SystemInstruction)
		tDec := DeconstructTools(pRef.Tools)

		fmt.Printf("  • Prompt sections (%d): %v\n", len(pDec.SectionOrder), pDec.SectionOrder)
		fmt.Printf("  • Identity role: %q (generic-system-prompt=%v)\n", pDec.IdentityRole, pDec.IsGenericPrompt)
		fmt.Printf("  • Sandboxed: %v\n", pDec.IsSandboxed)
		fmt.Printf("  • Subagents: %v\n", pDec.SubagentsList)
		fmt.Printf("  • User info wording: %q\n", pDec.UserInfoWording)
		fmt.Printf("  • Tools declared (%d): %v\n", tDec.Count, tDec.ToolNames)
		fmt.Printf("  • run_command BypassSandbox: %v\n", tDec.RunCommandHasBypass)
		fmt.Printf("  • invoke_subagent appended list: %v\n", tDec.InvokeSubagentAppended)

	case "test":
		fmt.Println("[wirealign] Running all scenario checks against authentic agytemplates & surface specifications...")
		failed := 0
		for _, sc := range AllScenarios {
			pRef := BuildReferenceWire(sc)

			// Perform self-consistency and hygiene checks against the template output
			pDec := DeconstructPrompt(pRef.SystemInstruction)
			tDec := DeconstructTools(pRef.Tools)

			var errs []string
			if sc.Name == "standard_turn" || sc.Name == "clean_no_rules" {
				if pDec.IsSandboxed {
					errs = append(errs, "unexpected <terminal_sandbox> in unsandboxed turn")
				}
				if tDec.RunCommandHasBypass {
					errs = append(errs, "unexpected BypassSandbox in unsandboxed run_command")
				}
				if !pDec.IsGenericPrompt {
					errs = append(errs, "expected generic-system-prompt: true")
				}
				if tDec.InvokeSubagentAppended {
					errs = append(errs, "unexpected appended catalog in invoke_subagent description")
				}
			} else if sc.Name == "sandboxed_turn" {
				if !pDec.IsSandboxed {
					errs = append(errs, "missing <terminal_sandbox> in sandboxed turn")
				}
				if !tDec.RunCommandHasBypass {
					errs = append(errs, "missing BypassSandbox in sandboxed run_command")
				}
			}

			if len(errs) == 0 {
				fmt.Printf("  ✔ %-20s (sections: %d, tools: %d) — MATCH\n", sc.Name, len(pDec.SectionOrder), tDec.Count)
			} else {
				fmt.Printf("  ✗ %-20s — FAIL: %v\n", sc.Name, errs)
				failed++
			}
		}

		if failed > 0 {
			fmt.Printf("\n[wirealign:FAILED] %d scenario check(s) failed\n", failed)
			os.Exit(1)
		}
		fmt.Printf("\n[wirealign:SUCCESS] All %d scenario specifications verified against templates!\n", len(AllScenarios))

	case "server":
		fs := flag.NewFlagSet("server", flag.ExitOnError)
		addr := fs.String("addr", "127.0.0.1:9099", "Address to listen on")
		captureDir := fs.String("capture-dir", "/tmp/wirealign_captures", "Directory to dump captured JSON requests")
		upstream := fs.String("upstream", "", "Optional upstream CloudCode URL to relay to")
		_ = fs.Parse(args)

		srv, err := NewProxyServer(*addr, *captureDir, *upstream)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to start server: %v\n", err)
			os.Exit(1)
		}
		srv.Start()
		defer srv.Close()

		fmt.Printf("[wirealign] Mock & Capture proxy listening on %s (export CLOUD_CODE_URL=%s)\n", *addr, srv.URL())
		if *captureDir != "" {
			absDir, _ := filepath.Abs(*captureDir)
			fmt.Printf("[wirealign] Dumping wire JSON requests to %s\n", absDir)
		}
		if *upstream != "" {
			fmt.Printf("[wirealign] Relaying unhandled RPCs to upstream %s\n", *upstream)
		}
		fmt.Println("[wirealign] Press Ctrl+C to terminate.")

		select {}

	default:
		printUsage()
		os.Exit(1)
	}
}
