package livechat

import (
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/voice"
)

// ProactiveWatcher monitors workspace git changes and pushes proactive nudges.
type ProactiveWatcher struct {
	workspace       string
	sess            *voice.NativeLiveSession
	prog            *tea.Program
	lastState       string
	lastSpoken      time.Time
	cooldown        time.Duration
	paused          atomic.Bool
	pendingUserTurn atomic.Bool
	stopCh          chan struct{}
}

func NewProactiveWatcher(workspace string, sess *voice.NativeLiveSession, cooldown time.Duration) *ProactiveWatcher {
	return &ProactiveWatcher{
		workspace: workspace,
		sess:      sess,
		cooldown:  cooldown,
		stopCh:    make(chan struct{}),
	}
}

func (w *ProactiveWatcher) SetProgram(prog *tea.Program) {
	w.prog = prog
}

func (w *ProactiveWatcher) SetPaused(paused bool) {
	w.paused.Store(paused)
}

func (w *ProactiveWatcher) IsPaused() bool {
	return w.paused.Load()
}

func (w *ProactiveWatcher) ResetPending() {
	w.pendingUserTurn.Store(false)
}

func (w *ProactiveWatcher) Start() {
	w.lastState = w.checkGitStatus()
	w.lastSpoken = time.Now()

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				w.poll()
			}
		}
	}()
}

func (w *ProactiveWatcher) Stop() {
	close(w.stopCh)
}

func (w *ProactiveWatcher) checkGitStatus() string {
	cmd := exec.Command("git", "status", "-s")
	cmd.Dir = w.workspace
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (w *ProactiveWatcher) poll() {
	if w.paused.Load() {
		return
	}
	if w.pendingUserTurn.Load() {
		// Do not repeatedly nag the user if they haven't spoken or acted since previous prompt
		return
	}
	if w.sess != nil && (w.sess.Speaking() || w.sess.IsReconnecting()) {
		return
	}
	if time.Since(w.lastSpoken) < w.cooldown {
		return
	}

	current := w.checkGitStatus()
	if current == "" || current == w.lastState {
		return
	}

	w.lastState = current
	w.lastSpoken = time.Now()
	w.pendingUserTurn.Store(true)

	files := parseChangedFiles(current)
	if w.prog != nil {
		w.prog.Send(proactiveEventMsg{files: files})
	}

	if w.sess != nil {
		prompt := fmt.Sprintf(
			"[PROACTIVE_EVENT: Detected file changes in workspace: %s. In one brief, natural spoken sentence, mention to the user that you noticed these changes and ask if they'd like you to run tests or review them.]",
			strings.Join(files, ", "),
		)
		_ = w.sess.SendText(prompt)
	}
}

func parseChangedFiles(status string) []string {
	var files []string
	lines := strings.Split(status, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[2:]))
		}
	}
	if len(files) > 4 {
		return append(files[:4], fmt.Sprintf("+%d more files", len(files)-4))
	}
	return files
}
