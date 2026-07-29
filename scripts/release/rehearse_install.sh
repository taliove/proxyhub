#!/usr/bin/env bash
# rehearse_install.sh - 发布前远程真机一键安装演练。
#
# 在配置的目标机上完整走一遍生产一键安装(install.sh 从 GitHub 下载制品),
# 验证可用后无痕卸载,确认环境零残留。随机域名 + 跳过公网 HTTPS 健康检查
# (PROXYHUB_SKIP_PUBLIC_HEALTH=1 演练缝),不要求证书走通。
#
# 配置: scripts/release/rehearsal.local.conf(gitignored,隐私)
# 用法: bash scripts/release/rehearse_install.sh
set -Eeuo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CONF="${REHEARSAL_CONF:-$SCRIPT_DIR/rehearsal.local.conf}"

log()  { printf '[rehearse] %s\n' "$*"; }
fail() { printf '[rehearse] ERROR: %s\n' "$*" >&2; exit 1; }

# --- 配置加载与校验 ---------------------------------------------------------

load_config() {
    [[ -f $CONF ]] || fail "缺少 $CONF(复制 rehearsal.example.conf 并填入目标机;目标机隐私信息永不签入)"
    # shellcheck disable=SC1090
    source "$CONF"
    : "${REHEARSAL_SSH_TARGET:?REHEARSAL_SSH_TARGET 必填}"
    REHEARSAL_LISTEN_PORT="${REHEARSAL_LISTEN_PORT:-18080}"
    REHEARSAL_SSH_OPTS="${REHEARSAL_SSH_OPTS:-}"
    [[ $REHEARSAL_LISTEN_PORT =~ ^[0-9]+$ ]] || fail "REHEARSAL_LISTEN_PORT 非数字: $REHEARSAL_LISTEN_PORT"
    if [[ -z ${REHEARSAL_VERSION:-} ]]; then
        REHEARSAL_VERSION=$(curl -fsSL https://api.github.com/repos/taliove/proxyhub/releases \
            | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' | head -1)
        [[ -n $REHEARSAL_VERSION ]] || fail "无法从 GitHub 解析最新版本号"
    fi
    log "target=$REHEARSAL_SSH_TARGET port=$REHEARSAL_LISTEN_PORT version=$REHEARSAL_VERSION"
}

# --- 远程执行原语 -------------------------------------------------------------

TARGET="" SSH_OPTS=()
rsh() { ssh "${SSH_OPTS[@]}" "$TARGET" "$@"; }
# rsh_script: 以 bash -s 方式在远端执行 stdin 脚本(避免引号地狱)。
rsh_script() { ssh "${SSH_OPTS[@]}" "$TARGET" 'bash -s' ; }

# --- 步骤 --------------------------------------------------------------------

preflight() {
    log "preflight: ssh/sudo/系统/既有安装/端口"
    local sudo_err
    if ! sudo_err=$(rsh 'sudo -n true' 2>&1); then
        fail "目标机 ssh/sudo 预检失败: ${sudo_err:-连接失败}(要求免密 ssh + 免密 sudo)"
    fi
    local info
    info=$(rsh 'echo "os=$(. /etc/os-release; echo "$ID $VERSION_ID") arch=$(uname -m) caddy=$(command -v caddy || true) record=$(sudo -n test -f /root/.proxyhub-install-info && echo present || echo absent) unit=$(sudo -n test -f /etc/systemd/system/proxyhub.service && echo present || echo absent)"') \
        || fail "ssh 不通: $TARGET"
    log "  $info"
    [[ $info == *"record=absent"* && $info == *"unit=absent"* ]] ||
        fail "目标机已存在受管安装,拒绝在其上演练(先手动 proxyhubctl uninstall 或换机器)"
    rsh "ss -ltnH | awk '{print \$4}' | grep -q ':${REHEARSAL_LISTEN_PORT}\$'" \
        && fail "端口 $REHEARSAL_LISTEN_PORT 已被占用"
    return 0
}

# ensure_caddy: 缺 caddy 时按官方源安装,并标记由我们安装(演练后卸载还原)。
WE_INSTALLED_CADDY=0
ensure_caddy() {
    if rsh 'command -v caddy >/dev/null 2>&1'; then
        log "caddy: 已存在,复用"
        return 0
    fi
    log "caddy: 缺失,按官方源安装(演练结束后卸载还原)"
    rsh_script <<'EOF' || fail "caddy 安装失败"
set -Eeuo pipefail
sudo -n apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl >/dev/null
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo -n gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo -n tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
sudo -n apt-get update -qq
sudo -n apt-get install -y -qq caddy >/dev/null
EOF
    WE_INSTALLED_CADDY=1
}

run_install() {
    local rand domain
    rand=$(head -c4 /dev/urandom | od -An -tx1 | tr -d ' \n')
    domain="ph-rehearse-${rand}.example.com"
    log "install: domain=$domain(随机保留域,不解析;公网健康检查走演练缝)"
    # 一键生产路径原样走:GitHub raw 的 install.sh,经 stdin 管道进 sudo bash
    # (sudo 会关多余 fd,process substitution 在 sudo 下不可靠,故用管道)。
    rsh "curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh \
        | sudo -n PROXYHUB_SKIP_PUBLIC_HEALTH=1 bash -s -- --non-interactive \
            --domain '$domain' --skip-dns-check \
            --version '$REHEARSAL_VERSION' \
            --listen-addr '127.0.0.1:${REHEARSAL_LISTEN_PORT}'" \
        >"$WORKDIR/install.log" 2>&1 || {
        sed -e 's/^ *Admin password *:.*/  Admin password : <redacted>/' "$WORKDIR/install.log" >&2
        fail "install.sh 执行失败(输出如上,密码已脱敏)"
    }
    # 安装输出含一次性管理员密码:只留本地 0600 文件,日志打印脱敏版。
    chmod 0600 "$WORKDIR/install.log"
    grep -v "Admin password" "$WORKDIR/install.log" | tail -5
}

verify_install() {
    log "verify: 二进制版本自报 / proxyhubctl status / 回环健康"
    rsh "sudo -n /usr/local/bin/proxyhub version" || fail "proxyhub version 失败"
    rsh "sudo -n /usr/local/bin/proxyhubctl status" || fail "proxyhubctl status 失败"
    local site_path
    site_path=$(rsh "sudo -n sed -n 's/^SITE_PATH=//p' /root/.proxyhub-install-info") \
        || fail "读安装记录失败"
    rsh "curl -fsS -o /dev/null 'http://127.0.0.1:${REHEARSAL_LISTEN_PORT}/${site_path}/healthz'" \
        || fail "回环健康检查失败"
    log "  回环健康 OK(site_path 不落日志)"
}

cleanup() {
    log "cleanup: proxyhubctl uninstall --purge"
    rsh 'sudo -n /usr/local/bin/proxyhubctl uninstall --purge --yes' >/dev/null 2>&1 || true
    if ((WE_INSTALLED_CADDY == 1)); then
        log "cleanup: 卸载演练期间安装的 caddy 及其源"
        rsh_script <<'EOF' || true
set -Eeuo pipefail
sudo -n apt-get remove -y -qq caddy >/dev/null 2>&1 || true
sudo -n rm -f /etc/apt/sources.list.d/caddy-stable.list /usr/share/keyrings/caddy-stable-archive-keyring.gpg
sudo -n apt-get update -qq >/dev/null 2>&1 || true
EOF
    fi
}

verify_residue() {
    log "residue: 残留检查"
    local left
    left=$(rsh_script <<EOF
set -Eeuo pipefail
out=""
sudo -n test -f /etc/systemd/system/proxyhub.service && out="\$out unit"
sudo -n test -e /usr/local/bin/proxyhub && out="\$out binary"
sudo -n test -e /usr/local/bin/proxyhubctl && out="\$out ctl"
sudo -n test -d /etc/proxyhub && out="\$out config-dir"
sudo -n test -d /var/lib/proxyhub && out="\$out state-dir"
sudo -n test -d /var/log/proxyhub && out="\$out log-dir"
sudo -n test -d /var/backups/proxyhub && out="\$out backup-dir"
sudo -n test -f /root/.proxyhub-install-info && out="\$out install-record"
sudo -n test -f /etc/caddy/conf.d/proxyhub.caddy && out="\$out caddy-fragment"
id proxyhub >/dev/null 2>&1 && out="\$out user"
ss -ltnH | awk '{print \$4}' | grep -q ':${REHEARSAL_LISTEN_PORT}\$' && out="\$out port-${REHEARSAL_LISTEN_PORT}"
echo "residue=\${out:-none}"
EOF
)
    log "  $left"
    [[ $left == *"residue=none"* ]] || fail "存在残留: $left"
}

main() {
    load_config
    TARGET="$REHEARSAL_SSH_TARGET"
    # shellcheck disable=SC2206
    SSH_OPTS=(${REHEARSAL_SSH_OPTS:--o BatchMode=yes -o ConnectTimeout=10})
    WORKDIR=$(mktemp -d)
    trap 'rm -rf "$WORKDIR"' EXIT

    preflight
    ensure_caddy
    # 无论成功与否都尝试清理,保证"不污染环境"在失败路径同样成立。
    trap 'cleanup; rm -rf "$WORKDIR"' EXIT
    run_install
    verify_install
    cleanup
    trap 'rm -rf "$WORKDIR"' EXIT
    verify_residue
    log "PASS: 一键安装演练通过且环境零残留(target=$TARGET version=$REHEARSAL_VERSION)"
}

main "$@"
