#!/usr/bin/env bash
# test_rotate_uninstall.sh - Test suite for rotate-path, auto-update, and uninstall.
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${SCRIPT_DIR}/lib.sh"

readonly PROXYHUBCTL="${SCRIPT_DIR}/proxyhubctl"

# Test counters.
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# --------------------------------------------------------------------------
# Test framework helpers
# --------------------------------------------------------------------------

# setup_test - create a test environment.
setup_test() {
    export PROXYHUB_ROOT
    PROXYHUB_ROOT=$(mktemp -d)
    export PROXYHUB_ALLOW_NON_ROOT=1

    # Create directories.
    mkdir -p "$(root_path /var/lib/proxyhub)"
    mkdir -p "$(root_path /etc/proxyhub)"
    mkdir -p "$(root_path /var/backups/proxyhub)"
    mkdir -p "$(root_path /etc/caddy/conf.d)"
    mkdir -p "$(root_path /usr/local/bin)"
    mkdir -p "$(root_path /root)"
    mkdir -p "$(root_path /etc/systemd/system)"

    # Create install info.
    cat > "$(root_path /root/.proxyhub-install-info)" <<EOF
DOMAIN=example.com
SITE_PATH=old_secure_path_12345678
REPO=taliove/proxyhub
VERSION=v1.0.0
INSTALLED_AT=2026-07-19T00:00:00Z
ADMIN_USER=admin
EOF

    # Create stub binary.
    local binary_path
    binary_path=$(root_path /usr/local/bin/proxyhub)
    cat > "$binary_path" <<'BEOF'
#!/usr/bin/env bash
echo "mock proxyhub binary"
exit 0
BEOF
    chmod +x "$binary_path"

    # Create stub Caddy fragment.
    local caddy_frag
    caddy_frag=$(root_path /etc/caddy/conf.d/proxyhub.caddy)
    cat > "$caddy_frag" <<'CEOF'
example.com {
	@proxyhub path /old_secure_path_12345678 /old_secure_path_12345678/*
	handle @proxyhub {
		reverse_proxy 127.0.0.1:8080
	}
	handle {
		respond 404
	}
}
CEOF

    # Create stub systemd units.
    echo "[Unit]" > "$(root_path /etc/systemd/system/proxyhub.service)"
}

# teardown_test - clean up test environment.
teardown_test() {
    if [[ -n "${PROXYHUB_ROOT:-}" ]]; then
        rm -rf "$PROXYHUB_ROOT"
        unset PROXYHUB_ROOT
    fi
}

# assert_true CONDITION MSG - fail the test if condition is false.
assert_true() {
    TESTS_RUN=$((TESTS_RUN + 1))
    if eval "$1"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf '[PASS] %s\n' "$2"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf '[FAIL] %s\n' "$2" >&2
    fi
}

# assert_file_exists PATH MSG - fail if file doesn't exist.
assert_file_exists() {
    assert_true "[[ -f '$1' ]]" "$2"
}

# assert_file_not_exists PATH MSG - fail if file exists.
assert_file_not_exists() {
    assert_true "[[ ! -f '$1' ]]" "$2"
}

# assert_dir_exists PATH MSG - fail if directory doesn't exist.
assert_dir_exists() {
    assert_true "[[ -d '$1' ]]" "$2"
}

# assert_dir_not_exists PATH MSG - fail if directory exists.
assert_dir_not_exists() {
    assert_true "[[ ! -d '$1' ]]" "$2"
}

# assert_contains FILE PATTERN MSG - fail if file doesn't contain pattern.
assert_contains() {
    assert_true "grep -q '$2' '$1'" "$3"
}

# assert_out_contains TEXT PATTERN MSG - fail if TEXT (arbitrary command
# output, may carry quotes) doesn't contain the fixed string PATTERN. Both
# sides go through scratch files: assert_true evals its condition, so
# embedding raw output or patterns in the condition string breaks on quotes.
assert_out_contains() {
    local out_file="$PROXYHUB_ROOT/.assert-out.$$"
    local pat_file="$PROXYHUB_ROOT/.assert-pat.$$"
    printf '%s' "$1" >"$out_file"
    printf '%s\n' "$2" >"$pat_file"
    assert_true "grep -qFf '$pat_file' '$out_file'" "$3"
    rm -f "$out_file" "$pat_file"
}

# --------------------------------------------------------------------------
# Tests: rotate-path
# --------------------------------------------------------------------------

# test_rotate_path_generates_new - verify rotation generates new Site Path.
test_rotate_path_generates_new() {
    echo "==> test_rotate_path_generates_new"
    setup_test

    "$PROXYHUBCTL" rotate-path --yes

    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    local new_path
    new_path=$(sed -n 's/^SITE_PATH=//p' "$install_info")

    assert_true "[[ '$new_path' != 'old_secure_path_12345678' ]]" \
        "Site Path should be different from old path"
    assert_true "[[ ${#new_path} -ge 20 ]]" \
        "New Site Path should be at least 20 characters"

    teardown_test
}

# test_rotate_path_custom - verify rotation accepts custom Site Path.
test_rotate_path_custom() {
    echo "==> test_rotate_path_custom"
    setup_test

    local custom_path="my_custom_path_567890"
    "$PROXYHUBCTL" rotate-path "$custom_path" --yes

    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    assert_contains "$install_info" "SITE_PATH=${custom_path}" \
        "Install info should contain custom Site Path"

    local caddy_frag
    caddy_frag=$(root_path /etc/caddy/conf.d/proxyhub.caddy)
    assert_contains "$caddy_frag" "path /${custom_path}" \
        "Caddy fragment should contain custom Site Path"

    teardown_test
}

# test_rotate_path_no_caddy_refused - verify --no-caddy installs refuse rotation.
test_rotate_path_no_caddy_refused() {
    echo "==> test_rotate_path_no_caddy_refused"
    setup_test

    # Mark the install as --no-caddy (operator's own reverse proxy).
    echo "NO_CADDY=1" >> "$(root_path /root/.proxyhub-install-info)"

    local rc=0
    "$PROXYHUBCTL" rotate-path --yes >/dev/null 2>&1 || rc=$?
    assert_true "[[ $rc -ne 0 ]]" \
        "rotate-path should fail on a --no-caddy install"

    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    assert_contains "$install_info" "SITE_PATH=old_secure_path_12345678" \
        "Site Path should be unchanged after refusal"

    teardown_test
}

# test_rotate_path_rejects_reserved - verify reserved words are rejected.
test_rotate_path_rejects_reserved() {
    echo "==> test_rotate_path_rejects_reserved"
    setup_test

    set +e
    "$PROXYHUBCTL" rotate-path "admin_path_1234567890" --yes 2>&1
    local exit_code=$?
    set -e

    assert_true "[[ $exit_code -ne 0 ]]" \
        "Rotation should reject reserved word 'admin'"

    teardown_test
}

# test_rotate_path_requires_yes - verify --yes is required.
test_rotate_path_requires_yes() {
    echo "==> test_rotate_path_requires_yes"
    setup_test

    set +e
    "$PROXYHUBCTL" rotate-path 2>&1
    local exit_code=$?
    set -e

    assert_true "[[ $exit_code -ne 0 ]]" \
        "Rotation without --yes should fail"

    teardown_test
}

# --------------------------------------------------------------------------
# Tests: auto-update
# --------------------------------------------------------------------------

# test_enable_auto_update - verify timer is created and enabled.
test_enable_auto_update() {
    echo "==> test_enable_auto_update"
    setup_test

    "$PROXYHUBCTL" enable-auto-update

    local timer_path service_path
    timer_path=$(root_path /etc/systemd/system/proxyhub-auto-update.timer)
    service_path=$(root_path /etc/systemd/system/proxyhub-auto-update.service)

    assert_file_exists "$timer_path" \
        "Timer unit should be created"
    assert_file_exists "$service_path" \
        "Service unit should be created"

    assert_contains "$timer_path" "OnCalendar=Sun \*-\*-\* 03:17:00" \
        "Timer should run on Sunday at 03:17"
    assert_contains "$service_path" "ExecStart=/usr/local/bin/proxyhubctl update --yes --stable-only" \
        "Service should run update with --stable-only"

    teardown_test
}

# test_disable_auto_update - verify timer is removed.
test_disable_auto_update() {
    echo "==> test_disable_auto_update"
    setup_test

    "$PROXYHUBCTL" enable-auto-update
    "$PROXYHUBCTL" disable-auto-update

    local timer_path service_path
    timer_path=$(root_path /etc/systemd/system/proxyhub-auto-update.timer)
    service_path=$(root_path /etc/systemd/system/proxyhub-auto-update.service)

    assert_file_not_exists "$timer_path" \
        "Timer unit should be removed"
    assert_file_not_exists "$service_path" \
        "Service unit should be removed"

    teardown_test
}

# --------------------------------------------------------------------------
# Tests: uninstall
# --------------------------------------------------------------------------

# test_uninstall_preserves_data - verify default uninstall preserves state.
test_uninstall_preserves_data() {
    echo "==> test_uninstall_preserves_data"
    setup_test

    "$PROXYHUBCTL" uninstall --yes

    local state_dir backup_dir install_info
    state_dir=$(root_path /var/lib/proxyhub)
    backup_dir=$(root_path /var/backups/proxyhub)
    install_info=$(root_path /root/.proxyhub-install-info)

    assert_dir_exists "$state_dir" \
        "State directory should be preserved"
    assert_dir_exists "$backup_dir" \
        "Backup directory should be preserved"
    assert_file_exists "$install_info" \
        "Install info should be preserved"

    local binary_path unit_path caddy_frag
    binary_path=$(root_path /usr/local/bin/proxyhub)
    unit_path=$(root_path /etc/systemd/system/proxyhub.service)
    caddy_frag=$(root_path /etc/caddy/conf.d/proxyhub.caddy)

    assert_file_not_exists "$binary_path" \
        "Binary should be removed"
    assert_file_not_exists "$unit_path" \
        "Service unit should be removed"
    assert_file_not_exists "$caddy_frag" \
        "Caddy fragment should be removed"

    teardown_test
}

# test_uninstall_purge - verify --purge removes all data.
test_uninstall_purge() {
    echo "==> test_uninstall_purge"
    setup_test

    "$PROXYHUBCTL" uninstall --purge --yes

    local state_dir backup_dir install_info
    state_dir=$(root_path /var/lib/proxyhub)
    backup_dir=$(root_path /var/backups/proxyhub)
    install_info=$(root_path /root/.proxyhub-install-info)

    assert_dir_not_exists "$state_dir" \
        "State directory should be purged"
    assert_dir_not_exists "$backup_dir" \
        "Backup directory should be purged"
    assert_file_not_exists "$install_info" \
        "Install info should be purged"

    teardown_test
}

# test_uninstall_requires_yes - verify --yes is required.
test_uninstall_requires_yes() {
    echo "==> test_uninstall_requires_yes"
    setup_test

    set +e
    "$PROXYHUBCTL" uninstall 2>&1
    local exit_code=$?
    set -e

    assert_true "[[ $exit_code -ne 0 ]]" \
        "Uninstall without --yes should fail"

    teardown_test
}

# --------------------------------------------------------------------------
# Tests: docker caddy mode (ADR 0035, ticket 04/#19)
# --------------------------------------------------------------------------

# mock_docker_lib - copy lib.sh into the scratch search path and append the
# _docker override fed on stdin, so the proxyhubctl subprocess (which prefers
# $PROXYHUB_ROOT/scripts/install/lib.sh, see its search order) runs the mock.
# Adapts test_install.sh's function-override convention to proxyhubctl's
# out-of-process invocation.
mock_docker_lib() {
    mkdir -p "$PROXYHUB_ROOT/scripts/install"
    cp "$SCRIPT_DIR/lib.sh" "$PROXYHUB_ROOT/scripts/install/lib.sh"
    cat >>"$PROXYHUB_ROOT/scripts/install/lib.sh"
}

# setup_docker_install CADDY_MODE_EXTRA... - turn the scratch install into a
# docker-mode one: record the mode fields and move the fragment onto the
# mocked container mount ($PROXYHUB_ROOT/srv/caddy). The native-path fragment
# stays behind on purpose: docker-mode operations must never touch it.
setup_docker_install() {
    cat >>"$(root_path /root/.proxyhub-install-info)" <<'EOF'
CADDY_MODE=docker
CADDY_CONTAINER=caddy
EOF
    mkdir -p "$(root_path /srv/caddy/conf.d)"
    cp "$(root_path /etc/caddy/conf.d/proxyhub.caddy)" \
        "$(root_path /srv/caddy/conf.d/proxyhub.caddy)"
    : >"$PROXYHUB_ROOT/docker.calls"
}

# mock_docker_alive_host - _docker answers for a running host-network caddy
# container with a bind mount at /srv/caddy, logging every call.
mock_docker_alive_host() {
    mock_docker_lib <<'MOCK'
_docker() {
    printf '%s\n' "$*" >>"$PROXYHUB_ROOT/docker.calls"
    if [[ $1 == inspect ]]; then
        case $3 in
            *State.Running*) printf 'true\n' ;;
            *HostConfig.NetworkMode*) printf 'host\n' ;;
            *Mounts*) printf 'bind\t/etc/caddy\t/srv/caddy\t\n' ;;
        esac
    fi
    return 0
}
MOCK
}

# mock_docker_alive_bridge - _docker answers for a running bridge-network
# caddy container (host-gateway mapping, 172.17.0.1/16, 80/443 published).
mock_docker_alive_bridge() {
    mock_docker_lib <<'MOCK'
_docker() {
    printf '%s\n' "$*" >>"$PROXYHUB_ROOT/docker.calls"
    if [[ $1 == inspect ]]; then
        case $3 in
            *State.Running*) printf 'true\n' ;;
            *HostConfig.NetworkMode*) printf 'bridge\n' ;;
            *HostConfig.ExtraHosts*) printf 'host.docker.internal:host-gateway\n' ;;
            *NetworkSettings.Networks*) printf 'bridge\t172.17.0.1\t16\n' ;;
            *Mounts*) printf 'bind\t/etc/caddy\t/srv/caddy\t\n' ;;
        esac
    elif [[ $1 == port ]]; then
        printf '80/tcp -> 0.0.0.0:80\n443/tcp -> 0.0.0.0:443\n'
    fi
    return 0
}
MOCK
}

# mock_docker_lost - _docker answers as if the recorded container were gone.
mock_docker_lost() {
    mock_docker_lib <<'MOCK'
_docker() {
    printf '%s\n' "$*" >>"$PROXYHUB_ROOT/docker.calls"
    return 1
}
MOCK
}

# test_require_running_unit - direct coverage of the lib.sh preflight:
# no-op outside docker mode, passes for a running container, fails closed
# for lost/stopped containers and for a corrupt record (no CADDY_CONTAINER).
test_require_running_unit() {
    echo "==> test_require_running_unit"
    setup_test

    assert_true "bash -c 'source \"\$0\"; _docker() { return 1; }; docker_caddy_require_running' '$SCRIPT_DIR/lib.sh'" \
        "preflight is a no-op in native mode"
    assert_true "bash -c 'source \"\$0\"; PROXYHUB_CADDY_MODE=docker; PROXYHUB_CADDY_CONTAINER=cad; _docker() { printf \"true\n\"; }; docker_caddy_require_running' '$SCRIPT_DIR/lib.sh'" \
        "preflight passes for a running recorded container"

    local err
    err=$(bash -c 'source "$0"; PROXYHUB_CADDY_MODE=docker; PROXYHUB_CADDY_CONTAINER=gone
        _docker() { return 1; }
        docker_caddy_require_running' "$SCRIPT_DIR/lib.sh" 2>&1 >/dev/null) && true
    assert_out_contains "$err" "recorded caddy container 'gone' no longer exists" \
        "preflight fails closed for a lost container, naming it"
    err=$(bash -c 'source "$0"; PROXYHUB_CADDY_MODE=docker; PROXYHUB_CADDY_CONTAINER=stopped
        _docker() { printf "false\n"; }
        docker_caddy_require_running' "$SCRIPT_DIR/lib.sh" 2>&1 >/dev/null) && true
    assert_out_contains "$err" "'stopped' is not running" \
        "preflight fails closed for a stopped container, naming it"
    assert_out_contains "$err" "docker start stopped" \
        "preflight prints the start hint for a stopped container"
    err=$(bash -c 'source "$0"; PROXYHUB_CADDY_MODE=docker; PROXYHUB_CADDY_CONTAINER=
        docker_caddy_require_running' "$SCRIPT_DIR/lib.sh" 2>&1 >/dev/null) && true
    assert_out_contains "$err" "no CADDY_CONTAINER" \
        "preflight fails closed for a corrupt record (docker mode, no container)"

    teardown_test
}

# test_rotate_path_docker_host - docker mode, host-network container: the
# fragment is rewritten on the container mount with the loopback upstream,
# the native-path fragment is untouched, and the liveness preflight is the
# first docker call (fail fast before touching the mount).
test_rotate_path_docker_host() {
    echo "==> test_rotate_path_docker_host"
    setup_test
    setup_docker_install
    mock_docker_alive_host

    "$PROXYHUBCTL" rotate-path --yes

    local frag
    frag=$(root_path /srv/caddy/conf.d/proxyhub.caddy)
    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    local new_path
    new_path=$(sed -n 's/^SITE_PATH=//p' "$install_info")

    assert_true "[[ '$new_path' != 'old_secure_path_12345678' ]]" \
        "Site Path rotated in the install record"
    assert_contains "$frag" "path /${new_path}" \
        "fragment on the container mount carries the new Site Path"
    assert_contains "$frag" "reverse_proxy 127.0.0.1:8080" \
        "host-network container keeps the loopback upstream"
    assert_contains "$(root_path /etc/caddy/conf.d/proxyhub.caddy)" "old_secure_path_12345678" \
        "native-path fragment untouched in docker mode"
    assert_true "head -1 '$PROXYHUB_ROOT/docker.calls' | grep -q 'State.Running'" \
        "liveness preflight is the first docker call"
    assert_true "grep -q 'Mounts' '$PROXYHUB_ROOT/docker.calls'" \
        "fragment path resolved through the container mount"

    teardown_test
}

# test_rotate_path_docker_bridge - docker mode, bridge-network container:
# prepare_topology re-derives the gateway reach-back, so the rewritten
# fragment proxies to host.docker.internal with XFF replacement intact.
test_rotate_path_docker_bridge() {
    echo "==> test_rotate_path_docker_bridge"
    setup_test
    setup_docker_install
    mock_docker_alive_bridge

    "$PROXYHUBCTL" rotate-path --yes

    local frag
    frag=$(root_path /srv/caddy/conf.d/proxyhub.caddy)
    assert_contains "$frag" "reverse_proxy host.docker.internal:8080" \
        "bridge fragment proxies to host.docker.internal"
    assert_contains "$frag" "header_up X-Forwarded-For {remote_host}" \
        "XFF replacement discipline holds in docker mode"
    assert_contains "$frag" "header_up X-Real-IP {remote_host}" \
        "X-Real-IP replacement discipline holds in docker mode"
    assert_true "grep -q 'NetworkMode' '$PROXYHUB_ROOT/docker.calls'" \
        "topology re-derived from the live container"

    teardown_test
}

# setup_docker_file_install - docker-mode install in the FILE layout (ADR
# 0039): record the mode fields and seed a single-file Caddyfile on the
# mocked mount, carrying an old managed block plus operator content.
setup_docker_file_install() {
    cat >>"$(root_path /root/.proxyhub-install-info)" <<'EOF'
CADDY_MODE=docker
CADDY_CONTAINER=caddy
EOF
    mkdir -p "$(root_path /srv)"
    cat >"$(root_path /srv/Caddyfile)" <<'EOFCF'
{
	auto_https off
}

other.example.com {
	respond 200
}
# >>> proxyhub managed - do not edit between markers
proxy.example.com {
	@proxyhub path /old_secure_path_12345678 /old_secure_path_12345678/*
	handle @proxyhub {
		reverse_proxy 127.0.0.1:8080 {
			header_up X-Forwarded-For {remote_host}
			header_up X-Real-IP {remote_host}
		}
	}

	handle {
		respond 404
	}
}
# <<< proxyhub managed
EOFCF
    : >"$PROXYHUB_ROOT/docker.calls"
}

# mock_docker_alive_file - _docker answers for a running host-network caddy
# container with only /etc/caddy/Caddyfile bind-mounted (file layout).
mock_docker_alive_file() {
    mock_docker_lib <<'MOCK'
_docker() {
    printf '%s\n' "$*" >>"$PROXYHUB_ROOT/docker.calls"
    if [[ $1 == inspect ]]; then
        case $3 in
            *State.Running*) printf 'true\n' ;;
            *HostConfig.NetworkMode*) printf 'host\n' ;;
            *Mounts*) printf 'bind\t/etc/caddy/Caddyfile\t/srv/Caddyfile\t\n' ;;
        esac
    fi
    return 0
}
MOCK
}

# test_rotate_path_docker_file - file layout: rotate-path rewrites the
# managed block inline, operator content survives, exactly one block.
test_rotate_path_docker_file() {
    echo "==> test_rotate_path_docker_file"
    setup_test
    setup_docker_file_install
    mock_docker_alive_file

    "$PROXYHUBCTL" rotate-path --yes

    local cf
    cf=$(root_path /srv/Caddyfile)
    local new_path
    new_path=$(sed -n 's/^SITE_PATH=//p' "$(root_path /root/.proxyhub-install-info)")

    assert_contains "$cf" "path /${new_path}" \
        "managed block carries the new Site Path"
    assert_true "! grep -qF 'old_secure_path_12345678' '$cf'" \
        "old Site Path gone from the Caddyfile"
    assert_contains "$cf" "other.example.com {" \
        "operator content untouched by the rewrite"
    assert_true "[[ \$(grep -c '^# >>> proxyhub managed' '$cf') -eq 1 ]]" \
        "exactly one managed block after rotate"
    assert_true "head -1 '$PROXYHUB_ROOT/docker.calls' | grep -q 'State.Running'" \
        "liveness preflight is the first docker call"

    teardown_test
}

# test_uninstall_docker_file - file layout uninstall: the managed block is
# spliced OUT; the operator's Caddyfile itself must survive.
test_uninstall_docker_file() {
    echo "==> test_uninstall_docker_file"
    setup_test
    setup_docker_file_install
    mock_docker_alive_file

    "$PROXYHUBCTL" uninstall --yes

    local cf
    cf=$(root_path /srv/Caddyfile)
    assert_file_exists "$cf" "operator Caddyfile survives uninstall"
    assert_true "! grep -qF 'proxyhub managed' '$cf'" \
        "managed block removed from the Caddyfile"
    assert_contains "$cf" "other.example.com {" \
        "operator content untouched by uninstall"

    teardown_test
}

# test_rotate_path_docker_lost - a lost recorded container fails closed:
# neither the install record nor the fragment is touched.
test_rotate_path_docker_lost() {
    echo "==> test_rotate_path_docker_lost"
    setup_test
    setup_docker_install
    mock_docker_lost

    local rc=0 output
    output=$("$PROXYHUBCTL" rotate-path --yes 2>&1) || rc=$?

    assert_true "[[ $rc -ne 0 ]]" "rotate-path fails closed when the container is lost"
    assert_out_contains "$output" "container 'caddy'" "error names the recorded container"
    assert_contains "$(root_path /root/.proxyhub-install-info)" "SITE_PATH=old_secure_path_12345678" \
        "install record unchanged after refusal"
    assert_contains "$(root_path /srv/caddy/conf.d/proxyhub.caddy)" "old_secure_path_12345678" \
        "fragment unchanged after refusal"
    assert_true "! grep -q 'Mounts' '$PROXYHUB_ROOT/docker.calls'" \
        "mount never inspected after the liveness preflight failed"

    teardown_test
}

# test_uninstall_docker - docker mode uninstall removes the fragment from
# the container mount and nothing else Caddy-side.
test_uninstall_docker() {
    echo "==> test_uninstall_docker"
    setup_test
    setup_docker_install
    mock_docker_alive_host

    "$PROXYHUBCTL" uninstall --yes

    assert_file_not_exists "$(root_path /srv/caddy/conf.d/proxyhub.caddy)" \
        "fragment removed from the container mount"
    assert_file_exists "$(root_path /etc/caddy/conf.d/proxyhub.caddy)" \
        "native-path fragment untouched in docker mode"
    assert_file_not_exists "$(root_path /usr/local/bin/proxyhub)" \
        "binary removed"
    assert_file_exists "$(root_path /root/.proxyhub-install-info)" \
        "install record preserved without --purge"

    teardown_test
}

# test_uninstall_docker_lost - a lost recorded container fails closed even
# for cleanup: nothing is removed, and the error explains the manual path.
test_uninstall_docker_lost() {
    echo "==> test_uninstall_docker_lost"
    setup_test
    setup_docker_install
    mock_docker_lost

    local rc=0 output
    output=$("$PROXYHUBCTL" uninstall --yes 2>&1) || rc=$?

    assert_true "[[ $rc -ne 0 ]]" "uninstall fails closed when the container is lost"
    assert_out_contains "$output" "container 'caddy'" "error names the recorded container"
    assert_out_contains "$output" "verify manually" \
        "error explains the manual record-removal path"
    assert_file_exists "$(root_path /usr/local/bin/proxyhub)" \
        "binary preserved after refusal"
    assert_file_exists "$(root_path /etc/systemd/system/proxyhub.service)" \
        "service unit preserved after refusal"
    assert_file_exists "$(root_path /srv/caddy/conf.d/proxyhub.caddy)" \
        "fragment preserved after refusal"

    teardown_test
}

# --------------------------------------------------------------------------
# Main test runner
# --------------------------------------------------------------------------

main() {
    printf '[test_rotate_uninstall] Running tests...\n'

    # Rotate-path tests.
    test_rotate_path_generates_new
    test_rotate_path_custom
    test_rotate_path_no_caddy_refused
    test_rotate_path_rejects_reserved
    test_rotate_path_requires_yes

    # Auto-update tests.
    test_enable_auto_update
    test_disable_auto_update

    # Uninstall tests.
    test_uninstall_preserves_data
    test_uninstall_purge
    test_uninstall_requires_yes

    # Docker caddy mode tests (ADR 0035, ticket 04/#19).
    test_require_running_unit
    test_rotate_path_docker_host
    test_rotate_path_docker_bridge
    test_rotate_path_docker_file
    test_rotate_path_docker_lost
    test_uninstall_docker
    test_uninstall_docker_file
    test_uninstall_docker_lost

    printf '\n'
    if [[ "$TESTS_FAILED" -eq 0 ]]; then
        printf '[test_rotate_uninstall] All %d tests passed\n' "$TESTS_PASSED"
        exit 0
    else
        printf '[test_rotate_uninstall] %d/%d tests passed, %d failed\n' \
            "$TESTS_PASSED" "$TESTS_RUN" "$TESTS_FAILED" >&2
        exit 1
    fi
}

main "$@"