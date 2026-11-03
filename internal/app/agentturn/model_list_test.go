package agentturn_test

import (
	"context"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/agentturn"
)

func TestAgyRemote_ListModels(t *testing.T) {
	r := &agentturn.AgyRemote{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := r.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least 1 model from AgyRemote")
	}

	foundFlash := false
	for _, m := range models {
		if m.ID == "gemini-3.8-flash-high" || m.ID == "gemini-3.8-flash" {
			foundFlash = true
			break
		}
	}
	if !foundFlash {
		t.Errorf("expected gemini-3.8-flash model in agy model list, got: %+v", models)
	}
}

func TestClaudeRemote_ListModels(t *testing.T) {
	r := &agentturn.ClaudeRemote{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := r.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected at least 1 model from ClaudeRemote")
	}

	foundSonnet := false
	for _, m := range models {
		if m.ID == "sonnet" || m.ID == "claude-sonnet-4-6" {
			foundSonnet = true
			break
		}
	}
	if !foundSonnet {
		t.Errorf("expected sonnet model in claude model list, got: %+v", models)
	}
}

func TestCodexRemote_ListModelsLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	codex, err := agentturn.SpawnCodexRemote(ctx, "", "", "")
	if err != nil {
		t.Skipf("skipping live codex model list test: %v", err)
		return
	}
	defer codex.Close()

	models, err := codex.ListModels(ctx)
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
