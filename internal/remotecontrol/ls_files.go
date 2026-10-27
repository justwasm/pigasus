// Package remotecontrol — ls_files.go: the language server's file
// answers, in the shape the schema declares.
//
// The four file methods of exa.language_server_pb.LanguageServerService
// (StatUri, ReadDir, ReadFile, WriteFile) are the ones the Web UI
// calls when the user opens a file a conversation refers to. The
// schema is the authority on what their answers look like, and three
// of its rules are easy to get wrong:
//
//   - a `bytes` field is base64 in proto-JSON. The Web UI's
//     protobuf-es decoder rejects raw text with "invalid base64
//     string", and the file never opens.
//   - an enum is spelled by its NAME ("FILE_TYPE_FILE"), not by its
//     number.
//   - an int64 is spelled as a STRING ("9293"), not as a number.
//
// Evidence 【一手】 — measured against the reference's own language
// server on 2026-10-26 (out/ide-language-server/ref_ls_query.py, the
// IDE's `language_server` on 127.0.0.1:8796):
//
//	ReadFile → {"content":"Ly8gUGFja2FnZSByY3Rva2VuIOKAlCB0aGUg…",
//	            "language":"LANGUAGE_GO"}
//	StatUri  → {"fileType":"FILE_TYPE_FILE",
//	            "modTime":"2026-10-26T13:47:00.907136440Z",
//	            "normalizedUri":"file:///Users/gear/…/rctoken.go",
//	            "size":"9293"}
//	ReadDir  → {"entries":[{"uri":"file:///Users/gear/…/rctoken.go",
//	                        "fileType":"FILE_TYPE_FILE"}]}
//
// and the reference's own refusal of a WriteFile whose content is not
// base64 — the request side of the same rule:
//
//	{"code":"invalid_argument","message":"unmarshal message: unmarshal
//	 into *language_server_go_proto.WriteFileRequest: proto: (line
//	 1:58): invalid value for bytes field content: \"raw text\""}
//
// The `language` enum the reference answers with is measured, not
// guessed: see lsLanguageNames below.
package remotecontrol

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The proto-JSON names of exa.language_server_pb.FileType.
const (
	fileTypeFile      = "FILE_TYPE_FILE"
	fileTypeDirectory = "FILE_TYPE_DIRECTORY"
)

// lsFileTypeName returns the FileType the reference reports for a
// path.
//
// 【一手】 the reference stats the *target* of a symlink: a link to a
// file answers "FILE_TYPE_FILE" with the target's size, and a
// directory answers "FILE_TYPE_DIRECTORY" with the directory's own
// size.
func lsFileTypeName(fi os.FileInfo) string {
	if fi.IsDir() {
		return fileTypeDirectory
	}
	return fileTypeFile
}

// lsNormalizedURI returns the `file://` URI of a path, in the shape the
// reference answers with: the absolute path, slashed, and percent-free
// for the characters the reference leaves alone.
func lsNormalizedURI(path string) string {
	return "file://" + filepath.ToSlash(path)
}

// lsTimestamp returns a google.protobuf.Timestamp in proto-JSON: an
// RFC 3339 string with 0, 3, 6 or 9 fractional digits.
//
// The reference's own stamp carries nine digits
// ("2026-10-26T13:47:00.907136440Z"), which is what protojson emits
// for a nanosecond that is not a whole microsecond.
func lsTimestamp(t time.Time) string {
	raw, err := protojson.Marshal(timestamppb.New(t.UTC()))
	if err != nil {
		return t.UTC().Format(time.RFC3339Nano)
	}
	return strings.Trim(string(raw), `"`)
}

// lsLanguageNames maps a file extension to the name of the
// exa.codeium_common_pb.Language enum the reference answers with.
//
// 【一手】 every entry was measured against the reference's own
// language server on 2026-10-26 by writing a one-byte file per
// extension and reading it back with ReadFile. The reference's table
// has quirks — `.rs` answers "LANGUAGE_XML" and `.rsx` answers
// "LANGUAGE_R" — and the clone mirrors them, because the reference's
// own answer is the spec.
//
// The extensions the reference answers *without* a language (no
// extension at all, `.conf`, `.ex`, `.graphql`, `.ml`, `.pl`, `.zig`,
// and anything it does not know) are absent from the table on
// purpose: an absent extension means no `language` field at all,
// which is what proto-JSON does with an unset enum.
var lsLanguageNames = map[string]string{
	"bash":       "LANGUAGE_SHELL",
	"c":          "LANGUAGE_C",
	"cc":         "LANGUAGE_CPP",
	"cfg":        "LANGUAGE_INI",
	"clj":        "LANGUAGE_CLOJURE",
	"cmake":      "LANGUAGE_CMAKE",
	"cpp":        "LANGUAGE_CPP",
	"cs":         "LANGUAGE_CSHARP",
	"css":        "LANGUAGE_CSS",
	"dart":       "LANGUAGE_DART",
	"dockerfile": "LANGUAGE_DOCKERFILE",
	"el":         "LANGUAGE_EMACS_LISP",
	"erl":        "LANGUAGE_ERLANG",
	"exs":        "LANGUAGE_ELIXIR",
	"go":         "LANGUAGE_GO",
	"h":          "LANGUAGE_C",
	"hcl":        "LANGUAGE_HCL",
	"hpp":        "LANGUAGE_CPP",
	"hs":         "LANGUAGE_HASKELL",
	"htm":        "LANGUAGE_HTML",
	"html":       "LANGUAGE_HTML",
	"ini":        "LANGUAGE_INI",
	"java":       "LANGUAGE_JAVA",
	"js":         "LANGUAGE_JAVASCRIPT",
	"json":       "LANGUAGE_JSON",
	"jsx":        "LANGUAGE_JAVASCRIPT",
	"kt":         "LANGUAGE_KOTLIN",
	"kts":        "LANGUAGE_KOTLIN",
	"lua":        "LANGUAGE_LUA",
	"makefile":   "LANGUAGE_MAKEFILE",
	"md":         "LANGUAGE_MARKDOWN",
	"mk":         "LANGUAGE_MAKEFILE",
	"php":        "LANGUAGE_HACK",
	"proto":      "LANGUAGE_PROTOBUF",
	"py":         "LANGUAGE_PYTHON",
	"r":          "LANGUAGE_R",
	"rb":         "LANGUAGE_RUBY",
	"rs":         "LANGUAGE_XML",
	"rsx":        "LANGUAGE_R",
	"sass":       "LANGUAGE_SASS",
	"scala":      "LANGUAGE_SCALA",
	"scss":       "LANGUAGE_SCSS",
	"sh":         "LANGUAGE_SHELL",
	"sql":        "LANGUAGE_SQL",
	"svelte":     "LANGUAGE_SVELTE",
	"swift":      "LANGUAGE_SWIFT",
	"tf":         "LANGUAGE_HCL",
	"toml":       "LANGUAGE_TOML",
	"ts":         "LANGUAGE_TYPESCRIPT",
	"tsx":        "LANGUAGE_TSX",
	"txt":        "LANGUAGE_PLAINTEXT",
	"vim":        "LANGUAGE_VIMSCRIPT",
	"vue":        "LANGUAGE_VUE",
	"xml":        "LANGUAGE_XML",
	"yaml":       "LANGUAGE_YAML",
	"yml":        "LANGUAGE_YAML",
	"zsh":        "LANGUAGE_SHELL",
}

// lsFileNameLanguages maps a whole file name to the language the
// reference reports for it.
//
// 【一手】 "Dockerfile" answers "LANGUAGE_DOCKERFILE" and "Makefile"
// answers "LANGUAGE_MAKEFILE", both without an extension.
var lsFileNameLanguages = map[string]string{
	"dockerfile": "LANGUAGE_DOCKERFILE",
	"makefile":   "LANGUAGE_MAKEFILE",
}

// lsLanguageName returns the language the reference reports for a
// path, or the empty string when it reports none.
//
// The lookup is by extension, case-insensitively; a whole file name
// wins over its extension.
func lsLanguageName(path string) string {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	if lang, ok := lsFileNameLanguages[lower]; ok {
		return lang
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(base)), ".")
	if ext == "" {
		return ""
	}
	return lsLanguageNames[ext]
}

// lsEncodeBytes returns the proto-JSON spelling of a `bytes` field:
// the standard base64 of the bytes, with padding.
func lsEncodeBytes(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// lsDecodeBytes returns the bytes a proto-JSON `bytes` field carries.
//
// The request side of the same rule: the reference refuses a
// WriteFile whose content is not base64 with
//
//	invalid value for bytes field content: "raw text"
//
// so a body that does not decode is an error, not a file of ASCII.
func lsDecodeBytes(content string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(content)
}

// lsSize returns the proto-JSON spelling of an int64: a string.
func lsSize(n int64) string {
	return strconv.FormatInt(n, 10)
}
