#!/usr/bin/env bash
# test_lib.sh - plain-bash test harness for scripts/install/lib.sh (no bats).
#
# Usage: bash scripts/install/test_lib.sh
#
# SC2016: single-quoted strings passed to `bash -c` are intentionally not
# expanded in this shell; they run in a child process.
# shellcheck disable=SC2016
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib.sh
# shellcheck disable=SC1091
source "$SCRIPT_DIR/lib.sh"

PASS=0
FAIL=0

_assert_ok() {
    if "$@" >/dev/null 2>&1; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        printf 'FAIL (expected success): %s\n' "$*" >&2
    fi
}

_assert_fail() {
    if "$@" >/dev/null 2>&1; then
        FAIL=$((FAIL + 1))
        printf 'FAIL (expected failure): %s\n' "$*" >&2
    else
        PASS=$((PASS + 1))
    fi
}

_assert_eq() {
    local expected="$1"
    local actual="$2"
    local label="$3"
    if [[ "$expected" == "$actual" ]]; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        printf 'FAIL (%s): expected [%s], got [%s]\n' "$label" "$expected" "$actual" >&2
    fi
}

_assert_file_contains() {
    local file="$1"
    local needle="$2"
    if grep -qF -- "$needle" "$file"; then
        PASS=$((PASS + 1))
    else
        FAIL=$((FAIL + 1))
        printf 'FAIL: %s does not contain [%s]\n' "$file" "$needle" >&2
    fi
}

# --------------------------------------------------------------------------
# validate_site_path
# --------------------------------------------------------------------------

# Build exact-length strings: PREFIX (4 chars, 4 classes) + N lowercase filler.
_fill() { local n="$1"; printf 'aB3_'; printf 'x%.0s' $(seq "$n"); }
SP_19=$(_fill 15)
SP_20=$(_fill 16)
SP_64=$(_fill 60)
SP_65=$(_fill 61)

# Valid: 4 classes, lengths at and within boundaries.
_assert_ok validate_site_path "abCD1234_efgh-ijklMNOP"       # 22 chars, 4 classes
_assert_ok validate_site_path "$SP_20"                        # 20 chars (min)
_assert_ok validate_site_path "$SP_64"                        # 64 chars (max)
# Valid: exactly 3 classes (no separator).
_assert_ok validate_site_path "abCD1234efghijklMNOPqr"

# Invalid: length.
_assert_fail validate_site_path "$SP_19"                      # 19 chars
_assert_fail validate_site_path "$SP_65"                      # 65 chars
_assert_fail validate_site_path ""

# Invalid: charset.
_assert_fail validate_site_path "aB3_efghijklmnopqrs!uv"
_assert_fail validate_site_path "aB3 efghijklmnopqrst"
_assert_fail validate_site_path "aB3/efghijklmnopqrst"

# Invalid: fewer than 3 classes.
_assert_fail validate_site_path "abcdefghijklmnopqrstuvwxyz"   # 1 class
_assert_fail validate_site_path "abcdefghijklmnopqrstuvwx1"    # 2 classes
_assert_fail validate_site_path "abcdefghijklmnopqrstuvwx-"    # 2 classes
_assert_fail validate_site_path "ABCDEFGHIJKLMNOPQRSTUVWX1"    # 2 classes

# Reserved words: rejected case-insensitively, anywhere in the path.
_assert_fail validate_site_path "xX9_admin_xX9_xX9_xX9_x"
_assert_fail validate_site_path "xX9_ADMIN_xX9_xX9_xX9_x"
_assert_fail validate_site_path "xX9_AdMin_xX9_xX9_xX9_x"
for word in "${PROXYHUB_RESERVED_WORDS[@]}"; do
    _assert_fail validate_site_path "xX9_${word}_xX9_xX9_xX9_x"
    # ASCII-only upper-casing: reserved words are ASCII by construction.
    # shellcheck disable=SC2018,SC2019
    _assert_fail validate_site_path "xX9_$(printf '%s' "$word" | tr 'a-z' 'A-Z')_xX9_xX9_xX9_x"
done

# Reserved list size: 14 words.
_assert_eq "14" "${#PROXYHUB_RESERVED_WORDS[@]}" "reserved word count"

# --------------------------------------------------------------------------
# validate_domain
# --------------------------------------------------------------------------

_assert_ok validate_domain "example.com"
_assert_ok validate_domain "sub.example.co.uk"
_assert_ok validate_domain "a-b.example.com"
_assert_ok validate_domain "xn--nxasmq6b.example.com"

_assert_fail validate_domain "example"
_assert_fail validate_domain "-bad.example.com"
_assert_fail validate_domain "bad-.example.com"
_assert_fail validate_domain "bad..example.com"
_assert_fail validate_domain "ex_ample.com"
_assert_fail validate_domain "example.c"
_assert_fail validate_domain "example .com"
_assert_fail validate_domain ""

# --------------------------------------------------------------------------
# validate_repo
# --------------------------------------------------------------------------

_assert_ok validate_repo "xtls/xray-core"
_assert_ok validate_repo "foo/bar"
_assert_ok validate_repo "a1-b2/c.d_e"
_assert_ok validate_repo "O/r"

_assert_fail validate_repo "foo"
_assert_fail validate_repo "foo/"
_assert_fail validate_repo "/bar"
_assert_fail validate_repo "foo/bar/baz"
_assert_fail validate_repo "-foo/bar"
_assert_fail validate_repo "foo bar/baz"
_assert_fail validate_repo ""

# --------------------------------------------------------------------------
# validate_version
# --------------------------------------------------------------------------

_assert_ok validate_version "1.2.3"
_assert_ok validate_version "v1.2.3"
_assert_ok validate_version "0.0.0"
_assert_ok validate_version "1.2.3-rc.1"
_assert_ok validate_version "1.2.3+build.5"
_assert_ok validate_version "v10.20.30-alpha+001"

_assert_fail validate_version "1.2"
_assert_fail validate_version "1.2.3.4"
_assert_fail validate_version "v"
_assert_fail validate_version "01.2.3"
_assert_fail validate_version "1.2.x"
_assert_fail validate_version ""

# --------------------------------------------------------------------------
# random_token
# --------------------------------------------------------------------------

tok32=$(random_token 32)
_assert_eq "32" "${#tok32}" "random_token 32 length"
tok64=$(random_token 64)
_assert_eq "64" "${#tok64}" "random_token 64 length"
_assert_eq "32" "$(printf '%s' "$tok32" | LC_ALL=C tr -cd 'A-Za-z0-9_-' | wc -c | tr -d ' ')" "random_token 32 url-safe"
tok32b=$(random_token 32)
if [[ "$tok32" != "$tok32b" ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: two random_token 32 calls produced identical tokens\n' >&2
fi
_assert_fail random_token 0
_assert_fail random_token -5
_assert_fail random_token abc
_assert_fail random_token ""

# --------------------------------------------------------------------------
# root_path
# --------------------------------------------------------------------------

# Empty assignment is intentional: clears PROXYHUB_ROOT for the child env.
# shellcheck disable=SC1007
_assert_eq "/etc/proxyhub" "$(PROXYHUB_ROOT= root_path /etc/proxyhub)" "root_path without prefix"
_assert_eq "/tmp/x/etc/proxyhub" "$(PROXYHUB_ROOT=/tmp/x root_path /etc/proxyhub)" "root_path with prefix"

# --------------------------------------------------------------------------
# Service identity / directories (test mode via PROXYHUB_ROOT)
# --------------------------------------------------------------------------

TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-lib-test.XXXXXX")
SIGN_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/proxyhub-sign-test.XXXXXX")
trap 'rm -rf "$TEST_ROOT" "$SIGN_ROOT"' EXIT

_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; ensure_proxyhub_group' "$SCRIPT_DIR/lib.sh"
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; ensure_proxyhub_user' "$SCRIPT_DIR/lib.sh"
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; ensure_directories' "$SCRIPT_DIR/lib.sh"

for d in var/lib/proxyhub etc/proxyhub var/log/proxyhub var/backups/proxyhub; do
    if [[ -d "$TEST_ROOT/$d" ]]; then PASS=$((PASS + 1)); else
        FAIL=$((FAIL + 1)); printf 'FAIL: directory %s not created\n' "$TEST_ROOT/$d" >&2
    fi
done

_perm_of() {
    if stat -f '%Lp' "$1" >/dev/null 2>&1; then stat -f '%Lp' "$1"; else stat -c '%a' "$1"; fi
}
_assert_eq "750" "$(_perm_of "$TEST_ROOT/var/lib/proxyhub")" "state dir perms"
_assert_eq "750" "$(_perm_of "$TEST_ROOT/etc/proxyhub")" "config dir perms"
_assert_eq "750" "$(_perm_of "$TEST_ROOT/var/log/proxyhub")" "log dir perms"
_assert_eq "700" "$(_perm_of "$TEST_ROOT/var/backups/proxyhub")" "backup dir perms"

# Idempotent: second run also succeeds.
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; ensure_directories' "$SCRIPT_DIR/lib.sh"

# --------------------------------------------------------------------------
# write_proxyhub_unit
# --------------------------------------------------------------------------

_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; write_proxyhub_unit' "$SCRIPT_DIR/lib.sh"
UNIT="$TEST_ROOT/etc/systemd/system/proxyhub.service"
_assert_file_contains "$UNIT" "User=proxyhub"
_assert_file_contains "$UNIT" "Group=proxyhub"
_assert_file_contains "$UNIT" "ExecStart=/usr/local/bin/proxyhub --config /etc/proxyhub/config.yaml"
_assert_file_contains "$UNIT" "Restart=on-failure"
_assert_file_contains "$UNIT" "NoNewPrivileges=true"
_assert_file_contains "$UNIT" "ProtectSystem=strict"
_assert_file_contains "$UNIT" "PrivateTmp=true"
_assert_file_contains "$UNIT" "ReadWritePaths=/var/lib/proxyhub /var/log/proxyhub /var/backups/proxyhub"
if grep -qi "xray" <(grep -i "execstart\|\[unit\]" "$UNIT" | grep -iv "proxyhub\|description\|after\|wants") ; then
    FAIL=$((FAIL + 1)); printf 'FAIL: unit references a separate Xray service\n' >&2
else
    PASS=$((PASS + 1))
fi

# --------------------------------------------------------------------------
# write_caddy_fragment
# --------------------------------------------------------------------------

SITE="aB3_efghijklmnopqrstuv"
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; write_caddy_fragment "example.com" "'"$SITE"'"' "$SCRIPT_DIR/lib.sh"
CADDY="$TEST_ROOT/etc/caddy/conf.d/proxyhub.caddy"
_assert_file_contains "$CADDY" "example.com {"
_assert_file_contains "$CADDY" "reverse_proxy 127.0.0.1:8080"
# C1 防线依赖替换语义:转发头必须被代理整体替换,调用方伪造的 XFF 才活不过代理跳。
_assert_file_contains "$CADDY" "header_up X-Forwarded-For {remote_host}"
_assert_file_contains "$CADDY" "header_up X-Real-IP {remote_host}"
_assert_file_contains "$CADDY" "path /$SITE /$SITE/*"
_assert_file_contains "$CADDY" "respond 404"

# Rejects invalid inputs and does not write.
rm -f "$CADDY"
_assert_fail env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; write_caddy_fragment "bad_domain" "'"$SITE"'"' "$SCRIPT_DIR/lib.sh"
_assert_fail env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; write_caddy_fragment "example.com" "short"' "$SCRIPT_DIR/lib.sh"
_assert_fail env PROXYHUB_ROOT="$TEST_ROOT" bash -c 'source "$0"; write_caddy_fragment "example.com" "xX9_admin_xX9_xX9_xX9_x"' "$SCRIPT_DIR/lib.sh"
if [[ ! -e "$CADDY" ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: caddy fragment written despite invalid input\n' >&2
fi

# --------------------------------------------------------------------------
# Docker caddy mode (ADR 0035)
# --------------------------------------------------------------------------

# Image matcher: all caddy reference forms match, lookalikes do not.
_assert_ok _docker_image_is_caddy caddy
_assert_ok _docker_image_is_caddy caddy:2
_assert_ok _docker_image_is_caddy caddy:2-alpine
_assert_ok _docker_image_is_caddy caddy@sha256:0000000000000000000000000000000000000000000000000000000000000000
_assert_ok _docker_image_is_caddy docker.io/library/caddy:2
_assert_fail _docker_image_is_caddy nginx:latest
_assert_fail _docker_image_is_caddy caddy-fork:1
_assert_fail _docker_image_is_caddy example.com/team/caddy-proxy:1

# Candidate enumeration filters running containers by caddy image.
cand=$(bash -c '
    source "$0"
    _docker() {
        printf "web caddy:2\napi nginx:1.27\nedge docker.io/library/caddy:2-alpine\n"
        printf "pin caddy@sha256:0000000000000000000000000000000000000000000000000000000000000000\n"
        printf "fork example.com/team/caddy-fork:1\n"
    }
    docker_caddy_candidates
' "$SCRIPT_DIR/lib.sh")
_assert_eq "$(printf 'web\nedge\npin')" "$cand" "docker_caddy_candidates filters caddy images"

# Docker CLI unavailable -> enumeration fails closed.
_assert_fail bash -c '
    source "$0"
    _docker() { return 1; }
    docker_caddy_candidates
' "$SCRIPT_DIR/lib.sh"

# Explicit container validation matrix.
_assert_ok bash -c '
    source "$0"
    _docker() { case $3 in *State.Running*) printf "true\n" ;; *Config.Image*) printf "caddy:2\n" ;; esac; return 0; }
    docker_validate_caddy_container caddy
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() { return 1; }
    docker_validate_caddy_container ghost
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() { case $3 in *State.Running*) printf "false\n" ;; esac; return 0; }
    docker_validate_caddy_container stopped
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() { case $3 in *State.Running*) printf "true\n" ;; *Config.Image*) printf "nginx:latest\n" ;; esac; return 0; }
    docker_validate_caddy_container web
' "$SCRIPT_DIR/lib.sh"

# Layout resolution: bind mount resolves to the root layout with its host
# Source directory.
mroot=$(env PROXYHUB_ROOT="$TEST_ROOT" bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv/caddy"
    _docker() { printf "bind\t/etc/caddy\t/srv/caddy\t\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "root /srv/caddy" "$mroot" "root layout bind resolution"

# Layout resolution: named volumes resolve to the mount's Source (the docker
# volume data path - correct even for a custom data-root or rootless docker),
# with the same existence check as bind mounts.
mroot=$(env PROXYHUB_ROOT="$TEST_ROOT" bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/var/lib/docker/volumes/caddy-data/_data"
    _docker() { printf "volume\t/etc/caddy\t/var/lib/docker/volumes/caddy-data/_data\tcaddy-data\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "root /var/lib/docker/volumes/caddy-data/_data" "$mroot" "root layout volume resolution (Source)"
mroot=$(env PROXYHUB_ROOT="$TEST_ROOT" bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/data/docker/volumes/caddy-data/_data"
    _docker() { printf "volume\t/etc/caddy\t/data/docker/volumes/caddy-data/_data\tcaddy-data\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "root /data/docker/volumes/caddy-data/_data" "$mroot" "root layout custom data-root Source"
_assert_fail env PROXYHUB_ROOT="$TEST_ROOT" bash -c '
    source "$0"
    _docker() { printf "volume\t/etc/caddy\t/var/lib/docker/volumes/gone/_data\tgone\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh"

# Layout resolution: a single-file Caddyfile bind is the file layout (ADR
# 0039) - accepted, not refused.
mroot=$(env PROXYHUB_ROOT="$TEST_ROOT" bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv"
    : > "$PROXYHUB_ROOT/srv/Caddyfile"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/srv/Caddyfile\t\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "file /srv/Caddyfile" "$mroot" "file layout single-Caddyfile resolution"

# Layout resolution: unrelated mounts and non-file sub-paths fail closed with
# remediation guidance pointing at a directory mount.
_assert_fail bash -c '
    source "$0"
    _docker() { printf "volume\t/data\t/var/lib/docker/volumes/other/_data\tother\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh"
sf_msg=$(bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/conf.d\t/srv/conf.d\t\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh" 2>&1 || true)
if [[ $sf_msg == *"-v /srv/caddy:/etc/caddy"* ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: sub-path mount error lacks remediation guidance: %s\n' "$sf_msg" >&2
fi
if [[ $sf_msg == *"mounts only sub-paths under /etc/caddy"* ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: sub-path mount error misdiagnosed: %s\n' "$sf_msg" >&2
fi
# A mount AT /etc/caddy whose Source is unusable (missing dir / unsupported
# type) fails closed with an accurate "not a usable persistent directory".
bad_msg=$(bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy\t/srv/proxyhub-test-definitely-absent\t\n"; }
    docker_caddy_config_layout caddy
' "$SCRIPT_DIR/lib.sh" 2>&1 || true)
if [[ $bad_msg == *"not a usable persistent directory"* ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: unusable /etc/caddy mount misdiagnosed: %s\n' "$bad_msg" >&2
fi

# Site block splicing (file layout, ADR 0039): insert, idempotent replace,
# and clean removal, leaving the operator's other content byte-identical.
_sb_root=$(mktemp -d)
cat > "$_sb_root/Caddyfile" <<'EOFCF'
{
	auto_https off
}

other.example.com {
	respond 200
}
EOFCF
_assert_ok env PROXYHUB_ROOT="$_sb_root" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/Caddyfile\t\n"; }
    write_caddy_siteblock proxy.example.com abcDEF123_-xYz 127.0.0.1:8080
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$_sb_root/Caddyfile" "# >>> proxyhub managed"
_assert_file_contains "$_sb_root/Caddyfile" "proxy.example.com {"
_assert_file_contains "$_sb_root/Caddyfile" "reverse_proxy 127.0.0.1:8080"
_assert_file_contains "$_sb_root/Caddyfile" "other.example.com {"
# Replace with a new site path: exactly one managed block survives.
_assert_ok env PROXYHUB_ROOT="$_sb_root" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/Caddyfile\t\n"; }
    write_caddy_siteblock proxy.example.com NEWpath999_-xYz 127.0.0.1:8080
' "$SCRIPT_DIR/lib.sh"
if [[ $(grep -c "^# >>> proxyhub managed" "$_sb_root/Caddyfile") == 1 ]] && \
   [[ $(grep -c "^# <<< proxyhub managed" "$_sb_root/Caddyfile") == 1 ]] && \
   grep -qF "NEWpath999_-xYz" "$_sb_root/Caddyfile" && \
   ! grep -qF "abcDEF123_-xYz" "$_sb_root/Caddyfile" && \
   grep -qF "other.example.com {" "$_sb_root/Caddyfile"; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); printf 'FAIL: siteblock replace left inconsistent Caddyfile\n' >&2
fi
# Removal: block gone, operator content intact.
_assert_ok env PROXYHUB_ROOT="$_sb_root" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/Caddyfile\t\n"; }
    remove_caddy_siteblock
' "$SCRIPT_DIR/lib.sh"
if ! grep -qF "proxyhub managed" "$_sb_root/Caddyfile" && grep -qF "other.example.com {" "$_sb_root/Caddyfile"; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); printf 'FAIL: siteblock removal inconsistent\n' >&2
fi
rm -rf "$_sb_root"

# Marker completeness guard (Check review): a dangling BEGIN fails closed on
# both splice and removal instead of deleting to end-of-file.
_dangle=$(mktemp -d)
printf 'operator head\n# >>> proxyhub managed - do not edit between markers\nstray line\ntrailing operator content\n' \
    > "$_dangle/Caddyfile"
_assert_fail env PROXYHUB_ROOT="$_dangle" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/Caddyfile\t\n"; }
    write_caddy_siteblock proxy.example.com abcDEF123_-xYz 127.0.0.1:8080
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$_dangle/Caddyfile" "trailing operator content"
_assert_fail env PROXYHUB_ROOT="$_dangle" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/Caddyfile\t\n"; }
    remove_caddy_siteblock
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$_dangle/Caddyfile" "trailing operator content"
rm -rf "$_dangle"

# Missing trailing newline: the begin marker must NOT glue onto the
# operator's last line.
_nonl=$(mktemp -d)
printf 'last line no newline' > "$_nonl/Caddyfile"
_assert_ok env PROXYHUB_ROOT="$_nonl" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    _docker() { printf "bind\t/etc/caddy/Caddyfile\t/Caddyfile\t\n"; }
    write_caddy_siteblock proxy.example.com abcDEF123_-xYz 127.0.0.1:8080
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$_nonl/Caddyfile" "last line no newline"
_assert_eq "1" "$(grep -c '^# >>> proxyhub managed' "$_nonl/Caddyfile")" \
    "begin marker on its own line despite missing trailing newline"
rm -rf "$_nonl"

# Port publishing: host networking is exempt; bridge must publish 80 and 443.
_assert_ok bash -c '
    source "$0"
    _docker() { printf "host\n"; }
    docker_caddy_ports_published caddy
' "$SCRIPT_DIR/lib.sh"
_assert_ok bash -c '
    source "$0"
    _docker() {
        case $1 in
            inspect) printf "bridge\n" ;;
            port) printf "80/tcp -> 0.0.0.0:80\n443/tcp -> 0.0.0.0:443\n" ;;
        esac
        return 0
    }
    docker_caddy_ports_published caddy
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() {
        case $1 in
            inspect) printf "bridge\n" ;;
            port) printf "80/tcp -> 0.0.0.0:80\n" ;;
        esac
        return 0
    }
    docker_caddy_ports_published caddy
' "$SCRIPT_DIR/lib.sh"
# Port publishing: 8080/8443 must NOT satisfy the 80/443 requirement (the
# match is line-anchored; unprivileged caddy images publish high ports).
_assert_fail bash -c '
    source "$0"
    _docker() {
        case $1 in
            inspect) printf "bridge\n" ;;
            port) printf "8080/tcp -> 0.0.0.0:8080\n8443/tcp -> 0.0.0.0:8443\n" ;;
        esac
        return 0
    }
    docker_caddy_ports_published caddy
' "$SCRIPT_DIR/lib.sh"

# Docker channel: fragment path resolves through the mount (bind + volume).
frag=$(env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv/caddy"
    _docker() { printf "bind\t/etc/caddy\t/srv/caddy\t\n"; }
    caddy_fragment_path
' "$SCRIPT_DIR/lib.sh")
_assert_eq "$TEST_ROOT/srv/caddy/conf.d/proxyhub.caddy" "$frag" "docker fragment path (bind)"
frag=$(env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/var/lib/docker/volumes/caddy-data/_data"
    _docker() { printf "volume\t/etc/caddy\t/var/lib/docker/volumes/caddy-data/_data\tcaddy-data\n"; }
    caddy_fragment_path
' "$SCRIPT_DIR/lib.sh")
_assert_eq "$TEST_ROOT/var/lib/docker/volumes/caddy-data/_data/conf.d/proxyhub.caddy" "$frag" "docker fragment path (volume)"

# Docker channel: fmt/validate/reload execute inside the container at the
# constant container paths (host-side arguments are mount paths, not visible
# to the container).
DL="$TEST_ROOT/docker.calls"
: >"$DL"
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv/caddy"
    _docker() {
        printf "%s\n" "$*" >>"$PROXYHUB_ROOT/docker.calls"
        case $3 in *Mounts*) printf "bind\t/etc/caddy\t/srv/caddy\t\n" ;; esac
        return 0
    }
    caddy_fmt /host/side/proxyhub.caddy &&
        caddy_validate /host/side/Caddyfile &&
        caddy_reload
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$DL" "exec -- cad caddy fmt --overwrite /etc/caddy/conf.d/proxyhub.caddy"
_assert_file_contains "$DL" "exec -- cad caddy validate --config /etc/caddy/Caddyfile"
_assert_file_contains "$DL" "exec -- cad caddy reload --config /etc/caddy/Caddyfile"

# Docker channel: reload failure falls back to docker restart with the same
# interruption warning as the native systemctl restart fallback.
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv/caddy"
    _docker() {
        printf "%s\n" "$*" >>"$PROXYHUB_ROOT/docker.calls"
        case $3 in *Mounts*) printf "bind\t/etc/caddy\t/srv/caddy\t\n" ;; esac
        [[ $1 == exec ]] && return 1
        return 0
    }
    caddy_reload 2>"$PROXYHUB_ROOT/reload.err"
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$DL" "restart -- cad"
_assert_file_contains "$TEST_ROOT/reload.err" "brief interruption"

# Docker channel, file layout: fmt is a no-op (validate/reload unchanged).
DFL="$TEST_ROOT/docker-file.calls"
: >"$DFL"
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    : > "$PROXYHUB_ROOT/srv/Caddyfile"
    _docker() {
        printf "%s\n" "$*" >>"$PROXYHUB_ROOT/docker-file.calls"
        case $3 in *Mounts*) printf "bind\t/etc/caddy/Caddyfile\t/srv/Caddyfile\t\n" ;; esac
        return 0
    }
    caddy_fmt /host/side/Caddyfile &&
        caddy_validate /host/side/Caddyfile
' "$SCRIPT_DIR/lib.sh"
if grep -qF "caddy fmt" "$DFL"; then
    FAIL=$((FAIL + 1)); printf 'FAIL: file layout unexpectedly ran caddy fmt\n' >&2
else
    PASS=$((PASS + 1))
fi
_assert_file_contains "$DFL" "exec -- cad caddy validate --config /etc/caddy/Caddyfile"

# Docker channel: write_caddy_fragment lands on the host-side mount path.
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv/caddy"
    _docker() { printf "bind\t/etc/caddy\t/srv/caddy\t\n"; }
    write_caddy_fragment "example.com" "'"$SITE"'"
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$TEST_ROOT/srv/caddy/conf.d/proxyhub.caddy" "example.com {"
_assert_file_contains "$TEST_ROOT/srv/caddy/conf.d/proxyhub.caddy" "reverse_proxy 127.0.0.1:8080"

# --------------------------------------------------------------------------
# Docker bridge topology (ADR 0035, ticket 03/#18)
# --------------------------------------------------------------------------

# IPv4 network math: exact prefixes, a non-octet boundary, and refuse junk.
_assert_eq "172.17.0.0/16" "$(_ipv4_network 172.17.0.1 16)" "ipv4 network /16"
_assert_eq "192.168.32.0/20" "$(_ipv4_network 192.168.32.1 20)" "ipv4 network /20 non-octet"
_assert_eq "10.0.0.0/8" "$(_ipv4_network 10.0.3.1 8)" "ipv4 network /8"
_assert_eq "172.18.0.0/24" "$(_ipv4_network 172.18.0.9 24)" "ipv4 network /24"
_assert_fail _ipv4_network 999.1.1.1 16
_assert_fail _ipv4_network not-an-ip 16
_assert_fail _ipv4_network 172.17.0.1 0
_assert_fail _ipv4_network 172.17.0.1 33

# Network mode: printed verbatim; inspect failure and empty mode fail closed.
_assert_eq "bridge" "$(bash -c '
    source "$0"
    _docker() { printf "bridge\n"; }
    docker_caddy_network_mode caddy
' "$SCRIPT_DIR/lib.sh")" "network mode printed"
_assert_fail bash -c '
    source "$0"
    _docker() { return 1; }
    docker_caddy_network_mode caddy
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() { printf "\n"; }
    docker_caddy_network_mode caddy
' "$SCRIPT_DIR/lib.sh"

# Bridge topology: gateway + subnet derived from the attached network.
topo=$(bash -c '
    source "$0"
    _docker() { printf "bridge\t172.17.0.1\t16\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "172.17.0.1 172.17.0.0/16" "$topo" "bridge topology single network"

# Multi-network determinism: the alphabetically first network with an IPv4
# gateway wins, regardless of inspect output order.
topo=$(bash -c '
    source "$0"
    _docker() { printf "znet\t172.19.0.1\t24\nanet\t172.18.0.1\t16\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "172.18.0.1 172.18.0.0/16" "$topo" "bridge topology multi-network deterministic"

# Missing/invalid prefix length falls back to the /16 default-bridge
# convention.
topo=$(bash -c '
    source "$0"
    _docker() { printf "bridge\t172.17.0.1\t0\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh")
_assert_eq "172.17.0.1 172.17.0.0/16" "$topo" "bridge topology prefix fallback"

# Public-range gateways are refused: the admin plane may only bind
# RFC1918/loopback/link-local (bounded widening, ADR 0035).
pub_msg=$(bash -c '
    source "$0"
    _docker() { printf "pubnet\t8.8.8.1\t24\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh" 2>&1 || true)
if [[ $pub_msg == *"not a private address"* ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: public gateway not refused: %s\n' "$pub_msg" >&2
fi
_assert_fail bash -c '
    source "$0"
    _docker() { printf "pubnet\t8.8.8.1\t24\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh"
# 172.x boundary: 172.16-172.31 is private, 172.32 is not.
_assert_ok bash -c '
    source "$0"
    _docker() { printf "bridge\t172.31.0.1\t16\n"; }
    docker_caddy_bridge_topology caddy >/dev/null
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() { printf "bridge\t172.32.0.1\t16\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh"

# IPv6-only gateways and gateway-less networks fail closed.
_assert_fail bash -c '
    source "$0"
    _docker() { printf "bridge\tfd00::1\t64\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() { printf "bridge\t\t0\n"; }
    docker_caddy_bridge_topology caddy
' "$SCRIPT_DIR/lib.sh"

# host-gateway mapping gate: present passes; absent fails closed with both
# docker-run and compose remediation forms in the message.
_assert_ok bash -c '
    source "$0"
    _docker() { printf "host.docker.internal:host-gateway\n"; }
    docker_caddy_require_host_gateway caddy
' "$SCRIPT_DIR/lib.sh"
hg_msg=$(bash -c '
    source "$0"
    _docker() { printf "myalias:10.0.0.2\n"; }
    docker_caddy_require_host_gateway caddy
' "$SCRIPT_DIR/lib.sh" 2>&1 || true)
if [[ $hg_msg == *"--add-host host.docker.internal:host-gateway"* ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: host-gateway error lacks --add-host guidance: %s\n' "$hg_msg" >&2
fi
if [[ $hg_msg == *"extra_hosts"* ]]; then PASS=$((PASS + 1)); else
    FAIL=$((FAIL + 1)); printf 'FAIL: host-gateway error lacks compose guidance: %s\n' "$hg_msg" >&2
fi

# Topology adoption: host networks take the zero-change path (bridge globals
# stay empty); bridge networks resolve gateway/subnet after the mapping gate.
top=$(bash -c '
    source "$0"
    _docker() { printf "host\n"; }
    docker_caddy_prepare_topology caddy >/dev/null &&
        printf "NET=%s GW=%s SUB=%s\n" "$PROXYHUB_DOCKER_NETMODE" "$PROXYHUB_BRIDGE_GATEWAY" "$PROXYHUB_BRIDGE_SUBNET"
' "$SCRIPT_DIR/lib.sh")
_assert_eq "NET=host GW= SUB=" "$top" "host network zero-change adoption"

top=$(bash -c '
    source "$0"
    _docker() {
        case $3 in
            *HostConfig.NetworkMode*) printf "bridge\n" ;;
            *NetworkSettings.Networks*) printf "bridge\t172.17.0.1\t16\n" ;;
            *HostConfig.ExtraHosts*) printf "host.docker.internal:host-gateway\n" ;;
        esac
        return 0
    }
    docker_caddy_prepare_topology caddy >/dev/null &&
        printf "NET=%s GW=%s SUB=%s\n" "$PROXYHUB_DOCKER_NETMODE" "$PROXYHUB_BRIDGE_GATEWAY" "$PROXYHUB_BRIDGE_SUBNET"
' "$SCRIPT_DIR/lib.sh")
_assert_eq "NET=bridge GW=172.17.0.1 SUB=172.17.0.0/16" "$top" "bridge adoption resolves topology"

_assert_fail bash -c '
    source "$0"
    _docker() {
        case $3 in
            *HostConfig.NetworkMode*) printf "bridge\n" ;;
            *NetworkSettings.Networks*) printf "bridge\t172.17.0.1\t16\n" ;;
            *HostConfig.ExtraHosts*) printf "\n" ;;
        esac
        return 0
    }
    docker_caddy_prepare_topology caddy >/dev/null 2>&1
' "$SCRIPT_DIR/lib.sh"
_assert_fail bash -c '
    source "$0"
    _docker() {
        case $3 in
            *HostConfig.NetworkMode*) printf "bridge\n" ;;
            *NetworkSettings.Networks*) printf "bridge\t\t0\n" ;;
            *HostConfig.ExtraHosts*) printf "host.docker.internal:host-gateway\n" ;;
        esac
        return 0
    }
    docker_caddy_prepare_topology caddy >/dev/null 2>&1
' "$SCRIPT_DIR/lib.sh"

# Fragment upstream: bridge mode targets host.docker.internal carrying only
# the listen port; every other topology keeps the loopback listener.
_assert_eq "127.0.0.1:8080" "$(bash -c 'source "$0"; caddy_upstream_addr' "$SCRIPT_DIR/lib.sh")" \
    "native upstream loopback"
_assert_eq "127.0.0.1:8080" "$(env PROXYHUB_CADDY_MODE=docker PROXYHUB_DOCKER_NETMODE=host \
    bash -c 'source "$0"; caddy_upstream_addr' "$SCRIPT_DIR/lib.sh")" \
    "host-network docker upstream loopback"
_assert_eq "host.docker.internal:8080" "$(env PROXYHUB_CADDY_MODE=docker PROXYHUB_DOCKER_NETMODE=bridge \
    bash -c 'source "$0"; caddy_upstream_addr' "$SCRIPT_DIR/lib.sh")" \
    "bridge upstream host-gateway"
_assert_eq "host.docker.internal:18080" "$(env PROXYHUB_CADDY_MODE=docker PROXYHUB_DOCKER_NETMODE=bridge \
    PROXYHUB_LISTEN_ADDR=127.0.0.1:18080 bash -c 'source "$0"; caddy_upstream_addr' "$SCRIPT_DIR/lib.sh")" \
    "bridge upstream carries custom listen port"

# Bridge fragment: reverse_proxy host.docker.internal, XFF/X-Real-IP stay
# replace-written.
_assert_ok env PROXYHUB_ROOT="$TEST_ROOT" PROXYHUB_CADDY_MODE=docker PROXYHUB_DOCKER_NETMODE=bridge \
    PROXYHUB_CADDY_CONTAINER=cad bash -c '
    source "$0"
    mkdir -p "$PROXYHUB_ROOT/srv/caddy-br"
    _docker() { printf "bind\t/etc/caddy\t/srv/caddy-br\t\n"; }
    write_caddy_fragment "example.com" "'"$SITE"'"
' "$SCRIPT_DIR/lib.sh"
_assert_file_contains "$TEST_ROOT/srv/caddy-br/conf.d/proxyhub.caddy" "reverse_proxy host.docker.internal:8080"
_assert_file_contains "$TEST_ROOT/srv/caddy-br/conf.d/proxyhub.caddy" "header_up X-Forwarded-For {remote_host}"
_assert_file_contains "$TEST_ROOT/srv/caddy-br/conf.d/proxyhub.caddy" "header_up X-Real-IP {remote_host}"

# --------------------------------------------------------------------------
# verify_minisig (ADR 0036 signature trust anchor)
# --------------------------------------------------------------------------

# The verifier may only rely on tools a stock Ubuntu/Debian base image always
# ships: coreutils (base64/tail/head/wc/sed/mktemp) + openssl.
for tool in base64 tail head wc sed mktemp openssl; do
    _assert_ok command -v "$tool"
done

# Synthetic throwaway Ed25519 keypair, generated inside the test scratch and
# destroyed with it. Never committed, never usable for real releases.
openssl genpkey -algorithm ed25519 -out "$SIGN_ROOT/testkey.pem" 2>/dev/null
openssl pkey -in "$SIGN_ROOT/testkey.pem" -pubout -outform DER 2>/dev/null \
    | tail -c 32 >"$SIGN_ROOT/testkey.raw"
# Minisign text-format pubkey: base64("Ed" || 8-byte keynum || 32-byte key).
TEST_PUBKEY_B64=$( { printf 'Ed'; head -c 8 /dev/zero; cat "$SIGN_ROOT/testkey.raw"; } | base64 | tr -d '\n')

# _make_minisig PRIVKEY_PEM FILE OUT - assemble a minisign-format .minisig for
# FILE using a test private key: line 1 is a comment, line 2 is
# base64("Ed" || 8-byte keynum || 64-byte Ed25519 signature).
_make_minisig() {
    openssl pkeyutl -sign -inkey "$1" -rawin -in "$2" -out "$3.sigbin" 2>/dev/null
    {
        printf 'untrusted comment: signature from synthetic test key\n'
        { printf 'Ed'; head -c 8 /dev/zero; cat "$3.sigbin"; } | base64 | tr -d '\n'
        printf '\n'
    } >"$3"
}

printf '%s  %s\n' "0000000000000000000000000000000000000000000000000000000000000000" \
    "proxyhub_0.0.0_linux_amd64.tar.gz" >"$SIGN_ROOT/SHA256SUMS"
_make_minisig "$SIGN_ROOT/testkey.pem" "$SIGN_ROOT/SHA256SUMS" "$SIGN_ROOT/SHA256SUMS.minisig"

# Positive: a valid signature verifies.
_assert_ok verify_minisig "$SIGN_ROOT/SHA256SUMS" "$SIGN_ROOT/SHA256SUMS.minisig" "$TEST_PUBKEY_B64"

# Tamper: content changed after signing is rejected.
printf '%s  %s\n' "1111111111111111111111111111111111111111111111111111111111111111" \
    "proxyhub_0.0.0_linux_amd64.tar.gz" >"$SIGN_ROOT/SHA256SUMS.tampered"
_assert_fail verify_minisig "$SIGN_ROOT/SHA256SUMS.tampered" "$SIGN_ROOT/SHA256SUMS.minisig" "$TEST_PUBKEY_B64"

# Missing .minisig fails closed.
_assert_fail verify_minisig "$SIGN_ROOT/SHA256SUMS" "$SIGN_ROOT/SHA256SUMS.minisig.absent" "$TEST_PUBKEY_B64"

# Malformed .minisig fails closed: wrong decoded length, wrong prefix.
printf 'untrusted comment: x\n%s\n' "$(printf 'EdSHORT' | base64 | tr -d '\n')" >"$SIGN_ROOT/badsize.minisig"
_assert_fail verify_minisig "$SIGN_ROOT/SHA256SUMS" "$SIGN_ROOT/badsize.minisig" "$TEST_PUBKEY_B64"
{
    printf 'untrusted comment: x\n'
    { printf 'ED'; head -c 72 /dev/zero; } | base64 | tr -d '\n'
    printf '\n'
} >"$SIGN_ROOT/badprefix.minisig"
_assert_fail verify_minisig "$SIGN_ROOT/SHA256SUMS" "$SIGN_ROOT/badprefix.minisig" "$TEST_PUBKEY_B64"

# Embedded release pubkey constant is well-formed minisign text format.
_assert_eq "42" "$(printf '%s' "$PROXYHUB_MINISIGN_PUBKEY" | base64 -d | wc -c | tr -d ' ')" \
    "embedded pubkey decodes to 42 bytes"
_assert_eq "Ed" "$(printf '%s' "$PROXYHUB_MINISIGN_PUBKEY" | base64 -d | head -c 2)" \
    "embedded pubkey has Ed prefix"

# Missing openssl fails closed (stripped PATH; the openssl check fires before
# any other tool is needed).
_assert_fail env PATH=/nonexistent bash -c \
    'source "$0"; verify_minisig "$1" "$2" "$3"' \
    "$SCRIPT_DIR/lib.sh" "$SIGN_ROOT/SHA256SUMS" "$SIGN_ROOT/SHA256SUMS.minisig" "$TEST_PUBKEY_B64"

# --------------------------------------------------------------------------
# resolve_latest_version: two-channel latest resolution (ADR 0037/0038)
# --------------------------------------------------------------------------

# Garbage GitHub redirect (non-version tag) -> jsDelivr channel resolves.
rv=$(bash -c '
    source "$0"
    _curl() {
        local url=""
        while (($#)); do
            case $1 in -o) shift 2 ;; -*) shift ;; *) url=$1; shift ;; esac
        done
        case $url in
            *releases/latest*) printf "https://github.com/o/r/releases/tag/not-a-version\n" ;;
            *data.jsdelivr.com*) printf "{\n \"versions\": [\n  {\n    \"version\": \"2.0.0\"\n  }\n ]\n}\n" ;;
        esac
        return 0
    }
    resolve_latest_version o/r
' "$SCRIPT_DIR/lib.sh" 2>/dev/null)
_assert_eq "v2.0.0" "$rv" "garbage redirect falls through to jsDelivr"

# jsDelivr list with only prereleases -> fail closed, no usable version.
_assert_fail bash -c '
    source "$0"
    _curl() {
        local url=""
        while (($#)); do
            case $1 in -o) shift 2 ;; -*) shift ;; *) url=$1; shift ;; esac
        done
        case $url in
            *releases/latest*) return 1 ;;
            *data.jsdelivr.com*) printf "{\n \"versions\": [\n  {\n    \"version\": \"2.0.0-rc.1\"\n  },\n  {\n    \"version\": \"1.0.0-beta\"\n  }\n ]\n}\n" ;;
        esac
        return 0
    }
    resolve_latest_version o/r
' "$SCRIPT_DIR/lib.sh" >/dev/null 2>&1

# Both channels down -> rc 1, and stderr preserves the GitHub error FIRST
# and the jsDelivr failure second (diagnosis never misattributes).
both_err=$(bash -c '
    source "$0"
    _curl() { return 1; }
    resolve_latest_version o/r
' "$SCRIPT_DIR/lib.sh" 2>&1 >/dev/null || true)
if [[ $both_err == *"could not resolve the latest release"* && $both_err == *"jsDelivr data API as well"* ]] && \
   [[ ${both_err%%jsDelivr*} == *"could not resolve"* ]]; then
    PASS=$((PASS + 1))
else
    FAIL=$((FAIL + 1)); printf 'FAIL: dual-channel failure misattributed: %s\n' "$both_err" >&2
fi

# --------------------------------------------------------------------------
# release_base_candidates (ADR 0036/0037 fetch-level prefix fallback)

# Default official base, not explicit -> official first, prefix follows.
cand=$(release_base_candidates \
    "https://github.com/taliove/proxyhub/releases/download" "taliove/proxyhub" 0)
_assert_eq "https://github.com/taliove/proxyhub/releases/download
https://gh-proxy.com/https://github.com/taliove/proxyhub/releases/download" \
    "$cand" "candidates: default base yields official + prefix"

# Explicit base -> single candidate, no fallback (operator owns the mirror).
cand=$(release_base_candidates \
    "https://github.com/taliove/proxyhub/releases/download" "taliove/proxyhub" 1)
_assert_eq "https://github.com/taliove/proxyhub/releases/download" \
    "$cand" "candidates: explicit flag suppresses prefix"

# Custom (non-official) base -> single candidate even without explicit flag.
cand=$(release_base_candidates "https://mirror.example.com/dl" "taliove/proxyhub" 0)
_assert_eq "https://mirror.example.com/dl" "$cand" "candidates: custom base never gains prefixes"

# Already-prefixed base (probe fell back) -> single candidate, no double-wrap.
cand=$(release_base_candidates \
    "https://gh-proxy.com/https://github.com/taliove/proxyhub/releases/download" "taliove/proxyhub" 0)
_assert_eq "https://gh-proxy.com/https://github.com/taliove/proxyhub/releases/download" \
    "$cand" "candidates: prefixed base does not re-wrap"

# --------------------------------------------------------------------------
# docker_validate_caddy_container (custom plugin-baked images, ADR 0035)

eval "_docker_orig() $(declare -f _docker | tail -n +2)"
MOCK_IMAGE=""
MOCK_EXEC_VER=""
_docker() {
    case $1 in
        inspect)
            case $3 in
                *Running*) printf 'true\n' ;;
                *Image*) printf '%s\n' "$MOCK_IMAGE" ;;
            esac
            return 0
            ;;
        exec)
            if [[ -n $MOCK_EXEC_VER ]]; then
                printf '%s\n' "$MOCK_EXEC_VER"
                return 0
            fi
            return 1
            ;;
    esac
    return 1
}

# Official image passes by name (no functional probe needed).
MOCK_IMAGE="caddy:2.11.4"
_assert_ok docker_validate_caddy_container caddy
# Registry-prefixed official image passes by name.
MOCK_IMAGE="registry.example.com/team/caddy:2"
_assert_ok docker_validate_caddy_container caddy
# Custom plugin-baked image passes via functional `caddy version` probe.
MOCK_IMAGE="caddy-dnspod:2.11.4-fb7cc31-fix1"
MOCK_EXEC_VER="v2.11.4 h1:abcdef"
_assert_ok docker_validate_caddy_container caddy
# Custom image whose binary does not answer fails closed.
MOCK_EXEC_VER=""
_assert_fail docker_validate_caddy_container caddy
# Lookalike with a bogus banner fails closed.
MOCK_IMAGE="team/caddy-proxy:1"
MOCK_EXEC_VER="caddy-fork build x"
_assert_fail docker_validate_caddy_container caddy
# v1-era banner (wrong major) fails closed.
MOCK_IMAGE="caddy-custom:1"
MOCK_EXEC_VER="v1.0.0"
_assert_fail docker_validate_caddy_container caddy

eval "_docker() $(declare -f _docker_orig | tail -n +2)"
unset -f _docker_orig MOCK_IMAGE MOCK_EXEC_VER

# --------------------------------------------------------------------------

printf 'passed: %d, failed: %d\n' "$PASS" "$FAIL"
(( FAIL == 0 ))
