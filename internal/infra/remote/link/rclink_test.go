package link

import (
	"strings"
	"testing"
)

// TestBrowserURLUsesTheAccountChooser pins the shape of the link the
// clone prints: Google's account chooser, naming the account, carrying
// the whole deep link in "continue".
//
// The expected string is the one the user handed over:
//
//	https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2Fd4465084-5df7-4817-84ee-355a7801b045-v2
func TestBrowserURLUsesTheAccountChooser(t *testing.T) {
	got := BrowserURLForAccount("naivgaid@gmail.com",
		"d4465084-5df7-4817-84ee-355a7801b045-v2", "")
	want := "https://accounts.google.com/AccountChooser?Email=naivgaid%40gmail.com" +
		"&continue=https%3A%2F%2Fantigravity.google.com%2Fr%2Fd4465084-5df7-4817-84ee-355a7801b045-v2"
	if got != want {
		t.Errorf("BrowserURLForAccount =\n  %q\nwant\n  %q", got, want)
	}
}

// TestBrowserURLWithoutAnAccount pins the fallback: a chooser that names
// no account chooses nothing, so the bare deep link is the link.
func TestBrowserURLWithoutAnAccount(t *testing.T) {
	got := BrowserURLForAccount("", "abc-v2", "")
	want := "https://antigravity.google.com/r/abc-v2"
	if got != want {
		t.Errorf("BrowserURLForAccount(no account) = %q, want %q", got, want)
	}
}

// TestBrowserURLKeepsTheConversation pins that the conversation-scoped
// query rides inside the chooser's "continue" parameter, not beside it.
func TestBrowserURLKeepsTheConversation(t *testing.T) {
	got := BrowserURLForAccount("naivgaid@gmail.com", "abc-v2", "p=c%2F45fde9e1")
	if !strings.Contains(got, "continue=https%3A%2F%2Fantigravity.google.com%2Fr%2Fabc-v2%3Fp%3Dc%252F45fde9e1") {
		t.Errorf("the conversation query did not ride inside the continue parameter: %q", got)
	}
}

// TestDeepLinkCarriesTheQuery pins the bare link's own shape.
func TestDeepLinkCarriesTheQuery(t *testing.T) {
	got := DeepLink("abc-v2", "p=c%2F45fde9e1")
	want := "https://antigravity.google.com/r/abc-v2?p=c%2F45fde9e1"
	if got != want {
		t.Errorf("DeepLink = %q, want %q", got, want)
	}
}
