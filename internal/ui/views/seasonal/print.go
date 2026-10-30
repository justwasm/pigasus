package seasonal

import (
	"fmt"
	"io"
	"strings"
)

// Print writes the art block to w, one terminal line per row.
//
// The lines carry the reference's own 24-bit colour escapes, so the
// picture needs a truecolour terminal to look right.
//
// 【一手】 agy 1.4.7 paints its banner exactly this way — see the
// captures under out/changelog-147/ref/seasonal-scenes/.
func (a Art) Print(w io.Writer) error {
	for _, line := range a.Lines() {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// ArtNames returns the event's scene names, in the order the reference
// draws them.
func (e *Event) ArtNames() []string {
	if e == nil {
		return nil
	}
	names := make([]string, 0, len(e.Arts))
	for _, art := range e.Arts {
		names = append(names, art.Name)
	}
	return names
}

// ArtByName returns the scene with that name. The lookup is
// case-insensitive.
//
// 【推测】 The names are the clone's own labels; the reference does not
// name its scenes.
func (e *Event) ArtByName(name string) (Art, bool) {
	if e == nil {
		return Art{}, false
	}
	for _, art := range e.Arts {
		if strings.EqualFold(art.Name, name) {
			return art, true
		}
	}
	return Art{}, false
}

// PrintScenes writes the named scenes to w. An empty list writes every
// scene of the event.
//
// One scene is written as the bare picture; several are written with
// their names above them, so the pictures can be told apart.
//
// 【一手】 The five scenes are the reference's own banner art: agy 1.4.7
// draws one of them per launch (out/changelog-147/ref/seasonal-scenes/).
func (e *Event) PrintScenes(w io.Writer, names []string) error {
	if e == nil || len(e.Arts) == 0 {
		return fmt.Errorf("the event has no banner art")
	}
	if len(names) == 0 {
		names = e.ArtNames()
	}
	arts := make([]Art, 0, len(names))
	for _, name := range names {
		art, ok := e.ArtByName(name)
		if !ok {
			return fmt.Errorf("unknown scene %q (want one of %s)", name, strings.Join(e.ArtNames(), ", "))
		}
		arts = append(arts, art)
	}
	for i, art := range arts {
		if len(arts) > 1 {
			if _, err := fmt.Fprintf(w, "%s\n", art.Name); err != nil {
				return err
			}
		}
		if err := art.Print(w); err != nil {
			return err
		}
		if i+1 < len(arts) {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
	}
	return nil
}
