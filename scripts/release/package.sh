#!/usr/bin/env bash
#
# package.sh — build reproducible ProxyHub release tarballs.
#
# The binary embeds Xray-core and the frontend via go:embed; there is NO
# external Xray download anywhere in this pipeline.
#
# Usage:
#   scripts/release/package.sh
#
# Environment knobs:
#   SKIP_FRONTEND=1        Skip `npm ci && npm run build` (use the frontend
#                          already present in cmd/server/web).
#   TARGETS="linux/amd64"  Space-separated GOOS/GOARCH list to build instead
#                          of the full matrix.
#   SOURCE_DATE_EPOCH=...  Unix timestamp stamped into tar entries for
#                          reproducibility. Default: commit time of HEAD
#                          (git log -1 --format=%ct). Same tree + same epoch
#                          => byte-identical tarballs.
#   OUTPUT_DIR=path        Output directory (default: dist/release).
#   MINISIGN_KEY_FILE=path minisign private key file. When set, SHA256SUMS is
#                          signed into SHA256SUMS.minisig (the CI release path,
#                          ADR 0036). When unset, signing is skipped with a
#                          WARN so local rehearsals stay unblocked.
#
set -Eeuo pipefail

LOG_PREFIX="[proxyhub-release]"

log()  { printf '%s %s\n' "$LOG_PREFIX" "$*"; }
fail() { printf '%s ERROR: %s\n' "$LOG_PREFIX" "$*" >&2; exit 1; }

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

# --- Release metadata -------------------------------------------------------

read_version() {
  [[ -f VERSION ]] || fail "VERSION file is missing (single source of truth)"
  local v
  v="$(tr -d '[:space:]' <VERSION)"
  v="${v#v}"
  local semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$'
  [[ "$v" =~ $semver ]] || fail "VERSION '$v' is not valid SemVer (build metadata '+' is not allowed)"
  printf '%s\n' "$v"
}

VERSION="$(read_version)"
COMMIT="$(git rev-parse HEAD 2>/dev/null)" || fail "must run inside a git checkout"
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || true)}"
[[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || fail "SOURCE_DATE_EPOCH must be a non-negative integer"
OUTPUT_DIR="${OUTPUT_DIR:-$repo_root/dist/release}"

# Build timestamp embedded via -X main.buildTime. Derived from
# SOURCE_DATE_EPOCH (not wall clock) so release builds stay reproducible.
# GNU date uses -d @epoch, BSD date uses -r epoch.
if date --version >/dev/null 2>&1; then
  BUILD_TIME="$(date -u -d "@$SOURCE_DATE_EPOCH" '+%Y-%m-%d_%H:%M:%S')"
else
  BUILD_TIME="$(date -u -r "$SOURCE_DATE_EPOCH" '+%Y-%m-%d_%H:%M:%S')"
fi

# Release targets: linux is what we ship; darwin/windows are dev/manual.
TARGETS="${TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64}"

log "version=$VERSION commit=${COMMIT:0:12} source_date_epoch=$SOURCE_DATE_EPOCH"
log "targets: $TARGETS"

# --- Frontend (go:embed source) ---------------------------------------------

build_frontend() {
  if [[ "${SKIP_FRONTEND:-0}" == "1" ]]; then
    log "SKIP_FRONTEND=1 — using existing cmd/server/web"
  else
    log "building frontend (npm ci && npm run build)"
    (cd web && npm ci && npm run build) || return 1
  fi
  [[ -f cmd/server/web/index.html ]] || { log "cmd/server/web/index.html missing"; return 1; }
}

# --- Go build ----------------------------------------------------------------

build_binary() { # os arch output
  local os="$1" arch="$2" out="$3"
  log "building $os/$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$VERSION -X main.buildTime=$BUILD_TIME" \
    -o "$out" ./cmd/server || return 1
}

# --- Staging + deterministic archive ----------------------------------------

stage_target() { # os arch stage_dir binary_source
  local os="$1" arch="$2" stage="$3" binary="$4"
  local name="proxyhub"
  [[ "$os" == "windows" ]] && name="proxyhub.exe"
  mkdir -p "$stage"
  install -m 0755 "$binary" "$stage/$name" || return 1
  install -m 0644 config.example.yaml "$stage/config.example.yaml" || return 1
  install -m 0644 README.md "$stage/README.md" || return 1
  [[ ! -f LICENSE ]] || install -m 0644 LICENSE "$stage/LICENSE" || return 1
}

sha256() { # file
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

# sign_sums PUBLISH_DIR - sign PUBLISH_DIR/SHA256SUMS into SHA256SUMS.minisig
# with minisign when MINISIGN_KEY_FILE points at a private key file. The
# release workflow always sets it (and fails earlier when the secret is
# absent), so a missing key here only happens in local rehearsals: warn and
# leave the manifest unsigned. Key material is never logged.
sign_sums() {
  local dir="$1"
  if [[ -z "${MINISIGN_KEY_FILE:-}" ]]; then
    log "WARN: MINISIGN_KEY_FILE not set - leaving SHA256SUMS unsigned (local rehearsal only; releases must be signed)"
    return 0
  fi
  [[ -f "$MINISIGN_KEY_FILE" ]] || { log "MINISIGN_KEY_FILE '$MINISIGN_KEY_FILE' does not exist"; return 1; }
  command -v minisign >/dev/null 2>&1 || { log "minisign is required to sign SHA256SUMS"; return 1; }
  # Legacy (non-prehashed) format is mandatory: minisign >= 0.11 prehashes by
  # default, and the openssl verifier in scripts/install/lib.sh only accepts
  # the legacy "Ed" signature layout.
  minisign -S -l -s "$MINISIGN_KEY_FILE" -x "$dir/SHA256SUMS.minisig" -m "$dir/SHA256SUMS" || return 1
  [[ -f "$dir/SHA256SUMS.minisig" ]] || { log "minisign produced no SHA256SUMS.minisig"; return 1; }
  log "signed SHA256SUMS -> SHA256SUMS.minisig"
}

# --- Main --------------------------------------------------------------------

workspace=""
cleanup() {
  local code=$?
  trap - EXIT HUP INT TERM
  [[ -z "$workspace" ]] || rm -rf -- "$workspace"
  exit "$code"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

main() {
  command -v go  >/dev/null 2>&1 || fail "go toolchain is required"
  build_frontend || fail "frontend build failed"

  workspace="$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-release.XXXXXX")"
  local built="$workspace/bin" publish="$workspace/publish"
  mkdir -p "$built" "$publish"

  # Build the deterministic-archiver once, then reuse it for every target.
  go build -trimpath -o "$workspace/archive" ./scripts/release/archive.go \
    || fail "could not build archive helper"

  local asset assets=() target os arch binary stage
  for target in $TARGETS; do
    os="${target%%/*}"
    arch="${target##*/}"
    [[ "$os" != "$arch" && -n "$arch" ]] || fail "invalid target '$target' (want os/arch)"
    binary="$built/${os}_${arch}"
    stage="$workspace/stage/${os}_${arch}"
    build_binary "$os" "$arch" "$binary" || fail "go build failed for $os/$arch"
    stage_target "$os" "$arch" "$stage" "$binary" || fail "staging failed for $os/$arch"
    asset="proxyhub_${VERSION}_${os}_${arch}.tar.gz"
    "$workspace/archive" -input "$stage" -output "$publish/$asset" -epoch "$SOURCE_DATE_EPOCH" \
      || fail "archive failed for $os/$arch"
    assets+=("$asset")
    log "packaged $asset"
  done

  # Combined checksum manifest, sorted by asset name for stability.
  local sums_tmp="$workspace/SHA256SUMS"
  : >"$sums_tmp"
  while IFS= read -r asset; do
    printf '%s  %s\n' "$(sha256 "$publish/$asset")" "$asset" >>"$sums_tmp" \
      || fail "sha256 tool unavailable"
  done < <(printf '%s\n' "${assets[@]}" | LC_ALL=C sort)
  mv "$sums_tmp" "$publish/SHA256SUMS"

  sign_sums "$publish" || fail "could not sign SHA256SUMS"

  mkdir -p "$OUTPUT_DIR"
  rm -f "$OUTPUT_DIR"/proxyhub_*.tar.gz "$OUTPUT_DIR/SHA256SUMS" "$OUTPUT_DIR/SHA256SUMS.minisig"
  cp "$publish"/* "$OUTPUT_DIR/" || fail "could not publish to $OUTPUT_DIR"

  log "release assets written to $OUTPUT_DIR:"
  local f
  for f in "$OUTPUT_DIR"/proxyhub_*.tar.gz "$OUTPUT_DIR/SHA256SUMS" "$OUTPUT_DIR/SHA256SUMS.minisig"; do
    [[ -f "$f" ]] || continue
    log "  $(basename "$f")"
  done
}

main "$@"
