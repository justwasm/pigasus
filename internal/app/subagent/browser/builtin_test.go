package browser

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/builtinsubagent"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// TestInstallBuiltinRegistersTheBrowserType pins the wiring the model
// depends on: the browser agent is registered under the reference's own
// name, so `invoke_subagent` can route to it.
func TestInstallBuiltinRegistersTheBrowserType(t *testing.T) {
	reg := builtinsubagent.Default()
	if err := InstallBuiltin(reg, subagenttools.Deps{}, tools.Catalog(t.TempDir())); err != nil {
		t.Fatalf("InstallBuiltin: %v", err)
	}
	spec, ok := reg.Spec(string(builtinsubagent.KindBrowser))
	if !ok {
		t.Fatalf("the registry does not carry %q; it carries %v", builtinsubagent.KindBrowser, reg.Names())
	}
	if spec.Description == "" {
		t.Error("the browser agent has no description; the model cannot tell what it is for")
	}
	if spec.Handler == nil {
		t.Error("the browser agent has no handler")
	}
}

// TestInstallBuiltinRejectsAnEmptyRegistry pins the two guards: a
// session that hands over nothing gets an error, not a panic.
func TestInstallBuiltinRejectsAnEmptyRegistry(t *testing.T) {
	if err := InstallBuiltin(nil, subagenttools.Deps{}, tools.Catalog(t.TempDir())); err == nil {
		t.Error("InstallBuiltin(nil registry) must fail")
	}
	if err := InstallBuiltin(builtinsubagent.Default(), subagenttools.Deps{}, nil); err == nil {
		t.Error("InstallBuiltin(nil catalogue) must fail")
	}
}

// TestSubagentAndRegistryAgree pins the invariant the model relies on:
// the prompt's "Available subagents" line and the registry's name are
// the same string. If they drift, the model names a type the registry
// cannot route to.
func TestSubagentAndRegistryAgree(t *testing.T) {
	reg := builtinsubagent.Default()
	if err := InstallBuiltin(reg, subagenttools.Deps{}, tools.Catalog(t.TempDir())); err != nil {
		t.Fatalf("InstallBuiltin: %v", err)
	}
	entry := Subagent()
	if entry.Name != string(builtinsubagent.KindBrowser) {
		t.Errorf("the prompt entry is %q, the registry name is %q", entry.Name, builtinsubagent.KindBrowser)
	}
	if _, ok := reg.Spec(entry.Name); !ok {
		t.Errorf("the prompt names %q but the registry does not carry it", entry.Name)
	}
	if entry.Description == "" {
		t.Error("the prompt entry has no description")
	}
}

// TestCatalogueNamesTheBrowserAgent pins that the model can learn the
// name from the tool it calls: the built-in catalogue is spliced into
// invoke_subagent's description.
func TestCatalogueNamesTheBrowserAgent(t *testing.T) {
	reg := builtinsubagent.Default()
	if err := InstallBuiltin(reg, subagenttools.Deps{}, tools.Catalog(t.TempDir())); err != nil {
		t.Fatalf("InstallBuiltin: %v", err)
	}
	catalog := reg.Catalog()
	if !strings.Contains(catalog, string(builtinsubagent.KindBrowser)) {
		t.Errorf("the catalogue does not name the browser agent:\n%s", catalog)
	}
	if !strings.Contains(catalog, string(builtinsubagent.KindImageGenerator)) {
		t.Errorf("the catalogue lost the image generator:\n%s", catalog)
	}
}

// TestTheModelCanSeeTheName pins the two places the model reads the
// type's name from.
//
// The invoke_subagent description carries the built-in catalogue — that
// is where a model that only has the tool's schema looks. The system
// prompt's "Available subagents" list is the other one — that is where
// a model that reads its instructions looks. Both must carry the same
// name, or the model calls a type the registry cannot route to.
func TestTheModelCanSeeTheName(t *testing.T) {
	reg := builtinsubagent.Default()
	if err := InstallBuiltin(reg, subagenttools.Deps{}, tools.Catalog(t.TempDir())); err != nil {
		t.Fatalf("InstallBuiltin: %v", err)
	}
	subagenttools.SetBuiltinSubagentRegistry(reg)
	t.Cleanup(func() { subagenttools.SetBuiltinSubagentRegistry(builtinsubagent.Default()) })

	if catalog := subagenttools.BuiltinSubagentCatalog(); !strings.Contains(catalog, string(builtinsubagent.KindBrowser)) {
		t.Errorf("the invoke_subagent catalogue does not name the browser agent:\n%s", catalog)
	}

	opts := agyprompt.DefaultOptions(t.TempDir(), t.TempDir(), "conv-1")
	opts.Subagents = append(opts.Subagents, Subagent())
	prompt := agyprompt.Build(opts)
	if !strings.Contains(prompt, "- "+string(builtinsubagent.KindBrowser)+":") {
		t.Errorf("the prompt's subagent list does not carry the browser agent")
	}
}

// TestInstallBuiltinHandlerRunsTheBrowserAgent is the integration
// proof: the installed type's handler really runs a browser agent and
// answers with what that agent said.
//
// The model is PiG's faux provider — the deterministic stand-in the
// clone's own subagent tests use — so the run needs no network and no
// browser: the agent is asked a question and answers it. The faux
// provider answers "42" to exactly one prompt, and that prompt is the
// task the browser agent is given.
//
// What this pins is the *path*, not the browser: the call reaches the
// built-in registry, the registry's handler builds a browser agent,
// and the agent's final text comes back in the result.
func TestInstallBuiltinHandlerRunsTheBrowserAgent(t *testing.T) {
	t.Setenv("PIG_TEST_FAUX", "1")

	reg := builtinsubagent.Default()
	if err := InstallBuiltin(reg, fauxDeps(t), tools.Catalog(t.TempDir())); err != nil {
		t.Fatalf("InstallBuiltin: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	res, err := reg.Invoke(ctx, string(builtinsubagent.KindBrowser), "What is 20+22?")
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(res.Text, "42") {
		t.Fatalf("the browser agent answered %q, want the agent's answer", res.Text)
	}
}

// fauxDeps builds the session handles the browser agent needs: a faux
// model and a session services container.
func fauxDeps(t *testing.T) subagenttools.Deps {
	t.Helper()
	dir := t.TempDir()
	svcs, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      dir,
		AgentDir: filepath.Join(dir, "agent"),
	})
	if err != nil {
		t.Fatalf("coding.CreateAgentSessionServices: %v", err)
	}
	t.Cleanup(func() { svcs.Close() })
	return subagenttools.Deps{
		Services: svcs,
		Model: &pi.Model{
			Type:         pi.ModelTypeChat,
			ID:           "faux-1",
			DisplayName:  "faux-1",
			Provider:     &pi.TestFauxProvider{},
			ProviderMeta: pi.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
			Input:        []string{"text"},
		},
		CWD:      dir,
		AgentDir: filepath.Join(dir, "agent"),
	}
}
