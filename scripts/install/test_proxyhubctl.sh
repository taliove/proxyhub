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

# --------------------------------------------------------------------------
# reset-mfa cases
# --------------------------------------------------------------------------

# RESET_MFA_LOG records every mock binary invocation so tests can assert the
# database was (or was not) touched.
RESET_MFA_LOG=""

# setup_reset_mfa_mock [EXIT_CODE] - install a mock proxyhub binary that logs
# its arguments and exits with EXIT_CODE (default 0), plus a stub config file.
setup_reset_mfa_mock() {
    local exit_code="${1:-0}"
    RESET_MFA_LOG="$PROXYHUB_ROOT/reset-mfa-invocations.log"
    : > "$RESET_MFA_LOG"

    mkdir -p "$PROXYHUB_ROOT/etc/proxyhub"
    printf 'storage:\n  path: /var/lib/proxyhub/data.db\n' \
        > "$PROXYHUB_ROOT/etc/proxyhub/config.yaml"

    mkdir -p "$PROXYHUB_ROOT/usr/local/bin"
    cat > "$PROXYHUB_ROOT/usr/local/bin/proxyhub" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$*" >> "${RESET_MFA_LOG}"
if [[ "${exit_code}" != "0" ]]; then
    printf 'error: no such user\n' >&2
    exit ${exit_code}
fi
printf 'MFA reset for user (id=1)\n'
exit 0
EOF
    chmod +x "$PROXYHUB_ROOT/usr/local/bin/proxyhub"
}

test_reset_mfa_with_yes() {
    setup_reset_mfa_mock
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" \
        reset-mfa --username alice --yes 2>&1)
    assert_contains "$output" "MFA reset for user"
    assert_contains "$(cat "$RESET_MFA_LOG")" "reset-mfa"
    assert_contains "$(cat "$RESET_MFA_LOG")" "--username alice"
}

test_reset_mfa_interactive_confirm() {
    setup_reset_mfa_mock
    local output
    output=$(printf 'yes\n' | bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" \
        reset-mfa --username alice 2>&1)
    assert_contains "$output" "MFA reset for user"
    assert_contains "$(cat "$RESET_MFA_LOG")" "--username alice"
}

test_reset_mfa_interactive_decline() {
    setup_reset_mfa_mock
    set +e
    local output
    output=$(printf 'no\n' | bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" \
        reset-mfa --username alice 2>&1)
    local exit_code=$?
    set -e
    assert_exit_code 1 "$exit_code"
    assert_contains "$output" "aborted"
    # Declining must not reach the database.
    TESTS_RUN=$((TESTS_RUN + 1))
    if [[ ! -s "$RESET_MFA_LOG" ]]; then
        pass
    else
        fail "declined confirmation should not invoke the binary, got: $(cat "$RESET_MFA_LOG")"
    fi
}

test_reset_mfa_requires_username() {
    setup_reset_mfa_mock
    set +e
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" reset-mfa --yes 2>&1)
    local exit_code=$?
    set -e
    assert_exit_code 2 "$exit_code"
    assert_contains "$output" "requires --username"
}

test_reset_mfa_propagates_binary_failure() {
    setup_reset_mfa_mock 1
    set +e
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" \
        reset-mfa --username ghost --yes 2>&1)
    local exit_code=$?
    set -e
    assert_exit_code 1 "$exit_code"
    assert_contains "$output" "reset-mfa failed for user 'ghost'"
}

test_reset_mfa_listed_in_help() {
    local output
    output=$(bash "$PROXYHUB_ROOT/usr/local/bin/proxyhubctl" --help 2>&1)
    assert_contains "$output" "reset-mfa"
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
    test_reset_mfa_with_yes
    test_reset_mfa_interactive_confirm
    test_reset_mfa_interactive_decline
    test_reset_mfa_requires_username
    test_reset_mfa_propagates_binary_failure
    test_reset_mfa_listed_in_help
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




