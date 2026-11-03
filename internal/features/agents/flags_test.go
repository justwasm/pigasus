// flags_test.go — the `--agent` flag's own reading.
//
// The flag's face of the catalogue: the empty value and "default" name
// the built-in agent, a name that matches no discovered agent names no
// custom agent, and a discovered one carries its body.

package agents

import (
	"flag"
	"path/filepath"
	"testing"
)

func TestCustomAgentFromFlags(t *testing.T) {
	workspace, home := t.TempDir(), t.TempDir()
	writeAgent(t, filepath.Join(workspace, ".agents", "agents"), "foo.md", "foo", "F", "body")
	t.Chdir(workspace)
	t.Setenv("HOME", home)

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("agent", "", "")

	// The empty value is the built-in agent: no custom agent.
	if a := CustomAgentFromFlags(fs); a != nil {
		t.Fatalf("empty flag: %+v, want nil", a)
	}
	if err := fs.Set("agent", "default"); err != nil {
		t.Fatal(err)
	}
	if a := CustomAgentFromFlags(fs); a != nil {
		t.Fatalf("default: %+v, want nil", a)
	}

	// A discovered name carries the agent and its body.
	if err := fs.Set("agent", "foo"); err != nil {
		t.Fatal(err)
	}
	a := CustomAgentFromFlags(fs)
	if a == nil || a.Name != "foo" || a.Description != "F" || a.Body != "body" {
		t.Fatalf("foo: %+v, want the discovered agent", a)
	}
	if got := AgentBody(a); got != "body" {
		t.Errorf("AgentBody = %q, want %q", got, "body")
	}

	// A name that matches nothing is no custom agent.
	if err := fs.Set("agent", "nosuch"); err != nil {
		t.Fatal(err)
	}
	if a := CustomAgentFromFlags(fs); a != nil {
		t.Fatalf("nosuch: %+v, want nil", a)
	}
	if got := AgentBody(nil); got != "" {
		t.Errorf("AgentBody(nil) = %q, want the empty string", got)
	}
}
