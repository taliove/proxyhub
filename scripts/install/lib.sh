#!/usr/bin/env bash
# lib.sh - ProxyHub production-install helper library.
#
# Usage:
#   source scripts/install/lib.sh
#
# All functions are safe under `set -Eeuo pipefail`. They report errors to
# stderr with a `[proxyhub]` prefix and return non-zero on failure instead of
# exiting (the only exception is the dedicated `_die`).
#
# Every host path is resolved through `root_path`, so setting PROXYHUB_ROOT
# redirects all filesystem effects under that prefix (used by tests; in this
# mode privileged operations such as useradd/groupadd/chown become no-ops).
#
# Architecture note: Xray is embedded in the single ProxyHub binary, so there
# is exactly ONE systemd unit (proxyhub.service) and exactly ONE loopback
# listener (127.0.0.1:8080) that Caddy proxies to. No Xray unit or port is
# ever exposed.

# --------------------------------------------------------------------------
# Constants
# --------------------------------------------------------------------------

# Case-insensitive reserved words that must not appear in a Site Path.
# shellcheck disable=SC2034
PROXYHUB_RESERVED_WORDS=(
    admin api assets dist distribution favicon health healthz
    login proxyhub root setup sub subscription
)
readonly PROXYHUB_RESERVED_WORDS

readonly PROXYHUB_USER="proxyhub"
readonly PROXYHUB_GROUP="proxyhub"
readonly PROXYHUB_BINARY="/usr/local/bin/proxyhub"
# Effective loopback listen address. NOT readonly: install.sh --listen-addr
# overrides it after argument parsing, and proxyhubctl re-points it from the
# install record's LISTEN_ADDR so regenerated Caddy fragments and health
# probes keep targeting the port chosen at install time.
PROXYHUB_LISTEN_ADDR="${PROXYHUB_LISTEN_ADDR:-127.0.0.1:8080}"
readonly PROXYHUB_STATE_DIR="/var/lib/proxyhub"
readonly PROXYHUB_CONFIG_DIR="/etc/proxyhub"
readonly PROXYHUB_LOG_DIR="/var/log/proxyhub"
readonly PROXYHUB_BACKUP_DIR="/var/backups/proxyhub"
readonly PROXYHUB_UNIT_PATH="/etc/systemd/system/proxyhub.service"
readonly PROXYHUB_CADDY_FRAGMENT="/etc/caddy/conf.d/proxyhub.caddy"

# --------------------------------------------------------------------------
# Output helpers
# --------------------------------------------------------------------------

# _ph_err MSG... - print an error line to stderr with the [proxyhub] prefix.
_ph_err() {
    printf '[proxyhub] %s\n' "$*" >&2
}

# _ph_log MSG... - print an informational line to stderr.
_ph_log() {
    printf '[proxyhub] %s\n' "$*" >&2
}

# _die MSG... - print an error and exit 1. The only function allowed to exit.
_die() {
    _ph_err "$@"
    exit 1
}

# --------------------------------------------------------------------------
# Path helper
# --------------------------------------------------------------------------

# root_path PATH - echo PATH prefixed with ${PROXYHUB_ROOT:-}. Lets tests
# redirect every absolute path (/etc/..., /var/...) under a scratch directory.
root_path() {
    printf '%s%s' "${PROXYHUB_ROOT:-}" "$1"
}

# --------------------------------------------------------------------------
# Validators
# --------------------------------------------------------------------------

# validate_site_path VALUE - enforce Site Path rules:
#   * 20-64 characters
#   * charset [A-Za-z0-9_-] only
#   * at least 3 of 4 classes: lowercase, uppercase, digit, separator (-/_)
#   * must not contain any reserved word (case-insensitive)
validate_site_path() {
    local sp="${1:-}"
    local len=${#sp}
    if (( len < 20 || len > 64 )); then
        _ph_err "site path must be 20-64 characters long (got ${len})"
        return 1
    fi
    local charset_re='^[A-Za-z0-9_-]+$'
    if [[ ! "$sp" =~ $charset_re ]]; then
        _ph_err "site path may only contain [A-Za-z0-9_-]"
        return 1
    fi
    local classes=0
    [[ "$sp" =~ [a-z] ]] && classes=$((classes + 1))
    [[ "$sp" =~ [A-Z] ]] && classes=$((classes + 1))
    [[ "$sp" =~ [0-9] ]] && classes=$((classes + 1))
    [[ "$sp" =~ [-_] ]] && classes=$((classes + 1))
    if (( classes < 3 )); then
        _ph_err "site path must use at least 3 of 4 character classes (lowercase, uppercase, digit, -/_)"
        return 1
    fi
    local lower_sp
    # ASCII-only fold is intentional: the reserved list is ASCII and the
    # site-path charset is [A-Za-z0-9_-], so locale-aware classes are wrong.
    # shellcheck disable=SC2018,SC2019
    lower_sp=$(printf '%s' "$sp" | tr 'A-Z' 'a-z')
    local word
    for word in "${PROXYHUB_RESERVED_WORDS[@]}"; do
        if [[ "$lower_sp" == *"$word"* ]]; then
            _ph_err "site path must not contain reserved word '${word}'"
            return 1
        fi
    done
    return 0
}

# validate_domain VALUE - enforce a standard DNS hostname.
validate_domain() {
    local domain="${1:-}"
    if (( ${#domain} == 0 || ${#domain} > 253 )); then
        _ph_err "domain must be 1-253 characters long"
        return 1
    fi
    local domain_re='^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$'
    if [[ ! "$domain" =~ $domain_re ]]; then
        _ph_err "invalid DNS hostname '${domain}'"
        return 1
    fi
    return 0
}

# validate_repo VALUE - enforce GitHub OWNER/REPO form.
validate_repo() {
    local repo="${1:-}"
    local repo_re='^[A-Za-z0-9]([A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$'
    if [[ ! "$repo" =~ $repo_re ]]; then
        _ph_err "invalid GitHub repo '${repo}' (expected OWNER/REPO)"
        return 1
    fi
    return 0
}

# validate_version VALUE - enforce semver-ish: v?MAJOR.MINOR.PATCH with
# optional -prerelease and +build suffixes. No leading zeros in numeric parts.
validate_version() {
    local version="${1:-}"
    local num='(0|[1-9][0-9]*)'
    local version_re="^v?${num}\\.${num}\\.${num}(-[0-9A-Za-z.-]+)?(\\+[0-9A-Za-z.-]+)?\$"
    if [[ ! "$version" =~ $version_re ]]; then
        _ph_err "invalid version '${version}' (expected semver, e.g. 1.2.3 or v1.2.3-rc.1)"
        return 1
    fi
    return 0
}

# random_token N - print a URL-safe high-entropy token of exactly N chars.
random_token() {
    local n="${1:-}"
    local n_re='^[0-9]+$'
    if [[ ! "$n" =~ $n_re ]] || (( n < 1 )); then
        _ph_err "random_token requires a positive integer length (got '${n}')"
        return 1
    fi
    local token
    # tr keeps only URL-safe chars; head takes exactly N. tr receives SIGPIPE
    # once head closes the pipe, so tolerate that under pipefail.
    token=$(LC_ALL=C tr -dc 'A-Za-z0-9_-' < /dev/urandom 2>/dev/null | head -c "$n") || true
    printf '%s' "$token"
}

# --------------------------------------------------------------------------
# Service identity and directory layout
# --------------------------------------------------------------------------

# _is_test_mode - true when PROXYHUB_ROOT redirects all host paths.
_is_test_mode() {
    [[ -n "${PROXYHUB_ROOT:-}" ]]
}

# ensure_proxyhub_group - create the low-privilege proxyhub system group.
# Idempotent; no-op under PROXYHUB_ROOT.
ensure_proxyhub_group() {
    if _is_test_mode; then
        _ph_log "test mode: skipping groupadd ${PROXYHUB_GROUP}"
        return 0
    fi
    if getent group "$PROXYHUB_GROUP" >/dev/null 2>&1; then
        _ph_log "group ${PROXYHUB_GROUP} already exists"
        return 0
    fi
    if ! groupadd --system "$PROXYHUB_GROUP"; then
        _ph_err "failed to create system group ${PROXYHUB_GROUP}"
        return 1
    fi
}

# ensure_proxyhub_user - create the low-privilege proxyhub system user.
# Idempotent; no-op under PROXYHUB_ROOT.
ensure_proxyhub_user() {
    if _is_test_mode; then
        _ph_log "test mode: skipping useradd ${PROXYHUB_USER}"
        return 0
    fi
    if id -u "$PROXYHUB_USER" >/dev/null 2>&1; then
        _ph_log "user ${PROXYHUB_USER} already exists"
        return 0
    fi
    ensure_proxyhub_group || return 1
    if ! useradd --system \
        --gid "$PROXYHUB_GROUP" \
        --home-dir "$PROXYHUB_STATE_DIR" \
        --shell /usr/sbin/nologin \
        --comment "ProxyHub service account" \
        "$PROXYHUB_USER"; then
        _ph_err "failed to create system user ${PROXYHUB_USER}"
        return 1
    fi
}

# _ensure_dir PATH PERMS - create PATH (through root_path) with PERMS, owned
# by proxyhub:proxyhub outside test mode. Idempotent.
_ensure_dir() {
    local path="$1"
    local perms="$2"
    local real
    real=$(root_path "$path")
    if ! mkdir -p "$real"; then
        _ph_err "failed to create directory ${real}"
        return 1
    fi
    if ! chmod "$perms" "$real"; then
        _ph_err "failed to chmod ${perms} ${real}"
        return 1
    fi
    if ! _is_test_mode && [[ ${EUID:-$(id -u)} -eq 0 ]]; then
        if ! chown "${PROXYHUB_USER}:${PROXYHUB_GROUP}" "$real"; then
            _ph_err "failed to chown ${PROXYHUB_USER}:${PROXYHUB_GROUP} ${real}"
            return 1
        fi
    fi
    return 0
}

# ensure_directories - create the production directory layout:
#   /var/lib/proxyhub     0750 (state)
#   /etc/proxyhub         0750 (config)
#   /var/log/proxyhub     0750 (logs)
#   /var/backups/proxyhub 0700 (backups)
ensure_directories() {
    _ensure_dir "$PROXYHUB_STATE_DIR" 0750 || return 1
    _ensure_dir "$PROXYHUB_CONFIG_DIR" 0750 || return 1
    _ensure_dir "$PROXYHUB_LOG_DIR" 0750 || return 1
    _ensure_dir "$PROXYHUB_BACKUP_DIR" 0700 || return 1
    return 0
}

# --------------------------------------------------------------------------
# systemd unit emitter
# --------------------------------------------------------------------------

# write_proxyhub_unit - write the proxyhub.service systemd unit. Runs the
# single embedded binary as proxyhub:proxyhub with hardening. There is NO
# separate Xray unit: Xray-core is embedded in the ProxyHub binary.
write_proxyhub_unit() {
    local unit_path
    unit_path=$(root_path "$PROXYHUB_UNIT_PATH")
    local unit_dir
    unit_dir=$(dirname "$unit_path")
    if ! mkdir -p "$unit_dir"; then
        _ph_err "failed to create directory ${unit_dir}"
        return 1
    fi
    local tmp
    tmp="${unit_path}.tmp.$$"
    if ! cat > "$tmp" <<EOF
[Unit]
Description=ProxyHub - proxy subscription aggregator and traffic distribution (embedded Xray)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${PROXYHUB_USER}
Group=${PROXYHUB_GROUP}
ExecStart=${PROXYHUB_BINARY} --config ${PROXYHUB_CONFIG_DIR}/config.yaml
Restart=on-failure
RestartSec=5s

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=${PROXYHUB_STATE_DIR} ${PROXYHUB_LOG_DIR} ${PROXYHUB_BACKUP_DIR}
CapabilityBoundingSet=
AmbientCapabilities=
LockPersonality=true
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
    then
        _ph_err "failed to write ${tmp}"
        rm -f "$tmp"
        return 1
    fi
    if ! chmod 0644 "$tmp"; then
        _ph_err "failed to chmod 0644 ${tmp}"
        rm -f "$tmp"
        return 1
    fi
    if ! mv "$tmp" "$unit_path"; then
        _ph_err "failed to install unit to ${unit_path}"
        rm -f "$tmp"
        return 1
    fi
    _ph_log "wrote ${unit_path}"
    return 0
}

# --------------------------------------------------------------------------
# Caddy fragment writer
# --------------------------------------------------------------------------

# write_caddy_fragment DOMAIN SITE_PATH - write the Caddy v2 site fragment.
# Terminates TLS for DOMAIN (Caddy automatic HTTPS), proxies /<site-path>/
# (including /<site-path>/dist/) to the ProxyHub loopback listener, and
# returns a plain 404 for / and everything else. The embedded Xray data-plane
# is reached only through ProxyHub; no Xray port is exposed.
write_caddy_fragment() {
    local domain="${1:-}"
    local site_path="${2:-}"
    validate_domain "$domain" || return 1
    validate_site_path "$site_path" || return 1

    local frag_path
    frag_path=$(root_path "$PROXYHUB_CADDY_FRAGMENT")
    local frag_dir
    frag_dir=$(dirname "$frag_path")
    if ! mkdir -p "$frag_dir"; then
        _ph_err "failed to create directory ${frag_dir}"
        return 1
    fi
    local tmp
    tmp="${frag_path}.tmp.$$"
    if ! cat > "$tmp" <<EOF
# ProxyHub site fragment - managed by the ProxyHub installer. Do not edit.
${domain} {
	@proxyhub path /${site_path} /${site_path}/*
	handle @proxyhub {
		# Replace (not append) forwarding headers: ProxyHub trusts XFF from its
		# loopback peer, so a caller-supplied X-Forwarded-For must never survive
		# the proxy hop - otherwise IP2Ban / honeypot / captcha / blacklist can
		# all be bypassed by spoofing 127.0.0.1.
		reverse_proxy ${PROXYHUB_LISTEN_ADDR} {
			header_up X-Forwarded-For {remote_host}
			header_up X-Real-IP {remote_host}
		}
	}

	handle {
		respond 404
	}
}
EOF
    then
        _ph_err "failed to write ${tmp}"
        rm -f "$tmp"
        return 1
    fi
    if ! chmod 0644 "$tmp"; then
        _ph_err "failed to chmod 0644 ${tmp}"
        rm -f "$tmp"
        return 1
    fi
    if ! mv "$tmp" "$frag_path"; then
        _ph_err "failed to install fragment to ${frag_path}"
        rm -f "$tmp"
        return 1
    fi
    _ph_log "wrote ${frag_path}"
    return 0
}
