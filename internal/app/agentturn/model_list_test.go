package agentturn_test

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/all"
)

func TestCodexRemote_ListModelsLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	eng, err := agentturn.SpawnCodexRemote(ctx, "", "", "")
	if err != nil {
		t.Skipf("skipping live codex model list test: %v", err)
		return
	}
	defer eng.Close()

	ml, ok := eng.(agentturn.ModelLister)
	if !ok {
		t.Fatal("expected codex engine to implement ModelLister")
	}

	models, err := ml.ListModels(ctx)
	if err != nil {
		t.Fatalf("Codex ListModels failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least 1 model from Codex")
	}

	foundSol := false
	foundGemini := false
	for _, m := range models {
		if m.ID == "gpt-6.1-sol" {
			foundSol = true
			if len(m.Tiers) == 0 {
				t.Errorf("expected gpt-6.1-sol to have effort tiers, got none")
			}
		}
		if m.ID == "gemini-3.8-flash" {
			foundGemini = true
			if len(m.Tiers) == 0 {
				t.Errorf("expected gemini-3.8-flash to have effort tiers, got none")
			}
		}
	}
	if !foundSol {
		t.Errorf("expected gpt-6.1-sol in codex models, got: %+v", models)
	}
	if !foundGemini {
		t.Errorf("expected gemini-3.8-flash in codex models, got: %+v", models)
	}
}
