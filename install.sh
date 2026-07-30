#!/usr/bin/env bash
# install.sh - ProxyHub one-command production installer. See --help for the
# full contract. Never modifies DNS, firewalls, or security groups.
# Tests source this file with PROXYHUB_INSTALL_NO_MAIN=1 and override the seam
# functions (_curl, _systemctl, _host_os, _dns_resolve, ...); PROXYHUB_ROOT
# redirects every absolute host path into a scratch directory.
set -Eeuo pipefail

# Locate and source the shared installer library (ticket 01). When install.sh
# itself was fetched standalone (curl | bash, or process substitution), the
# companion lib is not on disk next to it - fetch it from the same repo over
# verified HTTPS (PROXYHUB_LIB_URL can pin a fork/ref). The filesystem
# candidate is only trusted when the installer itself is a real on-disk file:
# in piped mode SCRIPT_DIR falls back to the caller's cwd, and sourcing
# $cwd/scripts/install/lib.sh as root would be a privesc vector (fail closed).
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || pwd)
_ph_lib=""
# Test seam first (PROXYHUB_ROOT scratch tree).
if [[ -n ${PROXYHUB_ROOT:-} && -r ${PROXYHUB_ROOT}/scripts/install/lib.sh ]]; then
    _ph_lib="${PROXYHUB_ROOT}/scripts/install/lib.sh"
# Trust the sibling copy only when the installer itself is a real on-disk
# file: in piped mode (curl | bash) SCRIPT_DIR falls back to the caller's
# cwd, and sourcing $cwd/scripts/install/lib.sh as root would be a privesc
# vector. Standalone fetches go straight to the HTTPS download below.
elif [[ -n ${BASH_SOURCE[0]:-} && -f ${BASH_SOURCE[0]} && -r ${SCRIPT_DIR}/scripts/install/lib.sh ]]; then
    _ph_lib="${SCRIPT_DIR}/scripts/install/lib.sh"
fi
_PH_LIB_TMP=""
if [[ -z $_ph_lib ]]; then
    # Only HTTPS is acceptable for root-executed code; file:// stays available
    # to the test suite via PROXYHUB_ROOT.
    _ph_lib_url="${PROXYHUB_LIB_URL:-https://raw.githubusercontent.com/taliove/proxyhub/main/scripts/install/lib.sh}"
    if [[ $_ph_lib_url != https://* && -z ${PROXYHUB_ROOT:-} ]]; then
        printf '[proxyhub] ERROR: PROXYHUB_LIB_URL must use https:// (got %s)\n' "$_ph_lib_url" >&2
        exit 1
    fi
    _PH_LIB_TMP=$(mktemp -d)
    chmod 700 "$_PH_LIB_TMP"
    if ! curl -fsSL "$_ph_lib_url" -o "$_PH_LIB_TMP/lib.sh"; then
        printf '[proxyhub] ERROR: cannot locate scripts/install/lib.sh locally, and download from %s failed\n' "$_ph_lib_url" >&2
        exit 1
    fi
    _ph_lib="$_PH_LIB_TMP/lib.sh"
fi
# Resolved companion-lib path, kept for the proxyhubctl install step
# (_ph_lib itself is unset below with the other search temporaries).
_PH_LIB_PATH="$_ph_lib"
# Dynamic source path: shellcheck cannot follow it; lib.sh has its own tests.
# shellcheck source=scripts/install/lib.sh
# shellcheck disable=SC1090,SC1091
source "$_ph_lib"
unset _ph_lib _ph_lib_candidate

# Constants and mutable globals (initialized for `set -u` sourcing)
readonly PROXYHUB_DEFAULT_REPO="taliove/proxyhub"
readonly PROXYHUB_INSTALL_INFO="/root/.proxyhub-install-info"
readonly PROXYHUB_CADDY_IMPORT_LINE='import /etc/caddy/conf.d/*.caddy'
readonly PROXYHUB_LOOPBACK_HEALTH_TRIES=15
readonly PROXYHUB_PUBLIC_HEALTH_TRIES=45

NON_INTERACTIVE=0 DOMAIN="" EMAIL="" VERSION="latest" VERSION_TAG=""
REPO="$PROXYHUB_DEFAULT_REPO" ARG_SITE_PATH="" SITE_PATH="" SKIP_DNS_CHECK=0
DETECTED_OS="" DETECTED_ARCH="" ADMIN_USER="" ADMIN_PASSWORD="" ARG_LISTEN_ADDR=""
TIMESTAMP="" WORKDIR="" CADDY_BACKUP="" NO_CADDY=0 ARG_CADDY_DOCKER=""

# _ph_fail MSG... - print each MSG as an error line and return 1 (guard helper).
_ph_fail() {
    local _m
    for _m in "$@"; do _ph_err "$_m"; done
    return 1
}
# _ph_die2 MSG... - print an error line and exit 2 (usage-error helper).
_ph_die2() { _ph_err "$@"; exit 2; }

usage() {
    cat <<'EOF'
ProxyHub one-command installer (single embedded-Xray binary)

USAGE
  bash install.sh                                  interactive mode (requires a TTY)
  bash install.sh --non-interactive --domain example.com [options]

REQUIRED (non-interactive)
  --domain DOMAIN        Public domain serving the management UI; an A/AAAA
                         record must point at this host before install.

OPTIONS
  --non-interactive      Never prompt; fail closed if a required value is missing.
  --email EMAIL          Optional ACME account email (certificate expiry notices).
  --version VERSION      "latest" (default) or explicit semver (1.2.3 / v1.2.3).
                         Pre-releases require an explicit value.
  --repo OWNER/REPO      GitHub releases source (default: taliove/proxyhub).
  --site-path PATH       Custom Site Path: 20-64 chars of [A-Za-z0-9_-], >=3
                         character classes, no reserved words.
  --listen-addr ADDR     Loopback listen address as 127.0.0.1:PORT
                         (default 127.0.0.1:8080). Use when 8080 is taken.
  --no-caddy             Bring-your-own reverse proxy: do not touch Caddy and
                         skip the 80/443 requirement; writes ready-to-adapt
                         Caddy and nginx examples to /etc/proxyhub/.
  --caddy-docker NAME    Integrate an existing dockerized Caddy (ADR 0035):
                         NAME must be a running container of the caddy image
                         with a persistent /etc/caddy mount (bind or named
                         volume). Bridge-network containers must additionally
                         publish TCP 80/443 and map
                         host.docker.internal:host-gateway; the admin plane
                         then binds the bridge gateway with trusted_proxies
                         narrowed to the bridge subnet (summary warns).
                         Mutually exclusive with --no-caddy.
  --skip-dns-check       Skip DNS resolution check (CDN / private networks).
  -h, --help             Show this help.

REHEARSAL SEAM (never for real deployments)
  PROXYHUB_SKIP_PUBLIC_HEALTH=1 skips the public https://<domain>/... health
  check. Intended for one-command install rehearsals where DNS/ACME cannot
  succeed (random domain); the loopback health check still runs.

BEHAVIOR
  READ-ONLY host validation (OS: Ubuntu 22.04/24.04/26.04, Debian 12/13; amd64/arm64;
  systemd; root; outbound HTTPS; DNS; TCP 80/443 free or Caddy-owned) - never
  touches DNS, firewalls, or security groups. Downloads the release tarball +
  SHA256SUMS over verified HTTPS; checksum verified BEFORE unpacking. Installs
  the binary, proxyhub user, directories, config.yaml, systemd unit; generates
  admin credentials passed to `proxyhub init` via stdin (never argv); writes,
  validates and reloads the Caddy fragment; verifies BOTH the loopback and the
  https://<domain>/<site-path>/ health endpoints; records the install in
  root-only (0600) /root/.proxyhub-install-info (never the password). Never
  installs Caddy from third-party repos (existing Caddy v2 is reused) and never
  disables HTTPS certificate validation. Re-running on a managed install
  refuses to duplicate it: use `proxyhubctl update|repair|uninstall`.
EOF
}

# Argument parsing (usage errors exit 2)
_need_value() { (($2 >= 2)) || _ph_die2 "$1 requires a value"; }
_validate_email() { [[ $1 =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]]; }

# _validate_listen_addr - 127.0.0.1:PORT with port 1-65535. The admin plane
# binds loopback only (constitution red line), so wider binds are rejected
# at argument parsing rather than at first health check.
_validate_listen_addr() {
    [[ $1 =~ ^127\.0\.0\.1:([0-9]+)$ ]] || return 1
    local port=${BASH_REMATCH[1]}
    ((port >= 1 && port <= 65535))
}

parse_args() {
    while (($#)); do
        case $1 in
            --non-interactive) NON_INTERACTIVE=1; shift ;;
            --skip-dns-check) SKIP_DNS_CHECK=1; shift ;;
            --domain) _need_value "$1" $#; DOMAIN=$2; shift 2 ;;
            --email) _need_value "$1" $#; EMAIL=$2; shift 2 ;;
            --version) _need_value "$1" $#; VERSION=$2; shift 2 ;;
            --repo) _need_value "$1" $#; REPO=$2; shift 2 ;;
            --site-path) _need_value "$1" $#; ARG_SITE_PATH=$2; shift 2 ;;
            --listen-addr) _need_value "$1" $#; ARG_LISTEN_ADDR=$2; shift 2 ;;
            --no-caddy) NO_CADDY=1; shift ;;
            --caddy-docker) _need_value "$1" $#; ARG_CADDY_DOCKER=$2; shift 2 ;;
            -h|--help) usage; exit 0 ;;
            --) shift; break ;;
            *) _ph_die2 "unknown argument: $1 (see --help)" ;;
        esac
    done
    (($# == 0)) || _ph_die2 "unexpected positional arguments: $*"

    [[ -z $ARG_CADDY_DOCKER || $NO_CADDY == 0 ]] ||
        _ph_die2 "--caddy-docker and --no-caddy are mutually exclusive (see --help)"

    # Validate formats immediately - fail closed before touching the host.
    local bad=0
    [[ -z $DOMAIN ]] || validate_domain "$DOMAIN" || bad=1
    [[ -z $ARG_SITE_PATH ]] || validate_site_path "$ARG_SITE_PATH" || bad=1
    [[ -z $ARG_LISTEN_ADDR ]] || _validate_listen_addr "$ARG_LISTEN_ADDR" || {
        _ph_err "invalid --listen-addr '${ARG_LISTEN_ADDR}' (want 127.0.0.1:PORT, port 1-65535)"
        bad=1
    }
    [[ $VERSION == latest ]] || validate_version "$VERSION" || bad=1
    validate_repo "$REPO" || bad=1
    if [[ -n $EMAIL ]] && ! _validate_email "$EMAIL"; then
        _ph_err "invalid email '${EMAIL}'"; bad=1
    fi
    ((bad == 0)) || exit 2

    # The rehearsal seam only makes sense paired with --skip-dns-check;
    # refusing the lone form keeps it from becoming a lazy-production flag.
    if [[ ${PROXYHUB_SKIP_PUBLIC_HEALTH:-0} == 1 && $SKIP_DNS_CHECK != 1 ]]; then
        _ph_die2 "PROXYHUB_SKIP_PUBLIC_HEALTH=1 requires --skip-dns-check (rehearsal-only combination)"
    fi

    # Custom listen addr flows into config.yaml, the Caddy fragment and the
    # install record (proxyhubctl re-reads it from the record).
    [[ -z $ARG_LISTEN_ADDR ]] || PROXYHUB_LISTEN_ADDR="$ARG_LISTEN_ADDR"

    [[ $NON_INTERACTIVE == 0 || -n $DOMAIN ]] ||
        _ph_die2 "--non-interactive requires --domain (failing closed; the installer never guesses domains or credentials)"
    [[ $NON_INTERACTIVE == 1 || -t 0 ]] ||
        _ph_die2 "interactive mode requires a TTY; use --non-interactive --domain DOMAIN for automation (see --help)"
}

# Idempotency
_check_existing_install() {
    local m1 m2
    m1=$(root_path "$PROXYHUB_INSTALL_INFO")
    m2=$(root_path "$PROXYHUB_UNIT_PATH")
    [[ -f $m1 || -f $m2 ]] || return 0
    _ph_fail "an existing managed ProxyHub installation was detected ($([ -f "$m1" ] && echo "$m1")$([ -f "$m2" ] && echo " $m2"))" \
        "this installer refuses to duplicate it; use 'proxyhubctl update', 'proxyhubctl repair', or 'proxyhubctl uninstall'"
}

# Host validation (READ-ONLY: no DNS/firewall/security-group mutation)
_host_os() { uname -s; }
_host_arch() { uname -m; }

# _check_os - kernel, arch, and distro must be in the supported matrix.
_check_os() {
    local f id version_id
    case $(_host_os) in
        Linux) DETECTED_OS=linux ;;
        *) _ph_fail "unsupported kernel '$(_host_os)': the one-command installer supports Linux only" || return 1 ;;
    esac
    case $(_host_arch) in
        x86_64) DETECTED_ARCH=amd64 ;;
        aarch64 | arm64) DETECTED_ARCH=arm64 ;;
        *) _ph_fail "unsupported architecture '$(_host_arch)': supported architectures are amd64 and arm64" || return 1 ;;
    esac
    f=$(root_path /etc/os-release)
    [[ -r $f ]] ||
        _ph_fail "cannot read /etc/os-release - unsupported host (supported: Ubuntu 22.04/24.04/26.04, Debian 12/13)" || return 1
    id=$(sed -n 's/^ID=//p' "$f" | head -1 | tr -d '"')
    version_id=$(sed -n 's/^VERSION_ID=//p' "$f" | head -1 | tr -d '"')
    case "${id}:${version_id}" in
        ubuntu:22.04 | ubuntu:24.04 | ubuntu:26.04 | debian:12 | debian:13)
            _ph_log "host platform supported: ${id} ${version_id} ${DETECTED_OS}/${DETECTED_ARCH}" ;;
        *) _ph_fail "unsupported OS '${id} ${version_id}': supported systems are Ubuntu 22.04/24.04 and Debian 12/13" || return 1 ;;
    esac
}

# _have_caddy_bin - true when a native caddy binary is on PATH (test seam).
_have_caddy_bin() { command -v caddy >/dev/null 2>&1; }

# _note_bridge_listen_override - when the operator passed --listen-addr and
# the adopted topology is a docker bridge, surface the effective bind: the
# port survives, the host part is replaced by the bridge gateway (the
# user-facing flag stays loopback-only by design, spec decision 5).
_note_bridge_listen_override() {
    [[ $PROXYHUB_CADDY_MODE == docker && $PROXYHUB_DOCKER_NETMODE == bridge && -n $ARG_LISTEN_ADDR ]] || return 0
    _ph_log "--listen-addr ${ARG_LISTEN_ADDR}: bridge mode replaces the loopback host part with the bridge gateway; effective bind ${PROXYHUB_BRIDGE_GATEWAY}:${PROXYHUB_LISTEN_ADDR##*:}"
}

# _select_docker_caddy NAME - explicit --caddy-docker selection: validate the
# container, its port publishing, its /etc/caddy mount and its network
# topology (host-gateway mapping + gateway derivation for bridge networks),
# then adopt the docker Caddy mode (ADR 0035).
_select_docker_caddy() {
    local name=$1
    docker_validate_caddy_container "$name" || return 1
    docker_caddy_ports_published "$name" || return 1
    docker_caddy_mount_root "$name" >/dev/null || return 1
    docker_caddy_prepare_topology "$name" || return 1
    PROXYHUB_CADDY_CONTAINER=$name
    PROXYHUB_CADDY_MODE=docker
    _ph_log "docker caddy mode: integrating container '${name}'"
    _note_bridge_listen_override
}

# _autodetect_docker_caddy - return 0 after adopting the docker mode when
# exactly one running caddy-image container exists (announced); return 1 when
# there are none (caller falls through to the native fail-closed path);
# return 2 after printing the ambiguity error when there are several.
_autodetect_docker_caddy() {
    local candidates count
    candidates=$(docker_caddy_candidates 2>/dev/null) || return 1
    [[ -n $candidates ]] || return 1
    count=$(printf '%s\n' "$candidates" | wc -l | tr -d ' ')
    if ((count > 1)); then
        _ph_err "multiple running caddy containers found; refusing to guess which one to integrate:"
        printf '%s\n' "$candidates" | sed 's/^/[proxyhub]   - /' >&2
        _ph_err "re-run with --caddy-docker <name> to pick one explicitly"
        return 2
    fi
    PROXYHUB_CADDY_CONTAINER=$candidates
    PROXYHUB_CADDY_MODE=docker
    _ph_log "auto-selected the only running caddy container '${candidates}' (docker caddy mode; override with --caddy-docker)"
    docker_caddy_ports_published "$candidates" || return 2
    docker_caddy_mount_root "$candidates" >/dev/null || return 2
    docker_caddy_prepare_topology "$candidates" || return 2
    _note_bridge_listen_override
    return 0
}

# _check_caddy - Caddy mode detection (ADR 0035). Priority: --caddy-docker
# forces docker > a native caddy binary selects native > exactly one running
# caddy-image container selects docker (announced) > fail closed.
_check_caddy() {
    if [[ -n $ARG_CADDY_DOCKER ]]; then
        _select_docker_caddy "$ARG_CADDY_DOCKER"
        return
    fi
    _is_test_mode && return 0
    if _have_caddy_bin; then
        _ph_log "existing Caddy found: $(command caddy version 2>/dev/null | head -1)"; return 0
    fi
    local det=0
    _autodetect_docker_caddy || det=$?
    if ((det == 0)); then return 0; fi
    if ((det == 2)); then return 1; fi
    local conflicts
    conflicts=$(ss -ltnH 2>/dev/null | awk '{print $4}' | sed -n 's/.*:\(80\|443\)$/\1/p' | sort -u | tr '\n' ' ' || true)
    [[ -z ${conflicts// /} ]] ||
        _ph_fail \
            "TCP port(s) ${conflicts% }already bound by another service; Caddy must own 80/443 for TLS" \
            "identify the conflict with: ss -ltnp | grep -E ':(80|443) '  then stop or reconfigure it and re-run" || return 1
    _ph_fail \
        "Caddy v2 is required but not installed: no native caddy binary and no running docker caddy container found" \
        "this installer never installs Caddy from third-party sources; install it from the official Caddy repository: https://caddyserver.com/docs/install#debian-ubuntu-raspbian" \
        "already running caddy in docker? re-run with --caddy-docker <container-name>"
}

_check_dns() { # DOMAIN
    if [[ $SKIP_DNS_CHECK == 1 ]]; then
        _ph_log "DNS check skipped (--skip-dns-check)"; return 0
    fi
    _dns_resolve "$1" ||
        _ph_fail \
            "DNS does not resolve for '$1'" \
            "create an A/AAAA record pointing at this host, wait for propagation, and re-run" \
            "behind a CDN or private network? re-run with --skip-dns-check"
}
_dns_resolve() { getent ahosts "$1" >/dev/null 2>&1 || getent hosts "$1" >/dev/null 2>&1; }

# _validate_host_platform - all READ-ONLY host checks. Nothing here may modify
# DNS records, firewalls, security groups, or routers.
_validate_host_platform() {
    _check_os || return 1
    if ! _is_test_mode; then
        if [[ ! -d /run/systemd/system ]] || ! command -v systemctl >/dev/null 2>&1; then
            _ph_fail "systemd is required but not present (PID 1 is not systemd)" || return 1
        fi
        ((EUID == 0)) || _ph_fail "the installer must run as root (try: sudo bash install.sh ...)" || return 1
    fi
    _curl -fsS --max-time 10 -o /dev/null "https://github.com" ||
        _ph_fail "outbound HTTPS to github.com failed: the installer must reach GitHub releases (read-only check; nothing was modified)" || return 1
    # --no-caddy: the operator brings their own reverse proxy, so neither a
    # local Caddy nor free 80/443 is required.
    if ((NO_CADDY == 0)); then
        _check_caddy || return 1
    else
        _ph_log "--no-caddy: skipping Caddy presence and 80/443 checks"
    fi
}

# Interactive prompts
# _read_prompt VAR PROMPT - read one line from the operator into VAR.
_read_prompt() { # VAR PROMPT
    local _line
    read -r -p "$2" _line || _ph_fail "input closed" || return 1
    printf -v "$1" '%s' "$_line"
}

# _gather_interactive - prompt for missing domain and optional ACME email.
_gather_interactive() {
    local input
    while [[ -z $DOMAIN ]]; do
        _read_prompt input "Public domain (e.g. proxy.example.com): " || return 1
        validate_domain "$input" && DOMAIN=$input
    done
    [[ -n $EMAIL ]] && return 0
    read -r -p "ACME account email (optional, Enter to skip): " input || return 0
    if [[ -n $input ]] && ! _validate_email "$input"; then
        _ph_err "invalid email '${input}' - continuing without one"; input=""
    fi
    EMAIL=$input
}
# _obtain_site_path - --site-path wins; otherwise generate a 20-char random
# path and let interactive operators confirm or replace it.
_obtain_site_path() {
    if [[ -n $ARG_SITE_PATH ]]; then
        validate_site_path "$ARG_SITE_PATH" || return 1
        SITE_PATH=$ARG_SITE_PATH; return 0
    fi
    local generated=""
    until validate_site_path "$generated" >/dev/null 2>&1; do
        generated=$(random_token 20) || return 1
    done
    [[ $NON_INTERACTIVE == 1 ]] && { SITE_PATH=$generated; return 0; }
    _ph_log "generated Site Path: ${generated} (management UI: https://${DOMAIN:-<domain>}/${generated}/)"
    local input
    while :; do
        _read_prompt input "Press Enter to accept, or type your own Site Path: " || return 1
        [[ -z $input ]] && { SITE_PATH=$generated; return 0; }
        validate_site_path "$input" && { SITE_PATH=$input; return 0; }
    done
}

# Credentials (never passed as command-line arguments)
_generate_credentials() {
    local suffix
    suffix=$(LC_ALL=C tr -dc 'a-z0-9' </dev/urandom 2>/dev/null | head -c 8) || true
    [[ ${#suffix} -eq 8 ]] || _ph_fail "failed to generate a random admin username" || return 1
    ADMIN_USER="ph${suffix}"
    ADMIN_PASSWORD=$(random_token 32) || return 1
    [[ ${#ADMIN_PASSWORD} -ge 24 ]] || _ph_fail "generated admin password is too short"
}

# Download + verify (HTTPS verification is never disabled)
_curl() { command curl "$@"; }
_fetch() { _curl -fsSL --max-time 120 -o "$2" "$1" || _ph_fail "download failed: $1"; }

_resolve_latest_tag() { # REPO -> stdout tag
    local effective tag
    effective=$(_curl -fsSIL --max-time 15 -o /dev/null -w '%{url_effective}' \
        "https://github.com/$1/releases/latest") ||
        _ph_fail "could not resolve the latest release of $1 (network error, or no stable release published yet)" || return 1
    tag=${effective##*/}
    if [[ -z $tag || $tag == latest ]] || ! validate_version "$tag" >/dev/null; then
        _ph_fail "unexpected latest-release redirect for $1: '${effective}'"
        return 1
    fi
    printf '%s' "$tag"
}

_download_and_verify() { # WORKDIR
    local workdir=$1 asset base line_file="${1}/.checksum-line"
    asset="proxyhub_${VERSION_TAG#v}_${DETECTED_OS}_${DETECTED_ARCH}.tar.gz"
    base="https://github.com/${REPO}/releases/download/${VERSION_TAG}"

    _ph_log "downloading ${base}/${asset}"
    _fetch "${base}/SHA256SUMS" "${workdir}/SHA256SUMS" || return 1
    _fetch "${base}/${asset}" "${workdir}/${asset}" || return 1

    # Extract ONLY the matching checksum line; exactly one entry must exist.
    grep -F -- "$asset" "${workdir}/SHA256SUMS" | grep -E '^[0-9a-fA-F]{64}[[:space:]]+[^[:space:]]+$' >"$line_file" || true
    [[ $(wc -l <"$line_file" | tr -d ' ') == 1 ]] ||
        _ph_fail "SHA256SUMS does not contain exactly one entry for ${asset} - refusing to install" || return 1

    _ph_log "verifying SHA256 checksum of ${asset}"
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$workdir" && sha256sum -c "$line_file")
    elif command -v shasum >/dev/null 2>&1; then
        (cd "$workdir" && shasum -a 256 -c "$line_file")
    else
        _ph_fail "neither sha256sum nor shasum is available for checksum verification" || return 1
    fi || _ph_fail "checksum verification FAILED for ${asset} - the download is corrupt or substituted; refusing to install" || return 1

    mkdir -p "${workdir}/extract"
    tar -xzf "${workdir}/${asset}" -C "${workdir}/extract" || _ph_fail "failed to unpack ${asset}" || return 1
    [[ -f "${workdir}/extract/proxyhub" ]] || _ph_fail "${asset} does not contain the proxyhub binary"
}

# Install steps
_write_config() {
    local cfg listen_port server_host trusted_line=""
    cfg=$(root_path "${PROXYHUB_CONFIG_DIR}/config.yaml")
    listen_port="${PROXYHUB_LISTEN_ADDR##*:}"
    server_host="127.0.0.1"
    # Docker bridge topology (ADR 0035): the admin plane binds the bridge
    # gateway so the containerized Caddy can reach it, and XFF trust narrows
    # to the bridge subnet. Host-network/native keep the loopback bind and
    # stay byte-identical to before.
    if [[ $PROXYHUB_CADDY_MODE == docker && $PROXYHUB_DOCKER_NETMODE == bridge ]]; then
        server_host=$PROXYHUB_BRIDGE_GATEWAY
        trusted_line="  trusted_proxies: [\"${PROXYHUB_BRIDGE_SUBNET}\"]"$'\n'
    fi
    mkdir -p "$(dirname "$cfg")" || _ph_fail "failed to create $(dirname "$cfg")" || return 1
    if ! cat >"$cfg" <<EOF
server:
  host: "${server_host}"
  port: ${listen_port}
${trusted_line}storage:
  path: "${PROXYHUB_STATE_DIR}/data.db"
health_check:
  interval: 15m
  latency_threshold: 500
  test_url: "https://www.google.com"
filter:
  nodes_per_region: 10
  deduplicate: true
EOF
    then
        rm -f "$cfg"; _ph_fail "failed to write ${cfg}"; return 1
    fi
    chmod 0640 "$cfg" || { rm -f "$cfg"; return 1; }
    if ! _is_test_mode && ((EUID == 0)) &&
        ! chown "${PROXYHUB_USER}:${PROXYHUB_GROUP}" "$cfg"; then
        rm -f "$cfg"; _ph_fail "failed to chown ${cfg}"; return 1
    fi
    _ph_log "wrote ${cfg}"
}

# _run_svc_tool NAME ARGS... - run a system tool; no-op under PROXYHUB_ROOT.
_run_svc_tool() {
    local tool=$1
    shift
    if _is_test_mode; then
        _ph_log "test mode: ${tool} $*"; return 0
    fi
    command "$tool" "$@"
}
_systemctl() { _run_svc_tool systemctl "$@"; }

# _as_service_user CMD... - run CMD as the low-privilege service user so files
# it creates (the SQLite database) are owned by proxyhub:proxyhub.
_as_service_user() {
    if _is_test_mode; then "$@"; else runuser -u "$PROXYHUB_USER" -- "$@"; fi
}

# _run_proxyhub_init - the password travels ONLY through the stdin pipe: it
# never appears in argv, `ps`, shell history, or log files.
_run_proxyhub_init() {
    local bin cfg rc=0
    bin=$(root_path "$PROXYHUB_BINARY")
    cfg=$(root_path "${PROXYHUB_CONFIG_DIR}/config.yaml")
    _ph_log "initializing ProxyHub (admin credentials passed via protected stdin channel)"
    printf '%s\n' "$ADMIN_PASSWORD" | _as_service_user "$bin" init \
        --config "$cfg" --domain "$DOMAIN" --username "$ADMIN_USER" \
        --site-path "$SITE_PATH" --password-stdin || rc=$?
    ((rc == 0)) || _ph_fail "proxyhub init failed (exit ${rc})"
}

# Caddy integration
# _ensure_caddy_import - guarantee the main Caddyfile imports conf.d/*.caddy,
# backing it up first. In docker mode the Caddyfile lives at the container's
# /etc/caddy mount root (caddy_config_dir); the import line itself keeps
# container path semantics.
_ensure_caddy_import() {
    local cdir main
    cdir=$(caddy_config_dir) || return 1
    main="${cdir}/Caddyfile"
    mkdir -p "$cdir" || return 1
    CADDY_BACKUP=""
    if [[ ! -f $main ]]; then
        {
            [[ -z $EMAIL ]] || printf '{\n\temail %s\n}\n\n' "$EMAIL"
            printf '%s\n' "$PROXYHUB_CADDY_IMPORT_LINE"
        } >"$main" || return 1
        _ph_log "wrote minimal ${main}"; return 0
    fi
    CADDY_BACKUP="${main}.proxyhub-bak-${TIMESTAMP}"
    cp -a "$main" "$CADDY_BACKUP" || { CADDY_BACKUP=""; _ph_fail "failed to back up ${main}"; return 1; }
    grep -qF "$PROXYHUB_CADDY_IMPORT_LINE" "$main" || {
        printf '\n%s\n' "$PROXYHUB_CADDY_IMPORT_LINE" >>"$main" || return 1
        _ph_log "appended conf.d import to ${main} (backup: ${CADDY_BACKUP})"
    }
    # Optional ACME account email lives in the global options block; existing
    # Caddyfiles are reused, so the operator adds it there if missing.
    if [[ -n $EMAIL ]] && ! grep -Eq '^[[:space:]]*email[[:space:]]' "$main"; then
        _ph_log "NOTE: add 'email ${EMAIL}' to the global options block in ${main} for ACME expiry notices"
    fi
}

# _reload_caddy - test-override seam over the mode-dispatched caddy_reload
# channel (lib.sh). The channel reloads via the admin API and falls back to
# a full restart when the API is disabled ('admin off' in the global options
# block, e.g. 233boy-style Caddyfiles), briefly interrupting the other sites
# on this Caddy (warned there).
_reload_caddy() { caddy_reload; }

_configure_caddy() {
    local caddy_dir frag hit
    caddy_dir=$(caddy_config_dir) || return 1
    frag=$(caddy_fragment_path) || return 1
    if [[ -d $caddy_dir ]]; then
        hit=$(grep -RIlF -- "$DOMAIN" "$caddy_dir" 2>/dev/null | grep -v "^${frag}$" | head -1 || true)
        [[ -z $hit ]] ||
            _ph_fail \
                "domain '${DOMAIN}' already appears in an existing Caddy config: ${hit}" \
                "refusing to create a conflicting site block; remove or rename the existing site and re-run" || return 1
    fi
    write_caddy_fragment "$DOMAIN" "$SITE_PATH" || return 1
    _ensure_caddy_import || return 1
    local rc=0
    if _is_test_mode; then
        _ph_log "test mode: caddy fmt/validate/reload"
    else
        caddy_fmt "$frag" >/dev/null &&
            caddy_validate "${caddy_dir}/Caddyfile" &&
            _reload_caddy || rc=$?
    fi
    if ((rc != 0)); then
        _ph_err "caddy fmt/validate/reload failed - rolled back the Caddy changes (inspect with: journalctl -u caddy -n 50)"
        rm -f "$frag"
        [[ -z $CADDY_BACKUP || ! -f $CADDY_BACKUP ]] || cp -a "$CADDY_BACKUP" "${caddy_dir}/Caddyfile" || true
        return 1
    fi
    _ph_log "Caddy configured for https://${DOMAIN}/${SITE_PATH}/"
}

# _write_reverse_proxy_hints - --no-caddy mode: drop ready-to-adapt Caddy and
# nginx examples next to config.yaml. Both mirror the managed fragment's
# routing and its XFF replacement rule (spoofed X-Forwarded-For must never
# survive the proxy hop, or IP2Ban/captcha/blacklist are bypassable).
_write_reverse_proxy_hints() {
    local caddy_hint nginx_hint
    caddy_hint=$(root_path "${PROXYHUB_CONFIG_DIR}/reverse-proxy.caddy")
    nginx_hint=$(root_path "${PROXYHUB_CONFIG_DIR}/reverse-proxy.nginx.conf")
    cat >"$caddy_hint" <<EOF
# ProxyHub reverse-proxy example (Caddy v2), generated by install.sh --no-caddy.
# Merge into YOUR Caddy setup (e.g. /etc/caddy/conf.d/proxyhub.caddy with an
# import line in the main Caddyfile). Caddy auto-HTTPS still requires that
# 80/43 reach THIS Caddy instance and that ${DOMAIN} resolves here.
${DOMAIN} {
	@proxyhub path /${SITE_PATH} /${SITE_PATH}/*
	handle @proxyhub {
		# Replace (not append) forwarding headers: a caller-supplied
		# X-Forwarded-For must never survive the proxy hop.
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
    cat >"$nginx_hint" <<EOF
# ProxyHub reverse-proxy example (nginx), generated by install.sh --no-caddy.
# Adapt into your existing TLS server block for ${DOMAIN}.
server {
    listen 443 ssl;
    server_name ${DOMAIN};
    # ssl_certificate     /path/to/fullchain.pem;
    # ssl_certificate_key /path/to/privkey.pem;

    location = /${SITE_PATH} { return 301 /${SITE_PATH}/; }
    location /${SITE_PATH}/ {
        proxy_pass http://${PROXYHUB_LISTEN_ADDR};
        proxy_set_header Host \$host;
        # Replace, not append: a caller-supplied X-Forwarded-For must never
        # survive the proxy hop (IP2Ban / captcha / blacklist bypass).
        proxy_set_header X-Forwarded-For \$remote_addr;
        proxy_set_header X-Real-IP \$remote_addr;
    }
}
EOF
    chmod 0640 "$caddy_hint" "$nginx_hint" || { rm -f "$caddy_hint" "$nginx_hint"; return 1; }
    _ph_log "wrote reverse-proxy examples: ${caddy_hint} ${nginx_hint}"
}

# Health verification

# _verify_url URL TRIES UNIT - poll URL until healthy; UNIT names the systemd
# unit whose logs diagnose a failure.
_verify_url() { # URL TRIES UNIT
    local url=$1 tries=$2 i
    for ((i = 0; i < tries; i++)); do
        _curl -fsS --max-time 5 -o /dev/null "$url" && {
            _ph_log "healthy: $url"
            return 0
        }
        sleep 1
    done
    _ph_fail "health check failed: $url" "inspect with: journalctl -u $3 -n 50"
}

_verify_health() {
    local probe_addr=$PROXYHUB_LISTEN_ADDR
    # Docker bridge topology: the loopback probe targets the bridge gateway
    # address (reachable from the host), where the admin plane now listens.
    if [[ $PROXYHUB_CADDY_MODE == docker && $PROXYHUB_DOCKER_NETMODE == bridge ]]; then
        probe_addr="${PROXYHUB_BRIDGE_GATEWAY}:${PROXYHUB_LISTEN_ADDR##*:}"
    fi
    _verify_url "http://${probe_addr}/${SITE_PATH}/healthz" "$PROXYHUB_LOOPBACK_HEALTH_TRIES" proxyhub || return 1
    if [[ $NO_CADDY == 1 ]]; then
        _ph_log "public HTTPS health check skipped (--no-caddy): verify your own reverse proxy forwards /${SITE_PATH}/ to http://${PROXYHUB_LISTEN_ADDR}"
        return 0
    fi
    if [[ ${PROXYHUB_SKIP_PUBLIC_HEALTH:-0} == 1 ]]; then
        _ph_log "WARNING: public HTTPS health check skipped (PROXYHUB_SKIP_PUBLIC_HEALTH=1 - rehearsal only, certificate not verified)"
        return 0
    fi
    _verify_url "https://${DOMAIN}/${SITE_PATH}/healthz" "$PROXYHUB_PUBLIC_HEALTH_TRIES" caddy ||
        _ph_fail "common causes: DNS not pointing at this host, firewall blocking TCP 80/443, or ACME rate limits"
}

# _write_install_record - record the install (never the password) in the
# root-only (0600) install-info file, then print the one-time summary.
_write_install_record() {
    local rec
    rec=$(root_path "$PROXYHUB_INSTALL_INFO")
    mkdir -p "$(dirname "$rec")" || return 1
    # Caddy mode (ADR 0035): detected by _check_caddy (native default, docker
    # when a container was selected); --no-caddy records none. CADDY_CONTAINER
    # is only set in docker mode.
    local caddy_mode=$PROXYHUB_CADDY_MODE
    ((NO_CADDY == 0)) || caddy_mode=none
    printf '# ProxyHub installation record - managed by install.sh, keep root-only.\nDOMAIN=%s\nSITE_PATH=%s\nREPO=%s\nVERSION=%s\nINSTALLED_AT=%s\nADMIN_USER=%s\nLISTEN_ADDR=%s\nNO_CADDY=%s\nCADDY_MODE=%s\nCADDY_CONTAINER=%s\n' \
        "$DOMAIN" "$SITE_PATH" "$REPO" "$VERSION_TAG" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        "$ADMIN_USER" "$PROXYHUB_LISTEN_ADDR" "$NO_CADDY" "$caddy_mode" "$PROXYHUB_CADDY_CONTAINER" >"$rec" ||
        _ph_fail "failed to write ${rec}" || return 1
    chmod 0600 "$rec" || _ph_fail "failed to chmod ${rec}" || return 1
    _ph_log "wrote ${rec} (mode 0600)"

    printf '\n============================================================\n  ProxyHub %s installed\n  Management URL : https://%s/%s/\n  Admin username : %s\n  Admin password : %s\n============================================================\n  Password shown ONCE and NOT stored in %s.\n  Save it in a password manager, then change it after first login.\n  Operations: proxyhubctl status | logs | restart | update\n\n' \
        "$VERSION_TAG" "$DOMAIN" "$SITE_PATH" "$ADMIN_USER" "$ADMIN_PASSWORD" "$PROXYHUB_INSTALL_INFO"
    if ((NO_CADDY == 1)); then
        printf '  --no-caddy: no reverse proxy was configured. Adapt one of the\n  generated examples to your setup, then browse the Management URL:\n    %s\n    %s\n  Both enforce the X-Forwarded-For replacement rule (security).\n\n' \
            "$(root_path "${PROXYHUB_CONFIG_DIR}/reverse-proxy.caddy")" \
            "$(root_path "${PROXYHUB_CONFIG_DIR}/reverse-proxy.nginx.conf")"
    fi
    # Docker bridge topology security disclosure (ADR 0035 consequences): the
    # bounded widening of the admin-plane trust boundary must be stated in
    # the one-time summary so the operator can make an informed choice.
    if [[ $caddy_mode == docker && $PROXYHUB_DOCKER_NETMODE == bridge ]]; then
        printf '  WARNING (docker bridge mode): the management-plane trust boundary\n  widened from loopback to the %s docker bridge. Any container on\n  this bridge can reach the admin plane directly (no TLS) and can\n  spoof X-Forwarded-For. If you do not trust the other containers on\n  this bridge, use a host-network caddy container or native Caddy\n  instead, and isolate caddy on a dedicated bridge network.\n\n' \
            "$PROXYHUB_BRIDGE_SUBNET"
    fi
}

# Main
main() {
    parse_args "$@"

    _check_existing_install || _die "existing managed installation detected - nothing was changed"
    _validate_host_platform || _die "host validation failed - nothing was changed"

    if [[ $NON_INTERACTIVE == 0 ]]; then
        _gather_interactive || _die "interactive input failed"
    fi

    _check_dns "$DOMAIN" || _die "DNS validation failed for '${DOMAIN}'"
    _obtain_site_path || _die "no valid Site Path available"
    _generate_credentials || _die "credential generation failed"

    if [[ $VERSION == latest ]]; then
        VERSION_TAG=$(_resolve_latest_tag "$REPO") || _die "could not resolve the latest stable release of ${REPO}"
    else
        VERSION_TAG="v${VERSION#v}"
    fi
    _ph_log "installing ProxyHub ${VERSION_TAG} (${DETECTED_OS}/${DETECTED_ARCH}) from ${REPO}"

    TIMESTAMP=$(date -u +%Y%m%d%H%M%S)
    WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-install.XXXXXX") || _die "cannot create a temporary workspace"
    _download_and_verify "$WORKDIR" || _die "release download or checksum verification failed"
    if ! mkdir -p "$(dirname "$(root_path "$PROXYHUB_BINARY")")" ||
        ! install -m 0755 "${WORKDIR}/extract/proxyhub" "$(root_path "$PROXYHUB_BINARY")"; then
        _die "failed to install $(root_path "$PROXYHUB_BINARY")"
    fi
    _ph_log "installed $(root_path "$PROXYHUB_BINARY")"
    # Install the operator CLI and its helper library next to the binary.
    # proxyhubctl sources proxyhubctl-lib.sh from its own directory (see the
    # candidate search in proxyhubctl). Standalone fetches (curl | bash) have
    # no checkout on disk: download proxyhubctl from the same repo, and reuse
    # the companion lib already fetched into _PH_LIB_TMP at source time.
    _ph_ctl="${SCRIPT_DIR}/scripts/install/proxyhubctl"
    if [[ ! -r $_ph_ctl ]]; then
        if [[ -z $_PH_LIB_TMP ]]; then
            _PH_LIB_TMP=$(mktemp -d)
            chmod 700 "$_PH_LIB_TMP"
        fi
        _ph_ctl_url="${PROXYHUB_CTL_URL:-https://raw.githubusercontent.com/taliove/proxyhub/main/scripts/install/proxyhubctl}"
        curl -fsSL "$_ph_ctl_url" -o "$_PH_LIB_TMP/proxyhubctl" ||
            _die "failed to download proxyhubctl from ${_ph_ctl_url}"
        _ph_ctl="$_PH_LIB_TMP/proxyhubctl"
    fi
    install -m 0755 "$_ph_ctl" \
        "$(root_path /usr/local/bin/proxyhubctl)" ||
        _die "failed to install $(root_path /usr/local/bin/proxyhubctl)"
    install -m 0644 "$_PH_LIB_PATH" \
        "$(root_path /usr/local/bin/proxyhubctl-lib.sh)" ||
        _die "failed to install $(root_path /usr/local/bin/proxyhubctl-lib.sh)"
    _ph_log "installed $(root_path /usr/local/bin/proxyhubctl)"
    if ! ensure_proxyhub_group || ! ensure_proxyhub_user || ! ensure_directories; then
        _die "failed to create the service identity and directory layout"
    fi
    _write_config || _die "failed to write config.yaml"
    write_proxyhub_unit || _die "failed to write the systemd unit"
    _run_proxyhub_init || _die "proxyhub init failed"
    _systemctl daemon-reload || _die "systemctl daemon-reload failed"
    _systemctl enable --now proxyhub.service ||
        _die "failed to enable/start proxyhub.service - inspect with: journalctl -u proxyhub -n 50"
    if ((NO_CADDY == 1)); then
        _write_reverse_proxy_hints || _die "failed to write reverse-proxy examples"
    else
        _configure_caddy || _die "Caddy configuration failed"
    fi
    _verify_health || _die "post-install health verification failed"
    _write_install_record || _die "failed to write ${PROXYHUB_INSTALL_INFO}"

    unset ADMIN_PASSWORD
}

if [[ ${PROXYHUB_INSTALL_NO_MAIN:-0} != 1 ]]; then
    umask 022
    trap 'rc=$?; trap - EXIT; [[ -z ${WORKDIR:-} || ! -d ${WORKDIR:-} ]] || rm -rf -- "$WORKDIR"; [[ -z ${_PH_LIB_TMP:-} || ! -d ${_PH_LIB_TMP:-} ]] || rm -rf -- "$_PH_LIB_TMP"; exit "$rc"' EXIT
    trap 'exit 129' HUP; trap 'exit 130' INT; trap 'exit 143' TERM
    main "$@"
fi
