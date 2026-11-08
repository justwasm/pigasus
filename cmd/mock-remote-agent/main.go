// Command mock-remote-agent is the standalone compatibility binary for the
// Antigravity remote-agent proxy. The implementation lives in
// internal/rchost/agentserver and is also available as
// `pigasus agent-server`.
package main

import (
	"os"

	"github.com/justwasm/pigasus/internal/rchost/agentserver"
)

func main() {
	os.Exit(agentserver.Main(os.Args[1:]))
}
