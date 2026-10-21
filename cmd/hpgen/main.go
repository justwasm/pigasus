// Command hpgen writes a synthetic long conversation into the clone's store
// so the render hot path can be measured on a 5000-step transcript. It is a
// throwaway measurement tool (cycle-281), not part of the shipped surface.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/convstore"
)

func main() {
	steps := flag.Int("steps", 5000, "number of steps")
	replyBytes := flag.Int("reply", 0, "pad each model reply to this many bytes")
	id := flag.String("id", "hp-synth-5000", "conversation id")
	workspace := flag.String("workspace", "/tmp", "workspace")
	flag.Parse()

	// The generator writes through convstore, which resolves the store from
	// $ANTIGRAVITY_HOME / $HOME. Refuse to run without an explicit throwaway
	// home so it can never append a synthetic conversation to a real store.
	if os.Getenv("ANTIGRAVITY_HOME") == "" {
		log.Fatal("hpgen: set ANTIGRAVITY_HOME to a throwaway directory first")
	}

	reply := strings.Join([]string{
		"I checked the renderer and the viewport cache. The hot path walks the",
		"response rows once per frame, which is fine when the cache is warm.",
		"",
		"```go",
		"for i, r := range resp {",
		"\tcounts[len(header)+i] = screenRows(r)",
		"}",
		"```",
		"",
		"Next I will measure the per-frame cost at 200x40 with a 5000-step",
		"transcript and compare it with the reference's numbers.",
	}, "\n")

	if *replyBytes > 0 {
		unit := "the quick brown fox jumps over the lazy dog "
		buf := strings.Repeat(unit, *replyBytes/len(unit)+1)[:*replyBytes]
		reply = "```\n" + buf + "\n```"
	}

	var out []convstore.Step
	var idx int64 = 1
	for i := 0; i < *steps; i++ {
		switch i % 5 {
		case 0, 1:
			out = append(out, convstore.Step{
				Idx: idx, Type: convstore.StepUserInput, Status: 3,
				Text: fmt.Sprintf("prompt %d: please look at the renderer again", i),
			})
			idx++
		case 2, 3:
			out = append(out, convstore.Step{
				Idx: idx, Type: convstore.StepModelResponse, Status: 3,
				Text: reply,
			})
			idx++
		case 4:
			out = append(out, convstore.Step{
				Idx: idx, Type: convstore.StepToolCall, Status: 3,
				ToolName: "run_command", ToolArgs: `{"CommandLine":"ls -la"}`,
				ToolResult: "total 0",
			})
			idx++
		}
	}

	if err := convstore.SaveConversation(*id, *workspace, out); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d steps) store=%s\n", *id, len(out), convstore.StoreDir())
}
