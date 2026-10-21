// agents_bridge.go: cmd/wagyu -> internal/cli callback for the
// `agent` and `agents` subcommands.
package cli

var agentsLister func() []string

// RegisterAgentsLister installs the custom-agent discovery provider.
func RegisterAgentsLister(fn func() []string) {
	agentsLister = fn
}
