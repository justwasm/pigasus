package skillsui

import (
	"os"
	"regexp"
	"strings"
)

const (
	SkillsPanelDefaultWidth  = 120
	SkillsPanelDefaultHeight = 30

	MDText         = "\x1b[38;5;235m"
	MDTextClose    = "\x1b[39m"
	MDReset        = "\x1b[0m"
	MDCodeOpen     = "\x1b[38;5;69m\x1b[48;5;254m"
	MDCodeClose    = "\x1b[39m\x1b[49m"
	MDCodeCloseEnd = "\x1b[0m"
	MDBold         = "\x1b[1m"
	MDBoldRestore  = "\x1b[1m\x1b[38;5;235m"
	MDLinkOpen     = "\x1b[4m\x1b[38;5;36m"
	MDH1Open       = "\x1b[1m\x1b[38;5;228m\x1b[48;5;63m"
	MDH2Open       = "\x1b[1m\x1b[38;5;27m"
	MDHROpen       = "\x1b[38;5;249m"
	MDHRText       = "──────"
	MDTableBorder  = "\x1b[38;5;249m"
)

const (
	MDTokText = iota
	MDTokCode
	MDTokBold
	MDTokURL
)

// MDTok is one token in a parsed inline markdown sequence.
type MDTok struct {
	Kind int
	Text string
	Kids []MDTok
}

var (
	mdBulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	mdOrderedRe = regexp.MustCompile(`^(\s*)(\d+)\.\s+(.*)$`)
)

func scanMDLink(s string, i int) ([]MDTok, int, bool) {
	j := strings.IndexByte(s[i:], ']')
	if j > 0 && i+j+1 < len(s) && s[i+j+1] == '(' {
		if k := strings.IndexByte(s[i+j+2:], ')'); k >= 0 {
			return MDTokenize(s[i+1 : i+j]), j + 2 + k + 1, true
		}
	}
	return nil, 0, false
}

func scanBareURL(s string, i int) (MDTok, int, bool) {
	if i > 0 && isWordByte(s[i-1]) {
		return MDTok{}, 0, false
	}
	j := i
	for j < len(s) && s[j] != ' ' && s[j] != '\t' && s[j] != ')' {
		j++
	}
	u := strings.TrimRight(s[i:j], ".,;:")
	if IsURL(u) {
		return MDTok{Kind: MDTokURL, Text: u}, len(u), true
	}
	return MDTok{}, 0, false
}

func scanNextTok(s string, i int) ([]MDTok, int, bool) {
	switch {
	case s[i] == '`':
		if j := strings.IndexByte(s[i+1:], '`'); j >= 0 {
			return []MDTok{{Kind: MDTokCode, Text: s[i+1 : i+1+j]}}, j + 2, true
		}
	case strings.HasPrefix(s[i:], "**"):
		if j := strings.Index(s[i+2:], "**"); j >= 0 {
			return []MDTok{{Kind: MDTokBold, Kids: MDTokenize(s[i+2 : i+2+j])}}, j + 4, true
		}
	case s[i] == '<':
		if j := strings.IndexByte(s[i:], '>'); j >= 0 && IsURL(s[i+1 : i+j]) {
			return []MDTok{{Kind: MDTokURL, Text: s[i+1 : i+j]}}, j + 1, true
		}
	case s[i] == '[':
		if matched, n, ok := scanMDLink(s, i); ok {
			return matched, n, true
		}
	case IsURL(s[i:]):
		if tok, n, ok := scanBareURL(s, i); ok {
			return []MDTok{tok}, n, true
		}
	}
	return nil, 0, false
}

// MDTokenize splits an inline markdown string into tokens.
func MDTokenize(s string) []MDTok {
	var toks []MDTok
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			toks = append(toks, MDTok{Kind: MDTokText, Text: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); {
		if matched, adv, ok := scanNextTok(s, i); ok {
			flush()
			toks = append(toks, matched...)
			i += adv
			continue
		}
		text.WriteByte(s[i])
		i++
	}
	flush()
	return toks
}

// IsURL reports whether s starts with an HTTP URL prefix.
func IsURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}

func mdRenderTokens(toks []MDTok, base string) (string, bool) {
	var b strings.Builder
	need := false
	emitBase := func() {
		if need {
			b.WriteString(base)
			need = false
		}
	}
	for idx, t := range toks {
		last := idx == len(toks)-1
		switch t.Kind {
		case MDTokCode:
			if base == MDBoldRestore {
				b.WriteString(MDReset)
			}
			b.WriteString(MDCodeOpen)
			b.WriteString(t.Text)
			if last {
				b.WriteString(MDCodeCloseEnd)
			} else {
				b.WriteString(base + "\x1b[49m")
			}
			need = false
		case MDTokURL:
			emitBase()
			b.WriteString(MDLinkOpen)
			b.WriteString(t.Text)
			b.WriteString(MDReset)
			need = true
		case MDTokBold:
			emitBase()
			b.WriteString(MDBold)
			inner, _ := mdRenderTokens(t.Kids, MDBoldRestore)
			b.WriteString(inner)
			b.WriteString(MDReset)
			need = true
		default:
			emitBase()
			b.WriteString(t.Text)
		}
	}
	return b.String(), need
}

// MDBodyRow renders one non-heading body line at indent extra columns.
func MDBodyRow(indent int, marker, content string) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(MDText)
	b.WriteString(strings.Repeat(" ", indent))
	if marker != "" {
		b.WriteString(marker)
		b.WriteString(" ")
	}
	inner, need := mdRenderTokens(MDTokenize(content), MDText)
	b.WriteString(inner)
	if !need {
		b.WriteString(MDTextClose)
	}
	return b.String()
}

func mdConsumeTable(i int, lines []string, fns *MDTableFootnotes) ([]DetailLine, int, bool) {
	if !MDIsTableLine(lines[i]) || i+1 >= len(lines) || !MDTableSeparatorLine(lines[i+1]) {
		return nil, i, false
	}
	end := i + 2
	for end < len(lines) && MDIsTableLine(lines[end]) {
		end++
	}
	if end > i+2 {
		if tbl := MDRenderTable(i, end, lines, fns); tbl != nil {
			return tbl, end, true
		}
	}
	return nil, i, false
}

func mdHeadingOrHR(line, trimmed string) (DetailLine, bool) {
	switch {
	case strings.HasPrefix(line, "### ") || strings.HasPrefix(line, "## "):
		return DetailLine{Rows: []string{"  " + MDH2Open + line + MDReset}}, true
	case strings.HasPrefix(line, "# "):
		return DetailLine{Rows: []string{
			"  " + MDH1Open + " " + strings.TrimPrefix(line, "# ") + " " + MDReset}}, true
	case isHRLine(trimmed):
		return DetailLine{Rows: []string{"  " + MDHROpen + MDHRText + MDTextClose}}, true
	}
	return DetailLine{}, false
}

func mdFormatParagraphOrList(line string, inList *bool, listIndent *int) DetailLine {
	if m := mdBulletRe.FindStringSubmatch(line); m != nil {
		ind := len(m[1])
		*listIndent, *inList = ind, true
		return DetailLine{Rows: []string{MDBodyRow(ind, "•", m[2])}}
	}
	if m := mdOrderedRe.FindStringSubmatch(line); m != nil {
		ind := len(m[1])
		*listIndent, *inList = ind, true
		return DetailLine{Rows: []string{MDBodyRow(ind, m[2]+".", m[3])}}
	}
	ind := 0
	if *inList {
		ind = *listIndent
	}
	return DetailLine{Rows: []string{MDBodyRow(ind, "", strings.TrimSpace(line))}}
}

// MDBodyLines converts a SKILL.md body into document lines.
func MDBodyLines(body string) []DetailLine {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var out []DetailLine
	inList := false
	listIndent := 0
	fns := NewMDTableFootnotes(SkillsPanelDefaultWidth)
	for i := 0; i < len(lines); {
		line := strings.TrimRight(lines[i], " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, DetailLine{Rows: []string{""}})
			inList = false
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			i++
			continue
		}
		if tbl, next, ok := mdConsumeTable(i, lines, fns); ok {
			out = append(out, tbl...)
			i = next
			continue
		}
		if h, ok := mdHeadingOrHR(line, trimmed); ok {
			out = append(out, h)
			inList = false
		} else {
			out = append(out, mdFormatParagraphOrList(line, &inList, &listIndent))
		}
		i++
	}
	if fns != nil && len(fns.Order) > 0 {
		out = append(out, MDFootnotesBlock(fns)...)
	}
	return trimBlankLines(out)
}

func isHRLine(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != c {
			return false
		}
	}
	return true
}

func trimBlankLines(in []DetailLine) []DetailLine {
	i := 0
	for i < len(in) && len(in[i].Rows) == 1 && in[i].Rows[0] == "" {
		i++
	}
	j := len(in)
	for j > i && len(in[j-1].Rows) == 1 && in[j-1].Rows[0] == "" {
		j--
	}
	return in[i:j]
}

// MDSkillBody reads the SKILL.md at path and returns its rendered body.
func MDSkillBody(path string) []DetailLine {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	body := string(data)
	if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
		if rest := body[strings.IndexByte(body, '\n')+1:]; true {
			if idx := strings.Index(rest, "\n---"); idx >= 0 {
				after := rest[idx+4:]
				body = strings.TrimPrefix(after, "\r")
				body = strings.TrimPrefix(body, "\n")
			}
		}
	}
	return MDBodyLines(body)
}
