# The file opens — the language server's file answers speak proto-JSON — deliverable

**Cycle:** `cycle-329-ls-file-content-bytes`
**Subject:** the four file methods of
`exa.language_server_pb.LanguageServerService` — `StatUri`, `ReadDir`,
`ReadFile`, `WriteFile` — and the one rule the clone broke: a proto-JSON
`bytes` field is **base64**, and the Web UI's own decoder says so out
loud.
**Evidence:** 【一手】 the reference's own answers, taken live from the
IDE's `language_server` (`out/ls-file-content/capture.sh`, and the
reference's `ReadFile`/`StatUri`/`ReadDir` bodies committed next to the
clone's); 【一手】 the reference's own refusal of a `WriteFile` whose
content is not base64; 【一手】 the 67-row language table measured
against the reference (`out/ls-file-content/language_table.py`); 【一手】
the authoritative protobuf descriptor
(`internal/lsproto/descriptor/language_server.protoset`); 【一手】 the
Web UI's own error text, and the browser that renders the file once the
answer is legal.

---

## 0. Summary

| | before | after |
|---|---|---|
| **`ReadFile`** | `{"content":"// Package rctoken — …"}` — the raw text | `{"content":"Ly8gUGFja2FnZSByY3Rva2VuIOKAlCB0aGUg…","language":"LANGUAGE_GO"}` — byte-identical to the reference |
| **the Web UI's file tab** | "Failed to read file content — Cannot decode field exa.language_server_pb.ReadFileResponse.content from JSON: invalid base64 string" | the file renders, with its line numbers |
| **`StatUri`** | `{"fileType":1,"size":9293}` — the enum as a number, the int64 as a number, no timestamp, no uri | `{"fileType":"FILE_TYPE_FILE","modTime":"2026-10-26T13:47:00.907136440Z","normalizedUri":"file:///…","size":"9293"}` — byte-identical to the reference |
| **`ReadDir`** | `{"entries":[{"uri":"file:///…","fileType":1}]}` | the enum by its name; an empty directory answers `{}` |
| **`WriteFile`** | the request's `content` was written to the file **as it arrived** — the base64 text, not the bytes | the request's `content` is decoded from base64 first; content that is not base64 is refused |
| **`language`** | absent | the reference's own table, measured row by row — 67 rows, zero differences |

---

## 1. The symptom

The user opened a file that a conversation referred to, in the Web UI:

> Failed to read file content
>
> Cannot decode field exa.language_server_pb.ReadFileResponse.content from JSON: invalid base64 string

The second line is **the app's own decoder** speaking: protobuf-es
parsed the answer and refused the `content` field. The field is a
`bytes` field, and protobuf-es's JSON reader wants base64 for a
`bytes` field — the raw text the clone sent is not base64, so the
whole message fails to decode and the file never opens.

---

## 2. The rule, and where the clone broke it

【一手】 The schema (`internal/lsproto/descriptor/language_server.protoset`,
the descriptor closure the reference's own frontend bundle embeds):

```
message ReadFileRequest  { string uri = 1; string conversation_id = 2; bool user_initiated = 3; }
message ReadFileResponse { bytes  content = 1; exa.codeium_common_pb.Language language = 2; }
message StatUriRequest   { string uri = 1; … }
message StatUriResponse  { exa.language_server_pb.FileType file_type = 1;
                           google.protobuf.Timestamp mod_time = 2;
                           string normalized_uri = 3;
                           int64 size = 4; }
message ReadDirEntry     { string uri = 1; exa.language_server_pb.FileType file_type = 2; }
message ReadDirResponse  { repeated ReadDirEntry entries = 1; }
message WriteFileRequest { string uri = 1; bytes content = 2; bool overwrite = 3; }
```

Three proto-JSON rules follow from those declarations, and the clone
broke all three in the same four handlers:

| rule | proto-JSON | the clone answered |
|---|---|---|
| a `bytes` field | base64 | the raw text |
| an enum | its **name** (`"FILE_TYPE_FILE"`) | its number (`1`) |
| an `int64` | a **string** (`"9293"`) | a number (`9293`) |

The rule is not a style question: the app's decoder rejects the first
one outright, and the other two are what the reference's own bytes
carry.

---

## 3. The reference's own answers

【一手】 The IDE's `language_server` was started on `127.0.0.1:8796`
(`-use_mocked_data=true -persistent_mode=true`) and asked the same
questions the clone is asked. `out/ls-file-content/capture.sh` runs
both sides and compares the bytes; the captures are committed next to
each other.

**`ReadFile`** — the two bodies are byte-identical:

```
$ out/ls-file-content/capture.sh 8796 8921 file:///Users/gear/Documents/GitHub/openagy/internal/rctoken/rctoken.go
ReadFile: identical
StatUri: identical
ReadDir: identical
```

```json
{"content":"Ly8gUGFja2FnZSByY3Rva2VuIOKAlCB0aGUgcmVtb3RlLWNvbnRyb2wgc3VyZmFjZSdzIHNoYXJlZCBzZWNyZXQuCi8v…","language":"LANGUAGE_GO"}
```

**`StatUri`** — again byte-identical, and the two fields the clone never
sent at all are there:

```json
{"fileType":"FILE_TYPE_FILE","modTime":"2026-10-26T13:47:00.907136440Z","normalizedUri":"file:///Users/gear/Documents/GitHub/openagy/internal/rctoken/rctoken.go","size":"9293"}
```

**`ReadDir`** — the enum by its name, and an **empty** directory answers
`{}`, because proto-JSON omits an empty repeated field.

**`WriteFile`** — the request side of the same rule. The reference
refuses a content that is not base64, and says exactly why:

```json
{"code":"invalid_argument","message":"unmarshal message: unmarshal into *language_server_go_proto.WriteFileRequest: proto: (line 1:58): invalid value for bytes field content: \"raw text\""}
```

and a content that *is* base64 lands on disk as the decoded bytes:

```
$ python3 … WriteFile {"uri":"file:///tmp/…/written.txt","content":"aGVsbG8g5LiW55WMCg=="}
$ cat /tmp/…/written.txt
hello 世界
```

The clone's `WriteFile` wrote the base64 **text** to the file, so every
write the Web UI made was a file of ASCII.

---

## 4. The fix

`internal/remotecontrol/ls_files.go` (new) owns the four answers'
shapes; `internal/remotecontrol/dispatch_languageserver.go` calls it.

| | before | after |
|---|---|---|
| `ReadFile` | `"content": string(data)` | `"content": lsEncodeBytes(data)` — base64, and `"language"` beside it |
| `StatUri` | `"fileType": 1`, `"size": 9293` | `"fileType": "FILE_TYPE_FILE"`, `"modTime": …`, `"normalizedUri": …`, `"size": "9293"` |
| `ReadDir` | `"fileType": 1` | `"fileType": "FILE_TYPE_FILE"`; an empty directory answers `{}` |
| `WriteFile` | `os.WriteFile(p, []byte(req.Content), …)` | `lsDecodeBytes(req.Content)` first; a content that is not base64 is refused |

Three details worth naming:

- **the timestamp** is rendered by `protojson.Marshal` of a
  `timestamppb`, not by `time.Format`: proto-JSON emits 0, 3, 6 or 9
  fractional digits, and the reference's own stamp carries nine
  (`2026-10-26T13:47:00.907136440Z`).
- **the `int64`** is a string, because proto-JSON spells a 64-bit
  integer as a string.
- **the enum** is the name, because proto-JSON spells an enum by its
  name.

### The `language` table is measured, not guessed

`ReadFileResponse.language` is `exa.codeium_common_pb.Language` — 92
values, from `LANGUAGE_UNSPECIFIED` to `LANGUAGE_APEX`. Which one the
reference picks for a given file is *its* table, so
`out/ls-file-content/language_table.py` writes one file per extension,
reads every one of them back through the reference, and records the
answer. The measurement is committed
(`out/ls-file-content/language_table.json`, 67 rows), and the clone's
own answers are committed beside it
(`clone_language_table.json`) — **identical, 67 rows, zero
differences**.

The table has quirks, and the clone mirrors them because the
reference's answer is the spec:

| the file | the reference answers | |
|---|---|---|
| `a.rs` | `LANGUAGE_XML` | the reference's own quirk — a Rust file is XML to it |
| `a.rsx` | `LANGUAGE_R` | ditto |
| `a.php` | `LANGUAGE_HACK` | the enum has no PHP; PHP *is* Hack |
| `a.pl`, `a.ml`, `a.ex`, `a.zig`, `a.conf`, `a.graphql`, `a` | *no* `language` field at all | proto-JSON omits an unset enum, and the clone omits the field too |

The lookup is by extension, case-insensitively (`a.PY` answers
`LANGUAGE_PYTHON`), and a whole file name wins over its extension
(`Dockerfile` answers `LANGUAGE_DOCKERFILE`, `Makefile` answers
`LANGUAGE_MAKEFILE`).

---

## 5. The verification

**The bytes.** The clone's answers and the reference's answers are
byte-identical for `ReadFile`, `StatUri` and `ReadDir` — the committed
captures are the proof (`out/ls-file-content/`).

**The language table.** 67 probed names, zero differences.

**The binary dialect.** The v2 tunnel sends `application/proto`, so the
answer has to survive the *other* encoding too: the answer is decoded
as a binary protobuf and the bytes come back as the file's exact
9,293 bytes, with `language` = 9 (`LANGUAGE_GO`).

**The browser.** The clone's own Web UI was opened on the conversation
that refers to `rctoken.go`, and the file reference was clicked:

- before: "Failed to read file content — Cannot decode field
  exa.language_server_pb.ReadFileResponse.content from JSON: invalid
  base64 string";
- after: the file renders, with its line numbers, and neither string
  appears anywhere in the page.

**The tests.** `internal/remotecontrol/ls_files_test.go` pins the
answer against the schema the way the app does — it parses the answer
with `protojson` into the message the descriptor declares and reads the
bytes back — plus the request side, the field order, the enum name, the
timestamp, the int64-as-string, the language table row by row, and the
symlink and empty-directory cases.

```
go test ./internal/remotecontrol/ ./internal/lsproto/ ./internal/remotetransport/ ./internal/rchost/
go test ./cmd/... ./internal/...
```

Both green.

---

## 6. The commit

| | |
|---|---|
| `781c3b6a` | `fix(remote-control): the file answers speak proto-JSON — base64 bytes, enum names` |

The files are `internal/remotecontrol/ls_files.go` (new),
`internal/remotecontrol/ls_files_test.go` (new),
`internal/remotecontrol/dispatch_languageserver.go` and
`internal/remotecontrol/dispatch_languageserver_test.go` (the four
handlers and the expectations they pinned).

---

## 7. Hard constraints

1. **A proto-JSON `bytes` field is base64** — on the answer side *and*
   on the request side. The Web UI's decoder rejects raw text with
   "invalid base64 string", and the reference refuses a non-base64
   request with "invalid value for bytes field content".
2. **A proto-JSON enum is its name**, not its number:
   `"FILE_TYPE_FILE"`, `"LANGUAGE_GO"`.
3. **A proto-JSON int64 is a string**, not a number: `"9293"`.
4. **A proto-JSON timestamp is a string** with 0, 3, 6 or 9 fractional
   digits — the reference's own stamp carries nine.
5. **An empty repeated field is omitted**: an empty directory answers
   `{}`, not `{"entries":[]}`.
6. **The answer's field order is the descriptor's declaration order**,
   and the reference's answers are the proof: `content` then
   `language`; `fileType`, `modTime`, `normalizedUri`, `size`.
7. **The `language` table is the reference's own**, quirks included
   (`.rs` → `LANGUAGE_XML`); an extension the reference does not know
   gets **no** `language` field.
8. **Where a message is sent from does not change the shape**: the
   answer is the same for the JSON, the binary-protobuf and the
   gRPC-Web dialects — the base64 is decoded into bytes by
   `protojson` on the way to the binary one.

---

## 8. Open questions

- **The error path is still the clone's own.** The reference answers a
  failed call with a Connect error — HTTP 404 and
  `{"code":"not_found","message":"not_found: stat …: no such file or
  directory"}` — while the clone's JSON dialect answers `{}` with
  HTTP 200, because `DispatchDirect` hands the error back as a plain
  `error` and the transport drops it outside the gRPC-Web dialect.
  A file that does not exist therefore opens as an *empty* file in the
  clone, and says nothing. **Not fixed here** — it is the transport's
  error mapping, not the file surface, and the transport is being
  reworked at the same time.
- **The reference's `ReadFile` on a missing file answers HTTP 500**
  (and not the 404 that `StatUri` answers), which looks like a
  difference in the reference's own error paths rather than in the
  clone. Not chased.

## 9. Limitations

- The language table covers the 67 names the measurement probed. An
  extension outside the table gets no `language` field — which is what
  the reference does for the extensions *it* does not know, but the two
  tables are not proven equal for the extensions neither knows.
- The `language` field's *use* in the app was not traced: the app may
  use it to pick a syntax highlighter, and if it does, a Rust file will
  be highlighted as XML — because that is what the reference says.
- The `WriteFile` path was measured against the reference's own
  language server, not against the Web UI's write flow: the app's own
  `overwrite` flag and its "the file changed on disk" behaviour were
  not exercised.
- The clone's `WriteFile` still creates the parent directory
  (`MkdirAll`) and writes 0644; the reference's own permissions for a
  file it creates were not measured.

## 10. Files

| | |
|---|---|
| `internal/remotecontrol/ls_files.go` | the four answers' shapes: base64, the enum names, the timestamp, the int64-as-string, the measured language table |
| `internal/remotecontrol/dispatch_languageserver.go` | `handleLanguageServerStatUri`, `handleLanguageServerReadDir`, `handleLanguageServerReadFile`, `handleLanguageServerWriteFile` |
| `internal/remotecontrol/ls_files_test.go` | the answers decoded the way the app decodes them, and the table pinned row by row |
| `internal/remotecontrol/dispatch_languageserver_test.go` | the four methods' expectations, corrected |
| `out/ls-file-content/capture.sh` | both sides, byte-compared |
| `out/ls-file-content/language_table.py` | the language table, measured against the reference |
| `out/ls-file-content/*.json` | the captures: the reference's and the clone's, side by side |
