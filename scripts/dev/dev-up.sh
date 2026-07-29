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

# 数据卷里没有库 = 首次:先以一次性容器初始化,再正式起服务
# (卷名带 compose 项目前缀 proxyhub_,与 up 用的是同一个)
if ! docker run --rm -v proxyhub_proxyhub-dev-data:/data --entrypoint test proxyhub:dev -f /data/data.db 2>/dev/null; then
    echo "==> 首次启动,初始化开发管理员 ${DEV_USER}"
    printf '%s' "$DEV_PASS" | "${COMPOSE[@]}" run --rm -T proxyhub \
        ./proxyhub init -config /app/config.yaml \
        -username "$DEV_USER" -password-stdin \
        -domain "$DEV_URL" -site-path "/${SITE_PATH}"
fi

echo "==> 起服务"
"${COMPOSE[@]}" up -d

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
