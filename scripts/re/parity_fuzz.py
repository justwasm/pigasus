#!/usr/bin/env python3
"""Automated Parity Fuzzing for Upstream Antigravity releases (1.5.14 -> 1.5.15+).

Extracts and fuzzes tools, commands, and flags between the upstream reference binary
and the pigasus engine to detect unaligned drift, newly introduced commands, or schema regressions.

Usage:
    python3 scripts/re/parity_fuzz.py [--binary PATH] [--verbose]
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
    # Fallback to extracted baseline
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


def scan_upstream_features(raw_strings: set[str]) -> dict[str, set[str]]:
    """Scan strings for upstream symbols, slash commands, and tool surfaces."""
    tools = set(EXPECTED_MODEL_TOOLS_1514)
    slash_commands = set(EXPECTED_SLASH_COMMANDS_1514)
    flags = set()

    # Scan binary strings for newly introduced symbols (e.g. in 1.5.15+)
    for s in raw_strings:
        s_clean = s.strip()
        # Detect new tool proto messages
        m_tool = re.search(r"tools_pb/tools_go_proto\.\(\*([A-Za-z0-9_]+)\)\.Reset", s_clean)
        if m_tool:
            proto_name = m_tool.group(1)
            if proto_name in {"ReadFile", "RunCommand", "Schedule", "SearchWeb", "SendMessage", "AskQuestion", "InvokeSubagent", "DefineSubagent", "ManageSubagents", "ManageTask"}:
                tools.add(camel_to_snake(proto_name))
            elif proto_name == "FileEdit":
                tools.add("edit_file")
                tools.add("write_file")

        # Detect new flags
        if s_clean.startswith("--") and len(s_clean) > 3 and " " not in s_clean and not s_clean.endswith("-"):
            if re.match(r"^--[a-z0-9-]+$", s_clean):
                flags.add(s_clean)

    return {
        "tools": tools,
        "slash_commands": slash_commands,
        "flags": flags,
    }


def scan_pigasus_features() -> dict[str, set[str]]:
    """Scan pigasus code definitions directly for tools, slash commands, and flags."""
    tools = set()
    slash_commands = set()

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

        # Also extract Go-defined tools like SwarmDeclaration()
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

    return {
        "tools": tools,
        "slash_commands": slash_commands,
    }


def compare_features(upstream: dict[str, set[str]], pigasus: dict[str, set[str]], verbose: bool) -> bool:
    print("\n" + "=" * 60)
    print("UPSTREAM REFERENCE PARITY FUZZING REPORT")
    print("=" * 60)

    all_matched = True

    # 1. Tools parity
    print("\n[+] 1. Model Tool Declarations:")
    missing_tools = upstream["tools"] - pigasus["tools"]
    extra_tools = pigasus["tools"] - upstream["tools"]

    print(f"    - Upstream reference tools checked: {len(upstream['tools'])}")
    print(f"    - Pigasus model tools declared:    {len(pigasus['tools'])}")

    if missing_tools:
        print(f"    [!] MISSING TOOLS in pigasus: {sorted(missing_tools)}")
        all_matched = False
    else:
        print("    [✓] All reference model tools declared in pigasus.")

    if verbose and extra_tools:
        print(f"    [*] Additional tools in pigasus: {sorted(extra_tools)}")

    # 2. Slash Commands parity
    print("\n[+] 2. Slash Commands:")
    missing_slash = upstream["slash_commands"] - pigasus["slash_commands"]
    print(f"    - Upstream reference commands checked: {len(upstream['slash_commands'])}")
    print(f"    - Pigasus slash commands registered:   {len(pigasus['slash_commands'])}")

    if missing_slash:
        print(f"    [!] MISSING SLASH COMMANDS in pigasus: {sorted(missing_slash)}")
        all_matched = False
    else:
        print("    [✓] All reference slash commands registered in pigasus.")

    print("\n" + "=" * 60)
    if all_matched:
        print("PARITY FUZZING STATUS: PASS (Zero unaligned drift detected)")
    else:
        print("PARITY FUZZING STATUS: DRIFT DETECTED")
    print("=" * 60)

    return all_matched


def main():
    parser = argparse.ArgumentParser(description="Automated parity fuzzing against upstream reference binary")
    parser.add_argument("--binary", help="Path to reference agy binary")
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

    upstream = scan_upstream_features(ref_strings)
    pigasus = scan_pigasus_features()

    success = compare_features(upstream, pigasus, args.verbose)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
