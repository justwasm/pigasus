# The implicit trajectory — deliverable

**Cycle:** `cycle-304-implicit-trajectory`
**Subject:** a `!` command never reaches the model conversation, and until now
it did not reach **any** store either: the clone painted a TUI block and kept
the text in `m.response` / `m.blocks`. The reference records the command in a
separate, per-workspace, encrypted store — the *implicit trajectory* — and
this cycle writes it.
**Evidence:** 【一手】 the reference's own files on this machine
(`~/.gemini/antigravity-cli/implicit/`, 96 `.pb` files) decrypted with the
reference's own key; 【反编译】 the store's setup and save path in the 1.4.7
binary; 【strings】 the key literal and the store's own prose.

---

## 0. Summary

| | before | after |
|---|---|---|
| **a `!` command** | painted in the TUI, then dropped | painted in the TUI **and** written to `<gemini-dir>/implicit/<uuid>.pb` |
| **the store** | did not exist | `internal/implicit` — the reference's format, byte for byte |
| **the record** | — | a `CORTEX_STEP_TYPE_RUN_COMMAND` step, identical in shape to the reference's own bytes |
| **a canceled command** | — | recorded with `CORTEX_STEP_STATUS_CANCELED` and no exit code |
| **the model** | never saw the command | still does not — the cumulative-prompt handler is out of scope (§6) |

## 1. Where the reference puts it

【一手】 the reference's log, `~/.gemini/antigravity-cli/log/cli-20261022_125626.log`,
lines 804–813:

```
task_manager.go:161] Registered shell command 0 with LS tracker: "printf 'a\nb\n'" (pid=53988)
task_manager.go:403] bgTask 0: started streaming "printf 'a\nb\n'" (pid=53988)
task_manager.go:461] bgTask 0: finished (exit=0, outputLen=3)
task_manager.go:200] Finished shell command 0 in LS tracker (exit=0)
server.go:2939] OnShellCommandFinished: updating 1 active implicit trajectories
server.go:2941] OnShellCommandFinished: updating trajectory for workspace "file:///private/var/…/T/opencode"
server.go:2949] OnShellCommandFinished: successfully updated trajectory for workspace "file:///…"
```

So the reference's bash mode is a **background task** registered with the
language server's shell-command tracker, and when it finishes the server
updates the workspace's implicit trajectory.

【一手】 the store itself:

| fact | how it was established |
|---|---|
| store dir = `<gemini-dir>/implicit/` | `setUpImplicitTrajectorySaver` → `fs.Path_Join(baseDir, "implicit")`; the literal `implicit` is in the binary |
| file name = `<uuid>.pb` | 96 files, every name a UUID; `cascade_id` == the file name |
| cipher = AES-256-GCM | `proto_saver.(*DiskSaver[*uint8]).loadUnlocked` → `cipher.NewGCM` |
| key = `safeCodeiumworldKeYsecretBalloon` | a literal in the binary; it decrypts **every** one of the 96 files |
| layout = `nonce(12) ‖ ciphertext ‖ tag` | the decryption succeeds with exactly that split |
| retention = 100 files | `proto_saver.WithMaxSaveCount[*uint8]` with the literal `100` |
| scope = per workspace | `TrajectoryScope{workspace_uri, git_root_uri, branch_name}`; records keyed by `file://<workspace>` |

## 2. The record

【一手】 the reference's file
`~/.gemini/antigravity-cli/implicit/3a75e51c-a757-4e3c-9b6c-3f17f7c9b3f8.pb`
(23 863 B on disk → 23 835 B of plaintext), decrypted and walked:

```
#1 message(23755) {                    <- ImplicitTrajectory.trajectory
  #1 string(36) "8b2cf1f3-…"           <- trajectory_id
  #2 message(155) {                    <- steps (repeated)
    #1 varint 21                       <- CORTEX_STEP_TYPE_RUN_COMMAND
    #4 varint 3                        <- CORTEX_STEP_STATUS_DONE
    #5 message(28) {                   <- CortexStepMetadata
      #1 message(11) { #1 varint 1792644998 #2 varint 180119000 }  <- created_at
      #3 varint 4                      <- CORTEX_STEP_SOURCE_USER_EXPLICIT
      #8 message(11) { #1 varint 1792644998 #2 varint 196151000 }  <- completed_at
    }
    #28 message(118) {                 <- CortexStepRunCommand
      #2  string(65) "/private/var/…/T/opencode"     <- cwd
      #6  varint 0                                      <- exit_code
      #11 varint 1                                      <- blocking
      #21 message(5) { #1 string(3) "a\nb" }            <- combined_output
      #22 varint 1                                      <- used_ide_terminal
      #23 string(15) "printf 'a\nb\n'"                  <- command_line
      #25 string(15) "printf 'a\nb\n'"                  <- proposed_command_line
    }
  }
  #4 varint 1                          <- CORTEX_TRAJECTORY_TYPE_USER_MAINLINE
  #6 string(36) "3a75e51c-…"           <- cascade_id (== the file name)
  #7 message(130) {                    <- CortexTrajectoryMetadata
    #1 message(76) { #1 string(72) "file:///private/var/…" }
    #2 message(12) { #1 varint 1792434460 #2 varint 561202000 }
    #3 string(36) "f818e378-…"
  }
}
#5 string(74) "\nHfile:///private/var/…"  <- ImplicitTrajectory.trajectory_scope
```

The enum numbers are the reference's own and are confirmed against the
recovered `out/local-macos/protos/cortex_1.4.6.proto`: `RUN_COMMAND` = 21,
`STATUS_DONE` = 3, `STATUS_CANCELED` = 6, `SOURCE_USER_EXPLICIT` = 4,
`TRAJECTORY_TYPE_USER_MAINLINE` = 1.

**Note the trap**: `internal/cortex_pb` carries a *reconstructed* enum whose
`CortexStepTypeRunCommand` is **3**, not 21. That package is the clone's own
reading of the same `.proto`; the reference's numbering is the one above, and
`internal/implicit` repeats it rather than borrowing it.

## 3. What the clone writes

`internal/implicit` is the store:

| file | what it is |
|---|---|
| `implicit.go` | the store: open, record, read, prune; the AES-256-GCM seal/open; the file naming; the retention |
| `trajectory.go` | the message layout: the field numbers, the enum values, the encoder and the decoder |
| `protobuf.go` | a minimal protobuf writer and reader — varints and length-delimited fields, and a reader that keeps every field's raw bytes so an unknown field can be written back unchanged |
| `testdata/reference_step_1.4.7.pb` | the reference's own first step, 155 B, cut out of the file above |

The clone's record for a command is the reference's record:

- the command is written **twice** — as `command_line` and as
  `proposed_command_line` — because the reference writes the same bytes in
  both for a command the user typed;
- `blocking` and `used_ide_terminal` are set for every `!` command;
- the exit code is written explicitly, **including zero** — the reference's
  `printf 'a\nb\n'` record carries `#6 varint 0`;
- a canceled command carries `CORTEX_STEP_STATUS_CANCELED` and **no** exit
  code: the reference never set the field (its `top` record has none).

**The strongest check the clone can make is the one it now makes**:
`TestTheEncoderReproducesTheReferenceRecord` decodes the reference's own 155
bytes and re-encodes them, and the result is byte-identical. That pins the
field numbers, the enum values *and* the field order — the reference writes
the step's type, status, metadata and run_command in that order, and the
metadata's created_at, source and completed_at in that order.

## 4. Where the clone writes it

| reference | clone |
|---|---|
| the language server's shell-command tracker finishes the command → the server updates the workspace's trajectory | `internal/cliapp/tui_bash_mode.go`: `finishBashCommand` records the finished command; `preemptBashCommand` records the command it interrupted, as canceled |
| `OnShellCommandFinished` | `m.recordImplicitStep(...)` → `internal/cliapp/tui_implicit.go` |
| the trajectory's scope is the workspace | `implicit.WorkspaceURI(m.cwd)` — `file://` + the workspace path |
| the store lives under the gemini directory | `implicit.Open(convstore.StoreDir())` — the same `$ANTIGRAVITY_HOME` / `$HOME` rule the rest of the clone follows |

The exit code reaches the record through `bashDoneMsg.code` /
`bashDoneMsg.hasCode`: `execBashCommand` reads it out of the `*exec.ExitError`
(the clone previously kept only the failure flag), and a canceled command
carries no code — the reference's canceled record has none either.

**One deviation, on purpose.** The reference mints a random UUID per
trajectory and keeps a key→id map in memory; the clone derives the id from
the workspace URI (UUIDv5) so the file name is stable across sessions
without a sidecar map. The id is opaque either way — what matters is that the
file is loadable by the reference's own reader, and it is.

## 5. What is *not* wired

The trajectory is a **persistent** trajectory: the reference's
`cumulative_prompt_handler.getNewPersistentChatMessagePrompts` converts it
into chat messages for the next prompt, gated by the descriptor-only fields
`allowed_implicit_step_types` (prompt config) and `hide_user_implicit_steps`
(log-artifacts config).

That conversion is **not** in this cycle. The clone writes the record; the
model does not see it yet, and the reference's model does not see it either
until the next prompt — where the clone's prompt builder has no equivalent of
the cumulative-prompt handler. The clone's own `internal/cliapp/trajectory.go`
builds the model-visible steps from the model turn only, which is the
reference's behaviour for the *conversation* trajectory.

## 6. 硬约束 (hard constraints)

- **The store's format is the reference's.** The key, the layout
  (`nonce(12) ‖ ciphertext ‖ tag`), the field numbers, the enum values and
  the field order are all pinned by
  `TestTheEncoderReproducesTheReferenceRecord`. Do not "clean up" the
  encoder's field order — the byte comparison is the point.
- **A `!` command is recorded exactly once.** `finishBashCommand` records the
  finished command; `preemptBashCommand` records the interrupted one, as
  canceled. The preempted command's own completion message is dropped (its
  generation is stale), so recording it in both places would double it.
- **The exit code is written explicitly, zero included** — the reference does
  that, and a record that omits it reads differently from the reference's.
- **A canceled command has no exit code.** Do not write `0` for one.
- **The record is written under the gemini directory**, honouring
  `$ANTIGRAVITY_HOME` — never under the workspace and never into the
  conversation store.
- **Retention is 100 files**, the oldest dropped first.

## 7. 存疑 (open questions)

- **The reference's key→id map is not reproduced** (see §4). The clone's file
  names are therefore different from the reference's for the same workspace,
  and the reference would not *find* a trajectory the clone wrote — it would
  only be able to read it. Grade 【推测】 for the consequence.
- **The record's `created_at` and `completed_at` are the same instant** in
  the clone: the reference's are ~16 ms apart (the command really ran
  between them). The clone does not keep the command's start time.
- **The reference writes a second step for a long-running command** — a
  `CORTEX_STEP_TYPE_COMMAND_STATUS` step (type 28) with the same
  `command_id`. The clone does not write those, and its reader skips them.
- **The reference's `command_id`** (the tracker's id, e.g.
  `ef3ae6d0-…`) has no counterpart in the clone: the clone's bash mode runs
  the command as a plain process, not as a tracked background task.
- **The cumulative-prompt handler** (§5) is unknown in detail: the two gates
  are named in the binary's descriptor strings, but the conversion's own
  shape was not recovered.
- **The `git_root_uri` field** of the scope was not seen in any record; the
  clone writes the workspace URI and an empty branch name, like the
  reference's own outer scope.

## 8. Limitations

- The evidence is **this machine's** reference store: 96 files, written by
  1.4.7 on this machine, decrypted with the reference's own key. The
  committed fixture is one 155-byte step; the *rest* of the reference's
  record — its other steps, the metadata's timestamps — was read but not
  committed.
- The store's **retention** was read from the binary
  (`WithMaxSaveCount(100)`) and cross-checked against the 96 files on disk,
  but the clone's own prune is its own implementation: it drops the oldest
  file by modification time, and the reference's order was not recovered.
- The clone writes a **single** step type. The reference's records also carry
  the `COMMAND_STATUS` step of a long-running command, and the trajectory
  holds steps of other types in other sessions.
- The clone's bash mode is **not** the reference's: the reference runs the
  command as a background task in the language server's tracker and streams
  it; the clone runs it as a local process through the platform shell. The
  record's shape matches; the machinery behind it does not.
- No attempt was made to make the reference *read* a trajectory the clone
  wrote (§7).
