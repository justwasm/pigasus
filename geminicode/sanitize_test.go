package geminicode

import (
	"strings"
	"testing"
)

func TestSanitizePromptText_PreservesCodeBlocks(t *testing.T) {
	input := "Outside rule: Prefer read_file, edit_file, and write_file over cat/sed\n\n" +
		"```go\n" +
		"// In code: Prefer read_file, edit_file, and write_file over cat/sed\n" +
		"func test() {}\n" +
		"```\n\n" +
		"Inline: `Prefer read_file, edit_file, and write_file over cat/sed`"

	got := SanitizePromptText(input)

	// Outside should be sanitized
	if strings.Contains(got, "Outside rule: Prefer read_file, edit_file, and write_file over cat/sed") {
		t.Errorf("outside code was not sanitized: %s", got)
	}
	if !strings.Contains(got, "Outside rule: Prefer read file, edit file, and write file over cat/sed") {
		t.Errorf("expected sanitized outside text, got: %s", got)
	}

	// Inside code blocks must be preserved verbatim
	if !strings.Contains(got, "// In code: Prefer read_file, edit_file, and write_file over cat/sed") {
		t.Errorf("fenced code block was modified: %s", got)
	}
	if !strings.Contains(got, "`Prefer read_file, edit_file, and write_file over cat/sed`") {
		t.Errorf("inline code block was modified: %s", got)
	}
}

func TestSanitizePromptText_StripsBillingHeadersAndFingerprints(t *testing.T) {
	input := "x-anthropic-billing-header: cc_version=1.0\n" +
		"x-billing-token: secret123\n" +
		"You are a Claude agent, built on Anthropic's Claude Agent SDK.\n" +
		"User request: please help me with code.\n" +
		"Always read_file before edit_file."

	got := SanitizePromptText(input)

	if strings.Contains(got, "billing-header") || strings.Contains(got, "x-billing-token") {
		t.Errorf("billing headers were not stripped: %s", got)
	}
	if strings.Contains(got, "built on Anthropic's Claude Agent SDK") {
		t.Errorf("agent SDK fingerprint was not stripped: %s", got)
	}
	if !strings.Contains(got, "User request: please help me with code.") {
		t.Errorf("user request was lost: %s", got)
	}
	if !strings.Contains(got, "Always read file before edit file") {
		t.Errorf("tool guideline was not neutralized: %s", got)
	}
}

func TestSanitizePromptText_NeutralizesPiCodemodeAndWorkBuddy(t *testing.T) {
	input := "Running in codemode environment.\n" +
		"this conversation is powered by gemini-4-hydrogen\n" +
		"# Codex desktop context\n" +
		"as Codex:\n" +
		"call read_file on its SKILL.md"

	got := SanitizePromptText(input)

	if strings.Contains(got, "codemode") {
		t.Errorf("codemode was not neutralized: %s", got)
	}
	if !strings.Contains(got, "codem0de") {
		t.Errorf("expected codem0de, got: %s", got)
	}
	if !strings.Contains(got, "Selected model: gemini-4-hydrogen") {
		t.Errorf("WorkBuddy powered by was not converted: %s", got)
	}
	if !strings.Contains(got, "# Desktop application context") {
		t.Errorf("Codex desktop context was not converted: %s", got)
	}
	if !strings.Contains(got, "as an assistant:") {
		t.Errorf("as Codex was not converted: %s", got)
	}
	if !strings.Contains(got, "call read file on its SKILL.md") {
		t.Errorf("call read_file on its SKILL.md was not neutralized: %s", got)
	}
}
