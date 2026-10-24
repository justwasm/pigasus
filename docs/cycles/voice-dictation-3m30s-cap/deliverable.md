# Cycle voice-dictation-3m30s-cap — AGY 1.2.8 fix: voice dictation recordings auto-stop at 3 minutes 30 seconds

## Goal

Close the changelog gap:

> Fixed voice dictation sessions longer than 4 minutes failing with a
> deadline error and erasing the drafted transcription; recordings now
> automatically stop and finalize at 3 minutes 30 seconds.

The reference binary (`~/.local/bin/agy` 1.3.4) used a 4-minute cap on
`/voice` recordings. Sessions that crossed the cap tripped the upstream
transcription gRPC deadline, which discarded the partial transcription
the recorder had streamed so far. The fix shortens the cap to 3 minutes
30 seconds (210 seconds) and guarantees the drafted transcription
survives the auto-stop path intact.

The clone is currently text-only — the `/voice` slash command and the
TUI's f5 binding still surface the cycle-167 permission-denied stub
verbatim. This cycle ships the **timing primitive** that the future
gRPC-wired integration will sit on top of. No real microphone code is
added because the upstream wiring is its own cycle; the recorder is
the load-bearing piece.

## Scope (what this cycle changes)

1. **New package `internal/voice`** — owns the dictation timing
   primitive. Stdlib-only; no gRPC, no audio backend, no PiG. The
   package is the seam a future cycle wires into the `/voice` slash
   command and the TUI's f5 binding.
2. **`MaxRecordingDuration = 3*time.Minute + 30*time.Second`** — the
   new cap. The constant is exported so the future TUI renderer can
   surface "Recording will auto-stop at 3m30s" in the hint line.
3. **`Recorder` type with state machine** —
   `StateIdle → StateRecording → StateFinalized | StateAutoStopped`.
   `Start`, `Stop`, `AppendDraft`, `Tick`, `Reset`, `Snapshot`.
4. **Auto-stop preserves the draft** — the `Tick` path that crosses
   `MaxRecordingDuration` transitions to `StateAutoStopped` *without*
   clearing the draft. The caller can still call `Snapshot()` and read
   the partial transcription.
5. **Manual Stop preserves the draft** — the manual-stop path lands in
   `StateFinalized`, distinct from `StateAutoStopped` so the caller can
   branch on the reason in the transcript renderer.
6. **Sentinel errors** — `ErrNotIdle`, `ErrNotRecording`, `ErrEmptyID`.
   `errors.Is` works on them so the future TUI can recognise
   the f5-double-bind path without string-matching.
7. **Synthetic clock injection** — `NewRecorder` takes a
   `func() time.Duration` so tests can drive the auto-stop path in
   microseconds instead of waiting for the wall clock. Production uses
   the default `time.Now`-backed reader.

## Files changed

| File | +/− | Why |
|---|---|---|
| `internal/voice/recording.go` | **new, 308** | `Recorder` struct, state machine, sentinel errors, `MaxRecordingDuration` constant. |
| `internal/voice/recording_test.go` | **new, 335** | 9 unit tests covering the cap constant, the auto-stop path, the manual-stop path, the error sentinels, and the custom-cap override. |

No pre-existing files were touched. `internal/convstore/sqlite.go` was
explicitly left alone. The `cmd/wagyu` binary is unchanged — the
`/voice` slash command still surfaces the cycle-167 permission-denied
stub verbatim.

## State machine

```
         ┌────────┐
         │ State  │   initial state
         │ Idle   │   after Reset()
         └────┬───┘
              │ Start()
              ▼
         ┌────────────┐
         │   State    │ ◀────── Tick(d) where d < remaining
         │ Recording  │
         └─┬────────┬─┘
           │        │
   Stop()  │        │  Tick(d) where d >= remaining
           ▼        ▼
   ┌────────────┐  ┌──────────────┐
   │   State    │  │    State     │   draft is preserved
   │ Finalized  │  │ AutoStopped  │   (the bug-fix claim)
   └────────────┘  └──────────────┘
```

`Snapshot()` returns the state, the draft, the stop reason, and the
elapsed time under one mutex so the TUI render goroutine cannot race
the recorder goroutine.

## Hard constraints satisfied

| Constraint | Result |
|---|---|
| `go build -o /tmp/wagyu-vd ./cmd/wagyu` exits 0 | ✅ exit 0, binary 73.3 MB |
| `go test -vet=off -count=1 -run 'TestVoice\|TestDictation\|TestRecording' ./cmd/wagyu/ ./internal/.../` passes | ✅ 9 tests pass in `internal/voice` |
| `gofmt -l` on every NEW file empty | ✅ empty output for `internal/voice/` |
| `internal/convstore/sqlite.go` untouched | ✅ `git status` shows only `internal/voice/` |
| Pre-existing non-clean files not reformatted | ✅ no pre-existing files touched |
| ≥ 4 unit tests | ✅ 9 tests (4 spec-required + 5 bonus) |
| Constant `MaxRecordingDuration = 210s` | ✅ pinned by `TestVoice_MaxRecordingDurationIs210s` |
| Recording stops at 3m30s | ✅ pinned by `TestRecording_StopsAt3m30s` |
| Transcription preserved on auto-stop | ✅ pinned by `TestDictation_PreservesDraftOnAutoStop` |
| Recording under 3m30s continues | ✅ pinned by `TestRecording_ContinuesUnder3m30s` |
| Branch not pushed | ✅ no push performed |

## Test list (matching `TestVoice|TestDictation|TestRecording`)

| Test | What it pins |
|---|---|
| `TestVoice_MaxRecordingDurationIs210s` | The constant equals `3*time.Minute + 30*time.Second` (210 s). |
| `TestRecording_StopsAt3m30s` | Ticks `Recording`→`AutoStopped` exactly when elapsed crosses the cap. |
| `TestDictation_PreservesDraftOnAutoStop` | Concatenated draft text survives auto-stop verbatim. |
| `TestRecording_ContinuesUnder3m30s` | 60-second tick leaves the recorder in `StateRecording`. |
| `TestVoice_StopManual` | Manual `Stop()` lands in `StateFinalized` with `ReasonUserStopped` and a preserved draft; `Reset()` clears the draft. |
| `TestVoice_StartAfterFinalize` | A second `Start()` after a manual `Stop()` returns `ErrNotIdle` and the prior draft is preserved. |
| `TestVoice_AppendDraftOutsideRecording` | `AppendDraft` in idle and finalized states returns `ErrNotRecording`. |
| `TestVoice_NewRecorderEmptyID` | `NewRecorder("")` returns `ErrEmptyID`. |
| `TestRecording_CustomMaxDuration` | A 2-second custom cap honours `elapsed >= cap` and lands in `StateAutoStopped` after 2 s. |

The four spec-required tests are the first four in the table; the
remaining five are bonus coverage that branch on the recorder's
remaining surface (`Stop` / `Start` overlap, off-state `AppendDraft`,
empty-id rejection, custom-cap override).

## Wiring (deferred)

The recorder is intentionally not wired into `/voice` yet. Two
follow-up cycles own that:

1. A gRPC-wired `/voice` slash command (likely lands against the next
   recovered transcription handshake).
2. A TUI f5 binding that opens the `Recorder`, calls `AppendDraft` per
   delta, and renders the auto-stop hint at session start.

Until those land, `cmd/wagyu/slash_commands.go`'s `case "voice":`
branch keeps emitting the cycle-167 permission-denied text verbatim,
matching what the reference binary surfaces when the user has not
granted the dictation scope.

## Limitations / 硬约束 / 存疑

- **No real microphone path.** The recorder is the timing primitive;
  no audio capture is wired up. The future cycle that does the gRPC
  transcription wiring is out of scope.
- **Synthetic-clock injection is private to the constructor.** The
  `nowFunc` argument is part of the public API but the package does
  not expose a high-level production constructor without it. The
  default reader (`time.Now` via `UnixNano`) covers the production
  case; tests pass their own clock so they do not sleep.
- **`StoppedAt == 0` is a valid reading** for a recorder whose clock
  has not been advanced since boot. The tests that assert
  `StoppedAt != 0` first advance the synthetic clock so the field
  stamps a non-zero reading — production will hit the same path
  because `time.Now().UnixNano()` is essentially never zero after
  boot.
- **No PiG integration.** `internal/voice` does not depend on PiG.
  The future TUI binding lives in `internal/agent/pig.go`; this cycle
  does not touch that file.

## Build/test verification

```bash
$ go build -o /tmp/wagyu-vd ./cmd/wagyu
$ echo $?
0

$ go test -vet=off -count=1 -run 'TestVoice|TestDictation|TestRecording' \
    ./cmd/wagyu/ ./internal/.../
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.578s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/voice	0.260s
(... all other packages: ok, [no tests to run] ...)
$ echo $?
0

$ gofmt -l internal/voice/
$ echo $?
0
```
