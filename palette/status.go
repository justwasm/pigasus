package palette

// StatusModeColor is the colour the status row's mode prefix is painted
// with, or "" when the mode carries no prefix at all.
//
// The stored-empty mode draws nothing: it is the absence of a saved choice
// rather than a word the user cycled to, so there is nothing to draw.
//
// The three real modes all draw.
//
// A prefix was removed once on the strength of a tuialign capture in which
// agy's composer showed a bare ">". That capture could not settle the
// question: the binary declines auto entirely when the terminal sandbox is
// unavailable ("Auto mode is not available: it needs the OS terminal sandbox
// and may not be rolled out to you yet") and falls back to default, whose row
// is the one that was photographed. Two older captures agree that auto does
// name itself: cycle-283 records the 1.4.x row as "auto · MiniMax M3", and
// cycle-383 quotes the 1.4.7 seasonal prompt as
// "? for shortcuts … auto · Gemini 3.8 Flash · high".
//
// A grep of the binary cannot arbitrate either way. The row is assembled from
// a mode name and a separator at run time, so "auto · " never exists as a
// literal — and neither does " · " (nor even "·"), which is why an absence in
// `strings` proves nothing about what the binary draws.
//
// The decisive reason to draw it is usability: a shift+tab stop that renders
// identically to a dropped row leaves the user unable to tell which mode they
// landed on.
// auto takes the dim foreground in bold rather than the accent or the agent
// colour: it is the default mode, so it should read as the baseline the other
// two are deviations from rather than compete with them for attention. The
// bold keeps the word separable from the dim separator that follows it, which
// plain dim would blend into.
func StatusModeColor(mode string) string {
	switch mode {
	case "auto":
		return DimGrayBoldOpen
	case "plan":
		return BlueOpen
	case "accept-edits":
		return AgentFgOpen
	}
	return ""
}

// StatusModePrefix reports whether the mode is drawn on the status row at
// all, and its text with the separator when it is.
//
// Every real mode draws "<mode> · "; only the stored-empty value draws
// nothing. See StatusModeColor for why auto is included.
func StatusModePrefix(mode string) string {
	if StatusModeColor(mode) == "" {
		return ""
	}
	return mode + " · "
}

// StatusModeClose ends the mode word and returns the row to its baseline — the
// dim foreground with no intensity — or "" when the mode draws nothing.
//
// It is the pair of StatusModeColor, and it is not DimGrayOpen alone. auto's
// word is opened with the dim foreground *and* bold, and DimGrayOpen sets a
// colour without touching the intensity: closing with it left the bold on, so
// the " · " and the model after it rendered bold in auto mode and in no other
// mode. Switching mode therefore appeared to change the model's own weight,
// and the row ended with the terminal still bold.
//
// The close switches off the intensity (SGR 22 clears both bold and faint)
// before restoring the dim foreground. It deliberately does not emit a full
// reset (SGR 0): a reset would also clear a background the caller had set
// around the row, and the row is a view — it may only undo what it did.
//
// A mode that later adds a third attribute owes an entry here, which is what
// TestStatusModeCloseRestoresTheBaseline checks: it applies open + close and
// compares the resulting SGR state against the row's baseline, so a mode that
// opens something this close does not undo fails rather than silently leaking.
func StatusModeClose(mode string) string {
	if StatusModeColor(mode) == "" {
		return ""
	}
	return "\x1b[22m" + DimGrayOpen
}
