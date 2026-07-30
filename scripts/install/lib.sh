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
# probes keep targeting the port chosen at install time. In the docker bridge
# topology only its port part is used: the host part is replaced by the
# bridge gateway (config) or host.docker.internal (fragment).
PROXYHUB_LISTEN_ADDR="${PROXYHUB_LISTEN_ADDR:-127.0.0.1:8080}"
readonly PROXYHUB_STATE_DIR="/var/lib/proxyhub"
readonly PROXYHUB_CONFIG_DIR="/etc/proxyhub"
readonly PROXYHUB_LOG_DIR="/var/log/proxyhub"
readonly PROXYHUB_BACKUP_DIR="/var/backups/proxyhub"
readonly PROXYHUB_UNIT_PATH="/etc/systemd/system/proxyhub.service"
readonly PROXYHUB_CADDY_FRAGMENT="/etc/caddy/conf.d/proxyhub.caddy"
# Effective Caddy mode (native|docker|none, ADR 0035). NOT readonly:
# install.sh mode detection and proxyhubctl's install-record reader adopt
# the detected/recorded mode. Defaults to native; install records predating
# the CADDY_MODE field mean native.
PROXYHUB_CADDY_MODE="${PROXYHUB_CADDY_MODE:-native}"
# Name of the integrated Caddy container in docker mode; empty otherwise.
PROXYHUB_CADDY_CONTAINER="${PROXYHUB_CADDY_CONTAINER:-}"
# Docker network topology of the integrated container (ADR 0035): host
# (zero-change loopback path) or bridge (gateway listen + narrowed XFF
# trust). Resolved by docker_caddy_prepare_topology during mode detection;
# the bridge gateway/subnet only exist when NETMODE=bridge.
PROXYHUB_DOCKER_NETMODE="${PROXYHUB_DOCKER_NETMODE:-}"
PROXYHUB_BRIDGE_GATEWAY="${PROXYHUB_BRIDGE_GATEWAY:-}"
PROXYHUB_BRIDGE_SUBNET="${PROXYHUB_BRIDGE_SUBNET:-}"

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

# _docker ARGS... - docker CLI wrapper seam, isomorphic to install.sh's
# _run_svc_tool/_systemctl: a logged no-op under PROXYHUB_ROOT, otherwise
# `command docker "$@"`. Every docker invocation in install.sh, proxyhubctl
# and this library must go through this seam so tests can mock or intercept
# it (the docker Caddy channel, ADR 0035, builds on it).
_docker() {
    if _is_test_mode; then
        _ph_log "test mode: docker $*"
        return 0
    fi
    command docker "$@"
}

# _caddy_bin ARGS... - host caddy CLI wrapper seam, isomorphic to _docker: a
# logged no-op under PROXYHUB_ROOT, otherwise `command caddy "$@"`. The
# native channel routes through it so test-mode callers never execute a real
# host caddy (restores the deleted install.sh _caddy_cli seam semantics).
_caddy_bin() {
    if _is_test_mode; then
        _ph_log "test mode: caddy $*"
        return 0
    fi
    command caddy "$@"
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
# Docker caddy container helpers (ADR 0035)
# --------------------------------------------------------------------------
# All container inspection goes through the _docker seam so tests can feed
# canned inspect/port output. These helpers are the shared vocabulary of
# install.sh's mode detection and (later) proxyhubctl's docker channel.

# _docker_image_is_caddy IMAGE - true when IMAGE is the caddy image in any
# reference form: caddy, caddy:TAG, caddy@sha256:..., or registry-prefixed
# (.../caddy:TAG). Lookalikes (caddy-fork, team/caddy-proxy) do not match.
_docker_image_is_caddy() {
    local img=${1##*/}
    img=${img%%[:@]*}
    [[ $img == caddy ]]
}

# docker_caddy_candidates - print the names of running containers whose image
# is caddy, one per line. Returns 1 when docker itself is unavailable.
docker_caddy_candidates() {
    local ps_out name image
    ps_out=$(_docker ps --format '{{.Names}} {{.Image}}' 2>/dev/null) || return 1
    while read -r name image; do
        [[ -n $name ]] || continue
        _docker_image_is_caddy "$image" && printf '%s\n' "$name"
    done <<<"$ps_out"
    return 0
}

# docker_validate_caddy_container NAME - explicit --caddy-docker selection
# checks: the container must exist, be running, and run a caddy image.
docker_validate_caddy_container() {
    local name=$1 running image
    running=$(_docker inspect --format '{{.State.Running}}' "$name" 2>/dev/null) || {
        _ph_err "caddy container '${name}' does not exist (docker inspect failed)"
        _ph_err "list containers with: docker ps -a"
        return 1
    }
    if [[ $running != true ]]; then
        _ph_err "caddy container '${name}' is not running"
        _ph_err "start it with: docker start ${name}"
        return 1
    fi
    image=$(_docker inspect --format '{{.Config.Image}}' "$name") || return 1
    if ! _docker_image_is_caddy "$image"; then
        _ph_err "container '${name}' runs image '${image}', not a caddy image"
        _ph_err "point --caddy-docker at a container running the caddy image"
        return 1
    fi
    return 0
}

# docker_caddy_require_running - proxyhubctl's preflight for every Caddy
# touch point (ADR 0035): in docker mode the container recorded at install
# time must still exist and be running, otherwise the operation fails closed
# BEFORE any mutation - a stale record must never steer fmt/validate/reload
# at the wrong container or silently skip the Caddy side. No-op in other
# modes. Lighter than docker_validate_caddy_container (install-time
# selection): the image was already validated when the record was written.
docker_caddy_require_running() {
    [[ $PROXYHUB_CADDY_MODE == docker ]] || return 0
    local name=$PROXYHUB_CADDY_CONTAINER
    if [[ -z $name ]]; then
        _ph_err "install record sets CADDY_MODE=docker but no CADDY_CONTAINER; the install record is corrupt"
        return 1
    fi
    local running
    running=$(_docker inspect --format '{{.State.Running}}' "$name" 2>/dev/null) || {
        _ph_err "recorded caddy container '${name}' no longer exists (deleted or renamed?)"
        _ph_err "inspect with 'docker ps -a'; if the container was replaced, update CADDY_CONTAINER in the install record"
        return 1
    }
    if [[ $running != true ]]; then
        _ph_err "recorded caddy container '${name}' is not running"
        _ph_err "start it with 'docker start ${name}' (or update CADDY_CONTAINER in the install record if it was replaced)"
        return 1
    fi
    return 0
}

# _docker_mount_host_path TYPE SOURCE NAME - print the host-side path of an
# /etc/caddy mount candidate: bind mounts and named volumes both resolve to
# the mount's Source (for volumes that is the docker volume data path, even
# with a custom data-root or rootless docker). Fails when the resolved
# directory does not exist. Anything else returns 1.
_docker_mount_host_path() {
    case $1 in
        bind | volume)
            [[ -n $2 ]] || return 1
            if [[ -d $(root_path "$2") ]]; then printf '%s\n' "$2"; return 0; fi
            return 1
            ;;
        *) return 1 ;;
    esac
}

# docker_caddy_mount_root NAME - print the host-side path backing the
# container's /etc/caddy mount. Fails closed with accurate remediation when
# /etc/caddy itself is not a usable persistent directory: an /etc/caddy
# mount whose Source is missing or of an unsupported type, mounts that only
# cover sub-paths (e.g. a lone Caddyfile or conf.d), or no mount at all.
# Config on the container layer would vanish on recreate.
docker_caddy_mount_root() {
    local name=$1 mounts mtype mdest msrc mname bad_root="" sub_mounts=""
    mounts=$(_docker inspect \
        --format '{{range .Mounts}}{{printf "%s\t%s\t%s\t%s\n" .Type .Destination .Source .Name}}{{end}}' \
        "$name") || {
        _ph_err "failed to inspect container '${name}'"
        return 1
    }
    while IFS=$'\t' read -r mtype mdest msrc mname; do
        [[ $mdest == /etc/caddy || $mdest == /etc/caddy/* ]] || continue
        if [[ $mdest == /etc/caddy ]]; then
            if _docker_mount_host_path "$mtype" "$msrc" "$mname"; then return 0; fi
            bad_root="${mtype}:${msrc:-none}"
        else
            sub_mounts+="${mdest} (${mtype}) "
        fi
    done <<<"$mounts"
    if [[ -n $bad_root ]]; then
        _ph_err "container '${name}' mounts /etc/caddy but it is not a usable persistent directory (${bad_root})"
        _ph_err "fix: mount a host directory (or named volume) at /etc/caddy instead, e.g. -v /srv/caddy:/etc/caddy"
        return 1
    fi
    if [[ -n $sub_mounts ]]; then
        _ph_err "container '${name}' mounts only sub-paths under /etc/caddy (${sub_mounts% }); ProxyHub manages fragments under /etc/caddy/conf.d/ and needs the whole directory"
        _ph_err "fix: mount a host directory (or named volume) at /etc/caddy instead, e.g. -v /srv/caddy:/etc/caddy"
        return 1
    fi
    _ph_err "container '${name}' has no persistent /etc/caddy mount; configuration written to the container layer would vanish on recreate"
    _ph_err "fix: add a bind mount or named volume at /etc/caddy, e.g. -v /srv/caddy:/etc/caddy"
    return 1
}

# docker_caddy_network_mode NAME - print the container's HostConfig network
# mode: host, bridge, or a user-defined network name (compose-style). Fails
# closed when inspect fails or reports an empty mode.
docker_caddy_network_mode() {
    local name=$1 mode
    mode=$(_docker inspect --format '{{.HostConfig.NetworkMode}}' "$name") || {
        _ph_err "failed to inspect network mode of container '${name}'"
        return 1
    }
    if [[ -z $mode ]]; then
        _ph_err "container '${name}' reports an empty network mode"
        return 1
    fi
    printf '%s\n' "$mode"
}

# _ipv4_network IP PREFIX - print the base network CIDR (A.B.C.D/PREFIX) for
# an IPv4 dotted-quad and prefix length 1-32. Returns 1 on malformed input.
_ipv4_network() {
    local ip=$1 prefix=$2
    local ip_re='^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$'
    local num_re='^[0-9]+$'
    # The prefix check runs first: a second =~ would clobber BASH_REMATCH.
    [[ $prefix =~ $num_re ]] || return 1
    [[ $ip =~ $ip_re ]] || return 1
    local octets=("${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}" "${BASH_REMATCH[4]}")
    prefix=$((10#$prefix))
    ((prefix >= 1 && prefix <= 32)) || return 1
    local out="" i octet mask
    for i in 0 1 2 3; do
        octet=$((10#${octets[$i]}))
        ((octet <= 255)) || return 1
        if ((prefix >= (i + 1) * 8)); then
            mask=255
        elif ((prefix <= i * 8)); then
            mask=0
        else
            mask=$((256 - 2 ** (8 - (prefix - i * 8))))
        fi
        out+="$((octet & mask))"
        ((i == 3)) || out+="."
    done
    printf '%s/%d\n' "$out" "$prefix"
}

# docker_caddy_bridge_topology NAME - print "GATEWAY_IP SUBNET_CIDR" for the
# container's bridge attachment (one line, space separated). Multi-network
# containers pick deterministically: the alphabetically first network with a
# non-empty IPv4 gateway (inspect map order is not stable). A missing/invalid
# IPPrefixLen falls back to the /16 default-bridge convention. Fails closed
# when no attached network exposes an IPv4 gateway.
docker_caddy_bridge_topology() {
    local name=$1 nets
    # SC2016: the single-quoted argument is a docker Go template, not shell.
    # shellcheck disable=SC2016
    nets=$(_docker inspect \
        --format '{{range $n, $net := .NetworkSettings.Networks}}{{printf "%s\t%s\t%d\n" $n $net.Gateway $net.IPPrefixLen}}{{end}}' \
        "$name") || {
        _ph_err "failed to inspect networks of container '${name}'"
        return 1
    }
    local net gw prefix subnet num_re='^[0-9]+$'
    while IFS=$'\t' read -r net gw prefix; do
        [[ -n $gw && $gw != *:* ]] || continue # IPv4 gateways only
        [[ $prefix =~ $num_re ]] && ((10#$prefix >= 1 && 10#$prefix <= 32)) || prefix=16
        if subnet=$(_ipv4_network "$gw" "$prefix"); then
            printf '%s %s\n' "$gw" "$subnet"
            return 0
        fi
    done < <(printf '%s\n' "$nets" | LC_ALL=C sort)
    _ph_err "container '${name}' has no IPv4 gateway on any attached network; cannot derive the bridge listen address"
    return 1
}

# docker_caddy_require_host_gateway NAME - a bridge-network caddy reaches the
# host listener through host.docker.internal, which requires the
# host-gateway extra-hosts mapping (Docker >= 20.10). Fails closed with
# remediation guidance when the mapping is absent.
docker_caddy_require_host_gateway() {
    local name=$1 hosts
    hosts=$(_docker inspect \
        --format '{{range .HostConfig.ExtraHosts}}{{printf "%s\n" .}}{{end}}' \
        "$name") || {
        _ph_err "failed to inspect extra hosts of container '${name}'"
        return 1
    }
    if printf '%s\n' "$hosts" | grep -qxF 'host.docker.internal:host-gateway'; then
        return 0
    fi
    _ph_err "caddy container '${name}' (bridge networking) lacks the host.docker.internal:host-gateway mapping; the reverse proxy could not reach the ProxyHub listener on the host"
    _ph_err "fix: add '--add-host host.docker.internal:host-gateway' to docker run, or 'extra_hosts: [\"host.docker.internal:host-gateway\"]' to docker-compose.yml"
    return 1
}

# docker_caddy_prepare_topology NAME - classify the container's network
# topology and resolve everything the bridge listen path needs, BEFORE any
# file is written (fail-fast with the other host validations). Host-network
# containers take the zero-change path (loopback semantics stay). Bridge
# containers (including user-defined/compose networks) must expose a
# derivable IPv4 gateway and carry the host-gateway mapping; both checks
# fail closed. On success sets PROXYHUB_DOCKER_NETMODE (host|bridge) and,
# for bridge, PROXYHUB_BRIDGE_GATEWAY / PROXYHUB_BRIDGE_SUBNET.
docker_caddy_prepare_topology() {
    local name=$1 mode
    mode=$(docker_caddy_network_mode "$name") || return 1
    if [[ $mode == host ]]; then
        PROXYHUB_DOCKER_NETMODE=host
        PROXYHUB_BRIDGE_GATEWAY=""
        PROXYHUB_BRIDGE_SUBNET=""
        _ph_log "container '${name}' uses host networking: loopback topology unchanged"
        return 0
    fi
    local topo
    topo=$(docker_caddy_bridge_topology "$name") || return 1
    docker_caddy_require_host_gateway "$name" || return 1
    PROXYHUB_DOCKER_NETMODE=bridge
    PROXYHUB_BRIDGE_GATEWAY=${topo%% *}
    PROXYHUB_BRIDGE_SUBNET=${topo##* }
    _ph_log "container '${name}' uses bridge networking (${mode}): gateway ${PROXYHUB_BRIDGE_GATEWAY}, trusted subnet ${PROXYHUB_BRIDGE_SUBNET}"
}

# _is_bridge_topology - true when the docker caddy container sits on a
# bridge network and ProxyHub must therefore bind the bridge gateway (ADR
# 0035). PROXYHUB_DOCKER_NETMODE is already normalized to host|bridge by
# docker_caddy_prepare_topology, so a user-defined (compose) network also
# reads as bridge here.
_is_bridge_topology() {
    [[ $PROXYHUB_CADDY_MODE == docker && $PROXYHUB_DOCKER_NETMODE == bridge ]]
}

# caddy_upstream_addr - print the upstream address the managed fragment
# proxies to. Loopback topologies (native, none, host-network docker) keep
# PROXYHUB_LISTEN_ADDR; a bridge-network docker caddy reaches the host
# listener through its host-gateway mapping, so the target becomes
# host.docker.internal with only the listen port carried over.
caddy_upstream_addr() {
    if _is_bridge_topology; then
        printf 'host.docker.internal:%s' "${PROXYHUB_LISTEN_ADDR##*:}"
        return 0
    fi
    printf '%s' "$PROXYHUB_LISTEN_ADDR"
}

# docker_caddy_ports_published NAME - a bridge-network caddy container must
# publish TCP 80 and 443 (TLS issuance depends on them). Host-network
# containers are exempt: they bind the host interfaces directly. The bridge
# listen topology itself (host-gateway mapping, gateway bind) is gated by
# docker_caddy_prepare_topology.
docker_caddy_ports_published() {
    local name=$1 netmode ports=""
    netmode=$(docker_caddy_network_mode "$name") || return 1
    if [[ $netmode == host ]]; then
        _ph_log "container '${name}' uses host networking: 80/443 publish check exempt"
        return 0
    fi
    ports=$(_docker port "$name" 2>/dev/null) || ports=""
    if [[ $ports == *"80/tcp ->"* && $ports == *"443/tcp ->"* ]]; then return 0; fi
    _ph_err "caddy container '${name}' (${netmode} networking) does not publish both TCP 80 and 443; TLS issuance requires them"
    _ph_err "fix: publish the ports on the container, e.g. -p 80:80 -p 443:443"
    return 1
}

# --------------------------------------------------------------------------
# Caddy mode channel
# --------------------------------------------------------------------------
# Every Caddy operation (fragment path resolution, fmt/validate/reload) is
# dispatched on PROXYHUB_CADDY_MODE so the docker Caddy mode (ADR 0035) slots
# in behind the same call sites. The docker channel operates inside the
# selected container through _docker exec/restart while files are written
# through the resolved host-side mount path; none (--no-caddy) has no managed
# Caddy to operate on.

# _caddy_mode_fail - shared fail-closed branch for corrupt mode values.
_caddy_mode_fail() {
    _ph_err "unknown caddy mode '${PROXYHUB_CADDY_MODE}'"
    return 1
}

# caddy_config_dir - print the host-side Caddy config directory, resolved
# through root_path: /etc/caddy natively, the container's /etc/caddy mount
# root in docker mode.
caddy_config_dir() {
    case "$PROXYHUB_CADDY_MODE" in
        native | none) root_path /etc/caddy ;;
        docker)
            local mroot
            mroot=$(docker_caddy_mount_root "$PROXYHUB_CADDY_CONTAINER") || return 1
            root_path "$mroot"
            ;;
        *) _caddy_mode_fail ;;
    esac
}

# caddy_fragment_path - print the managed fragment path, resolved through
# root_path like every other host path in this library. In docker mode this
# is the host-side path under the container's /etc/caddy mount; the container
# always sees it at PROXYHUB_CADDY_FRAGMENT.
caddy_fragment_path() {
    case "$PROXYHUB_CADDY_MODE" in
        native | none) root_path "$PROXYHUB_CADDY_FRAGMENT" ;;
        docker)
            local cdir
            cdir=$(caddy_config_dir) || return 1
            printf '%s%s' "$cdir" "${PROXYHUB_CADDY_FRAGMENT#/etc/caddy}"
            ;;
        *) _caddy_mode_fail ;;
    esac
}

# caddy_fmt FRAGMENT - format the fragment in place (caddy fmt --overwrite).
# The docker branch ignores the host-side FRAGMENT argument: the container
# always sees the fragment at the constant container path.
caddy_fmt() {
    case "$PROXYHUB_CADDY_MODE" in
        native) _caddy_bin fmt --overwrite "$1" ;;
        docker) _docker exec "$PROXYHUB_CADDY_CONTAINER" caddy fmt --overwrite "$PROXYHUB_CADDY_FRAGMENT" ;;
        none) return 0 ;;
        *) _caddy_mode_fail ;;
    esac
}

# caddy_validate CONFIG - validate the full Caddy configuration. The docker
# branch validates the container-path Caddyfile regardless of the host-side
# CONFIG argument.
caddy_validate() {
    case "$PROXYHUB_CADDY_MODE" in
        native) _caddy_bin validate --config "$1" ;;
        docker) _docker exec "$PROXYHUB_CADDY_CONTAINER" caddy validate --config /etc/caddy/Caddyfile ;;
        none) return 0 ;;
        *) _caddy_mode_fail ;;
    esac
}

# caddy_reload - reload via the admin API, falling back to a full restart
# when the API is disabled ('admin off' in the global options block, e.g.
# 233boy-style Caddyfiles). Restart briefly interrupts the other sites on
# this Caddy (warned); docker mode restarts the container, mirroring the
# native systemctl restart fallback. install.sh keeps a thin seam
# (_reload_caddy) over this channel so tests can override the reload step.
caddy_reload() {
    case "$PROXYHUB_CADDY_MODE" in
        native)
            systemctl reload caddy.service 2>/dev/null && return 0
            caddy reload --config /etc/caddy/Caddyfile 2>/dev/null && return 0
            _ph_log "WARNING: caddy reload failed (admin API disabled, e.g. 'admin off'); falling back to 'systemctl restart caddy' - brief interruption for other sites on this Caddy"
            systemctl restart caddy.service
            ;;
        docker)
            _docker exec "$PROXYHUB_CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile 2>/dev/null && return 0
            _ph_log "WARNING: caddy reload failed (admin API disabled, e.g. 'admin off'); falling back to 'docker restart ${PROXYHUB_CADDY_CONTAINER}' - brief interruption for other sites on this Caddy"
            _docker restart "$PROXYHUB_CADDY_CONTAINER"
            ;;
        none) return 0 ;;
        *) _caddy_mode_fail ;;
    esac
}

# --------------------------------------------------------------------------
# Caddy fragment writer
# --------------------------------------------------------------------------

# write_caddy_fragment DOMAIN SITE_PATH - write the Caddy v2 site fragment.
# Terminates TLS for DOMAIN (Caddy automatic HTTPS), proxies /<site-path>/
# (including /<site-path>/dist/) to the ProxyHub listener (loopback, or
# host.docker.internal in the docker bridge topology - caddy_upstream_addr),
# and returns a plain 404 for / and everything else. The embedded Xray
# data-plane is reached only through ProxyHub; no Xray port is exposed.
write_caddy_fragment() {
    local domain="${1:-}"
    local site_path="${2:-}"
    validate_domain "$domain" || return 1
    validate_site_path "$site_path" || return 1

    local upstream
    upstream=$(caddy_upstream_addr)
    local frag_path
    frag_path=$(caddy_fragment_path) || return 1
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
		# Replace (not append) forwarding headers: ProxyHub trusts XFF only from
		# its declared peers (loopback, or the narrowed bridge subnet), so a
		# caller-supplied X-Forwarded-For must never survive the proxy hop -
		# otherwise IP2Ban / honeypot / captcha / blacklist can all be bypassed
		# by spoofing a trusted source.
		reverse_proxy ${upstream} {
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
