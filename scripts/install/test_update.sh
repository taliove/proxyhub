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

# sign_fixture_sums PRIVKEY_PEM SUMS_FILE - (re)sign a fixture SHA256SUMS in
# minisign text format: line 2 = base64("Ed" || 8-byte keynum || signature).
# Same assembly as test_install.sh's helper.
sign_fixture_sums() {
    openssl pkeyutl -sign -inkey "$1" -rawin -in "$2" -out "$2.sigbin" 2>/dev/null
    {
        printf 'untrusted comment: signature from synthetic test key\n'
        { printf 'Ed'; head -c 8 /dev/zero; cat "$2.sigbin"; } | base64 | tr -d '\n'
        printf '\n'
    } >"$2.minisig"
    rm -f "$2.sigbin"
}

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
if [[ "$1" == "state-fingerprint" ]]; then
    case " $* " in
        *" --config "*) : ;;
        *) echo "state-fingerprint invoked without --config" >&2; exit 1 ;;
    esac
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
    
    # Mock curl: logs every invocation to curl.calls, serves the GitHub
    # latest-release redirect (or failure when github-down marker exists),
    # the jsDelivr data API, the signed fixture SHA256SUMS and its .minisig,
    # and a freshly built mock tarball for any .tar.gz URL.
    cat > "${PROXYHUB_ROOT}/bin/curl" <<'EOFCURL'
#!/usr/bin/env bash
# Mock curl for testing.
printf '%s\n' "$*" >> "${PROXYHUB_ROOT}/curl.calls"
prev_arg=""
out=""
for arg in "$@"; do
    if [[ "$prev_arg" == "-o" ]]; then out="$arg"; fi
    prev_arg="$arg"
done
# GitHub latest-release redirect (resolve_latest_version channel 1); the
# github-down marker simulates GitHub being unreachable.
if [[ "$*" == *"releases/latest"* ]]; then
    if [[ -f "${PROXYHUB_ROOT}/github-down" ]]; then exit 1; fi
    echo "https://github.com/taliove/proxyhub/releases/tag/v1.1.0"
    exit 0
fi
# jsDelivr data API (channel 2); the jsdelivr-down marker simulates failure.
if [[ "$*" == *"data.jsdelivr.com"* ]]; then
    if [[ -f "${PROXYHUB_ROOT}/jsdelivr-down" ]]; then exit 1; fi
    printf '{\n  "versions": [\n    {\n      "version": "1.1.0"\n    }\n  ]\n}\n'
    exit 0
fi
if [[ "$*" == *"SHA256SUMS.minisig"* ]]; then
    [[ -n "$out" ]] && cp "${PROXYHUB_ROOT}/fixtures/SHA256SUMS.minisig" "$out" && exit 0
    exit 1
fi
if [[ "$*" == *"SHA256SUMS"* ]]; then
    [[ -n "$out" ]] && cp "${PROXYHUB_ROOT}/fixtures/SHA256SUMS" "$out" && exit 0
    exit 1
fi
if [[ "$*" == *".tar.gz"* && -n "$out" ]]; then
    # Create a minimal tarball with mock binary.
    tmpdir=$(mktemp -d)
    cat > "${tmpdir}/proxyhub" <<'EOFNEWBIN'
#!/usr/bin/env bash
if [[ "$1" == "state-fingerprint" ]]; then
    case " $* " in
        *" --config "*) : ;;
        *) echo "state-fingerprint invoked without --config" >&2; exit 1 ;;
    esac
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
    tar -czf "$out" -C "$tmpdir" proxyhub
    rm -rf "$tmpdir"
    exit 0
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

    # Signature trust chain fixture (ADR 0036): a throwaway Ed25519 keypair
    # per test. The mock curl serves a SHA256SUMS manifest signed with it and
    # the ctl subprocess verifies against the exported public key (lib.sh
    # reads PROXYHUB_MINISIGN_PUBKEY from the environment; the production
    # constant stays untouched). The manifest lists the full release matrix
    # with a syntactically valid 64-hex fake checksum, matching
    # scripts/release/package.sh output shape.
    local fix_dir="${PROXYHUB_ROOT}/fixtures"
    mkdir -p "$fix_dir"
    cat > "${fix_dir}/SHA256SUMS" <<'EOFSUMS'
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_darwin_amd64.tar.gz
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_darwin_arm64.tar.gz
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_linux_amd64.tar.gz
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_linux_arm64.tar.gz
aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  proxyhub_1.1.0_windows_amd64.tar.gz
EOFSUMS
    openssl genpkey -algorithm ed25519 -out "${fix_dir}/testkey.pem" 2>/dev/null
    openssl pkey -in "${fix_dir}/testkey.pem" -pubout -outform DER 2>/dev/null \
        | tail -c 32 >"${fix_dir}/testkey.raw"
    PROXYHUB_MINISIGN_PUBKEY=$(
        { printf 'Ed'; head -c 8 /dev/zero; cat "${fix_dir}/testkey.raw"; } | base64 | tr -d '\n'
    )
    export PROXYHUB_MINISIGN_PUBKEY
    sign_fixture_sums "${fix_dir}/testkey.pem" "${fix_dir}/SHA256SUMS"
}

teardown_test() {
    if [[ -n "${PROXYHUB_ROOT:-}" ]]; then
        rm -rf "$PROXYHUB_ROOT"
        unset PROXYHUB_ROOT
    fi
    unset PROXYHUB_MINISIGN_PUBKEY
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
# Download base and signature gate (ADR 0036, ticket C4/#27)
# --------------------------------------------------------------------------

# mark_live_binary - append a sentinel comment to the installed mock binary so
# fail-closed tests can prove the in-service binary was never replaced.
mark_live_binary() {
    LIVE_BINARY=$(root_path /usr/local/bin/proxyhub)
    LIVE_SENTINEL="# sentinel-$$"
    printf '%s\n' "$LIVE_SENTINEL" >>"$LIVE_BINARY"
}

# assert_install_untouched MSG_PREFIX - after a refused update: the sentinel
# survives in the live binary and the record still reads v1.0.0.
assert_install_untouched() {
    assert_true "grep -qF '$LIVE_SENTINEL' '$LIVE_BINARY'" \
        "$1: in-service binary not replaced"
    local version
    version=$(grep '^VERSION=' "$(root_path /root/.proxyhub-install-info)" | cut -d= -f2)
    assert_true "[[ '$version' == 'v1.0.0' ]]" "$1: version unchanged"
}

test_update_mirror_record_base() {
    echo "==> test_update_mirror_record_base"
    setup_test

    # Record carries a mirror download base (written by install.sh
    # --download-base at install time); update must follow it.
    printf 'DOWNLOAD_BASE=https://mirror.example.com/dl\n' \
        >>"$(root_path /root/.proxyhub-install-info)"

    "$PROXYHUBCTL" update v1.1.0

    local new_version
    new_version=$(grep '^VERSION=' "$(root_path /root/.proxyhub-install-info)" | cut -d= -f2)
    assert_true "[[ '$new_version' == 'v1.1.0' ]]" "mirror-mode update bumps the version"
    assert_true "grep -qF 'https://mirror.example.com/dl/v1.1.0/SHA256SUMS.minisig' '$PROXYHUB_ROOT/curl.calls'" \
        "signature file fetched from the recorded mirror"
    assert_true "grep -qF 'https://mirror.example.com/dl/v1.1.0/proxyhub_1.1.0_linux_amd64.tar.gz' '$PROXYHUB_ROOT/curl.calls'" \
        "tarball fetched from the recorded mirror"
    assert_true "! grep -q 'github.com' '$PROXYHUB_ROOT/curl.calls'" \
        "mirror mode never contacts github.com"

    teardown_test
}

test_update_explicit_download_base_wins() {
    echo "==> test_update_explicit_download_base_wins"
    setup_test

    printf 'DOWNLOAD_BASE=https://record-mirror.example.com/dl\n' \
        >>"$(root_path /root/.proxyhub-install-info)"

    "$PROXYHUBCTL" update v1.1.0 --download-base https://flag-mirror.example.com/dl

    local new_version
    new_version=$(grep '^VERSION=' "$(root_path /root/.proxyhub-install-info)" | cut -d= -f2)
    assert_true "[[ '$new_version' == 'v1.1.0' ]]" "explicit-flag update bumps the version"
    assert_true "grep -qF 'https://flag-mirror.example.com/dl/v1.1.0/proxyhub_1.1.0_linux_amd64.tar.gz' '$PROXYHUB_ROOT/curl.calls'" \
        "explicit --download-base wins over the record"
    assert_true "! grep -q 'record-mirror' '$PROXYHUB_ROOT/curl.calls'" \
        "recorded mirror not contacted when the flag is given"

    teardown_test
}

test_update_old_record_defaults_github() {
    echo "==> test_update_old_record_defaults_github"
    setup_test

    # The setup_test record predates the DOWNLOAD_BASE field: the effective
    # base must fall back to the official GitHub releases base.
    "$PROXYHUBCTL" update v1.1.0

    assert_true "grep -qF 'https://github.com/taliove/proxyhub/releases/download/v1.1.0/SHA256SUMS.minisig' '$PROXYHUB_ROOT/curl.calls'" \
        "old record: signature file fetched from the official base"
    assert_true "grep -qF 'https://github.com/taliove/proxyhub/releases/download/v1.1.0/proxyhub_1.1.0_linux_amd64.tar.gz' '$PROXYHUB_ROOT/curl.calls'" \
        "old record: tarball fetched from the official base"

    teardown_test
}

test_update_mirror_latest_via_github() {
    echo "==> test_update_mirror_latest_via_github"
    setup_test

    # Mirror record + no explicit version: GitHub redirect answers, latest
    # resolves through it and the update proceeds (ADR 0037 two-channel).
    printf 'DOWNLOAD_BASE=https://mirror.example.com/dl\n' \
        >>"$(root_path /root/.proxyhub-install-info)"

    local rc=0
    "$PROXYHUBCTL" update --yes >"$PROXYHUB_ROOT/out.log" 2>&1 || rc=$?
    assert_true "[[ $rc -eq 0 ]]" "mirror + no version: update proceeds (GitHub channel)"
    local version
    version=$(grep '^VERSION=' "$(root_path /root/.proxyhub-install-info)" | cut -d= -f2)
    assert_true "[[ '$version' == 'v1.1.0' ]]" "mirror + no version: bumped to latest"

    teardown_test
}

test_update_mirror_latest_jsdelivr_fallback() {
    echo "==> test_update_mirror_latest_jsdelivr_fallback"
    setup_test

    # Same but GitHub unreachable: latest resolves via the jsDelivr data API
    # and the update still proceeds from the recorded mirror.
    printf 'DOWNLOAD_BASE=https://mirror.example.com/dl\n' \
        >>"$(root_path /root/.proxyhub-install-info)"
    : >"$PROXYHUB_ROOT/github-down"

    local rc=0
    "$PROXYHUBCTL" update --yes >"$PROXYHUB_ROOT/out.log" 2>&1 || rc=$?
    assert_true "[[ $rc -eq 0 ]]" "github down: update proceeds via jsDelivr"
    assert_true "grep -qF 'via the jsDelivr data API' '$PROXYHUB_ROOT/out.log'" \
        "jsDelivr fallback logged"
    assert_true "grep -qF 'https://mirror.example.com/dl/v1.1.0/proxyhub_1.1.0_linux_amd64.tar.gz' '$PROXYHUB_ROOT/curl.calls'" \
        "tarball still from the recorded mirror"

    teardown_test
}

test_update_latest_both_channels_down() {
    echo "==> test_update_latest_both_channels_down"
    setup_test

    # GitHub AND jsDelivr unreachable, no explicit version: fail closed with
    # the explicit-version guidance; the live install is untouched.
    : >"$PROXYHUB_ROOT/github-down"
    : >"$PROXYHUB_ROOT/jsdelivr-down"
    mark_live_binary

    local rc=0
    "$PROXYHUBCTL" update --yes >"$PROXYHUB_ROOT/out.log" 2>&1 || rc=$?
    assert_true "[[ $rc -eq 1 ]]" "both channels down: update fails closed"
    assert_true "grep -qF -- '--version' '$PROXYHUB_ROOT/out.log'" \
        "explicit-version guidance present"
    assert_install_untouched "both channels down"

    teardown_test
}

test_update_missing_minisig_fails_closed() {
    echo "==> test_update_missing_minisig_fails_closed"
    setup_test

    # A mirror that does not forward the .minisig: the download fails and the
    # update must refuse before the live binary is touched.
    rm -f "$PROXYHUB_ROOT/fixtures/SHA256SUMS.minisig"
    mark_live_binary

    local rc=0 output
    output=$("$PROXYHUBCTL" update v1.1.0 2>&1) || rc=$?

    assert_true "[[ $rc -eq 1 ]]" "missing .minisig exits 1"
    printf '%s' "$output" >"$PROXYHUB_ROOT/out.log"
    assert_true "grep -qF 'SHA256SUMS.minisig' '$PROXYHUB_ROOT/out.log'" \
        "error names the missing signature file"
    assert_install_untouched "missing .minisig"

    teardown_test
}

test_update_bad_signature_fails_closed() {
    echo "==> test_update_bad_signature_fails_closed"
    setup_test

    # The mirror replaced SHA256SUMS AND its .minisig (signed with a
    # different key): verification must fail closed.
    openssl genpkey -algorithm ed25519 -out "$PROXYHUB_ROOT/fixtures/evilkey.pem" 2>/dev/null
    sign_fixture_sums "$PROXYHUB_ROOT/fixtures/evilkey.pem" "$PROXYHUB_ROOT/fixtures/SHA256SUMS"
    mark_live_binary

    local rc=0 output
    output=$("$PROXYHUBCTL" update v1.1.0 2>&1) || rc=$?

    assert_true "[[ $rc -eq 1 ]]" "wrong-key signature exits 1"
    printf '%s' "$output" >"$PROXYHUB_ROOT/out.log"
    assert_true "grep -qF 'signature verification FAILED' '$PROXYHUB_ROOT/out.log'" \
        "signature failure is reported"
    assert_install_untouched "bad signature"

    teardown_test
}

# --------------------------------------------------------------------------
# Docker caddy mode (ADR 0035, ticket 04/#19)
# --------------------------------------------------------------------------

# mock_docker_lib - copy lib.sh into the scratch search path and append the
# _docker override fed on stdin, so the proxyhubctl subprocess (which prefers
# $PROXYHUB_ROOT/scripts/install/lib.sh, see its search order) runs the mock.
mock_docker_lib() {
    mkdir -p "$PROXYHUB_ROOT/scripts/install"
    cp "$SCRIPT_DIR/lib.sh" "$PROXYHUB_ROOT/scripts/install/lib.sh"
    cat >>"$PROXYHUB_ROOT/scripts/install/lib.sh"
}

# setup_docker_install - mark the scratch install as docker mode and put the
# fragment on the mocked container mount ($PROXYHUB_ROOT/srv/caddy).
setup_docker_install() {
    cat >>"$(root_path /root/.proxyhub-install-info)" <<'EOF'
CADDY_MODE=docker
CADDY_CONTAINER=caddy
EOF
    mkdir -p "$(root_path /srv/caddy/conf.d)"
    echo "caddy config" > "$(root_path /srv/caddy/conf.d/proxyhub.caddy)"
    : >"$PROXYHUB_ROOT/docker.calls"
}

# mock_docker_alive - _docker answers for a running host-network caddy
# container with a bind mount at /srv/caddy, logging every call.
mock_docker_alive() {
    mock_docker_lib <<'MOCK'
_docker() {
    printf '%s\n' "$*" >>"$PROXYHUB_ROOT/docker.calls"
    if [[ $1 == inspect ]]; then
        case $3 in
            *State.Running*) printf 'true\n' ;;
            *Mounts*) printf 'bind\t/etc/caddy\t/srv/caddy\t\n' ;;
        esac
    fi
    return 0
}
MOCK
}

test_update_docker_happy() {
    echo "==> test_update_docker_happy"
    setup_test
    setup_docker_install
    mock_docker_alive

    "$PROXYHUBCTL" update v1.1.0

    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    local new_version
    new_version=$(grep '^VERSION=' "$install_info" | cut -d= -f2)
    assert_true "[[ '$new_version' == 'v1.1.0' ]]" "docker-mode update bumps the version"

    # The pre-update backup carried the fragment off the container mount.
    local archive_path
    archive_path=$(find "$(root_path /var/backups/proxyhub)" -name 'proxyhub-backup-*.tar.gz' | head -1)
    assert_true "tar -tzf '$archive_path' | grep -q 'caddy/proxyhub.caddy'" \
        "pre-update backup contains the fragment from the container mount"
    assert_true "head -1 '$PROXYHUB_ROOT/docker.calls' | grep -q 'State.Running'" \
        "liveness preflight is the first docker call"
    assert_true "grep -q 'Mounts' '$PROXYHUB_ROOT/docker.calls'" \
        "fragment path resolved through the container mount"

    teardown_test
}

test_update_docker_lost_container() {
    echo "==> test_update_docker_lost_container"
    setup_test
    setup_docker_install
    mock_docker_lib <<'MOCK'
_docker() {
    printf '%s\n' "$*" >>"$PROXYHUB_ROOT/docker.calls"
    return 1
}
MOCK

    local rc=0 output
    output=$("$PROXYHUBCTL" update v1.1.0 2>&1) || rc=$?

    assert_true "[[ $rc -ne 0 ]]" "update fails closed when the container is lost"
    printf '%s' "$output" > "$PROXYHUB_ROOT/out.log"
    assert_true "grep -qF \"container 'caddy'\" '$PROXYHUB_ROOT/out.log'" \
        "error names the recorded container"

    local install_info
    install_info=$(root_path /root/.proxyhub-install-info)
    local version
    version=$(grep '^VERSION=' "$install_info" | cut -d= -f2)
    assert_true "[[ '$version' == 'v1.0.0' ]]" "version unchanged after refusal"
    local backup_count
    backup_count=$(find "$(root_path /var/backups/proxyhub)" -name 'proxyhub-backup-*.tar.gz' | wc -l)
    assert_true "[[ $backup_count -eq 0 ]]" "no pre-update backup attempted after refusal"

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
    test_update_mirror_record_base
    test_update_explicit_download_base_wins
    test_update_old_record_defaults_github
    test_update_mirror_latest_via_github
    test_update_mirror_latest_jsdelivr_fallback
    test_update_latest_both_channels_down
    test_update_missing_minisig_fails_closed
    test_update_bad_signature_fails_closed
    test_update_docker_happy
    test_update_docker_lost_container

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
