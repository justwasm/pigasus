package hooks

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DefaultTimeoutSeconds is the timeout a handler without a `timeout` of
// its own gets. The documentation names 30 seconds.
const DefaultTimeoutSeconds = 30

// Handler is one entry of a `hooks` array.
//
// The documentation names three fields: `type` (only "command" is
// supported, and it is the default), `command` (the shell command to
// execute, and the only required one) and `timeout` (in seconds).
type Handler struct {
	// Type is the handler's type. Only "command" is supported.
	Type string
	// Command is the shell command the handler runs.
	Command string
	// Timeout is the handler's timeout in seconds. Zero means the
	// default (DefaultTimeoutSeconds).
	Timeout int
}

// TimeoutSeconds returns the handler's timeout in seconds, falling back to
// the documented default.
func (h Handler) TimeoutSeconds() int {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return DefaultTimeoutSeconds
}

// Entry is one handler bound to one event.
//
// The tool events carry a matcher that selects the tool the handler
// fires for; the lifecycle events ignore theirs.
type Entry struct {
	// Hook is the name of the hook the entry came from. The legacy flat
	// shape has no names, and its entries carry "hooks".
	Hook string
	// Event is the event the entry fires on.
	Event Event
	// Matcher is the tool-name pattern of a tool event's group. An empty
	// matcher matches every tool.
	Matcher string
	// Handler is the command the entry runs.
	Handler Handler
	// PluginRoot is the directory of the plugin defining this hook (if any).
	PluginRoot string
	// PluginData is the directory for plugin-specific persistent data (if any).
	PluginData string
}

// Problem is one thing a document got wrong.
//
// The loader skips the offending entry and keeps the rest of the
// document: a broken hook does not take the other hooks with it.
type Problem struct {
	// Path is the document the problem was found in.
	Path string
	// Reason says what is wrong with it.
	Reason string
}

// Registry is what a loader read: the entries of the documents, the
// documents it read and the problems it found.
type Registry struct {
	// Entries are the handlers of every document, in the order the
	// documents were read.
	Entries []Entry
	// Sources are the documents that were read.
	Sources []string
	// Problems are the entries that could not be loaded.
	Problems []Problem
}

// Match returns the entries that fire for ev.
//
// For the two tool events the matcher decides: an empty matcher or "*"
// matches every tool, and any other matcher is a regular expression over
// the tool's name. The expression is anchored, so "run_command" matches
// exactly that name and "browser_.*" matches every browser tool.
//
// For the four lifecycle events every entry of the event matches.
func (r *Registry) Match(ev Event, tool string) []Entry {
	if r == nil {
		return nil
	}
	var out []Entry
	for _, e := range r.Entries {
		if e.Event != ev {
			continue
		}
		if ev.UsesMatcher() && !matchesTool(e.Matcher, tool) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Len returns the number of entries the registry holds.
func (r *Registry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.Entries)
}

// matchesTool reports whether a matcher selects the tool.
//
// The matcher is a regular expression over the tool's name. It is
// anchored, because the documentation says "run_command" matches exactly
// that tool.
func matchesTool(matcher, tool string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	re, err := regexp.Compile("^(?:" + matcher + ")$")
	if err != nil {
		return false
	}
	return re.MatchString(tool)
}

// validMatcher reports whether a matcher is a regular expression.
func validMatcher(matcher string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	_, err := regexp.Compile("^(?:" + matcher + ")$")
	return err == nil
}

// Parse reads a hooks.json document.
//
// Three shapes are accepted:
//
//   - the documented one, where the document maps a hook's name onto the
//     hook's event configuration:
//
//     {"my-linter": {"PostToolUse": [{"matcher": "run_command",
//     "hooks": [{"type": "command", "command": "./lint.sh"}]}]}}
//
//   - the Claude Code shape, where the events sit under one "hooks" key.
//     The reference loads it as a single hook named "hooks", and so does
//     this parser.
//
//   - the legacy flat shape, where a "hooks" array lists the handlers
//     with their event:
//
//     {"hooks": [{"event": "PreToolUse", "command": "./lint.sh"}]}
//
// The entries the document carries come back with the reasons the
// document could not be used: a handler without a command, a handler type
// the documentation does not name, an event that is not one of the six, a
// matcher that is not a regular expression. The offending entry is
// skipped — the rest of the document still loads.
func Parse(raw []byte) ([]Entry, []string) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, []string{"failed to parse hooks.json: " + err.Error()}
	}
	var entries []Entry
	var problems []string
	for _, name := range sortedKeys(top) {
		value := top[name]
		if name == "hooks" && isJSONArray(value) {
			e, p := parseFlatList(value)
			entries = append(entries, e...)
			problems = append(problems, p...)
			continue
		}
		e, p := parseHookSpec(name, value)
		entries = append(entries, e...)
		problems = append(problems, p...)
	}
	return entries, problems
}

// parseHookSpec reads one hook's specification: its `enabled` flag and
// the handlers of each of the six events.
func parseHookSpec(name string, raw json.RawMessage) ([]Entry, []string) {
	var spec map[string]json.RawMessage
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, []string{"hook " + strconv.Quote(name) + ": failed to parse: " + err.Error()}
	}
	var problems []string
	if rawEnabled, ok := spec["enabled"]; ok {
		var enabled bool
		if err := json.Unmarshal(rawEnabled, &enabled); err != nil {
			problems = append(problems, "hook "+strconv.Quote(name)+": enabled must be a boolean")
		} else if !enabled {
			// A disabled hook is not an error: it is a hook the user
			// turned off without removing.
			return nil, problems
		}
	}
	var entries []Entry
	for _, ev := range Events() {
		rawEvent, ok := spec[string(ev)]
		if !ok {
			continue
		}
		e, p := parseEventEntries(name, ev, rawEvent)
		entries = append(entries, e...)
		problems = append(problems, p...)
	}
	return entries, problems
}

// parseEventEntries reads the array under one event key.
//
// An item is either a group — a matcher and the handlers under a "hooks"
// key — or a handler itself, which is the shape the lifecycle events
// use.
func parseEventEntries(name string, ev Event, raw json.RawMessage) ([]Entry, []string) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []string{"hook " + strconv.Quote(name) + ": " + string(ev) +
			": failed to parse: " + err.Error()}
	}
	var entries []Entry
	var problems []string
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			problems = append(problems, "hook "+strconv.Quote(name)+": "+string(ev)+
				": failed to parse: "+err.Error())
			continue
		}
		inner, isGroup := obj["hooks"]
		if !isGroup {
			h, problem := parseHandler(name, ev, "", obj, "")
			if problem != "" {
				problems = append(problems, problem)
				continue
			}
			entries = append(entries, Entry{Hook: name, Event: ev, Handler: h})
			continue
		}
		matcher := ""
		if rawMatcher, ok := obj["matcher"]; ok {
			if err := json.Unmarshal(rawMatcher, &matcher); err != nil {
				problems = append(problems, "hook "+strconv.Quote(name)+": "+string(ev)+
					": matcher must be a string")
				continue
			}
		}
		if !validMatcher(matcher) {
			problems = append(problems, "hook "+strconv.Quote(name)+": "+string(ev)+
				": matcher "+strconv.Quote(matcher)+" is not a regular expression")
			continue
		}
		e, p := parseHandlers(name, ev, matcher, inner)
		entries = append(entries, e...)
		problems = append(problems, p...)
	}
	return entries, problems
}

// parseHandlers reads the handlers of one group.
func parseHandlers(name string, ev Event, matcher string, raw json.RawMessage) ([]Entry, []string) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []string{"hook " + strconv.Quote(name) + ": " + string(ev) +
			": failed to parse: " + err.Error()}
	}
	var entries []Entry
	var problems []string
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			problems = append(problems, "hook "+strconv.Quote(name)+": "+string(ev)+
				": failed to parse: "+err.Error())
			continue
		}
		h, problem := parseHandler(name, ev, matcher, obj, "")
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		entries = append(entries, Entry{Hook: name, Event: ev, Matcher: matcher, Handler: h})
	}
	return entries, problems
}

// parseHandler reads one handler.
//
// The handlerType argument carries a type the caller has already read —
// the legacy flat shape uses the `type` key for the event's name, and
// the handler's own type is then unknown.
func parseHandler(name string, ev Event, matcher string, obj map[string]json.RawMessage, handlerType string) (Handler, string) {
	h := Handler{Type: "command"}
	if handlerType != "" {
		h.Type = handlerType
	} else if raw, ok := obj["type"]; ok {
		var t string
		if err := json.Unmarshal(raw, &t); err == nil {
			h.Type = t
		}
	}
	if raw, ok := obj["command"]; ok {
		var c string
		if err := json.Unmarshal(raw, &c); err == nil {
			h.Command = c
		}
	}
	if raw, ok := obj["timeout"]; ok {
		var t int
		if err := json.Unmarshal(raw, &t); err == nil {
			h.Timeout = t
		}
	}
	label := "hook"
	if name != "" {
		label = "hook " + strconv.Quote(name)
	}
	if h.Command == "" {
		return h, label + ": " + string(ev) + ": command hook must specify 'command'"
	}
	if h.Type != "command" {
		return h, label + ": " + string(ev) + ": unsupported hook type " + strconv.Quote(h.Type)
	}
	return h, ""
}

// parseFlatList reads the legacy flat shape: a "hooks" array whose items
// name their event.
func parseFlatList(raw json.RawMessage) ([]Entry, []string) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []string{"failed to parse hooks.json: " + err.Error()}
	}
	var entries []Entry
	var problems []string
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			problems = append(problems, "hook \"hooks\": failed to parse: "+err.Error())
			continue
		}
		eventName := ""
		usedType := false
		for _, key := range []string{"event", "type"} {
			rawName, ok := obj[key]
			if !ok {
				continue
			}
			var s string
			if err := json.Unmarshal(rawName, &s); err != nil {
				continue
			}
			if _, ok := ParseEvent(s); !ok {
				continue
			}
			eventName = s
			usedType = key == "type"
			break
		}
		ev, ok := ParseEvent(eventName)
		if !ok {
			problems = append(problems, "hook \"hooks\": unknown event "+
				strconv.Quote(eventName)+" is ignored")
			continue
		}
		matcher := ""
		if rawMatcher, ok := obj["matcher"]; ok {
			if err := json.Unmarshal(rawMatcher, &matcher); err == nil &&
				!validMatcher(matcher) {
				problems = append(problems, "hook \"hooks\": "+string(ev)+
					": matcher "+strconv.Quote(matcher)+" is not a regular expression")
				continue
			}
		}
		handlerType := ""
		if usedType {
			// The `type` key named the event, so the handler's own
			// type is the default.
			handlerType = "command"
		}
		h, problem := parseHandler("hooks", ev, matcher, obj, handlerType)
		if problem != "" {
			problems = append(problems, problem)
			continue
		}
		entries = append(entries, Entry{Hook: "hooks", Event: ev, Matcher: matcher, Handler: h})
	}
	return entries, problems
}

// isJSONArray reports whether a raw value is a JSON array.
func isJSONArray(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trimmed, "[")
}

// sortedKeys returns a map's keys in a stable order, so a document loads
// the same way every time.
func sortedKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
