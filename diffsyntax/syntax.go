// Package diffsyntax provides per-token syntax highlighting and truncation for diff views.
package diffsyntax

import (
	"container/list"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// TokenClass is the high-level syntax token class.
type TokenClass string

const (
	Plain   TokenClass = "plain"
	Keyword TokenClass = "keyword"
	String  TokenClass = "string"
	Number  TokenClass = "number"
	Comment TokenClass = "comment"
	Type    TokenClass = "type"
)

// Token is one element of a tokenised diff line.
type Token struct {
	Text  string
	Class TokenClass
}

// Colors holds ANSI escape codes for diff syntax rendering.
const (
	ColorKeywordOpen = "\x1b[38;2;166;38;164m"
	ColorStringOpen  = "\x1b[38;2;80;161;79m"
	ColorNumberOpen  = "\x1b[38;2;152;104;1m"
	ColorCommentOpen = "\x1b[38;2;160;161;167m\x1b[3m"
	ColorTypeOpen    = "\x1b[38;2;229;192;123m"
	ColorTokenClose  = "\x1b[23m\x1b[39m"

	ColorMarkerAddedOpen   = "\x1b[38;2;133;153;0m"
	ColorMarkerRemovedOpen = "\x1b[38;2;220;50;47m"
	ColorReset             = "\x1b[39m"
)

func isDiffMarker(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '+', '-', ' ':
		return true
	}
	return false
}

func isDiffMetadataLine(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '@':
		return strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "@ ")
	case '-':
		return strings.HasPrefix(line, "--- ")
	case '+':
		return strings.HasPrefix(line, "+++ ")
	case 'd':
		return strings.HasPrefix(line, "diff ")
	}
	return false
}

const (
	maxLexerCacheEntries   = 4096
	maxTokenCacheEntries   = 4096
	maxTokenCacheLineBytes = 4096
)

type cachedLexer struct {
	lex chroma.Lexer // already wrapped with chroma.Coalesce; nil means negative cache hit
}

var (
	lexerCacheMu sync.RWMutex
	lexerCache   = make(map[string]*cachedLexer)
)

func getCachedLexer(filename string) *cachedLexer {
	if filename == "" {
		return nil
	}
	// chroma's LexerRegistry.Match immediately reduces filename via filepath.Base,
	// so keying by basename shares one cachedLexer across all directories.
	base := filepath.Base(filename)
	lexerCacheMu.RLock()
	cl, ok := lexerCache[base]
	lexerCacheMu.RUnlock()
	if ok {
		return cl
	}
	lex := lexers.Match(base)
	if lex != nil {
		lex = chroma.Coalesce(lex)
	}
	lexerCacheMu.Lock()
	if existing, ok := lexerCache[base]; ok {
		lexerCacheMu.Unlock()
		return existing
	}
	cl = &cachedLexer{lex: lex}
	if len(lexerCache) >= maxLexerCacheEntries {
		clear(lexerCache)
	}
	lexerCache[base] = cl
	lexerCacheMu.Unlock()
	return cl
}

func chromaLexerForFilename(filename string) chroma.Lexer {
	cl := getCachedLexer(filename)
	if cl == nil {
		return nil
	}
	return cl.lex
}

type tokenCacheKey struct {
	lex  *cachedLexer
	text string
}

type tokenCacheEntry struct {
	key    tokenCacheKey
	tokens []Token
}

type tokenLRU struct {
	mu    sync.Mutex
	items map[tokenCacheKey]*list.Element
	order *list.List
}

var globalTokenCache = &tokenLRU{
	items: make(map[tokenCacheKey]*list.Element),
	order: list.New(),
}

func (c *tokenLRU) get(key tokenCacheKey) ([]Token, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*tokenCacheEntry).tokens, true
}

func (c *tokenLRU) put(key tokenCacheKey, tokens []Token) {
	if len(key.text) > maxTokenCacheLineBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		el.Value.(*tokenCacheEntry).tokens = tokens
		return
	}
	if c.order.Len() >= maxTokenCacheEntries {
		if back := c.order.Back(); back != nil {
			c.order.Remove(back)
			delete(c.items, back.Value.(*tokenCacheEntry).key)
		}
	}
	el := c.order.PushFront(&tokenCacheEntry{key: key, tokens: tokens})
	c.items[key] = el
}

func chromaToDiffClass(tt chroma.TokenType) TokenClass {
	switch tt {
	case chroma.KeywordType,
		chroma.NameClass,
		chroma.NameBuiltin,
		chroma.NameBuiltinPseudo,
		chroma.NameVariableClass,
		chroma.NameVariableMagic,
		chroma.LiteralStringSymbol:
		return Type
	}
	switch tt.SubCategory() {
	case chroma.Keyword:
		return Keyword
	case chroma.LiteralString:
		return String
	case chroma.LiteralNumber:
		return Number
	case chroma.Comment:
		return Comment
	}
	return Plain
}

func chromaTokenizeCached(cl *cachedLexer, text string) []Token {
	key := tokenCacheKey{lex: cl, text: text}
	if cached, ok := globalTokenCache.get(key); ok {
		return cached
	}
	toks := chromaTokenize(cl.lex, text)
	globalTokenCache.put(key, toks)
	return toks
}

func chromaTokenize(lex chroma.Lexer, text string) []Token {
	it, err := lex.Tokenise(nil, text)
	if err != nil {
		return []Token{{Text: text, Class: Plain}}
	}
	var out []Token
	for tok := it(); tok != chroma.EOF; tok = it() {
		if tok.Value == "" {
			continue
		}
		cls := chromaToDiffClass(tok.Type)
		if n := len(out); n > 0 && out[n-1].Class == cls {
			out[n-1].Text += tok.Value
			continue
		}
		out = append(out, Token{Text: tok.Value, Class: cls})
	}
	if len(out) == 0 {
		return []Token{{Text: text, Class: Plain}}
	}
	return out
}

// TokenizeLine splits one diff line into a flat token stream.
func TokenizeLine(filename, line string) []Token {
	if line == "" {
		return []Token{{Text: "", Class: Plain}}
	}
	if isDiffMetadataLine(line) {
		return []Token{{Text: line, Class: Plain}}
	}
	cl := getCachedLexer(filename)
	if cl == nil || cl.lex == nil {
		return []Token{{Text: line, Class: Plain}}
	}
	if isDiffMarker(line) {
		marker := line[:1]
		rest := line[1:]
		if rest == "" {
			return []Token{{Text: marker, Class: Plain}}
		}
		toks := chromaTokenizeCached(cl, rest)
		out := make([]Token, 1+len(toks))
		out[0] = Token{Text: marker, Class: Plain}
		copy(out[1:], toks)
		return out
	}
	toks := chromaTokenizeCached(cl, line)
	out := make([]Token, len(toks))
	copy(out, toks)
	return out
}

// HighlightCodeLine tokenises and renders a single code line (without a diff
// marker prefix) for inline diff views. Unknown languages return text unchanged.
func HighlightCodeLine(filename, text string) string {
	if filename == "" || text == "" {
		return text
	}
	cl := getCachedLexer(filename)
	if cl == nil || cl.lex == nil {
		return text
	}
	toks := chromaTokenizeCached(cl, text)
	if len(toks) == 1 && toks[0].Class == Plain && len(toks[0].Text) > 0 && toks[0].Text[0] != '+' && toks[0].Text[0] != '-' {
		return text
	}
	return RenderInlineTokenStream(toks)
}

// TokenOpen returns the ANSI opens for a non-plain token class.
func TokenOpen(class TokenClass) string {
	switch class {
	case Keyword:
		return ColorKeywordOpen
	case String:
		return ColorStringOpen
	case Number:
		return ColorNumberOpen
	case Comment:
		return ColorCommentOpen
	case Type:
		return ColorTypeOpen
	}
	return ""
}

// RenderInlineTokenStream walks tokens and produces an ANSI-coloured string.
func RenderInlineTokenStream(tokens []Token) string {
	var b strings.Builder
	for i, tok := range tokens {
		if tok.Class != Plain {
			b.WriteString(TokenOpen(tok.Class))
			b.WriteString(tok.Text)
			b.WriteString(ColorTokenClose)
			continue
		}
		if i == 0 && len(tok.Text) > 0 {
			switch tok.Text[0] {
			case '+':
				b.WriteString(ColorMarkerAddedOpen)
				b.WriteString(tok.Text)
				b.WriteString(ColorReset)
				continue
			case '-':
				b.WriteString(ColorMarkerRemovedOpen)
				b.WriteString(tok.Text)
				b.WriteString(ColorReset)
				continue
			}
		}
		b.WriteString(tok.Text)
	}
	return b.String()
}

// RenderInlineLine tokenises and renders a diff line in one call.
func RenderInlineLine(filename, line string) string {
	if line == "" {
		return line
	}
	if filename == "" {
		switch line[0] {
		case '+':
			return ColorMarkerAddedOpen + line + ColorReset
		case '-':
			return ColorMarkerRemovedOpen + line + ColorReset
		}
		return line
	}
	tokens := TokenizeLine(filename, line)
	return RenderInlineTokenStream(tokens)
}
