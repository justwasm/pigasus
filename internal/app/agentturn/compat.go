package agentturn

import "context"

// SpawnAgyRemote spawns an agy engine using the registered "agy" driver.
func SpawnAgyRemote(ctx context.Context, agyBin, cwd, model, effort, conversationID string) (Engine, error) {
	return Spawn(ctx, "agy", SpawnOptions{
		BinaryPath: agyBin,
		CWD:        cwd,
		Model:      model,
		Effort:     effort,
		SessionID:  conversationID,
	})
}

// SpawnPigasusRemote spawns a pigasus child engine using the registered "pigasus" driver.
func SpawnPigasusRemote(ctx context.Context, pigasusBin, cwd, model, effort, conversationID string) (Engine, error) {
	return Spawn(ctx, "pigasus", SpawnOptions{
		BinaryPath: pigasusBin,
		CWD:        cwd,
		Model:      model,
		Effort:     effort,
		SessionID:  conversationID,
	})
}

// SpawnGrokRemote spawns a grok engine using the registered "grok" driver.
func SpawnGrokRemote(ctx context.Context, grokBin, cwd, model, effort, sessionID string) (Engine, error) {
	return Spawn(ctx, "grok", SpawnOptions{
		BinaryPath: grokBin,
		CWD:        cwd,
		Model:      model,
		Effort:     effort,
		SessionID:  sessionID,
	})
}

// SpawnOpencodeRemote spawns an opencode engine using the registered "opencode" driver.
func SpawnOpencodeRemote(ctx context.Context, opencodeBin, cwd, model, sessionID string) (Engine, error) {
	return Spawn(ctx, "opencode", SpawnOptions{
		BinaryPath: opencodeBin,
		CWD:        cwd,
		Model:      model,
		SessionID:  sessionID,
	})
}

// SpawnClaudeRemote spawns a claude engine using the registered "claude" driver.
func SpawnClaudeRemote(ctx context.Context, claudeBin, cwd, model, sessionID string) (Engine, error) {
	return Spawn(ctx, "claude", SpawnOptions{
		BinaryPath: claudeBin,
		CWD:        cwd,
		Model:      model,
		SessionID:  sessionID,
	})
}

// SpawnCodexRemote spawns a codex engine using the registered "codex" driver.
func SpawnCodexRemote(ctx context.Context, codexBin, cwd, model string) (Engine, error) {
	return Spawn(ctx, "codex", SpawnOptions{
		BinaryPath: codexBin,
		CWD:        cwd,
		Model:      model,
	})
}
