#!/usr/bin/env bash
# test_backup_restore.sh - Test suite for proxyhubctl backup and restore.
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
    echo "install info" > "$(root_path /root/.proxyhub-install-info)"

    # Create mock binary with state-fingerprint command.
    local binary_path
    binary_path=$(root_path /usr/local/bin/proxyhub)
    cat > "$binary_path" <<'EOF'
#!/usr/bin/env bash
if [[ "$1" == "state-fingerprint" ]]; then
    # --config 必传(生产事故:不传则子命令在调用者 CWD 找 config.yaml)。
    if [[ "$2" != "--config" || -z "${3:-}" || "$4" != "--authentication-key-stdin" ]]; then
        echo "state-fingerprint invoked without --config" >&2
        exit 1
    fi
    read -r key
    echo "fingerprint_version: 1"
    echo "algorithm: HMAC-SHA256"
    echo "state_hash: abc123"
    echo "timestamp: 2024-01-01T00:00:00Z"
    exit 0
fi
exit 1
EOF
    chmod +x "$binary_path"
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

# assert_dir_exists PATH MSG - fail if directory doesn't exist.
assert_dir_exists() {
    assert_true "[[ -d '$1' ]]" "$2"
}

# --------------------------------------------------------------------------
# Tests
# --------------------------------------------------------------------------

# test_backup_creates_archive - verify backup creates expected archive.
test_backup_creates_archive() {
    echo "==> test_backup_creates_archive"
    setup_test

    "$PROXYHUBCTL" backup

    local backup_dir
    backup_dir=$(root_path /var/backups/proxyhub)
    local archive_count
    archive_count=$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | wc -l)

    assert_true "[[ $archive_count -eq 1 ]]" "backup creates one archive"

    local archive_path
    archive_path=$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | head -1)
    assert_file_exists "$archive_path" "archive file exists"

    # Extract and verify contents.
    local extract_dir
    extract_dir=$(mktemp -d)
    tar -xzf "$archive_path" -C "$extract_dir"

    assert_file_exists "${extract_dir}/fingerprint.meta" "archive contains fingerprint.meta"
    assert_dir_exists "${extract_dir}/state" "archive contains state directory"
    assert_dir_exists "${extract_dir}/config" "archive contains config directory"
    assert_dir_exists "${extract_dir}/caddy" "archive contains caddy directory"
    assert_file_exists "${extract_dir}/bin/proxyhub" "archive contains binary"

    rm -rf "$extract_dir"
    teardown_test
}

# test_backup_protect_flag - verify --protect flag creates marker.
test_backup_protect_flag() {
    echo "==> test_backup_protect_flag"
    setup_test

    "$PROXYHUBCTL" backup --protect

    local backup_dir
    backup_dir=$(root_path /var/backups/proxyhub)
    local archive_path
    archive_path=$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | head -1)

    assert_file_exists "${archive_path}.protected" "protected marker exists"

    teardown_test
}

# test_backup_records_last_backup - verify last backup path is recorded.
test_backup_records_last_backup() {
    echo "==> test_backup_records_last_backup"
    setup_test

    "$PROXYHUBCTL" backup

    local last_backup_file
    last_backup_file=$(root_path /var/lib/proxyhub/.last-backup)
    assert_file_exists "$last_backup_file" "last backup file exists"

    local recorded_path
    recorded_path=$(cat "$last_backup_file")
    assert_file_exists "$recorded_path" "recorded backup path is valid"

    teardown_test
}

# test_restore_happy_path - verify restore works with valid archive.
test_restore_happy_path() {
    echo "==> test_restore_happy_path"
    setup_test

    # Create a backup.
    "$PROXYHUBCTL" backup

    local backup_dir
    backup_dir=$(root_path /var/backups/proxyhub)
    local archive_path
    archive_path=$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | head -1)

    # Modify current state.
    echo "modified state" > "$(root_path /var/lib/proxyhub/state.db)"

    # Restore.
    "$PROXYHUBCTL" restore "$archive_path" --yes

    # Verify restored content.
    local restored_content
    restored_content=$(cat "$(root_path /var/lib/proxyhub/state.db)")
    assert_true "[[ '$restored_content' == 'state data' ]]" "state restored correctly"

    teardown_test
}

# test_restore_requires_yes - verify restore fails without --yes.
test_restore_requires_yes() {
    echo "==> test_restore_requires_yes"
    setup_test

    "$PROXYHUBCTL" backup

    local backup_dir
    backup_dir=$(root_path /var/backups/proxyhub)
    local archive_path
    archive_path=$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | head -1)

    if "$PROXYHUBCTL" restore "$archive_path" 2>/dev/null; then
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf '[FAIL] restore should require --yes flag\n' >&2
    else
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf '[PASS] restore requires --yes flag\n'
    fi

    teardown_test
}

# test_restore_missing_archive - verify restore fails for missing archive.
test_restore_missing_archive() {
    echo "==> test_restore_missing_archive"
    setup_test

    if "$PROXYHUBCTL" restore /nonexistent/archive.tar.gz --yes 2>/dev/null; then
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_FAILED=$((TESTS_FAILED + 1))
        printf '[FAIL] restore should fail for missing archive\n' >&2
    else
        TESTS_RUN=$((TESTS_RUN + 1))
        TESTS_PASSED=$((TESTS_PASSED + 1))
        printf '[PASS] restore fails for missing archive\n'
    fi

    teardown_test
}

# test_backup_filename_format - verify backup filename matches expected format.
test_backup_filename_format() {
    echo "==> test_backup_filename_format"
    setup_test

    "$PROXYHUBCTL" backup

    local backup_dir
    backup_dir=$(root_path /var/backups/proxyhub)
    local archive_name
    archive_name=$(basename "$(find "$backup_dir" -name 'proxyhub-backup-*.tar.gz' | head -1)")

    # Format: proxyhub-backup-YYYYMMDD-HHMMSS-5chars.tar.gz
    local pattern='^proxyhub-backup-[0-9]{8}-[0-9]{6}-[A-Za-z0-9_-]{5}\.tar\.gz$'
    assert_true "[[ '$archive_name' =~ $pattern ]]" "archive name matches expected format"

    teardown_test
}

# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------

main() {
    echo "Running proxyhubctl backup/restore tests..."
    echo

    test_backup_creates_archive
    test_backup_protect_flag
    test_backup_records_last_backup
    test_restore_happy_path
    test_restore_requires_yes
    test_restore_missing_archive
    test_backup_filename_format

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




