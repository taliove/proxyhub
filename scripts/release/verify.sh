#!/usr/bin/env bash
#
# verify.sh — verify release tarballs against a SHA256SUMS manifest, plus the
# minisign signature over that manifest (the trust anchor, ADR 0036).
#
# Usage:
#   scripts/release/verify.sh dist/release                 # verify everything listed in dist/release/SHA256SUMS
#   scripts/release/verify.sh dist/release/proxyhub_0.1.0_linux_amd64.tar.gz [more.tar.gz ...]
#
# Environment knobs:
#   MINISIGN_PUBKEY_B64  base64 minisign public key overriding the embedded
#                        release key (used by local rehearsals with a
#                        throwaway key pair).
#   MINISIGN_KEY_FILE    set by package.sh when signing was requested; if a
#                        directory then lacks SHA256SUMS.minisig, that is an
#                        error (a signed release must not silently degrade).
#
set -Eeuo pipefail

LOG_PREFIX="[proxyhub-release]"

log()  { printf '%s %s\n' "$LOG_PREFIX" "$*"; }
fail() { printf '%s ERROR: %s\n' "$LOG_PREFIX" "$*" >&2; exit 1; }

# verify_minisig (openssl-based, no minisign dependency) lives in the shared
# install library next to the embedded release public key.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=../install/lib.sh
# shellcheck disable=SC1090,SC1091
source "$repo_root/scripts/install/lib.sh"

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

# verify_dir_signature DIR - minisign trust-anchor check for DIR/SHA256SUMS.
# Policy (ADR 0036): when a directory holds release tarballs AND
# SHA256SUMS.minisig, the signature must verify against the embedded public
# key (MINISIGN_PUBKEY_B64 overrides it for rehearsal keys). A directory with
# tarballs but NO .minisig is an error only when signing was explicitly
# requested (MINISIGN_KEY_FILE set) - a signed release must not silently
# degrade; otherwise it is an unsigned local rehearsal and only warns.
verify_dir_signature() {
  local dir="$1"
  local sums="$dir/SHA256SUMS" minisig="$dir/SHA256SUMS.minisig"
  if [[ -f "$minisig" ]] && compgen -G "$dir/proxyhub_*.tar.gz" >/dev/null; then
    if [[ -n "${MINISIGN_PUBKEY_B64:-}" ]]; then
      verify_minisig "$sums" "$minisig" "$MINISIGN_PUBKEY_B64" || return 1
    else
      verify_minisig "$sums" "$minisig" || return 1
    fi
    log "OK SHA256SUMS.minisig (minisign signature over SHA256SUMS)"
    return 0
  fi
  if [[ ! -f "$minisig" && -n "${MINISIGN_KEY_FILE:-}" ]]; then
    log "SHA256SUMS.minisig missing in $dir but MINISIGN_KEY_FILE is set - signing was requested, refusing to pass"
    return 1
  fi
  log "WARN: no SHA256SUMS.minisig in $dir - skipping signature check (unsigned local rehearsal)"
  return 0
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
    verify_dir_signature "$dir" || fail "signature verification failed in $dir"
    return 0
  fi

  local tarball
  local -A seen_dirs=()
  for tarball in "$@"; do
    sums="$(dirname "$tarball")/SHA256SUMS"
    [[ -f "$sums" ]] || fail "no SHA256SUMS next to $tarball"
    verify_one "$tarball" "$sums" || fail "verification failed for $tarball"
    dir="$(dirname "$tarball")"
    if [[ -z "${seen_dirs[$dir]:-}" ]]; then
      seen_dirs[$dir]=1
      verify_dir_signature "$dir" || fail "signature verification failed in $dir"
    fi
  done
  log "all tarball(s) verified"
}

main "$@"
