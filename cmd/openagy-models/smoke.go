package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/models"
)

// smokeTest runs a single Chat call against the named model.
func smokeTest(r *models.Registry, name string) error {
	m, ok := r.GetByName(name)
	if !ok {
		return fmt.Errorf("not configured")
	}
	prov, err := models.NewProvider(*m)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(m.Timeout())*time.Second)
	defer cancel()
	fmt.Printf("[test %s] provider=%s model=%s url=%s ...\n", name, prov.Name(), m.ExternalModelName, m.APIURL)
	resp, err := prov.Chat(ctx, models.ChatRequest{
		Messages:  []models.ChatMessage{{Role: "user", Content: "ping"}},
		MaxTokens: 50,
	})
	if err != nil {
		return err
	}
	if resp.Content == "" {
		return fmt.Errorf("empty content")
	}
	if strings.Contains(resp.Content, "mock reply") {
		return fmt.Errorf("got 'mock reply' (provider misrouted to mock?)")
	}
	fmt.Printf("✓ %s (%d tokens): %q\n", name, resp.TotalTokens, truncate(resp.Content, 120))
	printSmokeRawMeta(resp.Raw)
	return nil
}

func printSmokeRawMeta(raw []byte) {
	if len(raw) == 0 {
		return
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if id, _ := asMap["id"].(string); id != "" {
			fmt.Printf("  upstream id: %s\n", id)
		}
		if model, _ := asMap["model"].(string); model != "" {
			fmt.Printf("  upstream model: %s\n", model)
		}
	}
}

func loadOrEmpty(path string) (*models.Registry, error) {
	if path == "" {
		return &models.Registry{}, nil
	}
	return models.LoadRegistry(path)
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func osReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
