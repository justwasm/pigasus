// run_git.go — the git runner the /diff surfaces share.
//
// The /diff panel (the interactive tabbed viewer) and the /diff one-shot
// feedback path both shell out to git. Both need the same environment
// contract: no pager, no editor, and a parseable locale. That contract
// lives here so the two surfaces cannot drift apart.

package diffaction

import (
	"os"
	"os/exec"
	"strings"
)

// RunGitEnvKVs is the slice of KEY=VALUE pairs RunGit always injects on top
// of os.Environ() before exec'ing git. Mirrors internal/tools.AgentShellEnvKVs
// in shape (a package-level slice + a small helper that prepends the parent
// environment) so unit tests can pin the contract without invoking git.
//
// Two groups:
//
//   - Pager disabling: PAGER=cat, GIT_PAGER=cat, JJ_PAGER=cat. AGY 1.3.3
//     changelog lines 50-51: "Fixed jj (Jujutsu VCS) command output being
//     broken by the `less` pager — JJ_PAGER=cat is now set alongside
//     GIT_PAGER=cat so commands like `jj log` show full output instead of
//     paging through interactive less." `cat` writes the file to stdout
//     unchanged. Even though RunGit only invokes `git` today, JJ_PAGER=cat
//     travels alongside GIT_PAGER=cat so the two settings stay in lockstep
//     with the agent-shell-env trio in internal/tools/command.go and the
//     vcsDiff env in diffaction.VCSDiff — a future caller that pipes git
//     output into a jj-aware step inherits the same pager contract.
//   - Editor disabling: GIT_EDITOR=false. AGY 1.3.6 changelog line 7: a
//     future caller invoking `git commit` without `-m` cannot hang the
//     TUI on a hidden vim. HGEDITOR / JJ_EDITOR are omitted because RunGit
//     only ever execs git; VCSDiff is the multi-VCS path and sets the full
//     trio.
//   - Locale pinning: LC_ALL=C. Keeps `git status` / `git diff` parseable
//     across locales (a German LANG= setting turns "On branch main" into
//     something ungreppable).
var RunGitEnvKVs = []string{
	"PAGER=cat",
	"GIT_PAGER=cat",
	"JJ_PAGER=cat",
	"GIT_EDITOR=false",
	"LC_ALL=C",
}

// RunGitEnv returns os.Environ() with RunGitEnvKVs appended. The KVs are
// appended *after* the parent environment so on POSIX systems where
// duplicate env entries take the last value, our overrides win over
// anything the user's shell set (e.g. a GIT_PAGER=less from their rcfile).
func RunGitEnv() []string {
	return append(os.Environ(), RunGitEnvKVs...)
}

// RunGit runs a git subcommand in dir and returns its trimmed stdout.
// PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat disable any pager so a long
// `git log` lands on stdout. GIT_EDITOR=false (1.3.6 line 7) keeps a
// future `git commit` without `-m` from wedging the TUI on a hidden vim.
// LC_ALL=C keeps diffs and status parseable across locales.
func RunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = RunGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}
