# examples

- `reamcp.py` — REA MCP stdio 会话驱动：起一个 `rea --mcp` 长会话，按 JSONL 计划逐条调用工具，结果按步骤落盘到 `./mcp/`。适合需要多次查询同一目标的场景（REA CLI 每次调用都会重跑分析，MCP 会话只分析一次）。
  用法：`python3 reamcp.py plan-agy.jsonl`（依赖环境变量 `GHIDRA_INSTALL_DIR` / `JAVA_HOME`，脚本内有默认值，按需修改）。
- `plan-agy.jsonl` — 教程案例的真实查询计划：open_binary → 全局搜索 → 13 个定点反编译 → 带快照关闭。**用前把里面的 `/Users/you/.local/bin/agy` 改成你机器上的目标路径**(MCP 要求绝对路径）。
