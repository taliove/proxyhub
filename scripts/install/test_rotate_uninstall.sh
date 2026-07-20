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
# Main test runner
# --------------------------------------------------------------------------

main() {
    printf '[test_rotate_uninstall] Running tests...\n'

    # Rotate-path tests.
    test_rotate_path_generates_new
    test_rotate_path_custom
    test_rotate_path_rejects_reserved
    test_rotate_path_requires_yes

    # Auto-update tests.
    test_enable_auto_update
    test_disable_auto_update

    # Uninstall tests.
    test_uninstall_preserves_data
    test_uninstall_purge
    test_uninstall_requires_yes

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