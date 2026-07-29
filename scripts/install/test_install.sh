#!/usr/bin/env bash
# test_install.sh - plain-bash test harness for install.sh (no bats).
#
# Usage: bash scripts/install/test_install.sh
#
# Runs unprivileged on macOS/Linux: PROXYHUB_ROOT redirects every host path
# into a scratch directory, and the network/systemd/caddy seams (_curl,
# _systemctl, _caddy_cli, _host_os, _dns_resolve, ...) are overridden inside
# subshells so overrides never leak between tests.
#
# SC2016: single-quoted strings passed to `bash -c` are intentionally not
# expanded in this shell; they run in a child process.
# SC2015: the `cond && _pass || _fail` assertion idiom is safe because _pass
# always succeeds. SC2030/SC2031/SC2329/SC2034/SC2153: mock functions and
# globals are consumed inside command-substitution subshells and by the
# sourced install.sh, which shellcheck cannot follow.
# shellcheck disable=SC2016,SC2015,SC2030,SC2031,SC2329,SC2034,SC2153
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

export PROXYHUB_INSTALL_NO_MAIN=1
# shellcheck source=../../install.sh
# shellcheck disable=SC1090,SC1091
source "$REPO_ROOT/install.sh"

PASS=0
FAIL=0
TEST_DIRS=()

_pass() { PASS=$((PASS + 1)); }
_fail() { FAIL=$((FAIL + 1)); printf 'FAIL: %s\n' "$*" >&2; }

_assert_eq() { # EXPECTED ACTUAL LABEL
    if [[ $1 == "$2" ]]; then _pass; else _fail "($3) expected [$1], got [$2]"; fi
}

_assert_rc() { # EXPECTED_RC LABEL CMD...  (subshell: CMD may call exit)
    local want=$1 label=$2 rc=0
    shift 2
    ( "$@" ) >/dev/null 2>&1 || rc=$?
    if [[ $rc -eq $want ]]; then _pass; else _fail "($label) expected rc=$want, got rc=$rc"; fi
}

_assert_file_contains() { # FILE NEEDLE
    if grep -qF -- "$2" "$1"; then _pass; else _fail "$1 does not contain [$2]"; fi
}

_assert_file_not_contains() { # FILE NEEDLE
    if grep -qF -- "$2" "$1"; then _fail "$1 unexpectedly contains [$2]"; else _pass; fi
}

_perm_of() {
    if stat -f '%Lp' "$1" >/dev/null 2>&1; then stat -f '%Lp' "$1"; else stat -c '%a' "$1"; fi
}

_cleanup_dirs() {
    local d
    for d in "${TEST_DIRS[@]:-}"; do
        [[ -n $d ]] && rm -rf -- "$d"
    done
}
trap _cleanup_dirs EXIT

# --------------------------------------------------------------------------
# Sandbox + mock builders (used inside subshells)
# --------------------------------------------------------------------------

# setup_sandbox - create PROXYHUB_ROOT with a supported /etc/os-release and a
# fixture release (fake binary tarball + SHA256SUMS with a decoy entry).
setup_sandbox() {
    SBX=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-install-test.XXXXXX")
    export PROXYHUB_ROOT=$SBX
    mkdir -p "$SBX/etc"
    cat >"$SBX/etc/os-release" <<'EOF'
PRETTY_NAME="Ubuntu 24.04.2 LTS"
ID=ubuntu
VERSION_ID="24.04"
EOF
    TEST_TAG="v9.9.9"
    TEST_ASSET="proxyhub_9.9.9_linux_amd64.tar.gz"
    FIX_DIR="$SBX/fixtures"
    mkdir -p "$FIX_DIR/stage"
    cat >"$FIX_DIR/stage/proxyhub" <<'EOF'
#!/usr/bin/env bash
# Fake proxyhub binary: records init invocations for assertions.
if [[ ${1:-} == "init" ]]; then
    {
        printf 'ARGV=%s\n' "$*"
        IFS= read -r secret
        printf 'STDIN=%s\n' "$secret"
    } >>"${PROXYHUB_ROOT}/init.calls"
    exit 0
fi
printf 'fake proxyhub\n'
EOF
    chmod +x "$FIX_DIR/stage/proxyhub"
    (cd "$FIX_DIR/stage" && tar -czf "$FIX_DIR/$TEST_ASSET" proxyhub)
    (
        cd "$FIX_DIR"
        {
            printf '%s  %s\n' "$(shasum -a 256 "$TEST_ASSET" | awk '{print $1}')" "$TEST_ASSET"
            printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' \
                "proxyhub_9.9.9_linux_arm64.tar.gz"
        } >SHA256SUMS
    )
}

# mock_host - override every host/network seam. Defines functions; call inside
# the same subshell that runs main.
mock_host() {
    _host_os() { printf 'Linux'; }
    _host_arch() { printf 'x86_64'; }
    _dns_resolve() { return 0; }
    _curl() {
        local out=/dev/null url="" effective=0
        while (($#)); do
            case $1 in
                -o) out=$2; shift 2 ;;
                -w) [[ $2 == *url_effective* ]] && effective=1; shift 2 ;;
                -*) shift ;;
                *) url=$1; shift ;;
            esac
        done
        if ((effective)); then
            printf 'https://github.com/%s/releases/tag/%s\n' "$REPO" "$TEST_TAG"
            return 0
        fi
        case $url in
            */SHA256SUMS) cp "$FIX_DIR/SHA256SUMS" "$out" ;;
            *.tar.gz) cp "$FIX_DIR/$TEST_ASSET" "$out" ;;
            *) : ;;
        esac
        return 0
    }
}

# --------------------------------------------------------------------------
# Argument parsing / fail-closed behavior
# --------------------------------------------------------------------------

# --help documents the contract and exits 0.
help_out=$(main --help 2>/dev/null)
for needle in --non-interactive --domain --email --version --repo --site-path \
    --listen-addr --no-caddy --skip-dns-check "proxyhubctl update" SHA256SUMS; do
    if [[ $help_out == *"$needle"* ]]; then _pass; else _fail "--help missing [$needle]"; fi
done

# Unknown argument -> usage error 2.
_assert_rc 2 "unknown argument" main --bogus-flag

# Missing value for a flag -> usage error 2.
_assert_rc 2 "missing --domain value" main --non-interactive --domain

# Non-interactive without --domain fails CLOSED (never guesses).
_assert_rc 2 "non-interactive without domain" main --non-interactive

# Bad domain / repo / version / email rejected at parse time (rc 2).
_assert_rc 2 "bad domain" main --non-interactive --domain "ex_ample.com"
_assert_rc 2 "bad domain 2" main --non-interactive --domain "-bad.example.com"
_assert_rc 2 "bad repo" main --non-interactive --domain example.com --repo "foo"
_assert_rc 2 "bad version" main --non-interactive --domain example.com --version "1.2"
_assert_rc 2 "bad email" main --non-interactive --domain example.com --email "not-an-email"

# Bad --listen-addr rejected at parse time (rc 2): non-loopback, out-of-range
# port, garbage. Loopback-only is a constitution red line.
_assert_rc 2 "bad listen addr (non-loopback)" main --non-interactive --domain example.com \
    --listen-addr "0.0.0.0:8080"
_assert_rc 2 "bad listen addr (port range)" main --non-interactive --domain example.com \
    --listen-addr "127.0.0.1:99999"
_assert_rc 2 "bad listen addr (garbage)" main --non-interactive --domain example.com \
    --listen-addr "foo"

# --help documents the new option and the rehearsal seam.
for needle in --listen-addr PROXYHUB_SKIP_PUBLIC_HEALTH; do
    if [[ $help_out == *"$needle"* ]]; then _pass; else _fail "--help missing [$needle]"; fi
done

# Standalone fetch simulation: script piped via stdin (curl | bash form) with
# no adjacent lib.sh -> companion lib fetched via PROXYHUB_LIB_URL. Runs from
# a foreign cwd so the repo-local candidate cannot be found. env -u undoes the
# harness's exported PROXYHUB_INSTALL_NO_MAIN so the child's main() runs;
# PROXYHUB_ROOT (empty scratch) marks test mode so file:// is acceptable.
_pipe_root=$(mktemp -d)
TEST_DIRS+=("$_pipe_root")
pipe_out=$(cd "${TMPDIR:-/tmp}" && env -u PROXYHUB_INSTALL_NO_MAIN \
    PROXYHUB_ROOT="$_pipe_root" \
    PROXYHUB_LIB_URL="file://$REPO_ROOT/scripts/install/lib.sh" \
    bash -s -- --help <"$REPO_ROOT/install.sh" 2>&1) && _pass ||
    _fail "stdin-pipe install.sh --help failed: $pipe_out"
[[ $pipe_out == *"--listen-addr"* ]] && _pass || _fail "pipe mode help content wrong"

# Non-https PROXYHUB_LIB_URL refused outside test mode.
_bad_url_out=$(cd "${TMPDIR:-/tmp}" && env -u PROXYHUB_INSTALL_NO_MAIN -u PROXYHUB_ROOT \
    PROXYHUB_LIB_URL="http://evil.example/lib.sh" \
    bash -s -- --help <"$REPO_ROOT/install.sh" 2>&1) && _fail "http PROXYHUB_LIB_URL accepted" ||
    [[ $_bad_url_out == *"must use https://"* ]] && _pass || _fail "http lib URL refusal message wrong: $_bad_url_out"

# The rehearsal seam requires --skip-dns-check (refuse lone form).
_seam_rc=0
( PROXYHUB_SKIP_PUBLIC_HEALTH=1 main --non-interactive --domain example.com >/dev/null 2>&1 ) || _seam_rc=$?
_assert_eq 2 "$_seam_rc" "skip-public-health without skip-dns-check"

# Reserved / weak Site Paths rejected at parse time (rc 2).
_assert_rc 2 "reserved site path admin" main --non-interactive --domain example.com \
    --site-path "xX9_admin_xX9_xX9_xX9_x"
_assert_rc 2 "reserved site path uppercase" main --non-interactive --domain example.com \
    --site-path "xX9_SUBSCRIPTION_xX9_xX9_"
_assert_rc 2 "short site path" main --non-interactive --domain example.com --site-path "aB3_short"
_assert_rc 2 "few classes site path" main --non-interactive --domain example.com \
    --site-path "abcdefghijklmnopqrstuvwxyz"

# Interactive mode without a TTY fails closed with guidance.
rc=0
(main </dev/null) >/dev/null 2>&1 || rc=$?
_assert_eq 2 "$rc" "interactive without TTY"

# --------------------------------------------------------------------------
# Idempotency
# --------------------------------------------------------------------------

# Marker: install record. (main is wrapped in a nested subshell so its
# exit-via-_die cannot kill the capture before RC is printed.)
idem_out=$(
    T=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-idem.XXXXXX")
    export PROXYHUB_ROOT=$T
    mkdir -p "$T/root"
    : >"$T/root/.proxyhub-install-info"
    rc=0
    ( main --non-interactive --domain proxy.example.com ) 2>&1 || rc=$?
    printf 'RC=%d\n' "$rc"
)
printf '%s\n' "$idem_out" | grep -q 'RC=1' && _pass || _fail "idempotent rerun (record) did not exit 1: $idem_out"
[[ $idem_out == *proxyhubctl* ]] && _pass || _fail "idempotent rerun (record) missing proxyhubctl guidance"

# Marker: systemd unit only.
idem_out2=$(
    T=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-idem2.XXXXXX")
    export PROXYHUB_ROOT=$T
    mkdir -p "$T/etc/systemd/system"
    : >"$T/etc/systemd/system/proxyhub.service"
    rc=0
    ( main --non-interactive --domain proxy.example.com ) 2>&1 || rc=$?
    printf 'RC=%d\n' "$rc"
)
printf '%s\n' "$idem_out2" | grep -q 'RC=1' && _pass || _fail "idempotent rerun (unit) did not exit 1: $idem_out2"
[[ $idem_out2 == *proxyhubctl* ]] && _pass || _fail "idempotent rerun (unit) missing proxyhubctl guidance"

# --------------------------------------------------------------------------
# OS detection fixtures
# --------------------------------------------------------------------------

_assert_rc 0 "os-release debian 12 accepted" env PROXYHUB_ROOT= bash -c '
    export PROXYHUB_INSTALL_NO_MAIN=1
    source "$1/install.sh"
    _host_os() { printf Linux; }
    _host_arch() { printf x86_64; }
    T=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-os.XXXXXX")
    PROXYHUB_ROOT=$T
    mkdir -p "$T/etc"
    printf "ID=debian\nVERSION_ID=\"12\"\n" >"$T/etc/os-release"
    _check_os
' _ "$REPO_ROOT"

_assert_rc 1 "os-release fedora rejected" env PROXYHUB_ROOT= bash -c '
    export PROXYHUB_INSTALL_NO_MAIN=1
    source "$1/install.sh"
    _host_os() { printf Linux; }
    _host_arch() { printf x86_64; }
    T=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-os.XXXXXX")
    PROXYHUB_ROOT=$T
    mkdir -p "$T/etc"
    printf "ID=fedora\nVERSION_ID=\"40\"\n" >"$T/etc/os-release"
    _check_os
' _ "$REPO_ROOT"

# --------------------------------------------------------------------------
# Latest-tag resolution (mocked redirect)
# --------------------------------------------------------------------------

latest=$(
    TEST_TAG="v9.9.9"
    REPO="taliove/proxyhub"
    _curl() {
        local a effective=0
        for a in "$@"; do [[ $a == *url_effective* ]] && effective=1; done
        ((effective)) && printf 'https://github.com/%s/releases/tag/%s\n' "$REPO" "$TEST_TAG"
        return 0
    }
    _resolve_latest_tag "$REPO"
)
_assert_eq "v9.9.9" "$latest" "latest tag resolution"

# --------------------------------------------------------------------------
# Interactive Site Path prompts (piped stdin; no TTY gate inside helpers)
# --------------------------------------------------------------------------

NON_INTERACTIVE=0
ARG_SITE_PATH=""
sp=$(printf '\n' | { _obtain_site_path >/dev/null 2>&1 && printf '%s' "$SITE_PATH"; })
if validate_site_path "$sp" >/dev/null 2>&1; then _pass; else _fail "generated site path invalid: [$sp]"; fi
_assert_eq 20 "${#sp}" "generated site path length"

custom_sp="aB3_customPath_12345"
sp2=$(printf '%s\n' "$custom_sp" | { _obtain_site_path >/dev/null 2>&1 && printf '%s' "$SITE_PATH"; })
_assert_eq "$custom_sp" "$sp2" "interactive site path replaced"

sp3=$(printf 'short\n%s\n' "$custom_sp" | { _obtain_site_path >/dev/null 2>&1 && printf '%s' "$SITE_PATH"; })
_assert_eq "$custom_sp" "$sp3" "interactive site path retries after invalid input"

d=$(printf 'bad_domain\nproxy.example.com\n' | { DOMAIN=""; _gather_interactive >/dev/null 2>&1 && printf '%s' "$DOMAIN"; })
_assert_eq "proxy.example.com" "$d" "interactive domain prompt retries"

# --------------------------------------------------------------------------
# Happy path (fully mocked network/systemd/caddy)
# --------------------------------------------------------------------------

happy=$(
    setup_sandbox
    mock_host
    rc=0
    ( main --non-interactive --domain proxy.example.com --email ops@example.com \
        >"$SBX/stdout.log" 2>"$SBX/stderr.log" ) || rc=$?
    printf 'RC=%d\n' "$rc"
    printf 'SBX=%s\n' "$SBX"
)
happy_rc=$(printf '%s\n' "$happy" | sed -n 's/^RC=//p')
SBX=$(printf '%s\n' "$happy" | sed -n 's/^SBX=//p')
TEST_DIRS+=("$SBX")

_assert_eq 0 "$happy_rc" "happy path exit code"
[[ -x $SBX/usr/local/bin/proxyhub ]] && _pass || _fail "binary not installed"
[[ -x $SBX/usr/local/bin/proxyhubctl ]] && _pass || _fail "proxyhubctl not installed"
[[ -f $SBX/usr/local/bin/proxyhubctl-lib.sh ]] && _pass || _fail "proxyhubctl-lib.sh not installed"
_assert_file_contains "$SBX/etc/proxyhub/config.yaml" 'host: "127.0.0.1"'
_assert_file_contains "$SBX/etc/proxyhub/config.yaml" "/var/lib/proxyhub/data.db"
[[ -f $SBX/etc/systemd/system/proxyhub.service ]] && _pass || _fail "systemd unit missing"
_assert_file_contains "$SBX/etc/caddy/conf.d/proxyhub.caddy" "proxy.example.com {"
_assert_file_contains "$SBX/etc/caddy/Caddyfile" "import /etc/caddy/conf.d/*.caddy"
_assert_file_contains "$SBX/etc/caddy/Caddyfile" "email ops@example.com"

# Install record: exists, 0600, no password.
REC="$SBX/root/.proxyhub-install-info"
[[ -f $REC ]] && _pass || _fail "install record missing"
_assert_eq 600 "$(_perm_of "$REC")" "install record permissions"
_assert_file_contains "$REC" "DOMAIN=proxy.example.com"
_assert_file_contains "$REC" "REPO=taliove/proxyhub"
_assert_file_contains "$REC" "VERSION=v9.9.9"

site_path=$(sed -n 's/^SITE_PATH=//p' "$REC")
if validate_site_path "$site_path" >/dev/null 2>&1; then _pass; else _fail "recorded site path invalid: [$site_path]"; fi
_assert_file_contains "$SBX/etc/caddy/conf.d/proxyhub.caddy" "path /$site_path /$site_path/*"

# Credentials: shown once on stdout, passed to init via stdin, never in argv,
# never in the install record.
pw=$(sed -n 's/^  Admin password : //p' "$SBX/stdout.log")
if [[ ${#pw} -ge 24 ]]; then _pass; else _fail "admin password too short in summary: ${#pw} chars"; fi
_assert_file_contains "$SBX/stdout.log" "https://proxy.example.com/${site_path}/"
_assert_file_not_contains "$REC" "$pw"
_assert_file_not_contains "$SBX/stderr.log" "$pw"

[[ -f $SBX/init.calls ]] && _pass || _fail "proxyhub init was not invoked"
_assert_file_contains "$SBX/init.calls" "--password-stdin"
_assert_file_contains "$SBX/init.calls" "--site-path $site_path"
_assert_file_contains "$SBX/init.calls" "--domain proxy.example.com"
init_stdin=$(sed -n 's/^STDIN=//p' "$SBX/init.calls")
_assert_eq "$pw" "$init_stdin" "password delivered via stdin"
init_argv=$(sed -n 's/^ARGV=//p' "$SBX/init.calls")
[[ $init_argv != *"$pw"* ]] && _pass || _fail "password leaked into init argv"

# --------------------------------------------------------------------------
# Checksum mismatch refuses to install (before any binary lands)
# --------------------------------------------------------------------------

bad=$(
    setup_sandbox
    mock_host
    printf 'tampered-bytes' >>"$FIX_DIR/$TEST_ASSET"
    rc=0
    ( main --non-interactive --domain proxy.example.com >/dev/null 2>&1 ) || rc=$?
    printf 'RC=%d\n' "$rc"
    printf 'SBX=%s\n' "$SBX"
)
bad_rc=$(printf '%s\n' "$bad" | sed -n 's/^RC=//p')
BAD_SBX=$(printf '%s\n' "$bad" | sed -n 's/^SBX=//p')
TEST_DIRS+=("$BAD_SBX")

_assert_eq 1 "$bad_rc" "checksum mismatch exit code"
[[ ! -e $BAD_SBX/usr/local/bin/proxyhub ]] && _pass || _fail "binary installed despite checksum mismatch"
[[ ! -e $BAD_SBX/root/.proxyhub-install-info ]] && _pass || _fail "install record written despite checksum mismatch"

# --------------------------------------------------------------------------
# Custom --listen-addr flows into config, Caddy fragment and install record
# --------------------------------------------------------------------------

custom=$(
    setup_sandbox
    mock_host
    rc=0
    ( main --non-interactive --domain proxy.example.com --listen-addr 127.0.0.1:18080 \
        >"$SBX/stdout.log" 2>"$SBX/stderr.log" ) || rc=$?
    printf 'RC=%d\n' "$rc"
    printf 'SBX=%s\n' "$SBX"
)
custom_rc=$(printf '%s\n' "$custom" | sed -n 's/^RC=//p')
CUSTOM_SBX=$(printf '%s\n' "$custom" | sed -n 's/^SBX=//p')
TEST_DIRS+=("$CUSTOM_SBX")

_assert_eq 0 "$custom_rc" "custom listen addr install exit code"
_assert_file_contains "$CUSTOM_SBX/etc/proxyhub/config.yaml" 'port: 18080'
_assert_file_contains "$CUSTOM_SBX/etc/caddy/conf.d/proxyhub.caddy" "reverse_proxy 127.0.0.1:18080"
_assert_file_contains "$CUSTOM_SBX/root/.proxyhub-install-info" "LISTEN_ADDR=127.0.0.1:18080"

# --------------------------------------------------------------------------
# Rehearsal seam: PROXYHUB_SKIP_PUBLIC_HEALTH skips the public HTTPS check
# --------------------------------------------------------------------------

rehearsal=$(
    setup_sandbox
    mock_host
    rc=0
    ( PROXYHUB_SKIP_PUBLIC_HEALTH=1 main --non-interactive --domain ph-rehearse.example.com \
        --skip-dns-check >"$SBX/stdout.log" 2>"$SBX/stderr.log" ) || rc=$?
    printf 'RC=%d\n' "$rc"
    printf 'SBX=%s\n' "$SBX"
)
rehearsal_rc=$(printf '%s\n' "$rehearsal" | sed -n 's/^RC=//p')
REH_SBX=$(printf '%s\n' "$rehearsal" | sed -n 's/^SBX=//p')
TEST_DIRS+=("$REH_SBX")

_assert_eq 0 "$rehearsal_rc" "rehearsal install exit code"
_assert_file_contains "$REH_SBX/stderr.log" "public HTTPS health check skipped"

# --------------------------------------------------------------------------
# --no-caddy: no Caddy touched, reverse-proxy examples written, record marked
# --------------------------------------------------------------------------

nocad=$(
    setup_sandbox
    mock_host
    rc=0
    ( main --non-interactive --domain proxy.example.com --no-caddy \
        >"$SBX/stdout.log" 2>"$SBX/stderr.log" ) || rc=$?
    printf 'RC=%d\n' "$rc"
    printf 'SBX=%s\n' "$SBX"
)
nocad_rc=$(printf '%s\n' "$nocad" | sed -n 's/^RC=//p')
NC_SBX=$(printf '%s\n' "$nocad" | sed -n 's/^SBX=//p')
TEST_DIRS+=("$NC_SBX")

_assert_eq 0 "$nocad_rc" "--no-caddy install exit code"
[[ ! -e $NC_SBX/etc/caddy/conf.d/proxyhub.caddy ]] && _pass || _fail "caddy fragment written despite --no-caddy"
_assert_file_contains "$NC_SBX/etc/proxyhub/reverse-proxy.caddy" "reverse_proxy 127.0.0.1:8080"
_assert_file_contains "$NC_SBX/etc/proxyhub/reverse-proxy.caddy" "proxy.example.com {"
_assert_file_contains "$NC_SBX/etc/proxyhub/reverse-proxy.nginx.conf" "proxy_pass http://127.0.0.1:8080"
_assert_file_contains "$NC_SBX/etc/proxyhub/reverse-proxy.nginx.conf" "X-Forwarded-For"
_assert_file_contains "$NC_SBX/root/.proxyhub-install-info" "NO_CADDY=1"
_assert_file_contains "$NC_SBX/stderr.log" "public HTTPS health check skipped (--no-caddy)"
_assert_file_contains "$NC_SBX/stdout.log" "reverse-proxy.caddy"

# --------------------------------------------------------------------------
# Caddy-missing guidance (skipped when a real caddy is on the base PATH)
# --------------------------------------------------------------------------

if ! env -i PATH=/usr/bin:/bin bash -c 'command -v caddy' >/dev/null 2>&1; then
    _assert_rc 1 "caddy missing stops with instructions" env PROXYHUB_ROOT= bash -c '
        export PROXYHUB_INSTALL_NO_MAIN=1 PATH=/usr/bin:/bin
        source "$1/install.sh"
        _check_caddy
    ' _ "$REPO_ROOT"
else
    printf 'SKIP: caddy present on base PATH; skipping caddy-missing test\n'
fi

# --------------------------------------------------------------------------

printf 'passed: %d, failed: %d\n' "$PASS" "$FAIL"
((FAIL == 0))
