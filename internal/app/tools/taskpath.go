// taskpath.go — the paths and counters the task-shaped tools share.
//
// Cycle 379: these three lived with the web tools and the task tools
// and were reached from the command tool as well. The conversation's
// step counter, the last-resort conversation id and the antigravity
// home are facts about *a conversation*, not about one capability, so
// they moved down to the contract.

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	stepMu sync.Mutex
	steps  = map[string]int{}
)

func NextStep(conversationID string) int {
	stepMu.Lock()
	defer stepMu.Unlock()
	steps[conversationID]++
	return steps[conversationID]
}

var (
	procConvID   string
	procConvOnce sync.Once
)

// ProcessConversationID is a last-resort stable id so the acknowledged log path stays inside one dir.
func ProcessConversationID() string {
	procConvOnce.Do(func() {
		procConvID = fmt.Sprintf("schedule-%d", os.Getpid())
	})
	return procConvID
}

// ScheduleHome resolves the antigravity home the reference writes brain/ under.
func ScheduleHome(override string) string {
	if override != "" {
		return override
	}
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}
