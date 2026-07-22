# 开发指南

本文是 ProxyHub 开发者上手指南,帮助你 5 分钟跑起开发环境。

> **权威规则在 [CLAUDE.md](../CLAUDE.md)**:目录布局、构建入口、测试门槛、安全红线等工程宪法以 CLAUDE.md 为准。本文只做入口,与 CLAUDE.md 冲突时以 CLAUDE.md 为准。

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

### 前端开发

```bash
make dev-frontend
# 访问 http://localhost:3000
# API 自动代理到后端 :8080
```

### 后端开发

```bash
make dev-backend
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

完整目录范式与"写文件先想归属"铁律见 CLAUDE.md §2。

## 深入阅读

- **[CLAUDE.md](../CLAUDE.md)** — 工程宪法(必读)
- **[CONTEXT.md](../CONTEXT.md)** — 领域术语表(机场/订阅地址/节点/聚合/刷新)
- **[架构决策记录](adr/)** — 为什么这么设计
- **[生产部署指南](DEPLOY.md)** — 运维视角的部署流程
