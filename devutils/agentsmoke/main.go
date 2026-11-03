// Command agentsmoke is the end-to-end check for the client/server split.
//
// It attaches to a running `pigasus server`, greets it, asks for one turn
// and prints the frames that come back. It is the smoke step for the
// client/server path: the wire is exercised over a real websocket, against
// a real server, and the frames are shown rather than asserted.
//
// It is also the shortest complete description of the protocol: a third
// party implementing the client side reads agentlink/protocol.go, and this
// is what a session of it looks like.
//
// Usage:
//
//	go run ./devutils/agentsmoke                                  # ws://127.0.0.1:7777
//	go run ./devutils/agentsmoke -server ws://host:port -token T
//	go run ./devutils/agentsmoke -prompt "say hello" -model claude-4.5-sonnet
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/agentlink"
)

// dialTimeout bounds the handshake.
const dialTimeout = 20 * time.Second

func main() {
	server := flag.String("server", "ws://127.0.0.1:7777", "address of the pigasus server")
	token := flag.String("token", os.Getenv("OPENAGY_AGENT_TOKEN"), "bearer token the server demands")
	prompt := flag.String("prompt", "Say hello in one sentence.", "prompt to send")
	model := flag.String("model", "", "model the turn runs under (empty: the server's own)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	client, err := agentlink.Dial(ctx, *server, agentlink.DialOptions{Token: *token})
	if err != nil {
		fatal("dial", err)
	}
	defer client.Close()

	// The handshake names the server's workspace, its model and its
	// version: the client shows what it attached to.
	if err := client.Send(ctx, agentlink.Frame{Kind: agentlink.KindHello}); err != nil {
		fatal("greet", err)
	}
	f, err := client.Next(ctx)
	if err != nil {
		fatal("greeting", err)
	}
	if f.Kind != agentlink.KindHello {
		fatal("greeting", fmt.Errorf("the server answered the greeting with %q", f.Kind))
	}
	fmt.Printf("server %s\n  workspace: %s\n  model:     %s\n  session:   %s\n",
		f.Version, f.Workspace, f.Model, f.Session)

	// The transcript travels with the prompt: the server replays it and
	// keeps no conversation of its own.
	promptModel := *model
	if promptModel == "" {
		promptModel = f.Model
	}
	if err := client.Send(ctx, agentlink.Frame{
		Kind:    agentlink.KindPrompt,
		Model:   promptModel,
		History: []agentlink.Message{{Role: "user", Content: *prompt}},
	}); err != nil {
		fatal("prompt", err)
	}

	for {
		f, err := client.Next(ctx)
		if err != nil {
			fatal("frame", err)
		}
		switch f.Kind {
		case agentlink.KindText:
			fmt.Print(f.Text)
		case agentlink.KindThought:
			fmt.Printf("[thought] %s\n", f.Text)
		case agentlink.KindTool:
			if f.Tool != nil {
				fmt.Printf("[tool %s] %s\n", f.Tool.Phase, f.Tool.Name)
			}
		case agentlink.KindCompact:
			if f.Compact != nil {
				fmt.Printf("[compact %s] %s\n", f.Compact.Phase, f.Compact.Reason)
			}
		case agentlink.KindNotice:
			if f.Notice != nil {
				fmt.Printf("[notice %s] %s\n", f.Notice.Kind, f.Notice.Text)
			}
		case agentlink.KindResult:
			if f.Usage != nil {
				fmt.Printf("\n[usage] %d in / %d out\n", f.Usage.PromptTokens, f.Usage.CompletionTokens)
			}
			if f.Error != "" {
				fmt.Fprintf(os.Stderr, "\n[error] %s\n", f.Error)
				os.Exit(1)
			}
			fmt.Println()
			return
		}
	}
}

// fatal reports a failure and leaves.
func fatal(what string, err error) {
	fmt.Fprintf(os.Stderr, "agentsmoke: %s: %v\n", what, strings.TrimSpace(err.Error()))
	os.Exit(1)
}
