package composer

import (
	"testing"
)

func TestWordNavigationBasic(t *testing.T) {
	cases := []struct {
		text string
		back []int
		fwd  []int
	}{
		{
			text: "hello world",
			back: []int{11, 6, 0},
			fwd:  []int{0, 5, 11},
		},
		{
			text: "foo.bar",
			back: []int{7, 4, 3, 0},
			fwd:  []int{0, 3, 4, 7},
		},
		{
			text: "path/to/file",
			back: []int{12, 8, 7, 5, 4, 0},
			fwd:  []int{0, 4, 5, 7, 8, 12},
		},
		{
			text: "foo...bar",
			back: []int{9, 6, 3, 0},
			fwd:  []int{0, 3, 6, 9},
		},
		{
			text: "  hello  ",
			back: []int{9, 2, 0},
			fwd:  []int{0, 7, 9},
		},
	}

	for _, tc := range cases {
		// Test backward
		for i := 0; i+1 < len(tc.back); i++ {
			got := FindWordBackward(tc.text, tc.back[i])
			if got != tc.back[i+1] {
				t.Errorf("FindWordBackward(%q, %d) = %d, want %d", tc.text, tc.back[i], got, tc.back[i+1])
			}
		}

		// Test forward
		for i := 0; i+1 < len(tc.fwd); i++ {
			got := FindWordForward(tc.text, tc.fwd[i])
			if got != tc.fwd[i+1] {
				t.Errorf("FindWordForward(%q, %d) = %d, want %d", tc.text, tc.fwd[i], got, tc.fwd[i+1])
			}
		}
	}
}

func TestWordNavigationPathAndMention(t *testing.T) {
	text := "@src/components/button.tsx:42"
	// Navigating backward from the end
	cur := len(text)
	var backwardSteps []int
	for cur > 0 {
		next := FindWordBackward(text, cur)
		backwardSteps = append(backwardSteps, next)
		if next == cur {
			t.Fatalf("infinite loop at cursor %d", cur)
		}
		cur = next
	}

	// Verify segments jumped backward: 42, :, tsx, ., button, /, components, /, src, @
	expectedBack := []int{27, 26, 23, 22, 16, 15, 5, 4, 1, 0}
	if len(backwardSteps) != len(expectedBack) {
		t.Fatalf("backward steps len %d != expected %d: got %v, want %v", len(backwardSteps), len(expectedBack), backwardSteps, expectedBack)
	}
	for i := range expectedBack {
		if backwardSteps[i] != expectedBack[i] {
			t.Errorf("step %d: got %d, want %d", i, backwardSteps[i], expectedBack[i])
		}
	}

	// Navigating forward from 0
	cur = 0
	var forwardSteps []int
	for cur < len(text) {
		next := FindWordForward(text, cur)
		forwardSteps = append(forwardSteps, next)
		if next == cur {
			t.Fatalf("infinite loop forward at cursor %d", cur)
		}
		cur = next
	}
	expectedFwd := []int{1, 4, 5, 15, 16, 22, 23, 26, 27, 29}
	if len(forwardSteps) != len(expectedFwd) {
		t.Fatalf("forward steps len %d != expected %d: got %v, want %v", len(forwardSteps), len(expectedFwd), forwardSteps, expectedFwd)
	}
	for i := range expectedFwd {
		if forwardSteps[i] != expectedFwd[i] {
			t.Errorf("forward step %d: got %d, want %d", i, forwardSteps[i], expectedFwd[i])
		}
	}
}

func TestWordNavigationAtomicSegments(t *testing.T) {
	marker := "[Pasted text +15 lines]"
	text := "hello " + marker + " world"
	markerStart := len("hello ")
	markerEnd := markerStart + len(marker)

	// Step backward from end of "world"
	cur := len(text)
	// 1. world -> lands before world (at markerEnd + 1)
	cur = FindWordBackward(text, cur)
	if cur != markerEnd+1 {
		t.Errorf("after world backward: got %d, want %d", cur, markerEnd+1)
	}
	// 2. from space after marker -> lands before marker (at markerStart)
	cur = FindWordBackward(text, cur)
	if cur != markerStart {
		t.Errorf("skipping marker backward: got %d, want %d", cur, markerStart)
	}
	// 3. from space after hello -> lands at 0
	cur = FindWordBackward(text, cur)
	if cur != 0 {
		t.Errorf("hello backward: got %d, want 0", cur)
	}

	// Step forward from 0
	cur = 0
	// 1. hello -> lands at 5
	cur = FindWordForward(text, cur)
	if cur != len("hello") {
		t.Errorf("hello forward: got %d, want 5", cur)
	}
	// 2. from space before marker -> lands after marker
	cur = FindWordForward(text, cur)
	if cur != markerEnd {
		t.Errorf("skipping marker forward: got %d, want %d", cur, markerEnd)
	}
	// 3. from markerEnd -> lands at end of text
	cur = FindWordForward(text, cur)
	if cur != len(text) {
		t.Errorf("world forward: got %d, want %d", cur, len(text))
	}
}

func TestWordNavigationClamps(t *testing.T) {
	text := "abc"
	if got := FindWordBackward(text, 0); got != 0 {
		t.Errorf("backward at 0 = %d, want 0", got)
	}
	if got := FindWordBackward(text, -5); got != 0 {
		t.Errorf("backward at -5 = %d, want 0", got)
	}
	if got := FindWordForward(text, len(text)); got != len(text) {
		t.Errorf("forward at len = %d, want %d", got, len(text))
	}
	if got := FindWordForward(text, len(text)+10); got != len(text) {
		t.Errorf("forward at len+10 = %d, want %d", got, len(text))
	}
}
