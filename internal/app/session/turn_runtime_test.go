package session

import (
	"context"
	"testing"
	"time"
)

func TestTurnRuntime_BeginAndCancel(t *testing.T) {
	var rt TurnRuntime
	if rt.Running() || rt.Cancel() {
		t.Fatal("empty runtime claims a turn")
	}

	ctx, end := rt.Begin(context.Background())
	if !rt.Running() {
		t.Fatal("Begin did not mark the turn running")
	}
	if !rt.Cancel() {
		t.Fatal("Cancel did not cancel the in-flight turn")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the turn context was not cancelled")
	}
	if rt.Running() {
		t.Fatal("Cancel left the turn running")
	}
	if rt.Cancel() {
		t.Fatal("a second Cancel reported an active turn")
	}
	end()
}

func TestTurnRuntime_EndClearsOnlyItsRun(t *testing.T) {
	rt := NewTurnRuntime()
	_, endFirst := rt.Begin(context.Background())
	_, endSecond := rt.Begin(context.Background())

	endFirst() // a stale end must not clear the newer run
	if !rt.Running() {
		t.Fatal("the stale end cleared the current run")
	}
	endSecond()
	if rt.Running() {
		t.Fatal("the current end did not clear the run")
	}
}

func TestTurnRuntime_ParentCancellationPropagates(t *testing.T) {
	rt := NewTurnRuntime()
	parent, cancel := context.WithCancel(context.Background())
	ctx, end := rt.Begin(parent)
	defer end()
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not reach the turn")
	}
}
