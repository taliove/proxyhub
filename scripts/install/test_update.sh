#!/usr/bin/env bash
# test_update.sh - Test suite for proxyhubctl update.
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

    # Create stub files.
    echo "state data" > "$(root_path /var/lib/proxyhub/state.db)"
    echo "config data" > "$(root_path /etc/proxyhub/config.yaml)"
    echo "caddy config" > "$(root_path /etc/caddy/conf.d/proxyhub.caddy)"

    # Create install info.
    cat > "$(root_path /root/.proxyhub-install-info)" <<'EOF'
DOMAIN=example.com
SITE_PATH=secure_mgmt_path_12345
REPO=taliove/proxyhub
VERSION=v1.0.0
INSTALLED_AT=2026-07-19T12:00:00Z
EOF

    # Create mock binary v1.0.0 with state-fingerprint.
    local binary_path
    binary_path=$(root_path /usr/local/bin/proxyhub)
    cat > "$binary_path" <<'EOFBIN'
#!/usr/bin/env bash
if [[ "$1" == "state-fingerprint" && "$2" == "--authentication-key-stdin" ]]; then
    read -r key
    echo "fingerprint_version: 1"
    echo "algorithm: HMAC-SHA256"
    echo "state_hash: abc123"
    echo "timestamp: 2024-01-01T00:00:00Z"
    exit 0
fi
exit 1
EOFBIN
    chmod +x "$binary_path"

    # Mock curl, sha256sum for downloads.
    export PATH="${PROXYHUB_ROOT}/bin:${PATH}"
    mkdir -p "${PROXYHUB_ROOT}/bin"
    
    # Mock curl.
    cat > "${PROXYHUB_ROOT}/bin/curl" <<'EOFCURL'
#!/usr/bin/env bash
# Mock curl for testing.
prev_arg=""
if [[ "$*" == *"api.github.com/repos"*"/releases/latest"* ]]; then
    echo '{"tag_name":"v1.1.0","prerelease":false}'
    exit 0
fi
if [[ "$*" == *"SHA256SUMS"* ]]; then
    # Find output file. Emit the full release-matrix manifest (5 targets),
    # matching scripts/release/package.sh output shape, with a syntactically
    # valid 64-hex fake checksum.
    for arg in "$@"; do
        if [[ "$prev_arg" == "-o" ]]; then
            {
                echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_darwin_amd64.tar.gz"
                echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_darwin_arm64.tar.gz"
                echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_linux_amd64.tar.gz"
                echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_linux_arm64.tar.gz"
                echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_windows_amd64.tar.gz"
            } > "$arg"
            exit 0
        fi
        prev_arg="$arg"
    done
    exit 1
fi
if [[ "$*" == *".tar.gz"* ]]; then
    # Find output file.
    for arg in "$@"; do
        if [[ "$prev_arg" == "-o" ]]; then
            # Create a minimal tarball with mock binary.
            tmpdir=$(mktemp -d)
            cat > "${tmpdir}/proxyhub" <<'EOFNEWBIN'
#!/usr/bin/env bash
if [[ "$1" == "state-fingerprint" && "$2" == "--authentication-key-stdin" ]]; then
    read -r key
    echo "fingerprint_version: 1"
    echo "algorithm: HMAC-SHA256"
    echo "state_hash: abc123"
    echo "timestamp: 2024-01-01T00:00:00Z"
    exit 0
fi
exit 1
EOFNEWBIN
            chmod +x "${tmpdir}/proxyhub"
            tar -czf "$arg" -C "$tmpdir" proxyhub
            rm -rf "$tmpdir"
            exit 0
        fi
        prev_arg="$arg"
    done
    exit 1
fi
exit 1
EOFCURL
    chmod +x "${PROXYHUB_ROOT}/bin/curl"

    # Mock sha256sum.
    cat > "${PROXYHUB_ROOT}/bin/sha256sum" <<'EOFSHA'
#!/usr/bin/env bash
if [[ "$1" == "-c" ]]; then
    # Always succeed for checksum verification.
    exit 0
fi
/usr/bin/sha256sum "$@"
EOFSHA
    chmod +x "${PROXYHUB_ROOT}/bin/sha256sum"
}

teardown_test() {
    if [[ -n "${PROXYHUB_ROOT:-}" ]]; then
        rm -rf "$PROXYHUB_ROOT"
        unset PROXYHUB_ROOT
    fi
}

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

assert_file_exists() {
    assert_true "[[ -f '$1' ]]" "$2"
}

# --------------------------------------------------------------------------
# Tests
# --------------------------------------------------------------------------

test_update_already_at_version() {
    echo "==> test_update_already_at_version"
    setup_test

    # Update to same version should be no-op.
    if "$PROXYHUBCTL" update v1.0.0 2>&1 | grep -q "already at version"; then
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf '[PASS] update to same version is no-op\n'
    else
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf '[FAIL] update to same version should be no-op\n' >&2
    fi

    teardown_test
}

test_update_happy_path() {
    echo "==> test_update_happy_path"
    setup_test

    # Update to v1.1.0 (mocked).
    "$PROXYHUBCTL" update v1.1.0

    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    local new_version
    new_version=$(grep '^VERSION=' "$install_info" | cut -d= -f2)

    assert_true "[[ '$new_version' == 'v1.1.0' ]]" "version updated in install info"

    # Check backup was created.
    local backup_dir
    backup_dir=$(root_path /var/backups/proxyhub)
    local backup_count
    backup_count=$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | wc -l)
    assert_true "[[ $backup_count -ge 1 ]]" "backup created before update"

    teardown_test
}

test_update_prerelease_gating() {
    echo "==> test_update_prerelease_gating"
    setup_test

    # Attempt to update to prerelease without --prerelease flag.
    local output
    output=$("$PROXYHUBCTL" update v1.1.0-rc.1 2>&1) || true

    if echo "$output" | grep -q "prerelease"; then
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf '[PASS] prerelease gating works\n'
    else
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf '[FAIL] prerelease should be rejected with proper message\n' >&2
        echo "Output was: $output" >&2
    fi

    teardown_test
}

test_update_checksum_fail_rollback() {
    echo "==> test_update_checksum_fail_rollback"
    setup_test

    # Mock sha256sum to fail.
    cat > "${PROXYHUB_ROOT}/bin/sha256sum" <<'EOFSHA'
#!/usr/bin/env bash
if [[ "$1" == "-c" ]]; then
    exit 1
fi
/usr/bin/sha256sum "$@"
EOFSHA
    chmod +x "${PROXYHUB_ROOT}/bin/sha256sum"

    local output
    output=$("$PROXYHUBCTL" update v1.1.0 2>&1) || true

    if echo "$output" | grep -q "checksum verification failed"; then
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf '[PASS] checksum failure triggers rollback\n'
    else
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf '[FAIL] checksum failure should trigger proper rollback\n' >&2
        echo "Output was: $output" >&2
    fi

    # Verify version unchanged.
    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    local version
    version=$(grep '^VERSION=' "$install_info" | cut -d= -f2)
    assert_true "[[ '$version' == 'v1.0.0' ]]" "version unchanged after rollback"

    teardown_test
}

# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------

main() {
    echo "Running proxyhubctl update tests..."
    echo

    test_update_already_at_version
    test_update_happy_path
    test_update_prerelease_gating
    test_update_checksum_fail_rollback

    echo
    echo "========================================="
    echo "Tests run:    $TESTS_RUN"
    echo "Tests passed: $TESTS_PASSED"
    echo "Tests failed: $TESTS_FAILED"
    echo "========================================="

    if (( TESTS_FAILED > 0 )); then
        exit 1
    fi
}

main "$@"
