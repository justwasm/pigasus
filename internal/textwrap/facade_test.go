package textwrap_test

import (
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/internal/textwrap"
)

func TestTextwrapFacade(t *testing.T) {
	got := textwrap.Words("one two three", 7)
	want := []string{"one two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Words() = %v, want %v", got, want)
	}
}
