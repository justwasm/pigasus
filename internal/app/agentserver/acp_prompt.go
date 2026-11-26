package agentserver

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	schemav1 "github.com/ironpark/acp-go/schema/v1"
	schemav2 "github.com/ironpark/acp-go/schema/v2"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
)

type parsedACPPrompt struct {
	RawText     string
	Attachments []string
	Command     string
	CommandArgs string
	ModelPrompt string
	HistoryText string
}

// isWholeFileURI reports whether uri refers to a whole local file rather than
// a web URL, directory, or line-range/fragment link (e.g. file:///a.go#L10-L20).
func isWholeFileURI(uri string) bool {
	if len(uri) < 7 || !strings.EqualFold(uri[:7], "file://") {
		return false
	}
	if strings.IndexAny(uri, "?#") >= 0 {
		return false
	}
	display := fileURIToPath(uri)
	if display == "" {
		return false
	}
	return !strings.HasSuffix(display, "/") && !strings.HasSuffix(display, "\\")
}

func fileURIToPath(uri string) string {
	if u, err := url.Parse(uri); err == nil && strings.EqualFold(u.Scheme, "file") {
		if u.Host != "" && u.Host != "localhost" {
			return "//" + u.Host + u.Path
		}
		if u.Path != "" {
			return u.Path
		}
	}
	if len(uri) >= 7 && strings.EqualFold(uri[:7], "file://") {
		return uri[7:]
	}
	return uri
}

func resourceLinkText(uri, name, title string) string {
	label := strings.TrimSpace(title)
	if label == "" {
		label = strings.TrimSpace(name)
	}
	if label != "" && label != uri {
		return fmt.Sprintf("Resource link: %s (%s)", uri, label)
	}
	return fmt.Sprintf("Resource link: %s", uri)
}

func formatEmbeddedTextResource(uri, text string) string {
	if isWholeFileURI(uri) {
		path := fileURIToPath(uri)
		// Preserve 0-byte attached files explicitly even when text is empty.
		return fmt.Sprintf("[Attached Resource: %s (editor contents)]\n%s", path, text)
	}
	if text != "" {
		return fmt.Sprintf("[Attached Resource: %s]\n%s", uri, text)
	}
	return resourceLinkText(uri, "", "")
}

func formatEmbeddedBlobResource(uri string, size int) string {
	if isWholeFileURI(uri) {
		path := fileURIToPath(uri)
		return fmt.Sprintf("[Attached Resource: %s (binary, %d bytes)]", path, size)
	}
	return resourceLinkText(uri, "", "")
}

func formatResourceLink(uri, name, title string) string {
	if isWholeFileURI(uri) {
		path := fileURIToPath(uri)
		return fmt.Sprintf("[Attached File: %s]", path)
	}
	return resourceLinkText(uri, name, title)
}

func parseACPPromptV1(blocks []acp1.ContentBlock, cfg Config) parsedACPPrompt {
	var texts []string
	var attachments []string
	for _, block := range blocks {
		if txt, ok := block.As[schemav1.ContentBlockText](); ok {
			texts = append(texts, txt.Text)
		} else if res, ok := block.As[schemav1.ContentBlockResource](); ok {
			if tr, err := res.Resource.As[schemav1.TextResourceContents](); err == nil {
				attachments = append(attachments, formatEmbeddedTextResource(tr.URI, tr.Text))
			} else if br, err := res.Resource.As[schemav1.BlobResourceContents](); err == nil {
				attachments = append(attachments, formatEmbeddedBlobResource(br.URI, len(br.Blob)))
			}
		} else if rl, ok := block.As[schemav1.ContentBlockResourceLink](); ok {
			if rl.URI != "" {
				attachments = append(attachments, formatResourceLink(rl.URI, rl.Name, rl.GetTitle()))
			}
		}
	}
	return finalizeParsedACPPrompt(texts, attachments, cfg)
}

func parseACPPromptV2(blocks []acp2.ContentBlock, cfg Config) parsedACPPrompt {
	var texts []string
	var attachments []string
	for _, block := range blocks {
		if txt, ok := block.As[schemav2.ContentBlockText](); ok {
			texts = append(texts, txt.Text)
		} else if res, ok := block.As[schemav2.ContentBlockResource](); ok {
			if tr, err := res.Resource.As[schemav2.TextResourceContents](); err == nil {
				attachments = append(attachments, formatEmbeddedTextResource(tr.URI, tr.Text))
			} else if br, err := res.Resource.As[schemav2.BlobResourceContents](); err == nil {
				attachments = append(attachments, formatEmbeddedBlobResource(br.URI, len(br.Blob)))
			}
		} else if rl, ok := block.As[schemav2.ContentBlockResourceLink](); ok {
			if rl.URI != "" {
				attachments = append(attachments, formatResourceLink(rl.URI, rl.Name, rl.GetTitle()))
			}
		}
	}
	return finalizeParsedACPPrompt(texts, attachments, cfg)
}

func finalizeParsedACPPrompt(texts, attachments []string, cfg Config) parsedACPPrompt {
	rawText := strings.Join(texts, "\n")
	trimmedText := strings.TrimSpace(rawText)

	var cmdName, cmdArgs string
	if strings.HasPrefix(trimmedText, "/") {
		cmdName, cmdArgs = slashparse.SplitFields(trimmedText)
	}

	bodyText := rawText
	if cmdName != "" {
		if expanded, ok := expandACPSkill(cfg, cmdName, cmdArgs); ok {
			bodyText = expanded
		}
	}

	modelPrompt := joinPromptAndAttachments(bodyText, attachments)
	historyText := joinPromptAndAttachments(rawText, attachments)

	return parsedACPPrompt{
		RawText:     rawText,
		Attachments: attachments,
		Command:     cmdName,
		CommandArgs: cmdArgs,
		ModelPrompt: modelPrompt,
		HistoryText: historyText,
	}
}

func joinPromptAndAttachments(text string, attachments []string) string {
	if len(attachments) == 0 {
		return text
	}
	attBlock := strings.Join(attachments, "\n")
	if strings.TrimSpace(text) == "" {
		return attBlock
	}
	return text + "\n\n" + attBlock
}

func skillInvocationNames(s skills.SkillInfo) []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	add(s.Name)
	if i := strings.LastIndex(s.Name, ":"); i >= 0 {
		add(s.Name[i+1:])
	}
	if dir := filepath.Base(filepath.Dir(s.Path)); dir != "" && dir != "." && dir != string(filepath.Separator) {
		add(dir)
	}
	return names
}

func skillBodyFromPath(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return strings.TrimSpace(s)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return ""
}

func expandACPSkill(cfg Config, name, args string) (string, bool) {
	if name == "" {
		return "", false
	}
	// Built-in local ACP commands are not skills.
	switch name {
	case "clear", "tasks", "agents", "plan", "goal", "model", "context":
		return "", false
	}
	for _, s := range skills.Discover() {
		for _, n := range skillInvocationNames(s) {
			if n == name {
				instr := skillBodyFromPath(s.Path)
				if instr == "" {
					instr = strings.TrimSpace(s.Description)
				}
				if instr == "" {
					instr = "Follow the " + s.Name + " skill."
				}
				if args != "" {
					instr = strings.TrimRight(instr, "\n") + "\n\n" + args
				}
				return instr, true
			}
		}
	}
	for _, s := range agentSkills(cfg) {
		if s.Name == name {
			instr := strings.TrimSpace(s.Description)
			if instr == "" {
				instr = "Follow the " + s.Name + " skill."
			}
			if args != "" {
				instr = strings.TrimRight(instr, "\n") + "\n\n" + args
			}
			return instr, true
		}
	}
	return "", false
}

func hasPersistedSession(convID string) bool {
	if convID == "" {
		return false
	}
	home := confhome.Antigravity()
	transcriptPath := filepath.Join(home, "brain", convID, ".system_generated", "logs", "transcript.jsonl")
	if _, err := os.Stat(transcriptPath); err == nil {
		return true
	}
	dbPath := filepath.Join(home, "conversations", convID+".db")
	if _, err := os.Stat(dbPath); err == nil {
		return true
	}
	return false
}
