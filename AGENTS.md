# ProxyHub 工程原则

本文件是本仓库的工程宪法,也是**唯一主控**。任何会话、任何提交都必须遵守。
`CLAUDE.md` 只是一行指针,不承载内容。禁止再创建 `GEMINI.md` / `CODEBUDDY.md` / `.cursorrules` / `.windsurfrules` 等镜像拷贝,或 `.codebuddy/` / `.gemini/` / `.qoder/` / `.kiro/` 等工具配置目录(2026-07-28 已全部移除)——其他 AI 工具一律引用本文件。
术语以 [CONTEXT.md](CONTEXT.md) 为准(机场/订阅地址/节点/聚合/刷新有严格定义)。

## 1. 仓库内容边界

**只签入**:源代码、测试、构建脚本、迁移 SQL、长期文档(见 §3)、CI 配置。

**永不签入**:
- 运行时产物:`var/` 下的一切、`*.db`、`*.log`、`config.yaml`、`dist/`、二进制
- 凭证:任何真实节点密码/UUID/token/订阅 URL —— 测试 fixture 一律用 `example.com` + 全零 UUID
- 过程产物:合并分析、实施计划/总结/验证报告、AI 会话工作笔记、`.scratch/`、`.superpowers/`、spec-*.md(功能工作稿,归属 `.scratch/spec/`,见 §3)
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
.test/           集成测试脚手架的临时数据(gitignored)
internal/*/testdata/  Go 惯例:包内测试 fixture
```

**铁律**:
- 仓库根只放入口与控制文件(`Makefile`、`install.sh`、`Dockerfile`、`go.mod`、`config.example.yaml`、`*.md`)。任何新文件出现在根目录都要先回答"为什么它属于根"。
- 写文件先想归属:编译产物→`dist/`,运行态→`var/`,测试临时→`.test/` 或 `testdata/`,源码→按领域归位。
- 代码里**禁止**把默认路径指向仓库根(如 `data.db`);默认路径必须落在 `var/` 下,且写入方负责 `MkdirAll`(见 `store.Open`)。
- 生产环境路径(`/var/lib/proxyhub`、`/etc/proxyhub`、`/usr/local/bin`)只属于 `install.sh`/`proxyhubctl` 的领域,与开发布局互不污染。

## 3. 文档政策

### 分类法(命名即受众)

| 类别 | 命名约定 | 受众 | 内容 | 例子 |
|---|---|---|---|---|
| 术语表 | `CONTEXT.md`(根目录) | 所有人 | 领域术语严格定义 | CONTEXT.md |
| 用户向文档 | 大写文件名 `*.md` | 部署/使用本系统的人 | 怎么操作:安装、备份、FAQ、安全模型 | DEPLOY.md、SECURITY.md、FAQ.md |
| 设计文档 | `design-*.md` | 开发者 | 是什么、怎么运作,随代码演进 | design-node-exam.md |
| 决策记录 | `docs/adr/NNNN-*.md` | 开发者 | 为什么这么做,不可变,只追加新 ADR | adr/0003-*.md |
| 开发者入口 | `docs/DEVELOPMENT.md` | 开发者 | 环境搭建与开发循环,规则一律链回 AGENTS.md | DEVELOPMENT.md |
| README | `README.md`(根目录) | 最终用户 | 最小上手信息,细节一律链出 | README.md |

### 留 / 删

| 留 | 删 |
|---|---|
| 术语表(CONTEXT.md) | 过程记录(计划/总结/验证/合并分析) |
| ADR(docs/adr/,为什么这么做) | AI 工作流产物(plans/superpowers) |
| 设计文档(design-*,架构/模型) | 带日期的里程碑文档 |
| 用户向文档(DEPLOY/SECURITY/FAQ) | 旧版本死备份 |

**spec-\*.md 是过程产物**:功能开发的工作稿,归属 `.scratch/spec/`,**禁止签入 `docs/`**。功能落地时,spec 中未被 ADR/design 覆盖的持久知识必须蒸馏(决策进 ADR,模型进 design-*,步骤与截图丢弃),原稿随 `.scratch/` 消亡。

**README 段落白名单**:定位/badges、核心特性、快速开始、生产部署、常见问题(精选+链出)、安全(一段话+链出)、文档、贡献、License。新增段落必须回答"为什么是用户上手必需";开发者内容(技术栈/项目结构/make 命令)归 docs/DEVELOPMENT.md。

文档过期即删,不留"考古层"。文档语言:全中文(commit message 按 §4 用英文,与文档语言是两回事)。

**标点约定**:用户向文档(README、DEPLOY、SECURITY、FAQ 及未来同类大写文件)中文行文用全角标点(，；：？！（）),代码块/行内代码/URL/命令/英文术语括号内不受影响;开发者向文档(CONTEXT.md、design-*、ADR、DEVELOPMENT.md)维持半角行文,不回改(ADR 不可变)。动过用户向文档,提交前过一遍 `zh-punct` skill 的脚本机械化规整。

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
| 前端 lint(ESLint + Prettier + 类型检查) | `make lint-frontend` |
| 签入前聚合检查 | `make check`(= vet + test + test-shell + lint-frontend) |
| 前端/后端开发服务器 | `make dev-frontend` / `make dev-backend` |
| 服务运行生命周期(幂等) | `make start` / `make stop` / `make restart` / `make status` |
| 多平台发布 | `make build-all` |

唯一豁免:定向调试允许 `go test ./internal/<pkg>/ -run <TestName>`(纯读操作,不落盘)。

构建顺序不可逆:`make build` → `make restart` 重启(日志在 `var/log/`)。改前端不重启 = 没生效。运行生命周期只有 make 一个入口(`make start|stop|restart|status`)。日常开发范式见领域 skill:`backend`(Go)/`frontend`(web/)/`ops`(运行生命周期)。

验证纪律:汇报"完成"前必须自行验证生效(前端 = `make build` + `make restart` 后确认页面/接口实际变化;后端 = 接口实测或日志确认)。禁止只改代码未验证就汇报。

## 6. 测试门槛

- 每次签入前:`make check`(vet + Go 测试 + shell 套件 + 前端 lint)
- 推送前:`make check` 全量 + `make build` 验证完整构建
- 机械门禁由 git hooks 硬约束(`.githooks/`,`make hooks` 激活,已挂入 `make deps`):pre-commit = gitleaks staged + 垃圾文件守卫,commit-msg = 格式 + ASCII,pre-push = `make check`。hook 管"过不过";判断类步骤与 Check agent 语义审查仍在 skill(§8)
- 测试套件保持全绿,无隔离名单。2026-07-21 起,原 3 处既有失败已按维护者决策清零(模板断言改为从模板内容推导;nodetest 缺失目标拆分 400/404 语义)。**不许用 `-skip` 或改测试来掩盖真实回归**;发现失败先定位原因,再决定修代码还是修断言。

## 7. 安全红线

- 不禁用 TLS 验证(无 `InsecureSkipVerify`、无 `--insecure`)
- 管理面只走 Site Path + loopback;订阅地址 = 随机 path + token,公开但不可枚举
- 签入前必过 `gitleaks`(配置见 `.gitleaks.toml`)

## 8. Agents 与 Skills

### Agents(独立上下文,各守一类,仅三个)

- **Plan**(`.claude/agents/plan.md`):spec 收敛后的影响面分析与实施规划——动哪里、撞什么(ADR/术语/在途分支)、怎么拆步。只读分析,不写码。
- **Write**(`.claude/agents/write.md`):批量 ticket 实施者,按既定 plan/spec 写码。默认子代理并发调度;串行实施需先说明理由。
- **Check**(`.claude/agents/check.md`):签入/推送前的对抗性评审,Go 语义 + 安全攻击面双视角。只评审,不写码。

并行会话隔离:用户声明某范围由另一会话处理时,严禁触碰该范围。

### 领域 Skill(按任务组合使用)

| Skill | 领域 |
|---|---|
| `.claude/skills/product/` | 需求拷问收敛 spec、文档写作/ADR/spec 蒸馏 |
| `.claude/skills/frontend/` | web/ 前端开发循环、lint、go:embed 构建纪律 |
| `.claude/skills/backend/` | Go 开发范式、目录归属执行、测试纪律 |
| `.claude/skills/database/` | SQLite/迁移 SQL/fixture 脱敏/数据路径纪律 |
| `.claude/skills/ops/` | 运行生命周期(start/stop/restart/日志)、发布管理 |

### 流程 Skill(git 生命周期门禁,逐条执行,不允许跳项)

- 签入前:`.claude/skills/pre-commit`(dispatch Check agent 审 Go 增量)
- 推送前:`.claude/skills/pre-push`(dispatch Check agent 审推送增量的攻击面)

### 图谱 Skill(code-review-graph 驱动)

`explore-codebase` / `debug-issue` / `refactor-safely` / `review-changes` —— 探索、调试、重构、评审时优先用图谱工具。

## 9. 发布

GitHub Actions tag 触发自动发布(`.github/workflows/release.yml`):validate → test → package(矩阵 tarball + SHA256SUMS + attest)→ docker(GHCR)。版本唯一事实源是 `VERSION` 文件,tag 必须等于 `v$(cat VERSION)`。制品命名契约 `proxyhub_<version>_<os>_<arch>.tar.gz`(下划线)由 `scripts/release/package.sh` 生产、`install.sh` 与 `proxyhubctl update` 消费,**三处必须同步**。全流程见 ops skill 发布章节。

## MCP Tools: code-review-graph

本仓库配有知识图谱(code-review-graph MCP,配置见 `.mcp.json`,由 `.claude/settings.json` 的 hooks 自动维护更新)。
探索代码、评估影响面、代码评审时**优先用图谱工具**(`semantic_search_nodes_tool` / `query_graph_tool` / `get_impact_radius_tool` / `detect_changes_tool` / `get_review_context_tool` 等),图谱覆盖不到再退回 Grep/Glob/Read。用法细节见 `.claude/skills/explore-codebase/` 等四个图谱 skill。
