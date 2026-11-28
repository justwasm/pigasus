// contextview.go — the /debug surface's Context view: how big the context
// really is, and how much of the window it really takes.
//
// # Why this is not /context
//
// /context answers "what is in the prompt" — a breakdown by category, built
// from the host's own accounting, with a built-in figure standing in
// wherever the host cannot measure. It is a *model* of the context.
//
// This view answers a different question: "how big is it, measured". The
// two numbers that matter are both facts rather than estimates — the prompt
// token count the provider itself reported for the last turn, and the byte
// count the recorder actually saw go over the wire — and the view's job is
// to put them next to the window they have to fit in.
//
// # Why every row carries a source
//
// Three different kinds of number meet here and they are not
// interchangeable:
//
//	reported   the provider said so (usage metadata on the response)
//	measured   this process counted it (the recorder's byte count)
//	derived    arithmetic on the two above (a percentage, a ratio)
//	configured the catalogue or the environment said so (the window)
//
// A table that printed them in one column would invite reading a derived
// percentage as a measurement, and an estimate as a fact. So each row ends
// with the word that says which kind it is, and the reader explains where
// the number came from.
//
// # What it deliberately does not do
//
// It does not estimate what it can measure. The local three-bytes-per-token
// estimator appears once, on the "estimate" row, and only to show how far it
// is from the provider's own count — which is the one thing it is good for.
package debugpanel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/tokenfmt"
	"github.com/justwasm/pigasus/internal/core/wirelog"
	"github.com/justwasm/pigasus/palette"
)

// ContextState is the Context view's facts.
//
// The host fills it from two places that answer different questions: the
// session's own token accounting (what the provider reported) and the
// recorder (what actually went over the wire). Either may be absent — a
// session that has not completed a turn has no report, and a session with
// recording off has no exchange — and every row degrades to a dash rather
// than to a zero, because a zero reads as a measurement.
type ContextState struct {
	// Model is the model id the session is talking to.
	Model string

	// WindowTokens is the model's published context window, 0 when the
	// catalogue has no limit for it.
	WindowTokens int

	// AutoCompactTokens is the threshold compaction fires at, 0 when the
	// server decides.
	AutoCompactTokens int
	// AutoCompactSpec is the override that set the threshold, if any.
	AutoCompactSpec string

	// LastPromptTokens, LastCompletionTokens and LastCachedTokens are the
	// last completed turn's usage, as the provider reported it. A cache
	// read is part of the prompt: those tokens occupy the window even
	// though they are not billed as new input.
	LastPromptTokens     int
	LastCompletionTokens int
	LastCachedTokens     int

	// SessionPromptTokens and SessionCompletionTokens are the session's
	// cumulative totals — billing, not occupancy.
	SessionPromptTokens     int
	SessionCompletionTokens int

	// Messages is the conversation's length, and Streaming says whether a
	// turn is in flight (the report then describes the turn before it).
	Messages  int
	Streaming bool

	// Wire is the newest exchange the recorder holds, nil when nothing
	// was recorded. Its byte counts are the only *measured* sizes here.
	Wire *wirelog.Record

	// Compactions is the history of compaction events recorded this session.
	Compactions []compaction.Event

	// Width is the terminal width, for truncating the value column.
	Width int
}

// contextSource is where a row's number came from.
//
// The words are the view's whole honesty story, so they are a type rather
// than three string literals sprinkled through the renderer.
type contextSource string

const (
	sourceReported   contextSource = "reported"
	sourceMeasured   contextSource = "measured"
	sourceDerived    contextSource = "derived"
	sourceConfigured contextSource = "configured"
	sourceNone       contextSource = "—"
)

// contextFact is one row of the table: a measure, its value, and where the
// value came from.
type contextFact struct {
	// Key is the row's stable ID suffix; the reader is asked for "ctx/<key>".
	Key string
	// Label is the measure's name, left-aligned in the label column.
	Label string
	// Value is the row's number, already formatted and styled.
	Value string
	// Source says which kind of number Value is.
	Source contextSource
	// Detail is the provenance paragraph the reader shows. It is plain
	// text, because it is prose rather than a table cell.
	Detail string
}

// contextFacts builds the table's rows.
//
// It is a pure function of the state so the reader and the table cannot
// disagree about a number: the reader is handed the same facts the row was
// built from.
func contextFacts(st ContextState) []contextFact {
	facts := make([]contextFact, 0, 7)

	// 1. Occupancy: the last turn's prompt against the window.
	used := "—"
	if st.LastPromptTokens > 0 {
		used = contextTokens(st.LastPromptTokens)
	}
	window := "unknown"
	if st.WindowTokens > 0 {
		window = contextTokens(st.WindowTokens)
	}
	usedValue := used + palette.DimGrayOpen + " / " + palette.DimGrayClose + window
	if pct, ok := contextPercent(st); ok {
		usedValue += palette.DimGrayOpen + "  ·  " + palette.DimGrayClose + contextPercentStyled(pct)
	}
	facts = append(facts, contextFact{
		Key:    "used",
		Label:  "context",
		Value:  usedValue,
		Source: contextOccupancySource(st),
		Detail: contextUsedDetail(st),
	})

	// 2. The last turn's report, split the way the provider splits it.
	lastValue := "—"
	if st.LastPromptTokens > 0 || st.LastCompletionTokens > 0 {
		lastValue = fmt.Sprintf("%s in%s  %s out",
			contextTokens(st.LastPromptTokens), palette.DimGrayOpen+" · "+palette.DimGrayClose,
			contextTokens(st.LastCompletionTokens))
		if st.LastCachedTokens > 0 {
			lastValue += fmt.Sprintf("%s  %s cached",
				palette.DimGrayOpen+" · "+palette.DimGrayClose, contextTokens(st.LastCachedTokens))
		}
	}
	facts = append(facts, contextFact{
		Key:    "last",
		Label:  "last turn",
		Value:  lastValue,
		Source: contextSourceIf(st.LastPromptTokens > 0 || st.LastCompletionTokens > 0, sourceReported),
		Detail: contextLastDetail(st),
	})

	// 3. The wire: the only measured size in the table.
	wireValue, wireSource := "no exchange recorded", sourceNone
	if st.Wire != nil {
		wireValue = fmt.Sprintf("%s sent%s  %s received",
			HumanBytes64(st.Wire.RequestBytes), palette.DimGrayOpen+" · "+palette.DimGrayClose,
			HumanBytes64(st.Wire.ResponseBytes))
		wireSource = sourceMeasured
	}
	facts = append(facts, contextFact{
		Key:    "wire",
		Label:  "wire",
		Value:  wireValue,
		Source: wireSource,
		Detail: contextWireDetail(st),
	})

	// 4. The estimator's error, which is the only use the estimator has.
	estValue, estSource := "—", sourceNone
	if st.Wire != nil && st.Wire.RequestBytes > 0 && st.LastPromptTokens > 0 {
		est := int(st.Wire.RequestBytes) / compaction.BytesPerToken
		estValue = fmt.Sprintf("%s ÷ %d = %s%s  %s vs %s reported",
			HumanBytes64(st.Wire.RequestBytes), compaction.BytesPerToken,
			contextTokens(est), palette.DimGrayOpen+" · "+palette.DimGrayClose,
			contextTokens(est), contextTokens(st.LastPromptTokens))
		estValue += contextDriftSuffix(est, st.LastPromptTokens)
		estSource = sourceDerived
	}
	facts = append(facts, contextFact{
		Key:    "estimate",
		Label:  "estimate",
		Value:  estValue,
		Source: estSource,
		Detail: contextEstimateDetail(st),
	})

	// 5. The window itself, and where the compaction threshold sits in it.
	windowValue, windowSource := "unknown — the catalogue has no limit for this model", sourceNone
	if st.WindowTokens > 0 {
		windowValue = contextTokens(st.WindowTokens)
		if st.AutoCompactTokens > 0 {
			windowValue += fmt.Sprintf("%s  auto-compact at %s",
				palette.DimGrayOpen+" · "+palette.DimGrayClose, contextTokens(st.AutoCompactTokens))
			if st.AutoCompactSpec != "" {
				windowValue += palette.DimGrayOpen + " (" + st.AutoCompactSpec + ")" + palette.DimGrayClose
			}
		} else {
			windowValue += palette.DimGrayOpen + "  ·  auto-compact at the server's default" + palette.DimGrayClose
		}
		windowSource = sourceConfigured
	}
	facts = append(facts, contextFact{
		Key:    "window",
		Label:  "window",
		Value:  windowValue,
		Source: windowSource,
		Detail: contextWindowDetail(st),
	})

	// 6. The session's cumulative totals: billing, not occupancy. They are
	// here because the two are easy to confuse and the row says which is
	// which.
	sessionValue := "—"
	if st.SessionPromptTokens > 0 || st.SessionCompletionTokens > 0 {
		sessionValue = fmt.Sprintf("%s in%s  %s out%s  %s",
			contextTokens(st.SessionPromptTokens), palette.DimGrayOpen+" · "+palette.DimGrayClose,
			contextTokens(st.SessionCompletionTokens), palette.DimGrayOpen+" · "+palette.DimGrayClose,
			"billed across every turn")
	}
	facts = append(facts, contextFact{
		Key:    "session",
		Label:  "session",
		Value:  sessionValue,
		Source: contextSourceIf(st.SessionPromptTokens > 0 || st.SessionCompletionTokens > 0, sourceReported),
		Detail: contextSessionDetail(st),
	})

	// 7. The conversation the window has to hold.
	convValue := fmt.Sprintf("%d message(s)", st.Messages)
	if st.Streaming {
		convValue += palette.DimGrayOpen + "  ·  a turn is in flight" + palette.DimGrayClose
	}
	facts = append(facts, contextFact{
		Key:    "conversation",
		Label:  "conversation",
		Value:  convValue,
		Source: sourceMeasured,
		Detail: contextConversationDetail(st),
	})

	// 8. Compaction history: when and why the conversation was compacted.
	//
	// The count is measured: the process recorded every event it saw, so
	// "none" is a measurement of the history, not a missing number — which
	// is why this row keeps sourceMeasured when it is empty, like the
	// conversation row.
	//
	// The auto-compact threshold is *not* measured, and it does not go in
	// the value column: a configured number printed beside a measured one
	// is the exact confusion this column exists to prevent. It is in the
	// detail paragraph instead, where a reader is already reading prose.
	compactValue := "none recorded in this session"
	if len(st.Compactions) > 0 {
		saved := 0
		for _, c := range st.Compactions {
			saved += c.SavedTokens()
		}
		compactValue = fmt.Sprintf("%d run(s)%s  %s saved total",
			len(st.Compactions), palette.DimGrayOpen+" · "+palette.DimGrayClose,
			contextTokens(saved))
	}
	facts = append(facts, contextFact{
		Key:    "compactions",
		Label:  "compactions",
		Value:  compactValue,
		Source: sourceMeasured,
		Detail: contextCompactionsDetail(st),
	})

	return facts
}

// contextSourceIf returns src when ok, and the "no number" source when not,
// so a row whose measure is missing never claims a source for it.
func contextSourceIf(ok bool, src contextSource) contextSource {
	if ok {
		return src
	}
	return sourceNone
}

// contextOccupancySource names where the occupancy figure came from: the
// provider's report when there is one, and nothing when there is not —
// occupancy is never inferred from the estimate, because that would make
// the headline number the least trustworthy one in the table.
func contextOccupancySource(st ContextState) contextSource {
	if st.LastPromptTokens > 0 {
		return sourceReported
	}
	return sourceNone
}

// contextPercent is the share of the window the last turn's prompt took.
func contextPercent(st ContextState) (float64, bool) {
	if st.LastPromptTokens <= 0 || st.WindowTokens <= 0 {
		return 0, false
	}
	return float64(st.LastPromptTokens) / float64(st.WindowTokens) * 100, true
}

// contextPercentStyled paints the percentage by how close it is to the
// ceiling.
//
// The thresholds are the ones a reader acts on: below 70% there is room to
// work, past 90% the next long tool result is what triggers compaction.
func contextPercentStyled(pct float64) string {
	text := fmt.Sprintf("%.1f%%", pct)
	switch {
	case pct >= 90:
		return logErrorOpen + text + logColourClose
	case pct >= 70:
		return logRetryOpen + text + logColourClose
	default:
		return logSuccessOpen + text + logColourClose
	}
}

// contextDriftSuffix says how far the estimator is from the report, in the
// direction it is wrong.
func contextDriftSuffix(est, reported int) string {
	if est <= 0 || reported <= 0 {
		return ""
	}
	drift := (float64(reported) - float64(est)) / float64(est) * 100
	colour := logSuccessOpen
	if drift > 10 || drift < -10 {
		colour = logRetryOpen
	}
	return palette.DimGrayOpen + "  (" + colour +
		fmt.Sprintf("%+.1f%%", drift) + logColourClose + palette.DimGrayClose + ")"
}

// contextTokens formats a token count. It is tokenfmt's format, so this
// view and the compaction label print the same number the same way.
func contextTokens(n int) string {
	if n <= 0 {
		return "—"
	}
	return tokenfmt.K(n)
}

// contextColumns are the table's fixed widths, after the cursor.
const (
	ctxLabelWidth  = 12 // "conversation" is the longest label
	ctxSourceWidth = 10 // "configured" is the longest source
	ctxGap         = "  "
)

// ContextRows renders the Context view as a flat table.
//
// Flat, like the Requests view and for the same reason: these are measures,
// not a hierarchy. A row's provenance lives in the reader, where there is
// room to explain it.
func ContextRows(st ContextState) []FoldRow {
	width := st.Width
	if width <= 0 {
		width = 100
	}
	// The value column takes what the fixed columns leave. There is no
	// floor above that: a floor is what made a 40-column terminal render
	// 46-column rows, because the floor was added to columns that were
	// already spoken for. A narrow terminal shrinks the value column and
	// the composed row is truncated at the end, so the label and the source
	// — the two short, fixed columns — survive and the value is what gets
	// cut.
	valueWidth := width - (2 + ctxLabelWidth + len(ctxGap) + len(ctxGap) + ctxSourceWidth)
	if valueWidth < 1 {
		valueWidth = 1
	}

	facts := contextFacts(st)
	rows := make([]FoldRow, 0, len(facts))
	for _, f := range facts {
		value := f.Value
		if ansi.StringWidth(value) > valueWidth {
			value = ansi.Truncate(value, valueWidth, "…")
		}
		line := palette.DimGrayOpen + padRight(f.Label, ctxLabelWidth) + palette.DimGrayClose + ctxGap +
			value + ctxGap + palette.DimGrayOpen + padLeft(string(f.Source), ctxSourceWidth) + palette.DimGrayClose
		// The fixed columns plus a one-cell value still exceed a very narrow
		// terminal; cutting the composed row is what keeps the frame's width
		// promise at every width rather than at the widths that were tried.
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width, "…")
		}
		rows = append(rows, FoldRow{
			ID:   "ctx/" + f.Key,
			Text: line,
			// No Detail, so the row is a leaf and Enter opens the reader on
			// the first press. A Detail here would make Enter unfold an
			// inline excerpt instead — the same lie the Requests view told
			// while its legend said "read", and the reader's body is where
			// the provenance belongs anyway: it is prose, not a table cell.
			Flat: true,
		})
	}
	return rows
}

// ContextHeaderLines renders the view's pinned header: the headline
// occupancy and a bar, because "1.2%" is a number and a bar is a glance.
func ContextHeaderLines(st ContextState) []string {
	head := "\x1b[1m" + palette.SubtleOpen + "└ Context" + "\x1b[0m"
	head += palette.DimGrayOpen
	if st.Model != "" {
		head += "  ·  " + st.Model
	}
	if pct, ok := contextPercent(st); ok {
		head += "  ·  " + contextTokens(st.LastPromptTokens) + " / " + contextTokens(st.WindowTokens) +
			" tokens  ·  " + contextPercentStyled(pct)
	} else if st.WindowTokens > 0 {
		head += "  ·  " + contextTokens(st.WindowTokens) + " tokens  ·  no turn completed yet"
	} else {
		head += "  ·  no measurement yet"
	}
	head += palette.DimGrayClose

	return []string{head, "   " + contextBar(st, 32) + palette.DimGrayClose}
}

// contextBar draws the occupancy as filled and empty cells.
//
// The bar is painted from the same percentage the row prints, so the two
// cannot disagree; a bar that disagreed with its own number is worse than
// no bar.
func contextBar(st ContextState, cells int) string {
	if cells < 8 {
		cells = 8
	}
	pct, ok := contextPercent(st)
	if !ok {
		return palette.DimGrayOpen + strings.Repeat("░", cells) + "  no occupancy to draw"
	}
	filled := int(pct/100*float64(cells) + 0.5)
	if filled > cells {
		filled = cells
	}
	if filled < 0 {
		filled = 0
	}
	colour := logSuccessOpen
	switch {
	case pct >= 90:
		colour = logErrorOpen
	case pct >= 70:
		colour = logRetryOpen
	}
	return colour + strings.Repeat("█", filled) +
		palette.DimGrayOpen + strings.Repeat("░", cells-filled)
}

// ContextLegend is the view's key hint, ordered like the other views so a
// narrow terminal keeps the key that leaves the surface.
func ContextLegend() string {
	return "enter explain  ·  esc close  ·  y copy row  ·  tab next view"
}

// ContextBody is the reader's body for one row: where its number came from,
// then every measurement the view holds.
//
// The second part is deliberate. A provenance paragraph alone asks the
// reader to take the number on trust; printing the whole set next to it
// lets them check the arithmetic — and the set is seven short lines, not a
// document.
func ContextBody(id string, st ContextState) Body {
	key := strings.TrimPrefix(id, "ctx/")
	facts := contextFacts(st)

	var detail string
	title := "context"
	for _, f := range facts {
		if f.Key == key {
			detail = f.Detail
			// The row's label is the title. The occupancy row is the one
			// whose label repeats the view's name, so it gets the word
			// that says what it actually measures: "context · context"
			// would say nothing the bar above it does not.
			if f.Label == "context" {
				title = "context · usage"
			} else {
				title = "context · " + f.Label
			}
			break
		}
	}
	if detail == "" {
		detail = "this measure is not in the current report"
	}

	var all strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&all, "%s  %s  [%s]\n",
			padRight(f.Label, ctxLabelWidth), ansi.Strip(f.Value), f.Source)
	}

	return Body{
		Title: title,
		Parts: []ReaderPart{
			{Label: "where this number comes from", Text: detail},
			{Label: "every measure in this report", Text: strings.TrimRight(all.String(), "\n")},
		},
	}
}

// The provenance paragraphs.
//
// Each one names the mechanism rather than restating the number, so a
// reader who distrusts a figure knows what to go and check.

func contextUsedDetail(st ContextState) string {
	if st.LastPromptTokens <= 0 {
		return strings.Join([]string{
			"No turn has completed in this session, so no provider has reported a",
			"prompt token count yet. The occupancy figure is deliberately left blank",
			"rather than estimated: an estimate here would make the headline number",
			"the least trustworthy one in the table.",
			"",
			"Send a message and this row fills in from the response's usage metadata.",
		}, "\n")
	}
	lines := []string{
		fmt.Sprintf("The provider reported %s prompt tokens for the last completed turn.",
			contextTokens(st.LastPromptTokens)),
		"",
		"Every turn resends the whole conversation, so the prompt token count of the",
		"latest turn *is* what currently occupies the window — it is not a running",
		"total. That is why this row reads the last turn and not the session total.",
	}
	if st.Streaming {
		lines = append(lines, "",
			"A turn is in flight, so this describes the turn before it. The number",
			"moves when the response arrives, not while it streams.")
	}
	if st.LastCachedTokens > 0 {
		lines = append(lines, "",
			fmt.Sprintf("Of those, %s were cache reads. They still occupy the window;",
				contextTokens(st.LastCachedTokens)),
			"they are only cheaper to send.")
	}
	return strings.Join(lines, "\n")
}

func contextLastDetail(st ContextState) string {
	if st.LastPromptTokens <= 0 && st.LastCompletionTokens <= 0 {
		return "No completed turn yet: the provider has not reported usage."
	}
	return strings.Join([]string{
		"The last completed turn's usage, exactly as the response's usage metadata",
		"reported it — in, out, and (when the provider breaks it out) the cache",
		"reads. Nothing here is computed by this process.",
		"",
		"Different backends name these fields differently (CloudCode's",
		"usageMetadata, an OpenAI-shaped gateway's prompt_tokens, an",
		"Anthropic-shaped one's input_tokens) and the transports already map them",
		"onto one ChunkMeta, so this row is the same three numbers whichever",
		"dialect the session is speaking.",
	}, "\n")
}

func contextWireDetail(st ContextState) string {
	if st.Wire == nil {
		return strings.Join([]string{
			"No exchange is in the recorder.",
			"",
			"That is either a session that has not made a request yet, or a build with",
			"recording off. The Requests view shows the same ring; if it is empty too,",
			"nothing has been recorded.",
		}, "\n")
	}
	r := st.Wire
	lines := []string{
		fmt.Sprintf("#%d %s %s", r.Seq, r.Method, r.Path),
		"",
		"This is the only measured size in the table. The recorder wraps the HTTP",
		"transport, so these are the bytes the process actually put on the socket:",
		"request body and response body, counted as they flowed.",
		"",
		fmt.Sprintf("request body    %s", HumanBytes64(r.RequestBytes)),
		fmt.Sprintf("response body   %s", HumanBytes64(r.ResponseBytes)),
	}
	if r.Status != 0 {
		lines = append(lines, fmt.Sprintf("status          %d", r.Status))
	}
	if r.TTFB > 0 {
		lines = append(lines, fmt.Sprintf("first byte      %s", formatDuration(r.TTFB)))
	}
	if r.Duration > 0 {
		lines = append(lines, fmt.Sprintf("total           %s", formatDuration(r.Duration)))
	}
	lines = append(lines, "",
		"The body is capped at 128 KiB per side, so a very large request reports the",
		"real size in this row and a truncated copy in the Requests reader. The size",
		"here is the real one.")
	return strings.Join(lines, "\n")
}

func contextEstimateDetail(st ContextState) string {
	if st.Wire == nil || st.LastPromptTokens <= 0 {
		return strings.Join([]string{
			"The estimator needs both a recorded request and a provider report to",
			"compare, and this session is missing at least one of them.",
		}, "\n")
	}
	return strings.Join([]string{
		fmt.Sprintf("The tree's local estimator is %d bytes per token — the divisor the",
			compaction.BytesPerToken),
		"reference binary itself uses. It is cheap and it is approximate, and the",
		"only honest use for it is exactly this row: measuring its own error against",
		"the provider's count.",
		"",
		"The request body is not the prompt alone — it carries the JSON envelope, the",
		"tool declarations and the generation config — so a few percent of this drift",
		"is structure rather than tokenisation. A drift that stays large and",
		"one-directional is tokenisation; one that moves with the request size is",
		"envelope overhead.",
	}, "\n")
}

func contextWindowDetail(st ContextState) string {
	if st.WindowTokens <= 0 {
		return strings.Join([]string{
			"The model catalogue has no published input limit for this model id, so",
			"the window is unknown rather than assumed.",
			"",
			"Set one with /model (a configured provider carries its own context window)",
			"or pick a catalogue model, and this row fills in.",
		}, "\n")
	}
	lines := []string{
		fmt.Sprintf("%s is the model's published input limit, from the catalogue.",
			contextTokens(st.WindowTokens)),
		"",
		"It is configuration, not measurement: the endpoint is free to serve a",
		"different window, and a configured provider may declare its own.",
	}
	if st.AutoCompactTokens > 0 {
		lines = append(lines, "",
			fmt.Sprintf("Compaction fires at %s of it", contextTokens(st.AutoCompactTokens)))
		if st.AutoCompactSpec != "" {
			lines[len(lines)-1] += fmt.Sprintf(" (override: %s).", st.AutoCompactSpec)
		} else {
			lines[len(lines)-1] += ", from this model's configured threshold."
		}
	} else {
		lines = append(lines, "",
			"No compaction threshold is configured, so the server decides when to",
			"compact.")
	}
	return strings.Join(lines, "\n")
}

func contextSessionDetail(st ContextState) string {
	return strings.Join([]string{
		"Every turn's prompt and completion tokens, added up across the session.",
		"",
		"This is the billing figure. It is *not* the occupancy figure and it grows",
		"without bound: after twenty turns of a 12k-token conversation it reads 240k",
		"even though the window still holds 12k. The `context` row is the one that",
		"answers \"how full is it\".",
	}, "\n")
}

func contextConversationDetail(st ContextState) string {
	lines := []string{
		fmt.Sprintf("The conversation is %d message(s) long.", st.Messages),
		"",
		"Messages are what the prompt is built from: the system instruction, the",
		"tool declarations and the generation config are added on top, which is why",
		"the prompt token count exceeds anything the messages alone would suggest.",
	}
	if st.Streaming {
		lines = append(lines, "", "A turn is in flight right now.")
	}
	return strings.Join(lines, "\n")
}

func contextCompactionsDetail(st ContextState) string {
	if len(st.Compactions) == 0 {
		lines := []string{"No compaction events recorded in this session."}
		if st.AutoCompactTokens > 0 {
			lines = append(lines, "", fmt.Sprintf("Auto-compaction is configured to fire at %s tokens.", contextTokens(st.AutoCompactTokens)))
		}
		return strings.Join(lines, "\n")
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("%d compaction(s) occurred in this session:", len(st.Compactions)), "")
	for i := len(st.Compactions) - 1; i >= 0; i-- {
		ev := st.Compactions[i]
		ts := ev.Timestamp.Format("15:04:05")
		durStr := ""
		if ev.Duration > 0 {
			durStr = fmt.Sprintf(" (took %s)", formatDuration(ev.Duration))
		}
		lines = append(lines, fmt.Sprintf("#%d  %s  reason: %s%s", ev.Seq, ts, ev.Reason, durStr))
		lines = append(lines, fmt.Sprintf("    tokens: %s → %s (-%.1f%%, saved %s)",
			contextTokens(ev.TokensBefore), contextTokens(ev.TokensAfter),
			ev.PercentReduced(), contextTokens(ev.SavedTokens())))
		if ev.Summary != "" {
			lines = append(lines, fmt.Sprintf("    summary: %s", ev.Summary))
		}
		lines = append(lines, "")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
