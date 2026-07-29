#!/usr/bin/env bash
#
# verify.sh — verify release tarballs against a SHA256SUMS manifest.
#
# Usage:
#   scripts/release/verify.sh dist/release                 # verify everything listed in dist/release/SHA256SUMS
#   scripts/release/verify.sh dist/release/proxyhub_0.1.0_linux_amd64.tar.gz [more.tar.gz ...]
#
set -Eeuo pipefail

LOG_PREFIX="[proxyhub-release]"

log()  { printf '%s %s\n' "$LOG_PREFIX" "$*"; }
fail() { printf '%s ERROR: %s\n' "$LOG_PREFIX" "$*" >&2; exit 1; }

sha256() { # file
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

expected_for() { # sums_file asset_name
  awk -v name="$2" '$2 == name {print $1; found=1} END {exit !found}' "$1"
}

verify_one() { # tarball sums_file
  local tarball="$1" sums="$2" name expected actual
  [[ -f "$tarball" ]] || { log "missing tarball: $tarball"; return 1; }
  name="$(basename "$tarball")"
  expected="$(expected_for "$sums" "$name")" || { log "no entry for $name in $sums"; return 1; }
  actual="$(sha256 "$tarball")" || { log "no sha256sum/shasum tool available"; return 1; }
  if [[ "$actual" != "$expected" ]]; then
    log "MISMATCH $name"
    log "  expected: $expected"
    log "  actual:   $actual"
    return 1
  fi
  log "OK $name"
}

main() {
  (($# >= 1)) || fail "usage: verify.sh <dir-with-SHA256SUMS | tarball.tar.gz [...]>"

  local dir sums
  if [[ -d "$1" ]]; then
    dir="$1"
    sums="$dir/SHA256SUMS"
    [[ -f "$sums" ]] || fail "no SHA256SUMS in $dir"
    local failures=0 count=0 asset
    while IFS= read -r asset; do
      [[ -n "$asset" ]] || continue
      count=$((count + 1))
      verify_one "$dir/$asset" "$sums" || failures=$((failures + 1))
    done < <(awk '{print $2}' "$sums")
    ((count > 0)) || fail "$sums lists no assets"
    ((failures == 0)) || fail "$failures of $count asset(s) failed verification"
    log "all $count asset(s) verified against $sums"
    return 0
  fi

  local tarball
  for tarball in "$@"; do
    sums="$(dirname "$tarball")/SHA256SUMS"
    [[ -f "$sums" ]] || fail "no SHA256SUMS next to $tarball"
    verify_one "$tarball" "$sums" || fail "verification failed for $tarball"
  done
  log "all tarball(s) verified"
}

main "$@"
