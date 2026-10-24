# run_command result captures — agy 1.4.4 (live, 2026-10-22)

Every string below is the `functionResponse.response.output` of a real
`run_command` call, read out of the follow-up `streamGenerateContent` request
body captured by `cmd/agy` (`OPENAGY_CAPTURE_DIR`). Each probe was one `-p`
turn: "Use the run_command tool to run exactly this command line: <cmd>".

Paths contain a per-conversation brain uuid and a step number; they are left
verbatim, since the step number (`steps/2/output.txt`) is itself evidence.
## cnl — plain LF passthrough

```sh
printf 'a\nb\n'
```

bytes: 127

```
Created At: 2026-10-22T11:14:02+08:00
Completed At: 2026-10-22T11:14:03+08:00
Command exited with code 0.

Command output:
a
b

```

## ccrlf — CRLF -> blank line

```sh
printf 'a\r\nb\n'
```

bytes: 128

```
Created At: 2026-10-22T11:14:19+08:00
Completed At: 2026-10-22T11:14:20+08:00
Command exited with code 0.

Command output:
a

b

```

## ccr — bare CR -> LF

```sh
printf 'a\rb'
```

bytes: 126

```
Created At: 2026-10-22T11:15:19+08:00
Completed At: 2026-10-22T11:15:19+08:00
Command exited with code 0.

Command output:
a
b
```

## cexit3 — non-zero exit, stderr merged

```sh
sh -c 'echo out; echo err 1>&2; exit 3'
```

bytes: 131

```
Created At: 2026-10-22T11:11:17+08:00
Completed At: 2026-10-22T11:11:18+08:00
Command exited with code 3.

Command output:
out
err

```

## cempty — empty output

```sh
true
```

bytes: 134

```
Created At: 2026-10-22T11:11:39+08:00
Completed At: 2026-10-22T11:11:39+08:00
Command exited with code 0.

No output from the command.
```

## c9000 — below the threshold

```sh
python3 p9000.py  (9000 'C')
```

bytes: 9123

head of the result:

```
Created At: 2026-10-22T11:02:39+08:00
Completed At: 2026-10-22T11:02:39+08:00
Command exited with code 0.


```

marker: `none`

tail (last 120 bytes):

```
CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC
```

## c10195 — largest verbatim body

```sh
python3 p10195.py (10195 'K')
```

bytes: 10318

head of the result:

```
Created At: 2026-10-22T11:09:15+08:00
Completed At: 2026-10-22T11:09:15+08:00
Command exited with code 0.


```

marker: `none`

tail (last 120 bytes):

```
KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK
```

## c10196 — smallest truncated body

```sh
python3 p10196.py (10196 'K')
```

bytes: 10315

head of the result:

```
Created At: 2026-10-22T11:09:40+08:00
Completed At: 2026-10-22T11:09:40+08:00
The tool's output was truncated due to being too long. You can find the full output in a file at file:///Users/gear/.gemini/antigravity-cli/brain/cc2e5ae3-93bc-4f15-a193-7044130de3ca/.system_generated/steps/2/output.txt.

Here is a preview with the middle of the output truncated:
Command exited with code 0.


```

marker: `<truncated 347 of 10241 bytes from the middle of the output>`

preview head bytes: 4903  preview tail bytes: 4947  content newlines: head CRLF=0 tail CRLF=0

tail (last 120 bytes):

```
KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK
```

## c20000 — truncated, no newlines

```sh
python3 p20000.py (20000 'A')
```

bytes: 10317

head of the result:

```
Created At: 2026-10-22T11:02:57+08:00
Completed At: 2026-10-22T11:02:57+08:00
The tool's output was truncated due to being too long. You can find the full output in a file at file:///Users/gear/.gemini/antigravity-cli/brain/9d376293-ff7f-4100-b04f-abefabda88c9/.system_generated/steps/2/output.txt.

Here is a preview with the middle of the output truncated:
Command exited with code 0.


```

marker: `<truncated 10151 of 20045 bytes from the middle of the output>`

preview head bytes: 4903  preview tail bytes: 4947  content newlines: head CRLF=0 tail CRLF=0

tail (last 120 bytes):

```
AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
```

## c20000l — truncated, line-oriented

```sh
python3 p20000l.py (200x 'B'*99 + LF)
```

bytes: 10220

head of the result:

```
Created At: 2026-10-22T11:03:16+08:00
Completed At: 2026-10-22T11:03:16+08:00
The tool's output was truncated due to being too long. You can find the full output in a file at file:///Users/gear/.gemini/antigravity-cli/brain/0f2a4b6a-2fcd-40c9-aed9-5bc128f83f36/.system_generated/steps/2/output.txt.

Here is a preview with the middle of the output truncated:
Command exited with code 0.


```

marker: `<truncated 10351 of 20245 bytes from the middle of the output>`

preview head bytes: 4855  preview tail bytes: 4898  content newlines: head CRLF=0 tail CRLF=0

tail (last 120 bytes):

```
BBBBBBBBBBBBBBBBBBB
BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB

```

## cpreviewnl — truncated, newline-heavy

```sh
python3 pnl.py ('a\n' x 10000)
```

bytes: 7037

head of the result:

```
Created At: 2026-10-22T11:15:39+08:00
Completed At: 2026-10-22T11:15:39+08:00
The tool's output was truncated due to being too long. You can find the full output in a file at file:///Users/gear/.gemini/antigravity-cli/brain/3501116b-0b99-4818-9af8-af8d3d1613b0/.system_generated/steps/2/output.txt.

Here is a preview with the middle of the output truncated:
Command exited with code 0.


```

marker: `<truncated 20179 of 30073 bytes from the middle of the output>`

preview head bytes: 3270  preview tail bytes: 3300  content newlines: head CRLF=0 tail CRLF=0

tail (last 120 bytes):

```
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a
a

```

## cheadtail — head/tail split

```sh
python3 pheadtail.py ('H'*10000 + 'T'*10000)
```

bytes: 10317

head of the result:

```
Created At: 2026-10-22T11:08:52+08:00
Completed At: 2026-10-22T11:08:52+08:00
The tool's output was truncated due to being too long. You can find the full output in a file at file:///Users/gear/.gemini/antigravity-cli/brain/cf35a2bd-8fe5-4b13-a9ed-6baf633b4726/.system_generated/steps/2/output.txt.

Here is a preview with the middle of the output truncated:
Command exited with code 0.


```

marker: `<truncated 10151 of 20045 bytes from the middle of the output>`

preview head bytes: 4903  preview tail bytes: 4947  content newlines: head CRLF=0 tail CRLF=0

tail (last 120 bytes):

```
TTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTTT
```

