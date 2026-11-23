package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/personas"
)

func TestSlashPersonaAndSkinCommands(t *testing.T) {
	m := &RootModel{}
	m.session.persona = personas.PersonaEngineering
	m.session.productSkin = personas.ProductSkinDeveloper

	if m.IsKnowledgeWork() {
		t.Errorf("expected IsKnowledgeWork() to be false for developer skin")
	}

	// 1. Query current persona
	handled, _ := m.slashPersona("")
	if !handled {
		t.Fatalf("expected handled=true")
	}
	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "engineering") || !strings.Contains(joined, "Available personas") {
		t.Errorf("slashPersona query output missing current persona or listing: %s", joined)
	}

	// 2. Switch to knowledge-work persona
	m.conversation.response = nil
	handled, _ = m.slashPersona("knowledge-work")
	if !handled {
		t.Fatalf("expected handled=true")
	}
	joined = strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Switched to persona knowledge-work") {
		t.Errorf("slashPersona switch output unexpected: %s", joined)
	}
	if m.Persona() != personas.PersonaKnowledgeWork {
		t.Errorf("m.Persona() = %q, want %q", m.Persona(), personas.PersonaKnowledgeWork)
	}
	if m.ProductSkin() != personas.ProductSkinNonTechnical {
		t.Errorf("m.ProductSkin() = %d, want %d", m.ProductSkin(), personas.ProductSkinNonTechnical)
	}
	if !m.IsKnowledgeWork() {
		t.Errorf("expected IsKnowledgeWork() to be true after switching to knowledge-work")
	}

	// Verify promptOptions propagation
	opts := m.promptOptions()
	if opts.Persona != personas.PersonaKnowledgeWork {
		t.Errorf("promptOptions.Persona = %q, want %q", opts.Persona, personas.PersonaKnowledgeWork)
	}
	if opts.ProductSkin != personas.ProductSkinNonTechnical {
		t.Errorf("promptOptions.ProductSkin = %d, want %d", opts.ProductSkin, personas.ProductSkinNonTechnical)
	}

	// 3. Switch skin directly via /skin
	m.conversation.response = nil
	handled, _ = m.slashSkin("developer")
	if !handled {
		t.Fatalf("expected slashSkin handled=true")
	}
	joined = strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Switched product skin to developer") {
		t.Errorf("slashSkin output unexpected: %s", joined)
	}
	if m.ProductSkin() != personas.ProductSkinDeveloper {
		t.Errorf("m.ProductSkin() = %d, want %d", m.ProductSkin(), personas.ProductSkinDeveloper)
	}
	if m.IsKnowledgeWork() {
		t.Errorf("expected IsKnowledgeWork() to be false after switching to developer skin")
	}

	// Clean up settings changes from tests
	_ = settings.DeleteField("persona")
	_ = settings.DeleteField("productSkin")
	_ = settings.DeleteField("project4kMode")
}
