#!/usr/bin/env bash
# test_docker_caddy.sh - thin integration tests for the Docker Caddy install
# mode (ADR 0035) against REAL caddy:2 containers on the local Docker daemon.
#
# Usage: bash scripts/install/test_docker_caddy.sh
#
# Environment guard: without a reachable Docker daemon (or when the caddy:2
# image can neither be found locally nor pulled) the suite prints SKIP and
# exits 0 - it must never fail on docker-less hosts.
#
# Seam strategy: install.sh is sourced with PROXYHUB_INSTALL_NO_MAIN=1 and the
# docker seam is turned real (_docker -> command docker); _is_test_mode is
# overridden to false so the REAL in-container caddy fmt/validate/reload
# channel runs, while PROXYHUB_ROOT still redirects every installer file
# write into a scratch tree. The remaining seams (_curl, ss, useradd,
# systemctl) stay mocked or are never reached because the driver walks only
# the Caddy-relevant install steps (parse_args -> _check_caddy ->
# _obtain_site_path -> _generate_credentials -> _write_config ->
# _configure_caddy -> _write_install_record) instead of full main; the rest
# of the pipeline is covered by test_install.sh's mocked suites.
#
# Mount path trick: `docker inspect` reports the REAL host path of a bind
# mount, but every installer write goes through root_path (PROXYHUB_ROOT
# prefix). A symlink at "$PROXYHUB_ROOT<real-source>" pointing back at the
# real mount directory makes root_path(real source) resolve into the
# directory the container actually serves, so fragments written by the
# installer become visible inside the container.
#
# Test isolation: every container carries the label proxyhub.test=docker-caddy
# and a phdc-* name; a trap removes them (plus scratch trees and placeholder
# listeners) even on failure. Enumeration-sensitive cases (auto-detect, zero,
# multi) are skipped when foreign caddy containers are already running.
#
# SC2030/SC2031/SC2034/SC2153: mock functions and globals are consumed inside
# command-substitution subshells and by the sourced install.sh, which the
# static analyzer cannot follow. SC2329: the seam overrides (_docker,
# _is_test_mode, _have_caddy_bin, ss, _curl) are invoked indirectly by the
# sourced install.sh.
# shellcheck disable=SC2030,SC2031,SC2034,SC2153,SC2329
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

readonly TEST_LABEL="proxyhub.test=docker-caddy"
readonly CADDY_IMAGE="caddy:2"
readonly TEST_DOMAIN="proxy.example.com"

PASS=0
FAIL=0
SKIPS=0
TEST_DIRS=()
CASE_CONTAINERS=()
CASE_VOLUMES=()
PLACEHOLDER_PID=""

_pass() { PASS=$((PASS + 1)); }
_fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$*" >&2; }
_skip() { SKIPS=$((SKIPS + 1)); printf 'SKIP: %s\n' "$*"; }

_assert_eq() { # EXPECTED ACTUAL LABEL
    if [[ $1 == "$2" ]]; then _pass; else _fail "($3) expected [$1], got [$2]"; fi
}

_assert_file_contains() { # FILE NEEDLE
    if grep -qF -- "$2" "$1"; then _pass; else _fail "$1 does not contain [$2]"; fi
}

_assert_file_not_contains() { # FILE NEEDLE
    if grep -qF -- "$2" "$1"; then _fail "$1 unexpectedly contains [$2]"; else _pass; fi
}

_assert_out_contains() { # FILE NEEDLE LABEL - needle found in an output file
    if grep -qF -- "$2" "$1"; then _pass; else _fail "($3) output missing [$2]"; fi
}

# --------------------------------------------------------------------------
# Environment guards (skip, never fail, when Docker is unusable)
# --------------------------------------------------------------------------

if ! command -v docker >/dev/null 2>&1; then
    printf 'SKIP: docker CLI not found; Docker Caddy integration tests skipped\n'
    exit 0
fi
if ! docker info >/dev/null 2>&1; then
    printf 'SKIP: docker daemon unreachable; Docker Caddy integration tests skipped\n'
    exit 0
fi
if ! docker image inspect "$CADDY_IMAGE" >/dev/null 2>&1; then
    printf 'pulling %s for the integration tests...\n' "$CADDY_IMAGE" >&2
    if ! docker pull "$CADDY_IMAGE" >/dev/null 2>&1; then
        printf 'SKIP: %s unavailable locally and pull failed\n' "$CADDY_IMAGE"
        exit 0
    fi
fi

# Foreign running caddy containers pollute the auto-detect enumeration.
FOREIGN_CADDY=$(
    docker ps --format '{{.Names}} {{.Image}}' | while read -r _name _image; do
        _img=${_image##*/}
        _img=${_img%%[:@]*}
        [[ $_img == caddy ]] && printf '%s\n' "$_name"
    done || true # the final read at EOF makes the while pipeline return 1
)
HAVE_PYTHON3=0
command -v python3 >/dev/null 2>&1 && HAVE_PYTHON3=1

# --------------------------------------------------------------------------
# Cleanup (trap backstop: containers, placeholder listener, scratch trees)
# --------------------------------------------------------------------------

_stop_placeholder() {
    [[ -z $PLACEHOLDER_PID ]] || kill "$PLACEHOLDER_PID" 2>/dev/null || true
    PLACEHOLDER_PID=""
}

_stop_case_containers() {
    ((${#CASE_CONTAINERS[@]} == 0)) && return 0
    docker rm -f "${CASE_CONTAINERS[@]}" >/dev/null 2>&1 || true
    CASE_CONTAINERS=()
}

_stop_case_volumes() {
    ((${#CASE_VOLUMES[@]} == 0)) && return 0
    docker volume rm -f "${CASE_VOLUMES[@]}" >/dev/null 2>&1 || true
    CASE_VOLUMES=()
}

_cleanup() {
    _stop_placeholder
    _stop_case_containers
    _stop_case_volumes
    docker ps -aq --filter "label=$TEST_LABEL" 2>/dev/null |
        xargs -r docker rm -f >/dev/null 2>&1 || true
    local d
    for d in "${TEST_DIRS[@]:-}"; do
        [[ -n $d ]] && rm -rf -- "$d"
    done
}
trap _cleanup EXIT

# --------------------------------------------------------------------------
# Helpers
# --------------------------------------------------------------------------

# new_scratch - create and register a fresh scratch tree; sets SBX.
new_scratch() {
    SBX=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-docker-caddy-test.XXXXXX")
    TEST_DIRS+=("$SBX")
}

# setup_mount - create the caddy mount dir ($SBX/srv/caddy) plus the symlink
# that maps root_path(real mount source) back into the real mount; prints the
# mount path. See the header comment for why the symlink is needed.
setup_mount() {
    local mount="$SBX/srv/caddy"
    mkdir -p "$mount/conf.d"
    mkdir -p "$SBX$(dirname "$mount")"
    ln -s "$mount" "$SBX$mount"
    printf '%s\n' "$mount"
}

# write_caddyfile PATH IMPORT HTTPS_PORT - write the pre-existing container
# Caddyfile: auto_https off (the test domain never triggers real ACME), an
# optional conf.d import line (IMPORT=1), an optional HTTPS port override
# (host-network containers must not squat on the host's 443).
write_caddyfile() { # PATH IMPORT(0|1) HTTPS_PORT(empty|N)
    {
        printf '{\n\tauto_https off\n'
        [[ -z $3 ]] || printf '\thttps_port %s\n' "$3"
        printf '}\n\n'
        [[ $2 == 0 ]] || printf 'import /etc/caddy/conf.d/*.caddy\n'
    } >"$1"
}

# start_caddy NAME [docker run args...] - start a labeled test container.
start_caddy() {
    local name=$1
    shift
    CASE_CONTAINERS+=("$name")
    docker run -d --label "$TEST_LABEL" --name "$name" "$@" "$CADDY_IMAGE" >/dev/null
}

# wait_running NAME - wait until the container reports State.Running.
wait_running() {
    local i
    for i in $(seq 1 20); do
        [[ $(docker inspect --format '{{.State.Running}}' "$1" 2>/dev/null) == true ]] && return 0
        sleep 0.5
    done
    return 1
}

# wait_admin NAME - wait until the in-container admin API answers (the real
# reload channel depends on it; reloading too early would mask itself behind
# the docker-restart fallback).
wait_admin() {
    local i
    for i in $(seq 1 40); do
        docker exec "$1" wget -q -O /dev/null http://127.0.0.1:2019/config/ 2>/dev/null && return 0
        sleep 0.5
    done
    return 1
}

# admin_config NAME - print the container's ACTIVE caddy config (evidence of
# what the last reload actually loaded).
admin_config() {
    docker exec "$1" wget -q -O- http://127.0.0.1:2019/config/ 2>/dev/null
}

# drive_install ARGS... - run the Caddy-relevant install steps in a subshell
# with the docker seam real; extra args (e.g. --caddy-docker NAME) are passed
# to parse_args. Logs go to $SBX/install.{out,err}; prints RC=<n>.
drive_install() {
    (
        export PROXYHUB_INSTALL_NO_MAIN=1
        # shellcheck source=../../install.sh
        # shellcheck disable=SC1090,SC1091
        source "$REPO_ROOT/install.sh"
        export PROXYHUB_ROOT=$SBX
        _docker() { command docker "$@"; }
        _is_test_mode() { return 1; }
        _have_caddy_bin() { return 1; }
        ss() { :; }
        _curl() { return 0; }
        TIMESTAMP=$(date -u +%Y%m%d%H%M%S) EMAIL="" VERSION_TAG="v9.9.9"
        rc=0
        {
            parse_args --non-interactive --domain "$TEST_DOMAIN" --skip-dns-check "$@" &&
                _check_caddy &&
                _obtain_site_path &&
                _generate_credentials &&
                _write_config &&
                _configure_caddy &&
                _write_install_record
        } >"$SBX/install.out" 2>"$SBX/install.err" || rc=$?
        printf 'RC=%d\n' "$rc"
    )
}

# start_placeholder PORT DIR BIND_ADDR - serve DIR on BIND_ADDR:PORT. The
# bridge case passes the gateway address: reachable both from the host and,
# through the host-gateway mapping, from bridge containers - never 0.0.0.0.
start_placeholder() {
    (cd "$2" && exec python3 -m http.server "$1" --bind "$3" >/dev/null 2>&1) &
    PLACEHOLDER_PID=$!
    local i
    for i in $(seq 1 20); do
        curl -fsS -o /dev/null "http://$3:$1/" 2>/dev/null && return 0
        sleep 0.5
    done
    return 1
}

# bridge_facts NAME - print "GATEWAY SUBNET" of the container's bridge.
bridge_facts() {
    local gw
    gw=$(docker inspect --format '{{range $n, $net := .NetworkSettings.Networks}}{{printf "%s" $net.Gateway}}{{end}}' "$1")
    local subnet
    subnet=$(docker network inspect bridge --format '{{range .IPAM.Config}}{{printf "%s\n" .Subnet}}{{end}}' | head -1)
    printf '%s %s\n' "$gw" "$subnet"
}

# --------------------------------------------------------------------------
# Case 1: bridge container, single-container AUTO-DETECT, real fmt/validate/
# reload, gateway bind + trusted_proxies narrowing, live connectivity.
# --------------------------------------------------------------------------
case_auto_detect_bridge() {
    printf '==> case: bridge auto-detect install\n'
    new_scratch
    local mount
    mount=$(setup_mount)
    write_caddyfile "$mount/Caddyfile" 1 ""
    local name=phdc-auto
    if ! start_caddy "$name" \
        --add-host host.docker.internal:host-gateway \
        -p 127.0.0.1:18080:80 -p 127.0.0.1:18443:443 \
        -v "$mount:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_admin "$name" || _fail "admin API never came up: $name"

    local rc
    rc=$(drive_install | tail -1)
    _assert_eq "RC=0" "$rc" "bridge auto-detect install rc"
    _assert_out_contains "$SBX/install.err" \
        "auto-selected the only running caddy container '$name'" "auto-detect announcement"

    local facts gw subnet
    facts=$(bridge_facts "$name")
    gw=${facts%% *}
    subnet=${facts##* }
    _assert_file_contains "$SBX/etc/proxyhub/config.yaml" "host: \"$gw\""
    _assert_file_contains "$SBX/etc/proxyhub/config.yaml" "trusted_proxies: [\"$subnet\"]"

    local frag="$mount/conf.d/proxyhub.caddy"
    _assert_file_contains "$frag" "reverse_proxy host.docker.internal:8080"
    _assert_file_contains "$frag" "header_up X-Forwarded-For {remote_host}"
    _assert_file_contains "$frag" "header_up X-Real-IP {remote_host}"
    if docker exec "$name" test -f /etc/caddy/conf.d/proxyhub.caddy; then
        _pass
    else _fail "fragment not visible inside the container"; fi

    # The reload really happened: the active config serves the new site.
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _pass
    else _fail "active caddy config does not serve $TEST_DOMAIN after install"; fi
    _assert_file_contains "$SBX/root/.proxyhub-install-info" "CADDY_MODE=docker"
    _assert_file_contains "$SBX/root/.proxyhub-install-info" "CADDY_CONTAINER=$name"
    _assert_out_contains "$SBX/install.out" "trust boundary" "bridge summary warning"

    # Connectivity essence: a host listener bound on the gateway address is
    # reachable from the bridge container via host.docker.internal (the exact
    # reach-back path the fragment's reverse_proxy target uses). A python3
    # placeholder stands in for the proxyhub listener, which the rehearsal
    # install never starts.
    if ((HAVE_PYTHON3 == 1)); then
        mkdir -p "$SBX/www"
        printf 'proxyhub-placeholder-ok' >"$SBX/www/index.html"
        if start_placeholder 18099 "$SBX/www" "$gw"; then
            local host_side container_side
            host_side=$(curl -fsS --max-time 5 "http://$gw:18099/" 2>/dev/null || true)
            _assert_eq "proxyhub-placeholder-ok" "$host_side" "gateway-address listener reachable from host"
            container_side=$(docker exec "$name" wget -q -O- --timeout=5 \
                "http://host.docker.internal:18099/" 2>/dev/null || true)
            _assert_eq "proxyhub-placeholder-ok" "$container_side" \
                "container reaches host listener via host.docker.internal"
            _stop_placeholder
        else
            _fail "placeholder listener failed to start"
        fi
    else
        _skip "python3 missing; bridge connectivity assertion skipped"
    fi
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 2: host-network container, EXPLICIT --caddy-docker, zero-change path.
# --------------------------------------------------------------------------
case_explicit_host_network() {
    printf '==> case: host-network explicit install\n'
    new_scratch
    local mount
    mount=$(setup_mount)
    # https_port keeps the reloaded container off the host's real 443.
    write_caddyfile "$mount/Caddyfile" 1 18445
    local name=phdc-host
    if ! start_caddy "$name" --network host -v "$mount:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_admin "$name" || _fail "admin API never came up: $name"

    local rc
    rc=$(drive_install --caddy-docker "$name" | tail -1)
    _assert_eq "RC=0" "$rc" "host-network install rc"
    _assert_out_contains "$SBX/install.err" "uses host networking" "host-network log"
    _assert_file_contains "$SBX/etc/proxyhub/config.yaml" 'host: "127.0.0.1"'
    _assert_file_not_contains "$SBX/etc/proxyhub/config.yaml" "trusted_proxies"
    local frag="$mount/conf.d/proxyhub.caddy"
    _assert_file_contains "$frag" "reverse_proxy 127.0.0.1:8080"
    if docker exec "$name" test -f /etc/caddy/conf.d/proxyhub.caddy; then
        _pass
    else _fail "fragment not visible inside the host-network container"; fi
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _pass
    else _fail "active caddy config does not serve $TEST_DOMAIN after host-network install"; fi
    _assert_file_contains "$SBX/root/.proxyhub-install-info" "CADDY_CONTAINER=$name"
    if grep -qF "trust boundary" "$SBX/install.out"; then
        _fail "host-network summary unexpectedly carries the bridge trust-boundary warning"
    else _pass; fi
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 2b: named volume mount - resolution honors the inspect Source and the
# fragment really lands in the volume. Root-guarded: volume mountpoints are
# root-owned, so the true-write path needs root; otherwise SKIP (the
# Source-based resolution is covered non-root by test_lib.sh mocks).
# --------------------------------------------------------------------------
case_named_volume() {
    printf '==> case: named volume install\n'
    if ((EUID != 0)); then
        _skip "named volume case needs root (volume mountpoints are root-owned)"
        return
    fi
    new_scratch
    local vol=phdc-vol
    docker volume create "$vol" >/dev/null
    CASE_VOLUMES+=("$vol")
    local src
    src=$(docker volume inspect --format '{{.Mountpoint}}' "$vol")
    [[ -n $src ]] || {
        _fail "volume inspect returned empty mountpoint: $vol"
        return
    }
    write_caddyfile "$src/Caddyfile" 1 ""
    mkdir -p "$src/conf.d"
    # Same symlink trick as setup_mount: root_path(real source) must resolve
    # into the real volume data dir.
    mkdir -p "$SBX$(dirname "$src")"
    ln -s "$src" "$SBX$src"
    local name=phdc-vol
    if ! start_caddy "$name" \
        --add-host host.docker.internal:host-gateway \
        -p 127.0.0.1:18084:80 -p 127.0.0.1:18447:443 \
        -v "$vol:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_admin "$name" || _fail "admin API never came up: $name"

    local rc
    rc=$(drive_install --caddy-docker "$name" | tail -1)
    _assert_eq "RC=0" "$rc" "named volume install rc"
    _assert_file_contains "$src/conf.d/proxyhub.caddy" "reverse_proxy host.docker.internal:8080"
    if docker exec "$name" test -f /etc/caddy/conf.d/proxyhub.caddy; then
        _pass
    else _fail "fragment not visible inside the volume-backed container"; fi
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _pass
    else _fail "active caddy config does not serve $TEST_DOMAIN after named volume install"; fi
    _assert_file_contains "$SBX/root/.proxyhub-install-info" "CADDY_CONTAINER=$name"
    _stop_case_containers
    _stop_case_volumes
}

# --------------------------------------------------------------------------
# Case 3: zero running caddy containers fail closed (auto-detect path).
# --------------------------------------------------------------------------
case_zero_containers() {
    printf '==> case: zero containers fail closed\n'
    if [[ -n $FOREIGN_CADDY ]]; then
        _skip "foreign caddy containers running ($FOREIGN_CADDY); zero-container case skipped"
        return
    fi
    new_scratch
    local rc
    rc=$(drive_install | tail -1)
    _assert_eq "RC=1" "$rc" "zero-container rc"
    _assert_out_contains "$SBX/install.err" \
        "no native caddy binary and no running docker caddy container" "zero-container message"
    _assert_out_contains "$SBX/install.err" "--caddy-docker <container-name>" "zero-container guidance"
    if [[ ! -e $SBX/etc/proxyhub/config.yaml && ! -e $SBX/usr/local/bin/proxyhub ]]; then
        _pass
    else _fail "files written despite zero-container refusal"; fi
}

# --------------------------------------------------------------------------
# Case 4: multiple running caddy containers fail closed, listing candidates.
# --------------------------------------------------------------------------
case_multi_containers() {
    printf '==> case: multiple containers fail closed\n'
    if [[ -n $FOREIGN_CADDY ]]; then
        _skip "foreign caddy containers running ($FOREIGN_CADDY); multi-container case skipped"
        return
    fi
    new_scratch
    local ok=1
    start_caddy phdc-multi-a || ok=0
    start_caddy phdc-multi-b || ok=0
    if ((ok == 0)); then
        _fail "multi-container fixtures failed to start"
        _stop_case_containers
        return
    fi
    wait_running phdc-multi-a || _fail "phdc-multi-a not running"
    wait_running phdc-multi-b || _fail "phdc-multi-b not running"

    local rc
    rc=$(drive_install | tail -1)
    _assert_eq "RC=1" "$rc" "multi-container rc"
    _assert_out_contains "$SBX/install.err" "multiple running caddy containers" "ambiguity message"
    _assert_out_contains "$SBX/install.err" "- phdc-multi-a" "candidate a listed"
    _assert_out_contains "$SBX/install.err" "- phdc-multi-b" "candidate b listed"
    _assert_out_contains "$SBX/install.err" "--caddy-docker <name>" "ambiguity guidance"
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 5: single-file /etc/caddy mount fails closed with remediation, before
# any file is written.
# --------------------------------------------------------------------------
case_single_file_mount() {
    printf '==> case: single-file mount (file layout) install\n'
    new_scratch
    # File layout fixture: the Caddyfile bind-mount source, plus the same
    # symlink trick so root_path(real source) resolves into the real file.
    mkdir -p "$SBX/srv"
    write_caddyfile "$SBX/srv/Caddyfile" 0 ""
    mkdir -p "$SBX$SBX/srv"
    ln -s "$SBX/srv/Caddyfile" "$SBX$SBX/srv/Caddyfile"
    local name=phdc-sfile
    if ! start_caddy "$name" \
        --add-host host.docker.internal:host-gateway \
        -p 127.0.0.1:18083:80 -p 127.0.0.1:18446:443 \
        -v "$SBX/srv/Caddyfile:/etc/caddy/Caddyfile"; then
        _fail "container start failed: $name"
        return
    fi
    wait_admin "$name" || _fail "admin API never came up: $name"

    local rc
    rc=$(drive_install --caddy-docker "$name" | tail -1)
    _assert_eq "RC=0" "$rc" "file layout install rc"
    # The managed block is spliced inline, host-side and container-visible;
    # no import line is appended and no conf.d fragment is created.
    _assert_file_contains "$SBX/srv/Caddyfile" "# >>> proxyhub managed"
    _assert_file_contains "$SBX/srv/Caddyfile" "$TEST_DOMAIN {"
    _assert_file_contains "$SBX/srv/Caddyfile" "reverse_proxy host.docker.internal:8080"
    if grep -qF "import /etc/caddy/conf.d" "$SBX/srv/Caddyfile"; then
        _fail "file layout unexpectedly gained a conf.d import line"
    else _pass; fi
    if docker exec "$name" grep -qF "proxyhub managed" /etc/caddy/Caddyfile; then
        _pass
    else _fail "managed block not visible inside the container"; fi
    if docker exec "$name" test ! -e /etc/caddy/conf.d/proxyhub.caddy; then
        _pass
    else _fail "file layout unexpectedly wrote a conf.d fragment"; fi
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _pass
    else _fail "active caddy config does not serve $TEST_DOMAIN after file-layout install"; fi

    # Uninstall in file layout: the block is spliced OUT, the operator's
    # Caddyfile survives, and the reload drops the route. Same lib-copy seam
    # override as case_uninstall: real docker channel for proxyhubctl.
    mkdir -p "$SBX/scripts/install"
    cp "$SCRIPT_DIR/lib.sh" "$SBX/scripts/install/lib.sh"
    cat >>"$SBX/scripts/install/lib.sh" <<'EOF'

# Appended by test_docker_caddy.sh: real docker seam and non-test mode so
# proxyhubctl uninstall exercises the real caddy reload channel.
_docker() { command docker "$@"; }
_is_test_mode() { return 1; }
EOF
    local urc=0
    PROXYHUB_ROOT=$SBX PROXYHUB_ALLOW_NON_ROOT=1 \
        bash "$SCRIPT_DIR/proxyhubctl" uninstall --yes \
        >"$SBX/uninstall.out" 2>"$SBX/uninstall.err" || urc=$?
    _assert_eq 0 "$urc" "file layout uninstall rc"
    if [[ -f $SBX/srv/Caddyfile ]]; then
        _pass
    else _fail "uninstall deleted the operator Caddyfile"; fi
    if grep -qF "proxyhub managed" "$SBX/srv/Caddyfile"; then
        _fail "managed block not removed by uninstall"
    else _pass; fi
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _fail "active caddy config still serves $TEST_DOMAIN after file-layout uninstall"
    else _pass; fi
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 6: bridge container without the host-gateway mapping fails closed
# (fail-fast: nothing lands on disk).
# --------------------------------------------------------------------------
case_missing_host_gateway() {
    printf '==> case: missing host-gateway mapping fail closed\n'
    new_scratch
    local mount
    mount=$(setup_mount)
    write_caddyfile "$mount/Caddyfile" 1 ""
    local name=phdc-nohg
    if ! start_caddy "$name" \
        -p 127.0.0.1:18084:80 -p 127.0.0.1:18448:443 \
        -v "$mount:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_running "$name" || _fail "$name not running"

    local rc
    rc=$(drive_install --caddy-docker "$name" | tail -1)
    _assert_eq "RC=1" "$rc" "missing host-gateway rc"
    _assert_out_contains "$SBX/install.err" \
        "--add-host host.docker.internal:host-gateway" "host-gateway docker-run guidance"
    _assert_out_contains "$SBX/install.err" "extra_hosts" "host-gateway compose guidance"
    if [[ ! -e $SBX/etc/proxyhub/config.yaml && ! -e $SBX/usr/local/bin/proxyhub &&
        ! -e $mount/conf.d/proxyhub.caddy ]]; then
        _pass
    else _fail "files written despite missing host-gateway refusal"; fi
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 7: bridge container without published 80/443 fails closed.
# --------------------------------------------------------------------------
case_unpublished_ports() {
    printf '==> case: unpublished 80/443 fail closed\n'
    new_scratch
    local mount
    mount=$(setup_mount)
    write_caddyfile "$mount/Caddyfile" 1 ""
    local name=phdc-noports
    if ! start_caddy "$name" \
        --add-host host.docker.internal:host-gateway \
        -v "$mount:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_running "$name" || _fail "$name not running"

    local rc
    rc=$(drive_install --caddy-docker "$name" | tail -1)
    _assert_eq "RC=1" "$rc" "unpublished ports rc"
    _assert_out_contains "$SBX/install.err" \
        "does not publish both TCP 80 and 443" "unpublished ports message"
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 8: rollback - a config that breaks `caddy validate` removes the
# fragment and restores the Caddyfile backup; the container's configuration
# returns to its pre-install state.
# --------------------------------------------------------------------------
case_rollback() {
    printf '==> case: validate failure rolls back\n'
    new_scratch
    # No symlink needed here: the subshell runs with PROXYHUB_ROOT empty, so
    # root_path is the identity and the installer writes the real mount dir.
    local mount="$SBX/srv/caddy"
    mkdir -p "$mount/conf.d"
    # Caddyfile WITHOUT the import line: the pre-install config is valid
    # (broken.caddy is not imported yet); _ensure_caddy_import will append
    # the import, making validate see broken.caddy and fail -> rollback.
    write_caddyfile "$mount/Caddyfile" 0 ""
    printf 'broken.example.com {\n' >"$mount/conf.d/broken.caddy"
    cp "$mount/Caddyfile" "$SBX/Caddyfile.orig"
    local name=phdc-rollback
    if ! start_caddy "$name" -v "$mount:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_admin "$name" || _fail "admin API never came up: $name"

    local rc
    rc=$(
        export PROXYHUB_INSTALL_NO_MAIN=1
        # shellcheck source=../../install.sh
        # shellcheck disable=SC1090,SC1091
        source "$REPO_ROOT/install.sh"
        export PROXYHUB_ROOT=""
        _docker() { command docker "$@"; }
        _is_test_mode() { return 1; }
        PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=$name
        DOMAIN=$TEST_DOMAIN SITE_PATH="aB3_rollbackCase_0000" EMAIL=""
        TIMESTAMP=$(date -u +%Y%m%d%H%M%S)
        rc=0
        _configure_caddy >"$SBX/rb.out" 2>"$SBX/rb.err" || rc=$?
        printf 'RC=%d\n' "$rc"
    )
    _assert_eq "RC=1" "$rc" "rollback rc"
    _assert_out_contains "$SBX/rb.err" "rolled back the Caddy changes" "rollback message"
    if [[ ! -e $mount/conf.d/proxyhub.caddy ]]; then
        _pass
    else _fail "fragment survives rollback (host side)"; fi
    if docker exec "$name" test ! -e /etc/caddy/conf.d/proxyhub.caddy; then
        _pass
    else _fail "fragment survives rollback (container side)"; fi
    if cmp -s "$SBX/Caddyfile.orig" "$mount/Caddyfile"; then
        _pass
    else _fail "Caddyfile not restored to its pre-install content"; fi
    if docker exec "$name" caddy validate --config /etc/caddy/Caddyfile >/dev/null 2>&1; then
        _pass
    else _fail "container config invalid after rollback"; fi
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _fail "active caddy config serves $TEST_DOMAIN after rollback"
    else _pass; fi
    _stop_case_containers
}

# --------------------------------------------------------------------------
# Case 9: uninstall through proxyhubctl removes the fragment and reloads for
# real; the Caddyfile import line stays (native-isomorphic semantics).
# --------------------------------------------------------------------------
case_uninstall() {
    printf '==> case: uninstall cleanup\n'
    new_scratch
    local mount
    mount=$(setup_mount)
    write_caddyfile "$mount/Caddyfile" 1 ""
    local name=phdc-uninstall
    if ! start_caddy "$name" \
        --add-host host.docker.internal:host-gateway \
        -p 127.0.0.1:18085:80 -p 127.0.0.1:18449:443 \
        -v "$mount:/etc/caddy"; then
        _fail "container start failed: $name"
        return
    fi
    wait_admin "$name" || _fail "admin API never came up: $name"

    local rc
    rc=$(drive_install --caddy-docker "$name" | tail -1)
    _assert_eq "RC=0" "$rc" "pre-uninstall install rc"
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _pass
    else _fail "site not active before uninstall"; fi

    # proxyhubctl runs as a subprocess and prefers
    # $PROXYHUB_ROOT/scripts/install/lib.sh: plant a copy whose appended
    # overrides turn the docker seam and _is_test_mode real, so uninstall
    # exercises the true in-container reload channel.
    mkdir -p "$SBX/scripts/install"
    cp "$SCRIPT_DIR/lib.sh" "$SBX/scripts/install/lib.sh"
    cat >>"$SBX/scripts/install/lib.sh" <<'EOF'

# Appended by test_docker_caddy.sh: real docker seam and non-test mode so
# proxyhubctl uninstall exercises the real caddy reload channel.
_docker() { command docker "$@"; }
_is_test_mode() { return 1; }
EOF

    local urc=0
    PROXYHUB_ROOT=$SBX PROXYHUB_ALLOW_NON_ROOT=1 \
        bash "$SCRIPT_DIR/proxyhubctl" uninstall --yes \
        >"$SBX/uninstall.out" 2>"$SBX/uninstall.err" || urc=$?
    _assert_eq 0 "$urc" "uninstall rc"
    _assert_out_contains "$SBX/uninstall.err" "removed Caddy fragment" "uninstall fragment log"
    if [[ ! -e $mount/conf.d/proxyhub.caddy ]]; then
        _pass
    else _fail "fragment not removed by uninstall (host side)"; fi
    if docker exec "$name" test ! -e /etc/caddy/conf.d/proxyhub.caddy; then
        _pass
    else _fail "fragment not removed by uninstall (container side)"; fi
    # Corrected acceptance: uninstall deletes only the fragment (native
    # semantics); the Caddyfile import line stays - an empty glob is legal.
    _assert_file_contains "$mount/Caddyfile" "import /etc/caddy/conf.d/*.caddy"
    # The uninstall reload really took effect: the active config dropped the
    # site (caddy_reload failures are swallowed by uninstall, so only the
    # observable state is trustworthy evidence).
    if admin_config "$name" | grep -qF "$TEST_DOMAIN"; then
        _fail "active caddy config still serves $TEST_DOMAIN after uninstall reload"
    else _pass; fi
    _stop_case_containers
}

# --------------------------------------------------------------------------

case_auto_detect_bridge
case_explicit_host_network
case_named_volume
case_zero_containers
case_multi_containers
case_single_file_mount
case_missing_host_gateway
case_unpublished_ports
case_rollback
case_uninstall

printf 'passed: %d, failed: %d, skipped: %d\n' "$PASS" "$FAIL" "$SKIPS"
((FAIL == 0))
