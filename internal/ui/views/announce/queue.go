package announce

// queue.go — the announcement queue the TUI drives.
//
// The cards themselves (Catalog, VisibleCards) and their persistence
// (LoadDismissed, SaveDismissed) live in catalog.go; this file owns
// the *state* the TUI mutates while the session runs: the visible
// queue, the card the user hid for the session, and the last
// persistence failure.
//
// Cycle 381 (tranche 53): the queue moved here from
// internal/cliapp/elig_inline.go, where it was three fields on the
// host's runMode. The host keeps the wiring — when a card is
// dismissed, when a prompt hides one, what the view paints — and the
// queue keeps the state.

// Queue is the announcement queue: the cards the TUI still shows, in
// display order, plus the two facts that travel with them.
//
// The queue is a value the host holds and drives; every mutator is a
// method on *Queue so a call site reads as "queue.Dismiss()" rather
// than as a pile of field assignments.
type Queue struct {
	// Cards is the visible queue in display order — the head is the
	// card the TUI renders above the prompt.
	Cards []Card
	// SessionHiddenID is the id of the card the user acknowledged by
	// sending a message. It is recorded so a later cycle can restore
	// the card; no UI element consumes it yet.
	SessionHiddenID string
	// SaveError carries the last persistence failure, so the TUI can
	// warn the user that a dismissal did not stick.
	SaveError error
}

// Load seeds the visible queue from the persisted dismiss list.
//
// A corrupt or unreadable settings.json is not fatal: the queue falls
// back to the full catalogue and the failure is kept in SaveError so
// the host can surface it.
func (q *Queue) Load() {
	dismissed, err := LoadDismissed()
	if err != nil {
		q.SaveError = err
	}
	q.Cards = VisibleCards(dismissed)
	q.SessionHiddenID = ""
}

// Active returns the head of the queue, or nil when the queue is
// empty.
func (q *Queue) Active() *Card {
	if q == nil || len(q.Cards) == 0 {
		return nil
	}
	c := q.Cards[0]
	return &c
}

// Row renders the head of the queue as the announcement card box.
// Returns "" when the queue is empty.
func (q *Queue) Row() string {
	return RenderCard(q.Active())
}

// Dismiss removes the head of the visible queue and persists the
// dismissal.
//
// The head is popped first, so a persistence failure still advances
// the queue for the rest of the session; the failure is kept in
// SaveError and the host surfaces it.
func (q *Queue) Dismiss() {
	card := q.Active()
	if card == nil {
		return
	}
	q.Cards = q.Cards[1:]
	if err := SaveDismissed(card.ID); err != nil {
		q.SaveError = err
	}
	q.SessionHiddenID = ""
}

// HideForSession advances the visible queue head without persisting.
// A message the user sends acknowledges the card for the rest of the
// session; the dismissal is not written to disk.
func (q *Queue) HideForSession() {
	if len(q.Cards) == 0 {
		return
	}
	q.SessionHiddenID = q.Cards[0].ID
	q.Cards = q.Cards[1:]
}

// SaveErrorMessage returns a human-readable warning when persistence
// failed. Returns "" when nothing failed.
func (q *Queue) SaveErrorMessage() string {
	if q == nil || q.SaveError == nil {
		return ""
	}
	return "  ⓘ Could not save announcement dismissal: " + q.SaveError.Error()
}
