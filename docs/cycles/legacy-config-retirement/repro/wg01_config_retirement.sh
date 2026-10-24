#!/usr/bin/env bash
# wg01_config_retirement.sh — cycle legacy-config-retirement: the live proof
# that the flat configuration file is gone and the provider-centric file is
# the only configuration the clone and the wrapper read.
#
# The capture runs the built binaries against two fixtures:
#
#   1. a flat models.json on its own  → the catalogue is empty, because
#      nothing reads the flat file any more;
#   2. the same two models in providers.json → the catalogue has them.
#
# It also shows the wrapper's own view (`wrapper models list`) and the
# clone's `agy models` table.
#
# Usage: wg01_config_retirement.sh [clone-binary] [wrapper-binary] [outdir]
#        defaults: /tmp/wagyu  /tmp/agy-wrapper  ./captures
#
# The run is offline: the binaries are only asked to *list* the catalogue.
# No request is sent to any endpoint.

set -u

BIN=${1:-/tmp/wagyu}
WRAP=${2:-/tmp/agy-wrapper}
OUT=${3:-./captures}
DIR=$(mktemp -d "${TMPDIR:-/tmp}/wg01-config.XXXXXX")

mkdir -p "$OUT"

# ---- fixture 1: the flat form, on its own ------------------------------
cat > "$DIR/models.json" <<'JSON'
{
  "models": [
    {
      "name": "minimax-m3",
      "display_name": "MiniMax M3",
      "provider": "agw",
      "api_key": "-",
      "api_url": "https://agw.example/v1/chat/completions",
      "external_model_name": "MiniMax-M3",
      "context_window": 1000000
    }
  ]
}
JSON

echo "== fixture 1: flat models.json only ==" > "$OUT/01-flat-only.txt"
env OPENAGY_PROVIDERS_CONFIG="$DIR/absent-providers.json" \
  OPENAGY_MODELS_CONFIG="$DIR/models.json" \
  "$BIN" models >> "$OUT/01-flat-only.txt" 2>&1
env OPENAGY_PROVIDERS_CONFIG="$DIR/absent-providers.json" \
  OPENAGY_MODELS_CONFIG="$DIR/models.json" \
  "$WRAP" wrapper models list >> "$OUT/01-flat-only.txt" 2>&1

# ---- fixture 2: the provider-centric form ------------------------------
cat > "$DIR/providers.json" <<'JSON'
{
  "providers": {
    "agw": {
      "id": "agw",
      "name": "AGW",
      "type": "openai",
      "api_url": "https://agw.example/v1/chat/completions",
      "api_key": "-",
      "models": [
        {
          "id": "minimax-m3",
          "name": "MiniMax M3",
          "model": "MiniMax-M3",
          "context_window": 1000000
        }
      ]
    }
  }
}
JSON

echo "== fixture 2: providers.json ==" > "$OUT/02-provider-form.txt"
env OPENAGY_PROVIDERS_CONFIG="$DIR/providers.json" \
  OPENAGY_MODELS_CONFIG="$DIR/models.json" \
  "$BIN" models >> "$OUT/02-provider-form.txt" 2>&1
env OPENAGY_PROVIDERS_CONFIG="$DIR/providers.json" \
  OPENAGY_MODELS_CONFIG="$DIR/models.json" \
  "$WRAP" wrapper models list >> "$OUT/02-provider-form.txt" 2>&1

echo "fixture: $DIR"
echo "captures: $OUT"
