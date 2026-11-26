// Package assessor provides AST-based shell command safety evaluation and
// fine-grained multicall authorization (such as for the GitHub CLI `gh`).
//
// It lives in Tier 2 (internal/features) and uses mvdan.cc/sh/v3/syntax to
// parse shell command lines without executing them, resolving the deferred
// shell-parsing requirement noted in internal/core/policy/permissions/matching.go.
package assessor

import (
	"bytes"
	"fmt"
	"path"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Verdict classifies the safety level of a parsed shell command line.
type Verdict string

const (
	// SafeReadOnly indicates every command and construct in the command line
	// is classified as read-only and safe for auto-approval.
	SafeReadOnly Verdict = "safe_read_only"

	// NeedsReview indicates the command line contains mutating, unknown, or
	// complex shell constructs (e.g. non-/dev/null redirections, substitutions,
	// unrecognized commands) that require user review.
	NeedsReview Verdict = "needs_review"

	// Dangerous indicates the command line contains destructive operations,
	// privilege escalation, or remote-code-execution pipelines.
	Dangerous Verdict = "dangerous"

	// NeedsExactMatch indicates the command line could not be parsed cleanly
	// by the shell parser (ParseError != nil), so callers must fall back to
	// exact-match-only permission rules.
	NeedsExactMatch Verdict = "needs_exact_match"
)

// severity returns the ordering of verdicts so higher-risk findings win.
func (v Verdict) severity() int {
	switch v {
	case SafeReadOnly:
		return 0
	case NeedsReview:
		return 1
	case Dangerous:
		return 2
	case NeedsExactMatch:
		return 3
	default:
		return 1
	}
}

// CommandCall describes a single command invocation extracted from the AST.
type CommandCall struct {
	// Name is the resolved command name after unwrapping wrapper commands
	// (env, command, nohup, time, nice).
	Name string
	// RawName is the original leading command word before unwrapping.
	RawName string
	// Args are the arguments passed to the resolved command.
	Args []string
	// Wrappers lists any wrapper commands unwrapped to reach Name.
	Wrappers []string
	// Dynamic is true when the command name contains shell expansions or
	// substitutions rather than a static literal word.
	Dynamic bool
}

// Assessment is the result of evaluating a shell command line.
type Assessment struct {
	Verdict         Verdict
	Reason          string
	ParseError      error
	Commands        []CommandCall
	HasRedirection  bool
	HasSubstitution bool
	GHAssessments   []GHAssessment
}

// Assess parses commandLine with a Bash/POSIX AST parser and evaluates its
// safety, invoked commands, redirections, substitutions, and `gh` multicalls.
func Assess(commandLine string) Assessment {
	if strings.TrimSpace(commandLine) == "" {
		return Assessment{
			Verdict: NeedsReview,
			Reason:  "empty command",
		}
	}

	parser := syntax.NewParser(syntax.Variant(syntax.LangBash))
	file, err := parser.Parse(strings.NewReader(commandLine), "")
	if err != nil {
		return Assessment{
			Verdict:    NeedsExactMatch,
			ParseError: err,
			Reason:     fmt.Sprintf("unparseable shell syntax: %v", err),
		}
	}

	a := Assessment{
		Verdict: SafeReadOnly,
		Reason:  "all commands are read-only",
	}
	var reasons []string
	escalate := func(v Verdict, reason string) {
		if reason != "" {
			reasons = append(reasons, reason)
		}
		if v.severity() > a.Verdict.severity() {
			a.Verdict = v
		}
	}

	syntax.Walk(file, func(node syntax.Node) bool {
		if node == nil {
			return false
		}
		switch n := node.(type) {
		case *syntax.Stmt:
			if n.Background {
				escalate(NeedsReview, "background execution (&) requires review")
			}
			if n.Coprocess || n.Disown {
				escalate(NeedsReview, "coprocess/disown execution requires review")
			}

		case *syntax.Redirect:
			a.HasRedirection = true
			if isUnsafeOutputRedirect(n) {
				target := wordDisplay(n.Word)
				escalate(NeedsReview, fmt.Sprintf("output redirection (%s %s) requires review", n.Op.String(), target))
			}

		case *syntax.CmdSubst:
			a.HasSubstitution = true
			escalate(NeedsReview, "command substitution requires review")

		case *syntax.ProcSubst:
			a.HasSubstitution = true
			escalate(NeedsReview, "process substitution requires review")

		case *syntax.ArithmExp:
			escalate(NeedsReview, "arithmetic expansion requires review")

		case *syntax.ArithmCmd:
			escalate(NeedsReview, "arithmetic command requires review")

		case *syntax.LetClause:
			escalate(NeedsReview, "let arithmetic clause requires review")

		case *syntax.FuncDecl:
			escalate(NeedsReview, fmt.Sprintf("function declaration %q requires review", n.Name.Value))

		case *syntax.CoprocClause:
			escalate(NeedsReview, "coproc clause requires review")

		case *syntax.DeclClause:
			escalate(NeedsReview, fmt.Sprintf("shell declaration %q requires review", n.Variant.Value))

		case *syntax.BinaryCmd:
			if n.Op == syntax.Pipe || n.Op == syntax.PipeAll {
				if reason, dangerous := checkDangerousPipeline(n); dangerous {
					escalate(Dangerous, reason)
				}
			}

		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				if len(n.Assigns) > 0 {
					escalate(NeedsReview, "shell variable assignment requires review")
				}
				return true
			}
			for _, assign := range n.Assigns {
				if assign.Name != nil && isDangerousEnvVar(assign.Name.Value) {
					escalate(Dangerous, fmt.Sprintf("dangerous environment override %s", assign.Name.Value))
				}
			}
			call, envOverrides := extractCommandCall(n)
			for _, k := range envOverrides {
				if isDangerousEnvVar(k) {
					escalate(Dangerous, fmt.Sprintf("dangerous environment override %s", k))
				}
			}
			a.Commands = append(a.Commands, call)

			v, reason, ghRes := classifyCommandCall(call)
			if ghRes != nil {
				a.GHAssessments = append(a.GHAssessments, *ghRes)
			}
			if v != SafeReadOnly {
				escalate(v, reason)
			}
		}
		return true
	})

	if len(a.Commands) == 0 && a.Verdict == SafeReadOnly {
		escalate(NeedsReview, "no executable command found")
	}

	if len(reasons) > 0 {
		a.Reason = strings.Join(dedupeStrings(reasons), "; ")
	}
	return a
}

// EvaluateForAutoMode combines AST assessment and `gh` multicall classification
// to decide whether a command can be auto-allowed in Auto mode.
//
// In Auto mode:
//   - Safe and routine development commands (build, test, formatting, git commits/branches,
//     file modifications and redirects inside the workspace) run without asking (allow=true, ask=false).
//   - Dangerous commands (destructive filesystem operations like `rm -rf /`, `mkfs`, or privilege
//     escalation like `sudo`) are denied outright (allow=false, ask=false).
//   - Remote mutating operations (e.g. `gh pr merge`, `gh pr checkout`) and unparseable shell syntax
//     prompt for approval (allow=false, ask=true).
func EvaluateForAutoMode(commandLine string) (allow bool, ask bool, reason string) {
	a := Assess(commandLine)
	if a.Verdict == Dangerous {
		return false, false, a.Reason
	}
	if a.ParseError != nil || a.Verdict == NeedsExactMatch {
		return false, true, a.Reason
	}
	// Check for non-read-only `gh` multicalls that mutate remote repositories
	for _, gh := range a.GHAssessments {
		if gh.Action != GHReadOnly {
			return false, true, gh.Reason
		}
	}
	// Routine development commands proceed without prompting in Auto mode.
	return true, false, ""
}

func isUnsafeOutputRedirect(r *syntax.Redirect) bool {
	if r == nil {
		return false
	}
	switch r.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob,
		syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob,
		syntax.RdrInOut:
		target, static := wordLiteral(r.Word)
		return !(static && target == "/dev/null")
	case syntax.DplOut:
		// `2>&1` or `>&-` duplicates/closes an fd; `>& file` redirects to a file.
		target, static := wordLiteral(r.Word)
		if static && (target == "-" || isAllDigits(target)) {
			return false
		}
		return !(static && target == "/dev/null")
	default:
		return false
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func checkDangerousPipeline(b *syntax.BinaryCmd) (string, bool) {
	stages := flattenPipeline(b)
	if len(stages) < 2 {
		return "", false
	}
	sawFetcher := false
	fetcherName := ""
	for _, st := range stages {
		callExpr, ok := st.Cmd.(*syntax.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			continue
		}
		call, _ := extractCommandCall(callExpr)
		base := commandBase(call.Name)
		if isNetworkFetcher(base) {
			sawFetcher = true
			fetcherName = base
			continue
		}
		if sawFetcher && isShellOrInterpreter(base) {
			return fmt.Sprintf("piping %s output into %s is dangerous", fetcherName, base), true
		}
	}
	return "", false
}

func flattenPipeline(b *syntax.BinaryCmd) []*syntax.Stmt {
	var stages []*syntax.Stmt
	var collect func(stmt *syntax.Stmt)
	collect = func(stmt *syntax.Stmt) {
		if stmt == nil {
			return
		}
		if inner, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && (inner.Op == syntax.Pipe || inner.Op == syntax.PipeAll) {
			collect(inner.X)
			collect(inner.Y)
			return
		}
		stages = append(stages, stmt)
	}
	collect(b.X)
	collect(b.Y)
	return stages
}

func isNetworkFetcher(name string) bool {
	switch name {
	case "curl", "wget", "fetch", "http", "xh":
		return true
	default:
		return false
	}
}

func isShellOrInterpreter(name string) bool {
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh", "ash", "csh", "tcsh", "fish",
		"python", "python3", "perl", "ruby", "node", "osascript", "pwsh", "powershell":
		return true
	default:
		return false
	}
}

func isDangerousEnvVar(name string) bool {
	switch name {
	case "LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT",
		"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH",
		"BASH_ENV", "ENV", "ZDOTDIR", "IFS",
		"GIT_SSH", "GIT_SSH_COMMAND", "GIT_EXEC_PATH", "GIT_EXTERNAL_DIFF",
		"GIT_ASKPASS", "SSH_ASKPASS":
		return true
	default:
		return false
	}
}

func extractCommandCall(c *syntax.CallExpr) (CommandCall, []string) {
	words := make([]string, len(c.Args))
	statics := make([]bool, len(c.Args))
	for i, w := range c.Args {
		lit, ok := wordLiteral(w)
		if ok {
			words[i] = lit
		} else {
			words[i] = wordDisplay(w)
		}
		statics[i] = ok
	}

	rawName := words[0]
	if !statics[0] {
		return CommandCall{
			Name:    rawName,
			RawName: rawName,
			Args:    words[1:],
			Dynamic: true,
		}, nil
	}

	var wrappers []string
	var envOverrides []string
	idx := 0

	for idx < len(words) {
		if !statics[idx] {
			return CommandCall{
				Name:     words[idx],
				RawName:  rawName,
				Args:     words[idx+1:],
				Wrappers: wrappers,
				Dynamic:  true,
			}, envOverrides
		}
		cmd := commandBase(words[idx])
		switch cmd {
		case "env":
			nextIdx, envs, ok := unwrapEnv(words, idx+1)
			envOverrides = append(envOverrides, envs...)
			if !ok || nextIdx >= len(words) {
				return CommandCall{
					Name:     words[idx],
					RawName:  rawName,
					Args:     words[idx+1:],
					Wrappers: wrappers,
				}, envOverrides
			}
			wrappers = append(wrappers, words[idx])
			idx = nextIdx
		case "command":
			nextIdx, lookupOnly := unwrapCommandBuiltin(words, idx+1)
			if lookupOnly || nextIdx >= len(words) {
				return CommandCall{
					Name:     words[idx],
					RawName:  rawName,
					Args:     words[idx+1:],
					Wrappers: wrappers,
				}, envOverrides
			}
			wrappers = append(wrappers, words[idx])
			idx = nextIdx
		case "nohup":
			if idx+1 >= len(words) {
				return CommandCall{
					Name:     words[idx],
					RawName:  rawName,
					Args:     words[idx+1:],
					Wrappers: wrappers,
				}, envOverrides
			}
			wrappers = append(wrappers, words[idx])
			idx++
		case "time":
			nextIdx := idx + 1
			for nextIdx < len(words) && (words[nextIdx] == "-p" || words[nextIdx] == "--") {
				nextIdx++
			}
			if nextIdx >= len(words) {
				return CommandCall{
					Name:     words[idx],
					RawName:  rawName,
					Args:     words[idx+1:],
					Wrappers: wrappers,
				}, envOverrides
			}
			wrappers = append(wrappers, words[idx])
			idx = nextIdx
		case "nice":
			nextIdx := unwrapNice(words, idx+1)
			if nextIdx >= len(words) {
				return CommandCall{
					Name:     words[idx],
					RawName:  rawName,
					Args:     words[idx+1:],
					Wrappers: wrappers,
				}, envOverrides
			}
			wrappers = append(wrappers, words[idx])
			idx = nextIdx
		default:
			return CommandCall{
				Name:     words[idx],
				RawName:  rawName,
				Args:     words[idx+1:],
				Wrappers: wrappers,
			}, envOverrides
		}
	}

	return CommandCall{
		Name:     rawName,
		RawName:  rawName,
		Args:     words[1:],
		Wrappers: wrappers,
	}, envOverrides
}

func unwrapEnv(words []string, start int) (nextIdx int, envNames []string, ok bool) {
	i := start
	for i < len(words) {
		w := words[i]
		if w == "--" {
			i++
			break
		}
		if w == "-S" || w == "--split-string" || strings.HasPrefix(w, "--split-string=") {
			// `env -S` parses a command string itself; do not unwrap blindly.
			return start, envNames, false
		}
		if w == "-u" || w == "--unset" || w == "-C" || w == "--chdir" {
			i += 2
			continue
		}
		if strings.HasPrefix(w, "-") {
			i++
			continue
		}
		if eq := strings.IndexByte(w, '='); eq > 0 {
			envNames = append(envNames, w[:eq])
			i++
			continue
		}
		break
	}
	return i, envNames, i < len(words)
}

func unwrapCommandBuiltin(words []string, start int) (nextIdx int, lookupOnly bool) {
	i := start
	for i < len(words) {
		w := words[i]
		if w == "--" {
			i++
			break
		}
		if !strings.HasPrefix(w, "-") || w == "-" {
			break
		}
		for _, ch := range w[1:] {
			if ch == 'v' || ch == 'V' {
				lookupOnly = true
			}
		}
		i++
	}
	return i, lookupOnly
}

func unwrapNice(words []string, start int) int {
	i := start
	for i < len(words) {
		w := words[i]
		if w == "--" {
			i++
			break
		}
		if w == "-n" || w == "--adjustment" {
			i += 2
			continue
		}
		if strings.HasPrefix(w, "-") {
			i++
			continue
		}
		break
	}
	return i
}

func classifyCommandCall(call CommandCall) (Verdict, string, *GHAssessment) {
	if call.Dynamic {
		return NeedsReview, fmt.Sprintf("dynamic command name %q requires review", call.Name), nil
	}

	base := commandBase(call.Name)

	// Privilege escalation commands are always flagged as Dangerous.
	switch base {
	case "sudo", "su", "doas":
		return Dangerous, fmt.Sprintf("privilege escalation command %q is dangerous", base), nil
	}

	// Check destructive commands and flags regardless of path prefix.
	if reason, dangerous := checkDangerousCommand(base, call.Args); dangerous {
		return Dangerous, reason, nil
	}

	// Require bare command names (or standard system bin paths) for safe allowlist.
	if strings.Contains(call.Name, "/") && !isStandardSystemBin(call.Name) {
		return NeedsReview, fmt.Sprintf("path-qualified command %q requires review", call.Name), nil
	}

	// `command -v` / `command -V` is a read-only lookup.
	if base == "command" {
		if _, lookupOnly := unwrapCommandBuiltin(call.Args, 0); lookupOnly {
			return SafeReadOnly, "", nil
		}
		return NeedsReview, "command builtin requires review", nil
	}

	// `gh` multicall fine-grained authorizer.
	if base == "gh" {
		ghRes := AssessGH(call.Args)
		switch ghRes.Action {
		case GHReadOnly:
			return SafeReadOnly, "", &ghRes
		default:
			return NeedsReview, ghRes.Reason, &ghRes
		}
	}

	// `git` subcommand classification.
	if base == "git" {
		v, reason := classifyGit(call.Args)
		return v, reason, nil
	}

	// `find` classification.
	if base == "find" {
		v, reason := classifyFind(call.Args)
		return v, reason, nil
	}

	// `rg` with `--pre` executes an external preprocessor command.
	if base == "rg" {
		for _, arg := range call.Args {
			if arg == "--pre" || strings.HasPrefix(arg, "--pre=") {
				return NeedsReview, "rg --pre executes an external command", nil
			}
		}
	}

	if safeStandaloneCommands[base] {
		return SafeReadOnly, "", nil
	}

	return NeedsReview, fmt.Sprintf("command %q is not in the read-only allowlist", call.Name), nil
}

func commandBase(name string) string {
	if name == "" {
		return ""
	}
	return path.Base(name)
}

func isStandardSystemBin(name string) bool {
	dir := path.Dir(name)
	switch dir {
	case "/bin", "/usr/bin", "/usr/local/bin", "/opt/homebrew/bin":
		return true
	default:
		return false
	}
}

func checkDangerousCommand(base string, args []string) (string, bool) {
	switch {
	case base == "rm":
		if hasRMRecursiveForce(args) {
			return "destructive command: rm -rf", true
		}
	case base == "mkfs" || strings.HasPrefix(base, "mkfs."):
		return fmt.Sprintf("destructive filesystem command: %s", base), true
	case base == "dd":
		return "destructive block-copy command: dd", true
	case base == "wipefs" || base == "fdisk" || base == "parted" || base == "shred":
		return fmt.Sprintf("destructive disk command: %s", base), true
	case base == "shutdown" || base == "reboot" || base == "halt" || base == "poweroff":
		return fmt.Sprintf("system power command: %s", base), true
	case base == "chmod":
		for _, arg := range args {
			if arg == "777" || arg == "0777" || arg == "a+rwx" || arg == "ugo+rwx" {
				return fmt.Sprintf("insecure permission change: chmod %s", arg), true
			}
		}
	case base == "git":
		if reason, dangerous := checkDangerousGit(args); dangerous {
			return reason, true
		}
	case base == "find":
		if reason, dangerous := checkDangerousFind(args); dangerous {
			return reason, true
		}
	}
	return "", false
}

func hasRMRecursiveForce(args []string) bool {
	var recursive, force bool
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--recursive" {
			recursive = true
			continue
		}
		if arg == "--force" {
			force = true
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			for _, ch := range arg[1:] {
				switch ch {
				case 'r', 'R':
					recursive = true
				case 'f':
					force = true
				}
			}
		}
	}
	return recursive && force
}

func checkDangerousGit(args []string) (string, bool) {
	subcmd, subArgs, _ := parseGitSubcommand(args)
	switch subcmd {
	case "push":
		for _, arg := range subArgs {
			if arg == "--" {
				break
			}
			if arg == "--force" || arg == "-f" || strings.HasPrefix(arg, "--force=") {
				return "destructive git command: git push --force", true
			}
		}
	case "reset":
		for _, arg := range subArgs {
			if arg == "--" {
				break
			}
			if arg == "--hard" {
				return "destructive git command: git reset --hard", true
			}
		}
	case "clean":
		for _, arg := range subArgs {
			if arg == "--" {
				break
			}
			if arg == "--force" {
				return "destructive git command: git clean --force", true
			}
			if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg[1:], 'f') {
				return fmt.Sprintf("destructive git command: git clean %s", arg), true
			}
		}
	}
	return "", false
}

func checkDangerousFind(args []string) (string, bool) {
	for i, arg := range args {
		if arg == "-exec" || arg == "-execdir" {
			if i+1 < len(args) {
				execCmd := commandBase(args[i+1])
				if execCmd == "rm" || isShellOrInterpreter(execCmd) {
					return fmt.Sprintf("dangerous find %s %s", arg, execCmd), true
				}
			}
		}
	}
	return "", false
}

func parseGitSubcommand(args []string) (subcmd string, subArgs []string, hasUnsafeGlobal bool) {
	i := 0
	for i < len(args) {
		arg := args[i]
		if arg == "--" {
			i++
			break
		}
		if !strings.HasPrefix(arg, "-") {
			return arg, args[i+1:], hasUnsafeGlobal
		}
		switch arg {
		case "-C", "--git-dir", "--work-tree", "--namespace":
			i += 2
			continue
		case "-c":
			hasUnsafeGlobal = true
			i += 2
			continue
		case "--no-pager", "-p", "--paginate", "--bare", "--no-replace-objects",
			"--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs",
			"--icase-pathspecs", "--no-optional-locks":
			i++
			continue
		default:
			if strings.HasPrefix(arg, "--git-dir=") || strings.HasPrefix(arg, "--work-tree=") ||
				strings.HasPrefix(arg, "--namespace=") || strings.HasPrefix(arg, "-C") {
				i++
				continue
			}
			if strings.HasPrefix(arg, "-c") {
				hasUnsafeGlobal = true
				i++
				continue
			}
			// Unknown global flag before subcommand.
			hasUnsafeGlobal = true
			i++
		}
	}
	return "", nil, hasUnsafeGlobal
}

func classifyGit(args []string) (Verdict, string) {
	subcmd, subArgs, hasUnsafeGlobal := parseGitSubcommand(args)
	if hasUnsafeGlobal {
		return NeedsReview, "git global option requires review"
	}
	if subcmd == "" {
		return NeedsReview, "git invocation without subcommand requires review"
	}

	// Check for --output flags that write files on otherwise read-only subcommands.
	for _, arg := range subArgs {
		if arg == "--" {
			break
		}
		if arg == "--output" || strings.HasPrefix(arg, "--output=") || arg == "--ext-diff" {
			return NeedsReview, fmt.Sprintf("git %s with %s requires review", subcmd, arg)
		}
	}

	switch subcmd {
	case "log", "diff", "status", "show", "rev-parse", "ls-files", "rev-list", "describe":
		return SafeReadOnly, ""
	case "branch":
		if isGitBranchReadOnly(subArgs) {
			return SafeReadOnly, ""
		}
		return NeedsReview, "git branch mutation requires review"
	case "tag":
		if isGitTagReadOnly(subArgs) {
			return SafeReadOnly, ""
		}
		return NeedsReview, "git tag mutation requires review"
	default:
		return NeedsReview, fmt.Sprintf("git subcommand %q requires review", subcmd)
	}
}

func isGitBranchReadOnly(args []string) bool {
	hasListFlag := false
	hasPositional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				hasPositional = true
			}
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			hasPositional = true
			continue
		}
		switch arg {
		case "--list":
			hasListFlag = true
		case "-a", "--all", "-r", "--remotes", "-v", "-vv", "--verbose",
			"--abbrev", "--no-abbrev", "--show-current", "--color", "--no-color",
			"--column", "--no-column", "--merged", "--no-merged",
			"--contains", "--no-contains", "--points-at", "--sort", "--format":
			// Value-taking read-only flags:
			if arg == "--contains" || arg == "--no-contains" || arg == "--points-at" ||
				arg == "--merged" || arg == "--no-merged" || arg == "--sort" || arg == "--format" {
				i++
			}
		default:
			if strings.HasPrefix(arg, "--contains=") || strings.HasPrefix(arg, "--no-contains=") ||
				strings.HasPrefix(arg, "--points-at=") || strings.HasPrefix(arg, "--merged=") ||
				strings.HasPrefix(arg, "--no-merged=") || strings.HasPrefix(arg, "--sort=") ||
				strings.HasPrefix(arg, "--format=") || strings.HasPrefix(arg, "--abbrev=") ||
				strings.HasPrefix(arg, "--color=") {
				continue
			}
			// Any mutating or unknown flag (e.g. -d, -D, -m, -M, -c, -C, -f, -u).
			return false
		}
	}
	// `git branch <name>` creates a branch unless `--list` is specified.
	if hasPositional && !hasListFlag {
		return false
	}
	return true
}

func isGitTagReadOnly(args []string) bool {
	hasListOrVerify := false
	hasPositional := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				hasPositional = true
			}
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			hasPositional = true
			continue
		}
		switch arg {
		case "-l", "--list", "-v", "--verify":
			hasListOrVerify = true
		case "-n", "--sort", "--contains", "--no-contains", "--points-at", "--merged", "--no-merged", "--format", "--color", "--no-color", "--column", "--no-column":
			if arg == "--sort" || arg == "--contains" || arg == "--no-contains" ||
				arg == "--points-at" || arg == "--merged" || arg == "--no-merged" || arg == "--format" {
				i++
			}
		default:
			if strings.HasPrefix(arg, "-n") || strings.HasPrefix(arg, "--sort=") ||
				strings.HasPrefix(arg, "--contains=") || strings.HasPrefix(arg, "--no-contains=") ||
				strings.HasPrefix(arg, "--points-at=") || strings.HasPrefix(arg, "--merged=") ||
				strings.HasPrefix(arg, "--no-merged=") || strings.HasPrefix(arg, "--format=") ||
				strings.HasPrefix(arg, "--color=") {
				continue
			}
			// Any mutating or unknown flag (-a, -s, -d, -f, -m, -F, -u, etc.).
			return false
		}
	}
	if hasPositional && !hasListOrVerify {
		return false
	}
	return true
}

func classifyFind(args []string) (Verdict, string) {
	for _, arg := range args {
		switch {
		case arg == "-exec" || arg == "-execdir" || arg == "-ok" || arg == "-okdir":
			return NeedsReview, fmt.Sprintf("find %s executes commands and requires review", arg)
		case arg == "-delete":
			return NeedsReview, "find -delete mutates the filesystem and requires review"
		case strings.HasPrefix(arg, "-fprint") || arg == "-fls":
			return NeedsReview, fmt.Sprintf("find %s writes files and requires review", arg)
		}
	}
	return SafeReadOnly, ""
}

// safeStandaloneCommands aligns with the agy_tool_safety allowlist.
var safeStandaloneCommands = map[string]bool{
	"ls":        true,
	"pwd":       true,
	"echo":      true,
	"date":      true,
	"whoami":    true,
	"printenv":  true,
	"uname":     true,
	"hostname":  true,
	"which":     true,
	"type":      true,
	"true":      true,
	"false":     true,
	"wc":        true,
	"head":      true,
	"tail":      true,
	"diff":      true,
	"comm":      true,
	"id":        true,
	"df":        true,
	"du":        true,
	"free":      true,
	"uptime":    true,
	"stat":      true,
	"readlink":  true,
	"realpath":  true,
	"basename":  true,
	"dirname":   true,
	"md5sum":    true,
	"sha256sum": true,
	"sha1sum":   true,
	"column":    true,
	"cat":       true,
	"strings":   true,
	"hexdump":   true,
	"od":        true,
	"grep":      true,
	"egrep":     true,
	"fgrep":     true,
	"rg":        true,
	"jq":        true,
	"ps":        true,
	"sleep":     true,
}

func wordLiteral(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, dp := range p.Parts {
				lit, ok := dp.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

func wordDisplay(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var buf bytes.Buffer
	_ = syntax.NewPrinter().Print(&buf, w)
	return buf.String()
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
