# rea-go-seeds

Recover stripped Go function names and feed them into [REA](https://github.com/morluto/rea)'s
Ghidra analysis as seeds — so a "stripped" Go binary opens with **all real function names**.

把一个被剥掉符号表的 Go 二进制，变成 Ghidra 里全量真名可搜可反编译的目标。
核心 trick:Go 的 `pclntab`(panic/栈展开表）在 `-s -w` 剥符号后仍然保留每个函数的
入口地址和名字；REA 的 `REA_GHIDRA_SEED_FILE` 支持在导入时批量创建并命名函数。
把前者喂给后者，"剥符号"基本失效。

## Quick start

```bash
# 1. Recover names and build the seed file (needs only Go, ~15s for a 180MB binary)
go run . /path/to/stripped-go-binary ./out
# → ./out/seeds.txt  (REA seed format: 0xaddr<TAB>function<TAB>name)
# → ./out/funcs.txt  (same data, human-readable)

# 2. Hand the seeds to REA's Ghidra provider
export GHIDRA_INSTALL_DIR=/path/to/ghidra_12.1.4_PUBLIC
export JAVA_HOME=/path/to/jdk-21
export REA_GHIDRA_SEED_FILE=$PWD/out/seeds.txt
rea analyze /path/to/stripped-go-binary --provider ghidra --json
```

Then query by real name, e.g. via REA's MCP session: `search_procedures` →
`procedure_pseudo_code`. See [docs/TUTORIAL.md](docs/TUTORIAL.md) for the full walkthrough
(中文) based on a real 180 MB binary with 157,992 recovered functions.

## Why not gore / redress?

[gore](https://github.com/goretk/gore) and redress parse the same pclntab, but they locate it
by hard-coded section names (`__gopclntab` / `.gopclntab`). Some toolchains — e.g. Google's
internal Go toolchain — rename the section (`__lrodata_gopcln`), and those tools silently
find nothing. This tool tries both Mach-O names, plus ELF `.gopclntab` and a PE `.rdata`
heuristic, using only the Go standard library (`debug/macho`, `debug/elf`, `debug/pe`,
`debug/gosym`). Zero dependencies.

## REA seed format reference

`REA_GHIDRA_SEED_FILE` points to a text file, one entry per line (≤ 1,000,000 entries,
≤ 32 MiB), applied by REA's pre-analysis Ghidra script:

```
0x1000011c0	function	internal/abi.(*RegArgs).Dump
0x1000013b0	function	internal/abi.(*RegArgs).IntRegArgAddr
```

Names must not contain whitespace. `label` and `code` seed kinds also exist; this tool
emits `function` seeds only.

## Files

- `main.go` — the extractor/seed generator (the whole point, ~150 lines)
- `examples/reamcp.py` — minimal REA MCP stdio driver: one `rea --mcp` session, a JSONL
  plan of tool calls, results saved per step. Useful because each REA **CLI** call
  re-runs provider analysis; an MCP session runs it once.
- `examples/plan-agy.jsonl` — the real query plan used in the tutorial case
  (open → searches → 13 targeted decompiles → close with snapshot)

## Boundary

For analysis and learning on binaries you are entitled to inspect. This repo contains only
our own tooling — no extracted data, no decompiled output, no third-party code.

## License

MIT
