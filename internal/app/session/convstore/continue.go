package convstore

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
// only resolve `--continue` to a conversation id.

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	rwidth "github.com/mattn/go-runewidth"
)

// ContinueParentMaxDepth is how many ancestors FindMostRecent walks before
// giving up on parents and trying immediate children. The changelog says
// "parent/child directories"; we treat "parent" as "the workspace's
// immediate ancestry, up to the user's home folder", and "child" as the
// single level of immediate subdirectories.
const ContinueParentMaxDepth = 8

// ResolveContinue picks the conversation `--continue` should adopt, using
// the same-workspace / parent / child walk the v1.4.0 changelog describes.
// Returns ("", false, nil) when nothing was found so the caller can open
// the /resume picker as a fallback.
//
// The second return value is true when the caller's current workspace
// owns the conversation (cwd == conversation.workspace). The renderer
// uses that to drop the "Resumed conversation" prefix in the rare case
// the user is resuming exactly the workspace they are in.
func ResolveContinue(cwd string) (id string, sameWorkspace bool, err error) {
	if cwd == "" {
		return "", false, nil
	}

	// 1. exact workspace match in cwd (fast path, no IO beyond the index)
	if id := MostRecentInWorkspace(cwd); id != "" {
		abs, _ := filepath.Abs(cwd)
		return id, abs == cwd, nil
	}

	// 2. parent / child walk (the v1.4.0 fix). Parents first, then
	//    immediate children of cwd.
	cand, err := FindMostRecent(cwd, ContinueParentMaxDepth)
	if err != nil {
		return "", false, err
	}
	if cand == nil {
		return "", false, nil
	}
	// We have a workspace; pick the most recent conversation under it.
	if id := MostRecentInWorkspace(cand.Dir); id != "" {
		abs, _ := filepath.Abs(cwd)
		return id, abs == cand.Dir, nil
	}
	// FindMostRecent saw a candidate but MostRecentInWorkspace didn't
	// match any row — fall through so the caller can show /resume.
	return "", false, nil
}

// ContinueNoConversationMessage is the recovery message printed when
// `--continue` cannot find a previous conversation. v1.4.0 falls back
// to opening the /resume picker; the clone mirrors that so the user
// still has a recovery path.
func ContinueNoConversationMessage(out io.Writer) {
	fmt.Fprintln(out, "No previous conversation to continue.")
}

// GenerateConversationTitle produces a 4-6 word summary of the first
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
func GenerateConversationTitle(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	cleaned := CleanMarkdownPrompt(prompt)
	if cleaned == "" {
		return ""
	}
	return ExtractTitleTokens(cleaned)
}

// CleanMarkdownPrompt strips code fences and leading block prefixes from prompt.
func CleanMarkdownPrompt(prompt string) string {
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
		trim = StripMarkdownPrefix(trim)
		b.WriteString(trim)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(b.String())
}

// StripMarkdownPrefix removes markdown block prefixes like "> ", "# ", "- ", "* ", "+ ".
func StripMarkdownPrefix(s string) string {
	for {
		if strings.HasPrefix(s, "> ") || strings.HasPrefix(s, "# ") ||
			strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ") ||
			strings.HasPrefix(s, "+ ") {
			s = s[2:]
			continue
		}
		break
	}
	return s
}

// ExtractTitleTokens extracts 4-6 words within a 60-cell runewidth window.
func ExtractTitleTokens(cleaned string) string {
	tokens := strings.Fields(cleaned)
	out := make([]string, 0, 6)
	for _, t := range tokens {
		t = strings.TrimRight(t, ":;,.")
		if t == "" {
			continue
		}
		if rwidth.StringWidth(strings.Join(append(out, t), " ")) > 60 {
			break
		}
		out = append(out, t)
		if len(out) >= 6 {
			break
		}
	}
	if len(out) > 0 {
		return strings.Join(out, " ")
	}
	return strings.TrimSpace(cleaned)
}

// ResumeHintBanner prints the v1.4.0 startup hint the reference
// binary shows when a previous conversation exists for the current
// workspace (or its parent/child walk).
func ResumeHintBanner(cwd, binaryName string, alreadyHaveID, continueFlag bool, out io.Writer) {
	if out == nil || alreadyHaveID || continueFlag {
		return
	}
	id, _, _ := ResolveContinue(cwd)
	if id == "" {
		return
	}
	if binaryName == "" {
		binaryName = "wagyu"
	}
	fmt.Fprintf(out, "\nResume with -c (or command below):\n%s --conversation=%s\n\n", binaryName, id)
}
