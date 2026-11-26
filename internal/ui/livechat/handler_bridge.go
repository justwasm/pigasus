package livechat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/voice"
)

type liveChatHandlerBridge struct {
	prog          *tea.Program
	workspace     string
	sess          *voice.NativeLiveSession
	thinkerBridge *voice.ThinkerBridge

	thinkerMu         sync.Mutex
	thinkerCancel     context.CancelFunc
	thinkerInvID      string
	thinkerTask       string
	thinkerCancelSent bool
	thinkerDoneCh     chan struct{}
}

func (h *liveChatHandlerBridge) thinkerBridgeOptions() []voice.ThinkerBridgeOption {
	return []voice.ThinkerBridgeOption{
		voice.WithOnProgress(func(note string) {
			if h.prog != nil && strings.TrimSpace(note) != "" {
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s🔧 [Thinker]: %s%s", ansiDim, note, ansiReset)})
			}
		}),
		voice.WithOnComplete(func(answer string, err error) {
			h.thinkerMu.Lock()
			task := h.thinkerTask
			alreadySent := h.thinkerCancelSent
			doneCh := h.thinkerDoneCh
			h.thinkerInvID = ""
			h.thinkerTask = ""
			h.thinkerCancelSent = false
			h.thinkerDoneCh = nil
			h.thinkerMu.Unlock()

			if doneCh != nil {
				close(doneCh)
			}
			if !alreadySent && h.prog != nil {
				h.prog.Send(thinkerDoneMsg{task: task, result: answer, err: err})
			}
		}),
	}
}

func (h *liveChatHandlerBridge) OnUserSpeech(text string, interim bool) {
	if h.prog != nil {
		h.prog.Send(userSpeechMsg{text: text, interim: interim})
	}
}

func (h *liveChatHandlerBridge) OnAgentSpeech(text string) {
	if h.prog != nil {
		h.prog.Send(agentSpeechMsg{text: text})
	}
}

func (h *liveChatHandlerBridge) OnAgentSpeechDelta(delta string) {
	if h.prog != nil {
		h.prog.Send(agentSpeechDeltaMsg{delta: delta})
	}
}

func (h *liveChatHandlerBridge) OnTurnComplete() {
	if h.prog != nil {
		h.prog.Send(agentTurnCompleteMsg{})
	}
}

func (h *liveChatHandlerBridge) OnInterrupted() {
	if h.prog != nil {
		h.prog.Send(interruptedMsg{})
	}
}

func (h *liveChatHandlerBridge) OnError(err error) {
	if h.prog != nil && err != nil {
		h.prog.Send(errorMsg{err: err})
	}
}

func (h *liveChatHandlerBridge) OnClose() {
	if h.prog != nil {
		h.prog.Send(logEventMsg{text: fmt.Sprintf("%s📞 [Live session closed]%s", ansiDim, ansiReset)})
	}
}

func (h *liveChatHandlerBridge) OnReconnecting(attempt int, maxAttempts int, err error) {
	if h.prog != nil {
		h.prog.Send(reconnectingMsg{attempt: attempt, maxAttempts: maxAttempts, err: err})
	}
}

func (h *liveChatHandlerBridge) OnReconnected() {
	if h.prog != nil {
		h.prog.Send(reconnectedMsg{})
	}
}

func (h *liveChatHandlerBridge) OnToolCall(call voice.LiveToolCall) {
	switch call.Name {
	case "search_web", "web_search":
		query, _ := call.Args["query"].(string)
		if query == "" {
			query, _ = call.Args["q"].(string)
		}
		query = strings.TrimSpace(query)
		domain, _ := call.Args["domain"].(string)
		domain = strings.TrimSpace(domain)
		if query == "" {
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": "search_web: query is required"})
			}
			return
		}

		if h.prog != nil {
			msg := fmt.Sprintf("%s🔍 [Web Search]: %s%s", ansiCyan+ansiBold, query, ansiReset)
			if domain != "" {
				msg += fmt.Sprintf(" %s(site: %s)%s", ansiDim, domain, ansiReset)
			}
			h.prog.Send(logEventMsg{text: msg})
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := performWebSearch(ctx, query, domain)
			if err != nil {
				if h.prog != nil {
					h.prog.Send(logEventMsg{text: fmt.Sprintf("%s❌ [Search Failed]: %v%s", ansiRed, err, ansiReset)})
				}
				if h.sess != nil {
					_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": err.Error()})
				}
				return
			}
			if h.prog != nil {
				summary := summarizeResult(result)
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s✅ [Search Results]:%s\n%s%s%s", ansiGreen, ansiReset, ansiDim, summary, ansiReset)})
			}
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"result": result})
			}
		}()

	case "fetch_url":
		rawURL, _ := call.Args["url"].(string)
		if rawURL == "" {
			rawURL, _ = call.Args["Url"].(string)
		}
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": "fetch_url: url is required"})
			}
			return
		}
		if h.prog != nil {
			h.prog.Send(logEventMsg{text: fmt.Sprintf("%s🌐 [Fetch URL]: %s%s", ansiCyan+ansiBold, rawURL, ansiReset)})
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			content, err := fetchURLContent(ctx, rawURL)
			if err != nil {
				if h.prog != nil {
					h.prog.Send(logEventMsg{text: fmt.Sprintf("%s❌ [Fetch Failed]: %v%s", ansiRed, err, ansiReset)})
				}
				if h.sess != nil {
					_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": err.Error()})
				}
				return
			}
			if h.prog != nil {
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s✅ [Fetched]: %d bytes from %s%s", ansiGreen, len(content), rawURL, ansiReset)})
			}
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"content": content})
			}
		}()

	case "run_command":
		cmdStr, _ := call.Args["command"].(string)
		if cmdStr == "" {
			cmdStr, _ = call.Args["cmd"].(string)
		}
		cmdStr = strings.TrimSpace(cmdStr)
		if cmdStr == "" {
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": "run_command: command is required"})
			}
			return
		}
		if h.prog != nil {
			h.prog.Send(logEventMsg{text: fmt.Sprintf("%s⚙️ [Command]: %s%s", ansiYellow+ansiBold, cmdStr, ansiReset)})
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			out, err := executeRawCommand(ctx, h.workspace, cmdStr)
			if err != nil {
				if h.prog != nil {
					h.prog.Send(logEventMsg{text: fmt.Sprintf("%s❌ [Command Failed]: %v%s\n%s%s%s", ansiRed, err, ansiReset, ansiDim, out, ansiReset)})
				}
				if h.sess != nil {
					_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"output": out, "error": err.Error()})
				}
				return
			}
			if h.prog != nil {
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s✅ [Command Output]:%s\n%s%s%s", ansiGreen, ansiReset, ansiDim, out, ansiReset)})
			}
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"output": out})
			}
		}()

	case "read_file":
		path, _ := call.Args["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": "read_file: path is required"})
			}
			return
		}
		if h.prog != nil {
			h.prog.Send(logEventMsg{text: fmt.Sprintf("%s📄 [Read File]: %s%s", ansiCyan, path, ansiReset)})
		}
		go func() {
			content, err := readWorkspaceFile(h.workspace, path)
			if err != nil {
				if h.sess != nil {
					_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": err.Error()})
				}
				return
			}
			if h.sess != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"content": content})
			}
		}()
	}
}

func (h *liveChatHandlerBridge) OnThinkerCancel(invocationID string) {
	if h.thinkerBridge != nil {
		h.thinkerMu.Lock()
		activeID := h.thinkerInvID
		wasRunning := h.thinkerBridge.IsRunning()
		if wasRunning || activeID != "" {
			h.thinkerCancelSent = true
		}
		h.thinkerMu.Unlock()

		_ = h.thinkerBridge.CancelActive()

		if invocationID == "" {
			invocationID = activeID
		}
		if h.sess != nil && h.sess.IsActive() {
			if !wasRunning {
				_ = h.sess.SendTaskStopped()
			}
			if invocationID != "" && (!wasRunning || invocationID != activeID) {
				_ = h.sess.SendToolResponse(invocationID, "cancel_thinker_agent", map[string]any{"cancelled": true})
			}
		}
		if h.prog != nil {
			h.prog.Send(thinkerDoneMsg{task: "cancelled", err: context.Canceled})
		}
		return
	}

	h.thinkerMu.Lock()
	cancel := h.thinkerCancel
	activeID := h.thinkerInvID
	if cancel != nil {
		h.thinkerCancelSent = true
	}
	h.thinkerMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if invocationID == "" {
		invocationID = activeID
	}
	if h.sess != nil && h.sess.IsActive() {
		_ = h.sess.SendTaskStopped()
		if invocationID != "" {
			_ = h.sess.SendToolResponse(invocationID, "cancel_thinker_agent", map[string]any{"cancelled": true})
		}
	}
	if h.prog != nil {
		h.prog.Send(thinkerDoneMsg{task: "cancelled", err: context.Canceled})
	}
}

func (h *liveChatHandlerBridge) OnThinkerHandoff(invocationID string, task string) {
	if h.prog != nil {
		h.prog.Send(thinkerStartMsg{id: invocationID, task: task})
	}

	if h.thinkerBridge != nil {
		h.thinkerMu.Lock()
		prevDone := h.thinkerDoneCh
		if prevDone != nil {
			h.thinkerCancelSent = true
		}
		h.thinkerMu.Unlock()

		if prevDone != nil {
			_ = h.thinkerBridge.CancelActive()
			select {
			case <-prevDone:
			case <-time.After(500 * time.Millisecond):
			}
		}

		var history []voice.LiveTurnRecord
		if h.sess != nil {
			history = h.sess.ConversationHistory()
			_ = h.sess.SendThinkerNote("Starting execution for: " + task)
		}
		promptWithHistory := formatThinkerPromptWithHistory(task, history)

		doneCh := make(chan struct{})
		h.thinkerMu.Lock()
		h.thinkerInvID = invocationID
		h.thinkerTask = task
		h.thinkerCancelSent = false
		h.thinkerDoneCh = doneCh
		h.thinkerMu.Unlock()

		var liveSess voice.LiveSession
		if h.sess != nil {
			liveSess = &nativeLiveSessionAdapter{sess: h.sess}
		}

		if err := h.thinkerBridge.Dispatch(context.Background(), liveSess, invocationID, promptWithHistory); err != nil {
			h.thinkerMu.Lock()
			if h.thinkerDoneCh == doneCh {
				close(doneCh)
				h.thinkerDoneCh = nil
			}
			if h.thinkerInvID == invocationID {
				h.thinkerInvID = ""
				h.thinkerTask = ""
				h.thinkerCancelSent = false
			}
			h.thinkerMu.Unlock()

			if h.sess != nil && h.sess.IsActive() {
				if invocationID != "" {
					_ = h.sess.CompleteHandoff(invocationID, map[string]any{"error": err.Error()})
				}
				_ = h.sess.SendThinkerNote("Task failed: " + err.Error())
			}
			if h.prog != nil {
				h.prog.Send(thinkerDoneMsg{task: task, err: err})
			}
		}
		return
	}

	taskCtx, taskCancel := context.WithCancel(context.Background())
	h.thinkerMu.Lock()
	prevCancel := h.thinkerCancel
	h.thinkerCancel = taskCancel
	h.thinkerInvID = invocationID
	h.thinkerCancelSent = false
	h.thinkerMu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}

	go func() {
		defer taskCancel()
		if h.sess != nil {
			_ = h.sess.SendThinkerNote("Starting execution for: " + task)
		}

		var history []voice.LiveTurnRecord
		if h.sess != nil {
			history = h.sess.ConversationHistory()
		}
		result, err := executeThinkerTaskWithContext(taskCtx, h.workspace, task, history)

		h.thinkerMu.Lock()
		alreadySent := h.thinkerCancelSent
		if h.thinkerInvID == invocationID {
			h.thinkerCancel = nil
			h.thinkerInvID = ""
			h.thinkerCancelSent = false
		}
		h.thinkerMu.Unlock()

		if err != nil {
			if errors.Is(err, context.Canceled) {
				if !alreadySent && h.sess != nil && h.sess.IsActive() {
					_ = h.sess.SendTaskStopped()
					if invocationID != "" {
						_ = h.sess.CompleteHandoff(invocationID, map[string]any{"cancelled": true})
					}
				}
			} else if h.sess != nil && h.sess.IsActive() {
				if invocationID != "" {
					_ = h.sess.CompleteHandoff(invocationID, map[string]any{"error": err.Error()})
				}
				_ = h.sess.SendThinkerNote("Task failed: " + err.Error())
			}
			if !alreadySent && h.prog != nil {
				h.prog.Send(thinkerDoneMsg{task: task, result: result, err: err})
			}
			return
		}

		if h.sess != nil && h.sess.IsActive() {
			if invocationID != "" {
				_ = h.sess.CompleteHandoff(invocationID, map[string]any{"answer": result})
			}
			_ = h.sess.SendThinkerAnswer(result)
		}
		if h.prog != nil {
			h.prog.Send(thinkerDoneMsg{task: task, result: result})
		}
	}()
}
