package actor

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// MessageType classifies actor messages.
//
// In-band (Mailbox) Messages:
// Messages posted to the Actor's mailbox (Send / SendSync) are processed sequentially
// by the background run loop. Turn-initiating messages (UserMessage, ScheduledWakeup)
// execute full turns one after another.
//
// Out-of-band Interventions:
// For active turn interventions (Steer, CancelTurn, Approve), in-process callers
// should invoke TurnController / Actor methods directly so they take effect concurrently
// and immediately without waiting for the mailbox queue. The message structs
// (SteerMessage, CancelSignal, ApprovalDecision) are preserved for wire protocols,
// IPC, and remote agent dispatch.
type MessageType string

const (
	// In-band mailbox messages (turn initiators & configuration)
	MessageTypeUser            MessageType = "user"
	MessageTypeScheduledWakeup MessageType = "scheduled_wakeup"
	MessageTypeSystemPrompt    MessageType = "system_prompt"
	MessageTypeCustom          MessageType = "custom"

	// Out-of-band turn intervention messages (IPC / wire representations)
	MessageTypeSteer            MessageType = "steer"
	MessageTypeCancel           MessageType = "cancel"
	MessageTypeApprovalDecision MessageType = "approval_decision"
)

// Message is the interface implemented by all messages that can be posted
// to an Actor's mailbox or serialized across process boundaries.
type Message interface {
	MessageType() MessageType
	ID() string
	Timestamp() time.Time
}

// IsTurnInitiator returns whether a message starts a new execution turn
// when processed sequentially from the mailbox.
func IsTurnInitiator(m Message) bool {
	if m == nil {
		return false
	}
	switch m.MessageType() {
	case MessageTypeUser, MessageTypeScheduledWakeup:
		return true
	default:
		return false
	}
}

// Attachment carries supplemental media or file data alongside a message.
type Attachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	URI      string `json:"uri,omitempty"`
	Content  []byte `json:"content,omitempty"`
}

// UserMessage represents an explicit turn request from a human or client.
type UserMessage struct {
	id          string
	timestamp   time.Time
	Text        string         `json:"text"`
	Attachments []Attachment   `json:"attachments,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func NewUserMessage(text string) *UserMessage {
	return &UserMessage{
		id:        newID("msg_usr"),
		timestamp: time.Now(),
		Text:      text,
		Metadata:  make(map[string]any),
	}
}

func (m *UserMessage) MessageType() MessageType { return MessageTypeUser }
func (m *UserMessage) ID() string               { return m.id }
func (m *UserMessage) Timestamp() time.Time     { return m.timestamp }

// ScheduledWakeup is delivered when a timer or recurring cron schedule triggers.
type ScheduledWakeup struct {
	id         string
	timestamp  time.Time
	ScheduleID string         `json:"schedule_id"`
	Prompt     string         `json:"prompt"`
	Source     string         `json:"source"` // "timer" or "cron"
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func NewScheduledWakeup(scheduleID, prompt, source string) *ScheduledWakeup {
	return &ScheduledWakeup{
		id:         newID("msg_sch"),
		timestamp:  time.Now(),
		ScheduleID: scheduleID,
		Prompt:     prompt,
		Source:     source,
		Metadata:   make(map[string]any),
	}
}

func (m *ScheduledWakeup) MessageType() MessageType { return MessageTypeScheduledWakeup }
func (m *ScheduledWakeup) ID() string               { return m.id }
func (m *ScheduledWakeup) Timestamp() time.Time     { return m.timestamp }

// ApprovalDecision is delivered in response to an EventApprovalRequired.
//
// In-process callers should prefer calling Actor.Approve(...) or TurnController.Approve(...)
// directly so that authorization is delivered immediately to the waiting tool execution.
// ApprovalDecision is used when serializing approvals across IPC, network, or mailbox channels.
type ApprovalDecision struct {
	id         string
	timestamp  time.Time
	ToolCallID string `json:"tool_call_id"`
	Approved   bool   `json:"approved"`
	Feedback   string `json:"feedback,omitempty"`
}

func NewApprovalDecision(toolCallID string, approved bool, feedback string) *ApprovalDecision {
	return &ApprovalDecision{
		id:         newID("msg_app"),
		timestamp:  time.Now(),
		ToolCallID: toolCallID,
		Approved:   approved,
		Feedback:   feedback,
	}
}

func (m *ApprovalDecision) MessageType() MessageType { return MessageTypeApprovalDecision }
func (m *ApprovalDecision) ID() string               { return m.id }
func (m *ApprovalDecision) Timestamp() time.Time     { return m.timestamp }

// SteerMessage injects dynamic instructions into the agent.
//
// In-process callers should prefer calling Actor.Steer(...) or TurnController.Steer(...)
// directly so guidance is applied concurrently without waiting for mailbox processing.
// SteerMessage is used when transmitting steering guidance over IPC, network, or mailbox channels.
type SteerMessage struct {
	id          string
	timestamp   time.Time
	Instruction string `json:"instruction"`
}

func NewSteerMessage(instruction string) *SteerMessage {
	return &SteerMessage{
		id:          newID("msg_str"),
		timestamp:   time.Now(),
		Instruction: instruction,
	}
}

func (m *SteerMessage) MessageType() MessageType { return MessageTypeSteer }
func (m *SteerMessage) ID() string               { return m.id }
func (m *SteerMessage) Timestamp() time.Time     { return m.timestamp }

// CancelSignal requests cancellation of an in-flight turn or all processing.
//
// In-process callers should prefer calling Actor.CancelTurn(...) or TurnController.CancelTurn(...)
// directly so cancellation is triggered immediately out-of-band.
// CancelSignal is used when transmitting cancellation signals over IPC, network, or mailbox channels.
type CancelSignal struct {
	id        string
	timestamp time.Time
	TurnID    string `json:"turn_id,omitempty"` // If empty, cancels active turn
	Reason    string `json:"reason,omitempty"`
}

func NewCancelSignal(turnID, reason string) *CancelSignal {
	return &CancelSignal{
		id:        newID("msg_cnc"),
		timestamp: time.Now(),
		TurnID:    turnID,
		Reason:    reason,
	}
}

func (m *CancelSignal) MessageType() MessageType { return MessageTypeCancel }
func (m *CancelSignal) ID() string               { return m.id }
func (m *CancelSignal) Timestamp() time.Time     { return m.timestamp }

// SystemPromptUpdate dynamically modifies the agent's base instructions.
type SystemPromptUpdate struct {
	id           string
	timestamp    time.Time
	SystemPrompt string `json:"system_prompt"`
}

func NewSystemPromptUpdate(prompt string) *SystemPromptUpdate {
	return &SystemPromptUpdate{
		id:           newID("msg_sys"),
		timestamp:    time.Now(),
		SystemPrompt: prompt,
	}
}

func (m *SystemPromptUpdate) MessageType() MessageType { return MessageTypeSystemPrompt }
func (m *SystemPromptUpdate) ID() string               { return m.id }
func (m *SystemPromptUpdate) Timestamp() time.Time     { return m.timestamp }

// CustomMessage carries arbitrary client-defined payload.
type CustomMessage struct {
	id        string
	timestamp time.Time
	Kind      string `json:"kind"`
	Payload   any    `json:"payload"`
}

func NewCustomMessage(kind string, payload any) *CustomMessage {
	return &CustomMessage{
		id:        newID("msg_cst"),
		timestamp: time.Now(),
		Kind:      kind,
		Payload:   payload,
	}
}

func (m *CustomMessage) MessageType() MessageType { return MessageTypeCustom }
func (m *CustomMessage) ID() string               { return m.id }
func (m *CustomMessage) Timestamp() time.Time     { return m.timestamp }

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
