package convstore

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SaveQueuedPrompts stores unexecuted pending prompts for a conversation on disk.
func SaveQueuedPrompts(convID string, prompts []string) error {
	if convID == "" {
		return nil
	}
	dir := filepath.Join(StoreDir(), "conversations")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, convID+".queued.json")
	if len(prompts) == 0 {
		_ = os.Remove(path)
		return nil
	}
	data, err := json.Marshal(prompts)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadQueuedPrompts loads unexecuted pending prompts for a conversation from disk.
func LoadQueuedPrompts(convID string) []string {
	if convID == "" {
		return nil
	}
	path := filepath.Join(StoreDir(), "conversations", convID+".queued.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var prompts []string
	if err := json.Unmarshal(data, &prompts); err != nil {
		return nil
	}
	return prompts
}
