package assessor

import (
	"fmt"
	"strings"
)

// GHAction classifies the effect of a GitHub CLI (`gh`) invocation.
type GHAction string

const (
	// GHReadOnly indicates a read-only `gh` query (e.g. `gh pr view`, `gh api` GET).
	GHReadOnly GHAction = "read_only"
	// GHCheckout indicates `gh pr checkout`, which mutates the local git branch/working tree.
	GHCheckout GHAction = "checkout"
	// GHMutate indicates a mutating `gh` operation (e.g. `gh pr merge`, `gh api -X POST`).
	GHMutate GHAction = "mutate"
	// GHUnknown indicates an unrecognized `gh` subcommand or missing action.
	GHUnknown GHAction = "unknown"
)

// GHAssessment describes the parsed `gh` multicall classification.
type GHAssessment struct {
	Action   GHAction
	Resource string
	Subcmd   string
	Endpoint string
	Reason   string
}

type ghParsedArgs struct {
	positionals   []string
	method        string
	hasMethodFlag bool
	hasFieldFlag  bool
	hasInputFlag  bool
	hasHelpOrVer  bool
}

// AssessGH parses and classifies a `gh` CLI invocation from its argument list
// (excluding the leading `gh` command name).
func AssessGH(args []string) GHAssessment {
	parsed := parseGHArgs(args)

	if parsed.hasHelpOrVer && len(parsed.positionals) == 0 {
		return GHAssessment{
			Action: GHReadOnly,
			Subcmd: "help",
			Reason: "gh help/version is read-only",
		}
	}

	if len(parsed.positionals) == 0 {
		return GHAssessment{
			Action: GHUnknown,
			Reason: "gh invocation without subcommand requires review",
		}
	}

	resource := parsed.positionals[0]
	subcmd := ""
	if len(parsed.positionals) > 1 {
		subcmd = parsed.positionals[1]
	}

	switch resource {
	case "help", "version":
		return GHAssessment{
			Action:   GHReadOnly,
			Resource: resource,
			Reason:   fmt.Sprintf("gh %s is read-only", resource),
		}

	case "status":
		return GHAssessment{
			Action:   GHReadOnly,
			Resource: resource,
			Reason:   "gh status is read-only",
		}

	case "search":
		return GHAssessment{
			Action:   GHReadOnly,
			Resource: resource,
			Subcmd:   subcmd,
			Reason:   "gh search is read-only",
		}

	case "pr":
		switch subcmd {
		case "view", "list", "diff", "checks", "status":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh pr %s is read-only", subcmd),
			}
		case "checkout":
			return GHAssessment{
				Action:   GHCheckout,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   "gh pr checkout mutates local git branch and working tree",
			}
		case "create", "merge", "close", "reopen", "edit", "comment", "review", "ready", "lock", "unlock":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh pr %s mutates pull request state", subcmd),
			}
		}

	case "issue":
		switch subcmd {
		case "view", "list", "status":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh issue %s is read-only", subcmd),
			}
		case "create", "close", "reopen", "edit", "comment", "delete", "lock", "unlock", "pin", "unpin", "transfer", "develop":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh issue %s mutates issue state", subcmd),
			}
		}

	case "repo":
		switch subcmd {
		case "view", "list":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh repo %s is read-only", subcmd),
			}
		case "create", "delete", "fork", "clone", "rename", "archive", "unarchive", "edit", "sync", "deploy-key":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh repo %s mutates repository or local workspace", subcmd),
			}
		}

	case "run":
		switch subcmd {
		case "view", "list", "watch":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh run %s is read-only", subcmd),
			}
		case "cancel", "rerun", "delete", "download":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh run %s mutates workflow run or local files", subcmd),
			}
		}

	case "workflow":
		switch subcmd {
		case "list", "view":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh workflow %s is read-only", subcmd),
			}
		case "run", "enable", "disable":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh workflow %s mutates workflow state", subcmd),
			}
		}

	case "release":
		switch subcmd {
		case "list", "view":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh release %s is read-only", subcmd),
			}
		case "create", "delete", "edit", "upload", "download", "delete-asset":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh release %s mutates release or local files", subcmd),
			}
		}

	case "auth":
		switch subcmd {
		case "status":
			return GHAssessment{
				Action:   GHReadOnly,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   "gh auth status is read-only",
			}
		case "login", "logout", "refresh", "setup-git", "switch", "token":
			return GHAssessment{
				Action:   GHMutate,
				Resource: resource,
				Subcmd:   subcmd,
				Reason:   fmt.Sprintf("gh auth %s accesses or mutates credentials", subcmd),
			}
		}

	case "api":
		return assessGHAPI(parsed, subcmd)
	}

	if parsed.hasHelpOrVer {
		return GHAssessment{
			Action:   GHReadOnly,
			Resource: resource,
			Subcmd:   subcmd,
			Reason:   "gh help flag is read-only",
		}
	}

	return GHAssessment{
		Action:   GHUnknown,
		Resource: resource,
		Subcmd:   subcmd,
		Reason:   fmt.Sprintf("unrecognized gh command %q requires review", strings.TrimSpace(resource+" "+subcmd)),
	}
}

func assessGHAPI(parsed ghParsedArgs, endpoint string) GHAssessment {
	method := "GET"
	if parsed.hasMethodFlag {
		method = strings.ToUpper(strings.TrimSpace(parsed.method))
		if method == "" {
			method = "GET"
		}
	} else if parsed.hasFieldFlag || parsed.hasInputFlag {
		// `gh api` defaults to POST when -f/-F/--raw-field/--field/--input is supplied.
		method = "POST"
	}

	if method == "GET" && !parsed.hasInputFlag && (parsed.hasMethodFlag || !parsed.hasFieldFlag) {
		return GHAssessment{
			Action:   GHReadOnly,
			Resource: "api",
			Subcmd:   method,
			Endpoint: endpoint,
			Reason:   fmt.Sprintf("gh api %s %s is read-only", method, endpoint),
		}
	}

	return GHAssessment{
		Action:   GHMutate,
		Resource: "api",
		Subcmd:   method,
		Endpoint: endpoint,
		Reason:   fmt.Sprintf("gh api %s %s may mutate remote state", method, endpoint),
	}
}

func parseGHArgs(args []string) ghParsedArgs {
	var out ghParsedArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out.positionals = append(out.positionals, args[i+1:]...)
			break
		}

		if strings.HasPrefix(arg, "--") {
			name, val, hasEq := strings.Cut(arg, "=")
			switch name {
			case "--help", "--version":
				out.hasHelpOrVer = true
			case "--method":
				out.hasMethodFlag = true
				if hasEq {
					out.method = val
				} else if i+1 < len(args) {
					i++
					out.method = args[i]
				}
			case "--raw-field", "--field":
				out.hasFieldFlag = true
				if !hasEq && i+1 < len(args) {
					i++
				}
			case "--input":
				out.hasInputFlag = true
				if !hasEq && i+1 < len(args) {
					i++
				}
			default:
				if !hasEq && ghLongValueFlags[name] && i+1 < len(args) {
					i++
				}
			}
			continue
		}

		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			short := arg[:2]
			attached := arg[2:]
			switch short {
			case "-h":
				out.hasHelpOrVer = true
			case "-X":
				out.hasMethodFlag = true
				if attached != "" {
					out.method = attached
				} else if i+1 < len(args) {
					i++
					out.method = args[i]
				}
			case "-f", "-F":
				out.hasFieldFlag = true
				if attached == "" && i+1 < len(args) {
					i++
				}
			default:
				if attached == "" && ghShortValueFlags[short] && i+1 < len(args) {
					i++
				}
			}
			continue
		}

		out.positionals = append(out.positionals, arg)
	}
	return out
}

var ghLongValueFlags = map[string]bool{
	"--repo":       true,
	"--hostname":   true,
	"--jq":         true,
	"--json":       true,
	"--template":   true,
	"--header":     true,
	"--cache":      true,
	"--limit":      true,
	"--state":      true,
	"--search":     true,
	"--author":     true,
	"--label":      true,
	"--assignee":   true,
	"--reviewer":   true,
	"--milestone":  true,
	"--project":    true,
	"--body":       true,
	"--body-file":  true,
	"--title":      true,
	"--base":       true,
	"--head":       true,
	"--format":     true,
	"--output":     true,
	"--dir":        true,
	"--pattern":    true,
	"--branch":     true,
	"--workflow":   true,
	"--event":      true,
	"--status":     true,
	"--user":       true,
	"--commit":     true,
	"--tag":        true,
	"--target":     true,
	"--notes":      true,
	"--notes-file": true,
	"--language":   true,
	"--sort":       true,
	"--order":      true,
	"--visibility": true,
	"--owner":      true,
	"--match":      true,
	"--extension":  true,
	"--filename":   true,
}

var ghShortValueFlags = map[string]bool{
	"-R": true,
	"-q": true,
	"-t": true,
	"-H": true,
	"-L": true,
	"-s": true,
	"-S": true,
	"-A": true,
	"-l": true,
	"-a": true,
	"-r": true,
	"-m": true,
	"-p": true,
	"-b": true,
	"-B": true,
	"-D": true,
	"-w": false, // -w is --web (boolean)
}
