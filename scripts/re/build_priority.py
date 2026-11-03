#!/usr/bin/env python3
"""Select the auth/eligibility-relevant functions from seeds.txt for decompilation.

Same package selection the Linux-sandbox pass used (out/decompile/priority-funcs.txt),
re-expressed as patterns so it can be re-run against any agy build.

Usage:
    build_priority.py <seeds.txt> > priority-funcs.txt
"""
import re
import sys

PACKAGES = [
    # The whole main package is small (137 funcs) and carries the CLI flag surface:
    # hideFlagsForExternal, the daemon commands, host bridge, hub server.
    r"^main\.",
    r"^google3/third_party/jetski/cli/store/store\.",
    r"^google3/third_party/jetski/cli/backend/backend\.",
    r"^google3/third_party/jetski/cli/backend/auth/auth\.",
    r"^google3/third_party/jetski/cli/printmode/printmode\.",
    r"^google3/third_party/jetski/cli/commands/commands\.",
    r"^google3/third_party/jetski/cli/updater/updater\.",
    r"^google3/third_party/jetski/language_server/code_assist_client/codeassistclient\.",
]
# Generated protobuf/grpc boilerplate — huge, low value, slows the run down.
SKIP = re.compile(
    r"\.(Marshal|Unmarshal|Size|String|Reset|ProtoMessage|ProtoReflect|Descriptor|"
    r"Enum|Equal|Get[A-Z][A-Za-z]*)\b"
    r"|_Handler$|go\.(init|func[0-9])$|\.(deferwrap|gowrap)[0-9]"
)
# Explicitly keep these even if they match SKIP — they are the interesting ones.
KEEP = re.compile(
    r"(\(\*AuthStatus\)\.(EligibilityError|IsAuthenticated|GetAuthState)|"
    r"auth\.(RunLogin|getOauthParams|defaultTokenStorage|Logout)|"
    r"codeassistclient\.(shouldBypassKeyring|keyringRecentlyUnavailable)|"
    r"updater\.GetManifestURLFromBase)"
)

kept = 0
for line in open(sys.argv[1]):
    parts = line.rstrip("\n").split("\t")
    if len(parts) != 3 or parts[1] != "function":
        continue
    _, _, name = parts
    if not any(re.search(p, name) for p in PACKAGES):
        continue
    if SKIP.search(name) and not KEEP.search(name):
        continue
    print(line.rstrip("\n"))
    kept += 1

print(f"# selected {kept} functions", file=sys.stderr)
