package tuilib

import "testing"

func TestComposerArgumentHint(t *testing.T) {
	cases := []struct {
		input    string
		wantHint string
		wantOK   bool
	}{
		{"", "", false},
		{"/model", "", false},
		{"/model ", "[model]", true},
		{"/model gpt", "", false},
		{"/voice", "", false},
		{"/voice ", "[live|dictate|stop|mute|speaker|duplex|volume|speed]", true},
		{"/voice speaker ", "[name]", true},
		{"/voice duplex ", "[on|off]", true},
		{"/voice volume ", "[0-100]", true},
		{"/voice speed ", "[0.5-3]", true},
		{"/voice live ", "", false},
		{"/swarm", " board", true},
		{"/swarm ", "board", true},
		{"/swarm b", "oard", true},
		{"/swarm bo", "ard", true},
		{"/swarm boa", "rd", true},
		{"/swarm boar", "d", true},
		{"/swarm board", "", false},
		{"/swarm board ", "", false},
		{"/swarm my task", "", false},
		{"/copy", " btw", true},
		{"/copy ", "btw", true},
		{"/copy b", "tw", true},
		{"/copy bt", "w", true},
		{"/copy btw", "", false},
	}

	for _, c := range cases {
		hint, ok := ComposerArgumentHint(c.input)
		if ok != c.wantOK || hint != c.wantHint {
			t.Errorf("ComposerArgumentHint(%q) = (%q, %v), want (%q, %v)", c.input, hint, ok, c.wantHint, c.wantOK)
		}
	}
}
