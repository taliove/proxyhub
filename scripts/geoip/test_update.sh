#!/usr/bin/env bash
# test_update.sh - plain-bash tests for scripts/geoip/update.sh.
#
# No network: a local directory is served via file:// URLs as the download
# source. Covers trust-on-first-use checksum recording, cache-hit skip, and
# checksum-mismatch failure.
#
# Usage: bash scripts/geoip/test_update.sh
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
UPDATE_SH="$SCRIPT_DIR/update.sh"

PASS=0
FAIL=0
_ok()  { PASS=$((PASS + 1)); }
_bad() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$1" >&2; }

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}';
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

WORK=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-geoip-test.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

MONTH="2099-01"
GZ_NAME="dbip-country-lite-${MONTH}.mmdb.gz"

# Build a fake download source: a gzipped payload standing in for the mmdb.
SRC="$WORK/src"
mkdir -p "$SRC"
printf 'FAKE-MMDB-PAYLOAD-v1' | gzip >"$SRC/$GZ_NAME"

run_update() { # cache embed sha base -> runs update.sh with those knobs
  GEOIP_MONTH="$MONTH" \
  GEOIP_CACHE_DIR="$1" \
  GEOIP_EMBED_TARGET="$2" \
  GEOIP_SHA_DIR="$3" \
  GEOIP_BASE_URL="$4" \
    bash "$UPDATE_SH" >/dev/null 2>&1
}

# --------------------------------------------------------------------------
# Case 1: fresh download records the checksum (TOFU) and stages the embed file.
# --------------------------------------------------------------------------
C1_CACHE="$WORK/c1/cache"; C1_EMBED="$WORK/c1/geoip/dbip-country.mmdb"; C1_SHA="$WORK/c1/sha"
if run_update "$C1_CACHE" "$C1_EMBED" "$C1_SHA" "file://$SRC"; then _ok; else _bad "case1 expected success"; fi
[[ -f "$C1_EMBED" ]] && _ok || _bad "case1 embed target not staged"
if [[ -f "$C1_EMBED" ]] && [[ "$(cat "$C1_EMBED")" == "FAKE-MMDB-PAYLOAD-v1" ]]; then _ok; else _bad "case1 embed content wrong"; fi
SHA_FILE="$C1_SHA/dbip-country-lite-${MONTH}.mmdb.sha256"
[[ -f "$SHA_FILE" ]] && _ok || _bad "case1 sha sidecar not recorded"
if [[ -f "$SHA_FILE" ]] && [[ "$(awk '{print $1}' "$SHA_FILE")" == "$(sha256_of "$C1_EMBED")" ]]; then _ok; else _bad "case1 recorded sha mismatch"; fi

# --------------------------------------------------------------------------
# Case 2: cache present + verified sha -> skip download entirely. Base points at
# an EMPTY dir, so any download attempt would 404 and fail; success proves skip.
# --------------------------------------------------------------------------
EMPTY="$WORK/empty"; mkdir -p "$EMPTY"
C2_EMBED="$WORK/c2/geoip/dbip-country.mmdb"
if run_update "$C1_CACHE" "$C2_EMBED" "$C1_SHA" "file://$EMPTY"; then _ok; else _bad "case2 expected success (cache hit skip)"; fi
[[ -f "$C2_EMBED" ]] && _ok || _bad "case2 embed target not staged from cache"

# --------------------------------------------------------------------------
# Case 3: recorded checksum does not match the downloaded file -> hard failure,
# embed target left untouched.
# --------------------------------------------------------------------------
C3_CACHE="$WORK/c3/cache"; C3_EMBED="$WORK/c3/geoip/dbip-country.mmdb"; C3_SHA="$WORK/c3/sha"
mkdir -p "$C3_SHA"
printf '%s  dbip-country-lite-%s.mmdb\n' "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" "$MONTH" \
  >"$C3_SHA/dbip-country-lite-${MONTH}.mmdb.sha256"
if run_update "$C3_CACHE" "$C3_EMBED" "$C3_SHA" "file://$SRC"; then _bad "case3 expected failure on sha mismatch"; else _ok; fi
[[ ! -f "$C3_EMBED" ]] && _ok || _bad "case3 embed target should not be staged on mismatch"

# --------------------------------------------------------------------------
printf 'passed: %d, failed: %d\n' "$PASS" "$FAIL"
(( FAIL == 0 ))
