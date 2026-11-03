#!/usr/bin/env python3
"""
crawl_official_docs.py — Crawls and discovers all official Google Antigravity markdown documentation
from https://gweb-jetski.appspot.com and saves them to ./official-docs.

Discovery mechanisms:
1. XML sitemaps (/sitemap.xml, /sitemap-0.xml, /sitemap-index.xml)
2. HTML link extraction from key index pages (/docs/getting-started, /docs/overview, etc.)
3. Recursive link extraction from every downloaded markdown document
4. Heuristic candidate probing for unlisted/nested CLI commands, SDK topics, IDE extensions, etc.
"""

import concurrent.futures
import os
import re
import sys
import time
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
from pathlib import Path

BASE_HOST = "https://gweb-jetski.appspot.com"
OUTPUT_DIR = Path("official-docs")
USER_AGENT = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
HEADERS = {"User-Agent": USER_AGENT}

# Known candidate stems for heuristic probing
KNOWN_COMMANDS = [
    "agents", "btw", "changelog", "codesearch", "context", "credits", "diff",
    "effort", "permissions", "resume", "statusline", "title", "usage", "voice",
    "browser", "goal", "grill-me", "learn", "schedule", "plan", "boost",
    "teamwork", "clear", "new", "exit", "quit", "help", "model", "config",
    "continue", "copy", "feature-diff", "featdiff", "fork", "hooks",
    "keybindings", "logout", "login", "mcp", "open", "plugin", "remote-control",
    "rename", "rewind", "skills", "tasks", "record", "add-dir", "artifact",
    "quote", "compact", "branch", "narration", "share", "generative-ui"
]

KNOWN_EXTENSIONS = [
    "vscode", "visual-studio", "jetbrains", "zed", "xcode", "eclipse",
    "cursor", "windsurf", "neovim", "vim", "emacs", "sublime"
]

KNOWN_SDK_TOPICS = [
    "overview", "lifecycle", "local-models", "mcp", "personas", "policies",
    "structured-output", "subagents", "tools", "quickstart", "sessions",
    "storage", "transports", "hooks", "python", "typescript", "go"
]

KNOWN_IDE_TOPICS = [
    "agent-side-panel", "allowlist-denylist", "browser-recordings", "browser",
    "extensions", "review-changes-editor", "separate-chrome-profile", "tab",
    "workflows", "terminal", "composer", "diff-viewer", "settings"
]

KNOWN_GENERAL_DOCS = [
    "agent-settings", "agent", "artifact-review", "artifacts", "at-mentions",
    "boost", "build-with-google", "changelog", "enterprise", "faq", "features",
    "firebase-studio-migration", "getting-started", "hooks", "implementation-plan",
    "install", "marketplace", "mcp", "models", "overview", "permissions",
    "plan", "plans", "plugins", "projects", "remote-control", "review-changes",
    "rules", "sandbox", "screenshots", "search", "settings", "sidecars",
    "skills", "slash-commands", "subagents", "teamwork", "tools", "walkthrough",
    "workflows-to-skills", "security", "privacy", "architecture"
]

def clean_doc_path(path: str) -> str:
    """Normalize /docs/path/ -> /docs/path without trailing slash or .md."""
    p = path.strip()
    if p.startswith("http"):
        parsed = urllib.parse.urlparse(p)
        p = parsed.path
    # Strip leading/trailing slashes, remove .md if present
    p = re.sub(r"^/+", "/", p)
    p = re.sub(r"\.md$", "", p)
    p = p.rstrip("/")
    if not p.startswith("/docs"):
        if p.startswith("/"):
            p = "/docs" + p
        else:
            p = "/docs/" + p
    return p

def fetch_sitemap_paths() -> set[str]:
    paths = set()
    for sm in ["/sitemap.xml", "/sitemap-0.xml", "/sitemap-index.xml"]:
        url = BASE_HOST + sm
        try:
            req = urllib.request.Request(url, headers=HEADERS)
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = resp.read()
                root = ET.fromstring(data)
                for elem in root.iter():
                    if elem.tag.endswith("loc") and elem.text:
                        loc = elem.text.strip()
                        if "/docs" in loc:
                            paths.add(clean_doc_path(loc))
        except Exception as e:
            print(f"[-] Sitemap fetch error {sm}: {e}")
    return paths

def fetch_html_links(doc_path: str) -> set[str]:
    links = set()
    url = BASE_HOST + doc_path
    try:
        req = urllib.request.Request(url, headers=HEADERS)
        with urllib.request.urlopen(req, timeout=10) as resp:
            html = resp.read().decode("utf-8", errors="ignore")
            # Match href="/docs/..."
            matches = re.findall(r'href=[\'"](/docs/[^\'\"#?\s]+)', html)
            for m in matches:
                links.add(clean_doc_path(m))
    except Exception as e:
        pass
    return links

def generate_heuristic_paths() -> set[str]:
    paths = set()
    # General docs
    for d in KNOWN_GENERAL_DOCS:
        paths.add(f"/docs/{d}")
    # CLI commands
    for c in KNOWN_COMMANDS:
        paths.add(f"/docs/cli/commands/{c}")
        paths.add(f"/docs/commands/{c}")
        paths.add(f"/docs/cli/{c}")
    # IDE extensions & topics
    for ext in KNOWN_EXTENSIONS:
        paths.add(f"/docs/ide/extensions/{ext}")
    for ide in KNOWN_IDE_TOPICS:
        paths.add(f"/docs/ide/{ide}")
    # SDK topics
    for sdk in KNOWN_SDK_TOPICS:
        paths.add(f"/docs/sdk/{sdk}")
    return paths

def fetch_markdown(doc_path: str) -> tuple[str, str | None, set[str]]:
    """
    Attempts to fetch doc_path.md from BASE_HOST.
    Returns (doc_path, markdown_content or None, discovered_doc_links).
    """
    md_url = BASE_HOST + doc_path + ".md"
    discovered = set()
    try:
        req = urllib.request.Request(md_url, headers=HEADERS)
        with urllib.request.urlopen(req, timeout=10) as resp:
            ct = resp.headers.get("Content-Type", "").lower()
            if resp.status == 200 and ("markdown" in ct or "text/plain" in ct or resp.headers.get("etag")):
                content = resp.read().decode("utf-8", errors="ignore")
                # Quick verification that it is markdown content (not HTML error redirect)
                if content.startswith("<!doctype") or "<html" in content[:200].lower():
                    return doc_path, None, discovered

                # Extract markdown links: [text](/docs/something) or [text](something.md)
                for link in re.findall(r'\[.*?\]\((/docs/[^)#?\s]+)', content):
                    discovered.add(clean_doc_path(link))
                for link in re.findall(r'\[.*?\]\(([^)#?\s:]+\.md)\)', content):
                    if not link.startswith("http"):
                        # Relative link resolution
                        parent = "/".join(doc_path.split("/")[:-1])
                        resolved = urllib.parse.urljoin(parent + "/", link)
                        discovered.add(clean_doc_path(resolved))

                return doc_path, content, discovered
    except Exception:
        pass
    return doc_path, None, discovered

def save_document(doc_path: str, content: str) -> Path:
    # doc_path is like /docs/cli/commands/agents
    # destination file should be official-docs/cli/commands/agents.md
    rel = doc_path.removeprefix("/docs").lstrip("/")
    if not rel:
        rel = "index"
    dest_path = OUTPUT_DIR / f"{rel}.md"
    dest_path.parent.mkdir(parents=True, exist_ok=True)
    with open(dest_path, "w", encoding="utf-8") as f:
        f.write(content)
    return dest_path

def extract_title(content: str) -> str:
    for line in content.splitlines():
        line = line.strip()
        if line.startswith("# "):
            return line[2:].strip()
    return ""

def main():
    print("[*] Phase 1: Gathering seed URLs from sitemaps...")
    discovered_paths = fetch_sitemap_paths()
    print(f"    Found {len(discovered_paths)} doc paths in sitemaps.")

    print("[*] Phase 2: Scraping HTML navigation from key doc entrypoints...")
    key_entrypoints = [
        "/docs/getting-started",
        "/docs/overview",
        "/docs/features",
        "/docs/cli/reference",
        "/docs/sdk/overview",
        "/docs/ide/extensions",
    ]
    for ep in key_entrypoints:
        html_links = fetch_html_links(ep)
        new_links = html_links - discovered_paths
        if new_links:
            print(f"    {ep}: +{len(new_links)} new links found.")
        discovered_paths.update(html_links)

    print("[*] Phase 3: Adding heuristic candidate paths...")
    heuristics = generate_heuristic_paths()
    discovered_paths.update(heuristics)
    print(f"    Total candidates to probe: {len(discovered_paths)}")

    print("[*] Phase 4: Fetching markdown documents and recursing...")
    visited = set()
    downloaded = {}
    to_visit = set(discovered_paths)

    round_num = 1
    while to_visit:
        print(f"\n--- Crawl Round {round_num}: Probing {len(to_visit)} candidate URLs ---")
        current_batch = list(to_visit)
        visited.update(current_batch)
        to_visit = set()

        with concurrent.futures.ThreadPoolExecutor(max_workers=12) as executor:
            future_to_path = {executor.submit(fetch_markdown, p): p for p in current_batch}
            for future in concurrent.futures.as_completed(future_to_path):
                p, content, new_discovered = future.result()
                if content is not None:
                    dest = save_document(p, content)
                    title = extract_title(content) or dest.stem
                    downloaded[p] = (dest, title, len(content))
                    print(f"  [+] {p}.md -> {dest} ({len(content):,} B) [{title}]")
                    # Add newly discovered links that haven't been visited
                    for n in new_discovered:
                        if n not in visited and n not in to_visit:
                            to_visit.add(n)

        round_num += 1

    print(f"\n[✓] Crawl complete! Successfully downloaded {len(downloaded)} markdown documents.")

    print("[*] Phase 5: Generating SUMMARY.md and INDEX.md...")
    generate_indexes(downloaded)
    print("[✓] Documentation index generated.")

def generate_indexes(downloaded: dict):
    # Group documents by directory/category
    categories = {}
    for p, (dest, title, size) in downloaded.items():
        rel = dest.relative_to(OUTPUT_DIR)
        parts = rel.parts
        if len(parts) == 1:
            cat = "Getting Started & Core Concepts"
        elif parts[0] == "cli":
            cat = "CLI Reference & Usage"
        elif parts[0] == "ide":
            cat = "IDE Extensions & Workflows"
        elif parts[0] == "sdk":
            cat = "Antigravity SDK"
        elif parts[0] == "commands":
            cat = "Slash Commands"
        else:
            cat = parts[0].replace("-", " ").title()

        categories.setdefault(cat, []).append((str(rel), title, size, p))

    index_lines = [
        "# Official Google Antigravity Documentation Index",
        "",
        "> Mirror of official documentation crawled from `https://gweb-jetski.appspot.com/docs/`.",
        f"> Last updated: {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())}",
        f"> Total documents: **{len(downloaded)}**",
        "",
        "## Table of Contents",
        ""
    ]

    for cat in sorted(categories.keys()):
        index_lines.append(f"### {cat}\n")
        index_lines.append("| Title | Document | Size | Official URL |")
        index_lines.append("| :--- | :--- | :--- | :--- |")
        docs = sorted(categories[cat], key=lambda x: x[1].lower())
        for rel, title, size, p in docs:
            web_url = f"https://antigravity.google{p}/"
            index_lines.append(f"| **{title}** | [{rel}](./{rel}) | {size:,} B | [{p}]({web_url}) |")
        index_lines.append("")

    with open(OUTPUT_DIR / "INDEX.md", "w", encoding="utf-8") as f:
        f.write("\n".join(index_lines))

    # Also write README.md pointing to INDEX.md
    with open(OUTPUT_DIR / "README.md", "w", encoding="utf-8") as f:
        f.write("\n".join(index_lines))

if __name__ == "__main__":
    main()
