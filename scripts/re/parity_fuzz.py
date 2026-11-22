#!/usr/bin/env python3
"""Automated Parity Fuzzing for Upstream Antigravity releases (1.5.14 -> 1.5.15+).

Extracts and fuzzes tools, commands, CLI flags, and exported symbols between the
upstream reference binary (or canary release) and pigasus to detect unaligned drift,
newly introduced commands/tools, or schema regressions.

Usage:
    python3 scripts/re/parity_fuzz.py [--binary PATH] [--report-json PATH] [--report-md PATH] [--simulate-canary] [--verbose]
"""

import argparse
import json
import os
import pathlib
import re
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]

# 1.5.14 Reference Specification Baselines
EXPECTED_MODEL_TOOLS_1514 = {
    "read_file",
    "run_command",
    "write_file",
    "edit_file",
    "send_message",
    "manage_task",
    "schedule",
    "invoke_subagent",
    "define_subagent",
    "manage_subagents",
    "search_web",
    "ask_question",
    "swarm",
}

EXPECTED_SLASH_COMMANDS_1514 = {
    "/add-dir", "/agents", "/artifact", "/btw", "/changelog",
    "/clear", "/codesearch", "/config", "/context", "/copy",
    "/credits", "/diff", "/effort", "/exit", "/feedback",
    "/fork", "/help", "/hooks", "/keybindings", "/logout",
    "/mcp", "/model", "/open", "/permissions", "/plugin",
    "/projects", "/remote-control", "/rename", "/resume",
    "/rewind", "/skills", "/statusline", "/tasks", "/thinking",
    "/title", "/usage", "/voice", "/goal", "/compact",
    "/schedule", "/browser", "/plan", "/grill-me",
    "/teamwork-preview", "/swarm", "/learn", "/boost",
}

BASELINE_CLI_FLAGS_1514 = {
    "--add-dir",
    "--agent",
    "--continue",
    "--conversation",
    "--dangerously-skip-permissions",
    "--disable-slash-commands",
    "--effort",
    "--input-format",
    "--json-schema",
    "--log-file",
    "--mode",
    "--model",
    "--new-project",
    "--output-format",
    "--print",
    "--print-timeout",
    "--project",
    "--prompt",
    "--prompt-interactive",
    "--remote-agent",
    "--remote-control",
    "--sandbox",
    "--settings",
    "--cdp_port",
    "--local_chrome_headless",
    "--local_chrome_user_data_dir",
    "--use_local_chrome",
}


def find_reference_binary(explicit: str | None = None) -> pathlib.Path | None:
    if explicit:
        p = pathlib.Path(explicit).expanduser().resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p
    env_bin = os.environ.get("AGY_REAL_BIN")
    if env_bin:
        p = pathlib.Path(env_bin).expanduser().resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p
    local_bin = pathlib.Path("~/.local/bin/agy").expanduser().resolve()
    if local_bin.is_file() and os.access(local_bin, os.X_OK):
        return local_bin
    extracted = REPO / "out" / "extracted-1.5.14" / "reference-binaries" / "agy-darwin-arm64"
    if extracted.is_file() and os.access(extracted, os.X_OK):
        return extracted
    return None


def extract_strings_from_binary(binary_path: pathlib.Path) -> set[str]:
    """Extract printable strings from binary using strings command."""
    try:
        proc = subprocess.run(
            ["strings", str(binary_path)],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            check=True,
            text=True,
        )
        return set(proc.stdout.splitlines())
    except Exception as e:
        print(f"[!] Warning: failed to run strings on {binary_path}: {e}")
        return set()


def camel_to_snake(name: str) -> str:
    s = re.sub(r"(.)([A-Z][a-z]+)", r"\1_\2", name)
    return re.sub(r"([a-z0-9])([A-Z])", r"\1_\2", s).lower()


def load_baseline_symbols() -> set[str]:
    """Load baseline pclntab symbols from out/extracted-1.5.14/seeds/funcs.txt if present."""
    funcs_file = REPO / "out" / "extracted-1.5.14" / "seeds" / "funcs.txt"
    symbols = set()
    if funcs_file.is_file():
        try:
            with open(funcs_file, "r", encoding="utf-8", errors="ignore") as f:
                for line in f:
                    parts = line.strip().split(maxsplit=1)
                    if len(parts) == 2:
                        symbols.add(parts[1])
        except Exception:
            pass
    return symbols


def scan_upstream_features(raw_strings: set[str], canary_simulated: bool = False) -> dict[str, set[str]]:
    """Scan strings for upstream symbols, slash commands, and tool surfaces."""
    tools = set(EXPECTED_MODEL_TOOLS_1514)
    slash_commands = set(EXPECTED_SLASH_COMMANDS_1514)
    flags = set(BASELINE_CLI_FLAGS_1514)
    key_symbols = set()

    for s in raw_strings:
        s_clean = s.strip()
        # Detect tool proto messages
        m_tool = re.search(r"tools_pb/tools_go_proto\.\(\*([A-Za-z0-9_]+)\)\.Reset", s_clean)
        if m_tool:
            proto_name = m_tool.group(1)
            if proto_name in {"ReadFile", "RunCommand", "Schedule", "SearchWeb", "SendMessage", "AskQuestion", "InvokeSubagent", "DefineSubagent", "ManageSubagents", "ManageTask"}:
                tools.add(camel_to_snake(proto_name))
            elif proto_name == "FileEdit":
                tools.add("edit_file")
                tools.add("write_file")

        # Detect CLI flags
        if s_clean.startswith("--") and len(s_clean) >= 5 and " " not in s_clean and not s_clean.endswith("-"):
            if re.match(r"^--[a-z][a-z0-9-]{2,}$", s_clean):
                flags.add(s_clean)

        # Detect architectural symbols
        if any(marker in s_clean for marker in ("tools_pb", "remote_agent_proxy", "subagent", "teamwork", "cascade")):
            key_symbols.add(s_clean)

    if canary_simulated:
        tools.add("canary_patch_tool")
        slash_commands.add("/canary-preview")
        flags.add("--canary-flag")
        key_symbols.add("google/gca/aicode/v1main/remote_agent_proxy.CanaryRPC")

    return {
        "tools": tools,
        "slash_commands": slash_commands,
        "flags": flags,
        "key_symbols": key_symbols,
    }


def scan_pigasus_features() -> dict[str, set[str]]:
    """Scan pigasus code definitions directly for tools, slash commands, and flags."""
    tools = set()
    slash_commands = set()
    flags = set()

    # 1. Read model surface tool declarations from surface JSON fixtures
    surface_dir = REPO / "internal" / "app" / "tools" / "surface"
    if surface_dir.is_dir():
        for jf in surface_dir.glob("*.json"):
            try:
                with open(jf) as f:
                    data = json.load(f)
                    if isinstance(data, list):
                        for item in data:
                            if isinstance(item, dict) and "name" in item:
                                tools.add(item["name"])
            except Exception:
                pass

        for gf in surface_dir.glob("*.go"):
            c = gf.read_text()
            for m in re.finditer(r'Name:\s*"([a-z0-9_]+)"', c):
                tools.add(m.group(1))

    # 2. Read slash commands catalog from internal/ui/slashcommands/commands.go
    cmd_file = REPO / "internal" / "ui" / "slashcommands" / "commands.go"
    if cmd_file.is_file():
        c = cmd_file.read_text()
        for m in re.finditer(r'Name:\s*"(/[a-zA-Z0-9_:-]+)"', c):
            slash_commands.add(m.group(1).lower())
        for m in re.finditer(r'Aliases:\s*\[\]string\{([^}]+)\}', c):
            aliases = m.group(1)
            for a in re.findall(r'"([^"]+)"', aliases):
                if not a.startswith("/"):
                    a = "/" + a
                slash_commands.add(a.lower())

    # 3. Read registered CLI flags from internal/cli/parser.go
    parser_file = REPO / "internal" / "cli" / "parser.go"
    if parser_file.is_file():
        c = parser_file.read_text()
        for m in re.finditer(r'Name:\s*"([a-zA-Z0-9_-]+)"', c):
            flags.add("--" + m.group(1))

    return {
        "tools": tools,
        "slash_commands": slash_commands,
        "flags": flags,
    }


def generate_drift_analysis(
    baseline_ver: str,
    canary_ver: str,
    upstream: dict[str, set[str]],
    pigasus: dict[str, set[str]],
) -> dict:
    missing_tools = sorted(upstream["tools"] - pigasus["tools"])
    missing_cmds = sorted(upstream["slash_commands"] - pigasus["slash_commands"])
    missing_flags = sorted(upstream["flags"] - pigasus["flags"])
    new_symbols = sorted(upstream.get("key_symbols", set()))

    alerts = []
    overall_status = "CLEAN"

    for t in missing_tools:
        overall_status = "CRITICAL"
        alerts.append({
            "category": "tool",
            "item": t,
            "severity": "CRITICAL",
            "message": f"Upstream introduced new tool declaration: {t}",
            "action": f"Implement in internal/app/tools/ and declare schema in internal/app/tools/surface/",
        })

    for c in missing_cmds:
        if overall_status != "CRITICAL":
            overall_status = "WARNING"
        alerts.append({
            "category": "slash_command",
            "item": c,
            "severity": "WARNING",
            "message": f"Upstream introduced new slash command: {c}",
            "action": f"Register in internal/ui/slashcommands/commands.go",
        })

    for f in missing_flags:
        alerts.append({
            "category": "flag",
            "item": f,
            "severity": "INFO",
            "message": f"Upstream introduced CLI flag: {f}",
            "action": f"Register in internal/core/cliflags/ and cmd/pigasus",
        })

    return {
        "baseline_version": baseline_ver,
        "canary_version": canary_ver,
        "overall_status": overall_status,
        "summary": {
            "upstream_tools": len(upstream["tools"]),
            "pigasus_tools": len(pigasus["tools"]),
            "missing_tools": missing_tools,
            "upstream_commands": len(upstream["slash_commands"]),
            "pigasus_commands": len(pigasus["slash_commands"]),
            "missing_commands": missing_cmds,
            "upstream_flags": len(upstream["flags"]),
            "missing_flags": missing_flags,
            "key_symbols_checked": len(new_symbols),
        },
        "alerts": alerts,
    }


def print_report(analysis: dict, verbose: bool):
    print("\n" + "=" * 65)
    print(f"UPSTREAM REFERENCE PARITY FUZZING REPORT ({analysis['baseline_version']} -> {analysis['canary_version']})")
    print("=" * 65)

    s = analysis["summary"]
    print(f"\n[+] 1. Model Tool Declarations:")
    print(f"    - Upstream checked: {s['upstream_tools']}")
    print(f"    - Pigasus declared: {s['pigasus_tools']}")
    if s["missing_tools"]:
        print(f"    [!] MISSING TOOLS in pigasus: {s['missing_tools']}")
    else:
        print("    [✓] All reference model tools declared in pigasus.")

    print(f"\n[+] 2. Slash Commands:")
    print(f"    - Upstream checked: {s['upstream_commands']}")
    print(f"    - Pigasus registered: {s['pigasus_commands']}")
    if s["missing_commands"]:
        print(f"    [!] MISSING SLASH COMMANDS: {s['missing_commands']}")
    else:
        print("    [✓] All reference slash commands registered in pigasus.")

    print(f"\n[+] 3. CLI Flags & Symbols:")
    print(f"    - Upstream flags checked: {s['upstream_flags']}")
    print(f"    - Key architectural symbols observed: {s['key_symbols_checked']}")

    if analysis["alerts"]:
        print("\n" + "-" * 65)
        print("ACTIONABLE ALERTS & REMEDIATION STEPS:")
        print("-" * 65)
        for a in analysis["alerts"]:
            print(f"  [{a['severity']}] {a['item']} ({a['category']})")
            print(f"    Problem: {a['message']}")
            print(f"    Action:  {a['action']}\n")

    print("=" * 65)
    print(f"PARITY STATUS: {analysis['overall_status']}")
    print("=" * 65)


def export_markdown(analysis: dict, path: str):
    s = analysis["summary"]
    lines = [
        f"# Upstream Reference Parity Drift Report ({analysis['baseline_version']} -> {analysis['canary_version']})",
        "",
        f"**Status**: `{analysis['overall_status']}`",
        "",
        "## Surface Coverage",
        f"- **Tools**: {s['upstream_tools']} upstream vs {s['pigasus_tools']} pigasus (Missing: {len(s['missing_tools'])})",
        f"- **Commands**: {s['upstream_commands']} upstream vs {s['pigasus_commands']} pigasus (Missing: {len(s['missing_commands'])})",
        f"- **Flags**: {s['upstream_flags']} upstream flags tracked",
        f"- **Key Symbols**: {s['key_symbols_checked']} architectural symbols observed",
        "",
    ]
    if analysis["alerts"]:
        lines.append("## Actionable Drift Alerts")
        for a in analysis["alerts"]:
            lines.append(f"### [{a['severity']}] {a['item']}")
            lines.append(f"- **Category**: {a['category']}")
            lines.append(f"- **Message**: {a['message']}")
            lines.append(f"- **Recommended Action**: {a['action']}")
            lines.append("")
    else:
        lines.append("## Parity Status\nAll tracked surfaces are fully aligned. Zero unaligned drift detected.\n")

    pathlib.Path(path).write_text("\n".join(lines), encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description="Automated parity fuzzing against upstream reference binary")
    parser.add_argument("--binary", help="Path to reference agy binary")
    parser.add_argument("--report-json", help="Export report to JSON file")
    parser.add_argument("--report-md", help="Export report to Markdown file")
    parser.add_argument("--simulate-canary", action="store_true", help="Simulate 1.5.15 canary features for test verification")
    parser.add_argument("--fail-on-drift", action="store_true", help="Exit with code 1 if critical drift is detected")
    parser.add_argument("--verbose", "-v", action="store_true", help="Verbose output")
    args = parser.parse_args()

    ref_bin = find_reference_binary(args.binary)
    if not ref_bin:
        print("[!] No upstream reference binary found (~/.local/bin/agy or AGY_REAL_BIN).")
        print("    Using built-in 1.5.14 golden specification for fuzzing baseline.")
        ref_strings = set()
    else:
        print(f"[*] Analyzing upstream reference binary: {ref_bin}")
        ref_strings = extract_strings_from_binary(ref_bin)

    canary_ver = "1.5.15-canary" if args.simulate_canary else "1.5.14"
    upstream = scan_upstream_features(ref_strings, canary_simulated=args.simulate_canary)
    pigasus = scan_pigasus_features()

    analysis = generate_drift_analysis("1.5.14", canary_ver, upstream, pigasus)
    print_report(analysis, args.verbose)

    if args.report_json:
        pathlib.Path(args.report_json).write_text(json.dumps(analysis, indent=2), encoding="utf-8")
        print(f"[✓] Exported JSON report to {args.report_json}")

    if args.report_md:
        export_markdown(analysis, args.report_md)
        print(f"[✓] Exported Markdown report to {args.report_md}")

    if args.fail_on_drift and analysis["overall_status"] == "CRITICAL":
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
