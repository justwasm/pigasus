// engine_plan.go — integration with execution plans from external agent turn engines.
//
// When connected to an external or remote turn engine (e.g., ACP, Claude, Codex),
// the engine may maintain and expose an execution plan describing multi-step progress.
// This file discovers whether the active engine implements PlanLister (or duck-typed
// Plan() via reflection) and formats a concise progress string for the TUI status line,
// tasks indicator, and telemetry payloads.

package cliapp

import (
	"fmt"
	"reflect"
	"strings"
)

// PlanEntry represents an item in an agent engine's execution plan.
type PlanEntry struct {
	Title     string `json:"title"`
	Status    string `json:"status,omitempty"` // e.g. "completed", "in_progress", "pending"
	Completed bool   `json:"completed,omitempty"`
}

// PlanLister is an optional interface an agentturn.Engine may implement to expose
// its execution plan.
type PlanLister interface {
	Plan() []PlanEntry
}

// enginePlan returns a concise plan status string (e.g. "Plan: 1/3 completed")
// if the active engine implements PlanLister or exposes a Plan() method.
// Returns "" if no plan is available.
func (m *runMode) enginePlan() string {
	if m == nil {
		return ""
	}
	eng := m.turnEngine()
	if eng == nil {
		return ""
	}
	return formatEnginePlan(eng)
}

// formatEnginePlan formats plan progress from any engine implementing PlanLister
// or a duck-typed Plan() method returning a slice of entries or status strings.
func formatEnginePlan(eng any) string {
	if eng == nil {
		return ""
	}

	// 1. Direct type assertion.
	if pl, ok := eng.(PlanLister); ok {
		return formatPlanEntries(pl.Plan())
	}

	// 2. Reflection fallback for duck-typed Plan() methods on third-party engines.
	val := reflect.ValueOf(eng)
	if !val.IsValid() {
		return ""
	}
	method := val.MethodByName("Plan")
	if !method.IsValid() || method.Type().NumIn() != 0 || method.Type().NumOut() < 1 {
		return ""
	}

	res := method.Call(nil)
	if len(res) == 0 || !res[0].IsValid() {
		return ""
	}
	if len(res) > 1 && !res[1].IsNil() {
		return ""
	}

	out := res[0]
	switch out.Kind() {
	case reflect.Slice:
		total := out.Len()
		if total == 0 {
			return ""
		}
		completed := 0
		for i := 0; i < total; i++ {
			elem := out.Index(i)
			if elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Interface {
				if elem.IsNil() {
					continue
				}
				elem = elem.Elem()
			}
			if isElemCompleted(elem) {
				completed++
			}
		}
		return fmt.Sprintf("Plan: %d/%d completed", completed, total)
	case reflect.String:
		return strings.TrimSpace(out.String())
	default:
		return ""
	}
}

func formatPlanEntries(entries []PlanEntry) string {
	if len(entries) == 0 {
		return ""
	}
	completed := 0
	for _, e := range entries {
		if isEntryCompleted(e) {
			completed++
		}
	}
	return fmt.Sprintf("Plan: %d/%d completed", completed, len(entries))
}

func isEntryCompleted(e PlanEntry) bool {
	if e.Completed {
		return true
	}
	return isStatusCompleted(strings.ToLower(strings.TrimSpace(e.Status)))
}

func isElemCompleted(elem reflect.Value) bool {
	if !elem.IsValid() {
		return false
	}
	if elem.Kind() == reflect.Struct {
		if cf := elem.FieldByName("Completed"); cf.IsValid() && cf.Kind() == reflect.Bool && cf.Bool() {
			return true
		}
		if sf := elem.FieldByName("Status"); sf.IsValid() && sf.Kind() == reflect.String {
			if isStatusCompleted(strings.ToLower(strings.TrimSpace(sf.String()))) {
				return true
			}
		}
		if tf := elem.FieldByName("Title"); tf.IsValid() && tf.Kind() == reflect.String {
			t := strings.TrimSpace(tf.String())
			if strings.HasPrefix(t, "[x]") || strings.HasPrefix(t, "[X]") {
				return true
			}
		}
		return false
	}
	if elem.Kind() == reflect.String {
		s := strings.ToLower(elem.String())
		return strings.Contains(s, "completed") || strings.Contains(s, "done") || strings.HasPrefix(s, "[x]")
	}
	return false
}

func isStatusCompleted(status string) bool {
	switch status {
	case "completed", "done", "finished", "complete", "success":
		return true
	default:
		return false
	}
}
