// provider_cmds.go — `agy provider …` and `agy model …`.
//
// # Why a separate command
//
// `agy models` prints the catalogue the reference prints, and its output
// is byte-pinned: the ids and labels, tab separated when the output is a
// pipe and space aligned when it is a terminal. The commands that *change*
// the configuration therefore live under their own names, so the pinned
// output is never at risk.
//
// Both commands are hidden: `agy --help` lists the reference's twelve
// visible subcommands and nothing else (see internal/cli/parser_test.go).
//
// # What they write
//
// Every write goes through models.Editor, which rewrites the field it
// changes and leaves the rest of the file alone (see internal/models/edit.go).
// The wrapper's `agy wrapper providers …` writes the same file through the
// same layer.
//
// # Merge, not replace
//
// `provider add` and `model add` merge: a flag the user did not pass keeps
// the value the file already had. `agy provider add gateway --base-url X`
// changes the endpoint and leaves the credential, the header and the
// models alone. Clearing a field is an explicit act (`--api-key ""`).
package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// ---- usage ---------------------------------------------------------------

func usageProvider(n string) string {
	return "Usage: " + n + " provider <subcommand> [flags]\n" +
		"\n" +
		"Manage the configured model providers\n" +
		"\n" +
		"Subcommands:\n" +
		"  list     List the configured providers and their models\n" +
		"  show     Show one provider in detail\n" +
		"  add      Create or update a provider\n" +
		"  remove   Remove a provider and its models\n" +
		"\n" +
		"The configuration file is " + models.ProvidersPath() + "\n"
}

func usageProviderShow(n string) string {
	return "Usage: " + n + " provider show <id>\n" +
		"\n" +
		"Show one provider in detail\n"
}

func usageProviderAdd(n string) string {
	return "Usage: " + n + " provider add <id> [flags]\n" +
		"\n" +
		"Create or update a provider\n" +
		"\n" +
		"Flags:\n" +
		"  --name              Display name of the provider\n" +
		"  --type              Wire protocol: openai or anthropic\n" +
		"  --base-url          API base; the endpoint is derived from it\n" +
		"  --api-url           Full endpoint, used verbatim\n" +
		"  --api-key           Credential; $NAME reads it from the environment\n" +
		"  --timeout-seconds   Per-request timeout\n" +
		"  --max-retries       Retry count for 5xx and network failures\n" +
		"  --header            Extra request header, as name=value (repeatable)\n" +
		"  --model             Model to serve, as id or id=upstream-id (repeatable)\n" +
		"\n" +
		"Notes:\n" +
		"  A flag that is not passed keeps the value the file already had.\n" +
		"  The endpoint is derived from --base-url by protocol: openai appends\n" +
		"  /chat/completions, anthropic appends /v1/messages. --api-url overrides\n" +
		"  both and is used verbatim.\n"
}

func usageProviderRemove(n string) string {
	return "Usage: " + n + " provider remove <id>\n" +
		"\n" +
		"Remove a provider and the models it serves\n"
}

func usageProviderTest(n string) string {
	return "Usage: " + n + " provider test <id> [--model <id>]\n" +
		"\n" +
		"Probe the provider's endpoint and credential\n" +
		"\n" +
		"Flags:\n" +
		"  --model   Model to probe; the provider's first model by default\n" +
		"\n" +
		"Notes:\n" +
		"  The probe is a minimal chat completion when a model is known, and a\n" +
		"  model listing otherwise. It makes one request and reports what the\n" +
		"  endpoint answered. The exit code is 1 when the probe fails.\n"
}

func usageModel(n string) string {
	return "Usage: " + n + " model <subcommand> [flags]\n" +
		"\n" +
		"Manage the configured models\n" +
		"\n" +
		"Subcommands:\n" +
		"  list     List the configured models\n" +
		"  show     Show one model in detail\n" +
		"  add      Create or update a model\n" +
		"  remove   Remove a model\n" +
		"\n" +
		"The configuration file is " + models.ProvidersPath() + "\n"
}

func usageModelShow(n string) string {
	return "Usage: " + n + " model show <id>\n" +
		"\n" +
		"Show one model in detail\n"
}

func usageModelAdd(n string) string {
	return "Usage: " + n + " model add <id> --provider <provider> [flags]\n" +
		"\n" +
		"Create or update a model\n" +
		"\n" +
		"Flags:\n" +
		"  --provider             Provider that serves this model (required)\n" +
		"  --model                Model id sent upstream\n" +
		"  --name                 Display name\n" +
		"  --context-window       Declared context window in tokens\n" +
		"  --default-max-tokens   Declared output cap in tokens\n" +
		"  --reasoning-effort     low, medium, high or default\n" +
		"\n" +
		"Notes:\n" +
		"  A flag that is not passed keeps the value the file already had.\n"
}

func usageModelRemove(n string) string {
	return "Usage: " + n + " model remove <id> [--provider <provider>]\n" +
		"\n" +
		"Remove a model\n"
}

// ---- parent dispatchers --------------------------------------------------

// runProviderParent handles `agy provider` with no subcommand.
func runProviderParent(j *JetskiCLI, args []string) error {
	if len(args) > 0 {
		return errExitCodeMsg(1, fmt.Sprintf("Error: unknown command: %s", args[0]))
	}
	fmt.Fprint(j.ErrOut, usageProvider(j.Name))
	return errExitCode(1)
}

// runModelParent handles `agy model` with no subcommand.
func runModelParent(j *JetskiCLI, args []string) error {
	if len(args) > 0 {
		return errExitCodeMsg(1, fmt.Sprintf("Error: unknown command: %s", args[0]))
	}
	fmt.Fprint(j.ErrOut, usageModel(j.Name))
	return errExitCode(1)
}

// ---- shared helpers ------------------------------------------------------

// wireModelID returns the model id sent upstream: the explicit one when
// there is one, the catalogue id otherwise.
func wireModelID(m models.ProviderModel) string {
	if strings.TrimSpace(m.Model) != "" {
		return m.Model
	}
	return m.ID
}

// splitPositionalID pulls the identifier out of args and returns the rest
// for the flag parser.
//
// The identifier may appear before or after the flags, so the scan has to
// know which flags take a value: without that, `provider add --model a=b`
// would read "a=b" as the identifier.
func splitPositionalID(args []string, takesValue map[string]bool) (string, []string) {
	id := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			rest = append(rest, a)
			name := strings.TrimLeft(a, "-")
			if strings.ContainsRune(name, '=') {
				continue
			}
			if takesValue[name] && i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
			continue
		}
		if id == "" {
			id = a
			continue
		}
		rest = append(rest, a)
	}
	return id, rest
}

// flagSetFlags returns the names of the flags the user actually passed.
func flagSetFlags(fs *flag.FlagSet) map[string]bool {
	out := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { out[f.Name] = true })
	return out
}
