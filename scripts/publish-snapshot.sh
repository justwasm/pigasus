#!/usr/bin/env bash
# =============================================================================
# scripts/publish-snapshot.sh — Automated snapshot release from openagy to pigasus
#
# Usage:
#   ./scripts/publish-snapshot.sh [options]
#
# Options:
#   -r, --repo <slug>        Target GitHub repository (default: justwasm/pigasus)
#   -t, --tag <tag>          Release tag (default: v<version> from internal/cli/version.go)
#   -d, --target-dir <dir>   Target local directory (default: ../pigasus or temp dir)
#   -m, --message <msg>      Commit message for snapshot
#   -w, --watch              Watch GitHub Actions run after tag push
#   --no-tag                 Sync main branch without creating or pushing tag
#   -f, --force-tag          Overwrite tag if it already exists remotely
#   -n, --dry-run            Dry run without making commits or pushing
#   -h, --help               Show this help message
# =============================================================================

set -euo pipefail

# Find script directory and openagy root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Default configurations
REPO="justwasm/pigasus"
TARGET_DIR="${ROOT_DIR}/../pigasus"
CUSTOM_TAG=""
NO_TAG=false
FORCE_TAG=false
DRY_RUN=false
WATCH=false
CUSTOM_MSG=""

# Colors
C_RESET="\033[0m"
C_GREEN="\033[32m"
C_YELLOW="\033[33m"
C_CYAN="\033[36m"
C_RED="\033[31m"

log_info()  { echo -e "${C_CYAN}ℹ${C_RESET} $*"; }
log_ok()    { echo -e "${C_GREEN}✓${C_RESET} $*"; }
log_warn()  { echo -e "${C_YELLOW}⚠${C_RESET} $*"; }
log_err()   { echo -e "${C_RED}✗${C_RESET} $*" >&2; }

show_help() {
  cat << USAGE
Usage: $(basename "$0") [options]

Automate syncing a clean snapshot of openagy to justwasm/pigasus and releasing.

Options:
  -r, --repo <slug>        Target GitHub repository (default: justwasm/pigasus)
  -t, --tag <tag>          Release tag (default: v<version> from internal/cli/version.go)
  -d, --target-dir <dir>   Target local directory (default: ../pigasus or temp dir)
  -m, --message <msg>      Commit message for snapshot
  -w, --watch              Watch GitHub Actions run after tag push
  --no-tag                 Sync main branch without creating or pushing tag
  -f, --force-tag          Overwrite tag if it already exists remotely
  -n, --dry-run            Dry run without making commits or pushing
  -h, --help               Show this help message
USAGE
}

# Parse flags
while [[ $# -gt 0 ]]; do
  case "$1" in
    -r|--repo) REPO="$2"; shift 2 ;;
    -t|--tag) CUSTOM_TAG="$2"; shift 2 ;;
    -d|--target-dir) TARGET_DIR="$2"; shift 2 ;;
    -m|--message) CUSTOM_MSG="$2"; shift 2 ;;
    -w|--watch) WATCH=true; shift ;;
    --no-tag) NO_TAG=true; shift ;;
    -f|--force-tag) FORCE_TAG=true; shift ;;
    -n|--dry-run) DRY_RUN=true; shift ;;
    -h|--help) show_help; exit 0 ;;
    *) log_err "Unknown option: $1"; show_help; exit 1 ;;
  esac
done

# Prerequisite checks
command -v git >/dev/null 2>&1 || { log_err "git is required but not installed"; exit 1; }
command -v gh >/dev/null 2>&1 || { log_err "gh is required but not installed"; exit 1; }
command -v rsync >/dev/null 2>&1 || { log_err "rsync is required but not installed"; exit 1; }

if ! gh auth status >/dev/null 2>&1; then
  log_err "gh is not authenticated. Please run 'gh auth login' first."
  exit 1
fi

# Detect version
VERSION_FILE="${ROOT_DIR}/internal/cli/version.go"
if [[ ! -f "$VERSION_FILE" ]]; then
  log_err "Version file not found: $VERSION_FILE"
  exit 1
fi

DETECTED_VERSION=$(grep -E 'ReferenceVersion\s*=' "$VERSION_FILE" | sed -E 's/.*"([^"]+)".*/\1/')
TAG="${CUSTOM_TAG:-v${DETECTED_VERSION}}"

log_info "Source: ${ROOT_DIR}"
log_info "Target repo: ${REPO}"
log_info "Version: ${DETECTED_VERSION} (Tag: ${TAG})"

# Prepare target directory
TEMP_DIR_CREATED=false
if [[ ! -d "$TARGET_DIR/.git" ]]; then
  if [[ -d "$TARGET_DIR" ]]; then
    log_warn "$TARGET_DIR exists but is not a git repository. Using temporary clone."
  fi
  TARGET_DIR=$(mktemp -d "/tmp/pigasus-publish-XXXXXX")
  TEMP_DIR_CREATED=true
  log_info "Cloning ${REPO} into temporary workspace: ${TARGET_DIR}..."
  if ! $DRY_RUN; then
    gh repo clone "$REPO" "$TARGET_DIR" -- --depth=1
  fi
else
  log_info "Using existing local clone: ${TARGET_DIR}"
  if ! $DRY_RUN; then
    git -C "$TARGET_DIR" checkout main
    git -C "$TARGET_DIR" pull --ff-only origin main || true
  fi
fi

cleanup() {
  if $TEMP_DIR_CREATED && [[ -d "$TARGET_DIR" ]]; then
    log_info "Cleaning up temporary directory ${TARGET_DIR}..."
    rm -rf "$TARGET_DIR"
  fi
}
trap cleanup EXIT

# Snapshot Allowlist
#
# `docs/guide`, `docs/cycles` and `docs/research` are the only `docs/` subtrees
# that ship: they are what README.md and AGENTS.md link to, and together they are
# ~7 MB. The rest of `docs/` is RE artefacts (decompiled dumps, Ghidra
# projects, 500+ MB) and stays out of the published tree.
ALLOWLIST=(
  cmd
  internal
  devutils
  rea-go-seeds
  scripts
  .github
  docs/guide
  docs/cycles
  docs/research
  go.mod
  go.sum
  AGENTS.md
  README.md
  LICENSE
  .gitignore
)

log_info "Syncing snapshot allowlist (${ALLOWLIST[*]})..."

if $DRY_RUN; then
  log_info "[DRY-RUN] Would sync allowlist items to ${TARGET_DIR}"
else
  for item in "${ALLOWLIST[@]}"; do
    if [[ -d "${ROOT_DIR}/${item}" ]]; then
      rsync -av --delete \
        --exclude="*.log" \
        --exclude="*.err" \
        --exclude="pclnseeds" \
        --exclude="pclnseeds-v" \
        --exclude="__pycache__" \
        --exclude=".DS_Store" \
        "${ROOT_DIR}/${item}/" "${TARGET_DIR}/${item}/"
    elif [[ -f "${ROOT_DIR}/${item}" ]]; then
      cp "${ROOT_DIR}/${item}" "${TARGET_DIR}/${item}"
    fi
  done

  # Clean up publish-snapshot.sh from pigasus itself if desired, or keep it
  # Ensure large precompiled binaries are never kept in target
  rm -f "${TARGET_DIR}/rea-go-seeds/pclnseeds" "${TARGET_DIR}/rea-go-seeds/pclnseeds-v"
fi

if $DRY_RUN; then
  log_info "[DRY-RUN] Snapshot syncing verified."
  exit 0
fi

OPENAGY_SHA=$(git -C "$ROOT_DIR" rev-parse --short HEAD)
COMMIT_MSG="${CUSTOM_MSG:-feat: sync snapshot from openagy at ${OPENAGY_SHA}}"

STATUS=$(git -C "$TARGET_DIR" status --porcelain)
if [[ -n "$STATUS" ]]; then
  log_info "Changes detected in target repository:"
  git -C "$TARGET_DIR" status -s
  log_info "Committing and pushing..."
  git -C "$TARGET_DIR" add .
  git -C "$TARGET_DIR" commit -m "$COMMIT_MSG"
  git -C "$TARGET_DIR" push origin main
  log_ok "Main branch updated successfully."
else
  log_info "No code changes detected in snapshot."
fi

# Tagging and Release
if $NO_TAG; then
  log_info "--no-tag set; skipping release tag creation."
  exit 0
fi

REMOTE_TAG_EXISTS=$(git -C "$TARGET_DIR" ls-remote --tags origin "refs/tags/${TAG}" | wc -l | tr -d ' ')

TAG_PUSHED=false
if [[ "$REMOTE_TAG_EXISTS" -gt 0 ]]; then
  if $FORCE_TAG; then
    log_warn "Tag ${TAG} already exists on remote. Overwriting (--force-tag)..."
    git -C "$TARGET_DIR" tag -d "$TAG" || true
    git -C "$TARGET_DIR" push --delete origin "$TAG" || true
    git -C "$TARGET_DIR" tag -a "$TAG" -m "Pigasus CLI ${TAG}"
    git -C "$TARGET_DIR" push origin "$TAG"
    TAG_PUSHED=true
    log_ok "Tag ${TAG} re-pushed."
  else
    log_info "Tag ${TAG} already exists on remote. To re-release this tag, use --force-tag."
  fi
else
  log_info "Creating and pushing tag ${TAG}..."
  git -C "$TARGET_DIR" tag -a "$TAG" -m "Pigasus CLI ${TAG}"
  git -C "$TARGET_DIR" push origin "$TAG"
  TAG_PUSHED=true
  log_ok "Tag ${TAG} pushed. GitHub Actions release workflow triggered!"
fi

log_ok "Release URL: https://github.com/${REPO}/releases/tag/${TAG}"

if $WATCH; then
  if ! $TAG_PUSHED; then
    log_warn "The tag was not (re-)pushed, so there is no new run to watch."
  else
    log_info "Watching the release workflow for ${REPO}..."
    # The release run starts a moment after the tag lands. The main-branch
    # push queued a CI run too, so filter by workflow *and* by the tag —
    # otherwise the watch lands on the wrong run.
    RUN_ID=""
    for _ in $(seq 1 20); do
      RUN_ID=$(gh run list --repo "$REPO" --workflow release.yml --limit 10 \
        --json databaseId,headBranch \
        -q ".[] | select(.headBranch == \"${TAG}\") | .databaseId" 2>/dev/null | head -1)
      [[ -n "$RUN_ID" ]] && break
      sleep 3
    done
    if [[ -n "$RUN_ID" ]]; then
      gh run watch "$RUN_ID" --repo "$REPO"
    else
      log_warn "No release run for ${TAG} yet — check ${REPO}/actions manually."
    fi
  fi
fi

log_ok "Done!"
