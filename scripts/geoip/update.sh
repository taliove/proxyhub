#!/usr/bin/env bash
#
# update.sh - fetch the DB-IP Lite country mmdb and stage it for go:embed.
#
# The proxyhub binary embeds a country-level GeoIP database (DB-IP Lite,
# CC BY 4.0) so that self-hosted node region resolution is fully offline.
# The binary data file is NEVER committed; this script downloads it into a
# gitignored cache, verifies its SHA256, and copies it into the geoip package
# so `go build` can embed it.
#
# Usage:
#   scripts/geoip/update.sh
#
# Environment knobs (defaults target a real build; tests override them):
#   GEOIP_BASE_URL      Download base (default: https://download.db-ip.com/free).
#                       file:// URLs are supported for offline testing.
#   GEOIP_MONTH         Target month YYYY-MM (default: current UTC month).
#                       On download failure the previous month is retried.
#   GEOIP_CACHE_DIR     Gitignored download cache (default: <repo>/.geoip-cache).
#   GEOIP_EMBED_TARGET  Where the mmdb is copied for go:embed
#                       (default: <repo>/internal/geoip/dbip-country.mmdb).
#   GEOIP_SHA_DIR       Directory holding recorded per-month .sha256 sidecars
#                       (default: <repo>/scripts/geoip).
#
set -Eeuo pipefail

LOG_PREFIX="[proxyhub-geoip]"
# log/fail write to stderr: several helpers echo their result on stdout via
# command substitution, so stdout must carry data only.
log()  { printf '%s %s\n' "$LOG_PREFIX" "$*" >&2; }
fail() { printf '%s ERROR: %s\n' "$LOG_PREFIX" "$*" >&2; exit 1; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

BASE_URL="${GEOIP_BASE_URL:-https://download.db-ip.com/free}"
MONTH="${GEOIP_MONTH:-$(date -u +%Y-%m)}"
CACHE_DIR="${GEOIP_CACHE_DIR:-$repo_root/.geoip-cache}"
EMBED_TARGET="${GEOIP_EMBED_TARGET:-$repo_root/internal/geoip/dbip-country.mmdb}"
SHA_DIR="${GEOIP_SHA_DIR:-$repo_root/scripts/geoip}"

# prev_month YYYY-MM -> the calendar month before it (portable, no GNU date).
prev_month() {
  local y="${1%%-*}" m="${1##*-}"
  m=$((10#$m - 1))
  if (( m == 0 )); then m=12; y=$((y - 1)); fi
  printf '%04d-%02d\n' "$y" "$m"
}

sha256() { # file -> hex digest on stdout
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    fail "no sha256 tool (sha256sum/shasum) available"
  fi
}

# download_extract MONTH -> writes "$CACHE_DIR/dbip-country-lite-MONTH.mmdb",
# echoes the mmdb path on success, returns non-zero on failure.
download_extract() {
  local month="$1"
  local gz="$CACHE_DIR/dbip-country-lite-${month}.mmdb.gz"
  local mmdb="$CACHE_DIR/dbip-country-lite-${month}.mmdb"
  local url="$BASE_URL/dbip-country-lite-${month}.mmdb.gz"

  log "downloading $url"
  if ! curl -fsSL "$url" -o "$gz" 2>/dev/null; then
    rm -f "$gz"
    return 1
  fi
  if ! gunzip -c "$gz" >"$mmdb" 2>/dev/null; then
    rm -f "$gz" "$mmdb"
    return 1
  fi
  rm -f "$gz"
  printf '%s\n' "$mmdb"
}

main() {
  command -v curl >/dev/null 2>&1 || fail "curl is required"
  mkdir -p "$CACHE_DIR"

  local mmdb="" resolved="$MONTH"
  local sha_file="$SHA_DIR/dbip-country-lite-${MONTH}.mmdb.sha256"
  local cached="$CACHE_DIR/dbip-country-lite-${MONTH}.mmdb"

  # Fast path: verified cache present -> skip download entirely.
  if [[ -f "$cached" && -f "$sha_file" ]]; then
    local want got
    want="$(awk '{print $1}' "$sha_file")"
    got="$(sha256 "$cached")"
    if [[ "$want" == "$got" ]]; then
      log "cache hit for $MONTH (sha256 verified), skipping download"
      mmdb="$cached"
    fi
  fi

  # Download path: current month, then previous month as fallback.
  if [[ -z "$mmdb" ]]; then
    for m in "$MONTH" "$(prev_month "$MONTH")"; do
      if mmdb="$(download_extract "$m")"; then
        resolved="$m"
        sha_file="$SHA_DIR/dbip-country-lite-${m}.mmdb.sha256"
        break
      fi
      log "month $m unavailable, trying fallback"
      mmdb=""
    done
    [[ -n "$mmdb" ]] || fail "could not download DB-IP Lite country mmdb (tried $MONTH and $(prev_month "$MONTH"))"

    local got
    got="$(sha256 "$mmdb")"
    if [[ -f "$sha_file" ]]; then
      local want
      want="$(awk '{print $1}' "$sha_file")"
      [[ "$want" == "$got" ]] || fail "sha256 mismatch for $resolved: recorded $want, downloaded $got"
      log "sha256 verified against $sha_file"
    else
      mkdir -p "$SHA_DIR"
      printf '%s  dbip-country-lite-%s.mmdb\n' "$got" "$resolved" >"$sha_file"
      log "recorded sha256 for $resolved -> $sha_file"
    fi
  fi

  mkdir -p "$(dirname "$EMBED_TARGET")"
  cp "$mmdb" "$EMBED_TARGET"
  log "staged $(basename "$mmdb") -> ${EMBED_TARGET#"$repo_root"/} ($(sha256 "$EMBED_TARGET" | cut -c1-12)...)"
}

main "$@"
