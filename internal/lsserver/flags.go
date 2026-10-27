// Package lsserver — flags.go: the reference language server's flag
// surface, flag for flag.
//
// The Antigravity IDE spawns its `language_server` binary with a
// fixed set of flags (out/ide-language-server/ide-js/dist/
// languageServer.js) and the binary's own `--help` is the contract
// the IDE's operator sees. The reference prints Go's `flag` package
// default output, so the surface is reproducible exactly: the same
// flag names, the same defaults and the same usage strings make the
// clone's `--help` byte-identical to the reference's.
//
// Evidence grade: 一手 — the reference's `--help` was captured from
// the binary on this machine (out/langserver/help.txt).
//
// Three flags the IDE passes are NOT in the reference's `--help`:
//
//	--enable_sidecars
//	--host_bridge_url
//	--host_bridge_token
//
// The reference accepts all three and its help does not list them —
// they are hidden, not unknown. They take part in the argument scan
// like every other flag; see hiddenFlags.
package lsserver

import (
	"flag"
)

// Flags holds the reference's flag values. The names mirror the
// reference's flag names; the Go names are the camel-cased forms.
//
// Every field is read by the process wiring. The flags the clone
// cannot honour (the IDE-internal ones: the sidecar host, the Zerog
// VM, the Xbox tool-use server, the local Chrome) are parsed and
// ignored — the reference's own answers to them are "accepted, no
// effect" in every mode the clone runs.
type Flags struct {
	AnalyticsServerURL string
	APIServerURL       string
	AppDataDir         string
	BeyondQuotaBucket  string
	BeyondServiceAddr  string
	BrowserEvalEnv     bool
	CDPPort            int
	CloudCodeEndpoint  string
	ConfigDir          string
	CSRFToken          string
	DisableTelemetry   bool
	EnableLSP          bool
	// EnableSidecars is the reference's hidden flag.
	//
	// The IDE passes it, the reference accepts it and ignores it —
	// except for one thing: the sidecar block of the
	// `GetServerConfiguration` answer carries `allowAll` only when the
	// flag was *not* passed (out/langserver/answers.json).
	EnableSidecars                    bool
	ExitOnStdinClose                  bool
	ExtensionServerCSRFToken          string
	ExtensionServerPort               int
	FileWatchMaxDirCount              int
	GeminiDir                         string
	GenerativeServiceAddr             string
	GoogleCloudProject                string
	Headless                          bool
	HTTPServerPort                    int
	HTTPSServerPort                   int
	InferenceAPIServerURL             string
	IsGoogle3Workspace                bool
	IsGoogleInternal                  bool
	JetboxReleaseChannel              string
	LimitGoMaxProcs                   int
	LocalChromeHeadless               bool
	LocalChromeUserDataDir            string
	LSPPort                           int
	MaxNumTrackedWorkspaces           int
	MockFixture                       string
	ModelAPIClientType                string
	MQueryForContextModule            bool
	OverrideBusinessOAuthClientID     string
	OverrideBusinessOAuthClientSecret string
	OverrideIDEName                   string
	OverrideIDEVersion                string
	OverrideModelName                 string
	OverrideOAuthClientID             string
	OverrideOAuthClientSecret         string
	OverrideUserAgentName             string
	ParentPipePath                    string
	PersistentMode                    bool
	RunningInXbox                     bool
	SandboxMode                       bool
	Stamp                             bool
	Standalone                        bool
	StartupWarning                    string
	StubbyAuthCloudLocation           string
	StubbyAuthCloudProject            string
	StubbyAuthMode                    string
	SubclientType                     string
	TrafficStreamSelfID               string
	UseCustomPageActions              bool
	UseLocalChrome                    bool
	UseLSChromeDevtoolsMCP            bool
	UseMockedData                     bool
	UseStubbyAuth                     bool
	UseTestGaia                       bool
	WebBundlePath                     string
	WorkspaceID                       string
	XboxToolUsePort                   int
	ZerogAppHostPort                  int
	ZerogHTTPSocketPath               string
	ZerogTLSCertPath                  string
	ZerogVMSshKeyPath                 string
	ZerogVMSshSocketName              string
}

// Register declares the reference's flags on fs and returns the
// struct they write into.
//
// The reference's own `--help` is the oracle: same names, same
// defaults, same usage strings — the usage text is what the operator
// reads, and a different wording is a different help.
func Register(fs *flag.FlagSet) *Flags {
	f := &Flags{}
	registerStringFlags(fs, f)
	registerBoolFlags(fs, f)
	registerIntFlags(fs, f)
	return f
}

// hiddenFlags are the reference's flags its own `--help` does not
// print.
//
// The IDE's spawn recipe passes three of them
// (out/ide-language-server/ide-js/dist/languageServer.js), and the
// reference accepts all three: a run with them starts normally, while
// an unknown flag makes the reference print "flags provided but not
// defined" and stop. The flags are therefore defined but hidden.
//
//	--enable_sidecars     the sidecar switch
//	--host_bridge_url     the IDE's main-process bridge, when it is up
//	--host_bridge_token   the bridge's token
//
// 【一手】 the live IDE's own spawn line carries all three
// (out/langserver/spawn-args.txt).
//
// The hidden flags take part in the *same* scan as the visible ones
// (internal/lsserver/args.go): a hidden flag's value error is
// reported at its position, exactly like a visible flag's, and a
// hidden flag behind a positional argument is never seen — the scan
// stopped there. What makes them hidden is only that the usage does
// not list them.
var hiddenFlags = []hiddenFlag{
	{name: "enable_sidecars", boolFlag: true},
	{name: "host_bridge_url"},
	{name: "host_bridge_token"},
}

// hiddenFlag describes one of the reference's hidden flags.
type hiddenFlag struct {
	name string
	// boolFlag is true for the flags that take no value.
	boolFlag bool
}

// lookupHidden returns the hidden flag with that name.
func lookupHidden(name string) (hiddenFlag, bool) {
	for _, hidden := range hiddenFlags {
		if hidden.name == name {
			return hidden, true
		}
	}
	return hiddenFlag{}, false
}
