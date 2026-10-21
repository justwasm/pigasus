// stream_row_accounting_test.go — cycle 272 regression coverage for the
// mid-stream row explosion.
//
// Bug, in one sentence: the cycle-232/242 mid-stream pass built its
// "running buffer" by joining the already-styled rows in m.response, so at
// every blank-line boundary it fed glamour-padded, SGR-wrapped text back
// through glamour; glamour counted the trailing pad as content, word-
// wrapped it into extra rows and re-padded, and the live transcript roughly
// doubled in row count at each boundary. A 22-line reply reached 216 rows
// mid-stream before the turn-end replace collapsed it back.
//
// The fix renders the RAW accumulated reply (the in-progress session
// message) with the same renderMarkdown function turnDoneMsg uses, and
// swaps the whole live range in on every chunk. The live frame is then a
// truncated prefix of the final frame.
//
// These tests pin the user-visible property that was broken: the live row
// count tracks the render of the accumulated markdown and does not
// balloon at blank-line boundaries.
package main

import (
	"reflect"
	"strings"
	"testing"
)

const streamAccountingSample = "# Go 语言入门笔记\n" +
	"\n" +
	"这是一段介绍 **加粗** 和 *斜体* 以及 `行内代码` 的文字。\n" +
	"\n" +
	"## 1. 代码块\n" +
	"\n" +
	"```go\n" +
	"package main\n" +
	"\n" +
	"func main() {\n" +
	"\tfmt.Println(\"hello\")\n" +
	"}\n" +
	"```\n" +
	"\n" +
	"## 2. 表格\n" +
	"\n" +
	"| 列名 | 说明 |\n" +
	"| --- | --- |\n" +
	"| 语言 | Go |\n" +
	"\n" +
	"> 这是一个引用块。\n"

func TestStreamRowCountDoesNotExplode(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.width = 100
	m.response = append(m.response, "> seed")
	m.liveStart = len(m.response)

	sourceLines := strings.Split(strings.TrimRight(streamAccountingSample, "\n"), "\n")
	accum := ""
	for i, ln := range sourceLines {
		chunk := ln + "\n"
		if i == len(sourceLines)-1 {
			chunk = ln
		}
		accum += chunk
		_, _ = m.Update(backendChunkMsg([]byte(chunk)))
		rows := len(m.response) - m.replyStart
		// Contract: the live range is the whole-buffer render of the
		// raw text received so far. Under the pre-272 code this was
		// violated at every blank-line boundary (the pass re-rendered
		// already-styled rows), so the count ballooned.
		want := len(strings.Split(renderMarkdown(accum, m.width), "\n"))
		if rows != want {
			t.Fatalf("after chunk %q the live range has %d rows; "+
				"renderMarkdown(accumulated) has %d (re-rendering styled "+
				"rows feeds pad back through glamour); live range:\n%s",
				ln, rows, want, strings.Join(m.response[m.replyStart:], "\n"))
		}
		if rows > 3*len(sourceLines) {
			t.Fatalf("live transcript exploded: %d rows after %q (%d source lines)",
				rows, ln, len(sourceLines))
		}
	}
	// The final live range must be exactly the whole-buffer render — the
	// same bytes turnDoneMsg will install.
	want := strings.Split(renderMarkdown(streamAccountingSample, m.width), "\n")
	got := m.response[m.replyStart:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("final live range != renderMarkdown(accumulated):\ngot %d rows, want %d\n%s",
			len(got), len(want), strings.Join(got, "\n"))
	}
}
