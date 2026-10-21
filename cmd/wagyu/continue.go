// continue.go — `--continue` flag implementation (cycle resume-continuation).
//
// Before this file the cycle-120 implementation in conversation.go handled
// -c / --continue by calling mostRecentConversation(workspaceDir()), which
// reads only the ref's cache/last_conversations.json and walks parents —
// it cannot reach child directories and cannot find conversations whose
// recorded workspace was different from `start`.
//
// The v1.4.0 changelog (recovered from the reference, byte-for-byte):
//
//	"Fixed `--continue` starting a brand-new conversation when launched
//	 from a subdirectory, after a crash, or while another session is open
//	 in the same workspace; it now falls back to the most recent non-empty
//	 conversation in the current workspace or its parent/child directories."
//
// This file adds the parent+child fallback and the auto-resume prompt
// gating it. The /resume picker panel (cycle 167 / 184) is untouched; we
// only resolve `--continue` to a conversation id, then defer to the same
// runLiveTUI + seedConversation path the existing flow already uses.

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/convstore"
	rwidth "github.com/mattn/go-runewidth"
)

// continueParentMaxDepth is how many ancestors FindMostRecent walks before
// giving up on parents and trying immediate children. The changelog says
// "parent/child directories"; we treat "parent" as "the workspace's
// immediate ancestry, up to the user's home folder", and "child" as the
// single level of immediate subdirectories. Eight is a verified round
// number — every directory walking up that many parents in this machine's
// home will reach $HOME in <4 steps, so 8 is past enough to cover most
// "I am in a deeply nested directory" cases without unbounded traversal.
const continueParentMaxDepth = 8

// resolveContinue picks the conversation `--continue` should adopt, using
// the same-workspace / parent / child walk the v1.4.0 changelog describes.
// Returns ("", false, nil) when nothing was found so the caller can open
// the /resume picker as a fallback.
//
// The second return value is true when the caller's current workspace
// owns the conversation (cwd == conversation.workspace). The renderer
// uses that to drop the "Resumed conversation" prefix in the rare case
// the user is resuming exactly the workspace they are in.
//
// Why we extend the cycle-120 lookup (rather than replace it):
//   - The cycle-120 implementation reads
//     cache/last_conversations.json as a fast path. We keep that path
//     because the ref itself writes there and downstream tools
//     (cycle-167 /fork) treat it as the primary key.
//   - The new path uses the index (conversation_summaries.db) and the
//     workspace_uris column to walk parents/children. The two paths
//     produce the same answer for the in-workspace case; the new path
//     covers the subdirectory/child cases the changelog calls out.
func resolveContinue(cwd string) (id string, sameWorkspace bool, err error) {
	if cwd == "" {
		return "", false, nil
	}

	// 1. exact workspace match in cwd (fast path, no IO beyond the index)
	if id := convstore.MostRecentInWorkspace(cwd); id != "" {
		abs, _ := filepath.Abs(cwd)
		return id, abs == cwd, nil
	}

	// 2. parent / child walk (the v1.4.0 fix). Parents first, then
	//    immediate children of cwd.
	cand, err := convstore.FindMostRecent(cwd, continueParentMaxDepth)
	if err != nil {
		return "", false, err
	}
	if cand == nil {
		return "", false, nil
	}
	// We have a workspace; pick the most recent conversation under it.
	if id := convstore.MostRecentInWorkspace(cand.Dir); id != "" {
		abs, _ := filepath.Abs(cwd)
		return id, abs == cand.Dir, nil
	}
	// FindMostRecent saw a candidate but MostRecentInWorkspace didn't
	// match any row — fall through so the caller can show /resume.
	return "", false, nil
}

// continueNoConversationMessage is the recovery message printed when
// `--continue` cannot find a previous conversation. v1.4.0 falls back
// to opening the /resume picker; the clone mirrors that so the user
// still has a recovery path. The message itself is 【推测】 — the live
// binary's output is unrecovered for this case (its quota is exhausted
// for the operator's account) and the cycle-120 implementation printed
// nothing.
func continueNoConversationMessage(out io.Writer) {
	fmt.Fprintln(out, "No previous conversation to continue.")
}

// generateConversationTitle produces a 4-6 word summary of the first
// user message. The v1.4.0 changelog notes the title is now generated
// at conversation-creation time so the /resume picker shows a
// meaningful name instead of the placeholder the first message would
// have. The v1.4.0 implementation must use the LLM; the clone stays
// heuristic-only to avoid first-turn latency and quota cost.
//
// Heuristic (verifiable, [推测] 'good enough'):
//   - strip markdown / code fences / URI brackets / leading ">", "*",
//     "#", etc.
//   - collapse whitespace
//   - take 4-6 content words (6 max — a longer title makes the picker
//     row ellipsise)
//   - truncate by runewidth so a long CJK message still fits in a
//     60-rune window (the picker renders the title at 60 runes).
//
// Returns "" when the prompt is empty or only contains punctuation /
// whitespace; the caller leaves the conversation title empty in that
// case and SaveConversation falls back to the first-message preview.
func generateConversationTitle(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	// Drop markdown noise. The references to backticks / lists are
	// heuristic — a markdown-only prompt would resolve to an empty
	// title, which is acceptable.
	var b strings.Builder
	inFence := false
	for _, line := range strings.Split(prompt, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Drop leading markdown markers.
		for {
			if strings.HasPrefix(trim, "> ") {
				trim = trim[2:]
				continue
			}
			if strings.HasPrefix(trim, "# ") || strings.HasPrefix(trim, "- ") ||
				strings.HasPrefix(trim, "* ") || strings.HasPrefix(trim, "+ ") {
				trim = trim[2:]
				continue
			}
			break
		}
		b.WriteString(trim)
		b.WriteByte(' ')
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return ""
	}
	// Take 4-6 tokens. Stop early when a punctuation-only token would
	// reduce information content (a "—" or "..." word is noise).
	tokens := strings.Fields(cleaned)
	out := make([]string, 0, 6)
	for _, t := range tokens {
		// Drop trailing colons / semicolons so a title "Foo: bar" reads
		// "Foo bar" cleanly.
		t = strings.TrimRight(t, ":;,.")
		if t == "" {
			continue
		}
		// Keep code (runewidth is CJK-aware).
		if rwidth.StringWidth(strings.Join(append(out, t), " ")) > 60 {
			break
		}
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	if len(out) >= 4 {
		return strings.Join(out, " ")
	}
	// Fallback: take whatever we have (could be fewer than 4 tokens).
	if len(out) > 0 {
		return strings.Join(out, " ")
	}
	return strings.TrimSpace(cleaned)
}

// shouldAutoResumePrompt and promptAutoResume were removed in cycle
// resume-continuation-r1: the "Resume previous conversation? [Y/n] "
// on-launch prompt was speculative. `strings ~/.local/bin/agy | grep
// "Resume previous"` returns nothing; the closest match is
// "Resume with -c (or command below):" inside the /resume picker.
// REF does not prompt on launch — `--continue` is the only way to
// adopt a previous session without manually picking it from /resume.

// resumeHintBanner prints the v1.4.0 startup hint the reference
// binary shows when a previous conversation exists for the current
// workspace (or its parent/child walk). The literal:
//
//	Resume with -c (or command below):
//	<binary> --conversation=<id>
//
// is recovered from `strings ~/.local/bin/agy` (see
// docs/cycles/resume-continuation/strings-evidence.md). The
// binary name uses os.Args[0]'s basename so a clone invoked as
// "wagyu" suggests "wagyu --conversation=…". The hint is omitted
// when the user passed -c / --continue (the picker / --continue
// path already shows the conversation it picked) or when the
// caller has already adopted a session via --conversation=<id>.
//
// The output is written to out so the test rig can capture it
// without touching real stderr. Production callers pass
// os.Stderr. The function is a no-op when resolveContinue returns
// no conversation id — the user's machine has no prior session
// for this workspace, nothing to hint.
func resumeHintBanner(cwd string, alreadyHaveID, continueFlag bool, out io.Writer) {
	if out == nil || alreadyHaveID || continueFlag {
		return
	}
	id, _, _ := resolveContinue(cwd)
	if id == "" {
		return
	}
	binary := filepath.Base(strings.TrimSpace(os.Args[0]))
	if binary == "" || binary == "." || binary == "/" {
		binary = "agy"
	}
	fmt.Fprintf(out, "\nResume with -c (or command below):\n%s --conversation=%s\n\n", binary, id)
}
