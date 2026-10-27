// help.go — vim mode help catalogue and header constants.
package vim

// HelpRows is the binding catalogue shown on the /help VIM tab.
var HelpRows = []string{
	"> i, a, I, A, o, O                Enter Insert mode",
	"  esc                            Insert/Visual → Normal",
	"  h j k l                        Left / down / up / right",
	"  w b e, W B E                   Word / WORD motions",
	"  0 ^ $, gg G                    Line and buffer bounds",
	"  f F t T, ; ,                   Find character, repeat",
	"  d c y                          Delete / change / yank (motion or object)",
	"  x X D C                        Delete char / back / to end, change to end",
	"  iw aw iW aW ip ap              Text objects",
	"  i( a( i\" a\" ...                Bracket and quote pairs",
	"  v V                            Visual / Visual line",
	"  p P                            Paste after / before",
	"  u, ctrl+r                      Undo / redo",
	"  r{char}                        Replace one character",
	"  ~                              Toggle case",
	"  [count]{motion}                e.g. 3w, 2d3w, 3dd, 2di(",
	"  ctrl+s, ctrl+enter             Submit (Normal and Insert)",
	"  enter                          Submit in Normal; newline in Insert",
	"  shift+enter, ctrl+j            Insert a newline",
	"  ZZ                             Submit from Normal mode",
}

const (
	HelpTitle    = "vim"
	HelpHeader   = "Vim Modal Shortcuts"
	HelpSubtitle = "Standard Vim key bindings active in Vim Mode"
)
