#!/usr/bin/env python3
"""Build a REA MCP plan for the local agy binary.

Addresses are resolved by function NAME from seeds.txt, never hardcoded, because
every agy release shifts all of them (the addresses in
rea-go-seeds/examples/plan-agy.jsonl belong to an older build).

Usage:
    build_plan.py <seeds.txt> <binary> > plan-local.jsonl

Edit the TARGETS list to change what gets decompiled.
"""
import json
import re
import sys

SEEDS, BINARY = sys.argv[1], sys.argv[2]

# (label, regex matched against the Go function name, tool, extra args)
TARGETS = [
    ("hideFlagsForExternal",  r"^main\.hideFlagsForExternal$"),
    ("shouldBypassKeyring",   r"codeassistclient\.shouldBypassKeyring$"),
    ("memoizeKeyringTimeout", r"memoizeKeyringTimeout$"),
    ("LoadToken",             r"compositeTokenStorage\)\.LoadToken$"),
    ("keyringRecentlyUnavail", r"codeassistclient\.keyringRecentlyUnavailable$"),
    ("constructEndpoint",     r"constructCodeAssistEndpoint$"),
    ("RunLogin",              r"auth\.RunLogin$"),
    ("getOauthParams",        r"auth\.getOauthParams$"),
    ("defaultTokenStorage",   r"auth\.defaultTokenStorage$"),
    ("EligibilityError",      r"\(\*AuthStatus\)\.EligibilityError$"),
    ("IneligibilityFromRes",  r"backend\.IneligibilityFromResult$"),
    ("storeWaitForElig",      r"store\.\(\*Manager\)\.waitForEligibility$"),
    ("storeTrySilentAuth",    r"store\.\(\*Manager\)\.TrySilentAuth$"),
    ("storeStartOAuth",       r"store\.\(\*Manager\)\.StartOAuthProcess$"),
    ("storeInitAuth",         r"store\.\(\*Manager\)\.initAuth$"),
    ("storeCommitAuth",       r"store\.\(\*Manager\)\.commitAuth$"),
    ("printmodeWaitForElig",  r"printmode\.waitForEligibility$"),
    ("updaterManifestURL",    r"updater\.GetManifestURLFromBase$"),
]

SEARCHES = [
    ("search-waitForEligibility", "search_procedures", "waitForEligibility"),
    ("search-ineligibleTiers",    "search_strings",   "ineligibleTiers"),
    ("search-CLOUD_CODE_URL",     "search_strings",   "CLOUD_CODE_URL"),
    ("search-CLOUD_CODE_ENV",     "search_strings",   "CLOUD_CODE"),
]

by_name = {}
for line in open(SEEDS):
    parts = line.rstrip("\n").split("\t")
    if len(parts) == 3 and parts[1] == "function":
        by_name.setdefault(parts[2], parts[0])

plan = [{"label": "open", "tool": "open_binary",
         "args": {"path": BINARY, "provider_id": "ghidra"}}]
plan.append({"label": "overview", "tool": "binary_overview", "args": {}})

for label, tool, pattern in SEARCHES:
    plan.append({"label": label, "tool": tool, "args": {"pattern": pattern}})

missing = []
for label, pattern in TARGETS:
    hit = next((addr for name, addr in by_name.items() if re.search(pattern, name)), None)
    if hit is None:
        missing.append(label)
        continue
    plan.append({"label": f"decomp-{label}", "tool": "procedure_pseudo_code",
                 "args": {"procedure": hit}})

plan.append({"label": "close", "tool": "close_binary",
             "args": {"snapshot_path": "./ghidra-snapshot.json", "overwrite": True}})

for step in plan:
    print(json.dumps(step))

print(f"# unresolved targets: {missing}", file=sys.stderr)
