// Command storybook runs the interactive Antigravity TUI Component Storybook & Playground.
// It allows developers to browse, inspect, and interactively test every UI component in isolation.
package main

import (
	"fmt"
	"os"

	_ "github.com/justwasm/pigasus/dns"
	"github.com/justwasm/pigasus/internal/ui/storybook"
	sb "github.com/justwasm/storybook"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			printUsage()
			return
		}
	}

	cat := storybook.DefaultCatalog()
	storybook.SyncCatalogToSubstrate(cat)

	if err := sb.Run(sb.WithTitle("❖ Antigravity TUI Component Storybook & Substrate")); err != nil {
		fmt.Fprintf(os.Stderr, "storybook: error running interactive program: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("❖ Antigravity TUI Component Storybook & Playground")
	fmt.Println()
	fmt.Println("Usage: storybook [flags]")
	fmt.Println()
	fmt.Println("Interactive Controls:")
	fmt.Println("  Sidebar Mode:")
	fmt.Println("    ↑ / ↓, j / k      Navigate through component stories and categories")
	fmt.Println("    Enter / l         Focus playground / interact with selected component")
	fmt.Println("    Tab               Cycle component state variations (Default, Active, Error, etc.)")
	fmt.Println("    1..9              Directly jump to a state preset by number")
	fmt.Println("    /                 Filter component catalog by name or category")
	fmt.Println("    t                 Cycle visual color themes (Solarized Light, Dark, Tokyo Night, Terminal)")
	fmt.Println("    q / Ctrl+C        Quit storybook")
	fmt.Println()
	fmt.Println("  Playground Mode:")
	fmt.Println("    Typing / Keys     Interact with component directly (e.g. prompt, sliders, lists)")
	fmt.Println("    Esc               Exit playground mode and return to sidebar")
	fmt.Println()
	fmt.Println("Architectural Categories:")
	for _, c := range storybook.AllCategories {
		fmt.Printf("  • %s\n", c)
	}
}
