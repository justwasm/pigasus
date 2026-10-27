#!/usr/bin/env bash
# Install the clone's `cmd/langserver` into a *copy* of the Antigravity IDE
# shell, so the IDE's own Electron app can be run against the clone.
#
# The copy is fully isolated from the original:
#   * a different bundle identifier,
#   * a different product name (the IDE derives its data dir from it),
#   * therefore a different `~/.gemini/<name>` and a different Electron
#     `userData` directory.
#
# The original app is never touched: it is only read (ditto) and the copy is
# what gets rewritten.
#
# Four macOS facts make this non-trivial; each one cost a failed launch:
#
#   1. The bundle's *resource seal* is enforced at exec time. Changing
#      Info.plist / app.asar / the language_server binary invalidates the
#      main executable's signature and the kernel kills the process
#      (amfid: "The signature on the file is invalid", Code=-420). The
#      bundle must be re-signed after the surgery.
#   2. The *hardened runtime* flag must be DROPPED. With it set, library
#      validation rejects the Electron Framework
#      ("mapping process and mapped file (non-platform) have different Team
#      IDs"). The entitlements (allow-jit, automation, audio, camera) are
#      preserved; only the runtime flag is dropped.
#   3. Electron finds the helper app *by name*:
#      `<productName> Helper.app/Contents/MacOS/<productName> Helper`
#      (Electron 44, shell/app/electron_main_delegate_mac.mm). Renaming the
#      product means renaming the four helper bundles and their executables.
#   4. Electron's first choice is `<ELECTRON_PRODUCT_NAME> Helper.app`
#      ("Electron Helper.app"), which does not exist here, so it falls back
#      to the app name. Both are tried; only the second exists.
#
# Usage:
#   scripts/install-ide-shell.sh [--src PATH] [--dst PATH] [--id BUNDLE_ID]
#                                [--name PRODUCT_NAME] [--no-build]
#
# Defaults:
#   --src  /Applications/Antigravity.app
#   --dst  /Applications/Pigasus.app
#   --id   io.github.justwasm.pigasus
#   --name Pigasus

set -euo pipefail

SRC_APP="/Applications/Antigravity.app"
DST_APP="/Applications/Pigasus.app"
BUNDLE_ID="io.github.justwasm.pigasus"
PRODUCT_NAME="Pigasus"
BUILD=1

while [ $# -gt 0 ]; do
  case "$1" in
    --src) SRC_APP="$2"; shift 2 ;;
    --dst) DST_APP="$2"; shift 2 ;;
    --id) BUNDLE_ID="$2"; shift 2 ;;
    --name) PRODUCT_NAME="$2"; shift 2 ;;
    --no-build) BUILD=0; shift ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d "${TMPDIR:-/tmp}/pigasus-shell.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

say() { printf '\n== %s\n' "$*"; }

if [ "$BUILD" = "1" ]; then
  say "build the clone"
  ( cd "$REPO_ROOT" && go build -o "$WORK/language_server" ./cmd/langserver )
fi

say "copy the shell"
rm -rf "$DST_APP"
ditto "$SRC_APP" "$DST_APP"

say "rewrite app.asar's package.json (the product name drives the data dir)"
( cd "$WORK" && npx --yes @electron/asar extract "$DST_APP/Contents/Resources/app.asar" orig )
cp -R "$WORK/orig" "$WORK/patched"
PRODUCT_NAME="$PRODUCT_NAME" python3 - "$WORK/patched/package.json" <<'PY'
import json, os, pathlib, sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text())
data["name"] = os.environ["PRODUCT_NAME"].lower()
data["productName"] = os.environ["PRODUCT_NAME"]
path.write_text(json.dumps(data, indent=2) + "\n")
PY
( cd "$WORK" && npx --yes @electron/asar pack patched repacked.asar \
    --unpack-dir "node_modules/chrome-devtools-mcp" )
# The unpacked directory is a resource of the bundle; keep it as the pack
# produced it (verified byte-identical against the original's).
rm -rf "$DST_APP/Contents/Resources/app.asar.unpacked"
cp -R "$WORK/repacked.asar.unpacked" "$DST_APP/Contents/Resources/app.asar.unpacked"
cp "$WORK/repacked.asar" "$DST_APP/Contents/Resources/app.asar"

say "rewrite Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $BUNDLE_ID" \
                        -c "Set :CFBundleName $PRODUCT_NAME" \
                        -c "Set :CFBundleDisplayName $PRODUCT_NAME" \
                        "$DST_APP/Contents/Info.plist"

say "rename the helper bundles (Electron looks them up by name)"
FRAMEWORKS="$DST_APP/Contents/Frameworks"
for suffix in "" " (GPU)" " (Plugin)" " (Renderer)"; do
  old="Antigravity Helper${suffix}.app"
  new="${PRODUCT_NAME} Helper${suffix}.app"
  [ -d "$FRAMEWORKS/$old" ] || continue
  mv "$FRAMEWORKS/$old" "$FRAMEWORKS/$new"
  mv "$FRAMEWORKS/$new/Contents/MacOS/Antigravity Helper${suffix}" \
     "$FRAMEWORKS/$new/Contents/MacOS/${PRODUCT_NAME} Helper${suffix}"
  /usr/libexec/PlistBuddy -c "Set :CFBundleExecutable ${PRODUCT_NAME} Helper${suffix}" \
                          -c "Set :CFBundleDisplayName ${PRODUCT_NAME} Helper${suffix}" \
                          "$FRAMEWORKS/$new/Contents/Info.plist"
done

say "install the clone as the language server"
cp "$WORK/language_server" "$DST_APP/Contents/Resources/bin/language_server"
chmod 755 "$DST_APP/Contents/Resources/bin/language_server"

say "re-sign (ad-hoc; entitlements kept, the runtime flag dropped)"
codesign --force --sign - "$DST_APP/Contents/Resources/bin/language_server"
for lib in "$FRAMEWORKS/Electron Framework.framework/Versions/A/Libraries/libffmpeg.dylib" \
           "$FRAMEWORKS/Electron Framework.framework/Versions/A/Libraries/libvk_swiftshader.dylib"; do
  [ -e "$lib" ] && codesign --force --sign - --preserve-metadata=entitlements "$lib"
done
for fw in "$FRAMEWORKS/Electron Framework.framework" "$FRAMEWORKS/Mantle.framework" \
          "$FRAMEWORKS/ReactiveObjC.framework" "$FRAMEWORKS/Squirrel.framework"; do
  [ -e "$fw" ] && codesign --force --sign - --preserve-metadata=entitlements "$fw"
done
for helper in "$FRAMEWORKS"/*Helper*.app; do
  codesign --force --sign - --preserve-metadata=entitlements "$helper"
done
codesign --force --sign - --preserve-metadata=entitlements "$DST_APP"

say "verify"
codesign --verify --deep --strict "$DST_APP"
codesign -dv "$DST_APP" 2>&1 | grep -E "Identifier|flags"
"$DST_APP/Contents/Resources/bin/language_server" --stamp

say "done"
echo "app:   $DST_APP"
echo "data:  ~/.gemini/$(echo "$PRODUCT_NAME" | tr '[:upper:]' '[:lower:]')"
echo "logs:  ~/Library/Logs/$PRODUCT_NAME/"
echo "launch: open -a $DST_APP"
