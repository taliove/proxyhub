# 开发指南

本文是 ProxyHub 开发者上手指南,帮助你 5 分钟跑起开发环境。

> **权威规则在 [AGENTS.md](../AGENTS.md)**:目录布局、构建入口、测试门槛、安全红线等工程宪法以 AGENTS.md 为准。本文只做入口,与 AGENTS.md 冲突时以 AGENTS.md 为准。

## 前置要求

- Go 1.22+
- Node.js 20+
- npm

## 克隆项目

```bash
git clone https://github.com/taliove/proxyhub.git
cd proxyhub
make deps   # 安装 Go/前端依赖,并激活本地 git hooks(.githooks/)
```

## 开发循环

### Docker 本机开发环境(优先)

本机有 Docker 时的推荐方式:一条命令起好带账号的完整环境,与源码直跑互不影响(独立数据卷、独立端口 18081)。

```bash
make dev-docker        # 构建镜像 + 首次初始化 + 起服务(幂等)
make dev-docker-logs   # 看日志
make dev-docker-down   # 停止
```

首次启动自动初始化开发账号,脚本输出访问信息(也可随时重跑 `make dev-docker` 查看):

| 项 | 值 |
|---|---|
| 管理后台 | http://localhost:18081/dev-admin-x7k9m2p4q8w3/ |
| 账号 | `devadmin` |
| 密码 | `proxyhub-dev` |

> ⚠️ 这组账号口令是**公开写死的本机开发约定**(同 postgres/postgres 之于本地开发),只用于本机 Docker 开发环境,任何真实部署都不得使用。生产初始化走安装器或 Setup 向导。

开发环境经 `mfa_optional: true`(compose 挂载的派生配置)放开强制 MFA,登录直进业务面,不用绑认证器;生产默认 `false`,行为不变。

默认只绑回环(开发账号口令公开,绑全网卡等于把管理面送给同局域网的人);确需局域网访问(手机/他机检查)时:

```bash
DEV_BIND=0.0.0.0 make dev-docker
```

构成(全部入库,可复现):`docker-compose.dev.yml`(端口/数据卷)、`scripts/dev/dev-up.sh`(幂等编排:构建 → 无库则 `proxyhub init` 建号 → 起服务 → 打印访问信息)、`config.dev.example.yaml`(源码直跑开发配置模板)。

### 源码直跑开发

```bash
make dev-frontend   # 前端 vite dev server,http://localhost:3000,API 代理到 :8080
make dev-backend    # 后端
```

源码直跑想用独立配置时:`cp config.dev.example.yaml config.dev.yaml` 按需修改(gitignored),建号:

```bash
echo "proxyhub-dev" | ./dist/proxyhub init -config config.dev.yaml \
  -username devadmin -password-stdin \
  -domain http://localhost:8080 -site-path /dev-admin-x7k9m2p4q8w3
```

## 构建与测试

一切构建/测试动作统一经 Makefile(禁止裸 `go build`,避免二进制掉落根目录):

```bash
make build         # 完整构建(前端 + 后端,产物在 dist/)
make build-all     # 多平台构建
make test          # Go 测试
make test-shell    # 安装/运维脚本测试套件
make check         # 签入前聚合检查(vet + test + test-shell + lint-frontend)
make restart       # 重启本地服务(start/stop/status 同族)
make clean         # 清理产物
```

**注意**:前端经 `go:embed` 编入二进制。改了前端必须跑 `make build`(而非只 build-backend),再 `make restart` 重启才生效。服务的启动/停止/重启/状态统一走 `make start|stop|restart|status`(幂等,pid 文件驱动)。

## 项目结构速览

```
cmd/server/      唯一入口(main 保持薄)
internal/        私有实现(按领域分包)
web/             前端 SPA 源码(vite 构建产物嵌入二进制)
scripts/         install / release 运维脚本及测试套件
docs/            长期文档
dist/            编译产物(gitignored)
var/             本地运行态(gitignored):data/ 数据库、log/ 日志
```

完整目录范式与"写文件先想归属"铁律见 AGENTS.md §2。

## 深入阅读

- **[AGENTS.md](../AGENTS.md)** — 工程宪法(必读)
- **[CONTEXT.md](../CONTEXT.md)** — 领域术语表(机场/订阅地址/节点/聚合/刷新)
- **[架构决策记录](adr/)** — 为什么这么设计
- **[生产部署指南](DEPLOY.md)** — 运维视角的部署流程
