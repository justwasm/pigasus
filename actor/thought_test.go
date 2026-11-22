package actor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func TestThoughtNormalizer_GeminiNative(t *testing.T) {
	norm := actor.NewThoughtNormalizer()
	chunks := norm.Feed(actor.StreamChunk{
		Thought: "Analyzing user input...",
	})
	chunks = append(chunks, norm.Flush()...)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Thought != "Analyzing user input..." {
		t.Fatalf("unexpected thought: %s", chunks[0].Thought)
	}
	if chunks[0].ThoughtEvent == nil {
		t.Fatal("expected non-nil ThoughtEvent")
	}
	if chunks[0].ThoughtEvent.Source != actor.ThoughtSourceGemini {
		t.Fatalf("expected source gemini, got %s", chunks[0].ThoughtEvent.Source)
	}
	if chunks[0].ThoughtEvent.Content != "Analyzing user input..." {
		t.Fatalf("expected content 'Analyzing user input...', got %s", chunks[0].ThoughtEvent.Content)
	}
}

func TestThoughtNormalizer_ClaudeThinkingBlock(t *testing.T) {
	norm := actor.NewThoughtNormalizer()
	chunks := norm.Feed(actor.StreamChunk{
		Thought: "Planning architecture...",
		ThoughtEvent: &actor.ThoughtEvent{
			Source:    actor.ThoughtSourceAnthropic,
			Signature: "claude-3-7-sig-12345",
			Content:   "Planning architecture...",
		},
	})
	chunks = append(chunks, norm.Flush()...)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Thought != "Planning architecture..." {
		t.Fatalf("unexpected thought: %s", chunks[0].Thought)
	}
	if chunks[0].ThoughtEvent == nil {
		t.Fatal("expected non-nil ThoughtEvent")
	}
	if chunks[0].ThoughtEvent.Source != actor.ThoughtSourceAnthropic {
		t.Fatalf("expected source anthropic, got %s", chunks[0].ThoughtEvent.Source)
	}
	if chunks[0].ThoughtEvent.Signature != "claude-3-7-sig-12345" {
		t.Fatalf("expected signature preserved, got %s", chunks[0].ThoughtEvent.Signature)
	}
}

func TestThoughtNormalizer_DeepSeekStreamingTags(t *testing.T) {
	// Simulate chunks split across boundaries:
	// Chunk 1: "<th"
	// Chunk 2: "ink>I will solve "
	// Chunk 3: "this problem.\n"
	// Chunk 4: "Step 1 done.</th"
	// Chunk 5: "ink>\n\nHere is the answer: 42"
	inputs := []string{
		"<th",
		"ink>I will solve ",
		"this problem.\n",
		"Step 1 done.</th",
		"ink>\n\nHere is the answer: 42",
	}

	norm := actor.NewThoughtNormalizer()
	var allChunks []actor.StreamChunk
	for _, in := range inputs {
		out := norm.Feed(actor.StreamChunk{Delta: in})
		allChunks = append(allChunks, out...)
	}
	allChunks = append(allChunks, norm.Flush()...)

	var thoughtText strings.Builder
	var deltaText strings.Builder
	for _, c := range allChunks {
		if c.Thought != "" {
			if c.ThoughtEvent == nil || c.ThoughtEvent.Source != actor.ThoughtSourceDeepSeek {
				t.Fatalf("expected deepseek thought event, got: %+v", c.ThoughtEvent)
			}
			thoughtText.WriteString(c.Thought)
		}
		if c.Delta != "" {
			deltaText.WriteString(c.Delta)
		}
	}

	expectedThought := "I will solve this problem.\nStep 1 done."
	expectedDelta := "\n\nHere is the answer: 42"

	if thoughtText.String() != expectedThought {
		t.Fatalf("thought mismatch.\nGot:  %q\nWant: %q", thoughtText.String(), expectedThought)
	}
	if deltaText.String() != expectedDelta {
		t.Fatalf("delta mismatch.\nGot:  %q\nWant: %q", deltaText.String(), expectedDelta)
	}
}

func TestThoughtNormalizer_LookalikeTags(t *testing.T) {
	// Inputs that contain '<' but are NOT <think> tags:
	// "5 < 10 and 10 > 5"
	// "<th>table header</th>"
	// "<this is just text>"
	inputs := []string{
		"Comparison: 5 < 10 and 10 > 5. ",
		"HTML: <th>table header</th>. ",
		"<this is not a tag>",
	}

	norm := actor.NewThoughtNormalizer()
	var allChunks []actor.StreamChunk
	for _, in := range inputs {
		out := norm.Feed(actor.StreamChunk{Delta: in})
		allChunks = append(allChunks, out...)
	}
	allChunks = append(allChunks, norm.Flush()...)

	var thoughtText strings.Builder
	var deltaText strings.Builder
	for _, c := range allChunks {
		if c.Thought != "" {
			thoughtText.WriteString(c.Thought)
		}
		if c.Delta != "" {
			deltaText.WriteString(c.Delta)
		}
	}

	if thoughtText.Len() > 0 {
		t.Fatalf("expected no thought extracted, got: %q", thoughtText.String())
	}
	expectedTotalDelta := "Comparison: 5 < 10 and 10 > 5. HTML: <th>table header</th>. <this is not a tag>"
	if deltaText.String() != expectedTotalDelta {
		t.Fatalf("delta mismatch.\nGot:  %q\nWant: %q", deltaText.String(), expectedTotalDelta)
	}
}

func TestThoughtNormalizer_MultipleThinkBlocks(t *testing.T) {
	norm := actor.NewThoughtNormalizer()
	chunks := norm.Feed(actor.StreamChunk{
		Delta: "<think>part1</think>answer1<think>part2</think>answer2",
	})
	chunks = append(chunks, norm.Flush()...)

	var thoughts []string
	var deltas []string
	for _, c := range chunks {
		if c.Thought != "" {
			thoughts = append(thoughts, c.Thought)
		}
		if c.Delta != "" {
			deltas = append(deltas, c.Delta)
		}
	}

	if strings.Join(thoughts, "") != "part1part2" {
		t.Fatalf("unexpected thoughts: %v", thoughts)
	}
	if strings.Join(deltas, "") != "answer1answer2" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestParseThought_Helper(t *testing.T) {
	raw := "<think>Deep thoughts on life</think>The meaning is 42."
	thoughts, content := actor.ParseThought(raw)

	if len(thoughts) != 1 {
		t.Fatalf("expected 1 thought event, got %d", len(thoughts))
	}
	if thoughts[0].Source != actor.ThoughtSourceDeepSeek {
		t.Fatalf("expected deepseek source, got %s", thoughts[0].Source)
	}
	if thoughts[0].Content != "Deep thoughts on life" {
		t.Fatalf("unexpected content: %s", thoughts[0].Content)
	}
	if content != "The meaning is 42." {
		t.Fatalf("unexpected content: %s", content)
	}
}

func TestActor_ThoughtEvent_Integration(t *testing.T) {
	mock := actor.NewMockProvider()
	// DeepSeek provider sends streaming tokens containing <think>
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "<th"},
		{Delta: "ink>pondering the equation</think>x = 10"},
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Model:    "deepseek-reasoner",
		},
	})
	if err != nil {
		t.Fatalf("failed to create actor: %v", err)
	}
	defer act.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handle, err := act.Prompt(ctx, "solve equation")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	var observedThoughts []actor.ThoughtEvent
	var observedDeltas []string
	var completed bool
	var summaryOutput string

	timeout := time.After(2 * time.Second)
	for !completed {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for turn completion")
		case ev, ok := <-handle.Events():
			if !ok {
				completed = true
				break
			}
			switch ev.Type {
			case actor.EventThought:
				if ev.ThoughtEvent != nil {
					observedThoughts = append(observedThoughts, *ev.ThoughtEvent)
				}
			case actor.EventDelta:
				observedDeltas = append(observedDeltas, ev.Delta)
			case actor.EventTurnCompleted:
				completed = true
				if ev.TurnSummary != nil {
					summaryOutput = ev.TurnSummary.Output
				}
			}
		}
	}

	if summaryOutput != "x = 10" {
		t.Fatalf("expected output 'x = 10', got %q", summaryOutput)
	}

	if len(observedThoughts) == 0 {
		t.Fatal("expected at least one ThoughtEvent emitted")
	}
	if observedThoughts[0].Source != actor.ThoughtSourceDeepSeek {
		t.Fatalf("expected source deepseek, got %s", observedThoughts[0].Source)
	}
	if observedThoughts[0].Content != "pondering the equation" {
		t.Fatalf("expected content 'pondering the equation', got %q", observedThoughts[0].Content)
	}
	if strings.Join(observedDeltas, "") != "x = 10" {
		t.Fatalf("expected delta 'x = 10', got %q", strings.Join(observedDeltas, ""))
	}
}
