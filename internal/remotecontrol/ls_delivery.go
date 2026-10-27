// Package remotecontrol — ls_delivery.go: the delivery strategy the
// Web UI's message request carries.
//
// A message that arrives while the agent is working does not start a
// second turn: the reference decides what to do with it from the
// strategy the request carries — and while the agent is working, the
// reference *waits*, whatever the strategy says.
//
// 【一手】 2026-10-26, from the reference's own Web UI bundle
// (internal/rcfrontend/assets/main.js) — the composer sends the
// strategy with the message, and the frontend reads it back off the
// server's pending messages to decide whether the message it sent is
// still waiting:
//
//	function RSa(a,b){return a===void 0?!1:b===V.MessageDeliveryStrategy.NEXT_INVOCATION?a.executorLoopStatus===V.CascadeRunStatus.RUNNING:a.status!==V.CascadeRunStatus.IDLE||!a.fullyIdle}
//
// The predicate is the reference's own queue rule: a NEXT_INVOCATION
// message is *pending* exactly while the executor loop is RUNNING.
//
// 【一手】 the reference's own setting names the two modes: "Queue until
// after the current turn." (WHEN_IDLE) and "Interrupt the agent and
// send immediately." (NEXT_INVOCATION).
//
// 【反编译】 the enum is exa.cortex_pb.MessageDeliveryStrategy, decoded
// from the FileDescriptorProto set embedded in the same bundle:
//
//	MESSAGE_DELIVERY_STRATEGY_UNSPECIFIED     = 0
//	MESSAGE_DELIVERY_STRATEGY_NEXT_INVOCATION = 1
//	MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE       = 2
//
// 【反编译】 2026-10-26, the reference's IDE language server — the
// executor's own rule, in
// agentexecutor.(*AgentExecutor).scheduleUserCascadeMessageRequest
// (0x101e35620):
//
//	queue = (executorLoopStatus == RUNNING) || (deliveryStrategy == WHEN_IDLE && !fullyIdle)
//
// So the setting's copy overstates the implementation: while the
// executor loop is RUNNING *every* message waits, NEXT_INVOCATION
// included. The reference never interrupts a running turn — it queues
// the message and delivers it when the turn ends.
//
// UNSPECIFIED is treated like WHEN_IDLE: the reference's queue is the
// default, and a message that does not say what it wants waits.
package remotecontrol

import (
	"strconv"
	"strings"
)

// lsDeliveryStrategy is exa.cortex_pb.MessageDeliveryStrategy.
type lsDeliveryStrategy int

const (
	// lsDeliveryUnspecified is MESSAGE_DELIVERY_STRATEGY_UNSPECIFIED.
	// The reference queues it like WHEN_IDLE.
	lsDeliveryUnspecified lsDeliveryStrategy = 0
	// lsDeliveryNextInvocation is
	// MESSAGE_DELIVERY_STRATEGY_NEXT_INVOCATION: send the message at
	// the next invocation. While the executor loop is running the
	// message waits like any other — the reference's setting copy
	// ("Interrupt the agent and send immediately.") promises more than
	// the reference's executor delivers.
	lsDeliveryNextInvocation lsDeliveryStrategy = 1
	// lsDeliveryWhenIdle is MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE: wait
	// until the current turn ends.
	lsDeliveryWhenIdle lsDeliveryStrategy = 2
)

// UnmarshalJSON reads the strategy from the request. proto-JSON spells
// an enum by its name ("MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE"), but the
// frontend sends the numeric value — its own optimistic copy puts the
// numeric enum in the request — so a number, a numeric string and the
// name are all accepted.
func (s *lsDeliveryStrategy) UnmarshalJSON(b []byte) error {
	text := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if text == "" || text == "null" {
		*s = lsDeliveryUnspecified
		return nil
	}
	if n, err := strconv.Atoi(text); err == nil {
		*s = lsDeliveryStrategy(n)
		return nil
	}
	*s = lsDeliveryStrategyFromName(text)
	return nil
}

// lsDeliveryStrategyFromName maps the enum's spellings onto its
// values. An unknown name is UNSPECIFIED — the queue — because a
// message that does not say what it wants waits rather than takes the
// turn away from the work in flight.
func lsDeliveryStrategyFromName(name string) lsDeliveryStrategy {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "MESSAGE_DELIVERY_STRATEGY_NEXT_INVOCATION", "NEXT_INVOCATION":
		return lsDeliveryNextInvocation
	case "MESSAGE_DELIVERY_STRATEGY_WHEN_IDLE", "WHEN_IDLE":
		return lsDeliveryWhenIdle
	default:
		return lsDeliveryUnspecified
	}
}

// lsDeliveryStrategyName is the enum's name — the spelling proto-JSON
// uses. The clone sends the number (like every other enum it puts on
// the wire), so this is only for the logs.
func lsDeliveryStrategyName(s lsDeliveryStrategy) string {
	switch s {
	case lsDeliveryNextInvocation:
		return "NEXT_INVOCATION"
	case lsDeliveryWhenIdle:
		return "WHEN_IDLE"
	default:
		return "UNSPECIFIED"
	}
}
