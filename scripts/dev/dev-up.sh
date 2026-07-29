#!/usr/bin/env bash
# dev-up.sh - Docker 本机开发环境一键起(幂等)。
# 构建镜像 -> 首次初始化开发管理员 -> 起服务 -> 打印访问信息。
# 开发账号口令只用于本机开发环境,见 docs/DEVELOPMENT.md。
set -Eeuo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$REPO_ROOT"

COMPOSE=(docker compose -f docker-compose.dev.yml)
DEV_PORT=18081
DEV_URL="http://localhost:${DEV_PORT}"
SITE_PATH="dev-admin-x7k9m2p4q8w3"
DEV_USER=devadmin
DEV_PASS=proxyhub-dev

echo "==> 构建开发镜像(proxyhub:dev)"
"${COMPOSE[@]}" build

# 派生容器配置(与 Dockerfile 同源 sed + 开发放开 MFA),compose 挂载覆盖
# 镜像内置的 /app/config.yaml;config.example.yaml 变更时自动跟随
mkdir -p var/dev
sed -e 's/host: "127.0.0.1"/host: "0.0.0.0"/' \
    -e 's|path: "var/data/data.db"|path: "/data/data.db"|' \
    -e 's/# mfa_optional: false/mfa_optional: true/' \
    config.example.yaml > var/dev/docker-config.yaml
# config.example.yaml 漂移时 sed 会静默 no-op,与 Dockerfile 同款守卫
grep -q 'host: "0.0.0.0"' var/dev/docker-config.yaml
grep -q 'path: "/data/data.db"' var/dev/docker-config.yaml
grep -q 'mfa_optional: true' var/dev/docker-config.yaml

# 数据目录里没有库 = 首次:先以一次性容器初始化,再正式起服务
# (bind mount,不依赖 compose 项目名,clone 到任何目录都成立)
if [ ! -f var/dev/data/data.db ]; then
    echo "==> 首次启动,初始化开发管理员 ${DEV_USER}"
    printf '%s' "$DEV_PASS" | "${COMPOSE[@]}" run --rm -T proxyhub \
        ./proxyhub init -config /app/config.yaml \
        -username "$DEV_USER" -password-stdin \
        -domain "$DEV_URL" -site-path "/${SITE_PATH}"
fi

echo "==> 起服务"
"${COMPOSE[@]}" up -d

# 容器内 root 创建的数据文件收归宿主机用户,否则 go vet 遍历 ./... 报权限错
"${COMPOSE[@]}" exec -T proxyhub chown -R "$(id -u):$(id -g)" /data 2>/dev/null || true

# 等健康
for _ in $(seq 1 30); do
    code=$(curl -s -o /dev/null -w '%{http_code}' "${DEV_URL}/${SITE_PATH}/" || true)
    [ "$code" = "200" ] && break
    sleep 1
done

cat <<EOF

✅ 开发环境就绪
   管理后台: ${DEV_URL}/${SITE_PATH}/
   账号:     ${DEV_USER}
   密码:     ${DEV_PASS}
   停止:     make dev-docker-down
EOF
