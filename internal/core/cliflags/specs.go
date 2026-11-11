package cliflags

import (
	"io"
	"sync"
)

// FlagSpec is the static description of a single (sub)command-level flag.
type FlagSpec struct {
	Name   string // long form, e.g. "add-dir"
	Short  string // single-letter alias, e.g. "c" (empty for none)
	Type   string // "string", "bool", "duration", "stringSlice"
	Def    string // default value as a string
	Usage  string // one-line description
	Hidden bool   // keeps the flag parseable but out of the --help table
}

// SubSubSpec describes a sub-subcommand (e.g. plugin list, mcp add, etc.).
type SubSubSpec struct {
	Name     string
	Short    string
	Run      func(w io.Writer, args []string) error
	RunJ     any // func(j *JetskiCLI, args []string) error
	Help     string
	HelpFn   func(prog string) string
	HelpExit int
}

// SubcommandSpec is the static description of a single (sub)subcommand.
type SubcommandSpec struct {
	Name            string
	Aliases         []string
	Short           string
	Subs            []SubSubSpec
	Run             func(w io.Writer, args []string) error
	RunJ            any // func(j *JetskiCLI, args []string) error
	Hidden          bool
	Help            string
	HelpFn          func(prog string) string
	HelpExit        int
	UnknownSubError string
}

// Subcommand constructors / providers registry hooks
var (
	subcommandMu   sync.RWMutex
	providers      = make(map[string]func() SubcommandSpec)
	multiProviders = make(map[string]func() []SubcommandSpec)
)

func RegisterProvider(name string, fn func() SubcommandSpec) {
	subcommandMu.Lock()
	defer subcommandMu.Unlock()
	providers[name] = fn
}

func RegisterMultiProvider(name string, fn func() []SubcommandSpec) {
	subcommandMu.Lock()
	defer subcommandMu.Unlock()
	multiProviders[name] = fn
}

func GetProvider(name string) func() SubcommandSpec {
	subcommandMu.RLock()
	defer subcommandMu.RUnlock()
	return providers[name]
}

func GetMultiProvider(name string) func() []SubcommandSpec {
	subcommandMu.RLock()
	defer subcommandMu.RUnlock()
	return multiProviders[name]
}

func AccountsSubcommand() SubcommandSpec {
	if fn := GetProvider("accounts"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "accounts"}
}

func ProviderSubcommand() SubcommandSpec {
	if fn := GetProvider("provider"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "provider"}
}

func ModelSubcommand() SubcommandSpec {
	if fn := GetProvider("model"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "model"}
}

func HalloweenSubcommand() SubcommandSpec {
	if fn := GetProvider("halloween"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "halloween"}
}

func AgentSubcommands() []SubcommandSpec {
	if fn := GetMultiProvider("agent"); fn != nil {
		return fn()
	}
	return []SubcommandSpec{{Name: "agent"}, {Name: "agents"}}
}

func ChangelogSubcommand() SubcommandSpec {
	if fn := GetProvider("changelog"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "changelog"}
}

func UpdateSubcommand() SubcommandSpec {
	if fn := GetProvider("update"); fn != nil {
		return fn()
	}
	return SubcommandSpec{
		Name:     "update",
		Short:    "Update CLI",
		Help:     "Usage of update:\n",
		HelpExit: 2,
	}
}

func ModelsSubcommand() SubcommandSpec {
	if fn := GetProvider("models"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "models"}
}

func InstallSubcommand() SubcommandSpec {
	if fn := GetProvider("install"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "install"}
}

func MicServeSubcommand() SubcommandSpec {
	if fn := GetProvider("mic-serve"); fn != nil {
		return fn()
	}
	return SubcommandSpec{Name: "mic-serve"}
}

func RCSubcommands() []SubcommandSpec {
	if fn := GetMultiProvider("rc"); fn != nil {
		return fn()
	}
	return []SubcommandSpec{{Name: "remote-control"}}
}
