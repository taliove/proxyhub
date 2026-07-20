# ProxyHub 工程原则

本文件是本仓库的工程宪法。任何会话、任何提交都必须遵守。
术语以 [CONTEXT.md](CONTEXT.md) 为准(机场/订阅地址/节点/聚合/刷新有严格定义)。

## 1. 仓库内容边界

**只签入**:源代码、测试、构建脚本、迁移 SQL、长期文档(见 §3)、CI 配置。

**永不签入**:
- 运行时产物:`var/` 下的一切、`*.db`、`*.log`、`xray_config.json`、`config.yaml`、`dist/`、二进制
- 凭证:任何真实节点密码/UUID/token/订阅 URL —— 测试 fixture 一律用 `example.com` + 全零 UUID
- 过程产物:合并分析、实施计划/总结/验证报告、AI 会话工作笔记、`.scratch/`、`.superpowers/`
- 死备份:`*_old.*`、`*_backup.*`、注释掉的死代码文件
- 依赖目录:`node_modules/`、`vendor/`

判断标准:**这个文件对"下一个人理解系统"有帮助吗?** 没有就不进仓库。

## 2. 目录范式(布局基本法)

对齐 [golang-standards/project-layout](https://github.com/golang-standards/project-layout),并结合本项目(单二进制 + 内嵌前端 + 本地运行)裁剪:

```
cmd/server/      唯一入口(main 保持薄,逻辑全部下沉 internal)
internal/        私有实现(Go 编译器强制私有;按领域分包)
web/             前端 SPA 源码(vite 构建产物嵌入 cmd/server/web,不入库)
scripts/         install / release 运维脚本及其 test_*.sh 套件
docs/            长期文档(见 §3)
dist/            一切编译产物(二进制、release 包)—— 禁止写到别处
var/             一切本地运行态(gitignored)—— 禁止写到仓库根:
  var/data/      SQLite 数据库(默认 var/data/data.db)
  var/log/       服务日志、pid
  var/xray/      生成的 xray_config.json(含会话凭证,绝不入库)
.test/           集成测试脚手架的临时数据(gitignored)
internal/*/testdata/  Go 惯例:包内测试 fixture
```

**铁律**:
- 仓库根只放入口与控制文件(`Makefile`、`install.sh`、`start.sh`、`Dockerfile`、`go.mod`、`config.example.yaml`、`*.md`)。任何新文件出现在根目录都要先回答"为什么它属于根"。
- 写文件先想归属:编译产物→`dist/`,运行态→`var/`,测试临时→`.test/` 或 `testdata/`,源码→按领域归位。
- 代码里**禁止**把默认路径指向仓库根(如 `data.db`、`xray_config.json`);默认路径必须落在 `var/` 下,且写入方负责 `MkdirAll`(见 `store.Open`、`writeXrayConfig`)。
- 生产环境路径(`/var/lib/proxyhub`、`/etc/proxyhub`、`/usr/local/bin`)只属于 `install.sh`/`proxyhubctl` 的领域,与开发布局互不污染。

## 3. 文档政策

| 留 | 删 |
|---|---|
| 术语表(CONTEXT.md) | 过程记录(计划/总结/验证/合并分析) |
| ADR(docs/adr/,为什么这么做) | AI 工作流产物(plans/superpowers) |
| 设计文档(架构/模型/规格) | 带日期的里程碑文档 |
| 运维文档(DEPLOY/SECURITY/ACCEPTANCE) | 旧版本死备份 |

文档过期即删,不留"考古层"。

## 4. 提交规范

- 消息:**纯英文、纯 ASCII**(不用 `—`/`→`,用 `-`/`->`),conventional commits 格式:`<type>: <description>`,type ∈ feat/fix/refactor/docs/test/chore/perf/ci
- 作者:`taliove2009 <taliove2009@gmail.com>`(repo 级 config)
- 一个提交一个语义;修复不混进功能提交

## 5. 构建与运行(命令入口基本法)

**一切构建/测试/检查动作,唯一入口是 `make`。** 文档与 Skill 中禁止出现裸 `go build`/`npm run build` 示范;裸 `go build ./cmd/server`(无 `-o`)会在根目录掉落二进制,是历史事故源头。

| 动作 | 命令 |
|---|---|
| 完整构建(前端+后端) | `make build`(改了前端必须跑这个,go:embed) |
| 只改后端 | `make build-backend` |
| 只改前端 | `make build-frontend` |
| Go 测试 | `make test` |
| 安装/运维脚本套件 | `make test-shell` |
| 静态检查 | `make vet` |
| 签入前聚合检查 | `make check`(= vet + test + test-shell) |
| 前端/后端开发服务器 | `make dev-frontend` / `make dev-backend` |
| 多平台发布 | `make build-all` |

唯一豁免:定向调试允许 `go test ./internal/<pkg>/ -run <TestName>`(纯读操作,不落盘)。

构建顺序不可逆:`make build` → `./start.sh` 重启(日志在 `var/log/`)。改前端不重启 = 没生效。日常开发范式见 `.claude/skills/dev-workflow`。

## 6. 测试门槛

- 每次签入前:`make check`(vet + Go 测试 + shell 套件)
- 推送前:`make check` 全量 + `make build` 验证完整构建
- 既有失败 3 处(2 个默认模板测试 + `TestHandleTestNode_MissingTarget`,处置待定见 backlog):已在 `make test` 中显式隔离(`-skip`,Makefile 有注释清单);`make test-all` 可跑全量(预期红,用于完整性审计)。**不许通过改测试让它们消失**,也不许扩大隔离名单。

## 7. 安全红线

- Xray 任何入站必须 `listen: 127.0.0.1`(`internal/xray/config.go` 与 `internal/distribution/routing.go` 两处生成器,改一必查二,有测试断言)
- 不禁用 TLS 验证(无 `InsecureSkipVerify`、无 `--insecure`)
- 管理面只走 Site Path + loopback;订阅地址 = 随机 path + token,公开但不可枚举
- 签入前必过 `gitleaks`(配置见 `.gitleaks.toml`)

## 8. 工作流 Skills

- 日常开发(编译/运行/测试/目录归属):`.claude/skills/dev-workflow`
- 签入前:`.claude/skills/pre-commit`
- 推送前:`.claude/skills/pre-push`

Skills 是流程,不是建议——逐条执行,不允许跳项。
