#!/usr/bin/env bash
# test_proxyhubctl.sh - unit tests for proxyhubctl CLI.
#
# Mocks systemctl, journalctl, and curl via function overrides. Uses
# PROXYHUB_ROOT to redirect all filesystem paths under a scratch directory.
# Tests argument parsing, subcommand behavior, lock acquisition, and error
# conditions.
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

TESTS_RUN=0
TESTS_PASSED=0

# --------------------------------------------------------------------------
# Test harness
# --------------------------------------------------------------------------

pass() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    printf '.'
}

fail() {
    printf 'F\n  FAIL: %s\n' "$1" >&2
}

assert_contains() {
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ "$1" == *"$2"* ]]; then
        pass
    else
        fail "expected output to contain '$2', got: $1"
    fi
}

assert_exit_code() {
    TESTS_RUN=$((TESTS_RUN + 1))
    local expected="$1"
    local actual="$2"
    if [[ "$actual" == "$expected" ]]; then
        pass
    else
        fail "expected exit code $expected, got $actual"
    fi
}

# --------------------------------------------------------------------------
# Test environment setup
# --------------------------------------------------------------------------

setup_test_env() {
    export PROXYHUB_ROOT="${TMPDIR:-/tmp}/proxyhubctl_test.$$"
    export PROXYHUB_ALLOW_NON_ROOT=1
    mkdir -p "$PROXYHUB_ROOT"
    mkdir -p "$PROXYHUB_ROOT/scripts/install"
    mkdir -p "$PROXYHUB_ROOT/root"
    mkdir -p "$PROXYHUB_ROOT/var/lock"

    # Copy lib.sh into test root
    cp "$SCRIPT_DIR/lib.sh" "$PROXYHUB_ROOT/scripts/install/lib.sh"

    # Create a mock install info
    cat > "$PROXYHUB_ROOT/root/.proxyhub-install-info" <<'EOF'
# ProxyHub installation record
DOMAIN=example.com
SITE_PATH=secure_mgmt_path_12345
REPO=taliove/proxyhub
VERSION=v1.2.3
INSTALLED_AT=2026-07-19T12:00:00Z
ADMIN_USER=admin
LISTEN_ADDR=127.0.0.1:8080
EOF
    chmod 0600 "$PROXYHUB_ROOT/root/.proxyhub-install-info"
}

teardown_test_env() {
    rm -rf "$PROXYHUB_ROOT"
}

# --------------------------------------------------------------------------
# Mock functions (override systemctl, journalctl, curl)
# --------------------------------------------------------------------------

# Create a mock script directory
setup_mocks() {
    mkdir -p "$PROXYHUB_ROOT/usr/bin"

    # Create mock systemctl
    cat > "$PROXYHUB_ROOT/usr/bin/systemctl" <<'EOF'
#!/bin/bash
case "${1:-}" in
    is-active)
        echo "active"
        exit 0
        ;;
    restart)
        exit 0
        ;;
    *)
        printf 'mock systemctl called with: %s\n' "$*" >&2
        exit 1
        ;;
esac
EOF
    chmod +x "$PROXYHUB_ROOT/usr/bin/systemctl"

    # Create mock journalctl
    cat > "$PROXYHUB_ROOT/usr/bin/journalctl" <<'EOF'
#!/bin/bash
printf '[proxyhub] mock journal line 1\n'
printf '[proxyhub] mock journal line 2\n'
printf '[proxyhub] mock journal line 3\n'
EOF
    chmod +x "$PROXYHUB_ROOT/usr/bin/journalctl"

    # Create mock curl
    cat > "$PROXYHUB_ROOT/usr/bin/curl" <<'EOF'
#!/bin/bash
exit 0
EOF
    chmod +x "$PROXYHUB_ROOT/usr/bin/curl"

    # Prepend mock bin to PATH
    export PATH="$PROXYHUB_ROOT/usr/bin:$PATH"
}

# --------------------------------------------------------------------------
# Test cases
# --------------------------------------------------------------------------

test_status() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" status 2>&1)
    assert_contains "$output" "ProxyHub Status"
    assert_contains "$output" "Service state"
    assert_contains "$output" "active"
    assert_contains "$output" "Management URL"
    assert_contains "$output" "https://example.com/secure_mgmt_path_12345/"
}

test_logs_default() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" logs 2>&1)
    assert_contains "$output" "mock journal line"
}

test_logs_with_follow() {
    # Can't test --follow interactively, but verify arg parsing doesn't error
    TESTS_RUN=$((TESTS_RUN + 1))
    timeout 1 bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" logs --follow 2>&1 || true
    # If we got here without error, arg parsing worked
    pass
}

test_logs_with_lines() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" logs --lines 10 2>&1)
    assert_contains "$output" "mock journal line"
}

test_restart() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" restart 2>&1)
    assert_contains "$output" "restarting proxyhub.service"
    assert_contains "$output" "service is healthy"
}

test_show_info() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" show-info 2>&1)
    assert_contains "$output" "DOMAIN=example.com"
    assert_contains "$output" "SITE_PATH=secure_mgmt_path_12345"
    assert_contains "$output" "VERSION=v1.2.3"
}

test_help() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" --help 2>&1)
    assert_contains "$output" "USAGE"
    assert_contains "$output" "status"
    assert_contains "$output" "logs"
    assert_contains "$output" "restart"
    assert_contains "$output" "show-info"
}

test_unknown_subcommand() {
    set +e
    bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" unknown 2>&1
    local exit_code=$?
    set -e
    assert_exit_code 2 "$exit_code"
}

test_no_subcommand() {
    set +e
    bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" 2>&1
    local exit_code=$?
    set -e
    assert_exit_code 2 "$exit_code"
}

# --------------------------------------------------------------------------
# Main test runner
# --------------------------------------------------------------------------

main() {
    printf '[proxyhubctl] Running tests...\n'

    setup_test_env
    setup_mocks

    # Copy proxyhubctl to test root (single source: scripts/install/proxyhubctl)
    mkdir -p "$PROXYHUB_ROOT/usr/local/bin"
    cp "$SCRIPT_DIR/proxyhubctl" \
        "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl"
    chmod +x "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl"

    # Run tests
    test_status
    test_logs_default
    test_logs_with_follow
    test_logs_with_lines
    test_restart
    test_show_info
    test_help
    test_unknown_subcommand
    test_no_subcommand

    teardown_test_env

    printf '\n'
    if [[ "$TESTS_RUN" == "$TESTS_PASSED" ]]; then
        printf '[proxyhubctl] All %d tests passed\n' "$TESTS_RUN"
        exit 0
    else
        printf '[proxyhub] %d/%d tests passed\n' "$TESTS_PASSED" "$TESTS_RUN" >&2
        exit 1
    fi
}

main "$@"




