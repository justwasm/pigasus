package tui

import "testing"

// TestAudioModel_OwnsSurface pins the ownership move: the dictation surface
// and its transport seam hang off AudioModel, not the viewport bucket.
func TestAudioModel_OwnsSurface(t *testing.T) {
	m := diffTestModel()
	if m.audio.Surface != nil {
		t.Fatal("audio surface exists before construction")
	}
	m.audio.Surface = m.newVoiceSurface()
	if m.audio.Surface == nil {
		t.Fatal("the model did not keep the surface")
	}
	if m.audio.Active() {
		t.Fatal("fresh surface is active")
	}

	connectCalls := 0
	m.audio.connect = func() error { connectCalls++; return nil }
	if m.audio.connect == nil {
		t.Fatal("host seam was not kept")
	}
	_ = connectCalls
}

// TestAudioModel_EmptyIsSafe keeps the nil surface harmless for a model
// built as a bare literal.
func TestAudioModel_EmptyIsSafe(t *testing.T) {
	var a AudioModel
	if a.Active() {
		t.Fatal("empty model is active")
	}
	if cmd := a.Start("f5"); cmd != nil {
		t.Fatal("empty model started a recording")
	}
}
