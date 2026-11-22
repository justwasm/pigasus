#!/usr/bin/env python3
"""drift_analyzer.py — Automated Upstream Reference Drift & Parity Inspector.

Implements automated AST & symbol table comparison, cliflags, slashcommands,
and Tool Schemas deep diffing between upstream reference releases and pigasus.

Part of the Reverse-Engineering SOP (docs/guide/reverse-engineering-sop.md).
"""

from __future__ import annotations

import argparse
import copy
import dataclasses
import datetime
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
from typing import Any, Dict, List, Optional, Set, Tuple

REPO = pathlib.Path(__file__).resolve().parents[2]

# 1.5.14 Reference Specification Baselines
BASELINE_CLI_FLAGS_1514 = [
    "--add-dir",
    "--agent",
    "--cdp_port",
    "--continue",
    "--conversation",
    "--dangerously-skip-permissions",
    "--disable-slash-commands",
    "--effort",
    "--input-format",
    "--json-schema",
    "--local_chrome_headless",
    "--local_chrome_user_data_dir",
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
    "--use_local_chrome",
    "--version",
]

BASELINE_SLASH_COMMANDS_1514 = [
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
]

SWARM_TOOL_DECLARATION = {
    "name": "swarm",
    "description": "Manage the swarm of agents working with you on the shared message board. ONLY call this tool when the user explicitly invoked `/swarm`, a skill you read explicitly instructs you to call it, or this conversation already has an active swarm. NEVER start or inspect a swarm on your own (do not call `add`, `title`, `status`, or `models` unprompted); use `invoke_subagent` for normal subagent delegation instead. Actions: `add` spawns a worker under a roster name (or `<name>-1`..`<name>-<k>` when `tasks` or `n` is set), pinned to a model, and posts `brief` on the board as an @mention of it, which is where the swarm sees its assignment -- by default a worker starts fresh and sees nothing of this conversation, so the brief must say what it owns and what done looks like; pass `fork:\"<member>\"` (or `\"coordinator\"`) to branch the new worker from that member's conversation history instead. `workspace:\"share\"` gives each worker its own working copy of your repository, for authors who edit or build at the same time as others; `\"branch\"` gives an isolated clone; readers and verifiers keep the default `inherit`. Briefs are public: every member can read them. `models` lists the models to choose from (`all:true` lists every model `add` accepts); with no model, a worker runs on your own model, which is what to use unless the user asks for another. `status` lists every member, the model it runs on, and its state: `waiting` is asleep on the board, costing nothing until somebody posts; `working` is somewhere in a turn; `stopped` is not loaded (e.g. after a restart) and a post naming it revives it; `killed` is gone. `model` switches a `waiting` worker (or yourself, when `name` is your own name or omitted) to another model, keeping its conversation: it is told of the switch, resumes any unfinished work on the new model, and runs on it from then on. Use it only when a worker or you run out of quota or capacity, or the user asks for another model, and say why; never `kill` and `add` instead. `kill` ends a worker PERMANENTLY and deletes its own workspace, so land its work first (uploaded for review, or its branch merged into your working copy; push to a remote only when the user asks): a killed worker cannot be revived, so use it when the work is over, never as a pause -- bringing one back means `add`ing a new worker with a new conversation. `title` names the swarm. Workers may call `status`, `models` and `add` too, to bring in helpers for their own work: the helper's brief is posted as you, it works in your workspace (or a share/branch of it), and should report back to you by name. A worker may `kill` only the workers it added, and `model` itself or the workers it added; killing a worker does not kill its helpers. The coordinator may `kill` any worker and `model` itself or any worker, and `title` is the coordinator's alone.",
    "parameters": {
        "type": "OBJECT",
        "properties": {
            "action": {
                "type": "STRING",
                "description": "What to do: status, models, add, model, kill or title.",
                "enum": ["status", "models", "add", "model", "kill", "title"]
            },
            "all": {
                "type": "BOOLEAN",
                "description": "For models: list every model add accepts, not just the suggested ones."
            },
            "brief": {
                "type": "STRING",
                "description": "The worker's assignment, for add: what it owns and what done looks like. Posted publicly on the board."
            },
            "fork": {
                "type": "STRING",
                "description": "Optional roster name of an existing member (or \"coordinator\") whose conversation history the new worker forks from instead of starting fresh."
            },
            "model": {
                "type": "STRING",
                "description": "Model to pin this worker to, for add and model."
            },
            "n": {
                "type": "INTEGER",
                "description": "Optional number of homogeneous copies to spawn."
            },
            "name": {
                "type": "STRING",
                "description": "Roster name of the worker."
            },
            "tasks": {
                "type": "ARRAY",
                "description": "Optional per-worker slice targets."
            }
        },
        "required": ["action"]
    }
}


@dataclasses.dataclass
class ActionableAlert:
    category: str        # "symbol", "cliflag", "slash_command", "tool_schema"
    item: str
    severity: str        # "CRITICAL", "WARNING", "INFO"
    message: str
    recommendation: str

    def to_dict(self) -> dict[str, str]:
        return {
            "category": self.category,
            "item": self.item,
            "severity": self.severity,
            "message": self.message,
            "recommendation": self.recommendation,
        }


def hash_file(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def find_candidate_binary(specified: Optional[str] = None) -> Optional[pathlib.Path]:
    """Locate candidate agy binary for inspection."""
    if specified:
        p = pathlib.Path(specified).expanduser().resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p
        return None

    for env_var in ("AGY_BIN", "AGY_REAL_BIN"):
        val = os.environ.get(env_var)
        if val:
            p = pathlib.Path(val).expanduser().resolve()
            if p.is_file() and os.access(p, os.X_OK):
                return p

    for standard_path in [
        pathlib.Path.home() / ".openagy" / "bin" / "agy",
        pathlib.Path.home() / ".local" / "bin" / "agy",
        REPO / "out" / "extracted-1.5.14" / "reference-binaries" / "agy-darwin-arm64",
    ]:
        if standard_path.is_file() and os.access(standard_path, os.X_OK):
            return standard_path.resolve()

    which_res = shutil.which("agy")
    if which_res:
        p = pathlib.Path(which_res).resolve()
        if p.is_file() and os.access(p, os.X_OK):
            return p

    return None


def extract_strings_from_binary(binary_path: pathlib.Path) -> set[str]:
    """Extract printable ASCII strings from binary."""
    try:
        proc = subprocess.run(
            ["strings", str(binary_path)],
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            check=True,
            text=True,
        )
        return set(proc.stdout.splitlines())
    except Exception:
        return set()


# -----------------------------------------------------------------------------
# 1. AST & Symbol Analysis
# -----------------------------------------------------------------------------

def load_pclntab_symbols(funcs_path: pathlib.Path) -> dict[str, str]:
    """Loads function symbols from funcs.txt (address -> name)."""
    symbols: dict[str, str] = {}
    if not funcs_path.is_file():
        return symbols
    with open(funcs_path, "r", encoding="utf-8", errors="ignore") as f:
        for line in f:
            parts = line.strip().split(maxsplit=1)
            if len(parts) == 2:
                symbols[parts[1]] = parts[0]
    return symbols


def extract_symbols_from_binary(binary_path: pathlib.Path, target_seeds_dir: pathlib.Path) -> dict[str, str]:
    """Recovers pclntab function symbols using rea-go-seeds."""
    target_seeds_dir.mkdir(parents=True, exist_ok=True)
    rea_dir = REPO / "rea-go-seeds"
    if rea_dir.is_dir():
        subprocess.run(
            ["go", "run", ".", str(binary_path), str(target_seeds_dir)],
            cwd=str(rea_dir),
            capture_output=True,
            text=True,
        )
    funcs_file = target_seeds_dir / "funcs.txt"
    return load_pclntab_symbols(funcs_file)


def parse_go_ast_symbols(repo_root: pathlib.Path) -> dict[str, set[str]]:
    """Inspects Go AST across internal packages for declared functions & types."""
    declarations: dict[str, set[str]] = {
        "tools": set(),
        "commands": set(),
        "flags": set(),
        "types": set(),
        "methods": set(),
    }

    # 1. Scan tools surface
    surface_dir = repo_root / "internal" / "app" / "tools" / "surface"
    if surface_dir.is_dir():
        for gf in surface_dir.glob("*.go"):
            c = gf.read_text(encoding="utf-8", errors="ignore")
            for m in re.finditer(r'func\s+([A-Za-z0-9_]+)\s*\(', c):
                declarations["methods"].add(f"surface.{m.group(1)}")
            for m in re.finditer(r'type\s+([A-Za-z0-9_]+)\s+struct', c):
                declarations["types"].add(f"surface.{m.group(1)}")

    # 2. Scan slashcommands
    slash_dir = repo_root / "internal" / "ui" / "slashcommands"
    if slash_dir.is_dir():
        for gf in slash_dir.glob("*.go"):
            c = gf.read_text(encoding="utf-8", errors="ignore")
            for m in re.finditer(r'func\s+([A-Za-z0-9_]+)\s*\(', c):
                declarations["methods"].add(f"slashcommands.{m.group(1)}")

    # 3. Scan cliflags & parser
    cli_dir = repo_root / "internal" / "cli"
    if cli_dir.is_dir():
        for gf in cli_dir.glob("*.go"):
            c = gf.read_text(encoding="utf-8", errors="ignore")
            for m in re.finditer(r'func\s+([A-Za-z0-9_]+)\s*\(', c):
                declarations["methods"].add(f"cli.{m.group(1)}")

    return declarations


def categorize_symbols(symbol_set: set[str]) -> dict[str, int]:
    """Categorizes Go function symbols by architectural subsystem."""
    cats = {
        "cortex_agent": 0,
        "remote_agent_proxy": 0,
        "tools_pb": 0,
        "subagent_swarm": 0,
        "cascade_plugins": 0,
        "prompt_engine": 0,
        "language_server": 0,
        "other": 0,
    }
    for s in symbol_set:
        sl = s.lower()
        if "cortex" in sl or "jetski" in sl:
            cats["cortex_agent"] += 1
        elif "remote_agent_proxy" in sl or "aicode" in sl:
            cats["remote_agent_proxy"] += 1
        elif "tools_pb" in sl or "tool" in sl:
            cats["tools_pb"] += 1
        elif "subagent" in sl or "swarm" in sl or "teamwork" in sl:
            cats["subagent_swarm"] += 1
        elif "cascade" in sl or "plugin" in sl:
            cats["cascade_plugins"] += 1
        elif "prompt" in sl:
            cats["prompt_engine"] += 1
        elif "languageserver" in sl or "lsp" in sl:
            cats["language_server"] += 1
        else:
            cats["other"] += 1
    return cats


def analyze_symbol_drift(
    base_symbols: dict[str, str],
    canary_symbols: dict[str, str],
) -> Tuple[dict[str, Any], List[ActionableAlert]]:
    base_names = set(base_symbols.keys())
    canary_names = set(canary_symbols.keys())

    added = sorted(canary_names - base_names)
    removed = sorted(base_names - canary_names)

    alerts: List[ActionableAlert] = []

    # Identify critical architectural symbol additions
    for sym in added:
        if any(marker in sym for marker in ("tools_pb", "remote_agent_proxy", "subagent", "cortex", "swarm")):
            alerts.append(ActionableAlert(
                category="symbol",
                item=sym,
                severity="WARNING",
                message=f"New upstream architectural function symbol discovered: {sym}",
                recommendation="Inspect decompiled implementation and update corresponding subsystem contracts.",
            ))

    res = {
        "baseline_count": len(base_names),
        "canary_count": len(canary_names),
        "added_count": len(added),
        "removed_count": len(removed),
        "added_sample": added[:10],
        "removed_sample": removed[:10],
        "baseline_categories": categorize_symbols(base_names),
        "canary_categories": categorize_symbols(canary_names),
    }
    return res, alerts


# -----------------------------------------------------------------------------
# 2. CLI Flags Drift Analysis
# -----------------------------------------------------------------------------

def extract_pigasus_flags(repo_root: pathlib.Path) -> set[str]:
    """Extracts registered flags from internal/cli/parser.go and cliflags specs."""
    flags: set[str] = set()
    parser_file = repo_root / "internal" / "cli" / "parser.go"
    if parser_file.is_file():
        c = parser_file.read_text(encoding="utf-8", errors="ignore")
        for m in re.finditer(r'Name:\s*"([a-zA-Z0-9_-]+)"', c):
            flags.add("--" + m.group(1))

    specs_file = repo_root / "internal" / "core" / "cliflags" / "specs.go"
    if specs_file.is_file():
        c = specs_file.read_text(encoding="utf-8", errors="ignore")
        for m in re.finditer(r'Name:\s*"([a-zA-Z0-9_-]+)"', c):
            flags.add("--" + m.group(1))

    return flags


def extract_candidate_flags(binary_path: Optional[pathlib.Path], raw_strings: set[str]) -> set[str]:
    flags: set[str] = set(BASELINE_CLI_FLAGS_1514)

    # 1. If candidate binary is runnable, query --help directly
    if binary_path and binary_path.is_file() and os.access(binary_path, os.X_OK):
        try:
            res = subprocess.run([str(binary_path), "--help"], capture_output=True, text=True, timeout=5)
            help_text = res.stdout + res.stderr
            for m in re.finditer(r'(--[a-zA-Z0-9_-]+)', help_text):
                f = m.group(1)
                if not f.endswith("-"):
                    flags.add(f)
        except Exception:
            pass

    # 2. Check for hidden flags in binary strings
    hidden_candidates = [
        "--cdp_port",
        "--local_chrome_headless",
        "--local_chrome_user_data_dir",
        "--use_local_chrome",
    ]
    for hc in hidden_candidates:
        if hc in raw_strings:
            flags.add(hc)

    return flags


def analyze_flags_drift(
    baseline_flags: list[str],
    candidate_flags: set[str],
    pigasus_flags: set[str],
) -> Tuple[dict[str, Any], List[ActionableAlert]]:
    base_set = set(baseline_flags)
    added_to_upstream = sorted(candidate_flags - base_set)
    removed_from_upstream = sorted(base_set - candidate_flags)
    unmapped_in_pigasus = sorted(candidate_flags - pigasus_flags)

    alerts: List[ActionableAlert] = []

    for f in added_to_upstream:
        alerts.append(ActionableAlert(
            category="cliflag",
            item=f,
            severity="INFO",
            message=f"Upstream release introduced new CLI flag: {f}",
            recommendation=f"Evaluate flag semantics and register in internal/core/cliflags/ and internal/cli/.",
        ))

    for f in unmapped_in_pigasus:
        if f not in added_to_upstream:
            alerts.append(ActionableAlert(
                category="cliflag",
                item=f,
                severity="WARNING",
                message=f"Flag {f} present in upstream reference but unmapped in pigasus",
                recommendation=f"Add flag definition to internal/cli/parser.go StandardFlags()",
            ))

    res = {
        "baseline_count": len(base_set),
        "candidate_count": len(candidate_flags),
        "pigasus_count": len(pigasus_flags),
        "added_to_upstream": added_to_upstream,
        "removed_from_upstream": removed_from_upstream,
        "unmapped_in_pigasus": unmapped_in_pigasus,
    }
    return res, alerts


# -----------------------------------------------------------------------------
# 3. Slash Commands Drift Analysis
# -----------------------------------------------------------------------------

def extract_pigasus_slash_commands(repo_root: pathlib.Path) -> set[str]:
    """Extracts registered slash commands from internal/ui/slashcommands/commands.go."""
    cmds: set[str] = set()
    cmd_file = repo_root / "internal" / "ui" / "slashcommands" / "commands.go"
    if cmd_file.is_file():
        c = cmd_file.read_text(encoding="utf-8", errors="ignore")
        for m in re.finditer(r'Name:\s*"(/[a-zA-Z0-9_:-]+)"', c):
            cmds.add(m.group(1).lower())
        for m in re.finditer(r'Aliases:\s*\[\]string\{([^}]+)\}', c):
            for a in re.findall(r'"([^"]+)"', m.group(1)):
                if not a.startswith("/"):
                    a = "/" + a
                cmds.add(a.lower())
    return cmds


def extract_candidate_slash_commands(raw_strings: set[str]) -> set[str]:
    cmds: set[str] = set(BASELINE_SLASH_COMMANDS_1514)

    # Search for structured slash command declarations in prompt templates or help text
    for s in raw_strings:
        # Prompt bullet style: "- /command:"
        m_bullet = re.match(r'^\s*-\s*(/[a-z][a-z0-9_-]{2,24}):', s)
        if m_bullet:
            cmds.add(m_bullet.group(1))

        # Explicit slash declaration in code/help
        m_decl = re.findall(r'Name:\s*\"(/[a-z][a-z0-9_-]{2,24})\"', s)
        for d in m_decl:
            cmds.add(d)

    return cmds


def analyze_slash_commands_drift(
    baseline_commands: list[str],
    candidate_commands: set[str],
    pigasus_commands: set[str],
) -> Tuple[dict[str, Any], List[ActionableAlert]]:
    base_set = set(baseline_commands)
    added_to_upstream = sorted(candidate_commands - base_set)
    removed_from_upstream = sorted(base_set - candidate_commands)
    missing_in_pigasus = sorted(candidate_commands - pigasus_commands)

    alerts: List[ActionableAlert] = []

    for c in added_to_upstream:
        alerts.append(ActionableAlert(
            category="slash_command",
            item=c,
            severity="WARNING",
            message=f"Upstream release introduced new slash command: {c}",
            recommendation=f"Implement command handler and register entry in internal/ui/slashcommands/commands.go",
        ))

    for c in missing_in_pigasus:
        if c not in added_to_upstream:
            alerts.append(ActionableAlert(
                category="slash_command",
                item=c,
                severity="WARNING",
                message=f"Reference slash command {c} not registered in pigasus",
                recommendation=f"Add row for {c} into internal/ui/slashcommands/commands.go",
            ))

    res = {
        "baseline_count": len(base_set),
        "candidate_count": len(candidate_commands),
        "pigasus_count": len(pigasus_commands),
        "added_to_upstream": added_to_upstream,
        "removed_from_upstream": removed_from_upstream,
        "missing_in_pigasus": missing_in_pigasus,
    }
    return res, alerts


# -----------------------------------------------------------------------------
# 4. Tool Schemas Deep Drift Analysis
# -----------------------------------------------------------------------------

def load_baseline_tool_schemas(repo_root: pathlib.Path) -> dict[str, dict[str, Any]]:
    """Loads 1.5.14 baseline tool declarations (12 standard + 1 dynamic swarm)."""
    tools: dict[str, dict[str, Any]] = {}
    schema_file = repo_root / "internal" / "app" / "tools" / "surface" / "model_tools_1.5.14.json"
    if schema_file.is_file():
        raw_list = json.loads(schema_file.read_text(encoding="utf-8"))
        for t in raw_list:
            tools[t["name"]] = t

    # Include swarm tool specification
    tools["swarm"] = SWARM_TOOL_DECLARATION
    return tools


def deep_compare_tool_schemas(
    base_schemas: dict[str, dict[str, Any]],
    cand_schemas: dict[str, dict[str, Any]],
) -> Tuple[dict[str, Any], List[ActionableAlert]]:
    alerts: List[ActionableAlert] = []

    base_names = set(base_schemas.keys())
    cand_names = set(cand_schemas.keys())

    added_tools = sorted(cand_names - base_names)
    removed_tools = sorted(base_names - cand_names)
    common_tools = sorted(base_names & cand_names)

    tool_diffs: dict[str, dict[str, Any]] = {}

    for t_name in added_tools:
        alerts.append(ActionableAlert(
            category="tool_schema",
            item=t_name,
            severity="CRITICAL",
            message=f"Upstream introduced new tool declaration: {t_name}",
            recommendation=f"Implement tool handler in internal/app/tools/ and register schema in internal/app/tools/surface/",
        ))

    for t_name in removed_tools:
        alerts.append(ActionableAlert(
            category="tool_schema",
            item=t_name,
            severity="WARNING",
            message=f"Upstream deprecated or removed tool declaration: {t_name}",
            recommendation=f"Check upstream changelog and verify whether {t_name} is gated or retired.",
        ))

    for t_name in common_tools:
        b_tool = base_schemas[t_name]
        c_tool = cand_schemas[t_name]

        b_params = b_tool.get("parameters", {})
        c_params = c_tool.get("parameters", {})

        b_props = b_params.get("properties", {})
        c_props = c_params.get("properties", {})

        b_req = set(b_params.get("required", []))
        c_req = set(c_params.get("required", []))

        # Check required fields
        new_required = sorted(c_req - b_req)
        removed_required = sorted(b_req - c_req)

        for nr in new_required:
            alerts.append(ActionableAlert(
                category="tool_schema",
                item=f"{t_name}.{nr}",
                severity="CRITICAL",
                message=f"Tool {t_name} added new REQUIRED parameter: {nr}",
                recommendation=f"Update model caller harness to populate required parameter {nr} for tool {t_name}",
            ))

        for rr in removed_required:
            alerts.append(ActionableAlert(
                category="tool_schema",
                item=f"{t_name}.{rr}",
                severity="WARNING",
                message=f"Tool {t_name} relaxed required parameter: {rr} is now optional",
                recommendation=f"Update validation in internal/app/tools/surface/",
            ))

        # Check properties
        b_prop_names = set(b_props.keys())
        c_prop_names = set(c_props.keys())

        added_props = sorted(c_prop_names - b_prop_names)
        removed_props = sorted(b_prop_names - c_prop_names)
        type_mismatches = []

        for p_name in added_props:
            if p_name not in new_required:
                p_type = c_props[p_name].get("type", "unknown")
                alerts.append(ActionableAlert(
                    category="tool_schema",
                    item=f"{t_name}.{p_name}",
                    severity="WARNING",
                    message=f"Tool {t_name} added optional parameter: {p_name} ({p_type})",
                    recommendation=f"Add parameter support in internal/app/tools/ and surface schema fixture.",
                ))

        for p_name in removed_props:
            alerts.append(ActionableAlert(
                category="tool_schema",
                item=f"{t_name}.{p_name}",
                severity="CRITICAL",
                message=f"Tool {t_name} dropped parameter: {p_name}",
                recommendation=f"Verify parameter deprecation for {t_name}.{p_name}",
            ))

        for p_name in (b_prop_names & c_prop_names):
            b_type = b_props[p_name].get("type")
            c_type = c_props[p_name].get("type")
            if b_type != c_type:
                type_mismatches.append(f"{p_name}: {b_type} -> {c_type}")
                alerts.append(ActionableAlert(
                    category="tool_schema",
                    item=f"{t_name}.{p_name}",
                    severity="CRITICAL",
                    message=f"Tool {t_name} parameter {p_name} type changed from {b_type} to {c_type}",
                    recommendation=f"Update type assertion and marshaling in internal/app/tools/surface/",
                ))

        if added_props or removed_props or new_required or removed_required or type_mismatches:
            tool_diffs[t_name] = {
                "added_properties": added_props,
                "removed_properties": removed_props,
                "new_required": new_required,
                "removed_required": removed_required,
                "type_mismatches": type_mismatches,
            }

    res = {
        "baseline_count": len(base_names),
        "candidate_count": len(cand_names),
        "added_tools": added_tools,
        "removed_tools": removed_tools,
        "modified_tools": tool_diffs,
    }
    return res, alerts


# -----------------------------------------------------------------------------
# 5. Canary Simulation Helper
# -----------------------------------------------------------------------------

def simulate_canary_state(
    base_symbols: dict[str, str],
    base_flags: list[str],
    base_cmds: list[str],
    base_schemas: dict[str, dict[str, Any]],
) -> Tuple[dict[str, str], set[str], set[str], dict[str, dict[str, Any]]]:
    """Injects synthetic canary features to simulate upstream version move (1.5.15+)."""
    canary_symbols = dict(base_symbols)
    canary_symbols["jetski/cortex.CanaryAgentService"] = "0x110001000"
    canary_symbols["tools_pb.(*CanaryBatchTool).Reset"] = "0x110002000"
    canary_symbols["subagent.ParallelWorkerCluster"] = "0x110003000"
    canary_symbols["google/gca/aicode/v1main/remote_agent_proxy.StreamCanaryEvents"] = "0x110004000"

    canary_flags = set(base_flags)
    canary_flags.add("--canary-feature-gate")
    canary_flags.add("--max-speculative-depth")

    canary_cmds = set(base_cmds)
    canary_cmds.add("/canary-tune")
    canary_cmds.add("/parallel-run")

    canary_schemas = copy.deepcopy(base_schemas)

    # 1. New tool
    canary_schemas["batch_execute"] = {
        "name": "batch_execute",
        "description": "Execute multiple read or compute actions concurrently across workspace targets.",
        "parameters": {
            "type": "OBJECT",
            "properties": {
                "tasks": {"type": "ARRAY", "description": "Array of task descriptions."},
                "maxConcurrency": {"type": "INTEGER", "description": "Maximum parallel tasks."},
            },
            "required": ["tasks"],
        },
    }

    # 2. Modify existing tool with new optional param
    if "run_command" in canary_schemas:
        canary_schemas["run_command"]["parameters"]["properties"]["TimeoutSeconds"] = {
            "type": "INTEGER",
            "description": "Execution timeout limit in seconds.",
        }

    # 3. Add breaking change to edit_file (new required param)
    if "edit_file" in canary_schemas:
        canary_schemas["edit_file"]["parameters"]["properties"]["Checksum"] = {
            "type": "STRING",
            "description": "Checksum verification hash of target block.",
        }
        canary_schemas["edit_file"]["parameters"]["required"].append("Checksum")

    return canary_symbols, canary_flags, canary_cmds, canary_schemas


# -----------------------------------------------------------------------------
# 6. Report Generation & Docs Updater
# -----------------------------------------------------------------------------

def evaluate_overall_severity(alerts: List[ActionableAlert]) -> str:
    if any(a.severity == "CRITICAL" for a in alerts):
        return "CRITICAL"
    if any(a.severity == "WARNING" for a in alerts):
        return "WARNING"
    if any(a.severity == "INFO" for a in alerts):
        return "INFO"
    return "CLEAN"


def generate_markdown_report(report_data: dict[str, Any]) -> str:
    lines = [
        f"## Automated Reference Drift Inspection & Parity Audit",
        f"",
        f"> **Inspection Timestamp**: `{report_data['timestamp']}`  ",
        f"> **Baseline Version**: `agy {report_data['baseline_version']}` | **Candidate**: `{report_data['canary_version']}`  ",
        f"> **Overall Parity Status**: `{report_data['overall_status']}`",
        f"",
        f"### 1. Parity Audit Summary",
        f"",
        f"| Surface / Subsystem | Baseline ({report_data['baseline_version']}) | Candidate ({report_data['canary_version']}) | Status | Unaligned Diff |",
        f"|---|---|---|---|---|",
    ]

    sym = report_data["symbols_diff"]
    sym_status = "DRIFT" if sym["added_count"] or sym["removed_count"] else "CLEAN"
    lines.append(
        f"| **Pclntab Symbols** | {sym['baseline_count']:,} | {sym['canary_count']:,} | {sym_status} | +{sym['added_count']:,} / -{sym['removed_count']:,} |"
    )

    flg = report_data["flags_diff"]
    flg_status = "DRIFT" if flg["added_to_upstream"] or flg["unmapped_in_pigasus"] else "CLEAN"
    lines.append(
        f"| **CLI Flags** | {flg['baseline_count']} | {flg['candidate_count']} | {flg_status} | +{len(flg['added_to_upstream'])} / unmapped: {len(flg['unmapped_in_pigasus'])} |"
    )

    cmd = report_data["commands_diff"]
    cmd_status = "DRIFT" if cmd["added_to_upstream"] or cmd["missing_in_pigasus"] else "CLEAN"
    lines.append(
        f"| **Slash Commands** | {cmd['baseline_count']} | {cmd['candidate_count']} | {cmd_status} | +{len(cmd['added_to_upstream'])} / missing: {len(cmd['missing_in_pigasus'])} |"
    )

    tls = report_data["tools_diff"]
    tls_status = "DRIFT" if tls["added_tools"] or tls["modified_tools"] else "CLEAN"
    lines.append(
        f"| **Tool Schemas** | {tls['baseline_count']} tools | {tls['candidate_count']} tools | {tls_status} | +{len(tls['added_tools'])} / modified: {len(tls['modified_tools'])} |"
    )

    lines.append("")
    lines.append("### 2. Surface Breakdown")
    lines.append("")

    # Architectural symbols breakdown
    lines.append("#### Architectural Symbols by Subsystem")
    lines.append("| Subsystem | Baseline Count | Candidate Count | Delta |")
    lines.append("|---|---|---|---|")
    b_cats = sym["baseline_categories"]
    c_cats = sym["canary_categories"]
    for cat in sorted(b_cats.keys()):
        b_cnt = b_cats.get(cat, 0)
        c_cnt = c_cats.get(cat, 0)
        delta = c_cnt - b_cnt
        delta_str = f"+{delta}" if delta > 0 else str(delta)
        lines.append(f"| `{cat}` | {b_cnt:,} | {c_cnt:,} | {delta_str} |")
    lines.append("")

    # Tool Schema Modifications
    if tls["modified_tools"]:
        lines.append("#### Modified Tool Schemas")
        for tool_name, diffs in tls["modified_tools"].items():
            lines.append(f"- **`{tool_name}`**:")
            if diffs["new_required"]:
                lines.append(f"  - Added REQUIRED parameters: {diffs['new_required']}")
            if diffs["added_properties"]:
                lines.append(f"  - Added optional properties: {diffs['added_properties']}")
            if diffs["removed_properties"]:
                lines.append(f"  - Dropped properties: {diffs['removed_properties']}")
            if diffs["type_mismatches"]:
                lines.append(f"  - Type mismatches: {diffs['type_mismatches']}")
        lines.append("")

    # Actionable alerts
    alerts = report_data["alerts"]
    lines.append("### 3. Actionable Drift Alerts & Remediation")
    lines.append("")
    if alerts:
        for a in alerts:
            lines.append(f"#### [{a['severity']}] {a['item']} ({a['category']})")
            lines.append(f"- **Problem**: {a['message']}")
            lines.append(f"- **Action**: `{a['recommendation']}`")
            lines.append("")
    else:
        lines.append("All upstream reference contracts align with Pigasus implementation. Zero unaligned drift detected.")
        lines.append("")

    return "\n".join(lines)


def update_reference_drift_doc(repo_root: pathlib.Path, markdown_section: str):
    """Updates docs/guide/reference-drift.md with the latest audit section."""
    doc_path = repo_root / "docs" / "guide" / "reference-drift.md"
    if not doc_path.is_file():
        return

    content = doc_path.read_text(encoding="utf-8")

    start_marker = "<!-- BEGIN AUTOMATED DRIFT REPORT -->"
    end_marker = "<!-- END AUTOMATED DRIFT REPORT -->"

    block = f"{start_marker}\n{markdown_section}\n{end_marker}"

    if start_marker in content and end_marker in content:
        pattern = re.compile(rf"{re.escape(start_marker)}.*?{re.escape(end_marker)}", re.DOTALL)
        updated = pattern.sub(block, content)
    else:
        updated = content.rstrip() + f"\n\n---\n\n{block}\n"

    doc_path.write_text(updated, encoding="utf-8")


# -----------------------------------------------------------------------------
# 7. Terminal Formatter
# -----------------------------------------------------------------------------

def format_terminal_box(title: str, lines: list[str], status: str = "CLEAN") -> str:
    color_map = {
        "CLEAN": "\033[92m",      # Green
        "INFO": "\033[94m",       # Blue
        "WARNING": "\033[93m",    # Yellow
        "CRITICAL": "\033[91m",   # Red
        "RESET": "\033[0m",
        "BOLD": "\033[1m",
    }

    use_color = sys.stdout.isatty() and "NO_COLOR" not in os.environ
    c_status = color_map.get(status, "") if use_color else ""
    c_reset = color_map["RESET"] if use_color else ""
    c_bold = color_map["BOLD"] if use_color else ""

    width = 72
    out = []
    out.append("┌" + "─" * (width - 2) + "┐")
    title_line = f" {c_bold}{title}{c_reset} [{c_status}{status}{c_reset}] "
    raw_title_len = len(title) + len(status) + 4
    pad = width - 2 - raw_title_len
    out.append(f"│ {c_bold}{title}{c_reset} [{c_status}{status}{c_reset}]" + " " * max(pad, 0) + "│")
    out.append("├" + "─" * (width - 2) + "┤")
    for l in lines:
        cleaned_len = len(re.sub(r'\x1b\[[0-9;]*m', '', l))
        pad_l = width - 4 - cleaned_len
        out.append(f"│ {l}" + " " * max(pad_l, 0) + " │")
    out.append("└" + "─" * (width - 2) + "┘")
    return "\n".join(out)


# -----------------------------------------------------------------------------
# 8. Main CLI Runner
# -----------------------------------------------------------------------------

def run_drift_check(args: argparse.Namespace) -> int:
    timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

    baseline_dir = pathlib.Path(args.baseline_dir).resolve() if args.baseline_dir else REPO / "out" / "extracted-1.5.14"
    baseline_version = args.baseline_version or "1.5.14"

    # 1. Discover baseline data
    funcs_baseline = baseline_dir / "seeds" / "funcs.txt"
    base_symbols = load_pclntab_symbols(funcs_baseline)
    base_flags = list(BASELINE_CLI_FLAGS_1514)
    base_cmds = list(BASELINE_SLASH_COMMANDS_1514)
    base_schemas = load_baseline_tool_schemas(REPO)

    # 2. Discover / simulate candidate
    candidate_bin = find_candidate_binary(args.binary)
    if candidate_bin and not args.simulate_canary:
        canary_version = baseline_version
        try:
            ver_res = subprocess.run([str(candidate_bin), "--version"], capture_output=True, text=True)
            if ver_res.returncode == 0 and ver_res.stdout.strip():
                canary_version = ver_res.stdout.strip().splitlines()[0].strip().lstrip("v")
        except Exception:
            pass

        raw_strings = extract_strings_from_binary(candidate_bin)
        canary_symbols = base_symbols  # fallback to baseline seeds if same hash
        bin_hash = hash_file(candidate_bin)
        id_file = baseline_dir / "identity.txt"
        base_hash = ""
        if id_file.is_file():
            for line in id_file.read_text().splitlines():
                if line.startswith("sha256:"):
                    base_hash = line.split(":", 1)[1].strip()

        if bin_hash != base_hash:
            tmp_seeds = REPO / "out" / "tmp-canary-seeds"
            canary_symbols = extract_symbols_from_binary(candidate_bin, tmp_seeds)

        canary_flags = extract_candidate_flags(candidate_bin, raw_strings)
        canary_cmds = extract_candidate_slash_commands(raw_strings)
        canary_schemas = copy.deepcopy(base_schemas)
    elif args.simulate_canary:
        canary_version = "1.5.15-canary"
        canary_symbols, canary_flags, canary_cmds, canary_schemas = simulate_canary_state(
            base_symbols, base_flags, base_cmds, base_schemas
        )
    else:
        canary_version = baseline_version
        canary_symbols = base_symbols
        canary_flags = set(base_flags)
        canary_cmds = set(base_cmds)
        canary_schemas = copy.deepcopy(base_schemas)

    # 3. Read pigasus code declarations (AST)
    pigasus_flags = extract_pigasus_flags(REPO)
    pigasus_cmds = extract_pigasus_slash_commands(REPO)
    pigasus_ast = parse_go_ast_symbols(REPO)

    # 4. Perform deep comparisons
    all_alerts: List[ActionableAlert] = []

    sym_diff, sym_alerts = analyze_symbol_drift(base_symbols, canary_symbols)
    flg_diff, flg_alerts = analyze_flags_drift(base_flags, canary_flags, pigasus_flags)
    cmd_diff, cmd_alerts = analyze_slash_commands_drift(base_cmds, canary_cmds, pigasus_cmds)
    tls_diff, tls_alerts = deep_compare_tool_schemas(base_schemas, canary_schemas)

    all_alerts.extend(sym_alerts)
    all_alerts.extend(tls_alerts)
    all_alerts.extend(cmd_alerts)
    all_alerts.extend(flg_alerts)

    overall_status = evaluate_overall_severity(all_alerts)

    report_data = {
        "timestamp": timestamp,
        "baseline_version": baseline_version,
        "canary_version": canary_version,
        "overall_status": overall_status,
        "symbols_diff": sym_diff,
        "flags_diff": flg_diff,
        "commands_diff": cmd_diff,
        "tools_diff": tls_diff,
        "pigasus_ast_summary": {
            "declared_methods": len(pigasus_ast["methods"]),
            "declared_types": len(pigasus_ast["types"]),
        },
        "alerts": [a.to_dict() for a in all_alerts],
    }

    # 5. Output to terminal
    summary_lines = [
        f"Baseline: agy {baseline_version} ({baseline_dir.name})",
        f"Candidate: {canary_version}" + (f" ({candidate_bin})" if candidate_bin else " (simulated)"),
        f"Pclntab Symbols: {sym_diff['baseline_count']:,} base vs {sym_diff['canary_count']:,} canary (+{sym_diff['added_count']:,}, -{sym_diff['removed_count']:,})",
        f"CLI Flags:       {flg_diff['baseline_count']} base vs {flg_diff['candidate_count']} candidate (+{len(flg_diff['added_to_upstream'])}, unmapped: {len(flg_diff['unmapped_in_pigasus'])})",
        f"Slash Commands:  {cmd_diff['baseline_count']} base vs {cmd_diff['candidate_count']} candidate (+{len(cmd_diff['added_to_upstream'])}, missing: {len(cmd_diff['missing_in_pigasus'])})",
        f"Tool Schemas:    {tls_diff['baseline_count']} base vs {tls_diff['candidate_count']} candidate (+{len(tls_diff['added_tools'])}, modified: {len(tls_diff['modified_tools'])})",
        f"Total Alerts:    {len(all_alerts)} actionable item(s)",
    ]

    print("\n" + format_terminal_box("Upstream Reference Drift SOP Inspection", summary_lines, overall_status))

    if all_alerts:
        print("\n" + "─" * 72)
        print(" ACTIONABLE PARITY DRIFT ALERTS:")
        print("─" * 72)
        for idx, a in enumerate(all_alerts, 1):
            sev_badge = f"[{a.severity}]"
            print(f" {idx:2d}. {sev_badge:10s} {a.item} ({a.category})")
            print(f"     Cause:  {a.message}")
            print(f"     Action: {a.recommendation}\n")
    else:
        print("\n [✓] All upstream reference contracts are fully aligned with Pigasus. 0 drift detected.\n")

    # 6. Export Reports
    md_report = generate_markdown_report(report_data)

    if args.report_json:
        p_json = pathlib.Path(args.report_json).resolve()
        p_json.parent.mkdir(parents=True, exist_ok=True)
        p_json.write_text(json.dumps(report_data, indent=2), encoding="utf-8")
        print(f"[✓] Saved JSON report: {p_json}")

    if args.report_md:
        p_md = pathlib.Path(args.report_md).resolve()
        p_md.parent.mkdir(parents=True, exist_ok=True)
        p_md.write_text(md_report, encoding="utf-8")
        print(f"[✓] Saved Markdown report: {p_md}")

    # 7. Update docs/guide/reference-drift.md if requested
    if args.update_docs:
        update_reference_drift_doc(REPO, md_report)
        print(f"[✓] Synchronized reference drift documentation: docs/guide/reference-drift.md")

    if args.fail_on_drift and overall_status in ("CRITICAL", "WARNING"):
        return 1
    return 0


def main():
    parser = argparse.ArgumentParser(
        description="Automated upstream reference drift inspector (Reverse-Engineering SOP)",
    )
    parser.add_argument("--binary", help="Path to upstream candidate agy binary")
    parser.add_argument("--baseline-dir", help="Baseline extracted directory (default: out/extracted-1.5.14)")
    parser.add_argument("--baseline-version", help="Baseline version (default: 1.5.14)")
    parser.add_argument("--simulate-canary", action="store_true", help="Simulate 1.5.15+ upstream canary drift for verification")
    parser.add_argument("--update-docs", action="store_true", default=True, help="Update docs/guide/reference-drift.md")
    parser.add_argument("--no-update-docs", dest="update_docs", action="store_false", help="Do not update docs")
    parser.add_argument("--report-json", help="Path to write JSON report")
    parser.add_argument("--report-md", help="Path to write Markdown report")
    parser.add_argument("--fail-on-drift", action="store_true", help="Exit non-zero if critical or warning drift is detected")
    parser.add_argument("--verbose", "-v", action="store_true", help="Verbose output")

    args = parser.parse_args()
    sys.exit(run_drift_check(args))


if __name__ == "__main__":
    main()
